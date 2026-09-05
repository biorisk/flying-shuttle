package atlas

import (
	"fmt"
	"math"
	"math/rand"
	"slices"
	"testing"
)

// makeBlobs builds `groups` tight gaussian blobs of `per` unit vectors each in
// `dim` dimensions, deterministically.
func makeBlobs(groups, per, dim int, seed int64) ([]string, [][]float32, []int) {
	rng := rand.New(rand.NewSource(seed))
	var ids []string
	var vecs [][]float32
	var truth []int
	for g := 0; g < groups; g++ {
		center := make([]float32, dim)
		center[g%dim] = 1
		for i := 0; i < per; i++ {
			v := make([]float32, dim)
			var norm float64
			for k := range v {
				v[k] = center[k] + float32(rng.NormFloat64())*0.05
				norm += float64(v[k] * v[k])
			}
			inv := float32(1 / math.Sqrt(norm))
			for k := range v {
				v[k] *= inv
			}
			ids = append(ids, fmt.Sprintf("g%d-c%02d", g, i))
			vecs = append(vecs, v)
			truth = append(truth, g)
		}
	}
	return ids, vecs, truth
}

func TestClusterChunks_SeparatesBlobs(t *testing.T) {
	ids, vecs, truth := makeBlobs(4, 12, 8, 42)

	regions := ClusterChunks(ids, vecs, ClusterParams{MaxRegionSize: 15, MinRegionSize: 3})
	if len(regions) < 4 {
		t.Fatalf("expected >=4 regions for 4 blobs, got %d", len(regions))
	}

	idxOf := map[string]int{}
	for i, id := range ids {
		idxOf[id] = i
	}
	// Every region should be dominated by a single ground-truth blob.
	for _, r := range regions {
		counts := map[int]int{}
		for _, m := range r.Members {
			counts[truth[idxOf[m.ChunkID]]]++
		}
		best := 0
		for _, c := range counts {
			best = max(best, c)
		}
		if float64(best)/float64(len(r.Members)) < 0.8 {
			t.Fatalf("region mixes blobs: %v", counts)
		}
	}

	// Every chunk assigned exactly once.
	seen := map[string]bool{}
	total := 0
	for _, r := range regions {
		total += len(r.Members)
		for _, m := range r.Members {
			if seen[m.ChunkID] {
				t.Fatalf("chunk %s in two regions", m.ChunkID)
			}
			seen[m.ChunkID] = true
		}
	}
	if total != len(ids) {
		t.Fatalf("partition lost chunks: %d/%d", total, len(ids))
	}
}

func TestClusterChunks_RespectsMaxSize(t *testing.T) {
	ids, vecs, _ := makeBlobs(2, 40, 6, 7)
	regions := ClusterChunks(ids, vecs, ClusterParams{MaxRegionSize: 12, MinRegionSize: 3})
	for _, r := range regions {
		if r.ChunkCount > 12 {
			t.Fatalf("region of %d exceeds MaxRegionSize", r.ChunkCount)
		}
	}
}

func TestClusterChunks_Deterministic(t *testing.T) {
	ids, vecs, _ := makeBlobs(3, 20, 8, 99)
	a := ClusterChunks(ids, vecs, ClusterParams{})
	b := ClusterChunks(ids, vecs, ClusterParams{})
	if len(a) != len(b) {
		t.Fatalf("region count differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		am := memberIDs(a[i])
		bm := memberIDs(b[i])
		if !slices.Equal(am, bm) {
			t.Fatalf("region %d membership differs:\n %v\n %v", i, am, bm)
		}
	}
}

func TestClusterChunks_MembersSortedByDistance(t *testing.T) {
	ids, vecs, _ := makeBlobs(3, 10, 8, 3)
	for _, r := range ClusterChunks(ids, vecs, ClusterParams{}) {
		for i := 1; i < len(r.Members); i++ {
			if r.Members[i-1].Distance > r.Members[i].Distance+1e-9 {
				t.Fatalf("members not nearest-first: %+v", r.Members)
			}
		}
		// centroid is unit length
		var norm float64
		for _, v := range r.Centroid {
			norm += float64(v * v)
		}
		if math.Abs(math.Sqrt(norm)-1) > 1e-3 {
			t.Fatalf("centroid not normalised: |c|=%v", math.Sqrt(norm))
		}
	}
}

func TestClusterChunks_Small(t *testing.T) {
	if ClusterChunks(nil, nil, ClusterParams{}) != nil {
		t.Fatal("nil input")
	}
	ids := []string{"a", "b", "c"}
	vecs := [][]float32{{1, 0}, {0, 1}, {1, 1}}
	got := ClusterChunks(ids, vecs, ClusterParams{MaxRegionSize: 15})
	if len(got) != 1 || got[0].ChunkCount != 3 {
		t.Fatalf("tiny corpus should be one region: %+v", got)
	}
}

func memberIDs(r Region) []string {
	out := make([]string, len(r.Members))
	for i, m := range r.Members {
		out[i] = m.ChunkID
	}
	return out
}
