package api

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/pipeline"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/transcript"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds the chi router: the server-rendered UI (templ + Datastar) at
// "/", plus a tiny JSON surface under /api/v1 for the offline embedding
// pipeline (see python/README.md) and a health check. Everything the app does
// interactively goes through the "/" fragment endpoints, not JSON.
//
// afterIngest, if non-nil, is called after new chunks are stored and indexed
// (e.g. to nudge the embedding backfiller). It must not block.
// clusterEmbedder backs the cluster-suggestion feature; it may be a stub.
func NewRouter(s store.Store, uploadDir string, clusterEmbedder ingest.Embedder, idx *search.HybridIndex, stitcher stitch.Stitcher, afterIngest func()) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(cors)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	ingester := &pipeline.Ingester{Store: s, UploadDir: uploadDir, Index: idx, AfterIngest: afterIngest}

	// JSON API — offline embedding-pipeline ingest only. The Python side POSTs
	// {"path": "..."} pointing at a .fembed / .embed file or directory of them.
	ih := &ingestHandler{store: s, idx: idx}
	r.Route("/api/v1/ingest", func(r chi.Router) {
		r.Use(jsonContent)
		r.Post("/embed-file", ih.importEmbedFile)              // binary .fembed
		r.Post("/embed-file-legacy", ih.importLegacyEmbedFile) // TSV .embed
		r.Post("/directory", ih.importDirectory)               // dir of *.fembed
		r.Post("/directory-legacy", ih.importLegacyDirectory)  // dir of *.embed
	})

	// Server-rendered UI (templ + Datastar) at / and /static, plus the
	// markdown-export download and — when SHUTTLE_E2E=1 — a test reset hook.
	web.Mount(r, web.Deps{
		Store:      s,
		Outline:    &outline.Service{Store: s},
		Transcript: &transcript.Service{Store: s},
		Ingester:   ingester,
		Index:      idx,
		Stitcher:   stitcher,
	})

	return r
}
