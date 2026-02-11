package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/dag"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds the chi router with all API routes.
// If staticDir is non-empty and the directory exists, it serves the frontend
// build from that path with SPA fallback (unknown paths return index.html).
func NewRouter(s store.Store, uploadDir string, transcriber ingest.Transcriber, chunker *ingest.Chunker, idx *search.HybridIndex, stitcher stitch.Stitcher, staticDir string) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(cors)

	// Health check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	ch := &chunkHandler{store: s}
	nh := &nodeHandler{store: s}
	eh := &edgeHandler{store: s}
	th := &threadHandler{store: s}
	uh := &uploadHandler{store: s, uploadDir: uploadDir, transcribe: transcriber, chunker: chunker, index: idx}
	sh := &searchHandler{index: idx}
	clusterer := &search.EmbeddingClusterer{Embedder: chunker.Embedder}
	sgh := &suggestHandler{store: s, translator: &search.QueryTranslator{Index: idx}, clusterer: clusterer}
	sth := &stitchHandler{store: s, stitcher: stitcher}
	lh := &linearizeHandler{store: s, stitcher: stitcher}
	cxh := &contextHandler{store: s, checker: &search.ContextChecker{Index: idx}}
	exh := &exportHandler{store: s, stitcher: stitcher}
	snh := &snapshotHandler{store: s}
	bh := &branchHandler{store: s}

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(jsonContent)

		// Chunks (immutable — no PUT/DELETE)
		r.Route("/chunks", func(r chi.Router) {
			r.Get("/", ch.list)
			r.Post("/", ch.create)
			r.Get("/{id}", ch.get)
		})

		// Nodes (full CRUD)
		r.Route("/nodes", func(r chi.Router) {
			r.Get("/", nh.list)
			r.Post("/", nh.create)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", nh.get)
				r.Put("/", nh.update)
				r.Delete("/", nh.delete)
				r.Get("/chunks", nh.getChunks)
				r.Put("/chunks", nh.setChunks)
				r.Get("/edges", nh.getEdges)
				r.Post("/move", nh.move)
			r.Post("/check-context", cxh.checkContext)
				r.Get("/suggest", sgh.suggest)
				r.Get("/suggest-clusters", sgh.suggestClusters)
			})
		})

		// Edges (CRUD with cycle validation on create)
		r.Route("/edges", func(r chi.Router) {
			r.Get("/", eh.list)
			r.Post("/", eh.create)
			r.Get("/{id}", eh.get)
			r.Delete("/{id}", eh.delete)
		})

		// Threads (full CRUD + node ordering + render)
		r.Route("/threads", func(r chi.Router) {
			r.Get("/", th.list)
			r.Post("/", th.create)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", th.get)
				r.Put("/", th.update)
				r.Delete("/", th.delete)
				r.Get("/nodes", th.getNodes)
				r.Put("/nodes", th.setNodes)
				r.Get("/render", th.render)
			r.Get("/linearize", lh.linearizeThread)
			})
		})

		// Uploads (multipart create, no JSON content-type override)
		r.Route("/uploads", func(r chi.Router) {
			r.Get("/", uh.list)
			r.Post("/", uh.create)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", uh.get)
				r.Get("/segments", uh.listSegments)
				r.Post("/rechunk", uh.rechunk)
			})
		})

		// Search
		r.Get("/search", sh.query)

		// Stitch
		r.Post("/stitch", sth.stitch)

		// Export
		r.Route("/export", func(r chi.Router) {
			r.Post("/markdown", exh.exportMarkdown)
			r.Get("/markdown/download", exh.downloadMarkdown)
		})

		// Snapshots
		r.Route("/snapshots", func(r chi.Router) {
			r.Get("/", snh.list)
			r.Post("/", snh.create)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", snh.get)
				r.Delete("/", snh.delete)
				r.Post("/restore", snh.restore)
			})
		})

		// Branches
		r.Route("/branches", func(r chi.Router) {
			r.Get("/", bh.list)
			r.Post("/", bh.create)
			r.Get("/active", bh.active)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", bh.get)
				r.Put("/", bh.update)
				r.Delete("/", bh.delete)
				r.Post("/switch", bh.switchTo)
			})
		})

		// DAG operations
		r.Route("/dag", func(r chi.Router) {
			r.Get("/linearize", lh.linearizeManuscript)
			r.Get("/validate", func(w http.ResponseWriter, r *http.Request) {
				report, err := dag.ValidateGraph(s)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, report)
			})
			r.Get("/roots", func(w http.ResponseWriter, r *http.Request) {
				roots, err := dag.FindRoots(s)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, roots)
			})
		})
	})

	// Serve frontend static files with SPA fallback.
	if staticDir != "" {
		fs := http.FileServer(http.Dir(staticDir))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			// Try to serve the requested file.
			path := filepath.Join(staticDir, strings.TrimPrefix(r.URL.Path, "/"))
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				fs.ServeHTTP(w, r)
				return
			}
			// SPA fallback: serve index.html for unmatched paths.
			http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
		})
	}

	return r
}
