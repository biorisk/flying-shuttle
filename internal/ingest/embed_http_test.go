package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPEmbedder_EmbedBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embed" || r.Method != http.MethodPost {
			http.Error(w, "bad", http.StatusNotFound)
			return
		}
		var req struct {
			Texts []string `json:"texts"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		out := make([][]float32, len(req.Texts))
		for i := range req.Texts {
			out[i] = []float32{float32(i), 1, 2}
		}
		json.NewEncoder(w).Encode(map[string]any{"embeddings": out, "dim": 3})
	}))
	defer srv.Close()

	e := NewHTTPEmbedder(srv.URL)
	vecs, err := e.EmbedBatch(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[1]) != 3 || vecs[1][0] != 1 {
		t.Fatalf("unexpected vectors: %+v", vecs)
	}
}

func TestHTTPEmbedder_QueryVsDocumentPrompt(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texts  []string `json:"texts"`
			Prompt string   `json:"prompt"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		seen = append(seen, req.Prompt)
		out := make([][]float32, len(req.Texts))
		for i := range out {
			out[i] = []float32{1, 0}
		}
		json.NewEncoder(w).Encode(map[string]any{"embeddings": out, "dim": 2})
	}))
	defer srv.Close()

	e := NewHTTPEmbedder(srv.URL)
	if _, err := e.EmbedBatch(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.EmbedQuery(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "document" || seen[1] != "query" {
		t.Fatalf("prompt routing wrong: %v", seen)
	}

	// EmbedQueryOr picks the query path when the embedder implements it.
	var qe QueryEmbedder = e
	_ = qe
	if _, err := EmbedQueryOr(context.Background(), e, "x"); err != nil {
		t.Fatal(err)
	}
	if seen[len(seen)-1] != "query" {
		t.Fatalf("EmbedQueryOr did not use query path: %v", seen)
	}
}

func TestHTTPEmbedder_NotReadyOn503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	e := NewHTTPEmbedder(srv.URL)
	_, err := e.Embed(context.Background(), "x")
	if !errors.Is(err, ErrEmbedderNotReady) {
		t.Fatalf("want ErrEmbedderNotReady, got %v", err)
	}
}

func TestHTTPEmbedder_NotReadyOnConnRefused(t *testing.T) {
	e := NewHTTPEmbedder("http://127.0.0.1:1") // nothing listening
	_, err := e.Embed(context.Background(), "x")
	if !errors.Is(err, ErrEmbedderNotReady) {
		t.Fatalf("want ErrEmbedderNotReady, got %v", err)
	}
}

func TestHTTPEmbedder_Healthy(t *testing.T) {
	var ok bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	e := NewHTTPEmbedder(srv.URL)
	if e.Healthy(context.Background()) {
		t.Fatal("should be unhealthy")
	}
	ok = true
	if !e.Healthy(context.Background()) {
		t.Fatal("should be healthy")
	}
}
