package atlas

import (
	"sort"

	"github.com/biorisk/flying-shuttle/internal/ingest"
)

// LinkParams controls how the region similarity graph is built.
type LinkParams struct {
	// K is the max number of links kept per region (its nearest neighbours by
	// centroid cosine). Default 6.
	K int
	// MinWeight drops links below this cosine similarity. Default 0.15.
	MinWeight float64
}

func (p LinkParams) withDefaults() LinkParams {
	if p.K <= 0 {
		p.K = 6
	}
	if p.MinWeight == 0 {
		p.MinWeight = 0.15
	}
	return p
}

// BuildLinks connects regions into an undirected weighted graph: each region
// gets a link to its K nearest other regions (by centroid cosine) above
// MinWeight. A link kept from either endpoint's neighbour list survives, so
// degree can exceed K. Pairs are de-duplicated and returned in canonical
// (RegionA < RegionB) order, strongest weight first.
//
// Cost is O(n^2) in the region count, which is fine — builds have hundreds of
// regions, not millions.
func BuildLinks(regions []Region, params LinkParams) []Link {
	p := params.withDefaults()
	n := len(regions)
	if n < 2 {
		return nil
	}

	// Full pairwise similarity (upper triangle).
	type cand struct {
		j int
		w float64
	}
	neighbours := make([][]cand, n)
	sims := make(map[[2]int]float64)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			w := ingest.CosineSimilarity(regions[i].Centroid, regions[j].Centroid)
			if w < p.MinWeight {
				continue
			}
			sims[[2]int{i, j}] = w
			neighbours[i] = append(neighbours[i], cand{j, w})
			neighbours[j] = append(neighbours[j], cand{i, w})
		}
	}

	keep := make(map[[2]int]bool)
	for i := range neighbours {
		ns := neighbours[i]
		sort.Slice(ns, func(a, b int) bool {
			if ns[a].w != ns[b].w {
				return ns[a].w > ns[b].w
			}
			return regions[ns[a].j].ID < regions[ns[b].j].ID
		})
		if len(ns) > p.K {
			ns = ns[:p.K]
		}
		for _, c := range ns {
			a, b := i, c.j
			if a > b {
				a, b = b, a
			}
			keep[[2]int{a, b}] = true
		}
	}

	links := make([]Link, 0, len(keep))
	for pair := range keep {
		l, ok := NewLink(regions[pair[0]].ID, regions[pair[1]].ID, sims[[2]int{pair[0], pair[1]}])
		if ok {
			links = append(links, l)
		}
	}
	sort.Slice(links, func(a, b int) bool {
		if links[a].Weight != links[b].Weight {
			return links[a].Weight > links[b].Weight
		}
		if links[a].RegionA != links[b].RegionA {
			return links[a].RegionA < links[b].RegionA
		}
		return links[a].RegionB < links[b].RegionB
	})
	return links
}
