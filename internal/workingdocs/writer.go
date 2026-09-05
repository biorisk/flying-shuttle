package workingdocs

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/biorisk/flying-shuttle/internal/model"
	"github.com/biorisk/flying-shuttle/internal/doc"
	"github.com/google/renameio"
)

// State is everything the working docs mirror: the DAG plus the branch list
// (which lives outside SnapshotData).
type State struct {
	Project  string                `json:"project"`
	SavedAt  time.Time             `json:"saved_at"`
	Data     *model.SnapshotData   `json:"data"`
	Branches []model.BranchSummary `json:"branches"`
}

// gather reads the full project state from the doc.
func gather(s doc.Store, project string) (*State, error) {
	data, err := s.ExportState()
	if err != nil {
		return nil, err
	}
	branches, err := s.ListBranches()
	if err != nil {
		return nil, err
	}
	return &State{Project: project, SavedAt: time.Now().UTC(), Data: data, Branches: branches}, nil
}

// Write renders and atomically writes outline.md + state.json for a project.
func Write(outlineMD, stateJSON string, st *State) error {
	md := RenderOutline(st.Project, st.Data, st.Branches)
	if err := renameio.WriteFile(outlineMD, []byte(md), 0o644); err != nil {
		return err
	}
	js, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return renameio.WriteFile(stateJSON, append(js, '\n'), 0o644)
}

// LoadState reads a previously written state.json (for recovery).
func LoadState(stateJSON string) (*State, error) {
	b, err := os.ReadFile(stateJSON)
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Flusher keeps outline.md + state.json in sync with the doc. It polls
// (cheap for a book-sized DAG), writes only when the content changed, and does
// a final write when ctx is cancelled.
type Flusher struct {
	Store     doc.Store
	Project   string
	OutlineMD string
	StateJSON string
	Interval  time.Duration // default 3s
	OnWrite   func()        // called after each successful write (state changed)

	lastHash [32]byte
}

func (f *Flusher) Run(ctx context.Context) {
	iv := f.Interval
	if iv <= 0 {
		iv = 3 * time.Second
	}
	t := time.NewTicker(iv)
	defer t.Stop()

	f.flush() // write once at startup so the files exist
	for {
		select {
		case <-ctx.Done():
			f.flush()
			return
		case <-t.C:
			f.flush()
		}
	}
}

func (f *Flusher) flush() {
	st, err := gather(f.Store, f.Project)
	if err != nil {
		log.Printf("workingdocs: gather: %v", err)
		return
	}
	js, err := json.Marshal(st.Data) // hash the DAG + branches, not the timestamp
	if err != nil {
		return
	}
	bj, _ := json.Marshal(st.Branches)
	h := sha256.Sum256(append(js, bj...))
	if h == f.lastHash {
		return
	}
	if err := Write(f.OutlineMD, f.StateJSON, st); err != nil {
		log.Printf("workingdocs: write: %v", err)
		return
	}
	f.lastHash = h
	if f.OnWrite != nil {
		f.OnWrite()
	}
}
