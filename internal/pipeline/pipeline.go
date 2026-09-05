// Package pipeline is the transcript ingestion pipeline shared by the JSON API
// and the server-rendered UI: accept an uploaded transcript file, parse it into
// segments, chunk it, persist and index the chunks. It lives in its own package
// (rather than internal/ingest) because indexing pulls in internal/search,
// which already depends on internal/ingest.
package pipeline

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/biorisk/flying-shuttle/internal/corpus"
	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/search"
	"github.com/google/uuid"
)

// Ingester accepts and processes transcript uploads.
type Ingester struct {
	Store       corpus.Store
	UploadDir   string
	Index       *search.HybridIndex
	AfterIngest func() // optional; nudges the embedding backfiller. Must not block.
}

// ErrUnsupportedFormat is returned by Accept for a non-transcript extension.
type ErrUnsupportedFormat struct{ Ext string }

func (e ErrUnsupportedFormat) Error() string {
	return fmt.Sprintf("unsupported format: %s (transcripts only: .txt, .md, .markdown, .text)", e.Ext)
}

// Accept validates the extension, writes the file under UploadDir, and creates
// a pending Upload row. It does not start processing — call Start.
func (in *Ingester) Accept(filename string, src io.Reader) (*model.Upload, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if !ingest.IsTextTranscript(ext) {
		return nil, ErrUnsupportedFormat{Ext: ext}
	}

	id := uuid.NewString()
	dest := filepath.Join(in.UploadDir, id+ext)
	f, err := os.Create(dest)
	if err != nil {
		return nil, fmt.Errorf("save file: %w", err)
	}
	written, copyErr := io.Copy(f, src)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(dest)
		if copyErr != nil {
			return nil, fmt.Errorf("write file: %w", copyErr)
		}
		return nil, fmt.Errorf("close file: %w", closeErr)
	}

	u := &model.Upload{
		ID:        id,
		Filename:  filename,
		Format:    strings.TrimPrefix(ext, "."),
		SizeBytes: written,
		Status:    model.UploadStatusPending,
	}
	if err := in.Store.CreateUpload(u); err != nil {
		os.Remove(dest)
		return nil, err
	}
	return u, nil
}

// IngestPath ingests transcript files that already live on the machine running
// shuttle. path may be a single transcript file or a directory (walked
// recursively); only files with a transcript extension are accepted. Accepted
// files are copied into UploadDir and processing is started for each. It
// returns the accepted uploads and the paths that were skipped (wrong
// extension or unreadable).
func (in *Ingester) IngestPath(path string) (accepted []*model.Upload, skipped []string, err error) {
	abs, err := filepath.Abs(expandUser(strings.TrimSpace(path)))
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, nil, err
	}

	var files []string
	if info.IsDir() {
		walkErr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if ingest.IsTextTranscript(strings.ToLower(filepath.Ext(p))) {
				files = append(files, p)
			} else {
				skipped = append(skipped, p)
			}
			return nil
		})
		if walkErr != nil {
			return nil, skipped, walkErr
		}
		sort.Strings(files)
	} else {
		if !ingest.IsTextTranscript(strings.ToLower(filepath.Ext(abs))) {
			return nil, []string{abs}, ErrUnsupportedFormat{Ext: filepath.Ext(abs)}
		}
		files = []string{abs}
	}

	for _, p := range files {
		f, oerr := os.Open(p)
		if oerr != nil {
			skipped = append(skipped, p)
			continue
		}
		u, aerr := in.Accept(filepath.Base(p), f)
		f.Close()
		if aerr != nil {
			skipped = append(skipped, p)
			continue
		}
		accepted = append(accepted, u)
	}
	for _, u := range accepted {
		in.Start(u)
	}
	return accepted, skipped, nil
}

// expandUser resolves a leading ~ to the current user's home directory.
func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// DiskPath returns the on-disk location of an upload's file.
func (in *Ingester) DiskPath(u *model.Upload) string {
	return filepath.Join(in.UploadDir, u.ID+"."+u.Format)
}

// Start runs ingestion for one pending upload in the background.
func (in *Ingester) Start(u *model.Upload) {
	go in.run(u.ID, in.DiskPath(u), u.Filename)
}

// StartPending starts every upload still in the pending state and returns how
// many were kicked off. Safe to call repeatedly.
func (in *Ingester) StartPending(ids ...string) (int, error) {
	if len(ids) == 0 {
		all, err := in.Store.ListUploads()
		if err != nil {
			return 0, err
		}
		for _, u := range all {
			if u.Status == model.UploadStatusPending {
				ids = append(ids, u.ID)
			}
		}
	}
	started := 0
	for _, id := range ids {
		u, err := in.Store.GetUpload(id)
		if err != nil || u.Status != model.UploadStatusPending {
			continue
		}
		in.Start(u)
		started++
	}
	return started, nil
}

func (in *Ingester) run(uploadID, filePath, sourceName string) {
	_ = in.Store.UpdateUploadStatus(uploadID, model.UploadStatusTranscribing, "")

	raw, err := os.ReadFile(filePath)
	if err != nil {
		_ = in.Store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, "read transcript: "+err.Error())
		return
	}

	segments := ingest.ParseTranscript(string(raw))
	if len(segments) == 0 {
		_ = in.Store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, "transcript is empty")
		return
	}
	for i := range segments {
		seg := &segments[i]
		seg.ID = uuid.NewString()
		seg.UploadID = uploadID
		if err := in.Store.CreateTranscriptSegment(seg); err != nil {
			_ = in.Store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, "store segment: "+err.Error())
			return
		}
	}

	chunks := ingest.ChunkTranscript(sourceName, segments)
	if err := in.storeAndIndex(chunks); err != nil {
		_ = in.Store.UpdateUploadStatus(uploadID, model.UploadStatusFailed, err.Error())
		return
	}
	_ = in.Store.UpdateUploadStatus(uploadID, model.UploadStatusDone, "")
}

// Rechunk re-runs transcript chunking for an upload's stored segments.
func (in *Ingester) Rechunk(uploadID string) ([]model.Chunk, error) {
	u, err := in.Store.GetUpload(uploadID)
	if err != nil {
		return nil, err
	}
	segs, err := in.Store.ListTranscriptSegments(uploadID)
	if err != nil {
		return nil, err
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("no transcript segments to chunk")
	}
	chunks := ingest.ChunkTranscript(u.Filename, segs)
	if err := in.storeAndIndex(chunks); err != nil {
		return nil, err
	}
	return chunks, nil
}

func (in *Ingester) storeAndIndex(chunks []model.Chunk) error {
	for i := range chunks {
		if err := in.Store.CreateChunk(&chunks[i]); err != nil {
			return fmt.Errorf("store chunk: %w", err)
		}
		if in.Index != nil {
			in.Index.IndexChunk(&chunks[i])
		}
	}
	if len(chunks) > 0 && in.AfterIngest != nil {
		in.AfterIngest()
	}
	return nil
}
