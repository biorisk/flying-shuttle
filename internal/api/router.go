package api

import (
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/atlas"
	"github.com/biorisk/flying-shuttle/internal/corpus"
	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/pipeline"
	"github.com/biorisk/flying-shuttle/internal/project"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/transcript"
	"github.com/biorisk/flying-shuttle/internal/web"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// Deps is everything NewRouter needs.
type Deps struct {
	Store           doc.Store
	Corpus          corpus.Store // nil when the project is unbound (no corpus)
	CorpusName      string       // bound corpus name, for the project bar
	CorpusReadOnly  bool         // true when another project holds the writer lock
	CorpusHolder    string       // name of the project holding the writer lock
	UploadDir       string
	ClusterEmbedder ingest.Embedder // backs cluster suggestions; may be a stub
	Index           *search.HybridIndex
	Stitcher        stitch.Stitcher
	Atlas           *atlas.Service
	AfterIngest     func() // nudges the embedding backfiller; must not block

	ProjectName   string
	OutlineMDPath string
	PreviewReload *web.Broadcaster
	// Restart switches to the named project (persists the choice and re-execs
	// the process). nil disables project switching.
	Restart func(name string)
}

// NewRouter builds the chi router: the server-rendered UI (templ + Datastar) at
// "/", plus a tiny JSON surface under /api/v1 for the offline embedding
// pipeline (see python/README.md) and a health check.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(cors)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	ingester := &pipeline.Ingester{Store: d.Corpus, UploadDir: d.UploadDir, Index: d.Index, AfterIngest: d.AfterIngest}

	// JSON API — offline embedding-pipeline ingest only. The Python side POSTs
	// {"path": "..."} pointing at a .fembed / .embed file or directory of them.
	ih := &ingestHandler{store: d.Corpus, idx: d.Index}
	r.Route("/api/v1/ingest", func(r chi.Router) {
		r.Use(jsonContent)
		r.Post("/embed-file", ih.importEmbedFile)
		r.Post("/embed-file-legacy", ih.importLegacyEmbedFile)
		r.Post("/directory", ih.importDirectory)
		r.Post("/directory-legacy", ih.importLegacyDirectory)
	})

	home, _ := project.Home()
	web.Mount(r, web.Deps{
		Store:          d.Store,
		Corpus:         d.Corpus,
		CorpusName:     d.CorpusName,
		CorpusReadOnly: d.CorpusReadOnly,
		CorpusHolder:   d.CorpusHolder,
		Outline:        &outline.Service{Store: d.Store, Corpus: d.Corpus},
		Transcript:     &transcript.Service{Store: d.Corpus},
		Ingester:       ingester,
		Index:          d.Index,
		Stitcher:       d.Stitcher,
		Atlas:          d.Atlas,
		ProjectName:    d.ProjectName,
		OutlineMDPath:  d.OutlineMDPath,
		PreviewReload:  d.PreviewReload,
		ProjectHome:    home,
		SwitchProject:  d.Restart,
	})

	return r
}
