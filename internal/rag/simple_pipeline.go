package rag

import (
	"context"
	"fmt"
	"time"

	"rag-terminal/internal/models"
	"rag-terminal/internal/nexa"
	"rag-terminal/internal/vector"
)

// SimplePipeline implements Pipeline with simple conversation capabilities (no documents)
type SimplePipeline struct {
	*basePipeline
}

// ProcessUserMessage implements the Pipeline interface for SimplePipeline with basic conversation
func (p *SimplePipeline) ProcessUserMessage(
	ctx context.Context,
	chat *vector.Chat,
	llmModel, embedModel string,
	userMessage string,
) (<-chan string, <-chan error, error) {
	// Step 1: Generate embedding for user message
	embeddings, err := p.nexaClient.GenerateEmbeddings(ctx, embedModel, []string{userMessage}, &p.config.EmbeddingDimensions)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate user message embedding: %w", err)
	}
	userEmbedding := embeddings[0]

	// Step 2: Store user message WITHOUT embedding (will be embedded as Q&A pair later)
	userMsg := models.NewMessage(chat.ID, "user", userMessage)
	if err := p.vectorStore.StoreMessage(ctx, userMsg.ID, "user", userMessage, []float32{}, time.Now()); err != nil {
		return nil, nil, fmt.Errorf("failed to store user message: %w", err)
	}

	// Step 3: Search for similar Q&A pairs (conversation history only, no documents)
	retrievalTopK := chat.TopK
	if chat.UseReranking {
		retrievalTopK = chat.TopK * 2
	}

	contextMessages, err := p.vectorStore.SearchSimilar(ctx, userEmbedding, retrievalTopK)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to search similar messages: %w", err)
	}

	// Step 4: Optional LLM-based reranking
	if chat.UseReranking && len(contextMessages) > 0 {
		reranked, err := p.rerankMessagesWithLLM(ctx, llmModel, userMessage, contextMessages, chat.TopK)
		if err == nil {
			contextMessages = reranked
		} else {
			if len(contextMessages) > chat.TopK {
				contextMessages = contextMessages[:chat.TopK]
			}
		}
	} else {
		if len(contextMessages) > chat.TopK {
			contextMessages = contextMessages[:chat.TopK]
		}
	}

	// Step 5: Build simple prompt with conversation context and user profile
	prompt := p.buildPromptWithContext(ctx, chat.ID, chat.SystemPrompt, contextMessages, userMessage)

	// Step 6: Call chat completion
	req := nexa.ChatCompletionRequest{
		Model: llmModel,
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

	// Step 7: Collect response and store
	responseChan := make(chan string, 10)
	finalErrChan := make(chan error, 1)

	go func() {
		defer close(responseChan)
		defer close(finalErrChan)

		// Use helper to collect stream and store completion pair with fact extraction
		err := p.collectStreamedResponse(ctx, streamChan, errChan, responseChan, func(fullResponse string) error {
			return p.storeCompletionPairWithExtraction(ctx, chat, embedModel, userMessage, fullResponse)
		})

		if err != nil {
			finalErrChan <- err
		}
	}()

	return responseChan, finalErrChan, nil
}
