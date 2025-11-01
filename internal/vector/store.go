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

	// GetDocuments retrieves all documents for the currently open chat
	GetDocuments(ctx context.Context) ([]Document, error)

	// Profile management
	StoreUserProfile(ctx context.Context, profile *UserProfile) error
	GetUserProfile(ctx context.Context, chatID string) (*UserProfile, error)

	// Individual fact operations
	UpsertProfileFact(ctx context.Context, chatID string, fact ProfileFact) error
	GetProfileFact(ctx context.Context, chatID string, key string) (*ProfileFact, error)
	DeleteProfileFact(ctx context.Context, chatID string, key string) error

	// History and conflict resolution
	GetFactHistory(ctx context.Context, chatID string, key string) ([]ProfileFact, error)

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
	CreatedAt    time.Time

	// RAG parameters
	Temperature   float64
	TopK          int
	UseReranking  bool // When true, uses LLM-based reranking
	MaxTokens     int
	ContextWindow int // Total context window size (input + output tokens)
	FileCount     int // Number of files embedded in this chat
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

// FactCategory defines hierarchical fact organization
type FactCategory string

const (
	FactCategoryIdentity     FactCategory = "identity"     // name, age, location
	FactCategoryProfessional FactCategory = "professional" // role, company, experience
	FactCategoryPreference   FactCategory = "preference"   // language, tools, style
	FactCategoryProject      FactCategory = "project"      // current work, goals
	FactCategoryTask         FactCategory = "task"         // what interests user in current task: functions, variables, processes, clients
	FactCategoryPersonal     FactCategory = "personal"     // hobbies, interests (careful!)
)

// ProfileFact represents a single piece of information about the user
type ProfileFact struct {
	Key        string    `json:"key"`        // e.g., "name", "role", "company", "preference:language"
	Value      string    `json:"value"`      // e.g., "John", "Solution Architect", "Java"
	Confidence float64   `json:"confidence"` // 0.0-1.0, how certain we are
	Source     string    `json:"source"`     // "explicit" (user stated) or "inferred" (LLM extracted)
	FirstSeen  time.Time `json:"first_seen"` // When first extracted
	LastSeen   time.Time `json:"last_seen"`  // Most recent confirmation
	Context    string    `json:"context"`    // Original conversation snippet
}

// UserProfile represents persistent facts about the user in a specific chat
type UserProfile struct {
	ChatID    string                 `json:"chat_id"`
	Facts     map[string]ProfileFact `json:"facts"`
	UpdatedAt time.Time              `json:"updated_at"`
}
