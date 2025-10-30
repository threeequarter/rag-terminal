package nexa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

type EmbeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions *int     `json:"dimensions,omitempty"`
}

type EmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *Client) GenerateEmbeddings(ctx context.Context, model string, texts []string, dimensions *int) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts provided for embedding")
	}

	req := EmbeddingRequest{
		Model:      model,
		Input:      texts,
		Dimensions: dimensions,
	}

	resp, err := c.doRequest(ctx, "POST", "/v1/embeddings", req)
	if err != nil {
		return nil, fmt.Errorf("failed to make embeddings request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embeddings API returned status %d: %s", resp.StatusCode, string(body))
	}

	var embeddingResp EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embeddingResp); err != nil {
		return nil, fmt.Errorf("failed to decode embeddings response: %w", err)
	}

	// Extract embeddings in order
	embeddings := make([][]float32, len(embeddingResp.Data))
	for _, data := range embeddingResp.Data {
		if data.Index >= len(embeddings) {
			return nil, fmt.Errorf("invalid embedding index %d", data.Index)
		}
		embeddings[data.Index] = data.Embedding
	}

	return embeddings, nil
}
