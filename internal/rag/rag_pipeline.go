package rag

import (
	"context"
	"fmt"
	"time"

	"rag-terminal/internal/document"
	"rag-terminal/internal/logging"
	"rag-terminal/internal/models"
	"rag-terminal/internal/nexa"
	"rag-terminal/internal/vector"
)

// RAGPipeline extends basePipeline with RAG processing (document retrieval + generation)
type RAGPipeline struct {
	*basePipeline
}

// ProcessUserMessage implements Pipeline interface with full RAG capabilities
func (p *RAGPipeline) ProcessUserMessage(
	ctx context.Context,
	chat *vector.Chat,
	userMessage string,
) (<-chan string, <-chan error, error) {
	// Step 1: Generate embedding for user message (for retrieval purposes)
	embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, chat.EmbedModel, []string{userMessage}, &p.config.EmbeddingDimensions)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate user message embedding: %w", err)
	}
	userEmbedding := embeddings[0]

	// Step 2: Store user message WITHOUT embedding (will be embedded as Q&A pair later)
	userMsg := models.NewMessage(chat.ID, "user", userMessage)
	if err := p.vectorStore.StoreMessage(ctx, userMsg.ID, "user", userMessage, []float32{}, time.Now()); err != nil {
		return nil, nil, fmt.Errorf("failed to store user message: %w", err)
	}

	// Step 3: Search for similar content (Q&A pairs and document chunks only)
	retrievalTopK := chat.TopK * 2
	if !chat.UseReranking {
		retrievalTopK = chat.TopK
	}

	var contextMessages []vector.Message
	var contextChunks []vector.DocumentChunk

	badgerStore, ok := p.vectorStore.(*vector.BadgerStore)
	if !ok {
		return nil, nil, fmt.Errorf("vector store is not BadgerStore")
	}

	// Search for similar Q&A pairs and document chunks (not individual user/assistant messages)
	contextMessages, contextChunks, err = badgerStore.SearchSimilarContextAndChunks(ctx, userEmbedding, retrievalTopK)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to search similar content: %w", err)
	}

	// Check if user mentioned specific filenames - prioritize chunks from those files
	allDocs, _ := badgerStore.GetDocuments(ctx)
	mentionedFiles := p.documentManager.FindMentionedFiles(userMessage, allDocs)
	userMentionedFile := len(mentionedFiles) > 0

	if userMentionedFile {
		logging.Info("User mentioned specific files: %v - filtering chunks", mentionedFiles)
		filteredChunks := p.documentManager.FilterChunksByFiles(contextChunks, mentionedFiles)
		if len(filteredChunks) == 0 {
			logging.Info("No similar chunks from %v, fetching all chunks from files", mentionedFiles)
			filteredChunks = p.documentManager.GetAllChunksFromFiles(ctx, mentionedFiles)
		}
		contextChunks = filteredChunks
		logging.Debug("After filename filtering: %d chunks from %d files", len(contextChunks), len(mentionedFiles))

		// Apply smart chunk prioritization for code files
		if len(contextChunks) > 0 && document.IsCodeFile(contextChunks[0].FilePath) {
			prioritizer := &CodeChunkPrioritizer{}
			maxCodeChunks := chat.TopK / 2
			if maxCodeChunks < 3 {
				maxCodeChunks = 3 // Minimum for header + some context
			}
			contextChunks = prioritizer.PrioritizeCodeChunks(contextChunks, maxCodeChunks)
			logging.Info("Applied smart prioritization: %d code chunks selected", len(contextChunks))
		}
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

	// Limit document chunks (skip if we already applied smart prioritization for code)
	appliedSmartPrioritization := userMentionedFile && len(contextChunks) > 0 && document.IsCodeFile(contextChunks[0].FilePath)

	if !appliedSmartPrioritization && len(contextChunks) > chat.TopK/2 {
		contextChunks = contextChunks[:chat.TopK/2]
	}

	// Step 5: Build prompt with context
	var prompt string
	if len(contextChunks) > 0 {
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

		// Use helper to collect stream and store completion pair
		err := p.collectStreamedResponse(ctx, streamChan, errChan, responseChan, func(fullResponse string) error {
			return p.storeCompletionPair(ctx, chat, userMessage, fullResponse)
		})

		if err != nil {
			finalErrChan <- err
		}
	}()

	return responseChan, finalErrChan, nil
}
