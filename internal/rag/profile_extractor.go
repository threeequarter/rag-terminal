package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"rag-terminal/internal/logging"
	"rag-terminal/internal/nexa"
	"rag-terminal/internal/vector"
)

// ProfileExtractor manages LLM-based fact extraction from conversations
type ProfileExtractor struct {
	llmClient   *nexa.Client
	vectorStore vector.VectorStore
}

// ExtractedFact represents a single fact extracted by the LLM
type ExtractedFact struct {
	Category   string  `json:"category"`
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"`
	Context    string  `json:"context"`
}

// NewProfileExtractor creates a new fact extractor
func NewProfileExtractor(llmClient *nexa.Client, vectorStore vector.VectorStore) *ProfileExtractor {
	return &ProfileExtractor{
		llmClient:   llmClient,
		vectorStore: vectorStore,
	}
}

// ExtractFacts runs after each user message and assistant response to extract user facts
func (pe *ProfileExtractor) ExtractFacts(ctx context.Context, chatID string,
	userMsg string, assistantMsg string) error {

	// 1. Build extraction prompt
	prompt := pe.buildExtractionPrompt(userMsg, assistantMsg)

	// 2. Call LLM with structured output
	extractedFacts, err := pe.callLLMForExtraction(ctx, prompt)
	if err != nil {
		logging.Error("Failed to extract facts with LLM: %v", err)
		return fmt.Errorf("LLM extraction failed: %w", err)
	}

	// 3. Process each extracted fact
	for _, fact := range extractedFacts {
		if err := pe.processFact(ctx, chatID, fact); err != nil {
			// Log but don't fail - extraction is best-effort
			logging.Error("Failed to process fact %s: %v", fact.Key, err)
		}
	}

	logging.Debug("Extracted %d facts from conversation turn", len(extractedFacts))
	return nil
}

// buildExtractionPrompt constructs the LLM prompt for fact extraction
func (pe *ProfileExtractor) buildExtractionPrompt(userMsg, assistantMsg string) string {
	return fmt.Sprintf(`Analyze the following conversation turn and extract any factual information about the user.

User message: %s

Assistant response: %s

Extract facts in the following categories:
- Identity: name, location, age, etc.
- Professional: role, company, experience level, responsibilities
- Preferences: programming languages, tools, architectural styles, methodologies
- Current projects: what they're working on, goals, challenges
- Current task: what interests user in current task: functions, variables, processes, clients

Return ONLY facts explicitly stated or strongly implied. Do NOT infer speculative information.

For each fact, provide:
1. category: one of [identity, professional, preference, project, personal]
2. key: a concise identifier (e.g., "name", "role", "preference:language")
3. value: the actual value
4. confidence: 0.0-1.0 (1.0 = explicitly stated, 0.7-0.9 = strongly implied, <0.7 = weak inference)
5. source: one of [explicit, inferred]
6. context: the exact phrase from the conversation that supports this fact

Return valid JSON array format:
[
  {
    "category": "identity",
    "key": "name",
    "value": "John",
    "confidence": 1.0,
    "source": "explicit",
    "context": "My name is John"
  }
]

If no facts found, return empty array: []`, userMsg, assistantMsg)
}

// callLLMForExtraction calls the LLM with the extraction prompt and parses the response
func (pe *ProfileExtractor) callLLMForExtraction(ctx context.Context,
	prompt string) ([]ExtractedFact, error) {

	// Get the currently selected model - for now use the first available model
	// In a real implementation, this would use a currently selected model
	models, err := pe.llmClient.GetModels()
	if err != nil || len(models) == 0 {
		return nil, fmt.Errorf("no LLM models available: %w", err)
	}

	// Find a text-generation model
	var selectedModel string
	for _, model := range models {
		if model.Type == "text-generation" {
			selectedModel = model.Name
			break
		}
	}
	if selectedModel == "" {
		return nil, fmt.Errorf("no text-generation model found")
	}

	// Use synchronous chat completion for fact extraction
	resp, err := pe.llmClient.ChatCompletionSync(ctx, nexa.ChatCompletionRequest{
		Model: selectedModel,
		Messages: []nexa.ChatMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: 0.0, // Deterministic extraction
		MaxTokens:   1000,
	})
	if err != nil {
		return nil, fmt.Errorf("chat completion request failed: %w", err)
	}

	// Parse JSON response
	var facts []ExtractedFact
	if err := json.Unmarshal([]byte(resp), &facts); err != nil {
		// Log the actual response for debugging
		logging.Debug("LLM response (failed to parse): %s", resp)
		return nil, fmt.Errorf("failed to parse LLM response as JSON: %w", err)
	}

	return facts, nil
}

// processFact validates and stores an extracted fact to the user's profile with conflict resolution
func (pe *ProfileExtractor) processFact(ctx context.Context, chatID string, newFact ExtractedFact) error {
	// Validate fact
	if newFact.Key == "" || newFact.Value == "" {
		return fmt.Errorf("invalid fact: key and value are required")
	}

	// Skip facts with very low confidence
	if newFact.Confidence < 0.6 {
		logging.Debug("Skipping fact %s due to low confidence: %.2f", newFact.Key, newFact.Confidence)
		return nil
	}

	// Get existing fact if any
	existing, err := pe.vectorStore.GetProfileFact(ctx, chatID, newFact.Key)
	if err != nil {
		logging.Error("Failed to retrieve existing fact %s: %v", newFact.Key, err)
		return err
	}

	// No existing fact - store new one
	if existing == nil {
		profileFact := vector.ProfileFact{
			Key:        newFact.Key,
			Value:      newFact.Value,
			Confidence: newFact.Confidence,
			Source:     newFact.Source,
			Context:    newFact.Context,
			FirstSeen:  time.Now(),
			LastSeen:   time.Now(),
		}

		if err := pe.vectorStore.UpsertProfileFact(ctx, chatID, profileFact); err != nil {
			return fmt.Errorf("failed to upsert profile fact: %w", err)
		}

		logging.Debug("Stored new fact: %s = %s (confidence: %.2f)", newFact.Key, newFact.Value, newFact.Confidence)
		return nil
	}

	// Existing fact found - apply conflict resolution
	return pe.resolveConflict(ctx, chatID, existing, newFact)
}

// resolveConflict handles conflicts between existing and new facts
func (pe *ProfileExtractor) resolveConflict(ctx context.Context, chatID string,
	existing *vector.ProfileFact, newFact ExtractedFact) error {

	// Case 1: Same value - just update LastSeen and boost confidence
	if existing.Value == newFact.Value {
		existing.LastSeen = time.Now()
		// Boost confidence slightly (up to 1.0 max)
		existing.Confidence = math.Min(1.0, existing.Confidence+0.05)

		if err := pe.vectorStore.UpsertProfileFact(ctx, chatID, *existing); err != nil {
			return fmt.Errorf("failed to update existing fact: %w", err)
		}

		logging.Debug("Updated fact confirmation: %s = %s (new confidence: %.2f)", existing.Key, existing.Value, existing.Confidence)
		return nil
	}

	// Case 2: Different value - conflict detected
	// Strategy: Trust higher score = (confidence * recency_score)

	recencyScore := pe.calculateRecencyScore(existing.LastSeen)
	existingScore := existing.Confidence * recencyScore
	newScore := newFact.Confidence * 1.0 // New facts get full recency score

	logging.Debug("Conflict detected for %s: existing_score=%.3f vs new_score=%.3f (recency=%.3f)",
		existing.Key, existingScore, newScore, recencyScore)

	if newScore > existingScore {
		// New fact wins - save old fact to history and update
		logging.Info("New fact wins for %s: '%s' replaces '%s'",
			newFact.Key, newFact.Value, existing.Value)

		updatedFact := vector.ProfileFact{
			Key:        newFact.Key,
			Value:      newFact.Value,
			Confidence: newFact.Confidence,
			Source:     newFact.Source,
			Context:    newFact.Context,
			FirstSeen:  existing.FirstSeen, // Keep original first seen time
			LastSeen:   time.Now(),
		}

		if err := pe.vectorStore.UpsertProfileFact(ctx, chatID, updatedFact); err != nil {
			return fmt.Errorf("failed to upsert updated fact: %w", err)
		}

		return nil
	}

	// Existing fact wins - just update LastSeen to show it was challenged and reconfirmed
	existing.LastSeen = time.Now()
	logging.Info("Existing fact retained for %s: '%s' wins over '%s'",
		existing.Key, existing.Value, newFact.Value)

	if err := pe.vectorStore.UpsertProfileFact(ctx, chatID, *existing); err != nil {
		return fmt.Errorf("failed to update existing fact: %w", err)
	}

	return nil
}

// calculateRecencyScore applies exponential decay to confidence based on time since last update
// Facts lose 10% confidence per week (decay = 0.9^weeks)
// Minimum score is 50% to prevent very old facts from being completely discounted
func (pe *ProfileExtractor) calculateRecencyScore(lastSeen time.Time) float64 {
	weeksSince := time.Since(lastSeen).Hours() / 24 / 7
	decay := math.Pow(0.9, weeksSince)
	return math.Max(0.5, decay) // Never go below 50%
}
