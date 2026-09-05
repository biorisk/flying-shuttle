package atlas

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// CachedDigest is one row of the content-addressed digest cache
// (atlas_digest): an LLM (or extractive) summary of a fixed set of immutable
// chunks, reused across builds. See atlas_persistence_plan.md.
type CachedDigest struct {
	InputHash string
	Kind      string // "region" | "transcript"
	Digest    Digest
	Vec       []float32
	Source    string // "llm:<model>" (final) | "extractive" (provisional, retried with an LLM)
}

// digestInputHash hashes the identity of a summariser input. parts are joined
// with NULs; chunkIDs is sorted so order doesn't matter.
func digestInputHash(kind string, chunkIDs []string, parts ...string) string {
	ids := append([]string(nil), chunkIDs...)
	sort.Strings(ids)
	h := sha256.New()
	h.Write([]byte(kind))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	for _, id := range ids {
		h.Write([]byte{0})
		h.Write([]byte(id))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func regionDigestHash(r *Region) string {
	ids := make([]string, len(r.Members))
	for i, m := range r.Members {
		ids[i] = m.ChunkID
	}
	return digestInputHash("region", ids)
}

func transcriptDigestHash(sourceFile string, chunkIDs []string) string {
	return digestInputHash("transcript", chunkIDs, sourceFile)
}

// summariserSource is the Digest.Source a Summariser will stamp — used to
// decide whether a cached provisional ("extractive") digest can be upgraded.
func summariserSource(summ Summariser) string {
	if _, ok := summ.(*LLMSummariser); ok {
		return "llm"
	}
	return "extractive"
}

// reusable reports whether a cached digest can be used as-is rather than
// recomputed. A real LLM digest is always reusable; a provisional extractive
// one is reusable only when the summariser wouldn't do better (i.e. it's also
// extractive).
func (c CachedDigest) reusable(summ Summariser) bool {
	if strings.HasPrefix(c.Source, "llm:") {
		return true
	}
	return summariserSource(summ) != "llm"
}

// digestCache batch-fetches and de-dupes cache rows for a build phase.
type digestCache struct {
	store Store
	rows  map[string]CachedDigest
}

func newDigestCache(ctx context.Context, s Store, hashes []string) (*digestCache, error) {
	_ = ctx
	rows, err := s.GetDigests(hashes)
	if err != nil {
		return nil, err
	}
	return &digestCache{store: s, rows: rows}, nil
}

func (dc *digestCache) get(hash string) (CachedDigest, bool) {
	c, ok := dc.rows[hash]
	return c, ok
}

func (dc *digestCache) put(c CachedDigest) error {
	if err := dc.store.PutDigest(c); err != nil {
		return err
	}
	dc.rows[c.InputHash] = c
	return nil
}
