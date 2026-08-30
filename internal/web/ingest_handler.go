package web

import (
	"log"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

// uploadListLimit caps how many rows the drawer renders; the status summary
// still reflects all uploads.
const uploadListLimit = 500

// ingestView reads the current uploads into a drawer view model. Status counts
// and the Active flag are computed over *all* uploads (not just the rendered
// page), so a large batch keeps polling until every file is done.
func (h *handlers) ingestView() viewmodel.IngestDrawer {
	var vm viewmodel.IngestDrawer
	ups, total, err := h.d.Store.ListUploadsPage(0, 0) // all, newest first
	if err != nil {
		log.Printf("ingest: list uploads: %v", err)
		return vm
	}
	vm.Total = total
	for i, u := range ups {
		switch u.Status {
		case model.UploadStatusDone:
			vm.Done++
		case model.UploadStatusFailed:
			vm.Failed++
		default: // pending | transcribing
			vm.Pending++
			vm.Active = true
		}
		if i < uploadListLimit {
			vm.Uploads = append(vm.Uploads, viewmodel.UploadRow{
				ID: u.ID, Filename: u.Filename, Status: string(u.Status), Error: u.Error,
			})
		}
	}
	return vm
}

// ingest renders the #ingest drawer fragment (also used to poll for status).
//
//	GET /ingest
func (h *handlers) ingest(w http.ResponseWriter, r *http.Request) {
	if _, err := Patch(w, r, components.Ingest(h.ingestView())); err != nil {
		log.Printf("ingest: patch: %v", err)
	}
}

// ingestUpload accepts one or more transcript files, starts processing, and
// patches the refreshed drawer back.
//
//	POST /ingest   (multipart/form-data, field "files")
func (h *handlers) ingestUpload(w http.ResponseWriter, r *http.Request) {
	const (
		maxRequest  = 1 << 30  // 1 GiB total — a large batch of transcripts
		parseMemory = 32 << 20 // keep 32 MiB in memory, spill the rest to temp files
	)
	r.Body = http.MaxBytesReader(w, r.Body, maxRequest)
	if err := r.ParseMultipartForm(parseMemory); err != nil {
		http.Error(w, "invalid upload: "+err.Error(), http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	var accepted []*model.Upload
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			log.Printf("ingest: open %s: %v", fh.Filename, err)
			continue
		}
		u, err := h.d.Ingester.Accept(fh.Filename, f)
		f.Close()
		if err != nil {
			log.Printf("ingest: accept %s: %v", fh.Filename, err)
			continue
		}
		accepted = append(accepted, u)
	}
	log.Printf("ingest: accepted %d of %d files", len(accepted), len(files))
	for _, u := range accepted {
		h.d.Ingester.Start(u)
	}

	if _, err := Patch(w, r, components.Ingest(h.ingestView())); err != nil {
		log.Printf("ingest upload: patch: %v", err)
	}
}
