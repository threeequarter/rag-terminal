# RAG Terminal

A Go-based RAG (Retrieval-Augmented Generation) chat application with a Bubbletea TUI interface, utilizing local Nexa SDK models and BadgerDB for vector storage.

![demo.gif](media%2Fdemo.gif)

## Features

- **Interactive TUI**: Built with Bubbletea for a clean terminal interface
- **RAG Pipeline**: Retrieval-Augmented Generation with vector similarity search
- **Document Processing**: Load and embed documents from files or directories
- **Content Optimization**: Smart excerpt extraction and text normalization for efficient token usage
- **Code-Aware Chunking**: Preserves code structure by chunking at function/class boundaries instead of arbitrary sizes
- **Multilingual Support**: Automatic language detection and stop word filtering for English, German, French, Spanish, and Russian
- **Deduplication**: SHA-256 hashing prevents duplicate document storage
- **Cross-Platform Path Detection**: Automatically detects file paths on Windows, Linux, and macOS
- **Multi-File Support**: Load and compare multiple files in a single message
- **LLM-based Reranking**: Scores and reranks retrieved context for relevance
- **File-Specific Queries**: Prioritizes content from mentioned files
- **Database-per-Chat**: Isolated vector storage for each chat conversation
- **Streaming Responses**: Real-time streaming of AI responses
- **Model Selection**: Choose from available LLM and embedding models
- **Chat Management**: Create, list, and delete chat conversations
- **Persistent Storage**: All chats and messages stored locally in BadgerDB
- **Debug Logging**: Automatic logging to file for troubleshooting

## Architecture

### Components

- **Nexa API Client** (`internal/nexa/`): HTTP client for Nexa SDK API
  - Embeddings generation
  - Chat completions (streaming and synchronous)

- **Vector Storage** (`internal/vector/`): BadgerDB-based vector database
  - Separate database per chat (database-per-chat pattern)
  - Message storage with embeddings
  - Document and chunk storage with embeddings
  - Cosine similarity search for messages and document chunks
  - Chat metadata management

- **RAG Pipeline** (`internal/rag/`): Orchestrates the RAG flow
  - Embedding generation for queries
  - Document loading and chunking from files/directories
  - Vector similarity search for messages and document chunks
  - LLM-based reranking (scores 0-10 per message)
  - Hierarchical context building with smart excerpt extraction
  - File-specific query handling (prioritizes chunks from mentioned files)
  - Asynchronous embedding updates to prevent blocking

- **Document Processing** (`internal/document/`): File and path handling
  - Cross-platform path detection (Windows, Linux, macOS)
  - Multi-path extraction from single message
  - Text cleaning and normalization (whitespace, boilerplate removal)
  - Smart excerpt extraction based on query relevance
  - Code-aware chunking (chunks at function/class/struct boundaries)
  - Supports Go, Python, JavaScript, TypeScript, Java, C#, Rust, C/C++
  - Multilingual stop word filtering (EN, DE, FR, ES, RU)
  - Automatic language detection
  - Content summarization (LLM-based and extractive)
  - Optimized chunking (50-char overlap for text, structure-based for code)
  - SHA-256 content hashing for deduplication
  - Support for various document formats

- **UI Components** (`internal/ui/`): Bubbletea TUI screens
  - Model selection (LLM and embedding)
  - Chat list management
  - Chat creation with RAG parameters
  - Chat conversation view with document loading support

- **Logging** (`internal/logging/`): Debug logging system
  - File-based logging next to executable
  - Timestamped log files (rag-debug-YYYY-MM-DD.log)
  - Debug, Info, and Error level logging
  - Detailed operation tracing for troubleshooting

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

4. **Load Documents** (Optional):
   - Type one or more file paths:
     - Windows: `C:\path\to\file.txt` or `C:\folder`
     - Linux/macOS: `/home/user/file.txt` or `/usr/local/docs`
   - Load multiple files: `Compare C:\file1.txt and C:\file2.txt`
   - Include queries: `Analyze /home/user/data.csv and explain the trends`
   - Documents are automatically chunked and embedded
   - All loaded documents become part of the chat context

5. **Chat Workflow**:
   - Send message → generates embedding → searches similar messages and document chunks
   - If reranking enabled: retrieves top-K × 2, LLM scores each, takes top-K
   - If reranking disabled: uses cosine similarity ranking
   - If specific file mentioned: prioritizes chunks from that file
   - Context injected as numbered conversations and relevant document excerpts
   - LLM generates response with full context
   - Both user and assistant messages stored with embeddings

## RAG Flow

### Document Loading Flow

1. **Path Detection** → Detects file paths in user input across platforms (Windows: `C:\docs\file.txt`, Unix: `/home/user/file.txt`)
2. **Document Loading** → Loads file(s) and chunks content into manageable pieces
3. **Batch Embedding** → Generates embeddings for all chunks using embedding model
4. **Storage** → Stores document metadata and chunks with embeddings in BadgerDB (deduplication via SHA-256 hash)
5. **Query Processing** (if query included with path) → Immediately processes user query with document context

### Chat Flow

1. **User Message** → Generate embedding with embedding model
2. **Vector Search** → Cosine similarity search for both messages and document chunks (retrieves top-K × 2 if reranking enabled)
3. **File-Specific Filtering** (if file mentioned) → Prioritizes chunks from mentioned file
4. **LLM Reranking** (optional, enabled by default):
   - LLM scores each message/chunk 0-10 for relevance to user query
   - Sorts by score, selects top-K most relevant
   - Falls back to cosine similarity if reranking fails
5. **Context Injection** → Build prompt with:
   - Loaded documents list (for directory structure awareness)
   - Relevant document excerpts with file paths
   - Numbered conversations from message history
6. **LLM Generation** → Stream response using full context
7. **Storage** → Store user message immediately, assistant message stored with empty embedding first, then updated asynchronously (500ms delay to avoid model switching conflicts)

## Configuration

### Default Settings

- **Database Path**: `~/.rag-terminal/db/<chat-id>/` (separate database per chat)
- **Nexa API URL**: `http://127.0.0.1:18181`
- **Temperature**: 0.7
- **Top K**: 5
- **Max Tokens**: 2048
- **Chunk Size**: 1000 characters
- **Chunk Overlap**: 50 characters (optimized for token efficiency)
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
│   │   └── pipeline.go        # RAG orchestration, hierarchical context building
│   ├── document/               # Document processing
│   │   ├── loader.go          # Document loading & chunking
│   │   ├── path_detector.go   # Path detection (legacy)
│   │   ├── path_detector_v2.go # Cross-platform multi-path detection
│   │   ├── cleaner.go         # Text normalization & deduplication
│   │   ├── extractor.go       # Smart excerpt extraction
│   │   ├── stopwords.go       # Multilingual stop word filtering
│   │   ├── summarizer.go      # Content summarization
│   │   ├── chunker.go         # Optimized text chunking
│   │   └── code_chunker.go    # Code-aware structural chunking
│   ├── logging/                # Logging system
│   │   └── logger.go          # File-based debug logging
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

### Documents not loading
- Ensure the file path is valid for your platform:
  - Windows: `C:\path\to\file.txt` (drive letter + colon + backslash)
  - Linux: `/home/user/file.txt` (absolute path starting with /)
  - macOS: `/Users/name/file.txt` or `/Applications/App.app`
- Check that the file/directory exists and is accessible
- Verify the file format is supported
- Review debug logs: `rag-debug-YYYY-MM-DD.log` next to the executable

### Path detection not working
- Windows paths: Must start with drive letter (e.g., `C:\`, `D:\`)
- Unix paths: Must be absolute (start with `/`) and match common prefixes (`/home/`, `/usr/`, `/Applications/`)
- Avoid invalid filename characters (`< > | " * ?`)
- Try wrapping paths with spaces in quotes

### Debug logs
- Log files are created next to the executable as `rag-debug-YYYY-MM-DD.log`
- Logs include detailed tracing of document loading, embedding generation, and search operations
- Use logs to troubleshoot issues with document processing and path detection

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
