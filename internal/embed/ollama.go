package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Ollama struct {
	baseURL   string
	model     string
	dimension int
	client    *http.Client
}

func NewOllama(baseURL, model string, dimension int, timeout time.Duration) *Ollama {
	return &Ollama{baseURL: strings.TrimRight(baseURL, "/"), model: model, dimension: dimension, client: &http.Client{Timeout: timeout}}
}

func (o *Ollama) Model() string  { return o.model }
func (o *Ollama) Dimension() int { return o.dimension }

func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no text to embed")
	}
	payload := struct {
		Model     string   `json:"model"`
		Input     []string `json:"input"`
		Truncate  bool     `json:"truncate"`
		KeepAlive int      `json:"keep_alive"`
	}{Model: o.model, Input: texts, Truncate: true, KeepAlive: -1}
	var response struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := o.post(ctx, "/api/embed", payload, &response); err != nil {
		return nil, err
	}
	if len(response.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embedding provider returned %d vectors for %d inputs", len(response.Embeddings), len(texts))
	}
	for i, vector := range response.Embeddings {
		if len(vector) != o.dimension {
			return nil, fmt.Errorf("embedding %d has dimension %d, expected %d", i, len(vector), o.dimension)
		}
	}
	return response.Embeddings, nil
}

func (o *Ollama) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama readiness: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ollama readiness returned %s", resp.Status)
	}
	return nil
}

func (o *Ollama) post(ctx context.Context, path string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("ollama returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(output); err != nil {
		return fmt.Errorf("decode ollama response: %w", err)
	}
	return nil
}
