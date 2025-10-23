package nexa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type Model struct {
	Name      string
	Type      string
	Location  string
	IsRunning bool
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:18181"
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (c *Client) GetModels() ([]Model, error) {
	cmd := exec.Command("nexa", "list", "--verbose")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to execute nexa list: %w", err)
	}

	return parseModelList(string(output)), nil
}

func parseModelList(output string) []Model {
	var models []Model
	lines := strings.Split(output, "\n")

	// Find the table content (between ├ and └ lines, excluding header)
	inTable := false
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and log output
		if line == "" || strings.Contains(line, "[0m") || strings.Contains(line, "[2m") {
			continue
		}

		// Start of table header
		if strings.HasPrefix(line, "┌") {
			inTable = true
			continue
		}

		// End of table
		if strings.HasPrefix(line, "└") {
			break
		}

		// Skip header rows and separator rows
		if strings.HasPrefix(line, "│ NAME") || strings.HasPrefix(line, "├") {
			continue
		}

		// Parse data rows
		if inTable && strings.HasPrefix(line, "│") {
			// Split by │ and extract fields
			parts := strings.Split(line, "│")
			if len(parts) >= 5 {
				// parts[0] is empty, parts[1] is NAME, parts[2] is SIZE, parts[3] is PLUGIN, parts[4] is TYPE
				name := strings.TrimSpace(parts[1])
				modelType := strings.TrimSpace(parts[4])

				if name != "" && modelType != "" {
					// Map nexa types to our internal types
					mappedType := modelType
					if modelType == "llm" {
						mappedType = "text-generation"
					} else if modelType == "embedder" {
						mappedType = "embeddings"
					} else if modelType == "reranker" {
						mappedType = "reranking"
					}

					model := Model{
						Name:      name,
						Type:      mappedType,
						Location:  "", // Not critical for our use
						IsRunning: false,
					}
					models = append(models, model)
				}
			}
		}
	}

	return models
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}
