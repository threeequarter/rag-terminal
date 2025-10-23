package nexa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

type RerankingRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	BatchSize       int      `json:"batch_size,omitempty"`
	Normalize       bool     `json:"normalize,omitempty"`
	NormalizeMethod string   `json:"normalize_method,omitempty"`
}

type RerankingResponse struct {
	Result []float64 `json:"result"`
	Model  string    `json:"model"`
}

func (c *Client) Rerank(ctx context.Context, req RerankingRequest) ([]float64, error) {
	if len(req.Documents) == 0 {
		return nil, fmt.Errorf("no documents provided for reranking")
	}

	// Set defaults
	if req.BatchSize == 0 {
		req.BatchSize = 10
	}
	if req.NormalizeMethod == "" {
		req.NormalizeMethod = "softmax"
	}

	resp, err := c.doRequest(ctx, "POST", "/v1/reranking", req)
	if err != nil {
		return nil, fmt.Errorf("failed to make reranking request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("reranking API returned status %d: %s", resp.StatusCode, string(body))
	}

	var rerankResp RerankingResponse
	if err := json.NewDecoder(resp.Body).Decode(&rerankResp); err != nil {
		return nil, fmt.Errorf("failed to decode reranking response: %w", err)
	}

	return rerankResp.Result, nil
}
