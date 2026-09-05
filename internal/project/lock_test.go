package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCorpusLock_acquireReleaseReacquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.lock")

	l1, held, _, err := AcquireCorpusLock(path, "book-a")
	if err != nil || !held {
		t.Fatalf("first acquire: held=%v err=%v", held, err)
	}
	// Re-acquiring our own lock is re-entrant.
	_, held2, _, err := AcquireCorpusLock(path, "book-a")
	if err != nil || !held2 {
		t.Fatalf("re-entrant acquire: held=%v err=%v", held2, err)
	}

	l1.Release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock file should be gone after Release: %v", err)
	}

	_, held3, _, err := AcquireCorpusLock(path, "book-c")
	if err != nil || !held3 {
		t.Fatalf("re-acquire after release: held=%v err=%v", held3, err)
	}
}

func TestCorpusLock_seesLiveHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.lock")
	// pid 1 (init/launchd) is always alive and is not us.
	os.WriteFile(path, []byte("1\nother-book\n"), 0o644)

	_, held, info, err := AcquireCorpusLock(path, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if held {
		t.Fatal("should not acquire a lock held by a live pid")
	}
	if info.PID != 1 || info.Project != "other-book" {
		t.Fatalf("holder info: %+v", info)
	}
}

func TestCorpusLock_reclaimsStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.lock")
	os.WriteFile(path, []byte("999999999\nghost-project\n"), 0o644)

	_, held, _, err := AcquireCorpusLock(path, "book")
	if err != nil || !held {
		t.Fatalf("should reclaim stale lock: held=%v err=%v", held, err)
	}
}
