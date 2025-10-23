package vector

import (
	"context"
	"time"
)

type VectorStore interface {
	// StoreMessage stores a message with its embedding
	StoreMessage(ctx context.Context, chatID, messageID, role, content string, embedding []float32, timestamp time.Time) error

	// SearchSimilar searches for similar messages by vector similarity
	SearchSimilar(ctx context.Context, chatID string, queryEmbedding []float32, topK int) ([]Message, error)

	// GetMessages retrieves all messages for a chat in chronological order
	GetMessages(ctx context.Context, chatID string) ([]Message, error)

	// StoreChat stores chat metadata
	StoreChat(ctx context.Context, chat *Chat) error

	// GetChat retrieves chat metadata
	GetChat(ctx context.Context, chatID string) (*Chat, error)

	// ListChats retrieves all chat metadata
	ListChats(ctx context.Context) ([]Chat, error)

	// DeleteChat deletes a chat and all its messages
	DeleteChat(ctx context.Context, chatID string) error

	// Close closes the database connection
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
	RerankModel  string
	CreatedAt    time.Time

	// RAG parameters
	Temperature  float64
	TopK         int
	UseReranking bool
	MaxTokens    int
}
