package api

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/dag"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// NewRouter builds the chi router with all API routes.
func NewRouter(s store.Store, uploadDir string, transcriber ingest.Transcriber, chunker *ingest.Chunker) http.Handler {
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
	uh := &uploadHandler{store: s, uploadDir: uploadDir, transcribe: transcriber, chunker: chunker}

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

		// DAG operations
		r.Route("/dag", func(r chi.Router) {
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

	return r
}
