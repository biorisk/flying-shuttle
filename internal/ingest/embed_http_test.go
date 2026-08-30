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
