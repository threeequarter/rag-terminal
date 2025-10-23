package vector

import (
	"context"
	"time"
)

type VectorStore interface {
	// OpenChat opens the database for a specific chat
	// Must be called before any chat-specific operations
	OpenChat(ctx context.Context, chatID string) error

	// CloseChat gracefully closes the current chat's database
	CloseChat(ctx context.Context) error

	// StoreMessage stores a message with its embedding in the currently open chat
	StoreMessage(ctx context.Context, messageID, role, content string, embedding []float32, timestamp time.Time) error

	// SearchSimilar searches for similar messages by vector similarity in the currently open chat
	SearchSimilar(ctx context.Context, queryEmbedding []float32, topK int) ([]Message, error)

	// GetMessages retrieves all messages for the currently open chat in chronological order
	GetMessages(ctx context.Context) ([]Message, error)

	// StoreChat stores chat metadata (creates new chat database)
	StoreChat(ctx context.Context, chat *Chat) error

	// GetChat retrieves chat metadata from filesystem
	GetChat(ctx context.Context, chatID string) (*Chat, error)

	// ListChats retrieves all chat metadata by scanning filesystem
	ListChats(ctx context.Context) ([]Chat, error)

	// DeleteChat deletes a chat directory and all its data
	DeleteChat(ctx context.Context, chatID string) error

	// Close closes any open database connection and cleans up resources
	Close() error
}

type Message struct {
	ID        string
	ChatID    string
	Role      string
	Content   string
	Embedding []float32
	Timestamp time.Time
}

type Chat struct {
	ID           string
	Name         string
	SystemPrompt string
	LLMModel     string
	EmbedModel   string
	CreatedAt    time.Time

	// RAG parameters
	Temperature  float64
	TopK         int
	UseReranking bool // When true, uses LLM-based reranking
	MaxTokens    int
}
