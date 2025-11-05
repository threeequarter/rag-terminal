package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"rag-terminal/internal/config"
	"rag-terminal/internal/nexa"
	"rag-terminal/internal/vector"
)

// MessageProcessor handles message operations including merging, retrieval, and reranking
type MessageProcessor struct {
	vectorStore vector.VectorStore
	nexaClient  *nexa.Client
	config      *config.Config
}

// NewMessageProcessor creates a new message processor
func NewMessageProcessor(vectorStore vector.VectorStore, nexaClient *nexa.Client, cfg *config.Config) *MessageProcessor {
	return &MessageProcessor{
		vectorStore: vectorStore,
		nexaClient:  nexaClient,
		config:      cfg,
	}
}

// GroupAndMergeChunkedMessages groups message chunks and merges them into complete messages
// If any chunk of a message is in the results, all chunks are retrieved and merged
func (mp *MessageProcessor) GroupAndMergeChunkedMessages(ctx context.Context, messages []vector.Message) []vector.Message {
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
		allChunks, err := mp.retrieveAllChunks(ctx, baseID)
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
func (mp *MessageProcessor) retrieveAllChunks(ctx context.Context, baseID string) ([]vector.Message, error) {
	badgerStore, ok := mp.vectorStore.(*vector.BadgerStore)
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

// RerankMessagesWithLLM uses an LLM to score and rerank messages by relevance
func (mp *MessageProcessor) RerankMessagesWithLLM(ctx context.Context, llmModel, query string, messages []vector.Message, topK int) ([]vector.Message, error) {
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
	response, err := mp.nexaClient.ChatCompletionSync(ctx, req)
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
