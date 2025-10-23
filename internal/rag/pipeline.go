package rag

import (
	"context"
	"encoding/json"
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
