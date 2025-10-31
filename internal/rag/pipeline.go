package rag

import (
	"context"

	"rag-terminal/internal/document"
	"rag-terminal/internal/vector"
)

// Pipeline defines the interface for processing user messages with optional context
type Pipeline interface {
	ProcessUserMessage(ctx context.Context, chat *vector.Chat, llmModel, embedModel string, userMessage string) (<-chan string, <-chan error, error)
	GetDocumentManager() *document.DocumentManager
}

// ChatParams holds chat completion parameters
type ChatParams struct {
	Temperature  float64
	MaxTokens    int
	TopK         int
	UseReranking bool
}
