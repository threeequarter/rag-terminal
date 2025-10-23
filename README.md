# RAG Terminal

A Go-based RAG (Retrieval-Augmented Generation) chat application with a Bubbletea TUI interface, utilizing local Nexa SDK models and BadgerDB for vector storage.

## Features

- **Interactive TUI**: Built with Bubbletea for a clean terminal interface
- **RAG Pipeline**: Retrieval-Augmented Generation with vector similarity search
- **LLM-based Reranking**: Uses LLM to score and rerank retrieved context for better relevance
- **Database-per-Chat**: Isolated vector storage for each chat conversation
- **Streaming Responses**: Real-time streaming of AI responses
- **Model Selection**: Choose from available LLM and embedding models
- **Chat Management**: Create, list, and delete chat conversations
- **Persistent Storage**: All chats and messages stored locally in BadgerDB

## Architecture

### Components

- **Nexa API Client** (`internal/nexa/`): HTTP client for Nexa SDK API
  - Embeddings generation
  - Chat completions (streaming and synchronous)

- **Vector Storage** (`internal/vector/`): BadgerDB-based vector database
  - Separate database per chat (database-per-chat pattern)
  - Message storage with embeddings
  - Cosine similarity search
  - Chat metadata management

- **RAG Pipeline** (`internal/rag/`): Orchestrates the RAG flow
  - Embedding generation for queries
  - Vector similarity search (top-K × 2 when reranking enabled)
  - LLM-based reranking (scores 0-10 per message)
  - Context injection into prompts with numbered conversations
  - Asynchronous embedding updates to prevent blocking

- **UI Components** (`internal/ui/`): Bubbletea TUI screens
  - Model selection (LLM and embedding)
  - Chat list management
  - Chat creation with RAG parameters
  - Chat conversation view

## Prerequisites

1. **Go 1.21+**: Install from [golang.org](https://golang.org)

2. **Nexa SDK**: Install and set up Nexa
   ```bash
   pip install nexaai
   ```

3. **Nexa Server**: Ensure Nexa server is running
   ```bash
   # The application expects Nexa API at http://127.0.0.1:18181
   nexa server start
   ```

4. **Download Models**: Pull required models
   ```bash
   # Example: Pull an LLM model
   nexa pull gemma-2-2b-instruct

   # Example: Pull an embedding model
   nexa pull nomic-embed-text-v1.5
   ```

## Installation

1. Clone or navigate to the repository:
   ```bash
   cd .\rag-terminal
   ```

2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Build the application:
   ```bash
   go build -o rag-terminal.exe .
   ```

## Usage

1. **Start the application**:
   ```bash
   .\rag-terminal.exe
   ```

2. **Select Models**:
   - Select an LLM model (used for both generation and reranking)
   - Select an embedding model (for vector search)

3. **Create or Open Chat**:
   - **Chat Name**: Name your conversation
   - **System Prompt**: Define AI behavior
   - **Temperature**: Response randomness (0-1, default: 0.7)
   - **Top K**: Context messages to retrieve (default: 5)
   - **Use LLM Reranking**: Enabled by default - LLM scores retrieved messages for relevance

4. **Chat Workflow**:
   - Send message → generates embedding → searches similar messages
   - If reranking enabled: retrieves top-K × 2, LLM scores each, takes top-K
   - If reranking disabled: uses cosine similarity ranking
   - Context injected as numbered conversations
   - LLM generates response with full context
   - Both user and assistant messages stored with embeddings

## RAG Flow

1. **User Message** → Generate embedding with embedding model
2. **Vector Search** → Cosine similarity search in current chat's database (retrieves top-K × 2 if reranking enabled)
3. **LLM Reranking** (optional, enabled by default):
   - LLM scores each message 0-10 for relevance to user query
   - Sorts by score, selects top-K most relevant
   - Falls back to cosine similarity if reranking fails
4. **Context Injection** → Build prompt with numbered conversations: "Context from previous conversations (all relevant):"
5. **LLM Generation** → Stream response using context
6. **Storage** → Store user message immediately, assistant message stored with empty embedding first, then updated asynchronously (500ms delay to avoid model switching conflicts)

## Configuration

### Default Settings

- **Database Path**: `~/.rag-terminal/db/<chat-id>/` (separate database per chat)
- **Nexa API URL**: `http://127.0.0.1:18181`
- **Temperature**: 0.7
- **Top K**: 5
- **Max Tokens**: 2048
- **LLM Reranking**: Enabled by default

### RAG Parameters

- **Temperature**: Response randomness (0.0 = deterministic, 1.0 = creative)
- **Top K**: Number of context messages to retrieve
- **Use LLM Reranking**: When enabled, LLM scores each retrieved message for relevance (temperature 0.1 for consistency)
- **System Prompt**: AI personality and behavior

## Project Structure

```
rag-terminal/
├── main.go                     # Application entry point & state management
├── internal/
│   ├── nexa/                   # Nexa API client
│   │   ├── client.go          # HTTP client
│   │   ├── embeddings.go      # Embeddings API
│   │   └── chat.go            # Chat completions (streaming & sync)
│   ├── vector/                 # Vector storage
│   │   ├── store.go           # Storage interface & Chat struct
│   │   ├── badger.go          # BadgerDB with database-per-chat
│   │   └── similarity.go      # Cosine similarity
│   ├── rag/                    # RAG pipeline
│   │   └── pipeline.go        # RAG orchestration & LLM reranking
│   ├── models/                 # Data models
│   │   ├── chat.go            # Chat metadata
│   │   └── message.go         # Message structure
│   └── ui/                     # TUI components
│       ├── model_select.go    # Model selection (LLM & embedding)
│       ├── chat_list.go       # Chat list management
│       ├── chat_create.go     # Chat creation form
│       └── chat_view.go       # Chat conversation view
└── go.mod
```

## Keyboard Shortcuts

### Global
- **Ctrl+X**: Exit application
- **Ctrl+C**: Force quit

### Model Selection
- **↑/↓**: Navigate models
- **Enter**: Select model
- **Esc**: Go back

### Chat List
- **↑/↓**: Navigate chats
- **Enter**: Open chat
- **N**: Create new chat
- **D**: Delete selected chat

### Chat Creation
- **Tab/Shift+Tab**: Navigate fields
- **Enter**: Create chat
- **Space**: Toggle "Use LLM Reranking"
- **Esc**: Cancel

### Chat Conversation
- **Enter**: Send message
- **Esc**: Return to chat list

## Troubleshooting

### "Failed to get models"
- Ensure Nexa SDK is installed: `pip install nexaai`
- Check that models are downloaded: `nexa list`
- Verify Nexa server is running: Check http://127.0.0.1:18181

### "Failed to open badger database"
- Check that `~/.rag-terminal/db/<chat-id>/` directory is accessible
- Ensure no other instance is accessing the same chat
- Close chat properly before opening another to avoid database locks

### "Embeddings API returned status 500"
- Ensure the embedding model is loaded in Nexa
- Check Nexa server logs for errors
- Try pulling the model again: `nexa pull <model-name>`

### Streaming response not working
- Verify the LLM model is properly loaded
- Check network connectivity to Nexa server
- Ensure sufficient system resources

## Development

### Building from Source

```bash
# Navigate to repository
cd .\rag-terminal

# Install dependencies
go mod download

# Build
go build -o rag-terminal.exe .

# Run
.\rag-terminal.exe
```

### Testing

```bash
# Run tests
go test ./...

# Run with race detector
go test -race ./...
```

## API Endpoints Used

The application uses the following Nexa SDK endpoints:

1. **POST /v1/embeddings**: Generate text embeddings for messages
2. **POST /v1/chat/completions**: Generate chat responses (streaming for user interaction, synchronous for LLM reranking)

## Credits

Built with:
- [Nexa SDK](https://github.com/NexaAI/nexa-sdk) - Local AI model inference
- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [BadgerDB](https://github.com/dgraph-io/badger) - Embedded database
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Style definitions
