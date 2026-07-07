package segment

import (
	"image"
	"image/color"
	"reflect"
	"testing"
)

// solidImage builds a w×h NRGBA image whose pixel colours are supplied by fn.
func solidImage(w, h int, fn func(x, y int) color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := fn(x, y)
			o := img.PixOffset(x, y)
			img.Pix[o] = c.R
			img.Pix[o+1] = c.G
			img.Pix[o+2] = c.B
			img.Pix[o+3] = c.A
		}
	}
	return img
}

// assertDense verifies Labels use exactly {0..NumRegions-1} (each id present)
// and that every non-transparent label is in range.
func assertDense(t *testing.T, r Result) {
	t.Helper()
	seen := make([]bool, r.NumRegions)
	for i, lb := range r.Labels {
		if lb == TransparentLabel {
			continue
		}
		if lb < 0 || lb >= r.NumRegions {
			t.Fatalf("pixel %d has out-of-range label %d (NumRegions=%d)", i, lb, r.NumRegions)
		}
		seen[lb] = true
	}
	for id, ok := range seen {
		if !ok {
			t.Fatalf("label %d unused: labelling is not dense", id)
		}
	}
}

// assertContiguous verifies each region is a single 8-connected component.
func assertContiguous(t *testing.T, r Result) {
	t.Helper()
	w, h := r.W, r.H
	visited := make([]bool, len(r.Labels))
	// firstSeen[label] records whether we've already flood-filled a component of
	// that label; a second, disconnected occurrence means the region is split.
	firstSeen := make([]bool, r.NumRegions)
	stack := make([]int, 0, len(r.Labels))
	for start := 0; start < len(r.Labels); start++ {
		lb := r.Labels[start]
		if lb == TransparentLabel || visited[start] {
			continue
		}
		if firstSeen[lb] {
			t.Fatalf("region %d is not spatially contiguous (found a second component)", lb)
		}
		firstSeen[lb] = true
		// Flood fill this component with 8-connectivity.
		stack = stack[:0]
		stack = append(stack, start)
		visited[start] = true
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			px, py := p%w, p/w
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					nx, ny := px+dx, py+dy
					if nx < 0 || ny < 0 || nx >= w || ny >= h {
						continue
					}
					q := ny*w + nx
					if !visited[q] && r.Labels[q] == lb {
						visited[q] = true
						stack = append(stack, q)
					}
				}
			}
		}
	}
}

func TestFourQuadrantsExactRegions(t *testing.T) {
	const w, h = 40, 40
	cols := [4]color.NRGBA{
		{255, 0, 0, 255},   // top-left
		{0, 255, 0, 255},   // top-right
		{0, 0, 255, 255},   // bottom-left
		{255, 255, 0, 255}, // bottom-right
	}
	img := solidImage(w, h, func(x, y int) color.NRGBA {
		q := 0
		if x >= w/2 {
			q |= 1
		}
		if y >= h/2 {
			q |= 2
		}
		return cols[q]
	})

	r := Segment(img, Options{K: 300, MinSize: 0})
	if r.NumRegions != 4 {
		t.Fatalf("NumRegions = %d, want 4", r.NumRegions)
	}
	assertDense(t, r)
	assertContiguous(t, r)

	// Each region's mean colour must equal its solid block colour.
	means := MeanColors(img, r)
	if len(means) != 4 {
		t.Fatalf("MeanColors len = %d, want 4", len(means))
	}
	want := map[[4]uint8]bool{}
	for _, c := range cols {
		want[[4]uint8{c.R, c.G, c.B, c.A}] = true
	}
	for id, m := range means {
		key := [4]uint8{m.R, m.G, m.B, m.A}
		if !want[key] {
			t.Fatalf("region %d mean colour %v is not one of the block colours", id, m)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("not every block colour appeared as a region mean; missing %d", len(want))
	}
}

func TestGradientMonotonicInK(t *testing.T) {
	const w, h = 96, 24
	grad := solidImage(w, h, func(x, y int) color.NRGBA {
		v := uint8(x * 255 / (w - 1))
		return color.NRGBA{v, v, v, 255}
	})

	nSmall := Segment(grad, Options{K: 5}).NumRegions
	nMid := Segment(grad, Options{K: 50}).NumRegions
	nLarge := Segment(grad, Options{K: 5000}).NumRegions

	if nSmall <= 1 {
		t.Fatalf("gradient at small K gave %d regions, want > 1", nSmall)
	}
	if !(nSmall >= nMid && nMid >= nLarge) {
		t.Fatalf("region count not monotonically non-increasing in K: K=5→%d, K=50→%d, K=5000→%d",
			nSmall, nMid, nLarge)
	}
}

func TestDeterministic(t *testing.T) {
	const w, h = 50, 50
	img := solidImage(w, h, func(x, y int) color.NRGBA {
		// A textured pattern with many equal-weight edges to stress tie-breaking.
		v := uint8((x*7 + y*13) % 256)
		return color.NRGBA{v, uint8((x * 3) % 256), uint8((y * 5) % 256), 255}
	})
	opt := Options{K: 200, MinSize: 4, Sigma: 0.8}
	a := Segment(img, opt)
	b := Segment(img, opt)
	if a.NumRegions != b.NumRegions {
		t.Fatalf("NumRegions differ across runs: %d vs %d", a.NumRegions, b.NumRegions)
	}
	if !reflect.DeepEqual(a.Labels, b.Labels) {
		t.Fatal("Labels differ across identical runs: segmentation is not deterministic")
	}
}

func TestMinSizeAbsorbsSpeckle(t *testing.T) {
	const w, h = 40, 40
	bg := color.NRGBA{10, 10, 10, 255}
	fg := color.NRGBA{240, 240, 240, 255}
	img := solidImage(w, h, func(x, y int) color.NRGBA {
		// A 2×2 speckle of a contrasting colour in the middle.
		if x >= 20 && x < 22 && y >= 20 && y < 22 {
			return fg
		}
		return bg
	})

	base := Segment(img, Options{K: 50, MinSize: 0})
	if base.NumRegions != 2 {
		t.Fatalf("without MinSize: NumRegions = %d, want 2 (background + speckle)", base.NumRegions)
	}

	merged := Segment(img, Options{K: 50, MinSize: 16})
	if merged.NumRegions != 1 {
		t.Fatalf("with MinSize=16: NumRegions = %d, want 1 (speckle absorbed)", merged.NumRegions)
	}
	assertDense(t, merged)
	assertContiguous(t, merged)
}

func TestTransparentPixelsExcluded(t *testing.T) {
	const w, h = 20, 20
	img := solidImage(w, h, func(x, y int) color.NRGBA {
		if x < 10 {
			return color.NRGBA{200, 30, 30, 255} // opaque
		}
		return color.NRGBA{0, 0, 0, 0} // fully transparent
	})
	r := Segment(img, Options{K: 100})
	if r.NumRegions != 1 {
		t.Fatalf("NumRegions = %d, want 1 (only the opaque half)", r.NumRegions)
	}
	for i, lb := range r.Labels {
		x := i % w
		if x < 10 {
			if lb != 0 {
				t.Fatalf("opaque pixel %d has label %d, want 0", i, lb)
			}
		} else if lb != TransparentLabel {
			t.Fatalf("transparent pixel %d has label %d, want TransparentLabel", i, lb)
		}
	}
	means := MeanColors(img, r)
	if len(means) != 1 {
		t.Fatalf("MeanColors len = %d, want 1", len(means))
	}
	if means[0].R != 200 || means[0].G != 30 || means[0].B != 30 || means[0].A != 255 {
		t.Fatalf("mean colour = %v, want {200 30 30 255}", means[0])
	}
}

// regionAreas returns the pixel count of each dense region id in r.
func regionAreas(r Result) []int {
	a := make([]int, r.NumRegions)
	for _, lb := range r.Labels {
		if lb >= 0 {
			a[lb]++
		}
	}
	return a
}

// interRegionBoundary counts undirected 4-connected pixel adjacencies whose two
// pixels carry different (opaque) labels. It is a proxy for boundary
// length/roughness: a straighter, less jagged partition has fewer such
// adjacencies.
func interRegionBoundary(r Result) int {
	w, h := r.W, r.H
	count := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			lb := r.Labels[i]
			if lb < 0 {
				continue
			}
			if x+1 < w {
				if o := r.Labels[i+1]; o >= 0 && o != lb {
					count++
				}
			}
			if y+1 < h {
				if o := r.Labels[i+w]; o >= 0 && o != lb {
					count++
				}
			}
		}
	}
	return count
}

// TestBoundarySmoothReducesJaggedness builds two large regions separated by a
// deliberately staircased (square-wave) boundary and checks that smoothing both
// reduces the boundary roughness and keeps a valid two-region partition.
func TestBoundarySmoothReducesJaggedness(t *testing.T) {
	const w, h = 48, 48
	colA := color.NRGBA{200, 40, 40, 255}
	colB := color.NRGBA{40, 40, 200, 255}
	img := solidImage(w, h, func(x, y int) color.NRGBA {
		// Square-wave boundary: the split column alternates by ±3 every 3 rows,
		// producing a jagged staircase between the two solid halves.
		split := 24
		if (y/3)%2 == 0 {
			split += 3
		} else {
			split -= 3
		}
		if x < split {
			return colA
		}
		return colB
	})

	base := Segment(img, Options{K: 500, MinSize: 0})
	if base.NumRegions != 2 {
		t.Fatalf("base NumRegions = %d, want 2", base.NumRegions)
	}
	beforeBoundary := interRegionBoundary(base)

	sm := Segment(img, Options{K: 500, MinSize: 0, BoundarySmooth: 8})
	if sm.NumRegions != 2 {
		t.Fatalf("smoothed NumRegions = %d, want 2 (both regions must survive)", sm.NumRegions)
	}
	assertDense(t, sm)
	assertContiguous(t, sm)

	afterBoundary := interRegionBoundary(sm)
	if afterBoundary >= beforeBoundary {
		t.Fatalf("boundary roughness did not drop: before=%d after=%d", beforeBoundary, afterBoundary)
	}
}

// TestBoundarySmoothPreservesSmallRegion is the critical text-preservation
// guarantee: a tiny 3×3 region surrounded by a contrasting background must still
// exist, with its exact pixel count, after smoothing. Without the small-region
// freeze the mode filter would erode the 3×3 block to nothing within a couple of
// iterations, so this test is non-vacuous.
func TestBoundarySmoothPreservesSmallRegion(t *testing.T) {
	const w, h = 40, 40
	bg := color.NRGBA{20, 20, 20, 255}
	fg := color.NRGBA{230, 230, 230, 255}
	img := solidImage(w, h, func(x, y int) color.NRGBA {
		if x >= 18 && x < 21 && y >= 18 && y < 21 { // 3×3 block, area 9
			return fg
		}
		return bg
	})

	// MinSize 0 so the block is not merged away first; the default freeze floor
	// (defaultSmoothProtect = 16 >= 9) must still protect it.
	base := Segment(img, Options{K: 50, MinSize: 0})
	if base.NumRegions != 2 {
		t.Fatalf("base NumRegions = %d, want 2 (background + 3×3 block)", base.NumRegions)
	}

	sm := Segment(img, Options{K: 50, MinSize: 0, BoundarySmooth: 8})
	if sm.NumRegions != 2 {
		t.Fatalf("smoothed NumRegions = %d, want 2 (tiny region must survive)", sm.NumRegions)
	}
	assertDense(t, sm)
	assertContiguous(t, sm)

	// The tiny region must retain its exact 9 pixels and its fg colour.
	areas := regionAreas(sm)
	means := MeanColors(img, sm)
	foundTiny := false
	for id, a := range areas {
		if a == 9 {
			foundTiny = true
			m := means[id]
			if m.R != fg.R || m.G != fg.G || m.B != fg.B {
				t.Fatalf("tiny region colour = %v, want fg %v", m, fg)
			}
		}
	}
	if !foundTiny {
		t.Fatal("tiny 3×3 region was eroded by smoothing: no region of area 9 remains")
	}
}

// TestBoundarySmoothDeterministic verifies identical (img, Options) inputs with
// smoothing enabled produce identical Labels.
func TestBoundarySmoothDeterministic(t *testing.T) {
	const w, h = 50, 50
	img := solidImage(w, h, func(x, y int) color.NRGBA {
		v := uint8((x*7 + y*13) % 256)
		return color.NRGBA{v, uint8((x * 3) % 256), uint8((y * 5) % 256), 255}
	})
	opt := Options{K: 200, MinSize: 4, Sigma: 0.8, BoundarySmooth: 5}
	a := Segment(img, opt)
	b := Segment(img, opt)
	if a.NumRegions != b.NumRegions {
		t.Fatalf("NumRegions differ across runs: %d vs %d", a.NumRegions, b.NumRegions)
	}
	if !reflect.DeepEqual(a.Labels, b.Labels) {
		t.Fatal("Labels differ across identical smoothed runs: not deterministic")
	}
}

// TestBoundarySmoothZeroIsNoop confirms BoundarySmooth == 0 is exactly the
// pre-existing behaviour (back-compat) and that a positive count actually
// changes a jagged partition, i.e. the field is wired and 0 truly disables it.
func TestBoundarySmoothZeroIsNoop(t *testing.T) {
	const w, h = 48, 48
	img := solidImage(w, h, func(x, y int) color.NRGBA {
		split := 24
		if (y/3)%2 == 0 {
			split += 3
		} else {
			split -= 3
		}
		if x < split {
			return color.NRGBA{200, 40, 40, 255}
		}
		return color.NRGBA{40, 40, 200, 255}
	})

	noField := Segment(img, Options{K: 500, MinSize: 0})
	zero := Segment(img, Options{K: 500, MinSize: 0, BoundarySmooth: 0})
	if !reflect.DeepEqual(noField.Labels, zero.Labels) {
		t.Fatal("BoundarySmooth:0 changed the output; zero value must be a no-op")
	}

	smoothed := Segment(img, Options{K: 500, MinSize: 0, BoundarySmooth: 8})
	if reflect.DeepEqual(noField.Labels, smoothed.Labels) {
		t.Fatal("BoundarySmooth:8 produced identical labels; smoothing had no effect")
	}
}

func TestEmptyImage(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 0, 0))
	r := Segment(img, Options{K: 100})
	if r.NumRegions != 0 || len(r.Labels) != 0 {
		t.Fatalf("empty image: got NumRegions=%d len(Labels)=%d, want 0/0", r.NumRegions, len(r.Labels))
	}
}

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()
	if o.RangeSigma != DefaultRangeSigma || DefaultRangeSigma != 12 {
		t.Fatalf("DefaultOptions RangeSigma = %v, DefaultRangeSigma = %v, want 12", o.RangeSigma, DefaultRangeSigma)
	}
	if o.PreFilter != PreFilterBilateral {
		t.Fatalf("DefaultOptions PreFilter = %v, want bilateral", o.PreFilter)
	}
	if o.K <= 0 || o.MinSize <= 0 || o.SpatialSigma <= 0 || o.BoundarySmooth < 0 {
		t.Fatalf("DefaultOptions has non-sensible values: %+v", o)
	}
	// It must produce a valid segmentation on a small image.
	img := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	res := Segment(img, DefaultOptions())
	if res.NumRegions < 1 {
		t.Fatalf("DefaultOptions produced %d regions on a solid image, want >=1", res.NumRegions)
	}
}
