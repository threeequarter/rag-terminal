package document

import (
	"context"
	"fmt"
	"strings"

	"rag-chat/internal/nexa"
)

// Summarizer generates concise summaries of document chunks
type Summarizer struct {
	nexaClient *nexa.Client
}

// NewSummarizer creates a new summarizer
func NewSummarizer(nexaClient *nexa.Client) *Summarizer {
	return &Summarizer{
		nexaClient: nexaClient,
	}
}

// SummarizeChunk generates a concise summary of a chunk using LLM
// This allows fitting more document context in the LLM context window
func (s *Summarizer) SummarizeChunk(ctx context.Context, model string, chunkContent string, targetLength int) (string, error) {
	// If chunk is already small, don't summarize
	if len(chunkContent) <= targetLength {
		return chunkContent, nil
	}

	prompt := fmt.Sprintf(`Summarize the following text concisely, preserving key information and facts. Target length: %d characters.

Text:
%s

Summary:`, targetLength, chunkContent)

	req := nexa.ChatCompletionRequest{
		Model: model,
		Messages: []nexa.ChatMessage{
			{Role: "system", Content: "You are a text summarization assistant. Create concise, informative summaries that preserve key facts and concepts."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3, // Low temperature for consistent summarization
		MaxTokens:   targetLength / 2, // Rough token estimate
		Stream:      false,
	}

	summary, err := s.nexaClient.ChatCompletionSync(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	return strings.TrimSpace(summary), nil
}

// SummarizeDocument creates a high-level summary of an entire document
func (s *Summarizer) SummarizeDocument(ctx context.Context, model string, documentContent string, maxLength int) (string, error) {
	prompt := fmt.Sprintf(`Create a brief summary of this document in %d characters or less. Include main topics and key points.

Document:
%s

Summary:`, maxLength, documentContent)

	req := nexa.ChatCompletionRequest{
		Model: model,
		Messages: []nexa.ChatMessage{
			{Role: "system", Content: "You are a document summarization assistant. Create high-level overviews that capture the main themes."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
		MaxTokens:   maxLength / 2,
		Stream:      false,
	}

	summary, err := s.nexaClient.ChatCompletionSync(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to generate document summary: %w", err)
	}

	return strings.TrimSpace(summary), nil
}

// ExtractKeyPoints extracts bullet points of key information from text
func (s *Summarizer) ExtractKeyPoints(ctx context.Context, model string, content string) ([]string, error) {
	prompt := fmt.Sprintf(`Extract the key points from this text as a bullet list. Be concise.

Text:
%s

Key points:`, content)

	req := nexa.ChatCompletionRequest{
		Model: model,
		Messages: []nexa.ChatMessage{
			{Role: "system", Content: "You are an information extraction assistant. Extract key facts and points as concise bullets."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.3,
		MaxTokens:   300,
		Stream:      false,
	}

	response, err := s.nexaClient.ChatCompletionSync(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to extract key points: %w", err)
	}

	content = response
	lines := strings.Split(content, "\n")

	var keyPoints []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Remove bullet markers
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "• ")

		if len(line) > 0 {
			keyPoints = append(keyPoints, line)
		}
	}

	return keyPoints, nil
}

// GenerateExtractiveSummary creates a summary by selecting key sentences (no LLM needed)
// This is faster and cheaper than LLM summarization
func (s *Summarizer) GenerateExtractiveSummary(content string, targetLength int) string {
	extractor := NewExtractor()

	// Split into sentences
	sentences := extractor.splitIntoSentences(content)
	if len(sentences) == 0 {
		return content
	}

	// If content is short enough, return as-is
	if len(content) <= targetLength {
		return content
	}

	// Score sentences by position and length (heuristic: important sentences tend to be at start/end)
	type scoredSent struct {
		sentence string
		score    float64
		position int
	}

	scored := make([]scoredSent, len(sentences))
	for i, sent := range sentences {
		positionScore := 1.0
		// Boost first and last sentences
		if i == 0 || i == len(sentences)-1 {
			positionScore = 1.5
		}
		// Boost sentences in first/last third
		if i < len(sentences)/3 || i > 2*len(sentences)/3 {
			positionScore = 1.2
		}

		// Length score (prefer medium-length sentences)
		lengthScore := float64(len(sent)) / 100.0
		if lengthScore > 2.0 {
			lengthScore = 2.0
		}

		scored[i] = scoredSent{
			sentence: sent,
			score:    positionScore * lengthScore,
			position: i,
		}
	}

	// Sort by score
	var sortedScored []scoredSent
	sortedScored = append(sortedScored, scored...)
	for i := 0; i < len(sortedScored); i++ {
		for j := i + 1; j < len(sortedScored); j++ {
			if sortedScored[j].score > sortedScored[i].score {
				sortedScored[i], sortedScored[j] = sortedScored[j], sortedScored[i]
			}
		}
	}

	// Select sentences until target length
	var selected []scoredSent
	currentLen := 0
	for _, ss := range sortedScored {
		if currentLen+len(ss.sentence) > targetLength && currentLen > 0 {
			break
		}
		selected = append(selected, ss)
		currentLen += len(ss.sentence) + 1
	}

	// Sort selected back to original order
	for i := 0; i < len(selected); i++ {
		for j := i + 1; j < len(selected); j++ {
			if selected[j].position < selected[i].position {
				selected[i], selected[j] = selected[j], selected[i]
			}
		}
	}

	// Join sentences
	var result strings.Builder
	for i, ss := range selected {
		if i > 0 {
			result.WriteString(" ")
		}
		result.WriteString(ss.sentence)
	}

	return result.String()
}
