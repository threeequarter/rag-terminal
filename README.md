# RAG Terminal

[![License](https://img.shields.io/badge/license-APACHE2-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.25+-00ADD8.svg)](https://golang.org)

A Go-based RAG (Retrieval-Augmented Generation) chat application with a Bubbletea TUI interface, utilizing local Nexa SDK models and BadgerDB for vector storage.

![demo.gif](media%2Fdemo.gif)

## Features

- **Interactive TUI**: Built with Bubbletea for a clean terminal interface
- **RAG Pipeline**: Retrieval-Augmented Generation with vector similarity search
- **Document Processing**: Load and embed documents from files or directories
- **Content Optimization**: Excerpt extraction and text normalization for efficient token usage
- **Code-Aware Chunking**: Preserves code structure by chunking at function/class boundaries instead of arbitrary sizes
- **Multilingual Support**: Automatic language detection and stop word filtering for English, German, French, Spanish, and Russian
- **Multi-File Support**: Load and compare multiple files in a single message
- **LLM-based Reranking**: Scores and reranks retrieved context for relevance (applied only to conversation messages)
- **File-Specific Queries**: Prioritizes content from mentioned files
- **Model Selection**: Choose from available LLM and embedding models
- **Chat Management**: Create, list, and delete chat conversations
- **Persistent Storage**: All chats and messages stored locally in BadgerDB

## Prerequisites

1. **Nexa SDK**: Download and install NexaSDK https://github.com/NexaAI/nexa-sdk

2. **Pull models**: Choose at least one embedding model and one LLM for your hardware from here https://sdk.nexa.ai/model or here https://huggingface.co/models and pull them using `nexa pull`

3. **Nexa Server**: Ensure Nexa server is running `nexa serve`, default port is expected

## Installation

### Option 1: Download Pre-built Binary

Pre-built binaries are available for all major platforms. Download the latest release: [Latest Release](https://github.com/threeequarter/rag-terminal/releases/latest)

### Option 2: Build from Source

1. Clone the repository:
   ```bash
   git clone https://github.com/threeequarter/rag-terminal.git
   cd rag-terminal
   ```

2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Build the application:
   ```bash
   # Windows
   go build -o rag-terminal.exe .

   # Linux/macOS
   go build -o rag-terminal .
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
   - **Temperature**: Response randomness (0-2, default: 0.7)
   - **Top K**: Context messages to retrieve (default: 5)
   - **Context Window**: Total context budget for single completion (query plus injected context plus model response)
   - **Use LLM Reranking**: Enabled by default - LLM scores retrieved messages for relevance

4. **Load Documents** (Optional):
   - Drop file or folder to input field (or type one or more file paths):
     - Windows: `C:\path\to\file.txt` or `C:\folder`
     - Linux/macOS: `/home/user/file.txt` or `/usr/local/docs`
   - Load multiple files: `Compare C:\file1.txt and C:\file2.txt`
   - Include queries: `Analyze /home/user/data.csv and explain the trends`
   - Documents are automatically chunked and embedded
   - All loaded documents become part of the chat context

5. **Chat Workflow**:
   - Send message → generates embedding → searches similar messages and document chunks
   - If LLM reranking enabled: retrieves top-K × 2, LLM scores each, takes top-K
   - If reranking disabled: uses cosine similarity ranking
   - If specific file mentioned: prioritizes chunks from that file
   - Context injected as numbered conversations and relevant document excerpts
   - LLM generates response with full context
   - Both user and assistant messages stored with embeddings

## RAG Flow

### Document Loading Flow

1. **Path Detection** → Detects file paths in user input
2. **Document Loading** → Loads file(s) and chunks content into manageable pieces
3. **Batch Embedding** → Generates embeddings for all chunks using embedding model
4. **Storage** → Stores document metadata and chunks with embeddings in BadgerDB
5. **Query Processing** (if query included with path) → Immediately processes user query with document context

### Chat Flow

1. **User Message** → Generate embedding with embedding model
2. **Vector Search** → Cosine similarity search for both messages and document chunks (retrieves top-K × 2 if LLM reranking enabled)
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

## Configuration

### Default Settings

- **Database Path**: `~/.rag-terminal/db/<chat-id>/`
- **Config file**: `~/.rag-terminal/config.yaml`
- **Logs**: `~/.rag-terminal/logs/rag-YYYY-MM-DD.log`
- **Nexa API URL**: `http://127.0.0.1:18181`

### Environment Variables

- **RT_LOGS**: Controls logging behavior (optional)
  - `debug`: Enable verbose logging (all operations)
  - `info`: Enable informational logging (major operations)
  - `error`: Enable error-only logging
  - Not set (default): Logging disabled

### Additional global parameters
Can be set in `~./rag-terminal/config.yaml`:
- **input_ratio**: What part of total **context window** will be used to inject context to model (default 0.6, so model will receive no more than 0.6 * 4096 = 2457 tokens as context to answer)
- **excerpts**: What part of **input_ratio** will be used to inject relevant document excerpts
- **history**: What part of **input_ratio** will be used to inject relevant parts of conversation history

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
