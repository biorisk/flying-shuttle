package web

import (
	"log"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/outline"
	"github.com/biorisk/flying-shuttle/internal/pipeline"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/biorisk/flying-shuttle/internal/stitch"
	"github.com/biorisk/flying-shuttle/internal/store"
	"github.com/biorisk/flying-shuttle/internal/transcript"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
	"github.com/go-chi/chi/v5"
)

// Deps are the services the server-rendered UI needs. Fields are added as
// tasks require them.
type Deps struct {
	Store      store.Store
	Outline    *outline.Service
	Transcript *transcript.Service
	Ingester   *pipeline.Ingester
	Index      *search.HybridIndex
	Stitcher   stitch.Stitcher
}

// Mount attaches the server-rendered UI (templ + Datastar): the shell at "/",
// its fragment endpoints, and the /static asset tree. The JSON API lives under
// /api/v1 and is mounted separately.
func Mount(r chi.Router, d Deps) {
	// Fill in services derivable from the store so callers (and tests) can pass
	// just Store.
	if d.Outline == nil && d.Store != nil {
		d.Outline = &outline.Service{Store: d.Store}
	}
	if d.Transcript == nil && d.Store != nil {
		d.Transcript = &transcript.Service{Store: d.Store}
	}
	if d.Ingester == nil && d.Store != nil {
		d.Ingester = &pipeline.Ingester{Store: d.Store}
	}
	if d.Stitcher == nil {
		d.Stitcher = &stitch.StubStitcher{}
	}
	h := &handlers{d: d}

	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(StaticFS()))))

	r.Get("/", h.shell)
	r.Group(func(r chi.Router) {
		r.Get("/outline", h.outline)
		r.Get("/evidence", h.evidence)
		r.Get("/evidence/transcript", h.transcriptReader)
		r.Get("/stitch", h.stitchView)
		r.Get("/ingest", h.ingest)
		r.Post("/ingest", h.ingestUpload)
		h.mountOutlineEdit(r)
		h.mountThreads(r)
		h.mountExits(r)
		r.Post("/outline/rescue", h.outlineRescue)

		r.Get("/snapshots", h.snapshotBar)
		r.Post("/snapshots", h.snapshotCreate)
		r.Post("/snapshots/{id}/restore", h.snapshotRestore)
		r.Delete("/snapshots/{id}", h.snapshotDelete)

		r.Get("/branches", h.branchBar)
		r.Post("/branches", h.branchCreate)
		r.Post("/branches/{id}/switch", h.branchSwitch)
		r.Delete("/branches/{id}", h.branchDelete)
	})
}

type handlers struct {
	d Deps
}

func (h *handlers) evidenceFinder() *EvidenceFinder {
	return &EvidenceFinder{Index: h.d.Index, Store: h.d.Store}
}

// shell renders the full two-pane application page with every region SSR'd.
func (h *handlers) shell(w http.ResponseWriter, r *http.Request) {
	ov, err := h.outlineView()
	if err != nil {
		log.Printf("shell: outline: %v", err)
	}
	Render(w, r, components.Page(components.PageContent{
		Outline:     components.Outline(ov),
		Evidence:    components.Evidence(viewmodel.EvidencePane{}),
		Ingest:      components.Ingest(h.ingestView()),
		Preview:     components.Stitch(viewmodel.StitchView{Glue: 50}),
		ThreadBar:   components.ThreadBar(h.threadBarView()),
		SnapshotBar: components.SnapshotBar(h.snapshotBarView()),
		BranchBar:   components.BranchBar(h.branchBarView()),
	}))
}

// outline renders the #outline fragment as a Datastar SSE patch.
//
//	GET /outline
func (h *handlers) outline(w http.ResponseWriter, r *http.Request) {
	ov, err := h.outlineViewOpts(outlineOpts{
		ThreadID:    r.URL.Query().Get("thread"),
		DiffAgainst: r.URL.Query().Get("diff"),
	})
	if err != nil {
		log.Printf("outline: %v", err)
	}
	if _, err := Patch(w, r, components.Outline(ov)); err != nil {
		log.Printf("outline: patch: %v", err)
	}
}
