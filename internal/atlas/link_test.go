package atlas

import "testing"

func TestBuildLinks(t *testing.T) {
	// r1 and r2 point the same way (sim 1); r3 is orthogonal to both.
	regions := []Region{
		{ID: "r1", Centroid: []float32{1, 0, 0}},
		{ID: "r2", Centroid: []float32{0.99, 0.01, 0}},
		{ID: "r3", Centroid: []float32{0, 0, 1}},
	}

	links := BuildLinks(regions, LinkParams{K: 6, MinWeight: 0.15})
	if len(links) != 1 {
		t.Fatalf("want exactly the r1-r2 link, got %+v", links)
	}
	l := links[0]
	if l.RegionA != "r1" || l.RegionB != "r2" {
		t.Fatalf("link endpoints / ordering: %+v", l)
	}
	if l.Weight < 0.98 {
		t.Fatalf("weight too low: %v", l.Weight)
	}
}

func TestBuildLinks_KLimitAndDedup(t *testing.T) {
	// Five nearly-identical regions; with K=2 each keeps its 2 nearest, but
	// links are undirected and de-duplicated.
	regions := []Region{
		{ID: "a", Centroid: []float32{1, 0}},
		{ID: "b", Centroid: []float32{0.99, 0.1}},
		{ID: "c", Centroid: []float32{0.98, 0.2}},
		{ID: "d", Centroid: []float32{0.97, 0.3}},
		{ID: "e", Centroid: []float32{0.96, 0.4}},
	}
	links := BuildLinks(regions, LinkParams{K: 2, MinWeight: 0.1})

	seen := map[[2]string]bool{}
	for _, l := range links {
		if l.RegionA >= l.RegionB {
			t.Fatalf("non-canonical link: %+v", l)
		}
		key := [2]string{l.RegionA, l.RegionB}
		if seen[key] {
			t.Fatalf("duplicate link: %+v", l)
		}
		seen[key] = true
	}
	// Weights are sorted descending.
	for i := 1; i < len(links); i++ {
		if links[i-1].Weight < links[i].Weight {
			t.Fatalf("links not sorted by weight desc")
		}
	}
	if len(links) < 4 {
		t.Fatalf("expected the chain to be connected, got %d links", len(links))
	}
}

func TestBuildLinks_Trivial(t *testing.T) {
	if BuildLinks(nil, LinkParams{}) != nil {
		t.Fatal("nil regions should yield nil links")
	}
	if BuildLinks([]Region{{ID: "solo", Centroid: []float32{1}}}, LinkParams{}) != nil {
		t.Fatal("single region should yield nil links")
	}
}
