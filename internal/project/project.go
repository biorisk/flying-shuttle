// Package project resolves the Flying Shuttle home directory and the
// per-project subfolders inside it. Every project is a directory under the
// home dir holding its own SQLite database, search indexes, uploads, and the
// human-readable working-doc mirror (outline.md + state.json).
//
//	~/.shuttle/                 (or $SHUTTLE_HOME)
//	  config.json               {"current": "<name>"}
//	  <name>/
//	    shuttle.db  shuttle.db-wal  shuttle.db-shm
//	    shuttle.bm25  shuttle.hnsw
//	    uploads/
//	    outline.md  state.json
//	    branches/<branch>.md
package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/google/renameio"
)

// DefaultName is the project created (and selected) when the home dir is empty.
const DefaultName = "default"

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidName reports whether name is an acceptable project name: lowercase
// alphanumerics plus - and _, 1–64 chars, no path separators.
func ValidName(name string) bool { return nameRE.MatchString(name) }

// Home is the Flying Shuttle root directory: $SHUTTLE_HOME, or ~/.shuttle.
func Home() (string, error) {
	if v := os.Getenv("SHUTTLE_HOME"); v != "" {
		return v, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(h, ".shuttle"), nil
}

// Paths are the on-disk locations for one project.
type Paths struct {
	Name      string
	Dir       string
	DB        string
	BM25      string
	HNSW      string
	UploadDir string
	OutlineMD string
	StateJSON string
	BranchDir string
}

// PathsFor returns the Paths for a named project under home.
func PathsFor(home, name string) Paths {
	dir := filepath.Join(home, name)
	return Paths{
		Name:      name,
		Dir:       dir,
		DB:        filepath.Join(dir, "shuttle.db"),
		BM25:      filepath.Join(dir, "shuttle.bm25"),
		HNSW:      filepath.Join(dir, "shuttle.hnsw"),
		UploadDir: filepath.Join(dir, "uploads"),
		OutlineMD: filepath.Join(dir, "outline.md"),
		StateJSON: filepath.Join(dir, "state.json"),
		BranchDir: filepath.Join(dir, "branches"),
	}
}

// EnsureDirs creates the project directory tree.
func (p Paths) EnsureDirs() error {
	for _, d := range []string{p.Dir, p.UploadDir, p.BranchDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// config is the persisted selection of the active project.
type config struct {
	Current string `json:"current"`
}

func configPath(home string) string { return filepath.Join(home, "config.json") }

// List returns the project names present under home (any subdirectory), sorted.
func List(home string) ([]string, error) {
	entries, err := os.ReadDir(home)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && ValidName(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// Current reads the active project name from config.json, falling back to the
// first existing project, then DefaultName.
func Current(home string) (string, error) {
	b, err := os.ReadFile(configPath(home))
	if err == nil {
		var c config
		if json.Unmarshal(b, &c) == nil && ValidName(c.Current) {
			return c.Current, nil
		}
	}
	names, err := List(home)
	if err != nil {
		return "", err
	}
	if len(names) > 0 {
		return names[0], nil
	}
	return DefaultName, nil
}

// SetCurrent persists name as the active project.
func SetCurrent(home, name string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid project name %q", name)
	}
	b, _ := json.MarshalIndent(config{Current: name}, "", "  ")
	return renameio.WriteFile(configPath(home), append(b, '\n'), 0o644)
}

// Create makes the directory tree for a new project. It is not an error if the
// project already exists.
func Create(home, name string) (Paths, error) {
	if !ValidName(name) {
		return Paths{}, fmt.Errorf("invalid project name %q", name)
	}
	p := PathsFor(home, name)
	if err := p.EnsureDirs(); err != nil {
		return Paths{}, err
	}
	return p, nil
}

// Resolve prepares the home dir, ensures the current project exists, persists
// the selection, and returns its Paths.
func Resolve() (Paths, error) {
	home, err := Home()
	if err != nil {
		return Paths{}, err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return Paths{}, err
	}
	name, err := Current(home)
	if err != nil {
		return Paths{}, err
	}
	p, err := Create(home, name)
	if err != nil {
		return Paths{}, err
	}
	if err := SetCurrent(home, name); err != nil {
		return Paths{}, err
	}
	return p, nil
}
