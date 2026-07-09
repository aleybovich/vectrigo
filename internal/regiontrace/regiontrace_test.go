package regiontrace

import (
	"math"
	"reflect"
	"testing"
)

// diagLabels builds a w×h label map split by a staircase diagonal: label 0 where
// x >= y, else label 1. The shared boundary is a pixel staircase.
func diagLabels(w, h int) []int {
	labels := make([]int, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x >= y {
				labels[y*w+x] = 0
			} else {
				labels[y*w+x] = 1
			}
		}
	}
	return labels
}

// dedge is a directed unit edge used only by the sharing test.
type dedge struct{ sx, sy, ex, ey int }

func rev(e dedge) dedge { return dedge{e.ex, e.ey, e.sx, e.sy} }

// TestSharedEdgesGapless is the algebraic proof of gapless tiling: every
// interior boundary unit-edge (whose two adjacent cells are both opaque regions
// with different labels) must be emitted by BOTH those regions as the same
// undirected segment in opposite directions; every border/transparent-facing
// boundary edge is emitted by exactly one region.
func TestSharedEdgesGapless(t *testing.T) {
	cases := []struct {
		name    string
		w, h, n int
		labels  []int
	}{
		{"diagonal-staircase", 6, 6, 2, diagLabels(6, 6)},
		{"three-region-junction", 2, 2, 3, []int{0, 1, 2, 0}},
		{"with-transparent", 3, 3, 1, []int{0, 0, 0, 0, -1, 0, 0, 0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			re := boundaryEdges(tc.labels, tc.w, tc.h, tc.n)
			// Per-region set of directed edges.
			owner := make([]map[dedge]bool, tc.n)
			for r := 0; r < tc.n; r++ {
				owner[r] = make(map[dedge]bool)
				for _, e := range re[r] {
					de := dedge{e.sx, e.sy, e.ex, e.ey}
					if owner[r][de] {
						t.Fatalf("region %d emitted duplicate edge %+v", r, de)
					}
					owner[r][de] = true
				}
			}
			label := func(x, y int) int {
				if x < 0 || y < 0 || x >= tc.w || y >= tc.h {
					return -1
				}
				return tc.labels[y*tc.w+x]
			}
			// Recompute every boundary edge and verify the sharing invariant.
			for y := 0; y < tc.h; y++ {
				for x := 0; x < tc.w; x++ {
					r := tc.labels[y*tc.w+x]
					if r < 0 {
						continue
					}
					sides := []struct {
						nx, ny int
						e      dedge
					}{
						{x, y - 1, dedge{x, y, x + 1, y}},
						{x + 1, y, dedge{x + 1, y, x + 1, y + 1}},
						{x, y + 1, dedge{x + 1, y + 1, x, y + 1}},
						{x - 1, y, dedge{x, y + 1, x, y}},
					}
					for _, s := range sides {
						nr := label(s.nx, s.ny)
						if nr == r {
							continue // not a boundary side
						}
						if !owner[r][s.e] {
							t.Fatalf("region %d missing its boundary edge %+v", r, s.e)
						}
						if nr >= 0 {
							// Shared interior edge: neighbour must emit the reverse.
							if !owner[nr][rev(s.e)] {
								t.Fatalf("shared edge %+v owned by region %d but region %d does not emit its reverse", s.e, r, nr)
							}
						} else {
							// Border/transparent: no other region emits the reverse.
							for o := 0; o < tc.n; o++ {
								if owner[o][rev(s.e)] {
									t.Fatalf("border edge %+v (region %d) also reverse-emitted by region %d", s.e, r, o)
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestLoopValidityAndArea(t *testing.T) {
	// Region 0 is a 5×5 block with a one-pixel hole at (2,2); region 1 is that
	// single pixel. Net signed area must equal each region's pixel count exactly
	// at Smooth=0.
	w, h := 5, 5
	labels := make([]int, w*h)
	labels[2*w+2] = 1
	n := 2
	regions := Trace(labels, w, h, n, Options{Smooth: 0})

	byID := map[int]Region{}
	for _, rg := range regions {
		byID[rg.ID] = rg
	}
	if len(regions) != 2 {
		t.Fatalf("want 2 regions, got %d", len(regions))
	}

	areaOf := func(rg Region) float64 {
		var a float64
		for _, l := range rg.Loops {
			if len(l) < 3 {
				t.Fatalf("region %d loop has %d points (<3)", rg.ID, len(l))
			}
			if math.Abs(signedArea(l)) < 1e-9 {
				t.Fatalf("region %d has a degenerate loop", rg.ID)
			}
			a += signedArea(l)
		}
		return a
	}
	if got := areaOf(byID[0]); math.Abs(got-24) > 1e-9 {
		t.Fatalf("region 0 net area = %v, want 24", got)
	}
	if got := areaOf(byID[1]); math.Abs(got-1) > 1e-9 {
		t.Fatalf("region 1 net area = %v, want 1", got)
	}
	// Region 0 must have two loops (outer + hole); the hole winds negative.
	r0 := byID[0]
	if len(r0.Loops) != 2 {
		t.Fatalf("region 0 want 2 loops (outer+hole), got %d", len(r0.Loops))
	}
	var pos, neg int
	for _, l := range r0.Loops {
		if signedArea(l) > 0 {
			pos++
		} else {
			neg++
		}
	}
	if pos != 1 || neg != 1 {
		t.Fatalf("region 0 want 1 positive (outer) + 1 negative (hole) loop, got pos=%d neg=%d", pos, neg)
	}
}

func TestDeterminism(t *testing.T) {
	w, h, n := 12, 9, 3
	labels := make([]int, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// A deterministic, mildly interleaved pattern producing shared,
			// jagged boundaries and a few pinches.
			labels[y*w+x] = ((x*3 + y*2) / 4) % n
		}
	}
	for _, sm := range []int{0, 3} {
		a := Trace(labels, w, h, n, Options{Smooth: sm})
		b := Trace(labels, w, h, n, Options{Smooth: sm})
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("Trace not deterministic at Smooth=%d", sm)
		}
	}
}

func TestSmoothingKeepsSharing(t *testing.T) {
	w, h := 8, 8
	labels := diagLabels(w, h)
	regions := Trace(labels, w, h, 2, Options{Smooth: 4})
	if len(regions) != 2 {
		t.Fatalf("want 2 regions, got %d", len(regions))
	}

	set0 := map[Point]bool{}
	for _, l := range regions[0].Loops {
		for _, p := range l {
			set0[p] = true
		}
	}
	shared := 0
	movedInterior := false
	for _, l := range regions[1].Loops {
		for _, p := range l {
			if set0[p] {
				shared++
				// A shared point that moved off the integer grid proves the
				// smoothing is (a) active and (b) identical for both regions.
				if p.X != math.Trunc(p.X) || p.Y != math.Trunc(p.Y) {
					movedInterior = true
				}
			}
		}
	}
	if shared == 0 {
		t.Fatal("regions share no boundary vertices after smoothing")
	}
	if !movedInterior {
		t.Fatal("no shared interior vertex moved; smoothing not applied or not shared")
	}

	// Image-border corners must be pinned (exact integer coordinates).
	for _, rg := range regions {
		for _, l := range rg.Loops {
			for _, p := range l {
				onBorder := p.X == 0 || p.X == float64(w) || p.Y == 0 || p.Y == float64(h)
				if onBorder {
					if p.X != math.Trunc(p.X) || p.Y != math.Trunc(p.Y) {
						t.Fatalf("border point %+v is not pinned to integer", p)
					}
				}
			}
		}
	}
}

func TestJunctionPinned(t *testing.T) {
	// 2×2 with labels {0,1,2,0}: interior corner (1,1) is where 3 regions meet,
	// crack-degree 4, so it must stay exactly (1,1) after smoothing.
	labels := []int{0, 1, 2, 0}
	regions := Trace(labels, 2, 2, 3, Options{Smooth: 10})
	found := false
	for _, rg := range regions {
		for _, l := range rg.Loops {
			for _, p := range l {
				if p.X == 1 && p.Y == 1 {
					found = true
				}
				// The pinch corner is the only interior corner; every other
				// corner is on the image border. Nothing may drift.
				if p.X != math.Trunc(p.X) || p.Y != math.Trunc(p.Y) {
					t.Fatalf("point %+v drifted; junction/border should be pinned", p)
				}
			}
		}
	}
	if !found {
		t.Fatal("expected junction corner (1,1) to appear in some region loop")
	}
}

func TestDegenerate(t *testing.T) {
	// 1×1 single region.
	r := Trace([]int{0}, 1, 1, 1, Options{Smooth: 3})
	if len(r) != 1 || len(r[0].Loops) != 1 {
		t.Fatalf("1x1: want 1 region 1 loop, got %v", r)
	}
	if math.Abs(signedArea(r[0].Loops[0])-1) > 1e-9 {
		t.Fatalf("1x1 area = %v, want 1", signedArea(r[0].Loops[0]))
	}

	// All transparent: no regions.
	allT := []int{-1, -1, -1, -1}
	if got := Trace(allT, 2, 2, 0, Options{Smooth: 2}); got != nil {
		t.Fatalf("all-transparent numRegions=0: want nil, got %v", got)
	}
	// numRegions>0 but no pixels labelled: no regions emitted, no panic.
	if got := Trace(allT, 2, 2, 1, Options{Smooth: 2}); len(got) != 0 {
		t.Fatalf("all-transparent: want 0 regions, got %v", got)
	}

	// Single region filling the whole grid: one outer loop = canvas rectangle.
	full := make([]int, 9)
	fr := Trace(full, 3, 3, 1, Options{Smooth: 5})
	if len(fr) != 1 || len(fr[0].Loops) != 1 || len(fr[0].Loops[0]) != 4 {
		t.Fatalf("full grid: want 1 region, 1 rectangular loop, got %v", fr)
	}
	if math.Abs(signedArea(fr[0].Loops[0])-9) > 1e-9 {
		t.Fatalf("full grid area = %v, want 9", signedArea(fr[0].Loops[0]))
	}
}

// totalPoints counts every emitted loop vertex across all regions.
func totalPoints(regions []Region) int {
	n := 0
	for _, rg := range regions {
		for _, l := range rg.Loops {
			n += len(l)
		}
	}
	return n
}

// TestSimplifyZeroIsNoop confirms Simplify == 0 (a bare Options) reproduces the
// historical output exactly, both unsmoothed and smoothed.
func TestSimplifyZeroIsNoop(t *testing.T) {
	w, h := 16, 16
	labels := diagLabels(w, h)
	for _, sm := range []int{0, 3} {
		a := Trace(labels, w, h, 2, Options{Smooth: sm})
		b := Trace(labels, w, h, 2, Options{Smooth: sm, Simplify: 0})
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("Simplify:0 changed output at Smooth=%d", sm)
		}
	}
}

// jaggedLabels builds a w×h label map split by an IRREGULAR jagged vertical
// boundary (step widths vary), like a real image edge. Unlike a perfectly
// regular staircase — whose smoothed corners land exactly collinear by
// symmetry and collapse for free — an irregular edge smooths to corners that
// are only NEARLY collinear, which is precisely the node-explosion case.
func jaggedLabels(w, h int) []int {
	labels := make([]int, w*h)
	for y := 0; y < h; y++ {
		// 1px-amplitude staircase with irregular step lengths: the digitization
		// of a straight but non-45-degree edge (e.g. a ruled sign or hat brim).
		split := w/2 + (y*13%3)%2
		for x := 0; x < w; x++ {
			if x >= split {
				labels[y*w+x] = 1
			}
		}
	}
	return labels
}

// TestSimplifyCollapsesSmoothedStaircase is the node-explosion regression test:
// a smoothed irregular edge is NEARLY collinear, so without simplification
// nearly every boundary pixel survives as a vertex; with a sub-pixel tolerance
// the run must collapse to a fraction of that, and each region's area must be
// preserved to within tolerance x boundary-length.
func TestSimplifyCollapsesSmoothedStaircase(t *testing.T) {
	w, h := 64, 64
	labels := jaggedLabels(w, h)
	raw := Trace(labels, w, h, 2, Options{Smooth: 3})
	simp := Trace(labels, w, h, 2, Options{Smooth: 3, Simplify: 0.35})

	rawN, simpN := totalPoints(raw), totalPoints(simp)
	if simpN >= rawN/4 {
		t.Fatalf("simplification too weak: %d points -> %d, want < 1/4", rawN, simpN)
	}
	areaOf := func(regions []Region, id int) float64 {
		var a float64
		for _, rg := range regions {
			if rg.ID != id {
				continue
			}
			for _, l := range rg.Loops {
				a += signedArea(l)
			}
		}
		return a
	}
	for id := 0; id < 2; id++ {
		got, want := areaOf(simp, id), areaOf(raw, id)
		if math.Abs(got-want) > 0.35*float64(h)+1 {
			t.Fatalf("region %d area drifted: %v -> %v", id, want, got)
		}
	}
}

// TestSimplifyKeepsSharing is the seam-safety guarantee: after simplification
// the two regions flanking the staircase must still reference IDENTICAL vertex
// geometry along the shared boundary — same points, so no gaps and no overlap.
func TestSimplifyKeepsSharing(t *testing.T) {
	w, h := 32, 32
	labels := diagLabels(w, h)
	regions := Trace(labels, w, h, 2, Options{Smooth: 3, Simplify: 0.35})
	if len(regions) != 2 {
		t.Fatalf("want 2 regions, got %d", len(regions))
	}

	// Collect each region's vertices that are NOT on the image border (i.e. on
	// the shared diagonal). Every interior vertex of one region must appear in
	// the other's vertex set: the shared chain is one global point sequence.
	interior := func(rg Region) map[Point]bool {
		s := map[Point]bool{}
		for _, l := range rg.Loops {
			for _, p := range l {
				if p.X != 0 && p.X != float64(w) && p.Y != 0 && p.Y != float64(h) {
					s[p] = true
				}
			}
		}
		return s
	}
	i0, i1 := interior(regions[0]), interior(regions[1])
	if len(i0) == 0 || len(i1) == 0 {
		t.Fatal("expected interior (shared-boundary) vertices in both regions")
	}
	for p := range i0 {
		if !i1[p] {
			t.Fatalf("vertex %+v on region 0's shared boundary missing from region 1: seam would open", p)
		}
	}
	for p := range i1 {
		if !i0[p] {
			t.Fatalf("vertex %+v on region 1's shared boundary missing from region 0: seam would open", p)
		}
	}
}

// TestSimplifyDeterministic verifies identical inputs yield identical output
// with simplification enabled, including on label maps with junctions and
// pure-cycle (island) boundaries.
func TestSimplifyDeterministic(t *testing.T) {
	w, h, n := 24, 18, 3
	labels := make([]int, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			labels[y*w+x] = ((x*3 + y*2) / 5) % n
		}
	}
	// Carve an island (pure cycle boundary) into region 2's interior.
	for y := 6; y < 12; y++ {
		for x := 10; x < 16; x++ {
			labels[y*w+x] = 2
		}
	}
	for y := 8; y < 10; y++ {
		for x := 12; x < 14; x++ {
			labels[y*w+x] = 0
		}
	}
	a := Trace(labels, w, h, n, Options{Smooth: 3, Simplify: 0.35})
	b := Trace(labels, w, h, n, Options{Smooth: 3, Simplify: 0.35})
	if !reflect.DeepEqual(a, b) {
		t.Fatal("Trace not deterministic with Simplify enabled")
	}
}

// TestSimplifyIslandCycle checks a region strictly inside another (whose
// boundary is a pure degree-2 cycle with no junction anchor) survives
// simplification with its area intact, and its host's hole stays identical.
func TestSimplifyIslandCycle(t *testing.T) {
	w, h := 20, 20
	labels := make([]int, w*h)
	for y := 5; y < 15; y++ {
		for x := 5; x < 15; x++ {
			labels[y*w+x] = 1
		}
	}
	regions := Trace(labels, w, h, 2, Options{Smooth: 3, Simplify: 0.35})
	if len(regions) != 2 {
		t.Fatalf("want 2 regions, got %d", len(regions))
	}
	byID := map[int]Region{}
	for _, rg := range regions {
		byID[rg.ID] = rg
	}
	var island float64
	for _, l := range byID[1].Loops {
		island += signedArea(l)
	}
	if math.Abs(island-100) > 20 { // 10x10 block, smoothing rounds corners a bit
		t.Fatalf("island area = %v, want ~100", island)
	}
	// The host's hole must use exactly the island's outer geometry (negated
	// winding): same vertex set.
	islandPts := map[Point]bool{}
	for _, l := range byID[1].Loops {
		for _, p := range l {
			islandPts[p] = true
		}
	}
	holeFound := false
	for _, l := range byID[0].Loops {
		if signedArea(l) < 0 {
			holeFound = true
			for _, p := range l {
				if !islandPts[p] {
					t.Fatalf("host hole vertex %+v not shared with island: seam would open", p)
				}
			}
		}
	}
	if !holeFound {
		t.Fatal("host region has no hole loop for the island")
	}
}
