package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/biorisk/flying-shuttle/internal/api"
	"github.com/biorisk/flying-shuttle/internal/indexer"
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
	bm25Path := env("SHUTTLE_BM25_PATH", "shuttle.bm25")

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

	// Background workers stop when the process receives SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Embedding backend: a local Python server that Go spawns and supervises.
	// Calls made before the model has loaded return ErrEmbedderNotReady, so
	// ingestion never blocks — embeddings backfill once it's up.
	var embedder ingest.Embedder
	if env("SHUTTLE_EMBED_AUTOSTART", "1") != "0" {
		script, _ := filepath.Abs(env("SHUTTLE_EMBED_SCRIPT", "python/embed_server.py"))
		py := ingest.NewPythonEmbedder(ingest.PythonEmbedderConfig{
			Python: detectPython(),
			Script: script,
			Addr:   env("SHUTTLE_EMBED_ADDR", "127.0.0.1:8071"),
			Dir:    env("SHUTTLE_EMBED_DIR", filepath.Dir(script)),
		})
		py.Start(ctx)
		embedder = py
	} else {
		log.Printf("embedder: disabled (SHUTTLE_EMBED_AUTOSTART=0); vector search unavailable")
	}

	// StubEmbedder backs the cluster-suggestion feature only; it is never
	// persisted. Real chunk embeddings come from the backfiller.
	clusterEmbedder := &ingest.StubEmbedder{}

	idx := search.NewHybridIndex(embedder)
	if v := os.Getenv("SHUTTLE_RRF_K"); v != "" {
		if k, err := strconv.ParseFloat(v, 64); err == nil && k > 0 {
			idx.RRFk = k
			log.Printf("RRF k set to %.1f", k)
		} else {
			log.Printf("warning: invalid SHUTTLE_RRF_K %q, using default %.1f", v, idx.RRFk)
		}
	}

	// Load on-disk snapshots and reconcile against the store. After a clean
	// shutdown this is near-instant; on first run it's a full rebuild.
	if err := indexer.LoadAndReconcile(s, idx, bm25Path, hnswPath); err != nil {
		log.Fatalf("index reconcile: %v", err)
	}
	log.Printf("index ready: %d docs (BM25), %d vectors (HNSW)", idx.BM25.Len(), idx.Vector.Len())

	// Persist the index incrementally so subsequent boots stay instant.
	snap := indexer.NewSnapshotter(idx, bm25Path, hnswPath, 15*time.Second)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); snap.Run(ctx) }()

	// Backfill embeddings for any vector-less chunks, now and on every upload.
	afterIngest := func() {}
	if embedder != nil {
		bf := indexer.NewBackfiller(s, embedder, idx, 16, 30*time.Second)
		wg.Add(1)
		go func() { defer wg.Done(); bf.Run(ctx) }()
		afterIngest = bf.Trigger
	}

	stitcher := &stitch.StubStitcher{}

	if info, err := os.Stat(staticDir); err != nil || !info.IsDir() {
		log.Printf("static dir %q not found, serving API only", staticDir)
		staticDir = ""
	} else {
		log.Printf("serving frontend from %s", staticDir)
	}

	router := api.NewRouter(s, uploadDir, clusterEmbedder, idx, stitcher, staticDir, afterIngest)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

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
		log.Printf("shutdown: %v", err)
	}

	// Wait for background workers (the snapshotter performs a final flush).
	wg.Wait()
	log.Println("done")
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// detectPython picks an interpreter: an explicit override, then a project
// virtualenv, then whatever "python3" resolves to.
func detectPython() string {
	if v := os.Getenv("SHUTTLE_PYTHON"); v != "" {
		return v
	}
	for _, cand := range []string{
		filepath.Join("python", ".venv", "bin", "python"),
		filepath.Join(".venv", "bin", "python"),
	} {
		if _, err := os.Stat(cand); err == nil {
			abs, _ := filepath.Abs(cand)
			return abs
		}
	}
	if p, err := exec.LookPath("python3"); err == nil {
		return p
	}
	return "python3"
}
