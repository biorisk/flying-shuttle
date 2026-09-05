// Package project resolves the Flying Shuttle home directory and the split
// on-disk layout: a writing project (outline, evidence, snapshots) is bound
// to a corpus (transcripts, chunks, embeddings, search index, atlas) that
// may be shared by several projects.
//
//	~/.shuttle/                      (or $SHUTTLE_HOME)
//	  config.json                    {"current": "<project>"}
//	  corpora/<corpus>/
//	    corpus.db  corpus.bm25  corpus.hnsw  corpus.lock
//	    uploads/
//	  projects/<project>/
//	    project.db  project.json     {"corpus": "<corpus>"}
//	    outline.md  state.json
//	    branches/<branch>.md
//
// `shuttle migrate split` converts the pre-split `~/.shuttle/<name>/` layout.
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

// DefaultName is the project (and corpus) created when the home dir is empty.
const DefaultName = "default"

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidName reports whether name is acceptable: lowercase alphanumerics plus
// - and _, 1–64 chars, no path separators.
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

// ProjectsDir / CorporaDir are the two top-level trees under home.
func ProjectsDir(home string) string { return filepath.Join(home, "projects") }
func CorporaDir(home string) string  { return filepath.Join(home, "corpora") }

// ProjectPaths are the on-disk locations for one writing project.
type ProjectPaths struct {
	Name      string
	Dir       string
	DB        string // project.db
	JSON      string // project.json (the corpus binding)
	OutlineMD string
	StateJSON string
	BranchDir string
}

// CorpusPaths are the on-disk locations for one corpus.
type CorpusPaths struct {
	Name      string
	Dir       string
	DB        string // corpus.db
	BM25      string
	HNSW      string
	Lock      string
	UploadDir string
}

// Binding is a resolved project plus the corpus it points at (nil = unbound).
type Binding struct {
	Home    string
	Project ProjectPaths
	Corpus  *CorpusPaths
}

// ProjectPathsFor returns the layout for a named project under home.
func ProjectPathsFor(home, name string) ProjectPaths {
	dir := filepath.Join(ProjectsDir(home), name)
	return ProjectPaths{
		Name:      name,
		Dir:       dir,
		DB:        filepath.Join(dir, "project.db"),
		JSON:      filepath.Join(dir, "project.json"),
		OutlineMD: filepath.Join(dir, "outline.md"),
		StateJSON: filepath.Join(dir, "state.json"),
		BranchDir: filepath.Join(dir, "branches"),
	}
}

// CorpusPathsFor returns the layout for a named corpus under home.
func CorpusPathsFor(home, name string) CorpusPaths {
	dir := filepath.Join(CorporaDir(home), name)
	return CorpusPaths{
		Name:      name,
		Dir:       dir,
		DB:        filepath.Join(dir, "corpus.db"),
		BM25:      filepath.Join(dir, "corpus.bm25"),
		HNSW:      filepath.Join(dir, "corpus.hnsw"),
		Lock:      filepath.Join(dir, "corpus.lock"),
		UploadDir: filepath.Join(dir, "uploads"),
	}
}

// EnsureDirs creates a project's directory tree.
func (p ProjectPaths) EnsureDirs() error {
	for _, d := range []string{p.Dir, p.BranchDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDirs creates a corpus's directory tree.
func (c CorpusPaths) EnsureDirs() error {
	for _, d := range []string{c.Dir, c.UploadDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// --- config.json (active project) ---

type config struct {
	Current string `json:"current"`
}

func configPath(home string) string { return filepath.Join(home, "config.json") }

// --- project.json (corpus binding) ---

type binding struct {
	Corpus string `json:"corpus"`
}

// ReadBinding returns the corpus name a project is bound to, or "" if the
// project.json is absent or empty.
func ReadBinding(p ProjectPaths) (string, error) {
	b, err := os.ReadFile(p.JSON)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var bd binding
	if err := json.Unmarshal(b, &bd); err != nil {
		return "", fmt.Errorf("parse %s: %w", p.JSON, err)
	}
	return bd.Corpus, nil
}

// WriteBinding persists a project's corpus binding. An empty name clears it.
func WriteBinding(p ProjectPaths, corpusName string) error {
	if corpusName != "" && !ValidName(corpusName) {
		return fmt.Errorf("invalid corpus name %q", corpusName)
	}
	b, _ := json.MarshalIndent(binding{Corpus: corpusName}, "", "  ")
	return renameio.WriteFile(p.JSON, append(b, '\n'), 0o644)
}

// --- listing ---

func listNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
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

// ListProjects returns the project names under home, sorted.
func ListProjects(home string) ([]string, error) { return listNames(ProjectsDir(home)) }

// ListCorpora returns the corpus names under home, sorted.
func ListCorpora(home string) ([]string, error) { return listNames(CorporaDir(home)) }

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
	names, err := ListProjects(home)
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

// CreateProject makes a project's directory tree, binding it to corpusName
// (also created if missing). Idempotent. An empty corpusName leaves it
// unbound.
func CreateProject(home, name, corpusName string) (ProjectPaths, error) {
	if !ValidName(name) {
		return ProjectPaths{}, fmt.Errorf("invalid project name %q", name)
	}
	p := ProjectPathsFor(home, name)
	if err := p.EnsureDirs(); err != nil {
		return ProjectPaths{}, err
	}
	if corpusName != "" {
		if _, err := CreateCorpus(home, corpusName); err != nil {
			return ProjectPaths{}, err
		}
		if cur, _ := ReadBinding(p); cur == "" {
			if err := WriteBinding(p, corpusName); err != nil {
				return ProjectPaths{}, err
			}
		}
	}
	return p, nil
}

// CreateCorpus makes a corpus's directory tree. Idempotent.
func CreateCorpus(home, name string) (CorpusPaths, error) {
	if !ValidName(name) {
		return CorpusPaths{}, fmt.Errorf("invalid corpus name %q", name)
	}
	c := CorpusPathsFor(home, name)
	if err := c.EnsureDirs(); err != nil {
		return CorpusPaths{}, err
	}
	return c, nil
}

// Resolve prepares the home dir, ensures the current project exists, resolves
// its corpus binding, and returns the Binding. A project with no binding, or
// one naming a corpus directory that is absent, resolves with Corpus == nil
// (the outline still works; evidence/atlas/ingest are hidden).
//
// On a first run (no projects yet) it creates "default" bound to a "default"
// corpus.
func Resolve() (Binding, error) {
	home, err := Home()
	if err != nil {
		return Binding{}, err
	}
	if err := os.MkdirAll(ProjectsDir(home), 0o755); err != nil {
		return Binding{}, err
	}
	if err := os.MkdirAll(CorporaDir(home), 0o755); err != nil {
		return Binding{}, err
	}

	name, err := Current(home)
	if err != nil {
		return Binding{}, err
	}

	// First run: nothing exists yet -> default project + default corpus.
	existing, _ := ListProjects(home)
	corpusName := ""
	if len(existing) == 0 {
		corpusName = DefaultName
	}

	p, err := CreateProject(home, name, corpusName)
	if err != nil {
		return Binding{}, err
	}
	if err := SetCurrent(home, name); err != nil {
		return Binding{}, err
	}

	b := Binding{Home: home, Project: p}
	bound, err := ReadBinding(p)
	if err != nil {
		return Binding{}, err
	}
	if bound != "" {
		cp := CorpusPathsFor(home, bound)
		if fi, err := os.Stat(cp.Dir); err == nil && fi.IsDir() {
			if err := cp.EnsureDirs(); err != nil {
				return Binding{}, err
			}
			b.Corpus = &cp
		}
	}
	return b, nil
}
