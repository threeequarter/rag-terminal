package rag

import (
	"context"
	"time"

	"rag-terminal/internal/config"
	"rag-terminal/internal/document"
	"rag-terminal/internal/logging"
	"rag-terminal/internal/models"
	"rag-terminal/internal/nexa"
	"rag-terminal/internal/vector"
)

// basePipeline coordinates different pipeline implementations and helper components
// following the Single Responsibility Principle by delegating specific tasks to focused components
type basePipeline struct {
	nexaClient       *nexa.Client
	vectorStore      vector.VectorStore
	config           *config.Config
	documentManager  *document.DocumentManager
	profileExtractor *ProfileExtractor
	simplePipeline   *SimplePipeline
	ragPipeline      *RAGPipeline

	// Component helpers - each handles a specific responsibility
	promptBuilder     *PromptBuilder
	messageProcessor  *MessageProcessor
	responseProcessor *ResponseProcessor
	documentProcessor *DocumentProcessor
}

// NewPipeline creates a new pipeline that delegates between simple and RAG modes
func NewPipeline(nexaClient *nexa.Client, vectorStore vector.VectorStore) *basePipeline {
	// Load config, fallback to default if loading fails
	cfg, err := config.Load()
	if err != nil {
		logging.Info("Failed to load config, using defaults: %v", err)
		cfg = config.DefaultConfig()
	}

	profileExtractor := NewProfileExtractor(nexaClient, vectorStore)

	base := &basePipeline{
		nexaClient:       nexaClient,
		vectorStore:      vectorStore,
		config:           cfg,
		documentManager:  document.NewDocumentManager(nexaClient, vectorStore, cfg),
		profileExtractor: profileExtractor,

		// Initialize component helpers with appropriate dependencies
		promptBuilder:     NewPromptBuilder(vectorStore, cfg),
		messageProcessor:  NewMessageProcessor(vectorStore, nexaClient, cfg),
		responseProcessor: NewResponseProcessor(profileExtractor),
		documentProcessor: NewDocumentProcessor(vectorStore, nexaClient),
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

// ==== Message Processing Delegates ====

// groupAndMergeChunkedMessages delegates to messageProcessor
func (p *basePipeline) groupAndMergeChunkedMessages(ctx context.Context, messages []vector.Message) []vector.Message {
	return p.messageProcessor.GroupAndMergeChunkedMessages(ctx, messages)
}

// retrieveAllChunks delegates to messageProcessor (kept for backward compatibility)
func (p *basePipeline) retrieveAllChunks(ctx context.Context, baseID string) ([]vector.Message, error) {
	return p.messageProcessor.retrieveAllChunks(ctx, baseID)
}

// rerankMessagesWithLLM delegates to messageProcessor
func (p *basePipeline) rerankMessagesWithLLM(ctx context.Context, llmModel, query string, messages []vector.Message, topK int) ([]vector.Message, error) {
	return p.messageProcessor.RerankMessagesWithLLM(ctx, llmModel, query, messages, topK)
}

// ==== Prompt Building Delegates ====

// buildProfileContext delegates to promptBuilder
func (p *basePipeline) buildProfileContext(profile *vector.UserProfile) string {
	return p.promptBuilder.buildProfileContext(profile)
}

// buildPromptWithContext delegates to promptBuilder
func (p *basePipeline) buildPromptWithContext(ctx context.Context, chatID string, systemPrompt string, contextMessages []vector.Message, userMessage string) string {
	return p.promptBuilder.BuildPromptWithContext(ctx, chatID, systemPrompt, contextMessages, userMessage)
}

// buildPromptWithContextAndDocuments delegates to promptBuilder
func (p *basePipeline) buildPromptWithContextAndDocuments(systemPrompt string, contextMessages []vector.Message, contextChunks []vector.DocumentChunk, userMessage string) string {
	return p.promptBuilder.BuildPromptWithDocuments(systemPrompt, contextMessages, contextChunks, userMessage)
}

// buildPromptWithContextAndDocumentsAndFileList delegates to promptBuilder
func (p *basePipeline) buildPromptWithContextAndDocumentsAndFileList(ctx context.Context, chat *vector.Chat, contextMessages []vector.Message, contextChunks []vector.DocumentChunk, allDocs []vector.Document, userMessage string) string {
	return p.promptBuilder.BuildPromptWithContextAndDocumentsAndFileList(ctx, chat, contextMessages, contextChunks, allDocs, userMessage)
}

// ==== Response Processing Delegates ====

// collectStreamedResponse delegates to responseProcessor
func (p *basePipeline) collectStreamedResponse(
	ctx context.Context,
	streamChan <-chan string,
	errChan <-chan error,
	responseChan chan<- string,
	onComplete func(fullResponse string) error,
) error {
	return p.responseProcessor.CollectStreamedResponse(ctx, streamChan, errChan, responseChan, onComplete)
}

// storeCompletionPair stores assistant message and Q&A pair with embedding
func (p *basePipeline) storeCompletionPair(
	ctx context.Context,
	chat *vector.Chat,
	embedModel string,
	userQuery string,
	assistantResponse string,
) error {
	// This method intentionally kept in basePipeline as it coordinates multiple components
	// For now, we'll keep the original implementation as it's used by SimplePipeline/RAGPipeline
	// A future refactoring could extract this further into a coordinator pattern
	return storeCompletionPairImpl(ctx, p.vectorStore, p.nexaClient, p.config, chat, embedModel, userQuery, assistantResponse)
}

// storeCompletionPairWithExtraction stores completion pair and asynchronously extracts facts
func (p *basePipeline) storeCompletionPairWithExtraction(
	ctx context.Context,
	chat *vector.Chat,
	llmModel string,
	embedModel string,
	userQuery string,
	assistantResponse string,
) error {
	if err := p.storeCompletionPair(ctx, chat, embedModel, userQuery, assistantResponse); err != nil {
		return err
	}
	// Start async fact extraction (non-blocking)
	p.responseProcessor.StartAsyncFactExtraction(chat.ID, llmModel, userQuery, assistantResponse)
	return nil
}

// ==== Document Processing Delegates ====

// processDocument delegates to documentProcessor
func (p *basePipeline) processDocument(
	ctx context.Context,
	chat *vector.Chat,
	embedModel string,
	doc vector.Document,
	loader *document.Loader,
	responseChan chan<- string,
) error {
	return p.documentProcessor.ProcessDocument(ctx, chat, embedModel, doc, loader, responseChan)
}

// chunkAndStoreQAPair chunks a Q&A pair if needed and stores with embeddings
func (p *basePipeline) chunkAndStoreQAPair(ctx context.Context, chat *vector.Chat, embedModel string, qaText string) error {
	// This method intentionally kept in basePipeline as it's called from storeCompletionPair
	// For now, we'll keep the original implementation
	return chunkAndStoreQAPairImpl(ctx, p.vectorStore, p.nexaClient, p.config, chat, embedModel, qaText)
}

// storeCompletionPairImpl contains the original implementation logic
func storeCompletionPairImpl(
	ctx context.Context,
	vectorStore vector.VectorStore,
	nexaClient *nexa.Client,
	cfg *config.Config,
	chat *vector.Chat,
	embedModel string,
	userQuery string,
	assistantResponse string,
) error {
	// Store assistant message WITHOUT embedding (for display purposes)
	assistantMsg := models.NewMessage(chat.ID, "assistant", assistantResponse)
	if err := vectorStore.StoreMessage(ctx, assistantMsg.ID, "assistant", assistantResponse, []float32{}, time.Now()); err != nil {
		logging.Error("Failed to store assistant message: %v", err)
		return err
	}

	// Create and store the Q&A pair with embedding (for retrieval purposes)
	qaText := "Previously user asked: " + userQuery + "\nAssistant answered: " + assistantResponse
	return chunkAndStoreQAPairImpl(ctx, vectorStore, nexaClient, cfg, chat, embedModel, qaText)
}

// chunkAndStoreQAPairImpl contains the original implementation logic for Q&A pair chunking
func chunkAndStoreQAPairImpl(
	ctx context.Context,
	vectorStore vector.VectorStore,
	nexaClient *nexa.Client,
	cfg *config.Config,
	chat *vector.Chat,
	embedModel string,
	qaText string,
) error {
	const maxTokensPerChunk = 300
	estimatedTokens := EstimateTokens(qaText)

	// If small enough, store as single context message
	if estimatedTokens <= maxTokensPerChunk {
		qaEmbeddings, err := nexaClient.GenerateEmbeddings(ctx, embedModel, []string{qaText}, &cfg.EmbeddingDimensions)
		if err != nil {
			return err
		}

		qaPairMsg := models.NewMessage(chat.ID, "context", qaText)
		if err := vectorStore.StoreMessage(ctx, qaPairMsg.ID, "context", qaText, qaEmbeddings[0], time.Now()); err != nil {
			return err
		}
		return nil
	}

	logging.Info("Q&A pair is long (%d tokens estimated), chunking into smaller pieces", estimatedTokens)

	// Create chunker
	chunker := document.NewChunker()
	chunker.ChunkSize = maxTokensPerChunk * CharsPerToken
	chunker.ChunkOverlap = 50

	chunks := chunker.ChunkDocument(qaText)
	logging.Info("Split Q&A pair into %d chunks", len(chunks))

	// Generate base ID for all chunks
	baseMsg := models.NewMessage(chat.ID, "context", "")
	baseID := baseMsg.ID

	// Prepare all chunk contents for batch embedding
	chunkContents := make([]string, 0, len(chunks))

	for i, chunk := range chunks {
		// Prefix each chunk with abbreviated context so it's still meaningful
		var chunkContent string
		if i > 0 || len(chunks) > 1 {
			chunkContent = "[Part " + string(rune('0'+i+1)) + "/" + string(rune('0'+len(chunks))) + "] " + chunk.Content
		} else {
			chunkContent = chunk.Content
		}

		const maxSafeTokens = 800
		const maxSafeChars = maxSafeTokens * CharsPerToken

		if len(chunkContent) > maxSafeChars {
			logging.Info("Chunk %d exceeds safe size (%d chars), truncating to %d chars",
				i, len(chunkContent), maxSafeChars)
			chunkContent = chunkContent[:maxSafeChars] + "..."
		}

		chunkContents = append(chunkContents, chunkContent)
	}

	// Generate embeddings for all chunks in batch
	embeddings, err := nexaClient.GenerateEmbeddings(ctx, embedModel, chunkContents, &cfg.EmbeddingDimensions)
	if err != nil {
		return err
	}

	// Store each chunk as separate context message with chunk ID
	for i, chunkContent := range chunkContents {
		chunkID := baseID + "-chunk-" + string(rune('0'+i))
		if err := vectorStore.StoreMessage(ctx, chunkID, "context", chunkContent, embeddings[i], time.Now()); err != nil {
			return err
		}
	}

	logging.Info("Successfully stored %d Q&A chunks", len(chunkContents))
	return nil
}
