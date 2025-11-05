package rag

import (
	"context"
	"strings"
	"time"

	"rag-terminal/internal/logging"
)

// ResponseProcessor handles stream collection and completion processing
type ResponseProcessor struct {
	profileExtractor *ProfileExtractor
}

// NewResponseProcessor creates a new response processor
func NewResponseProcessor(profileExtractor *ProfileExtractor) *ResponseProcessor {
	return &ResponseProcessor{
		profileExtractor: profileExtractor,
	}
}

// CollectStreamedResponse collects tokens from a stream and calls onComplete when done
func (rp *ResponseProcessor) CollectStreamedResponse(
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

// StartAsyncFactExtraction starts background fact extraction without blocking
func (rp *ResponseProcessor) StartAsyncFactExtraction(chatID, llmModel, userQuery, assistantResponse string) {
	if rp.profileExtractor == nil {
		return
	}

	go func() {
		// Wait 2 seconds before processing facts to avoid rate limiting on the API
		time.Sleep(2 * time.Second)

		// Use a short timeout for fact extraction to not block too long
		extractCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := rp.profileExtractor.ExtractFacts(extractCtx, chatID, llmModel, userQuery, assistantResponse); err != nil {
			logging.Debug("Profile extraction failed (non-blocking): %v", err)
		}
	}()
}
