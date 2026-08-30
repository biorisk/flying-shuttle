package web

import (
	"log"
	"net/http"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/web/components"
	"github.com/biorisk/flying-shuttle/internal/web/viewmodel"
)

const uploadListLimit = 100

// ingestView reads the current uploads into a drawer view model.
func (h *handlers) ingestView() viewmodel.IngestDrawer {
	var vm viewmodel.IngestDrawer
	ups, _, err := h.d.Store.ListUploadsPage(uploadListLimit, 0)
	if err != nil {
		log.Printf("ingest: list uploads: %v", err)
		return vm
	}
	for _, u := range ups {
		vm.Uploads = append(vm.Uploads, viewmodel.UploadRow{
			ID: u.ID, Filename: u.Filename, Status: string(u.Status), Error: u.Error,
		})
		if u.Status == model.UploadStatusPending || u.Status == model.UploadStatusTranscribing {
			vm.Active = true
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
	const maxUpload = 100 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		http.Error(w, "invalid upload", http.StatusBadRequest)
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
	for _, u := range accepted {
		h.d.Ingester.Start(u)
	}

	if _, err := Patch(w, r, components.Ingest(h.ingestView())); err != nil {
		log.Printf("ingest upload: patch: %v", err)
	}
}
