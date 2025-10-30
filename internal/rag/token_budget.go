package rag

import (
	"rag-terminal/internal/config"
)

const (
	// CharsPerToken is a rough estimation of characters per token
	// This is a conservative estimate; actual values vary by tokenizer
	CharsPerToken = 4
)

// TokenBudget represents allocated token budgets for different content types
type TokenBudget struct {
	ContextWindow      int // Total context window (input + output)
	MaxTokens          int // Reserved for output
	AvailableInput     int // Available for input (ContextWindow - MaxTokens)
	ExcerptsBudget     int // Tokens allocated for document excerpts
	HistoryBudget      int // Tokens allocated for conversation history
	ChunksBudget       int // Tokens allocated for full chunks
	FileListBudget     int // Small fixed budget for file list
}

// CalculateTokenBudget calculates token budgets based on context window and config
// Uses the default (text) token budget configuration
func CalculateTokenBudget(contextWindow, maxTokens int, cfg *config.Config) *TokenBudget {
	return CalculateTokenBudgetForType(contextWindow, maxTokens, cfg, false)
}

// CalculateTokenBudgetForCode calculates token budgets optimized for code files
func CalculateTokenBudgetForCode(contextWindow, maxTokens int, cfg *config.Config) *TokenBudget {
	return CalculateTokenBudgetForType(contextWindow, maxTokens, cfg, true)
}

// CalculateTokenBudgetForType calculates token budgets based on file type
func CalculateTokenBudgetForType(contextWindow, maxTokens int, cfg *config.Config, isCodeFile bool) *TokenBudget {
	// Select appropriate budget config based on file type
	var budgetConfig config.TokenBudgetConfig
	if isCodeFile {
		budgetConfig = cfg.CodeTokenBudget
	} else {
		budgetConfig = cfg.TokenBudget
	}

	// Use inputRatio to determine available input tokens
	inputRatio := budgetConfig.InputRatio
	if inputRatio <= 0.0 || inputRatio > 1.0 {
		inputRatio = 0.5 // Fallback to default
	}

	availableInput := int(float64(contextWindow) * inputRatio)

	// Calculate implicit output reserve (not explicitly used but available)
	maxTokens = contextWindow - availableInput

	// Allocate percentages based on config
	excerptsBudget := int(float64(availableInput) * budgetConfig.Excerpts)
	historyBudget := int(float64(availableInput) * budgetConfig.History)

	// File list gets a small fixed budget (100 tokens ~= 400 chars)
	fileListBudget := 100

	// Chunks get the remainder
	chunksBudget := availableInput - excerptsBudget - historyBudget - fileListBudget

	// Ensure chunks budget is non-negative
	if chunksBudget < 0 {
		chunksBudget = 0
	}

	return &TokenBudget{
		ContextWindow:  contextWindow,
		MaxTokens:      maxTokens,
		AvailableInput: availableInput,
		ExcerptsBudget: excerptsBudget,
		HistoryBudget:  historyBudget,
		ChunksBudget:   chunksBudget,
		FileListBudget: fileListBudget,
	}
}

// EstimateTokens estimates the number of tokens in a text string
func EstimateTokens(text string) int {
	return len(text) / CharsPerToken
}

// TruncateToTokenLimit truncates text to fit within a token limit
func TruncateToTokenLimit(text string, tokenLimit int) string {
	maxChars := tokenLimit * CharsPerToken

	if len(text) <= maxChars {
		return text
	}

	// Truncate and add ellipsis
	if maxChars > 3 {
		return text[:maxChars-3] + "..."
	}

	return text[:maxChars]
}
