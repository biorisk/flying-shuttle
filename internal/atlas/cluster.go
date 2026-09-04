package atlas

import (
	"math"
	"math/rand"
	"sort"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

// ClusterParams controls Phase A partitioning. See source_atlas_plan.md §2.
type ClusterParams struct {
	MaxRegionSize int   // split any region larger than this; default 15
	MinRegionSize int   // merge any region smaller than this; default 4
	MaxRegions    int   // stop splitting past this many regions; default 400
	MaxIters      int   // k-means iterations per split; default 25
	Seed          int64 // RNG seed for reproducible builds; default 1
}

func (p ClusterParams) withDefaults() ClusterParams {
	if p.MaxRegionSize <= 0 {
		p.MaxRegionSize = 15
	}
	if p.MinRegionSize <= 0 {
		p.MinRegionSize = 4
	}
	if p.MaxRegions <= 0 {
		p.MaxRegions = 400
	}
	if p.MaxIters <= 0 {
		p.MaxIters = 25
	}
	if p.Seed == 0 {
		p.Seed = 1
	}
	return p
}

// ClusterChunks partitions embedded chunks into regions by bisecting spherical
// k-means with a size stop. ids[i] is the chunk id for vectors[i]; the two
// slices must be the same length. Regions come back with Centroid, ChunkCount,
// and Members (chunk id + cosine distance to centroid, nearest first) filled;
// ids and digests are left for later phases. Deterministic for a fixed Seed.
func ClusterChunks(ids []string, vectors [][]float32, params ClusterParams) []Region {
	p := params.withDefaults()
	n := len(ids)
	if n == 0 || len(vectors) != n {
		return nil
	}

	all := make([]int, n)
	for i := range all {
		all[i] = i
	}
	clusters := [][]int{all}
	unsplittable := map[int]bool{}
	rng := rand.New(rand.NewSource(p.Seed))

	for len(clusters) < p.MaxRegions {
		// Pick the largest over-size, still-splittable cluster; tie-break on
		// the smallest chunk id it contains.
		target := -1
		for ci, c := range clusters {
			if len(c) <= p.MaxRegionSize || unsplittable[ci] {
				continue
			}
			if target == -1 || len(c) > len(clusters[target]) ||
				(len(c) == len(clusters[target]) && minID(ids, c) < minID(ids, clusters[target])) {
				target = ci
			}
		}
		if target == -1 {
			break
		}

		left, right := bisect(clusters[target], vectors, ids, p.MaxIters, rng)
		if len(left) == 0 || len(right) == 0 {
			unsplittable[target] = true
			continue
		}
		clusters[target] = left
		clusters = append(clusters, right)
		// Indices shifted only by appends, so unsplittable keys stay valid.
	}

	clusters = mergeUndersized(clusters, vectors, ids, p.MinRegionSize)

	regions := make([]Region, 0, len(clusters))
	for _, c := range clusters {
		if len(c) == 0 {
			continue
		}
		cent := centroid(vectors, c)
		members := make([]Member, len(c))
		for i, idx := range c {
			members[i] = Member{
				ChunkID:  ids[idx],
				Distance: 1 - ingest.CosineSimilarity(vectors[idx], cent),
			}
		}
		sort.Slice(members, func(a, b int) bool {
			if members[a].Distance != members[b].Distance {
				return members[a].Distance < members[b].Distance
			}
			return members[a].ChunkID < members[b].ChunkID
		})
		regions = append(regions, Region{
			Centroid:   cent,
			ChunkCount: len(c),
			Members:    members,
		})
	}
	sort.Slice(regions, func(a, b int) bool {
		if regions[a].ChunkCount != regions[b].ChunkCount {
			return regions[a].ChunkCount > regions[b].ChunkCount
		}
		return regions[a].Members[0].ChunkID < regions[b].Members[0].ChunkID
	})
	return regions
}

// bisect splits one cluster into two with k=2 spherical k-means, seeded from
// the farthest-apart pair (deterministic).
func bisect(idxs []int, vectors [][]float32, ids []string, maxIters int, _ *rand.Rand) ([]int, []int) {
	a, b := farthestPair(idxs, vectors, ids)
	if a == b {
		return idxs, nil
	}
	c0 := cloneVec(vectors[a])
	c1 := cloneVec(vectors[b])

	var left, right []int
	for iter := 0; iter < maxIters; iter++ {
		left, right = left[:0], right[:0]
		for _, i := range idxs {
			if ingest.CosineSimilarity(vectors[i], c0) >= ingest.CosineSimilarity(vectors[i], c1) {
				left = append(left, i)
			} else {
				right = append(right, i)
			}
		}
		if len(left) == 0 || len(right) == 0 {
			break
		}
		n0, n1 := centroid(vectors, left), centroid(vectors, right)
		if vecEqual(n0, c0) && vecEqual(n1, c1) {
			break
		}
		c0, c1 = n0, n1
	}
	// Return stable copies.
	return append([]int(nil), left...), append([]int(nil), right...)
}

func farthestPair(idxs []int, vectors [][]float32, ids []string) (int, int) {
	bestA, bestB := idxs[0], idxs[0]
	bestSim := math.Inf(1)
	for i := 0; i < len(idxs); i++ {
		for j := i + 1; j < len(idxs); j++ {
			s := ingest.CosineSimilarity(vectors[idxs[i]], vectors[idxs[j]])
			if s < bestSim || (s == bestSim && idPairLess(ids, idxs[i], idxs[j], bestA, bestB)) {
				bestSim, bestA, bestB = s, idxs[i], idxs[j]
			}
		}
	}
	return bestA, bestB
}

func idPairLess(ids []string, a, b, ca, cb int) bool {
	amin, amax := ids[a], ids[b]
	if amin > amax {
		amin, amax = amax, amin
	}
	cmin, cmax := ids[ca], ids[cb]
	if cmin > cmax {
		cmin, cmax = cmax, cmin
	}
	if amin != cmin {
		return amin < cmin
	}
	return amax < cmax
}

func mergeUndersized(clusters [][]int, vectors [][]float32, ids []string, minSize int) [][]int {
	for {
		if len(clusters) <= 1 {
			return clusters
		}
		// Smallest under-size cluster, tie-break on smallest chunk id.
		small := -1
		for ci, c := range clusters {
			if len(c) >= minSize {
				continue
			}
			if small == -1 || len(c) < len(clusters[small]) ||
				(len(c) == len(clusters[small]) && minID(ids, c) < minID(ids, clusters[small])) {
				small = ci
			}
		}
		if small == -1 {
			return clusters
		}
		sc := centroid(vectors, clusters[small])
		best, bestSim := -1, math.Inf(-1)
		for ci, c := range clusters {
			if ci == small {
				continue
			}
			s := ingest.CosineSimilarity(sc, centroid(vectors, c))
			if best == -1 || s > bestSim ||
				(s == bestSim && minID(ids, c) < minID(ids, clusters[best])) {
				best, bestSim = ci, s
			}
		}
		clusters[best] = append(clusters[best], clusters[small]...)
		clusters = append(clusters[:small], clusters[small+1:]...)
	}
}

func centroid(vectors [][]float32, idxs []int) []float32 {
	if len(idxs) == 0 {
		return nil
	}
	d := len(vectors[idxs[0]])
	sum := make([]float64, d)
	for _, i := range idxs {
		for k, v := range vectors[i] {
			sum[k] += float64(v)
		}
	}
	out := make([]float32, d)
	var norm float64
	for k := range sum {
		out[k] = float32(sum[k])
		norm += sum[k] * sum[k]
	}
	if norm > 0 {
		inv := float32(1 / math.Sqrt(norm))
		for k := range out {
			out[k] *= inv
		}
	}
	return out
}

func minID(ids []string, idxs []int) string {
	m := ids[idxs[0]]
	for _, i := range idxs[1:] {
		if ids[i] < m {
			m = ids[i]
		}
	}
	return m
}

func cloneVec(v []float32) []float32 { return append([]float32(nil), v...) }

func vecEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 1e-6 {
			return false
		}
	}
	return true
}
