package project

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// ownedLocks tracks corpus-lock paths this process holds, so a second acquire
// of the same lock is re-entrant rather than being mistaken for a stale one.
var ownedLocks = struct {
	sync.Mutex
	set map[string]bool
}{set: map[string]bool{}}

// CorpusLock is an advisory single-writer lock on a corpus, held for the
// lifetime of the process that acquired it. It is a file at
// corpora/<name>/corpus.lock containing the holder's "pid\nproject".
//
// The first session to open a corpus becomes its writer (ingest, backfill,
// index snapshots, atlas rebuilds). Later sessions open the corpus read-only.
type CorpusLock struct {
	path  string
	owned bool
}

// LockInfo describes who currently holds a corpus lock.
type LockInfo struct {
	PID     int
	Project string
}

// AcquireCorpusLock tries to take the lock at path on behalf of project.
//
//   - held == true: this process now owns it; call Release at shutdown.
//   - held == false: another live process owns it; info names the holder and
//     the caller should open the corpus read-only.
//
// A lock file whose PID is no longer running is treated as stale and reclaimed.
func AcquireCorpusLock(path, project string) (lock *CorpusLock, held bool, info LockInfo, err error) {
	ownedLocks.Lock()
	alreadyOurs := ownedLocks.set[path]
	ownedLocks.Unlock()
	if alreadyOurs {
		return &CorpusLock{path: path, owned: true}, true, LockInfo{PID: os.Getpid(), Project: project}, nil
	}

	for attempt := 0; attempt < 2; attempt++ {
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if e == nil {
			fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), project)
			f.Close()
			ownedLocks.Lock()
			ownedLocks.set[path] = true
			ownedLocks.Unlock()
			return &CorpusLock{path: path, owned: true}, true, LockInfo{PID: os.Getpid(), Project: project}, nil
		}
		if !errors.Is(e, os.ErrExist) {
			return nil, false, LockInfo{}, e
		}
		cur, readErr := readLock(path)
		if readErr != nil {
			return nil, false, LockInfo{}, readErr
		}
		if cur.PID > 0 && pidAlive(cur.PID) {
			return &CorpusLock{path: path}, false, cur, nil
		}
		// Stale (dead pid, or our own leftover) — remove and retry once.
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, false, LockInfo{}, err
		}
	}
	return nil, false, LockInfo{}, fmt.Errorf("could not acquire corpus lock %s after reclaiming a stale one", path)
}

// Release removes the lock file if this process owns it.
func (l *CorpusLock) Release() {
	if l != nil && l.owned {
		_ = os.Remove(l.path)
		l.owned = false
		ownedLocks.Lock()
		delete(ownedLocks.set, l.path)
		ownedLocks.Unlock()
	}
}

func readLock(path string) (LockInfo, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return LockInfo{}, err
	}
	lines := strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)
	pid, _ := strconv.Atoi(strings.TrimSpace(lines[0]))
	info := LockInfo{PID: pid}
	if len(lines) > 1 {
		info.Project = strings.TrimSpace(lines[1])
	}
	return info, nil
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, signal 0 probes existence without delivering anything.
	// EPERM means the process exists but is owned by someone else.
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM)
}
