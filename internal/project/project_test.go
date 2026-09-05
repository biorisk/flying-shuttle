package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidName(t *testing.T) {
	ok := []string{"default", "my-book", "book_2", "a", "abc123"}
	bad := []string{"", "Book", "../evil", "a/b", "has space", "-lead", string(make([]byte, 65))}
	for _, n := range ok {
		if !ValidName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range bad {
		if ValidName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
}

func TestHomeOverride(t *testing.T) {
	t.Setenv("SHUTTLE_HOME", "/tmp/shuttle-test-home")
	h, err := Home()
	if err != nil || h != "/tmp/shuttle-test-home" {
		t.Fatalf("Home() = %q, %v", h, err)
	}
}

func TestResolveAndSwitch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SHUTTLE_HOME", home)

	// First resolve creates + selects "default", bound to the default corpus.
	b, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if b.Project.Name != DefaultName {
		t.Fatalf("first project = %q, want %q", b.Project.Name, DefaultName)
	}
	if b.Corpus == nil || b.Corpus.Name != DefaultName {
		t.Fatalf("first project should bind the default corpus, got %+v", b.Corpus)
	}
	for _, d := range []string{b.Project.Dir, b.Project.BranchDir, b.Corpus.Dir, b.Corpus.UploadDir} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Fatalf("expected dir %s", d)
		}
	}
	if b.Project.DB != filepath.Join(home, "projects", "default", "project.db") {
		t.Fatalf("project DB path wrong: %s", b.Project.DB)
	}
	if b.Corpus.DB != filepath.Join(home, "corpora", "default", "corpus.db") {
		t.Fatalf("corpus DB path wrong: %s", b.Corpus.DB)
	}

	// Create + switch to a second project (its own corpus).
	if _, err := CreateProject(home, "book-two", "book-two"); err != nil {
		t.Fatal(err)
	}
	if err := SetCurrent(home, "book-two"); err != nil {
		t.Fatal(err)
	}
	if cur, _ := Current(home); cur != "book-two" {
		t.Fatalf("current = %q after switch", cur)
	}

	names, _ := ListProjects(home)
	if len(names) != 2 || names[0] != "book-two" || names[1] != "default" {
		t.Fatalf("ListProjects() = %v", names)
	}

	b2, _ := Resolve()
	if b2.Project.Name != "book-two" {
		t.Fatalf("Resolve() after switch = %q", b2.Project.Name)
	}
}

func TestResolve_unboundProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SHUTTLE_HOME", home)

	// A project directory with no project.json resolves unbound.
	if _, err := CreateProject(home, "loner", ""); err != nil {
		t.Fatal(err)
	}
	if err := SetCurrent(home, "loner"); err != nil {
		t.Fatal(err)
	}
	b, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if b.Corpus != nil {
		t.Fatalf("expected unbound, got corpus %+v", b.Corpus)
	}
}

func TestCurrent_emptyHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SHUTTLE_HOME", home)
	cur, err := Current(home)
	if err != nil || cur != DefaultName {
		t.Fatalf("Current(empty) = %q, %v", cur, err)
	}
}
