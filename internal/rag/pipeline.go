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

// ProcessUserMessage orchestrates the RAG flow
func (p *Pipeline) ProcessUserMessage(
	ctx context.Context,
	chat *vector.Chat,
	userMessage string,
) (<-chan string, <-chan error, error) {
	// Step 1: Generate embedding for user message
	embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, []string{userMessage})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate user message embedding: %w", err)
	}
	userEmbedding := embeddings[0]

	// Step 2: Store user message
	userMsg := models.NewMessage(chat.ID, "user", userMessage)
	if err := p.vectorStore.StoreMessage(ctx, userMsg.ID, "user", userMessage, userEmbedding, time.Now()); err != nil {
		return nil, nil, fmt.Errorf("failed to store user message: %w", err)
	}

	// Step 3: Search for similar messages
	retrievalTopK := chat.TopK * 2 // Retrieve more for reranking
	if !chat.UseReranking {
		retrievalTopK = chat.TopK
	}

	similarMessages, err := p.vectorStore.SearchSimilar(ctx, userEmbedding, retrievalTopK)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to search similar messages: %w", err)
	}

	// Step 4: Optional LLM-based reranking
	var contextMessages []vector.Message
	if chat.UseReranking && len(similarMessages) > 0 {
		contextMessages, err = p.rerankMessagesWithLLM(ctx, chat.LLMModel, userMessage, similarMessages, chat.TopK)
		if err != nil {
			// Fall back to similarity-based ranking if LLM reranking fails
			contextMessages = similarMessages
			if len(contextMessages) > chat.TopK {
				contextMessages = contextMessages[:chat.TopK]
			}
		}
	} else {
		contextMessages = similarMessages
		if len(contextMessages) > chat.TopK {
			contextMessages = contextMessages[:chat.TopK]
		}
	}

	// Step 5: Build prompt with context
	prompt := p.buildPromptWithContext(chat.SystemPrompt, contextMessages, userMessage)

	// Step 6: Call chat completion
	req := nexa.ChatCompletionRequest{
		Model: chat.LLMModel,
		Messages: []nexa.ChatMessage{
			{Role: "system", Content: chat.SystemPrompt},
			{Role: "user", Content: prompt},
		},
		Temperature: chat.Temperature,
		MaxTokens:   chat.MaxTokens,
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
					// Stream finished, store assistant message WITHOUT embedding initially
					// to avoid blocking on model switching
					assistantMsg := models.NewMessage(chat.ID, "assistant", fullResponse.String())

					// Store with empty embedding first
					emptyEmbedding := make([]float32, 0)
					if err := p.vectorStore.StoreMessage(ctx, assistantMsg.ID, "assistant", fullResponse.String(), emptyEmbedding, time.Now()); err != nil {
						finalErrChan <- fmt.Errorf("failed to store assistant message: %w", err)
						return
					}

					// Capture chat context BEFORE launching background goroutine
					// This prevents race conditions when user switches chats
					capturedChatID := chat.ID
					capturedMessageID := assistantMsg.ID
					capturedContent := fullResponse.String()
					capturedEmbedModel := chat.EmbedModel

					// Generate embedding asynchronously in background
					// This allows the UI to continue while embedding happens
					go func() {
						// Give SDK time to unload LLM before requesting embedder
						time.Sleep(500 * time.Millisecond)

						embeddings, err := p.nexaClient.GenerateEmbeddings(context.Background(), capturedEmbedModel, []string{capturedContent})
						if err != nil {
							// Log error but don't fail - message is already stored
							return
						}

						// Use StoreMessageToChat to write to the SPECIFIC chat without changing current context
						// This ensures the message goes to the correct chat even if user has switched chats
						if badgerStore, ok := p.vectorStore.(*vector.BadgerStore); ok {
							badgerStore.StoreMessageToChat(context.Background(), capturedChatID, capturedMessageID, "assistant", capturedContent, embeddings[0], time.Now())
						}
					}()

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

	if len(contextMessages) > 0 {
		builder.WriteString("Context from previous conversations (all relevant):\n\n")
		for i, msg := range contextMessages {
			builder.WriteString(fmt.Sprintf("%d. [%s]: %s\n", i+1, msg.Role, msg.Content))
		}
		builder.WriteString("\nUse information from all conversations above to answer.\n\n")
	}

	builder.WriteString("Current message: ")
	builder.WriteString(userMessage)

	return builder.String()
}

func (p *Pipeline) buildPromptWithContextAndDocuments(systemPrompt string, contextMessages []vector.Message, contextChunks []vector.DocumentChunk, userMessage string) string {
	var builder strings.Builder

	// Add document chunks first (more specific context)
	if len(contextChunks) > 0 {
		builder.WriteString("Relevant document excerpts:\n\n")
		for i, chunk := range contextChunks {
			builder.WriteString(fmt.Sprintf("%d. [doc: %s]\n%s\n\n", i+1, chunk.FilePath, chunk.Content))
		}
		builder.WriteString("---\n\n")
	}

	// Add conversation context
	if len(contextMessages) > 0 {
		builder.WriteString("Context from previous conversations:\n\n")
		for i, msg := range contextMessages {
			builder.WriteString(fmt.Sprintf("%d. [%s]: %s\n", i+1, msg.Role, msg.Content))
		}
		builder.WriteString("\n---\n\n")
	}

	builder.WriteString("Current message: ")
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
	if len(contextMessages) > 0 {
		builder.WriteString("# Previous Context\n")

		historyCharsRemaining := budget.HistoryBudget * CharsPerToken

		for _, msg := range contextMessages {
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
		builder.WriteString("\n")
	}

	// Current query (full detail - not budget-limited as it's essential)
	builder.WriteString("# Current Query\n")
	builder.WriteString(userMessage)

	return builder.String()
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

			// Store user query as message
			userMsg := models.NewMessage(chat.ID, "user", query)
			if err := p.vectorStore.StoreMessage(ctx, userMsg.ID, "user", query, queryEmbedding, time.Now()); err != nil {
				logging.Error("Failed to store user query: %v", err)
				errorChan <- fmt.Errorf("failed to store user query: %w", err)
				return
			}
			logging.Debug("Stored user query message")

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
						// Store assistant response
						assistantMsg := models.NewMessage(chat.ID, "assistant", fullResponse.String())

						// Store with empty embedding first
						emptyEmbedding := make([]float32, 0)
						if err := p.vectorStore.StoreMessage(ctx, assistantMsg.ID, "assistant", fullResponse.String(), emptyEmbedding, time.Now()); err != nil {
							errorChan <- fmt.Errorf("failed to store assistant message: %w", err)
							return
						}

						// Generate embedding asynchronously
						capturedChatID := chat.ID
						capturedMessageID := assistantMsg.ID
						capturedContent := fullResponse.String()
						capturedEmbedModel := chat.EmbedModel

						go func() {
							time.Sleep(500 * time.Millisecond)
							embeddings, err := p.nexaClient.GenerateEmbeddings(context.Background(), capturedEmbedModel, []string{capturedContent})
							if err != nil {
								return
							}
							if badgerStore, ok := p.vectorStore.(*vector.BadgerStore); ok {
								badgerStore.StoreMessageToChat(context.Background(), capturedChatID, capturedMessageID, "assistant", capturedContent, embeddings[0], time.Now())
							}
						}()

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

			// Store user query as message
			userMsg := models.NewMessage(chat.ID, "user", query)
			if err := p.vectorStore.StoreMessage(ctx, userMsg.ID, "user", query, queryEmbedding, time.Now()); err != nil {
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
						// Store assistant response
						assistantMsg := models.NewMessage(chat.ID, "assistant", fullResponse.String())
						if err := p.vectorStore.StoreMessage(ctx, assistantMsg.ID, "assistant", fullResponse.String(), []float32{}, time.Now()); err != nil {
							logging.Error("Failed to store assistant message: %v", err)
						}

						// Update embedding asynchronously
						capturedChatID := chat.ID
						capturedMessageID := assistantMsg.ID
						capturedContent := fullResponse.String()
						capturedEmbedModel := chat.EmbedModel

						go func() {
							time.Sleep(500 * time.Millisecond)
							embeddings, err := p.nexaClient.GenerateEmbeddings(context.Background(), capturedEmbedModel, []string{capturedContent})
							if err != nil {
								return
							}
							if badgerStore, ok := p.vectorStore.(*vector.BadgerStore); ok {
								badgerStore.StoreMessageToChat(context.Background(), capturedChatID, capturedMessageID, "assistant", capturedContent, embeddings[0], time.Now())
							}
						}()

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
	// Step 1: Generate embedding for user message
	embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, []string{userMessage})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate user message embedding: %w", err)
	}
	userEmbedding := embeddings[0]

	// Step 2: Store user message
	userMsg := models.NewMessage(chat.ID, "user", userMessage)
	if err := p.vectorStore.StoreMessage(ctx, userMsg.ID, "user", userMessage, userEmbedding, time.Now()); err != nil {
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
		contextMessages = msgs
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
		contextMessages = msgs
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
					assistantMsg := models.NewMessage(chat.ID, "assistant", fullResponse.String())

					emptyEmbedding := make([]float32, 0)
					if err := p.vectorStore.StoreMessage(ctx, assistantMsg.ID, "assistant", fullResponse.String(), emptyEmbedding, time.Now()); err != nil {
						finalErrChan <- fmt.Errorf("failed to store assistant message: %w", err)
						return
					}

					capturedChatID := chat.ID
					capturedMessageID := assistantMsg.ID
					capturedContent := fullResponse.String()
					capturedEmbedModel := chat.EmbedModel

					go func() {
						time.Sleep(500 * time.Millisecond)
						embeddings, err := p.nexaClient.GenerateEmbeddings(context.Background(), capturedEmbedModel, []string{capturedContent})
						if err != nil {
							return
						}
						if badgerStore, ok := p.vectorStore.(*vector.BadgerStore); ok {
							badgerStore.StoreMessageToChat(context.Background(), capturedChatID, capturedMessageID, "assistant", capturedContent, embeddings[0], time.Now())
						}
					}()

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
