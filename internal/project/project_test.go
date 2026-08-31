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

	// First resolve creates + selects "default".
	p, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != DefaultName {
		t.Fatalf("first project = %q, want %q", p.Name, DefaultName)
	}
	for _, d := range []string{p.Dir, p.UploadDir, p.BranchDir} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Fatalf("expected dir %s", d)
		}
	}
	if p.DB != filepath.Join(home, "default", "shuttle.db") {
		t.Fatalf("DB path wrong: %s", p.DB)
	}

	// Create + switch to a second project.
	if _, err := Create(home, "book-two"); err != nil {
		t.Fatal(err)
	}
	if err := SetCurrent(home, "book-two"); err != nil {
		t.Fatal(err)
	}
	cur, _ := Current(home)
	if cur != "book-two" {
		t.Fatalf("current = %q after switch", cur)
	}

	names, _ := List(home)
	if len(names) != 2 || names[0] != "book-two" || names[1] != "default" {
		t.Fatalf("List() = %v", names)
	}

	// A resolve now honours the switched selection.
	p2, _ := Resolve()
	if p2.Name != "book-two" {
		t.Fatalf("Resolve() after switch = %q", p2.Name)
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
