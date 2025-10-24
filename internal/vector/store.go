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

// Document represents a file that has been loaded into the chat context
type Document struct {
	ID          string            `json:"id"`
	ChatID      string            `json:"chat_id"`
	FilePath    string            `json:"file_path"`
	FileName    string            `json:"file_name"`
	FileSize    int64             `json:"file_size"`
	ContentHash string            `json:"content_hash"` // SHA-256 hash for deduplication
	MimeType    string            `json:"mime_type"`
	Encoding    string            `json:"encoding"`
	ChunkCount  int               `json:"chunk_count"`
	Metadata    map[string]string `json:"metadata"`
	UploadedAt  time.Time         `json:"uploaded_at"`
}

// DocumentChunk represents a chunk of a document that has been embedded
type DocumentChunk struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"document_id"`
	ChatID     string    `json:"chat_id"`
	ChunkIndex int       `json:"chunk_index"`
	Content    string    `json:"content"`
	Embedding  []float32 `json:"embedding"`
	StartPos   int       `json:"start_pos"`
	EndPos     int       `json:"end_pos"`
	FilePath   string    `json:"file_path"` // Denormalized for easy retrieval
}
