package atlas

import (
	"context"
	"sync"
	"time"
)

// ErrBuilding is returned by Service.Rebuild when a build is already running.
var ErrBuilding = errBuilding{}

type errBuilding struct{}

func (errBuilding) Error() string { return "atlas: a build is already in progress" }

// Phase is a coarse progress label for the running build.
type Phase string

const (
	PhaseIdle    Phase = "idle"
	PhaseRunning Phase = "running"
)

// Status is a snapshot of the Atlas service state for the UI.
type Status struct {
	Phase      Phase
	Building   bool
	LastBuilt  time.Time
	LastError  string
	Regions    int
	Links      int
	ChunkCount int
}

// Service owns the single current Atlas build and serialises rebuilds. It does
// not run anything in the background on its own — Rebuild is invoked on demand
// (POST /atlas/rebuild) and the caller runs it in a goroutine tied to the
// app's lifecycle context.
type Service struct {
	Builder *Builder
	// BaseCtx is the app-lifecycle context used by StartRebuild so a build
	// aborts cleanly on shutdown. Defaults to context.Background().
	BaseCtx context.Context

	mu       sync.Mutex
	building bool
	current  *Build
	index    *RegionIndex
	lastErr  string
	lastAt   time.Time
}

// LoadCurrent pulls the last ready build from the store into memory. Call once
// at startup. A missing build is not an error.
func (s *Service) LoadCurrent() error {
	b, err := s.Builder.Store.CurrentBuild()
	if err == ErrNoBuild {
		return nil
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.current, s.index, s.lastAt = b, LoadRegionIndex(b), b.CreatedAt
	s.mu.Unlock()
	return nil
}

// Rebuild runs a full build and blocks until it finishes. It returns
// ErrBuilding immediately if one is already running. ctx cancellation aborts
// the build cleanly.
func (s *Service) Rebuild(ctx context.Context) error {
	if !s.acquire() {
		return ErrBuilding
	}
	return s.runBuild(ctx)
}

// StartRebuild launches a rebuild in the background using BaseCtx, returning
// false if one is already running. This is what POST /atlas/rebuild calls.
func (s *Service) StartRebuild() bool {
	if !s.acquire() {
		return false
	}
	ctx := s.BaseCtx
	if ctx == nil {
		ctx = context.Background()
	}
	go s.runBuild(ctx)
	return true
}

func (s *Service) acquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.building {
		return false
	}
	s.building = true
	return true
}

func (s *Service) runBuild(ctx context.Context) error {
	build, idx, err := s.Builder.Build(ctx)
	s.mu.Lock()
	s.building = false
	s.lastAt = time.Now()
	if err != nil {
		s.lastErr = err.Error()
	} else {
		s.current, s.index, s.lastErr = build, idx, ""
	}
	s.mu.Unlock()
	return err
}

// Current returns the in-memory build and its region index (may be nil).
func (s *Service) Current() (*Build, *RegionIndex) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current, s.index
}

// RankRegions ranks the current build's regions against a query vector.
func (s *Service) RankRegions(query []float32, limit int) []RegionHit {
	_, idx := s.Current()
	if idx == nil {
		return nil
	}
	return idx.Rank(query, limit)
}

// Status returns a UI-facing snapshot.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Phase: PhaseIdle, Building: s.building, LastBuilt: s.lastAt, LastError: s.lastErr}
	if s.building {
		st.Phase = PhaseRunning
	}
	if s.current != nil {
		st.Regions = len(s.current.Regions)
		st.Links = len(s.current.Links)
		st.ChunkCount = s.current.ChunkCount
	}
	return st
}
