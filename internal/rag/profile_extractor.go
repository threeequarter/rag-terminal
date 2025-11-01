package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
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

// ExtractionError tracks detailed information about extraction failures
type ExtractionError struct {
	Stage       string // "llm_call", "json_parse", "validation"
	OriginalErr error
	Response    string // The raw response that failed
	FactCount   int    // How many facts were successfully extracted before failure
}

func (e *ExtractionError) Error() string {
	return fmt.Sprintf("extraction failed at stage '%s': %v", e.Stage, e.OriginalErr)
}

// ValidatedFacts tracks extraction results with validation stats
type ValidatedFacts struct {
	Facts        []ExtractedFact
	FailedCount  int // Number of facts that failed validation
	Warnings     []string
	ParsingMode  string // "clean_json", "markdown_code_block", "cleanup_required"
}

// NewProfileExtractor creates a new fact extractor
func NewProfileExtractor(llmClient *nexa.Client, vectorStore vector.VectorStore) *ProfileExtractor {
	return &ProfileExtractor{
		llmClient:   llmClient,
		vectorStore: vectorStore,
	}
}

// ExtractFacts runs after each user message and assistant response to extract user facts
// Uses the currently selected LLM model passed from the application
func (pe *ProfileExtractor) ExtractFacts(ctx context.Context, chatID string, llmModel string,
	userMsg string, assistantMsg string) error {

	// Validate that we have a model specified
	if llmModel == "" {
		return fmt.Errorf("no LLM model specified for fact extraction")
	}

	// 1. Build extraction prompt
	prompt := pe.buildExtractionPrompt(userMsg, assistantMsg)

	// 2. Call LLM with structured output using the selected model
	extractedFacts, err := pe.callLLMForExtraction(ctx, llmModel, prompt)
	if err != nil {
		logging.Error("Failed to extract facts with LLM %s: %v", llmModel, err)
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

// callLLMForExtraction calls the LLM with the extraction prompt and parses the response with robust error handling
// Uses the provided llmModel (currently selected by the application)
func (pe *ProfileExtractor) callLLMForExtraction(ctx context.Context, llmModel string,
	prompt string) ([]ExtractedFact, error) {

	// Validate model parameter
	if llmModel == "" {
		return nil, &ExtractionError{
			Stage:       "model_validation",
			OriginalErr: fmt.Errorf("LLM model name is empty"),
		}
	}

	// Call LLM for fact extraction with the currently selected model
	resp, err := pe.callLLMWithRetry(ctx, llmModel, prompt)
	if err != nil {
		return nil, err
	}

	// Parse and validate response
	validatedFacts, err := pe.parseAndValidateResponse(resp)
	if err != nil {
		return nil, err
	}

	// Log extraction quality metrics
	if validatedFacts.FailedCount > 0 {
		logging.Info("Fact extraction: %d valid, %d invalid (parsing mode: %s) using model %s",
			len(validatedFacts.Facts), validatedFacts.FailedCount, validatedFacts.ParsingMode, llmModel)
	}

	for _, warning := range validatedFacts.Warnings {
		logging.Debug("Extraction warning: %s", warning)
	}

	return validatedFacts.Facts, nil
}

// callLLMWithRetry calls the LLM with automatic retry on failure
func (pe *ProfileExtractor) callLLMWithRetry(ctx context.Context, model string, prompt string) (string, error) {
	maxRetries := 2
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			logging.Debug("Retrying fact extraction (attempt %d/%d)", attempt+1, maxRetries+1)
		}

		resp, err := pe.llmClient.ChatCompletionSync(ctx, nexa.ChatCompletionRequest{
			Model: model,
			Messages: []nexa.ChatMessage{
				{Role: "user", Content: prompt},
			},
			Temperature: 0.0, // Deterministic extraction
			MaxTokens:   1000,
		})

		if err != nil {
			lastErr = err
			continue
		}

		return resp, nil
	}

	return "", &ExtractionError{
		Stage:       "llm_call",
		OriginalErr: fmt.Errorf("LLM call failed after %d attempts: %w", maxRetries+1, lastErr),
	}
}

// parseAndValidateResponse extracts JSON and validates facts with robust error handling
func (pe *ProfileExtractor) parseAndValidateResponse(resp string) (*ValidatedFacts, error) {
	vf := &ValidatedFacts{
		Facts:    []ExtractedFact{},
		Warnings: []string{},
	}

	// Step 1: Extract JSON from response (handles markdown code blocks, etc.)
	jsonStr, mode, err := pe.extractJSON(resp)
	vf.ParsingMode = mode

	if err != nil {
		return nil, &ExtractionError{
			Stage:       "json_extraction",
			OriginalErr: err,
			Response:    resp,
		}
	}

	// Step 2: Parse JSON into facts array
	var facts []ExtractedFact
	if err := json.Unmarshal([]byte(jsonStr), &facts); err != nil {
		// Try to extract partial facts if array parsing fails
		if partialFacts, ok := pe.tryPartialExtraction(jsonStr); ok {
			facts = partialFacts
			vf.Warnings = append(vf.Warnings, "Used partial extraction due to malformed JSON array")
		} else {
			return nil, &ExtractionError{
				Stage:       "json_parse",
				OriginalErr: fmt.Errorf("failed to parse JSON: %w", err),
				Response:    jsonStr,
			}
		}
	}

	// Step 3: Validate each fact
	for i, fact := range facts {
		if err := pe.validateFact(fact); err != nil {
			vf.FailedCount++
			logging.Debug("Fact %d validation failed: %v", i, err)
			continue
		}
		vf.Facts = append(vf.Facts, fact)
	}

	// Check if we got any valid facts
	if len(vf.Facts) == 0 && len(facts) > 0 {
		return nil, &ExtractionError{
			Stage:       "validation",
			OriginalErr: fmt.Errorf("all %d extracted facts failed validation", len(facts)),
			FactCount:   len(facts),
		}
	}

	return vf, nil
}

// extractJSON extracts valid JSON from various response formats
// Handles: raw JSON, markdown code blocks, text with embedded JSON
func (pe *ProfileExtractor) extractJSON(resp string) (string, string, error) {
	resp = strings.TrimSpace(resp)

	// Try 1: Direct JSON (already clean)
	if (strings.HasPrefix(resp, "[") && strings.HasSuffix(resp, "]")) ||
		(strings.HasPrefix(resp, "{") && strings.HasSuffix(resp, "}")) {
		if json.Valid([]byte(resp)) {
			return resp, "clean_json", nil
		}
	}

	// Try 2: JSON wrapped in markdown code block (```json ... ```)
	if extracted, ok := pe.extractFromMarkdownBlock(resp); ok {
		return extracted, "markdown_code_block", nil
	}

	// Try 3: JSON wrapped in other delimiters
	if extracted, ok := pe.extractBetweenDelimiters(resp, "[", "]"); ok {
		if json.Valid([]byte("[" + extracted + "]")) {
			return "[" + extracted + "]", "cleanup_required", nil
		}
	}

	// Try 4: Clean up common JSON issues (trailing commas, unquoted keys)
	if cleaned, ok := pe.cleanupJSON(resp); ok {
		return cleaned, "cleanup_required", nil
	}

	return "", "failed", &ExtractionError{
		Stage:       "json_extraction",
		OriginalErr: fmt.Errorf("could not extract valid JSON from response"),
		Response:    truncateForLogging(resp, 200),
	}
}

// extractFromMarkdownBlock extracts JSON from markdown code blocks
func (pe *ProfileExtractor) extractFromMarkdownBlock(resp string) (string, bool) {
	// Match ```json ... ``` or ``` ... ```
	patterns := []string{
		`(?s)\x60{3}json\s*(.*?)\x60{3}`,
		`(?s)\x60{3}\s*(.*?)\x60{3}`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(resp)
		if len(matches) > 1 {
			extracted := strings.TrimSpace(matches[1])
			if json.Valid([]byte(extracted)) {
				return extracted, true
			}
		}
	}

	return "", false
}

// extractBetweenDelimiters extracts content between opening and closing delimiters
func (pe *ProfileExtractor) extractBetweenDelimiters(resp, open, close string) (string, bool) {
	start := strings.Index(resp, open)
	end := strings.LastIndex(resp, close)

	if start == -1 || end == -1 || start >= end {
		return "", false
	}

	// Extract content between delimiters (excluding the delimiters themselves)
	content := resp[start+len(open) : end]
	return strings.TrimSpace(content), true
}

// cleanupJSON attempts to fix common JSON formatting issues
func (pe *ProfileExtractor) cleanupJSON(resp string) (string, bool) {
	// Remove common issues
	cleaned := strings.TrimSpace(resp)

	// Try to find array-like content
	if !strings.HasPrefix(cleaned, "[") {
		cleaned = "[" + cleaned
	}
	if !strings.HasSuffix(cleaned, "]") {
		cleaned = cleaned + "]"
	}

	if json.Valid([]byte(cleaned)) {
		return cleaned, true
	}

	return "", false
}

// tryPartialExtraction attempts to extract individual valid JSON objects from malformed array
func (pe *ProfileExtractor) tryPartialExtraction(jsonStr string) ([]ExtractedFact, bool) {
	var facts []ExtractedFact

	// Try to find individual JSON objects
	re := regexp.MustCompile(`\{[^{}]*\}`)
	matches := re.FindAllString(jsonStr, -1)

	if len(matches) == 0 {
		return nil, false
	}

	for _, match := range matches {
		var fact ExtractedFact
		if err := json.Unmarshal([]byte(match), &fact); err == nil {
			facts = append(facts, fact)
		}
	}

	return facts, len(facts) > 0
}

// validateFact checks that a fact meets all schema requirements
func (pe *ProfileExtractor) validateFact(fact ExtractedFact) error {
	// Check required fields
	if fact.Key == "" {
		return fmt.Errorf("missing required field: key")
	}
	if fact.Value == "" {
		return fmt.Errorf("missing required field: value")
	}
	if fact.Source == "" {
		return fmt.Errorf("missing required field: source")
	}

	// Validate category
	validCategories := map[string]bool{
		"identity":     true,
		"professional": true,
		"preference":   true,
		"project":      true,
		"personal":     true,
	}
	if fact.Category != "" && !validCategories[fact.Category] {
		return fmt.Errorf("invalid category: %s", fact.Category)
	}

	// Validate source
	validSources := map[string]bool{
		"explicit": true,
		"inferred": true,
	}
	if !validSources[fact.Source] {
		return fmt.Errorf("invalid source: %s (must be 'explicit' or 'inferred')", fact.Source)
	}

	// Validate confidence (should be 0.0-1.0)
	if fact.Confidence < 0.0 || fact.Confidence > 1.0 {
		return fmt.Errorf("confidence out of range: %.2f (must be 0.0-1.0)", fact.Confidence)
	}

	// Validate key format (alphanumeric, underscore, colon)
	if !regexp.MustCompile(`^[a-zA-Z0-9_:]+$`).MatchString(fact.Key) {
		return fmt.Errorf("invalid key format: %s (must be alphanumeric, underscore, or colon)", fact.Key)
	}

	return nil
}

// truncateForLogging truncates long strings for logging
func truncateForLogging(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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
