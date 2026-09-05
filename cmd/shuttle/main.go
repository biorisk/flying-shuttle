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
	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/biorisk/flying-shuttle/internal/indexer"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/project"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/web"
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
	if err := reconcileEmbeddingModel(s, paths.HNSW); err != nil {
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

	// Shared compute budget: the embedder and (later) the instruct LLM acquire
	// this so large local-model jobs never run concurrently on 8GB.
	computeGate := ingest.NewComputeGate()

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
		py.Gate = computeGate
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

	previewReload := web.NewBroadcaster()
	docs := &workingdocs.Flusher{
		Store: s, Project: paths.Name,
		OutlineMD: paths.OutlineMD, StateJSON: paths.StateJSON,
		OnWrite: previewReload.Notify,
	}
	spawn(docs.Run)

	afterIngest := func() {}
	if embedder != nil {
		bf := indexer.NewBackfiller(s, embedder, idx, 16, 30*time.Second)
		spawn(bf.Run)
		afterIngest = bf.Trigger
	}

	// Shared instruct LLM (lazy — nothing loads until the first digest call).
	// It idle-sheds itself; the supervisor restarts it on demand.
	var atlasSummariser atlas.Summariser
	var atlasLabeller *atlas.ChunkLabeller
	if env("SHUTTLE_LLM_AUTOSTART", "1") != "0" {
		llmScript, _ := filepath.Abs(env("SHUTTLE_LLM_SCRIPT", "python/llm_server.py"))
		completer := ingest.NewPythonCompleter(ingest.PythonCompleterConfig{
			Python: detectPython(),
			Script: llmScript,
			Addr:   env("SHUTTLE_LLM_ADDR", "127.0.0.1:8072"),
			Dir:    env("SHUTTLE_LLM_DIR", filepath.Dir(llmScript)),
		})
		completer.Gate = computeGate // never overlaps a big embed batch
		llmModel := env("SHUTTLE_LLM_MODEL", "gemma-4-e2b-it-4bit")
		atlasSummariser = &atlas.LLMSummariser{Complete: completer, ModelName: llmModel}
		atlasLabeller = &atlas.ChunkLabeller{Complete: completer, ModelName: llmModel}
	}

	// Source Atlas: a derived network over the transcript corpus (see
	// source_atlas_plan.md). Rebuilds run on demand via POST /atlas/rebuild;
	// nothing runs in the background here. A nil embedder means digest search
	// / bullet affinity are unavailable but browsing still works. A nil
	// summariser (or an unreachable LLM) falls back to extractive digests.
	atlasSvc := &atlas.Service{
		BaseCtx:  ctx,
		Embedder: embedder,
		Builder: &atlas.Builder{
			Store:      atlas.NewStore(s.DB()),
			Corpus:     func() ([]atlas.CorpusChunk, error) { return loadAtlasCorpus(s) },
			Embedder:   embedder,
			Summariser: atlasSummariser,
			Labeller:   atlasLabeller,
		},
	}
	if err := atlasSvc.LoadCurrent(); err != nil {
		log.Printf("atlas: load current build: %v", err)
	}

	deps := api.Deps{
		Store:           s,
		Atlas:           atlasSvc,
		UploadDir:       paths.UploadDir,
		ClusterEmbedder: clusterEmbedder,
		Index:           idx,
		Stitcher:        &stitch.StubStitcher{},
		AfterIngest:     afterIngest,
		ProjectName:     paths.Name,
		OutlineMDPath:   paths.OutlineMD,
		PreviewReload:   previewReload,
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

// embedModelID identifies the current embedding model / vector space. Bump it
// whenever the model or its dimension changes so stale vectors are dropped.
const embedModelID = "embeddinggemma-300m-768"

// reconcileEmbeddingModel clears stored embeddings and the HNSW snapshot when
// the embedding model has changed since the last run (or when a pre-marker DB
// holds vectors of the wrong dimension). The backfiller then re-embeds.
func reconcileEmbeddingModel(s *store.SQLiteStore, hnswPath string) error {
	prev, err := s.GetMeta("embed_model")
	if err != nil {
		return err
	}
	stale := prev != "" && prev != embedModelID
	if prev == "" {
		if dim, err := s.SampleEmbeddingDim(); err != nil {
			return err
		} else if dim != 0 && dim != 768 {
			stale = true
		}
	}
	if stale {
		n, err := s.ClearAllEmbeddings()
		if err != nil {
			return err
		}
		_ = os.Remove(hnswPath)
		log.Printf("embedding model changed (%q -> %q): cleared %d stale vectors, dropped %s",
			prev, embedModelID, n, filepath.Base(hnswPath))
	}
	return s.SetMeta("embed_model", embedModelID)
}

// loadAtlasCorpus pulls every embedded chunk (content + vector) for an Atlas
// build.
func loadAtlasCorpus(s store.Store) ([]atlas.CorpusChunk, error) {
	ids, err := s.ListChunkIDsWithEmbedding()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	chunks, err := s.GetChunksByIDs(ids)
	if err != nil {
		return nil, err
	}
	out := make([]atlas.CorpusChunk, 0, len(chunks))
	for _, c := range chunks {
		if len(c.EmbeddingVec) == 0 {
			continue
		}
		out = append(out, atlas.CorpusChunk{
			ID:          c.ID,
			Content:     c.Content,
			Vec:         ingest.BytesToFloat32s(c.EmbeddingVec),
			SourceFile:  c.SourceFile,
			StartOffset: c.StartOffset,
		})
	}
	return out, nil
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
