package nexa

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	TopP        float64       `json:"top_p,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int         `json:"index"`
		Message      ChatMessage `json:"message,omitempty"`
		Delta        ChatMessage `json:"delta,omitempty"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

func (c *Client) ChatCompletion(ctx context.Context, req ChatCompletionRequest) (<-chan string, <-chan error, error) {
	req.Stream = true

	resp, err := c.doRequest(ctx, "POST", "/v1/chat/completions", req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to make chat completion request: %w", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, nil, fmt.Errorf("chat completion API returned status %d: %s", resp.StatusCode, string(body))
	}

	streamChan := make(chan string, 10)
	errChan := make(chan error, 1)

	go func() {
		defer resp.Body.Close()
		defer close(streamChan)
		defer close(errChan)

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					errChan <- fmt.Errorf("error reading stream: %w", err)
				}
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if line == "data:[DONE]" {
				return
			}

			if !strings.HasPrefix(line, "data:") {
				continue
			}

			jsonData := strings.TrimPrefix(line, "data:")
			var streamResp ChatCompletionResponse
			if err := json.Unmarshal([]byte(jsonData), &streamResp); err != nil {
				errChan <- fmt.Errorf("failed to decode stream response: %w", err)
				return
			}

			if len(streamResp.Choices) > 0 {
				delta := streamResp.Choices[0].Delta.Content
				if delta != "" {
					select {
					case streamChan <- delta:
					case <-ctx.Done():
						return
					}
				}

				if streamResp.Choices[0].FinishReason != "" {
					return
				}
			}
		}
	}()

	return streamChan, errChan, nil
}
