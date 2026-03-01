package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/biorisk/flying-shuttle/internal/api"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
)

func main() {
	dbPath := env("SHUTTLE_DB", "shuttle.db")
	addr := env("SHUTTLE_ADDR", ":8080")
	uploadDir := env("SHUTTLE_UPLOAD_DIR", "uploads")
	staticDir := env("SHUTTLE_STATIC_DIR", "web/dist")
	hnswPath := env("SHUTTLE_HNSW_PATH", "shuttle.hnsw")

	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		log.Fatalf("create upload dir: %v", err)
	}

	transcriber := &ingest.StubTranscriber{}
	// StubEmbedder is used only for clustering suggestions (EmbeddingClusterer).
	// The HybridIndex uses nil embedder → BM25-only query mode.
	stubEmbedder := &ingest.StubEmbedder{}
	chunker := &ingest.Chunker{Embedder: stubEmbedder}

	// Build hybrid search index in BM25-only mode (nil embedder).
	// Pre-computed embeddings from .fembed files are indexed via the ingest API.
	idx := search.NewHybridIndex(nil)

	// Load existing chunks into BM25 index and HNSW vector index.
	existingChunks, err := s.ListChunks()
	if err != nil {
		log.Fatalf("load chunks for index: %v", err)
	}
	idx.IndexChunks(existingChunks)
	log.Printf("indexed %d chunks (BM25)", len(existingChunks))

	// Load persisted HNSW index from disk if it exists.
	if _, statErr := os.Stat(hnswPath); statErr == nil {
		if err := idx.Vector.Load(hnswPath); err != nil {
			log.Printf("warning: failed to load HNSW index from %s: %v", hnswPath, err)
		} else {
			log.Printf("loaded HNSW index from %s (%d vectors)", hnswPath, idx.Vector.Len())
		}
	}

	stitcher := &stitch.StubStitcher{}

	// Check if the static frontend directory exists.
	if info, err := os.Stat(staticDir); err != nil || !info.IsDir() {
		log.Printf("static dir %q not found, serving API only", staticDir)
		staticDir = ""
	} else {
		log.Printf("serving frontend from %s", staticDir)
	}

	router := api.NewRouter(s, uploadDir, transcriber, chunker, idx, stitcher, staticDir, hnswPath)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("shuttle listening on %s (db: %s)", addr, dbPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("done")
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
