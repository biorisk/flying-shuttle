package main

import (
	"context"
	"errors"
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
	"github.com/biorisk/flying-shuttle/internal/project"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/workingdocs"
)

func main() {
	// A project switch re-execs this binary; loop so the process image is
	// replaced cleanly rather than nested.
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	addr := env("SHUTTLE_ADDR", ":8080")

	paths, err := project.Resolve()
	if err != nil {
		return err
	}
	log.Printf("project %q  (%s)", paths.Name, paths.Dir)

	s, err := store.NewSQLiteStore(paths.DB)
	if err != nil {
		return err
	}
	defer s.Close()
	if err := s.Migrate(); err != nil {
		return err
	}

	// Recovery: a fresh DB but a working-doc state.json present -> re-import it.
	if empty, _ := storeIsEmpty(s); empty {
		if st, err := workingdocs.LoadState(paths.StateJSON); err == nil && st.Data != nil {
			if err := s.ImportState(st.Data); err != nil {
				log.Printf("recovery: import state.json: %v", err)
			} else {
				n, _ := s.ListNodes()
				log.Printf("recovery: restored %d nodes from %s", len(n), paths.StateJSON)
			}
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	restart := make(chan string, 1) // carries the project to switch to

	// Embedder (optional local Python sidecar).
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

	clusterEmbedder := &ingest.StubEmbedder{}

	idx := search.NewHybridIndex(embedder)
	if v := os.Getenv("SHUTTLE_RRF_K"); v != "" {
		if k, err := strconv.ParseFloat(v, 64); err == nil && k > 0 {
			idx.RRFk = k
			log.Printf("RRF k set to %.1f", k)
		}
	}

	if err := indexer.LoadAndReconcile(s, idx, paths.BM25, paths.HNSW); err != nil {
		return err
	}
	log.Printf("index ready: %d docs (BM25), %d vectors (HNSW)", idx.BM25.Len(), idx.Vector.Len())

	var wg sync.WaitGroup
	spawn := func(fn func(context.Context)) {
		wg.Add(1)
		go func() { defer wg.Done(); fn(ctx) }()
	}

	snap := indexer.NewSnapshotter(idx, paths.BM25, paths.HNSW, 15*time.Second)
	spawn(snap.Run)

	docs := &workingdocs.Flusher{Store: s, Project: paths.Name, OutlineMD: paths.OutlineMD, StateJSON: paths.StateJSON}
	spawn(docs.Run)

	afterIngest := func() {}
	if embedder != nil {
		bf := indexer.NewBackfiller(s, embedder, idx, 16, 30*time.Second)
		spawn(bf.Run)
		afterIngest = bf.Trigger
	}

	deps := api.Deps{
		Store:           s,
		UploadDir:       paths.UploadDir,
		ClusterEmbedder: clusterEmbedder,
		Index:           idx,
		Stitcher:        &stitch.StubStitcher{},
		AfterIngest:     afterIngest,
		ProjectName:     paths.Name,
		Restart:         func(name string) { trySend(restart, name) },
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      api.NewRouter(deps),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	go func() {
		log.Printf("shuttle listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	var switchTo string
	select {
	case <-ctx.Done():
	case switchTo = <-restart:
	}
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	stop() // stop catching signals so workers see ctx.Done via the parent
	wg.Wait()

	if switchTo != "" {
		home, _ := project.Home()
		if err := project.SetCurrent(home, switchTo); err != nil {
			return err
		}
		s.Close()
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		log.Printf("switching to project %q — restarting", switchTo)
		return syscall.Exec(exe, os.Args, os.Environ())
	}
	log.Println("done")
	return nil
}

func storeIsEmpty(s store.Store) (bool, error) {
	n, err := s.ListNodes()
	if err != nil {
		return false, err
	}
	return len(n) == 0, nil
}

func trySend(ch chan string, v string) {
	select {
	case ch <- v:
	default:
	}
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
