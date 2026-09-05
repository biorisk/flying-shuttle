package atlas

import (
	"fmt"
	"math"
	"testing"
)

func TestTagFiles_PrimaryPlusMargin(t *testing.T) {
	regions := []Region{
		// file "f1": 3 chunks in r1, 2 in r2 -> r1 primary (60%), r2 within margin.
		{ID: "r1", Members: []Member{{ChunkID: "a1"}, {ChunkID: "a2"}, {ChunkID: "a3"}}},
		{ID: "r2", Members: []Member{{ChunkID: "a4"}, {ChunkID: "a5"}, {ChunkID: "b1"}}},
	}
	fileOf := map[string]string{
		"a1": "f1", "a2": "f1", "a3": "f1", "a4": "f1", "a5": "f1",
		"b1": "f2",
	}

	tags := TagFiles(regions, fileOf, FileTagParams{MinShare: 0.1, Margin: 0.5, MaxExtra: 2})

	if got := tags["f1"]; len(got) != 2 || got[0].RegionID != "r1" || got[1].RegionID != "r2" {
		t.Fatalf("f1: want [r1 r2], got %+v", got)
	}
	if got := tags["f2"]; len(got) != 1 || got[0].RegionID != "r2" {
		t.Fatalf("f2: want [r2] only, got %+v", got)
	}
}

func TestTagFiles_MinShareExcludesExtras(t *testing.T) {
	var r1Members []Member
	fileOf := map[string]string{}
	for i := 0; i < 9; i++ {
		id := fmt.Sprintf("a%d", i)
		r1Members = append(r1Members, Member{ChunkID: id})
		fileOf[id] = "f1"
	}
	fileOf["b0"] = "f1"
	regions := []Region{
		{ID: "r1", Members: r1Members},
		{ID: "r2", Members: []Member{{ChunkID: "b0"}}}, // 10% of f1's chunks -> below MinShare
	}

	tags := TagFiles(regions, fileOf, FileTagParams{MinShare: 0.15, Margin: 1, MaxExtra: 2})
	if got := tags["f1"]; len(got) != 1 || got[0].RegionID != "r1" {
		t.Fatalf("want only the primary tag (r2's 10%% share is below MinShare), got %+v", got)
	}
}

func TestTagFiles_MaxExtraCaps(t *testing.T) {
	regions := []Region{
		{ID: "r1", Members: []Member{{ChunkID: "a"}}},
		{ID: "r2", Members: []Member{{ChunkID: "b"}}},
		{ID: "r3", Members: []Member{{ChunkID: "c"}}},
		{ID: "r4", Members: []Member{{ChunkID: "d"}}},
	}
	fileOf := map[string]string{"a": "f1", "b": "f1", "c": "f1", "d": "f1"}

	tags := TagFiles(regions, fileOf, FileTagParams{MinShare: 0, Margin: 1, MaxExtra: 1})
	if len(tags["f1"]) != 2 { // primary + exactly one extra, despite three tied qualifiers
		t.Fatalf("want primary + 1 extra, got %+v", tags["f1"])
	}
}

func TestBuildChunkEdges_SparseNotClique(t *testing.T) {
	// Five near-identical chunks: a flat threshold would link every pair
	// (10 edges); top-K=2 should keep it sparse.
	ids := []string{"a", "b", "c", "d", "e"}
	vecs := [][]float32{
		{1, 0, 0}, {0.99, 0.01, 0}, {0.98, 0.02, 0}, {0.97, 0.03, 0}, {0.96, 0.04, 0},
	}
	edges := BuildChunkEdges(ids, vecs, GraphEdgeParams{K: 2, MinWeight: 0.5})
	if len(edges) == 0 {
		t.Fatal("expected some edges")
	}
	if len(edges) >= 10 {
		t.Fatalf("top-K sparsification should avoid a clique, got %d edges", len(edges))
	}
	for _, e := range edges {
		if e.Weight < 0.5 {
			t.Fatalf("edge %+v below MinWeight", e)
		}
	}
}

func TestBuildChunkEdges_SkipsMissingVectors(t *testing.T) {
	ids := []string{"a", "b", "c"}
	vecs := [][]float32{{1, 0}, nil, {1, 0}}
	edges := BuildChunkEdges(ids, vecs, GraphEdgeParams{})
	for _, e := range edges {
		if e.A == "b" || e.B == "b" {
			t.Fatalf("chunk with no vector should not appear in an edge: %+v", e)
		}
	}
}

func TestBuildChunkEdges_KeepTopFraction(t *testing.T) {
	// 12 chunks in two well-separated blobs of 6, mutually near-identical
	// within a blob (so top-K alone keeps all 15 pairs per blob = 30 edges).
	var ids []string
	var vecs [][]float32
	for g := 0; g < 2; g++ {
		for i := 0; i < 6; i++ {
			ids = append(ids, string(rune('A'+g))+string(rune('0'+i)))
			v := []float32{0, 0}
			v[g] = 1 - float32(i)*0.001 // tiny jitter so weights aren't all tied
			v[1-g] = float32(i) * 0.0005
			vecs = append(vecs, v)
		}
	}

	full := BuildChunkEdges(ids, vecs, GraphEdgeParams{K: 11, MinWeight: 0})
	trimmed := BuildChunkEdges(ids, vecs, GraphEdgeParams{K: 11, MinWeight: 0, KeepTopFraction: 0.25})

	wantN := int(math.Ceil(float64(len(full)) * 0.25))
	if len(trimmed) != wantN {
		t.Fatalf("want %d edges after a 25%% keep of %d, got %d", wantN, len(full), len(trimmed))
	}
	// The kept edges must be exactly the strongest ones from the untrimmed set.
	for i, e := range trimmed {
		if e != full[i] {
			t.Fatalf("trimmed[%d] = %+v, want strongest edge %+v", i, e, full[i])
		}
	}
}

func TestBuildTranscriptEdges_UsesMaxNotSum(t *testing.T) {
	// Two chunk edges cross f1<->f2: 0.9 and 0.4. Weight must be the max
	// (0.9), not the sum (1.3) or mean (0.65).
	chunkEdges := []ChunkEdge{
		{A: "a1", B: "b1", Weight: 0.9},
		{A: "a2", B: "b2", Weight: 0.4},
	}
	fileOf := map[string]string{"a1": "f1", "a2": "f1", "b1": "f2", "b2": "f2"}

	edges := BuildTranscriptEdges(chunkEdges, fileOf, TranscriptEdgeParams{})
	if len(edges) != 1 || edges[0].Weight != 0.9 {
		t.Fatalf("want one f1-f2 edge at weight 0.9 (the max), got %+v", edges)
	}
}

func TestBuildTranscriptEdges_IgnoresIntraFile(t *testing.T) {
	chunkEdges := []ChunkEdge{{A: "a1", B: "a2", Weight: 0.99}}
	fileOf := map[string]string{"a1": "f1", "a2": "f1"}

	edges := BuildTranscriptEdges(chunkEdges, fileOf, TranscriptEdgeParams{})
	if len(edges) != 0 {
		t.Fatalf("a chunk edge within one file must not become a transcript edge, got %+v", edges)
	}
}

func TestBuildTranscriptEdges_KeepsTopKPerTranscript(t *testing.T) {
	// 6 files, every pair cross-linked (15 possible edges) with a distinct
	// chunk-edge weight per pair — K=2 should sparsify well below 15, the
	// same clique-breaking property BuildChunkEdges has at chunk granularity.
	files := []string{"f0", "f1", "f2", "f3", "f4", "f5"}
	fileOf := map[string]string{}
	for _, f := range files {
		fileOf["c-"+f] = f
	}
	var chunkEdges []ChunkEdge
	w := 0.5
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			w += 0.01
			chunkEdges = append(chunkEdges, ChunkEdge{A: "c-" + files[i], B: "c-" + files[j], Weight: w})
		}
	}

	edges := BuildTranscriptEdges(chunkEdges, fileOf, TranscriptEdgeParams{K: 2})
	if len(edges) == 0 || len(edges) >= 15 {
		t.Fatalf("top-K sparsification should avoid a clique, got %d edges: %+v", len(edges), edges)
	}
	// The single weakest pair (f0-f1) wants nothing from either side once
	// each file has 4 other, stronger candidates to pick from with K=2.
	for _, e := range edges {
		if (e.A == "f0" && e.B == "f1") || (e.A == "f1" && e.B == "f0") {
			t.Fatalf("weakest edge f0-f1 should have been trimmed: %+v", edges)
		}
	}
}

func TestBuildChunkEdges_Deterministic(t *testing.T) {
	ids := []string{"a", "b", "c", "d"}
	vecs := [][]float32{{1, 0}, {0.9, 0.1}, {0, 1}, {0.1, 0.9}}
	e1 := BuildChunkEdges(ids, vecs, GraphEdgeParams{})
	e2 := BuildChunkEdges(ids, vecs, GraphEdgeParams{})
	if len(e1) != len(e2) {
		t.Fatalf("non-deterministic edge count: %d vs %d", len(e1), len(e2))
	}
	for i := range e1 {
		if e1[i] != e2[i] {
			t.Fatalf("non-deterministic edge order at %d: %+v vs %+v", i, e1[i], e2[i])
		}
	}
}
