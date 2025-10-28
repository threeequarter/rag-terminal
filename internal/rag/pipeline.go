package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rag-terminal/internal/config"
	"rag-terminal/internal/document"
	"rag-terminal/internal/logging"
	"rag-terminal/internal/models"
	"rag-terminal/internal/nexa"
	"rag-terminal/internal/vector"
)

type Pipeline struct {
	nexaClient  *nexa.Client
	vectorStore vector.VectorStore
	config      *config.Config
}

type ChatParams struct {
	Temperature  float64
	MaxTokens    int
	TopK         int
	UseReranking bool
}

func NewPipeline(nexaClient *nexa.Client, vectorStore vector.VectorStore) *Pipeline {
	// Load config, fallback to default if loading fails
	cfg, err := config.Load()
	if err != nil {
		logging.Info("Failed to load config, using defaults: %v", err)
		cfg = config.DefaultConfig()
	}

	return &Pipeline{
		nexaClient:  nexaClient,
		vectorStore: vectorStore,
		config:      cfg,
	}
}

// groupAndMergeChunkedMessages groups message chunks and merges them into complete messages
// If any chunk of a message is in the results, all chunks are retrieved and merged
func (p *Pipeline) groupAndMergeChunkedMessages(ctx context.Context, messages []vector.Message) []vector.Message {
	// Separate regular messages and chunks
	regularMessages := []vector.Message{}
	chunkGroups := make(map[string][]vector.Message) // baseID -> chunks

	for _, msg := range messages {
		if strings.Contains(msg.ID, "-chunk-") {
			// Extract base message ID (everything before "-chunk-")
			parts := strings.Split(msg.ID, "-chunk-")
			if len(parts) == 2 {
				baseID := parts[0]
				chunkGroups[baseID] = append(chunkGroups[baseID], msg)
			}
		} else {
			regularMessages = append(regularMessages, msg)
		}
	}

	// For each chunk group, retrieve ALL chunks and merge
	mergedMessages := []vector.Message{}
	for baseID, chunks := range chunkGroups {
		// Retrieve all chunks from database for this base ID
		allChunks, err := p.retrieveAllChunks(ctx, baseID)
		if err != nil || len(allChunks) == 0 {
			// Fallback: use what we have
			allChunks = chunks
		}

		// Sort chunks by index
		sort.Slice(allChunks, func(i, j int) bool {
			return allChunks[i].ID < allChunks[j].ID
		})

		// Merge chunks into single message
		var contentBuilder strings.Builder
		for _, chunk := range allChunks {
			// Remove "[Part X/Y]" prefix if present
			content := chunk.Content
			if strings.HasPrefix(content, "[Part ") {
				if idx := strings.Index(content, "\n"); idx != -1 {
					content = content[idx+1:]
				}
			}
			contentBuilder.WriteString(content)
			if !strings.HasSuffix(content, "\n") {
				contentBuilder.WriteString("\n")
			}
		}

		// Create merged message using first chunk's metadata
		mergedMsg := allChunks[0]
		mergedMsg.ID = baseID
		mergedMsg.Content = strings.TrimSpace(contentBuilder.String())
		mergedMessages = append(mergedMessages, mergedMsg)
	}

	// Combine regular messages and merged messages
	result := append(regularMessages, mergedMessages...)

	// Sort by timestamp to maintain chronological order
	sort.Slice(result, func(i, j int) bool {
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result
}

// retrieveAllChunks retrieves all chunks for a given base message ID
func (p *Pipeline) retrieveAllChunks(ctx context.Context, baseID string) ([]vector.Message, error) {
	badgerStore, ok := p.vectorStore.(*vector.BadgerStore)
	if !ok {
		return nil, fmt.Errorf("vector store is not BadgerStore")
	}

	// Search for all messages with IDs matching baseID-chunk-*
	allMessages, err := badgerStore.GetAllMessages(ctx)
	if err != nil {
		return nil, err
	}

	chunks := []vector.Message{}
	prefix := baseID + "-chunk-"
	for _, msg := range allMessages {
		if strings.HasPrefix(msg.ID, prefix) {
			chunks = append(chunks, msg)
		}
	}

	return chunks, nil
}

func (p *Pipeline) rerankMessagesWithLLM(ctx context.Context, llmModel, query string, messages []vector.Message, topK int) ([]vector.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}

	// Build reranking prompt
	var promptBuilder strings.Builder
	promptBuilder.WriteString("You are a relevance scoring system. Given a user query and a list of message pairs, ")
	promptBuilder.WriteString("score each message's relevance to the query on a scale of 0-10.\n\n")
	promptBuilder.WriteString(fmt.Sprintf("User Query: %s\n\n", query))
	promptBuilder.WriteString("Messages to score:\n")

	for i, msg := range messages {
		promptBuilder.WriteString(fmt.Sprintf("%d. [%s]: %s\n", i+1, msg.Role, msg.Content))
	}

	promptBuilder.WriteString("\nRespond ONLY with a JSON array of scores in order, e.g., [8.5, 3.2, 9.0, ...]. ")
	promptBuilder.WriteString("Higher scores mean more relevant to the query.")

	// Call LLM for scoring
	req := nexa.ChatCompletionRequest{
		Model: llmModel,
		Messages: []nexa.ChatMessage{
			{Role: "user", Content: promptBuilder.String()},
		},
		Temperature: 0.1, // Low temperature for consistent scoring
		MaxTokens:   500,
		Stream:      false,
	}

	// Use non-streaming API
	response, err := p.nexaClient.ChatCompletionSync(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM reranking scores: %w", err)
	}

	// Parse JSON scores from response
	var scores []float64
	response = strings.TrimSpace(response)

	// Extract JSON array if wrapped in markdown code blocks
	if strings.HasPrefix(response, "```") {
		lines := strings.Split(response, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "[") {
				response = strings.Join(lines[i:], "\n")
				break
			}
		}
		response = strings.TrimSuffix(strings.TrimSpace(response), "```")
	}

	if err := json.Unmarshal([]byte(response), &scores); err != nil {
		return nil, fmt.Errorf("failed to parse LLM scores: %w (response: %s)", err, response)
	}

	if len(scores) != len(messages) {
		return nil, fmt.Errorf("LLM returned %d scores but expected %d", len(scores), len(messages))
	}

	// Create scored message pairs
	type scoredMessage struct {
		message vector.Message
		score   float64
	}

	scored := make([]scoredMessage, len(messages))
	for i, msg := range messages {
		scored[i] = scoredMessage{message: msg, score: scores[i]}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Take top K
	if len(scored) > topK {
		scored = scored[:topK]
	}

	result := make([]vector.Message, len(scored))
	for i, sm := range scored {
		result[i] = sm.message
	}

	return result, nil
}

func (p *Pipeline) buildPromptWithContext(systemPrompt string, contextMessages []vector.Message, userMessage string) string {
	var builder strings.Builder

	// Filter out messages with identical content to current message (to avoid treating first message as context)
	var relevantContext []vector.Message
	for _, msg := range contextMessages {
		if msg.Content != userMessage {
			relevantContext = append(relevantContext, msg)
		}
	}

	if len(relevantContext) > 0 {
		builder.WriteString("You have access to the following previous conversation history for reference:\n\n")
		for _, msg := range relevantContext {
			builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content))
		}
		builder.WriteString("Use the above context to help answer the user's question if relevant.\n\n")
		builder.WriteString("---\n\n")
	}

	builder.WriteString("User's question or message to you: ")
	builder.WriteString(userMessage)

	return builder.String()
}

func (p *Pipeline) buildPromptWithContextAndDocuments(systemPrompt string, contextMessages []vector.Message, contextChunks []vector.DocumentChunk, userMessage string) string {
	var builder strings.Builder

	// Add document chunks first (most specific context)
	if len(contextChunks) > 0 {
		builder.WriteString("You have access to the following relevant document excerpts:\n\n")
		for i, chunk := range contextChunks {
			builder.WriteString(fmt.Sprintf("[Document %d: %s]\n%s\n\n", i+1, chunk.FilePath, chunk.Content))
		}
		builder.WriteString("---\n\n")
	}

	// Filter out messages with identical content to current message (to avoid treating first message as context)
	var relevantContext []vector.Message
	for _, msg := range contextMessages {
		if msg.Content != userMessage {
			relevantContext = append(relevantContext, msg)
		}
	}

	// Add conversation context
	if len(relevantContext) > 0 {
		builder.WriteString("Previous conversation history:\n\n")
		for _, msg := range relevantContext {
			builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", msg.Role, msg.Content))
		}
		builder.WriteString("---\n\n")
	}

	if len(contextChunks) > 0 || len(relevantContext) > 0 {
		builder.WriteString("Use the above information to help answer the user's question.\n\n")
		builder.WriteString("---\n\n")
	}

	builder.WriteString("User's question or message to you: ")
	builder.WriteString(userMessage)

	return builder.String()
}

func (p *Pipeline) buildPromptWithContextAndDocumentsAndFileList(chat *vector.Chat, contextMessages []vector.Message, contextChunks []vector.DocumentChunk, allDocs []vector.Document, userMessage string) string {
	var builder strings.Builder

	// Calculate token budgets
	contextWindow := chat.ContextWindow
	if contextWindow <= 0 {
		contextWindow = 4096 // Fallback to default
	}

	maxTokens := chat.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048 // Fallback to default
	}

	budget := CalculateTokenBudget(contextWindow, maxTokens, p.config)

	// HIERARCHICAL CONTEXT STRUCTURE
	// Layer 1: Document overview (uses FileListBudget)
	if len(allDocs) > 0 {
		builder.WriteString("# Available Documents\n")
		fileListChars := budget.FileListBudget * CharsPerToken

		var fileListBuilder strings.Builder
		for i, doc := range allDocs {
			line := fmt.Sprintf("%d. %s (%d chunks)\n", i+1, doc.FileName, doc.ChunkCount)
			if fileListBuilder.Len()+len(line) > fileListChars {
				break // Stop if we exceed budget
			}
			fileListBuilder.WriteString(line)
		}
		builder.WriteString(fileListBuilder.String())
		builder.WriteString("\n")
	}

	// Layer 2: Relevant excerpts (uses ExcerptsBudget)
	if len(contextChunks) > 0 {
		builder.WriteString("# Relevant Information\n\n")

		excerptCharsRemaining := budget.ExcerptsBudget * CharsPerToken
		extractor := document.NewExtractor()

		for _, chunk := range contextChunks {
			if excerptCharsRemaining <= 0 {
				break // Budget exhausted
			}

			// Calculate max excerpt size for this chunk
			maxExcerptSize := 500
			if excerptCharsRemaining < maxExcerptSize {
				maxExcerptSize = excerptCharsRemaining
			}

			if maxExcerptSize < 50 {
				break // Not enough space for meaningful excerpt
			}

			excerpt := extractor.ExtractRelevantExcerpt(chunk.Content, userMessage, maxExcerptSize)
			fileName := filepath.Base(chunk.FilePath)

			chunkText := fmt.Sprintf("[%s]\n%s\n\n", fileName, excerpt)
			builder.WriteString(chunkText)

			excerptCharsRemaining -= len(chunkText)
		}
		builder.WriteString("---\n\n")
	}

	// Layer 3: Conversation history (uses HistoryBudget)
	// Filter out messages with identical content to current message (to avoid treating first message as context)
	var relevantContext []vector.Message
	for _, msg := range contextMessages {
		if msg.Content != userMessage {
			relevantContext = append(relevantContext, msg)
		}
	}

	if len(relevantContext) > 0 {
		builder.WriteString("# Previous Conversation History\n")

		historyCharsRemaining := budget.HistoryBudget * CharsPerToken

		for _, msg := range relevantContext {
			if historyCharsRemaining <= 0 {
				break // Budget exhausted
			}

			content := msg.Content
			maxContentSize := historyCharsRemaining - 20 // Reserve space for role label and formatting

			if maxContentSize < 20 {
				break // Not enough space
			}

			if len(content) > maxContentSize {
				content = content[:maxContentSize-3] + "..."
			}

			msgText := fmt.Sprintf("[%s]: %s\n", msg.Role, content)
			builder.WriteString(msgText)
			historyCharsRemaining -= len(msgText)
		}
		builder.WriteString("\n---\n\n")
	}

	// Instruction to use context
	if len(allDocs) > 0 || len(contextChunks) > 0 || len(relevantContext) > 0 {
		builder.WriteString("Use the above information to help answer the user's question.\n\n")
		builder.WriteString("---\n\n")
	}

	// Current query (full detail - not budget-limited as it's essential)
	builder.WriteString("# User's question or message to you: ")
	builder.WriteString(userMessage)

	return builder.String()
}

// chunkAndStoreQAPair chunks a Q&A pair text if it exceeds token limits and stores each chunk with embeddings
// This prevents errors when Q&A pairs exceed the embedding model's max sequence length (1024 tokens)
func (p *Pipeline) chunkAndStoreQAPair(ctx context.Context, chat *vector.Chat, qaText string) error {
	// Conservative token limit per chunk to account for:
	// 1. Imperfect char-to-token estimation (varies by language/content)
	// 2. Prefix text added to chunks ("[Part X/Y]")
	// 3. Safety margin for embedding model's 1024 token hard limit
	const maxTokensPerChunk = 300 // Very safe limit: 300 * 4 = 1200 chars << 1024 tokens

	// Estimate tokens in Q&A text using conservative 4 chars/token ratio
	estimatedTokens := EstimateTokens(qaText)

	// If small enough, store as single context message
	if estimatedTokens <= maxTokensPerChunk {
		qaEmbeddings, err := p.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, []string{qaText})
		if err != nil {
			return fmt.Errorf("failed to generate Q&A pair embedding: %w", err)
		}

		qaPairMsg := models.NewMessage(chat.ID, "context", qaText)
		if err := p.vectorStore.StoreMessage(ctx, qaPairMsg.ID, "context", qaText, qaEmbeddings[0], time.Now()); err != nil {
			return fmt.Errorf("failed to store Q&A pair: %w", err)
		}

		return nil
	}

	// Q&A pair is too long, need to chunk it
	logging.Info("Q&A pair is long (%d tokens estimated), chunking into smaller pieces", estimatedTokens)

	// Use existing chunker with conservative chunk size for Q&A pairs
	chunker := document.NewChunker()
	chunker.ChunkSize = maxTokensPerChunk * CharsPerToken // Convert tokens to chars (300 * 4 = 1200)
	chunker.ChunkOverlap = 50                             // Small overlap to maintain context

	chunks := chunker.ChunkDocument(qaText)
	logging.Info("Split Q&A pair into %d chunks", len(chunks))

	// Generate base ID for all chunks
	baseID := models.NewMessage(chat.ID, "context", "").ID

	// Prepare all chunk contents for batch embedding with safety validation
	chunkContents := make([]string, 0, len(chunks))
	validChunkIndices := make([]int, 0, len(chunks))

	for i, chunk := range chunks {
		// Prefix each chunk with abbreviated context so it's still meaningful
		prefix := fmt.Sprintf("[Part %d/%d] ", i+1, len(chunks))
		chunkContent := prefix + chunk.Content

		// Safety check: validate chunk size before embedding
		// Embedding model has hard limit of 1024 tokens
		// Use conservative estimate: if > 800 tokens (3200 chars), truncate
		const maxSafeTokens = 800
		const maxSafeChars = maxSafeTokens * CharsPerToken // 3200 chars

		if len(chunkContent) > maxSafeChars {
			logging.Info("Chunk %d exceeds safe size (%d chars), truncating to %d chars",
				i, len(chunkContent), maxSafeChars)
			chunkContent = chunkContent[:maxSafeChars] + "..."
		}

		chunkContents = append(chunkContents, chunkContent)
		validChunkIndices = append(validChunkIndices, i)
	}

	// Generate embeddings for all chunks in batch
	embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, chunkContents)
	if err != nil {
		return fmt.Errorf("failed to generate embeddings for Q&A chunks: %w", err)
	}

	// Store each chunk as separate context message with chunk ID
	for i, chunkContent := range chunkContents {
		chunkID := fmt.Sprintf("%s-chunk-%d", baseID, i)

		if err := p.vectorStore.StoreMessage(ctx, chunkID, "context", chunkContent, embeddings[i], time.Now()); err != nil {
			return fmt.Errorf("failed to store Q&A chunk %d: %w", i, err)
		}
	}

	logging.Info("Successfully stored %d Q&A chunks", len(chunkContents))
	return nil
}

// LoadDocuments loads documents from a file or directory path
func (p *Pipeline) LoadDocuments(ctx context.Context, chat *vector.Chat, path string, query string) (<-chan string, <-chan error, error) {
	logging.Info("LoadDocuments called: path=%s, query=%s, chatID=%s", path, query, chat.ID)

	loader := document.NewLoader()

	// Load documents
	loadResult, err := loader.LoadPath(ctx, path, chat.ID)
	if err != nil {
		logging.Error("Failed to load documents from path %s: %v", path, err)
		return nil, nil, fmt.Errorf("failed to load documents: %w", err)
	}

	logging.Info("Load result: success=%d, total_chunks=%d, errors=%d",
		loadResult.SuccessCount, loadResult.TotalChunks, len(loadResult.Errors))

	if loadResult.SuccessCount == 0 {
		logging.Error("No supported documents found in path: %s", path)
		return nil, nil, fmt.Errorf("no supported documents found in path")
	}

	// Create response channels
	responseChan := make(chan string, 10)
	errorChan := make(chan error, 1)

	// Process documents asynchronously
	go func() {
		defer close(responseChan)
		defer close(errorChan)

		// Store documents and embed chunks
		totalChunks := 0
		badgerStore, ok := p.vectorStore.(*vector.BadgerStore)
		if !ok {
			errorChan <- fmt.Errorf("vector store is not BadgerStore type")
			return
		}

		for _, doc := range loadResult.Documents {
			logging.Debug("Processing document: %s (size=%d, hash=%s)", doc.FileName, doc.FileSize, doc.ContentHash)

			// Check if document with same content hash already exists
			existingDoc, err := badgerStore.FindDocumentByHash(ctx, doc.ContentHash)
			if err == nil && existingDoc != nil {
				logging.Info("Document %s already exists (duplicate of %s), skipping", doc.FileName, existingDoc.FileName)
				responseChan <- fmt.Sprintf("Skipped %s (duplicate of %s)\n", doc.FileName, existingDoc.FileName)
				continue
			}

			// Store document metadata
			if err := badgerStore.StoreDocument(ctx, &doc); err != nil {
				logging.Error("Failed to store document metadata for %s: %v", doc.FileName, err)
				errorChan <- fmt.Errorf("failed to store document %s: %w", doc.FileName, err)
				return
			}
			logging.Debug("Stored document metadata for %s", doc.FileName)

			// Get chunks for this document
			chunks, err := loader.GetDocumentChunks(doc.ID, doc.FilePath, chat.ID)
			if err != nil {
				logging.Error("Failed to get chunks for %s: %v", doc.FileName, err)
				errorChan <- fmt.Errorf("failed to chunk document %s: %w", doc.FileName, err)
				return
			}
			logging.Debug("Created %d chunks for %s", len(chunks), doc.FileName)

			// Prepare chunk contents for batch embedding
			chunkContents := make([]string, len(chunks))
			for i, chunk := range chunks {
				chunkContents[i] = chunk.Content
			}

			// Generate embeddings for all chunks in batch
			logging.Debug("Generating embeddings for %d chunks of %s", len(chunks), doc.FileName)
			embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, chunkContents)
			if err != nil {
				logging.Error("Failed to generate embeddings for %s: %v", doc.FileName, err)
				errorChan <- fmt.Errorf("failed to generate embeddings for %s: %w", doc.FileName, err)
				return
			}
			logging.Debug("Generated %d embeddings for %s (dim=%d)", len(embeddings), doc.FileName, len(embeddings[0]))

			// Store chunks with embeddings
			for i, chunk := range chunks {
				chunk.Embedding = embeddings[i]
				if err := badgerStore.StoreDocumentChunk(ctx, &chunk); err != nil {
					logging.Error("Failed to store chunk %d of %s: %v", i, doc.FileName, err)
					errorChan <- fmt.Errorf("failed to store chunk %d of %s: %w", i, doc.FileName, err)
					return
				}
			}
			logging.Info("Successfully stored %d chunks for %s", len(chunks), doc.FileName)

			totalChunks += len(chunks)
			// Removed verbose output - all logged to file instead
		}

		// If user provided a query, process it immediately
		if query != "" {
			logging.Info("Processing query after document loading: %s", query)

			// Generate embedding for query
			logging.Debug("Generating embedding for query")
			embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, []string{query})
			if err != nil {
				logging.Error("Failed to generate query embedding: %v", err)
				errorChan <- fmt.Errorf("failed to generate query embedding: %w", err)
				return
			}
			queryEmbedding := embeddings[0]
			logging.Debug("Query embedding generated (dim=%d)", len(queryEmbedding))

			// Store user query as message WITHOUT embedding (will be embedded as Q&A pair later)
			userMsg := models.NewMessage(chat.ID, "user", query)
			if err := p.vectorStore.StoreMessage(ctx, userMsg.ID, "user", query, []float32{}, time.Now()); err != nil {
				logging.Error("Failed to store user query: %v", err)
				errorChan <- fmt.Errorf("failed to store user query: %w", err)
				return
			}
			logging.Debug("Stored user query message (without embedding)")

			// Search for relevant context (both messages and document chunks)
			retrievalTopK := chat.TopK
			if chat.UseReranking {
				retrievalTopK = chat.TopK * 2
			}
			logging.Debug("Searching for similar content (topK=%d)", retrievalTopK)

			var contextMessages []vector.Message
			var contextChunks []vector.DocumentChunk

			msgs, chunks, err := badgerStore.SearchSimilarWithChunks(ctx, queryEmbedding, retrievalTopK)
			if err != nil {
				logging.Error("Failed to search similar content: %v", err)
				errorChan <- fmt.Errorf("failed to search context: %w", err)
				return
			}
			contextMessages = msgs
			contextChunks = chunks

			logging.Info("Search results: found %d messages and %d document chunks", len(msgs), len(chunks))
			for i, chunk := range chunks {
				logging.Debug("Chunk %d: file=%s, pos=%d-%d, content_len=%d",
					i, chunk.FilePath, chunk.StartPos, chunk.EndPos, len(chunk.Content))
			}

			// Get full document list for context
			allDocs, err := badgerStore.GetDocuments(ctx)
			if err != nil {
				logging.Error("Failed to get documents list: %v", err)
				allDocs = []vector.Document{} // Continue with empty list
			}
			logging.Debug("Total documents in chat: %d", len(allDocs))

			// Build prompt with both message and document context
			prompt := p.buildPromptWithContextAndDocumentsAndFileList(chat, contextMessages, contextChunks, allDocs, query)

			// Call LLM
			req := nexa.ChatCompletionRequest{
				Model: chat.LLMModel,
				Messages: []nexa.ChatMessage{
					{Role: "system", Content: chat.SystemPrompt},
					{Role: "user", Content: prompt},
				},
				Temperature: chat.Temperature,
				MaxTokens:   chat.MaxTokens,
				TopK:        chat.TopK,
				Nctx:        chat.ContextWindow,
				Stream:      true,
			}

			streamChan, errChan, err := p.nexaClient.ChatCompletion(ctx, req)
			if err != nil {
				errorChan <- fmt.Errorf("failed to start chat completion: %w", err)
				return
			}

			// Stream response
			var fullResponse strings.Builder
			for {
				select {
				case token, ok := <-streamChan:
					if !ok {
						// Store assistant message and Q&A pair
						assistantContent := fullResponse.String()

						// First, store the assistant message WITHOUT embedding (for display purposes)
						assistantMsg := models.NewMessage(chat.ID, "assistant", assistantContent)
						if err := p.vectorStore.StoreMessage(ctx, assistantMsg.ID, "assistant", assistantContent, []float32{}, time.Now()); err != nil {
							logging.Error("Failed to store assistant message: %v", err)
							errorChan <- fmt.Errorf("failed to store assistant message: %w", err)
							return
						}

						// Now create and store the Q&A pair with embedding (for retrieval purposes)
						qaText := fmt.Sprintf("Previously user asked: %s\nYou answered: %s", query, assistantContent)

						// Use chunking helper to handle long Q&A pairs
						if err := p.chunkAndStoreQAPair(ctx, chat, qaText); err != nil {
							logging.Error("Failed to chunk and store Q&A pair: %v", err)
							errorChan <- fmt.Errorf("failed to store Q&A pair: %w", err)
							return
						}

						return
					}
					fullResponse.WriteString(token)
					responseChan <- token

				case err := <-errChan:
					if err != nil {
						errorChan <- err
						return
					}

				case <-ctx.Done():
					errorChan <- ctx.Err()
					return
				}
			}
		}
	}()

	return responseChan, errorChan, nil
}

// LoadMultipleDocuments loads documents from multiple file or directory paths
func (p *Pipeline) LoadMultipleDocuments(ctx context.Context, chat *vector.Chat, paths []document.PathDetectionResult, query string) (<-chan string, <-chan error, error) {
	logging.Info("LoadMultipleDocuments called: pathCount=%d, query=%s, chatID=%s", len(paths), query, chat.ID)

	if len(paths) == 0 {
		return nil, nil, fmt.Errorf("no paths provided")
	}

	// Create response channels
	responseChan := make(chan string, 10)
	errorChan := make(chan error, 1)

	// Process all paths asynchronously
	go func() {
		defer close(responseChan)
		defer close(errorChan)

		loader := document.NewLoader()
		allDocuments := []vector.Document{}
		totalChunks := 0
		totalSuccess := 0

		badgerStore, ok := p.vectorStore.(*vector.BadgerStore)
		if !ok {
			errorChan <- fmt.Errorf("vector store is not BadgerStore type")
			return
		}

		// Count total documents to embed
		totalDocsToEmbed := 0
		for _, pathResult := range paths {
			loadResult, err := loader.LoadPath(ctx, pathResult.Path, chat.ID)
			if err == nil {
				totalDocsToEmbed += loadResult.SuccessCount
			}
		}

		// Send initial progress
		responseChan <- fmt.Sprintf("@@PROGRESS:0/%d@@", totalDocsToEmbed)

		// Reload loader for actual processing
		loader = document.NewLoader()

		// Load each path
		for i, pathResult := range paths {
			logging.Info("Processing path %d/%d: %s", i+1, len(paths), pathResult.Path)

			loadResult, err := loader.LoadPath(ctx, pathResult.Path, chat.ID)
			if err != nil {
				logging.Error("Failed to load documents from path %s: %v", pathResult.Path, err)
				responseChan <- fmt.Sprintf("⚠ Failed to load %s: %v\n", pathResult.Path, err)
				continue
			}

			if loadResult.SuccessCount == 0 {
				logging.Info("No supported documents found in path: %s", pathResult.Path)
				responseChan <- fmt.Sprintf("⚠ No supported documents in %s\n", pathResult.Path)
				continue
			}

			// Process documents from this path
			for _, doc := range loadResult.Documents {
				logging.Debug("Processing document: %s (size=%d, hash=%s)", doc.FileName, doc.FileSize, doc.ContentHash)

				// Check for duplicates
				existingDoc, err := badgerStore.FindDocumentByHash(ctx, doc.ContentHash)
				if err == nil && existingDoc != nil {
					logging.Info("Document %s already exists (duplicate of %s), skipping", doc.FileName, existingDoc.FileName)
					responseChan <- fmt.Sprintf("⊘ Skipped %s (duplicate)\n", doc.FileName)
					continue
				}

				// Store document metadata
				if err := badgerStore.StoreDocument(ctx, &doc); err != nil {
					logging.Error("Failed to store document metadata for %s: %v", doc.FileName, err)
					errorChan <- fmt.Errorf("failed to store document %s: %w", doc.FileName, err)
					return
				}

				// Get chunks
				chunks, err := loader.GetDocumentChunks(doc.ID, doc.FilePath, chat.ID)
				if err != nil {
					logging.Error("Failed to get chunks for %s: %v", doc.FileName, err)
					errorChan <- fmt.Errorf("failed to chunk document %s: %w", doc.FileName, err)
					return
				}

				// Prepare chunk contents for batch embedding
				chunkContents := make([]string, len(chunks))
				for i, chunk := range chunks {
					chunkContents[i] = chunk.Content
				}

				// Generate embeddings
				embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, chunkContents)
				if err != nil {
					logging.Error("Failed to generate embeddings for %s: %v", doc.FileName, err)
					errorChan <- fmt.Errorf("failed to generate embeddings for %s: %w", doc.FileName, err)
					return
				}

				// Store chunks with embeddings
				for i, chunk := range chunks {
					chunk.Embedding = embeddings[i]
					if err := badgerStore.StoreDocumentChunk(ctx, &chunk); err != nil {
						logging.Error("Failed to store chunk %d of %s: %v", i, doc.FileName, err)
						errorChan <- fmt.Errorf("failed to store chunk %d of %s: %w", i, doc.FileName, err)
						return
					}
				}

				logging.Info("Successfully stored %d chunks for %s", len(chunks), doc.FileName)

				totalChunks += len(chunks)
				totalSuccess++
				allDocuments = append(allDocuments, doc)

				// Send progress update
				responseChan <- fmt.Sprintf("@@PROGRESS:%d/%d@@", totalSuccess, totalDocsToEmbed)
			}
		}

		// Update chat file count
		if totalSuccess > 0 {
			chat.FileCount += totalSuccess
			if err := badgerStore.UpdateChat(ctx, chat); err != nil {
				logging.Error("Failed to update chat file count: %v", err)
			}
		}

		// Documents loaded successfully, no success message needed in chat

		// If user provided a query, process it immediately
		if query != "" {
			logging.Info("Processing query after loading %d documents: %s", len(allDocuments), query)

			// Generate embedding for query
			embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, []string{query})
			if err != nil {
				logging.Error("Failed to generate query embedding: %v", err)
				errorChan <- fmt.Errorf("failed to generate query embedding: %w", err)
				return
			}
			queryEmbedding := embeddings[0]

			// Store user query as message WITHOUT embedding (will be embedded as Q&A pair later)
			userMsg := models.NewMessage(chat.ID, "user", query)
			if err := p.vectorStore.StoreMessage(ctx, userMsg.ID, "user", query, []float32{}, time.Now()); err != nil {
				logging.Error("Failed to store user query: %v", err)
				errorChan <- fmt.Errorf("failed to store user query: %w", err)
				return
			}

			// Search for relevant context
			retrievalTopK := chat.TopK
			if chat.UseReranking {
				retrievalTopK = chat.TopK * 2
			}

			msgs, chunks, err := badgerStore.SearchSimilarWithChunks(ctx, queryEmbedding, retrievalTopK)
			if err != nil {
				logging.Error("Failed to search similar content: %v", err)
				errorChan <- fmt.Errorf("failed to search context: %w", err)
				return
			}

			// Get full document list
			allDocs, _ := badgerStore.GetDocuments(ctx)
			prompt := p.buildPromptWithContextAndDocumentsAndFileList(chat, msgs, chunks, allDocs, query)

			// Call LLM
			req := nexa.ChatCompletionRequest{
				Model: chat.LLMModel,
				Messages: []nexa.ChatMessage{
					{Role: "system", Content: chat.SystemPrompt},
					{Role: "user", Content: prompt},
				},
				Temperature: chat.Temperature,
				MaxTokens:   chat.MaxTokens,
				TopK:        chat.TopK,
				Nctx:        chat.ContextWindow,
				Stream:      true,
			}

			streamChan, streamErrChan, err := p.nexaClient.ChatCompletion(ctx, req)
			if err != nil {
				errorChan <- fmt.Errorf("failed to call LLM: %w", err)
				return
			}

			// Stream LLM response
			var fullResponse strings.Builder
			responseChan <- "\n"

			for {
				select {
				case token, ok := <-streamChan:
					if !ok {
						// Store assistant message and Q&A pair
						assistantContent := fullResponse.String()

						// First, store the assistant message WITHOUT embedding (for display purposes)
						assistantMsg := models.NewMessage(chat.ID, "assistant", assistantContent)
						if err := p.vectorStore.StoreMessage(ctx, assistantMsg.ID, "assistant", assistantContent, []float32{}, time.Now()); err != nil {
							logging.Error("Failed to store assistant message: %v", err)
							errorChan <- fmt.Errorf("failed to store assistant message: %w", err)
							return
						}

						// Now create and store the Q&A pair with embedding (for retrieval purposes)
						qaText := fmt.Sprintf("Previously user asked: %s\nYou answered: %s", query, assistantContent)

						// Use chunking helper to handle long Q&A pairs
						if err := p.chunkAndStoreQAPair(ctx, chat, qaText); err != nil {
							logging.Error("Failed to chunk and store Q&A pair: %v", err)
							errorChan <- fmt.Errorf("failed to store Q&A pair: %w", err)
							return
						}

						return
					}
					fullResponse.WriteString(token)
					responseChan <- token

				case err := <-streamErrChan:
					if err != nil {
						errorChan <- fmt.Errorf("LLM stream error: %w", err)
						return
					}
				}
			}
		}
	}()

	return responseChan, errorChan, nil
}

// ProcessUserMessageWithDocuments is an enhanced version that considers document chunks
func (p *Pipeline) ProcessUserMessageWithDocuments(
	ctx context.Context,
	chat *vector.Chat,
	userMessage string,
) (<-chan string, <-chan error, error) {
	// Step 1: Generate embedding for user message (for retrieval purposes)
	embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, []string{userMessage})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate user message embedding: %w", err)
	}
	userEmbedding := embeddings[0]

	// Step 2: Store user message WITHOUT embedding (will be embedded as Q&A pair later)
	// We pass empty embedding to avoid immediate indexing
	userMsg := models.NewMessage(chat.ID, "user", userMessage)
	if err := p.vectorStore.StoreMessage(ctx, userMsg.ID, "user", userMessage, []float32{}, time.Now()); err != nil {
		return nil, nil, fmt.Errorf("failed to store user message: %w", err)
	}

	// Step 3: Search for similar content (both messages and document chunks)
	retrievalTopK := chat.TopK * 2
	if !chat.UseReranking {
		retrievalTopK = chat.TopK
	}

	var contextMessages []vector.Message
	var contextChunks []vector.DocumentChunk

	if badgerStore, ok := p.vectorStore.(*vector.BadgerStore); ok {
		msgs, chunks, err := badgerStore.SearchSimilarWithChunks(ctx, userEmbedding, retrievalTopK)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to search similar content: %w", err)
		}
		// Merge chunked messages into complete messages
		contextMessages = p.groupAndMergeChunkedMessages(ctx, msgs)
		contextChunks = chunks

		// Check if user mentioned specific filenames - if so, prioritize chunks from those files
		allDocs, _ := badgerStore.GetDocuments(ctx)
		mentionedFiles := findMentionedFiles(userMessage, allDocs)
		if len(mentionedFiles) > 0 {
			logging.Info("User mentioned specific files: %v - filtering chunks", mentionedFiles)
			// Filter to only chunks from those files, or if none found, get all chunks from those files
			filteredChunks := filterChunksByFiles(chunks, mentionedFiles)
			if len(filteredChunks) == 0 {
				// No chunks matched via similarity, fetch all chunks from the mentioned files
				logging.Info("No similar chunks from %v, fetching all chunks from files", mentionedFiles)
				filteredChunks = getAllChunksFromFiles(badgerStore, ctx, mentionedFiles)
			}
			contextChunks = filteredChunks
			logging.Debug("After filename filtering: %d chunks from %d files", len(contextChunks), len(mentionedFiles))
		}
	} else {
		// Fallback to message-only search
		msgs, err := p.vectorStore.SearchSimilar(ctx, userEmbedding, retrievalTopK)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to search similar messages: %w", err)
		}
		// Merge chunked messages into complete messages
		contextMessages = p.groupAndMergeChunkedMessages(ctx, msgs)
	}

	// Step 4: Optional LLM-based reranking (only for messages for now)
	if chat.UseReranking && len(contextMessages) > 0 {
		reranked, err := p.rerankMessagesWithLLM(ctx, chat.LLMModel, userMessage, contextMessages, chat.TopK/2)
		if err == nil {
			contextMessages = reranked
		} else {
			if len(contextMessages) > chat.TopK/2 {
				contextMessages = contextMessages[:chat.TopK/2]
			}
		}
	} else {
		if len(contextMessages) > chat.TopK/2 {
			contextMessages = contextMessages[:chat.TopK/2]
		}
	}

	// Limit document chunks
	if len(contextChunks) > chat.TopK/2 {
		contextChunks = contextChunks[:chat.TopK/2]
	}

	// Step 5: Build prompt with context
	var prompt string
	if len(contextChunks) > 0 {
		// Get full document list
		var allDocs []vector.Document
		if badgerStore, ok := p.vectorStore.(*vector.BadgerStore); ok {
			allDocs, _ = badgerStore.GetDocuments(ctx) // Ignore error, continue with empty list
		}
		prompt = p.buildPromptWithContextAndDocumentsAndFileList(chat, contextMessages, contextChunks, allDocs, userMessage)
	} else {
		prompt = p.buildPromptWithContext(chat.SystemPrompt, contextMessages, userMessage)
	}

	// Step 6: Call chat completion
	req := nexa.ChatCompletionRequest{
		Model: chat.LLMModel,
		Messages: []nexa.ChatMessage{
			{Role: "system", Content: chat.SystemPrompt},
			{Role: "user", Content: prompt},
		},
		Temperature: chat.Temperature,
		MaxTokens:   chat.MaxTokens,
		TopK:        chat.TopK,
		Nctx:        chat.ContextWindow,
		Stream:      true,
	}

	streamChan, errChan, err := p.nexaClient.ChatCompletion(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start chat completion: %w", err)
	}

	// Step 7: Collect response and store assistant message
	responseChan := make(chan string, 10)
	finalErrChan := make(chan error, 1)

	go func() {
		defer close(responseChan)
		defer close(finalErrChan)

		var fullResponse strings.Builder
		for {
			select {
			case token, ok := <-streamChan:
				if !ok {
					// Store assistant message and Q&A pair
					assistantContent := fullResponse.String()

					// First, store the assistant message WITHOUT embedding (for display purposes)
					assistantMsg := models.NewMessage(chat.ID, "assistant", assistantContent)
					if err := p.vectorStore.StoreMessage(ctx, assistantMsg.ID, "assistant", assistantContent, []float32{}, time.Now()); err != nil {
						finalErrChan <- fmt.Errorf("failed to store assistant message: %w", err)
						return
					}

					// Now create and store the Q&A pair with embedding (for retrieval purposes)
					qaText := fmt.Sprintf("Previously user asked: %s\nYou answered: %s", userMessage, assistantContent)

					// Use chunking helper to handle long Q&A pairs
					if err := p.chunkAndStoreQAPair(ctx, chat, qaText); err != nil {
						finalErrChan <- fmt.Errorf("failed to store Q&A pair: %w", err)
						return
					}

					return
				}
				fullResponse.WriteString(token)
				responseChan <- token

			case err := <-errChan:
				if err != nil {
					finalErrChan <- err
					return
				}

			case <-ctx.Done():
				finalErrChan <- ctx.Err()
				return
			}
		}
	}()

	return responseChan, finalErrChan, nil
}

// findMentionedFiles checks if the user message mentions any of the loaded document filenames
// Returns all mentioned files, not just the first one
func findMentionedFiles(userMessage string, docs []vector.Document) []string {
	lowerMessage := strings.ToLower(userMessage)
	var mentionedFiles []string
	seenPaths := make(map[string]bool) // Prevent duplicates

	for _, doc := range docs {
		// Check both full path and just filename
		fileName := filepath.Base(doc.FilePath)
		normalizedPath := filepath.Clean(doc.FilePath)

		if strings.Contains(lowerMessage, strings.ToLower(fileName)) {
			if !seenPaths[normalizedPath] {
				mentionedFiles = append(mentionedFiles, normalizedPath)
				seenPaths[normalizedPath] = true
			}
		} else if strings.Contains(lowerMessage, strings.ToLower(doc.FilePath)) {
			if !seenPaths[normalizedPath] {
				mentionedFiles = append(mentionedFiles, normalizedPath)
				seenPaths[normalizedPath] = true
			}
		}
	}

	return mentionedFiles
}

// filterChunksByFiles returns only chunks from the specified file paths
func filterChunksByFiles(chunks []vector.DocumentChunk, filePaths []string) []vector.DocumentChunk {
	var filtered []vector.DocumentChunk

	// Normalize all search paths for comparison
	normalizedSearchPaths := make(map[string]bool)
	for _, filePath := range filePaths {
		normalizedSearchPaths[strings.ToLower(filepath.Clean(filePath))] = true
	}

	for _, chunk := range chunks {
		// Normalize stored path for comparison (case-insensitive on Windows)
		normalizedChunkPath := strings.ToLower(filepath.Clean(chunk.FilePath))
		if normalizedSearchPaths[normalizedChunkPath] {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

// getAllChunksFromFiles retrieves all chunks for multiple specified files
func getAllChunksFromFiles(store *vector.BadgerStore, ctx context.Context, filePaths []string) []vector.DocumentChunk {
	// Get all documents to find matching document IDs
	docs, err := store.GetDocuments(ctx)
	if err != nil {
		logging.Error("Failed to get documents: %v", err)
		return []vector.DocumentChunk{}
	}

	// Build map of normalized paths to document IDs
	normalizedSearchPaths := make(map[string]bool)
	for _, filePath := range filePaths {
		normalizedSearchPaths[strings.ToLower(filepath.Clean(filePath))] = true
	}
	logging.Debug("Searching for documents with normalized paths: %v", filePaths)

	var targetDocIDs []string
	for _, doc := range docs {
		normalizedDocPath := strings.ToLower(filepath.Clean(doc.FilePath))
		if normalizedSearchPaths[normalizedDocPath] {
			targetDocIDs = append(targetDocIDs, doc.ID)
			logging.Debug("Found matching document ID: %s for path %s", doc.ID, doc.FilePath)
		}
	}

	if len(targetDocIDs) == 0 {
		logging.Error("No documents found for paths: %v", filePaths)
		logging.Error("Available documents: %d", len(docs))
		for i, doc := range docs {
			logging.Error("  [%d] %s", i, doc.FilePath)
		}
		return []vector.DocumentChunk{}
	}

	logging.Info("Found %d document IDs for %d requested paths", len(targetDocIDs), len(filePaths))

	// Create a map for fast lookup
	targetDocIDMap := make(map[string]bool)
	for _, id := range targetDocIDs {
		targetDocIDMap[id] = true
	}

	// Search with a dummy embedding to get all chunks, then filter by document IDs
	dummyEmbedding := make([]float32, 768)
	_, allChunks, err := store.SearchSimilarWithChunks(ctx, dummyEmbedding, 200)
	if err != nil {
		logging.Error("Failed to search chunks: %v", err)
		return []vector.DocumentChunk{}
	}

	var filtered []vector.DocumentChunk
	for _, chunk := range allChunks {
		if targetDocIDMap[chunk.DocumentID] {
			filtered = append(filtered, chunk)
		}
	}

	// Sort by document ID first, then by chunk index to maintain order
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].DocumentID != filtered[j].DocumentID {
			return filtered[i].DocumentID < filtered[j].DocumentID
		}
		return filtered[i].ChunkIndex < filtered[j].ChunkIndex
	})

	logging.Debug("Retrieved %d chunks for %d documents", len(filtered), len(targetDocIDs))
	return filtered
}
