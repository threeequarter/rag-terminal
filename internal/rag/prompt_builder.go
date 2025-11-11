package rag

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"rag-terminal/internal/config"
	"rag-terminal/internal/document"
	"rag-terminal/internal/logging"
	"rag-terminal/internal/vector"
)

// PromptBuilder handles prompt construction for various contexts
type PromptBuilder struct {
	vectorStore vector.VectorStore
	config      *config.Config
}

// NewPromptBuilder creates a new prompt builder
func NewPromptBuilder(vectorStore vector.VectorStore, cfg *config.Config) *PromptBuilder {
	return &PromptBuilder{
		vectorStore: vectorStore,
		config:      cfg,
	}
}

// buildProfileContext formats user facts from the profile for inclusion in prompts
func (pb *PromptBuilder) buildProfileContext(profile *vector.UserProfile) string {
	if len(profile.Facts) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("---\nKnown information about the user:\n")

	// Group facts by category (using key prefix before colon)
	categories := map[string][]vector.ProfileFact{
		"identity":     {},
		"professional": {},
		"preference":   {},
		"project":      {},
		"personal":     {},
	}

	for _, fact := range profile.Facts {
		// Only include high-confidence facts (>0.6)
		if fact.Confidence < 0.6 {
			continue
		}

		// Extract category from key (before colon if present)
		category := "personal" // default
		keyParts := strings.Split(fact.Key, ":")
		if len(keyParts) > 0 && keyParts[0] != "" {
			potentialCategory := keyParts[0]
			if _, exists := categories[potentialCategory]; exists {
				category = potentialCategory
			}
		}

		categories[category] = append(categories[category], fact)
	}

	// Format by category in a consistent order
	categoryOrder := []string{"identity", "professional", "preference", "project", "personal"}
	for _, category := range categoryOrder {
		facts := categories[category]
		if len(facts) == 0 {
			continue
		}

		// Capitalize category name for display
		displayCategory := strings.ToUpper(string([]rune(category)[0])) + category[1:]
		sb.WriteString(fmt.Sprintf("\n%s:\n", displayCategory))

		for _, fact := range facts {
			// Remove category prefix from key for display
			displayKey := strings.TrimPrefix(fact.Key, category+":")
			if displayKey == "" {
				displayKey = category
			}

			sb.WriteString(fmt.Sprintf("- %s: %s\n", displayKey, fact.Value))
		}
	}

	return sb.String()
}

// BuildPromptWithContext builds a simple prompt with user profile and conversation context
func (pb *PromptBuilder) BuildPromptWithContext(ctx context.Context, chatID string, systemPrompt string, contextMessages []vector.Message, userMessage string) string {
	var builder strings.Builder

	// Add user profile context if available
	profile, err := pb.vectorStore.GetUserProfile(ctx, chatID)
	if err != nil {
		logging.Debug("Failed to retrieve user profile: %v", err)
	} else if profile != nil && len(profile.Facts) > 0 {
		profileContext := pb.buildProfileContext(profile)
		if profileContext != "" {
			builder.WriteString("# User Profile\n")
			builder.WriteString(profileContext)
			builder.WriteString("\n\n")
		}
	}

	// Filter out messages with identical content to current message (to avoid treating first message as context)
	var relevantContext []vector.Message
	for _, msg := range contextMessages {
		if msg.Content != userMessage {
			relevantContext = append(relevantContext, msg)
		}
	}

	if len(relevantContext) > 0 {
		builder.WriteString("---\nRelevant previous conversation history for reference:\n")
		for _, msg := range relevantContext {
			builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content))
		}
		builder.WriteString("Use the above context to help answer the user's question if relevant.\n\n")
		builder.WriteString("---\n\n")
	}

	builder.WriteString("User's question or message to you: ")
	builder.WriteString(userMessage)

	return builder.String()
}

// BuildPromptWithDocuments builds a prompt with document context (no file list)
func (pb *PromptBuilder) BuildPromptWithDocuments(systemPrompt string, contextMessages []vector.Message, contextChunks []vector.DocumentChunk, userMessage string) string {
	var builder strings.Builder

	// Add document chunks first (most specific context)
	if len(contextChunks) > 0 {
		builder.WriteString("---\nRelevant document excerpts:\n")
		for i, chunk := range contextChunks {
			builder.WriteString(fmt.Sprintf("[Document %d: %s]\n%s\n\n", i+1, chunk.FilePath, chunk.Content))
		}
		builder.WriteString("---\n\n")
	}

	// Filter out messages with identical content to current message (to avoid treating first message as context)
	var relevantContext []vector.Message
	for _, msg := range contextMessages {
		if msg.Content != userMessage {
			relevantContext = append(relevantContext, msg)
		}
	}

	// Add conversation context
	if len(relevantContext) > 0 {
		builder.WriteString("---\nPrevious conversation history:\n\n")
		for _, msg := range relevantContext {
			builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content))
		}
		builder.WriteString("---\n\n")
	}

	if len(contextChunks) > 0 || len(relevantContext) > 0 {
		builder.WriteString("Use the above information to help answer the user's question.\n\n")
		builder.WriteString("---\n\n")
	}

	builder.WriteString("User's question or message to you: ")
	builder.WriteString(userMessage)

	return builder.String()
}

// BuildPromptWithContextAndDocumentsAndFileList builds a comprehensive prompt with file list, excerpts, and history
func (pb *PromptBuilder) BuildPromptWithContextAndDocumentsAndFileList(ctx context.Context, chat *vector.Chat, contextMessages []vector.Message, contextChunks []vector.DocumentChunk, allDocs []vector.Document, userMessage string) string {
	var builder strings.Builder

	// Calculate token budgets
	contextWindow := chat.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 4096 // Fallback to default
	}

	maxTokens := chat.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048 // Fallback to default
	}

	// Detect if we're working with code files
	isCodeFile := false
	if len(contextChunks) > 0 {
		// Check first chunk to determine file type
		isCodeFile = document.IsCodeFile(contextChunks[0].FilePath)
	} else if len(allDocs) > 0 {
		// Check first document
		isCodeFile = document.IsCodeFile(allDocs[0].FilePath)
	}

	// Use appropriate budget configuration
	budget := CalculateTokenBudgetForType(contextWindow, maxTokens, pb.config, isCodeFile)

	// Add user profile context if available
	profile, err := pb.vectorStore.GetUserProfile(ctx, chat.ID)
	if err != nil {
		logging.Debug("Failed to retrieve user profile: %v", err)
	} else if profile != nil && len(profile.Facts) > 0 {
		profileContext := pb.buildProfileContext(profile)
		if profileContext != "" {
			builder.WriteString("# User Profile\n")
			builder.WriteString(profileContext)
			builder.WriteString("\n\n")
		}
	}

	if isCodeFile {
		logging.Info("Using code-optimized token budget (input: %d, excerpts: %d, history: %d, chunks: %d)",
			budget.AvailableInput, budget.ExcerptsBudget, budget.HistoryBudget, budget.ChunksBudget)
	} else {
		logging.Debug("Using default token budget (input: %d, excerpts: %d, history: %d, chunks: %d)",
			budget.AvailableInput, budget.ExcerptsBudget, budget.HistoryBudget, budget.ChunksBudget)
	}

	// HIERARCHICAL CONTEXT STRUCTURE
	// Layer 1: Document overview (uses FileListBudget)
	if len(allDocs) > 0 {
		builder.WriteString("# Available Documents\n")
		fileListChars := budget.FileListBudget * CharsPerToken

		var fileListBuilder strings.Builder
		for i, doc := range allDocs {
			line := fmt.Sprintf("%d. %s (%d chunks)\n", i+1, doc.FileName, doc.ChunkCount)
			if fileListBuilder.Len()+len(line) > fileListChars {
				break // Stop if we exceed budget
			}
			fileListBuilder.WriteString(line)
		}
		builder.WriteString(fileListBuilder.String())
		builder.WriteString("\n")
	}

	// Layer 2: Relevant excerpts (uses ExcerptsBudget)
	if len(contextChunks) > 0 {
		builder.WriteString("# Relevant Information\n\n")

		excerptCharsRemaining := budget.ExcerptsBudget * CharsPerToken
		extractor := document.NewExtractor()

		for _, chunk := range contextChunks {
			if excerptCharsRemaining <= 0 {
				break // Budget exhausted
			}

			// Calculate max excerpt size for this chunk
			maxExcerptSize := 500
			if excerptCharsRemaining < maxExcerptSize {
				maxExcerptSize = excerptCharsRemaining
			}

			if maxExcerptSize < 50 {
				break // Not enough space for meaningful excerpt
			}

			excerpt := extractor.ExtractRelevantExcerptWithPath(chunk.Content, userMessage, maxExcerptSize, chunk.FilePath)
			fileName := filepath.Base(chunk.FilePath)

			chunkText := fmt.Sprintf("[%s]\n%s\n\n", fileName, excerpt)
			builder.WriteString(chunkText)

			excerptCharsRemaining -= len(chunkText)
		}
		builder.WriteString("---\n\n")
	}

	// Layer 3: Conversation history (uses HistoryBudget)
	// Filter out messages with identical content to current message (to avoid treating first message as context)
	var relevantContext []vector.Message
	for _, msg := range contextMessages {
		if msg.Content != userMessage {
			relevantContext = append(relevantContext, msg)
		}
	}

	if len(relevantContext) > 0 {
		builder.WriteString("# Previous Conversation History\n")

		historyCharsRemaining := budget.HistoryBudget * CharsPerToken

		for _, msg := range relevantContext {
			if historyCharsRemaining <= 0 {
				break // Budget exhausted
			}

			content := msg.Content
			maxContentSize := historyCharsRemaining - 20 // Reserve space for role label and formatting

			if maxContentSize < 20 {
				break // Not enough space
			}

			if len(content) > maxContentSize {
				content = content[:maxContentSize-3] + "..."
			}

			msgText := fmt.Sprintf("[%s]: %s\n", msg.Role, content)
			builder.WriteString(msgText)
			historyCharsRemaining -= len(msgText)
		}
		builder.WriteString("\n---\n\n")
	}

	// Instruction to use context
	if len(allDocs) > 0 || len(contextChunks) > 0 || len(relevantContext) > 0 {
		builder.WriteString("Use the above information to help answer the user's question.\n\n")
		builder.WriteString("---\n\n")
	}

	// Current query (full detail - not budget-limited as it's essential)
	builder.WriteString("# User's question or message to you: ")
	builder.WriteString(userMessage)

	return builder.String()
}
