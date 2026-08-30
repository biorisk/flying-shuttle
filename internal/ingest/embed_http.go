package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrEmbedderNotReady is returned by an embedder that is configured but whose
// backend is not currently reachable. Callers should treat this as
// "try again later" rather than a hard failure.
var ErrEmbedderNotReady = fmt.Errorf("embedder not ready")

// HTTPEmbedder calls an embedding service that speaks the small JSON protocol
// implemented by python/embed_server.py:
//
//	GET  /health              -> 200 once the model is loaded
//	POST /embed {"texts":[…]} -> {"embeddings":[[…]], "dim":N}
type HTTPEmbedder struct {
	BaseURL string
	Client  *http.Client
}

// NewHTTPEmbedder returns an HTTPEmbedder for baseURL (e.g. "http://127.0.0.1:8071").
func NewHTTPEmbedder(baseURL string) *HTTPEmbedder {
	return &HTTPEmbedder{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 5 * time.Minute},
	}
}

// Healthy reports whether the service is up and the model is loaded.
func (e *HTTPEmbedder) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.BaseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := e.Client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// Embed returns the embedding for a single text.
func (e *HTTPEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embed: expected 1 vector, got %d", len(vecs))
	}
	return vecs[0], nil
}

// EmbedBatch returns embeddings for multiple texts in order.
func (e *HTTPEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(map[string]any{"texts": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.BaseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbedderNotReady, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, ErrEmbedderNotReady
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return nil, fmt.Errorf("embed: status %d: %s", resp.StatusCode, bytes.TrimSpace(msg))
	}

	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
		Dim        int         `json:"dim"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embed: decode response: %w", err)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed: got %d vectors for %d texts", len(out.Embeddings), len(texts))
	}
	return out.Embeddings, nil
}
