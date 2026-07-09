package regionize

import (
	"reflect"
	"testing"
)

// grid is a test helper turning a compact rune matrix into a cluster label
// slice: digits are cluster ids, '.' is transparent (-1).
func grid(rows ...string) (clusters []int, w, h int) {
	h = len(rows)
	w = len(rows[0])
	clusters = make([]int, 0, w*h)
	for _, r := range rows {
		for _, c := range r {
			if c == '.' {
				clusters = append(clusters, -1)
			} else {
				clusters = append(clusters, int(c-'0'))
			}
		}
	}
	return clusters, w, h
}

// TestComponentsSplitSameCluster is the shape-contiguity property: two areas
// of the SAME cluster separated in space must become two distinct regions,
// each keeping the cluster's id for colouring.
func TestComponentsSplitSameCluster(t *testing.T) {
	clusters, w, h := grid(
		"00100",
		"00100",
		"00100",
	)
	res := Regionize(clusters, w, h, 0)

	if res.NumRegions != 3 {
		t.Fatalf("NumRegions = %d, want 3 (left 0s, middle 1s, right 0s)", res.NumRegions)
	}
	// Row-major first-appearance numbering: left block 0, stripe 1, right 2.
	if res.Cluster[0] != 0 || res.Cluster[1] != 1 || res.Cluster[2] != 0 {
		t.Errorf("Cluster = %v, want [0 1 0]", res.Cluster)
	}
	if res.Areas[0] != 6 || res.Areas[1] != 3 || res.Areas[2] != 6 {
		t.Errorf("Areas = %v, want [6 3 6]", res.Areas)
	}
	// Left and right 0-areas must be DIFFERENT regions.
	if res.Labels[0] == res.Labels[4] {
		t.Error("disconnected same-cluster areas got the same region id")
	}
}

// TestDiagonalIsNotConnected pins 4-connectivity: two same-cluster pixels
// touching only at a corner are separate regions.
func TestDiagonalIsNotConnected(t *testing.T) {
	clusters, w, h := grid(
		"10",
		"01",
	)
	res := Regionize(clusters, w, h, 0)
	if res.NumRegions != 4 {
		t.Fatalf("NumRegions = %d, want 4 (diagonals are not connected)", res.NumRegions)
	}
}

// TestTransparentPixels checks that negative input labels stay -1, join no
// region, and are not counted in any area.
func TestTransparentPixels(t *testing.T) {
	clusters, w, h := grid(
		"0.0",
		"...",
		"0.0",
	)
	res := Regionize(clusters, w, h, 0)
	if res.NumRegions != 4 {
		t.Fatalf("NumRegions = %d, want 4", res.NumRegions)
	}
	for i, c := range clusters {
		if (c < 0) != (res.Labels[i] < 0) {
			t.Fatalf("pixel %d: transparency not preserved (cluster %d -> label %d)", i, c, res.Labels[i])
		}
	}
	total := 0
	for _, a := range res.Areas {
		total += a
	}
	if total != 4 {
		t.Errorf("sum(Areas) = %d, want 4 opaque pixels", total)
	}
}

// TestAbsorbTinyIntoLongestBorderNeighbour checks the speckle rule: a
// component below minSize is merged into the adjacent component sharing the
// longest border, adopting its cluster (recoloured, not deleted).
func TestAbsorbTinyIntoLongestBorderNeighbour(t *testing.T) {
	// The single 2 touches cluster 1 on two sides (left+top... construct so
	// border with 1 is 3 edges, with 0 is 1 edge).
	clusters, w, h := grid(
		"111",
		"121",
		"101",
	)
	res := Regionize(clusters, w, h, 2)

	// The 2 must be absorbed into the 1-region (3 shared edges) not the 0
	// (1 shared edge)... the 0 is itself tiny and is absorbed too.
	for id, cl := range res.Cluster {
		if cl == 2 {
			t.Errorf("region %d still has cluster 2; speck was not absorbed", id)
		}
	}
	// Pixel (1,1) — the former 2 — must share a region with a 1-pixel.
	if res.Labels[4] != res.Labels[0] {
		t.Errorf("speck joined region %d, want the surrounding 1-region %d", res.Labels[4], res.Labels[0])
	}
	if res.Cluster[res.Labels[4]] != 1 {
		t.Errorf("speck recoloured to cluster %d, want 1", res.Cluster[res.Labels[4]])
	}
}

// TestAbsorbThresholdMatchesBitrace pins the strictly-below convention shared
// with bitrace's TurdSize: area == minSize survives, area < minSize is
// absorbed, and minSize <= 1 disables absorption entirely.
func TestAbsorbThresholdMatchesBitrace(t *testing.T) {
	clusters, w, h := grid(
		"000",
		"011",
		"000",
	)

	// The 2-pixel 1-component survives minSize 2 exactly.
	res := Regionize(clusters, w, h, 2)
	if res.NumRegions != 2 {
		t.Fatalf("minSize=2: NumRegions = %d, want 2 (area 2 >= 2 survives)", res.NumRegions)
	}

	// ...and is absorbed at minSize 3.
	res = Regionize(clusters, w, h, 3)
	if res.NumRegions != 1 {
		t.Fatalf("minSize=3: NumRegions = %d, want 1 (area 2 < 3 absorbed)", res.NumRegions)
	}
	if res.Cluster[0] != 0 || res.Areas[0] != 9 {
		t.Errorf("absorbed result Cluster/Areas = %v/%v, want [0]/[9]", res.Cluster, res.Areas)
	}

	// minSize 0 and 1 keep everything.
	for _, ms := range []int{0, 1} {
		if got := Regionize(clusters, w, h, ms).NumRegions; got != 2 {
			t.Errorf("minSize=%d: NumRegions = %d, want 2 (absorption disabled)", ms, got)
		}
	}
}

// TestAbsorbChain checks the multi-round case: two adjacent tiny components of
// different clusters merge, and the merged group keeps absorbing (or stops)
// per the threshold against the COMBINED area.
func TestAbsorbChain(t *testing.T) {
	clusters, w, h := grid(
		"00000",
		"01200",
		"00000",
	)
	res := Regionize(clusters, w, h, 3)

	// Both specks (area 1 each) are below 3; whether they first merge with
	// each other or straight into the background, all pixels must end in ONE
	// region of cluster 0.
	if res.NumRegions != 1 {
		t.Fatalf("NumRegions = %d, want 1", res.NumRegions)
	}
	if res.Cluster[0] != 0 {
		t.Errorf("surviving cluster = %d, want 0", res.Cluster[0])
	}
	if res.Areas[0] != 15 {
		t.Errorf("surviving area = %d, want 15", res.Areas[0])
	}
}

// TestIsolatedTinyIslandKept checks that a sub-threshold component with no
// opaque neighbour has nothing to merge into and survives (absorption must
// never delete pixels).
func TestIsolatedTinyIslandKept(t *testing.T) {
	clusters, w, h := grid(
		"0..",
		"...",
		"..1",
	)
	res := Regionize(clusters, w, h, 5)
	if res.NumRegions != 2 {
		t.Fatalf("NumRegions = %d, want 2 (islands kept)", res.NumRegions)
	}
	total := 0
	for _, a := range res.Areas {
		total += a
	}
	if total != 2 {
		t.Errorf("sum(Areas) = %d, want 2", total)
	}
}

// TestTilingInvariant is the gapless property at the label level: every opaque
// pixel belongs to exactly one region in [0,NumRegions), region areas sum to
// the opaque pixel count, and Cluster/Areas are sized by NumRegions.
func TestTilingInvariant(t *testing.T) {
	clusters, w, h := grid(
		"0011223",
		"0.12213",
		"3311220",
		"3.....0",
	)
	for _, minSize := range []int{0, 2, 4, 100} {
		res := Regionize(clusters, w, h, minSize)
		if len(res.Cluster) != res.NumRegions || len(res.Areas) != res.NumRegions {
			t.Fatalf("minSize=%d: Cluster/Areas length %d/%d, want NumRegions %d",
				minSize, len(res.Cluster), len(res.Areas), res.NumRegions)
		}
		counts := make([]int, res.NumRegions)
		for i, c := range clusters {
			lb := res.Labels[i]
			if c < 0 {
				if lb != -1 {
					t.Fatalf("minSize=%d: transparent pixel %d got label %d", minSize, i, lb)
				}
				continue
			}
			if lb < 0 || lb >= res.NumRegions {
				t.Fatalf("minSize=%d: opaque pixel %d got out-of-range label %d", minSize, i, lb)
			}
			counts[lb]++
		}
		if !reflect.DeepEqual(counts, res.Areas) {
			t.Errorf("minSize=%d: Areas = %v, recount = %v", minSize, res.Areas, counts)
		}
	}
}

// TestDeterminism runs the same non-trivial input repeatedly and requires
// deep-equal results (absorption iterates a map internally; the output order
// must not depend on it).
func TestDeterminism(t *testing.T) {
	clusters, w, h := grid(
		"001122334455",
		"0.1.2.3.4.55",
		"554433221100",
		"5.4.3.2.1.00",
	)
	first := Regionize(clusters, w, h, 3)
	for i := 0; i < 20; i++ {
		if got := Regionize(clusters, w, h, 3); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs from first run:\n%+v\nvs\n%+v", i, got, first)
		}
	}
}

// TestDegenerateInputs pins the nil-safety contract for empty and mismatched
// inputs.
func TestDegenerateInputs(t *testing.T) {
	if res := Regionize(nil, 0, 0, 2); res.NumRegions != 0 || res.Labels != nil {
		t.Errorf("empty input: got %+v, want zero Result", res)
	}
	if res := Regionize([]int{0, 0}, 3, 3, 2); res.NumRegions != 0 || res.Labels != nil {
		t.Errorf("mismatched length: got %+v, want zero Result", res)
	}
	// All-transparent input: zero regions, labels all -1.
	clusters, w, h := grid("..", "..")
	res := Regionize(clusters, w, h, 2)
	if res.NumRegions != 0 {
		t.Errorf("all-transparent: NumRegions = %d, want 0", res.NumRegions)
	}
	for i, lb := range res.Labels {
		if lb != -1 {
			t.Errorf("all-transparent: label[%d] = %d, want -1", i, lb)
		}
	}
}
