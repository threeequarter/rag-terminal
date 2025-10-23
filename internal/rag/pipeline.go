package rag

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"rag-chat/internal/models"
	"rag-chat/internal/nexa"
	"rag-chat/internal/vector"
)

type Pipeline struct {
	nexaClient  *nexa.Client
	vectorStore vector.VectorStore
}

type ChatParams struct {
	Temperature  float64
	MaxTokens    int
	TopK         int
	UseReranking bool
}

func NewPipeline(nexaClient *nexa.Client, vectorStore vector.VectorStore) *Pipeline {
	return &Pipeline{
		nexaClient:  nexaClient,
		vectorStore: vectorStore,
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

	// Step 4: Optional reranking
	var contextMessages []vector.Message
	if chat.UseReranking && chat.RerankModel != "" && len(similarMessages) > 0 {
		contextMessages, err = p.rerankMessages(ctx, chat.RerankModel, userMessage, similarMessages, chat.TopK)
		if err != nil {
			// Fall back to similarity-based ranking if reranking fails
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

					// Generate embedding asynchronously in background
					// This allows the UI to continue while embedding happens
					go func() {
						// Give SDK time to unload LLM before requesting embedder
						time.Sleep(500 * time.Millisecond)

						embeddings, err := p.nexaClient.GenerateEmbeddings(context.Background(), chat.EmbedModel, []string{fullResponse.String()})
						if err != nil {
							// Log error but don't fail - message is already stored
							return
						}

						// Update message with embedding
						p.vectorStore.StoreMessage(context.Background(), assistantMsg.ID, "assistant", fullResponse.String(), embeddings[0], time.Now())
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

func (p *Pipeline) rerankMessages(ctx context.Context, rerankModel, query string, messages []vector.Message, topK int) ([]vector.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}

	// Prepare documents for reranking
	documents := make([]string, len(messages))
	for i, msg := range messages {
		documents[i] = fmt.Sprintf("[%s] %s", msg.Role, msg.Content)
	}

	// Call reranking API
	req := nexa.RerankingRequest{
		Model:           rerankModel,
		Query:           query,
		Documents:       documents,
		BatchSize:       10,
		Normalize:       true,
		NormalizeMethod: "softmax",
	}

	scores, err := p.nexaClient.Rerank(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to rerank messages: %w", err)
	}

	// Create scored message pairs
	type scoredMessage struct {
		message vector.Message
		score   float64
	}

	scored := make([]scoredMessage, len(messages))
	for i, msg := range messages {
		score := 0.0
		if i < len(scores) {
			score = scores[i]
		}
		scored[i] = scoredMessage{message: msg, score: score}
	}

	// Sort by reranking score descending
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
		builder.WriteString("Previous relevant conversations:\n\n")
		for _, msg := range contextMessages {
			builder.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("Current message: ")
	builder.WriteString(userMessage)

	return builder.String()
}
