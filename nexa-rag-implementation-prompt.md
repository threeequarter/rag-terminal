# Nexa RAG Chat Application - Implementation Guide

## Project Overview

Build a Go-based RAG chat application with Bubbletea UI that uses local Nexa SDK models and BadgerDB for vector storage.

---

## Step 1: Project Setup & Dependencies

Create Go module and install dependencies:

```bash
go mod init nexa-rag-chat
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/bubbles/list
go get github.com/charmbracelet/bubbles/textinput
go get github.com/charmbracelet/bubbles/viewport
go get github.com/dgraph-io/badger/v4
```

Create project structure:

```
nexa-rag-chat/
├── main.go
├── internal/
│   ├── nexa/
│   │   ├── client.go          # HTTP client for Nexa API
│   │   ├── embeddings.go      # /v1/embeddings wrapper
│   │   ├── chat.go            # /v1/chat/completions wrapper
│   │   └── reranking.go       # /v1/reranking wrapper
│   ├── vector/
│   │   ├── store.go           # Vector storage interface
│   │   ├── badger.go          # BadgerDB implementation
│   │   └── similarity.go      # Cosine similarity calculation
│   ├── rag/
│   │   └── pipeline.go        # RAG orchestration
│   ├── models/
│   │   ├── chat.go            # Chat data structures
│   │   └── message.go         # Message data structures
│   └── ui/
│       ├── model_select.go    # Model selection screen
│       ├── chat_list.go       # Chat list screen
│       ├── chat_create.go     # Chat creation screen
│       └── chat_view.go       # Chat conversation screen
└── go.mod
```

---

## Step 2: Nexa API Client (`internal/nexa/`)

### File: `internal/nexa/client.go`

```go
type Client struct {
    baseURL    string // Default: "http://127.0.0.1:18181"
    httpClient *http.Client
}

// NewClient creates Nexa API client
// GetModels() returns list from `nexa list` command output parsing
```

### File: `internal/nexa/embeddings.go`

```go
type EmbeddingRequest struct {
    Model string   `json:"model"`
    Input []string `json:"input"` // Batch support
}

type EmbeddingResponse struct {
    Data []struct {
        Embedding []float32 `json:"embedding"`
        Index     int       `json:"index"`
    } `json:"data"`
}

// GenerateEmbeddings(ctx, model, texts) -> [][]float32
```

### File: `internal/nexa/chat.go`

```go
type ChatCompletionRequest struct {
    Model       string        `json:"model"`
    Messages    []ChatMessage `json:"messages"`
    Stream      bool          `json:"stream"`
    Temperature float64       `json:"temperature"`
    // Other params from API spec
}

type ChatMessage struct {
    Role    string `json:"role"`    // "system", "user", "assistant"
    Content string `json:"content"`
}

// ChatCompletion(ctx, req) -> response stream channel
```

### File: `internal/nexa/reranking.go`

```go
type RerankingRequest struct {
    Model           string   `json:"model"`
    Query           string   `json:"query"`
    Documents       []string `json:"documents"`
    BatchSize       int      `json:"batch_size"`
    Normalize       bool     `json:"normalize"`
    NormalizeMethod string   `json:"normalize_method"` // "softmax"
}

type RerankingResponse struct {
    Result []float64 `json:"result"` // Scores for each document
}

// Rerank(ctx, req) -> []float64
```

---

## Step 3: Vector Storage (`internal/vector/`)

### File: `internal/vector/store.go`

```go
type VectorStore interface {
    // Store message with embedding
    StoreMessage(ctx, chatID, messageID, role, content string, embedding []float32) error
    
    // Search similar messages by vector
    SearchSimilar(ctx, chatID string, queryEmbedding []float32, topK int) ([]Message, error)
    
    // Get all messages for a chat
    GetMessages(ctx, chatID string) ([]Message, error)
    
    // Close database
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
```

### File: `internal/vector/badger.go`

```go
type BadgerStore struct {
    db *badger.DB
}

// NewBadgerStore(dbPath) creates store
// Use prefix keys: "chat:{chatID}:msg:{messageID}"
// Store as msgpack or gob serialization
// Implement VectorStore interface
```

### File: `internal/vector/similarity.go`

```go
// CosineSimilarity(vec1, vec2 []float32) float32
// Returns similarity score [-1, 1]
```

---

## Step 4: RAG Pipeline (`internal/rag/pipeline.go`)

```go
type Pipeline struct {
    nexaClient  *nexa.Client
    vectorStore vector.VectorStore
    llmModel    string
    embedModel  string
}

// ProcessUserMessage orchestrates RAG flow:
// 1. Embed user message
// 2. Search similar messages (top 10-20)
// 3. (Optional) Rerank to get top 3-5
// 4. Build prompt with context
// 5. Call chat completion
// 6. Store user message + assistant response with embeddings
// Returns: response stream channel

func (p *Pipeline) ProcessUserMessage(
    ctx context.Context,
    chatID string,
    userMessage string,
    systemPrompt string,
    params ChatParams,
) (<-chan string, error)
```

### Prompt template for RAG context injection

```
System: {systemPrompt}

Previous relevant conversations:
{retrieved_message_1}
{retrieved_message_2}
...

Current message: {userMessage}
```

---

## Step 5: Data Models (`internal/models/`)

### File: `internal/models/chat.go`

```go
type Chat struct {
    ID           string
    Name         string
    SystemPrompt string
    LLMModel     string
    EmbedModel   string
    CreatedAt    time.Time
    
    // RAG parameters
    Temperature float64
    TopK        int  // For retrieval
    UseReranking bool
    // Other params from API
}

// Store chats metadata in BadgerDB with key: "metadata:chat:{chatID}"
```

### File: `internal/models/message.go`

```go
type Message struct {
    ID        string
    ChatID    string
    Role      string // "user" or "assistant"
    Content   string
    Timestamp time.Time
}
```

---

## Step 6: UI Components (`internal/ui/`)

### File: `internal/ui/model_select.go`

Use Bubbletea's `list.Model` to display models from `nexa list` output.

Parse `nexa list` command output to get:
- LLM models (filter by type: text-generation)
- Embedding models (filter by type: embeddings)

User selects one LLM and one embedding model using arrow keys + Enter.

On selection complete: transition to `chatListModel`

---

### File: `internal/ui/chat_list.go`

Display list of chats from BadgerDB metadata.

Keybindings:
- `↑/↓`: Navigate
- `Enter`: Open chat (transition to `chatViewModel`)
- `Ctrl+N`: Create new chat (transition to `chatCreateModel`)
- `Ctrl+D`: Delete selected chat
- `Ctrl+X`: Exit application

---

### File: `internal/ui/chat_create.go`

Form with `textinput` components for:
- Chat name
- System prompt (multiline textarea)
- Temperature (number input, default: 0.7)
- TopK for retrieval (number input, default: 5)
- Enable reranking (checkbox, default: true)

On submit: create Chat object, save to BadgerDB, transition to `chatViewModel`

---

### File: `internal/ui/chat_view.go`

Chat interface with:
- `viewport.Model` for message history display
- `textinput.Model` for user input
- Status bar showing: model names, RAG status

Keybindings:
- `Enter`: Send message (triggers RAG pipeline)
- `Esc`: Back to chat list
- `Ctrl+X`: Exit application

#### Message flow

1. User types message, presses Enter
2. Show "Thinking..." indicator
3. Call `Pipeline.ProcessUserMessage()`
4. Stream response tokens to viewport
5. Auto-scroll to bottom
6. Store both messages with embeddings

#### Rendering

- User messages: right-aligned, blue background
- Assistant messages: left-aligned, gray background
- Timestamp for each message

---

## Step 7: Main Application Flow (`main.go`)

```go
type appState int

const (
    stateModelSelect appState = iota
    stateChatList
    stateChatCreate
    stateChatView
)

type model struct {
    state       appState
    nexaClient  *nexa.Client
    vectorStore vector.VectorStore
    pipeline    *rag.Pipeline
    
    // UI models
    modelSelectModel tea.Model
    chatListModel    tea.Model
    chatCreateModel  tea.Model
    chatViewModel    tea.Model
    
    // Selected models
    llmModel   string
    embedModel string
    
    // Current chat
    currentChat *models.Chat
}

func main() {
    // 1. Initialize BadgerDB in ~/.nexa-rag-chat/
    // 2. Create Nexa client
    // 3. Start with modelSelectModel
    // 4. Run tea.NewProgram(initialModel)
}

// State transitions in Update():
// - modelSelect -> chatList (after model selection)
// - chatList -> chatCreate (Ctrl+N)
// - chatList -> chatView (Enter on chat)
// - chatCreate -> chatView (after creation)
// - chatView -> chatList (Esc)
// - Any state -> exit (Ctrl+X with cleanup)
```

---

## Step 8: Cleanup & Error Handling

### On Ctrl+X or exit

1. Close current chat's vector store properly
2. `vectorStore.Close()` to flush BadgerDB
3. Graceful shutdown

### Error handling

- Network errors to Nexa API: show error message, don't crash
- BadgerDB errors: log and show user-friendly message
- Model not loaded in Nexa: detect and show "Please load model with `nexa pull <model>`"

---

## Step 9: Default RAG Parameters

For chat creation, use these sensible defaults:

```go
Temperature: 0.7
TopK: 5 (retrieve top 5 similar messages)
MaxCompletionTokens: 2048
UseReranking: true
Stream: true
NormalizeMethod: "softmax"
```

---

## Step 10: Testing Flow

1. Run application
2. Select models from `nexa list` output
3. Create new chat with system prompt: "You are a helpful assistant"
4. Send message: "Hello, my name is John"
5. Send message: "What is my name?" (should retrieve first message via RAG)
6. Verify context injection works
7. Press Esc to go back to chat list
8. Test Ctrl+D to delete chat from chat list screen
9. Test Ctrl+X to exit cleanly

---

## Implementation Notes

- **Model list parsing**: Execute `nexa list` via `exec.Command()`, parse output
- **BadgerDB path**: Use `~/.nexa-rag-chat/db/` for storage
- **Chat metadata**: Store in separate BadgerDB key namespace
- **Vector dimension**: Depends on embedding model (typically 384-768 for small models)
- **Error messages**: Use Bubbletea's message system, show at bottom of screen
- **Loading states**: Show spinners during API calls
- **When implementing you can call API at http://127.0.0.1:18181/ at any time, I will keep it running**
---

## RAG Flow Summary

```
User Message → [/v1/embeddings] → Vector Search → Top-K previous messages
                ↓
   (Optional) [/v1/reranking] → Reordered messages
                ↓
   Context + Current Message → [/v1/chat/completions] → Response
                ↓
   Store user message + assistant response with embeddings
```

---

## API Endpoints Used

1. **`/v1/embeddings`** - Convert text to vectors (indexing + query)
2. **`/v1/reranking`** - Rerank retrieved chunks by relevance (optional but recommended)
3. **`/v1/chat/completions`** - Generate response with context (streaming)
