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

// basePipeline provides shared helpers and delegates to appropriate pipeline implementation
type basePipeline struct {
	nexaClient       *nexa.Client
	vectorStore      vector.VectorStore
	config           *config.Config
	documentManager  *document.DocumentManager
	profileExtractor *ProfileExtractor
	simplePipeline   *SimplePipeline
	ragPipeline      *RAGPipeline
}

// NewPipeline creates a new pipeline that delegates between simple and RAG modes
func NewPipeline(nexaClient *nexa.Client, vectorStore vector.VectorStore) *basePipeline {
	// Load config, fallback to default if loading fails
	cfg, err := config.Load()
	if err != nil {
		logging.Info("Failed to load config, using defaults: %v", err)
		cfg = config.DefaultConfig()
	}

	base := &basePipeline{
		nexaClient:       nexaClient,
		vectorStore:      vectorStore,
		config:           cfg,
		documentManager:  document.NewDocumentManager(nexaClient, vectorStore, cfg),
		profileExtractor: NewProfileExtractor(nexaClient, vectorStore),
	}

	// Initialize both pipeline implementations with shared base
	base.simplePipeline = &SimplePipeline{basePipeline: base}
	base.ragPipeline = &RAGPipeline{basePipeline: base}

	return base
}

// ProcessUserMessage implements Pipeline interface with delegation
// Delegates to SimplePipeline if no documents exist, otherwise uses RAGPipeline
func (p *basePipeline) ProcessUserMessage(
	ctx context.Context,
	chat *vector.Chat,
	llmModel, embedModel string,
	userMessage string,
) (<-chan string, <-chan error, error) {
	hasDocuments := chat.FileCount > 0

	if !hasDocuments {
		logging.Info("Using simple conversation mode (no documents loaded)")
		return p.simplePipeline.ProcessUserMessage(ctx, chat, llmModel, embedModel, userMessage)
	}

	logging.Info("Using RAG mode with document retrieval")
	return p.ragPipeline.ProcessUserMessage(ctx, chat, llmModel, embedModel, userMessage)
}

// GetDocumentManager returns the document manager for document loading operations
func (p *basePipeline) GetDocumentManager() *document.DocumentManager {
	return p.documentManager
}

// groupAndMergeChunkedMessages groups message chunks and merges them into complete messages
// If any chunk of a message is in the results, all chunks are retrieved and merged
func (p *basePipeline) groupAndMergeChunkedMessages(ctx context.Context, messages []vector.Message) []vector.Message {
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
func (p *basePipeline) retrieveAllChunks(ctx context.Context, baseID string) ([]vector.Message, error) {
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

func (p *basePipeline) rerankMessagesWithLLM(ctx context.Context, llmModel, query string, messages []vector.Message, topK int) ([]vector.Message, error) {
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

// buildProfileContext formats user facts from the profile for inclusion in prompts
func (p *basePipeline) buildProfileContext(profile *vector.UserProfile) string {
	if len(profile.Facts) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("---\nKnown information about the user:\n")

	// Group facts by category (using key prefix before colon)
	categories := map[string][]vector.ProfileFact{
		"identity":     {},
		"professional": {},
		"preference":   {},
		"project":      {},
		"personal":     {},
	}

	for _, fact := range profile.Facts {
		// Only include high-confidence facts (>0.6)
		if fact.Confidence < 0.6 {
			continue
		}

		// Extract category from key (before colon if present)
		category := "personal" // default
		keyParts := strings.Split(fact.Key, ":")
		if len(keyParts) > 0 && keyParts[0] != "" {
			potentialCategory := keyParts[0]
			if _, exists := categories[potentialCategory]; exists {
				category = potentialCategory
			}
		}

		categories[category] = append(categories[category], fact)
	}

	// Format by category in a consistent order
	categoryOrder := []string{"identity", "professional", "preference", "project", "personal"}
	for _, category := range categoryOrder {
		facts := categories[category]
		if len(facts) == 0 {
			continue
		}

		// Capitalize category name for display
		displayCategory := strings.ToUpper(string([]rune(category)[0])) + category[1:]
		sb.WriteString(fmt.Sprintf("\n%s:\n", displayCategory))

		for _, fact := range facts {
			// Remove category prefix from key for display
			displayKey := strings.TrimPrefix(fact.Key, category+":")
			if displayKey == "" {
				displayKey = category
			}

			sb.WriteString(fmt.Sprintf("- %s: %s\n", displayKey, fact.Value))
		}
	}

	return sb.String()
}

func (p *basePipeline) buildPromptWithContext(ctx context.Context, chatID string, systemPrompt string, contextMessages []vector.Message, userMessage string) string {
	var builder strings.Builder

	// Add user profile context if available
	profile, err := p.vectorStore.GetUserProfile(ctx, chatID)
	if err != nil {
		logging.Debug("Failed to retrieve user profile: %v", err)
	} else if profile != nil && len(profile.Facts) > 0 {
		profileContext := p.buildProfileContext(profile)
		if profileContext != "" {
			builder.WriteString("# User Profile\n")
			builder.WriteString(profileContext)
			builder.WriteString("\n\n")
		}
	}

	// Filter out messages with identical content to current message (to avoid treating first message as context)
	var relevantContext []vector.Message
	for _, msg := range contextMessages {
		if msg.Content != userMessage {
			relevantContext = append(relevantContext, msg)
		}
	}

	if len(relevantContext) > 0 {
		builder.WriteString("---\nRelevant previous conversation history for reference:\n")
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

func (p *basePipeline) buildPromptWithContextAndDocuments(systemPrompt string, contextMessages []vector.Message, contextChunks []vector.DocumentChunk, userMessage string) string {
	var builder strings.Builder

	// Add document chunks first (most specific context)
	if len(contextChunks) > 0 {
		builder.WriteString("---\nRelevant document excerpts:\n")
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
		builder.WriteString("---\nPrevious conversation history:\n\n")
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

func (p *basePipeline) buildPromptWithContextAndDocumentsAndFileList(ctx context.Context, chat *vector.Chat, contextMessages []vector.Message, contextChunks []vector.DocumentChunk, allDocs []vector.Document, userMessage string) string {
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

	// Detect if we're working with code files
	isCodeFile := false
	if len(contextChunks) > 0 {
		// Check first chunk to determine file type
		isCodeFile = document.IsCodeFile(contextChunks[0].FilePath)
	} else if len(allDocs) > 0 {
		// Check first document
		isCodeFile = document.IsCodeFile(allDocs[0].FilePath)
	}

	// Use appropriate budget configuration
	budget := CalculateTokenBudgetForType(contextWindow, maxTokens, p.config, isCodeFile)

	// Add user profile context if available
	profile, err := p.vectorStore.GetUserProfile(ctx, chat.ID)
	if err != nil {
		logging.Debug("Failed to retrieve user profile: %v", err)
	} else if profile != nil && len(profile.Facts) > 0 {
		profileContext := p.buildProfileContext(profile)
		if profileContext != "" {
			builder.WriteString("# User Profile\n")
			builder.WriteString(profileContext)
			builder.WriteString("\n\n")
		}
	}
	if isCodeFile {
		logging.Info("Using code-optimized token budget (input: %d, excerpts: %d, history: %d, chunks: %d)",
			budget.AvailableInput, budget.ExcerptsBudget, budget.HistoryBudget, budget.ChunksBudget)
	} else {
		logging.Debug("Using default token budget (input: %d, excerpts: %d, history: %d, chunks: %d)",
			budget.AvailableInput, budget.ExcerptsBudget, budget.HistoryBudget, budget.ChunksBudget)
	}

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

			excerpt := extractor.ExtractRelevantExcerptWithPath(chunk.Content, userMessage, maxExcerptSize, chunk.FilePath)
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
func (p *basePipeline) chunkAndStoreQAPair(ctx context.Context, chat *vector.Chat, embedModel string, qaText string) error {

	const maxTokensPerChunk = 300

	estimatedTokens := EstimateTokens(qaText)

	// If small enough, store as single context message
	if estimatedTokens <= maxTokensPerChunk {
		qaEmbeddings, err := p.nexaClient.GenerateEmbeddings(ctx, embedModel, []string{qaText}, &p.config.EmbeddingDimensions)
		if err != nil {
			return fmt.Errorf("failed to generate Q&A pair embedding: %w", err)
		}

		qaPairMsg := models.NewMessage(chat.ID, "context", qaText)
		if err := p.vectorStore.StoreMessage(ctx, qaPairMsg.ID, "context", qaText, qaEmbeddings[0], time.Now()); err != nil {
			return fmt.Errorf("failed to store Q&A pair: %w", err)
		}

		return nil
	}

	logging.Info("Q&A pair is long (%d tokens estimated), chunking into smaller pieces", estimatedTokens)

	chunker := document.NewChunker()
	chunker.ChunkSize = maxTokensPerChunk * CharsPerToken
	chunker.ChunkOverlap = 50

	chunks := chunker.ChunkDocument(qaText)
	logging.Info("Split Q&A pair into %d chunks", len(chunks))

	// Generate base ID for all chunks
	baseID := models.NewMessage(chat.ID, "context", "").ID

	// Prepare all chunk contents for batch embedding
	chunkContents := make([]string, 0, len(chunks))
	validChunkIndices := make([]int, 0, len(chunks))

	for i, chunk := range chunks {
		// Prefix each chunk with abbreviated context so it's still meaningful
		prefix := fmt.Sprintf("[Part %d/%d] ", i+1, len(chunks))
		chunkContent := prefix + chunk.Content

		const maxSafeTokens = 800
		const maxSafeChars = maxSafeTokens * CharsPerToken

		if len(chunkContent) > maxSafeChars {
			logging.Info("Chunk %d exceeds safe size (%d chars), truncating to %d chars",
				i, len(chunkContent), maxSafeChars)
			chunkContent = chunkContent[:maxSafeChars] + "..."
		}

		chunkContents = append(chunkContents, chunkContent)
		validChunkIndices = append(validChunkIndices, i)
	}

	// Generate embeddings for all chunks in batch
	embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, embedModel, chunkContents, &p.config.EmbeddingDimensions)
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

// processDocument handles pipeline for a single document:
// duplicate detection, metadata storage, chunking, embedding, and storage
func (p *basePipeline) processDocument(
	ctx context.Context,
	chat *vector.Chat,
	embedModel string,
	doc vector.Document,
	loader *document.Loader,
	responseChan chan<- string,
) error {
	badgerStore, ok := p.vectorStore.(*vector.BadgerStore)
	if !ok {
		return fmt.Errorf("vector store is not BadgerStore type")
	}

	logging.Debug("Processing document: %s (size=%d, hash=%s)", doc.FileName, doc.FileSize, doc.ContentHash)

	// Check if document with same content hash already exists
	existingDoc, err := badgerStore.FindDocumentByHash(ctx, doc.ContentHash)
	if err == nil && existingDoc != nil {
		logging.Info("Document %s already exists (duplicate of %s), skipping", doc.FileName, existingDoc.FileName)
		if responseChan != nil {
			responseChan <- fmt.Sprintf("Skipped %s (duplicate of %s)\n", doc.FileName, existingDoc.FileName)
		}
		return nil // Not an error, just skipped
	}

	// Store document metadata
	if err := badgerStore.StoreDocument(ctx, &doc); err != nil {
		logging.Error("Failed to store document metadata for %s: %v", doc.FileName, err)
		return fmt.Errorf("failed to store document %s: %w", doc.FileName, err)
	}
	logging.Debug("Stored document metadata for %s", doc.FileName)

	// Get chunks for this document
	chunks, err := loader.GetDocumentChunks(doc.ID, doc.FilePath, chat.ID)
	if err != nil {
		logging.Error("Failed to get chunks for %s: %v", doc.FileName, err)
		return fmt.Errorf("failed to chunk document %s: %w", doc.FileName, err)
	}
	logging.Debug("Created %d chunks for %s", len(chunks), doc.FileName)

	// Prepare chunk contents for batch embedding
	chunkContents := make([]string, len(chunks))
	for i, chunk := range chunks {
		chunkContents[i] = chunk.Content
	}

	// Generate embeddings for all chunks in batch
	logging.Debug("Generating embeddings for %d chunks of %s with dimensions=%d", len(chunks), doc.FileName, p.config.EmbeddingDimensions)
	embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, embedModel, chunkContents, &p.config.EmbeddingDimensions)
	if err != nil {
		logging.Error("Failed to generate embeddings for %s: %v", doc.FileName, err)
		return fmt.Errorf("failed to generate embeddings for %s: %w", doc.FileName, err)
	}
	logging.Debug("Generated %d embeddings for %s (dim=%d)", len(embeddings), doc.FileName, len(embeddings[0]))

	// Store chunks with embeddings
	for i, chunk := range chunks {
		chunk.Embedding = embeddings[i]
		if err := badgerStore.StoreDocumentChunk(ctx, &chunk); err != nil {
			logging.Error("Failed to store chunk %d of %s: %v", i, doc.FileName, err)
			return fmt.Errorf("failed to store chunk %d of %s: %w", i, doc.FileName, err)
		}
	}
	logging.Info("Successfully stored %d chunks for %s", len(chunks), doc.FileName)

	return nil
}

// storeCompletionPair stores both the assistant message and the Q&A pair with embedding
func (p *basePipeline) storeCompletionPair(
	ctx context.Context,
	chat *vector.Chat,
	embedModel string,
	userQuery string,
	assistantResponse string,
) error {
	// First, store the assistant message WITHOUT embedding (for display purposes)
	assistantMsg := models.NewMessage(chat.ID, "assistant", assistantResponse)
	if err := p.vectorStore.StoreMessage(ctx, assistantMsg.ID, "assistant", assistantResponse, []float32{}, time.Now()); err != nil {
		logging.Error("Failed to store assistant message: %v", err)
		return fmt.Errorf("failed to store assistant message: %w", err)
	}

	// Now create and store the Q&A pair with embedding (for retrieval purposes)
	qaText := fmt.Sprintf("Previously user asked: %s\nAssistant answered: %s", userQuery, assistantResponse)

	// Use chunking helper to handle long Q&A pairs
	if err := p.chunkAndStoreQAPair(ctx, chat, embedModel, qaText); err != nil {
		logging.Error("Failed to chunk and store Q&A pair: %v", err)
		return fmt.Errorf("failed to store Q&A pair: %w", err)
	}

	return nil
}

// storeCompletionPairWithExtraction stores the completion pair and asynchronously extracts user facts
func (p *basePipeline) storeCompletionPairWithExtraction(
	ctx context.Context,
	chat *vector.Chat,
	llmModel string,
	embedModel string,
	userQuery string,
	assistantResponse string,
) error {
	// First store the completion pair
	if err := p.storeCompletionPair(ctx, chat, embedModel, userQuery, assistantResponse); err != nil {
		return err
	}

	// Start async fact extraction (non-blocking)
	// This runs in the background and logs errors without failing the main pipeline
	go func() {
		// Wait 2 seconds before processing facts to avoid rate limiting on the API
		time.Sleep(2 * time.Second)

		// Use a short timeout for fact extraction to not block too long
		extractCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := p.profileExtractor.ExtractFacts(extractCtx, chat.ID, llmModel, userQuery, assistantResponse); err != nil {
			logging.Debug("Profile extraction failed (non-blocking): %v", err)
		}
	}()

	return nil
}

// collectStreamedResponse collects tokens from a stream and calls onComplete when done
func (p *basePipeline) collectStreamedResponse(
	ctx context.Context,
	streamChan <-chan string,
	errChan <-chan error,
	responseChan chan<- string,
	onComplete func(fullResponse string) error,
) error {
	var fullResponse strings.Builder

	for {
		select {
		case token, ok := <-streamChan:
			if !ok {
				// Stream complete, call completion handler
				if onComplete != nil {
					if err := onComplete(fullResponse.String()); err != nil {
						return err
					}
				}
				return nil
			}
			fullResponse.WriteString(token)
			if responseChan != nil {
				responseChan <- token
			}

		case err := <-errChan:
			if err != nil {
				return err
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
