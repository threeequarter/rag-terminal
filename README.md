# Nexa RAG Chat Application

A Go-based RAG (Retrieval-Augmented Generation) chat application with a Bubbletea TUI interface, utilizing local Nexa SDK models and BadgerDB for vector storage.

## Features

- **Interactive TUI**: Built with Bubbletea for a clean terminal interface
- **RAG Pipeline**: Retrieval-Augmented Generation with vector similarity search
- **Vector Storage**: BadgerDB for efficient message and embedding storage
- **Model Selection**: Choose from available LLM and embedding models
- **Chat Management**: Create, list, and delete chat conversations
- **Streaming Responses**: Real-time streaming of AI responses
- **Optional Reranking**: Improve retrieval quality with reranking models
- **Persistent Storage**: All chats and messages stored locally

## Architecture

### Components

- **Nexa API Client** (`internal/nexa/`): HTTP client for Nexa SDK API
  - Embeddings generation
  - Chat completions with streaming
  - Document reranking

- **Vector Storage** (`internal/vector/`): BadgerDB-based vector database
  - Message storage with embeddings
  - Cosine similarity search
  - Chat metadata management

- **RAG Pipeline** (`internal/rag/`): Orchestrates the RAG flow
  - Embedding generation for queries
  - Vector similarity search
  - Optional reranking
  - Context injection into prompts

- **UI Components** (`internal/ui/`): Bubbletea TUI screens
  - Model selection screen
  - Chat list screen
  - Chat creation form
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
   cd .\rag-ui
   ```

2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Build the application:
   ```bash
   go build -o rag-chat.exe .
   ```

## Usage

1. **Start the application**:
   ```bash
   .\rag-chat.exe
   ```

2. **Select Models**:
   - Use arrow keys to navigate
   - First, select an LLM model (text-generation)
   - Then, select an embedding model (embeddings)
   - Press Enter to confirm each selection

3. **Chat List Screen**:
   - View all your chat conversations
   - **N**: Create a new chat
   - **Enter**: Open selected chat
   - **D**: Delete selected chat
   - **Ctrl+X**: Exit application

4. **Create Chat Screen**:
   - **Chat Name**: Enter a name for your chat
   - **System Prompt**: Define the AI assistant's behavior
   - **Temperature**: Set creativity (0-1, default: 0.7)
   - **Top K**: Number of similar messages to retrieve (default: 5)
   - **Reranking**: Enable/disable reranking (default: enabled)
   - **Tab**: Navigate between fields
   - **Enter**: Create and start chat
   - **Esc**: Cancel and return to chat list

5. **Chat Conversation Screen**:
   - Type your message in the input area
   - **Enter**: Send message
   - The AI will respond with streaming output
   - Previous messages are automatically stored and used for context
   - **Esc**: Return to chat list
   - **Ctrl+X**: Exit application

## RAG Flow

1. **User sends message** → Generate embedding for the message
2. **Vector search** → Find top-K similar messages from history
3. **Optional reranking** → Rerank results for better relevance
4. **Context injection** → Inject retrieved messages into prompt
5. **LLM generation** → Generate response with context
6. **Storage** → Store both user message and AI response with embeddings

## Configuration

### Default Settings

- **Database Path**: `~/.rag-chat/db/`
- **Nexa API URL**: `http://127.0.0.1:18181`
- **Default Temperature**: 0.7
- **Default Top K**: 5
- **Max Tokens**: 2048
- **Reranking**: Enabled by default

### RAG Parameters

Customize these when creating a chat:

- **Temperature**: Controls response randomness (0.0 = deterministic, 1.0 = creative)
- **Top K**: Number of similar messages to retrieve for context
- **Reranking**: Improves relevance of retrieved messages
- **System Prompt**: Defines the AI's personality and behavior

## Project Structure

```
rag-chat/
├── main.go                     # Application entry point
├── internal/
│   ├── nexa/                   # Nexa API client
│   │   ├── client.go          # HTTP client
│   │   ├── embeddings.go      # Embeddings API
│   │   ├── chat.go            # Chat completions API
│   │   └── reranking.go       # Reranking API
│   ├── vector/                 # Vector storage
│   │   ├── store.go           # Storage interface
│   │   ├── badger.go          # BadgerDB implementation
│   │   └── similarity.go      # Cosine similarity
│   ├── rag/                    # RAG pipeline
│   │   └── pipeline.go        # RAG orchestration
│   ├── models/                 # Data models
│   │   ├── chat.go            # Chat metadata
│   │   └── message.go         # Message structure
│   └── ui/                     # TUI components
│       ├── model_select.go    # Model selection
│       ├── chat_list.go       # Chat list
│       ├── chat_create.go     # Chat creation
│       └── chat_view.go       # Chat conversation
└── go.mod
```

## Keyboard Shortcuts

### Global
- **Ctrl+X**: Exit application
- **Ctrl+C**: Force quit

### Model Selection
- **↑/↓**: Navigate models
- **Enter**: Select model
- **Esc**: Go back (when selecting embedding model)

### Chat List
- **↑/↓**: Navigate chats
- **Enter**: Open chat
- **N** or **Ctrl+N**: Create new chat
- **D** or **Ctrl+D**: Delete selected chat

### Chat Creation
- **Tab/Shift+Tab**: Navigate fields
- **Enter**: Create chat (or new line in system prompt)
- **Space**: Toggle reranking checkbox
- **Esc**: Cancel

### Chat Conversation
- **Enter**: Send message
- **Esc**: Return to chat list
- **↑/↓**: Scroll message history

## Troubleshooting

### "Failed to get models"
- Ensure Nexa SDK is installed: `pip install nexaai`
- Check that models are downloaded: `nexa list`
- Verify Nexa server is running: Check http://127.0.0.1:18181

### "Failed to open badger database"
- Check that `~/.rag-chat/db/` directory is accessible
- Ensure no other instance is running
- Try deleting the database directory to start fresh

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
cd .\rag-ui

# Install dependencies
go mod download

# Build
go build -o rag-chat.exe .

# Run
.\rag-chat.exe
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

1. **POST /v1/embeddings**: Generate text embeddings
2. **POST /v1/chat/completions**: Generate chat responses (streaming)
3. **POST /v1/reranking**: Rerank documents by relevance

## Credits

Built with:
- [Nexa SDK](https://github.com/NexaAI/nexa-sdk) - Local AI model inference
- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [BadgerDB](https://github.com/dgraph-io/badger) - Embedded database
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Style definitions