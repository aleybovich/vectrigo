package bitrace

import (
	"math"
	"testing"
)

// --- bitmap builders ---------------------------------------------------------

// filledRect returns a w×h bitmap with the half-open rectangle
// [x0,x1)×[y0,y1) set on.
func filledRect(w, h, x0, y0, x1, y1 int) Bitmap {
	b := Bitmap{W: w, H: h, Bits: make([]bool, w*h)}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if x >= 0 && y >= 0 && x < w && y < h {
				b.Bits[y*w+x] = true
			}
		}
	}
	return b
}

// filledDisk returns a w×h bitmap with a disk of radius r centred at (cx, cy).
func filledDisk(w, h, cx, cy, r int) Bitmap {
	b := Bitmap{W: w, H: h, Bits: make([]bool, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r*r {
				b.Bits[y*w+x] = true
			}
		}
	}
	return b
}

// filledDonut returns a disk of radius rOuter with a concentric hole of radius
// rInner removed.
func filledDonut(w, h, cx, cy, rOuter, rInner int) Bitmap {
	b := filledDisk(w, h, cx, cy, rOuter)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= rInner*rInner {
				b.Bits[y*w+x] = false
			}
		}
	}
	return b
}

// twoDiskUnion returns a w×h bitmap set on inside either of two overlapping
// disks: radius r1 centred at (c1x, c1y) or radius r2 centred at (c2x, c2y).
// The union of two tangent/overlapping disks is a blobby "peanut" shape whose
// contour commonly has a single detected corner at the neck.
func twoDiskUnion(w, h, c1x, c1y, r1, c2x, c2y, r2 int) Bitmap {
	b := Bitmap{W: w, H: h, Bits: make([]bool, w*h)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d1 := (x-c1x)*(x-c1x) + (y-c1y)*(y-c1y)
			d2 := (x-c2x)*(x-c2x) + (y-c2y)*(y-c2y)
			if d1 <= r1*r1 || d2 <= r2*r2 {
				b.Bits[y*w+x] = true
			}
		}
	}
	return b
}

// unitStaircase returns a w×h bitmap filled below the main diagonal (x <= y),
// producing a single-pixel staircase edge.
func unitStaircase(n int) Bitmap {
	b := Bitmap{W: n, H: n, Bits: make([]bool, n*n)}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if x <= y {
				b.Bits[y*n+x] = true
			}
		}
	}
	return b
}

// --- helpers on output -------------------------------------------------------

func countKinds(cmds []Command) map[CmdKind]int {
	m := map[CmdKind]int{}
	for _, c := range cmds {
		m[c.Kind]++
	}
	return m
}

func isClosed(p Path) bool {
	return len(p.Commands) >= 2 &&
		p.Commands[0].Kind == MoveTo &&
		p.Commands[len(p.Commands)-1].Kind == Close
}

// polyOfAnchors returns the anchor point of every drawing command, i.e. the
// polygon of segment endpoints, for winding/area checks.
func polyOfAnchors(p Path) []Point {
	var out []Point
	for _, c := range p.Commands {
		switch c.Kind {
		case MoveTo, LineTo, CubicTo:
			out = append(out, c.P)
		}
	}
	return out
}

// samplePolyline flattens a path into a dense polyline for geometric checks.
func samplePolyline(p Path, perCubic int) []Point {
	var out []Point
	var cur Point
	for _, c := range p.Commands {
		switch c.Kind {
		case MoveTo:
			cur = c.P
			out = append(out, cur)
		case LineTo:
			out = append(out, c.P)
			cur = c.P
		case CubicTo:
			for i := 1; i <= perCubic; i++ {
				t := float64(i) / float64(perCubic)
				mt := 1 - t
				x := mt*mt*mt*cur.X + 3*mt*mt*t*c.C1.X + 3*mt*t*t*c.C2.X + t*t*t*c.P.X
				y := mt*mt*mt*cur.Y + 3*mt*mt*t*c.C1.Y + 3*mt*t*t*c.C2.Y + t*t*t*c.P.Y
				out = append(out, Point{x, y})
			}
			cur = c.P
		}
	}
	return out
}

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// --- tests -------------------------------------------------------------------

func TestFilledRectangle(t *testing.T) {
	bm := filledRect(12, 10, 2, 2, 8, 7) // 6 wide, 5 tall
	paths, err := Trace(bm, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %d", len(paths))
	}
	p := paths[0]
	if p.IsHole {
		t.Errorf("outer rectangle must not be a hole")
	}
	if !isClosed(p) {
		t.Errorf("path must start with MoveTo and end with Close")
	}
	k := countKinds(p.Commands)
	if k[CubicTo] != 0 {
		t.Errorf("a rectangle must have no cubic segments, got %d", k[CubicTo])
	}
	if k[MoveTo] != 1 || k[Close] != 1 {
		t.Errorf("want exactly one MoveTo and one Close, got move=%d close=%d", k[MoveTo], k[Close])
	}
	// Four corners → four straight segments.
	if k[LineTo] != 4 {
		t.Errorf("rectangle should have 4 line segments, got %d", k[LineTo])
	}
	// The anchor polygon must be the rectangle [2,2]-[8,7].
	poly := polyOfAnchors(p)
	minX, minY, maxX, maxY := 1e9, 1e9, -1e9, -1e9
	for _, q := range poly {
		minX, maxX = math.Min(minX, q.X), math.Max(maxX, q.X)
		minY, maxY = math.Min(minY, q.Y), math.Max(maxY, q.Y)
	}
	if minX != 2 || minY != 2 || maxX != 8 || maxY != 7 {
		t.Errorf("rectangle bounds = [%v,%v]-[%v,%v], want [2,2]-[8,7]", minX, minY, maxX, maxY)
	}
	// Signed area for an outer contour is negative in image space.
	if a := shoelace(poly); a >= 0 {
		t.Errorf("outer contour signed area = %v, want negative", a)
	}
}

func TestFilledDisk(t *testing.T) {
	const cx, cy, r = 20, 20, 15
	bm := filledDisk(40, 40, cx, cy, r)
	paths, err := Trace(bm, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %d", len(paths))
	}
	p := paths[0]
	if p.IsHole {
		t.Errorf("disk must not be a hole")
	}
	if !isClosed(p) {
		t.Errorf("disk path must be closed")
	}
	if countKinds(p.Commands)[CubicTo] == 0 {
		t.Errorf("disk must be fitted with cubic Bézier curves")
	}
	// Every sampled point must lie within ~1.5px of the true radius.
	pts := samplePolyline(p, 40)
	minR, maxR := math.Inf(1), 0.0
	for _, q := range pts {
		rr := math.Hypot(q.X-cx, q.Y-cy)
		minR = math.Min(minR, rr)
		maxR = math.Max(maxR, rr)
	}
	if !approx(minR, r, 1.75) || !approx(maxR, r, 1.75) {
		t.Errorf("radial range [%.2f, %.2f] not within 1.75px of r=%d", minR, maxR, r)
	}
}

func TestDonutHoleWinding(t *testing.T) {
	const cx, cy = 25, 25
	bm := filledDonut(50, 50, cx, cy, 18, 8)
	paths, err := Trace(bm, DefaultConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("donut must yield 2 contours, got %d", len(paths))
	}
	var holes, outers int
	for _, p := range paths {
		if !isClosed(p) {
			t.Errorf("donut contour must be closed")
		}
		// Sample the fitted curve densely so the shoelace area is meaningful
		// even for cubic-fitted contours.
		poly := samplePolyline(p, 24)
		a := shoelace(poly)
		if p.IsHole {
			holes++
			if a <= 0 {
				t.Errorf("hole signed area = %v, want positive", a)
			}
		} else {
			outers++
			if a >= 0 {
				t.Errorf("outer signed area = %v, want negative", a)
			}
		}
	}
	if outers != 1 || holes != 1 {
		t.Errorf("want exactly 1 outer and 1 hole, got outer=%d hole=%d", outers, holes)
	}
}

func TestSpeckleRemoval(t *testing.T) {
	// A real rectangle plus a lone speckle pixel far away.
	bm := filledRect(20, 20, 2, 2, 10, 10)
	bm.Bits[15*20+15] = true // isolated 1px speckle, area 1

	// TurdSize 2 removes the speckle.
	kept, err := Trace(bm, Config{TurdSize: 2, AlphaMax: 1.0, Optimize: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 {
		t.Errorf("with TurdSize=2 want 1 path (speckle removed), got %d", len(kept))
	}

	// TurdSize 0 keeps everything.
	all, err := Trace(bm, Config{TurdSize: 0, AlphaMax: 1.0, Optimize: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("with TurdSize=0 want 2 paths (speckle kept), got %d", len(all))
	}
}

func TestStaircaseAlphaMax(t *testing.T) {
	bm := unitStaircase(16)
	contours := extractContours(bm)
	if len(contours) != 1 {
		t.Fatalf("staircase must have exactly 1 contour, got %d", len(contours))
	}
	c := contours[0]

	countCorners := func(alpha float64) int {
		n := 0
		for _, isCorner := range detectCorners(c, alpha) {
			if isCorner {
				n++
			}
		}
		return n
	}

	// Corner count must be non-increasing as AlphaMax rises.
	alphas := []float64{0.1, 0.3, 0.5, 1.0, 1.334}
	prev := math.MaxInt32
	counts := make([]int, len(alphas))
	for i, a := range alphas {
		counts[i] = countCorners(a)
		if counts[i] > prev {
			t.Errorf("corners not monotonic: alpha=%v gave %d, previous %d", a, counts[i], prev)
		}
		prev = counts[i]
	}
	// And the effect must be substantial: low alpha keeps far more corners.
	if counts[0] <= counts[len(counts)-1] {
		t.Errorf("expected low alpha (%d corners) to keep more corners than high alpha (%d)",
			counts[0], counts[len(counts)-1])
	}

	// End-to-end: lower alpha yields more (or equal) line joins in the output.
	lineJoins := func(alpha float64) int {
		paths, _ := Trace(bm, Config{TurdSize: 0, AlphaMax: alpha, Optimize: true})
		n := 0
		for _, p := range paths {
			n += countKinds(p.Commands)[LineTo]
		}
		return n
	}
	if lineJoins(0.1) < lineJoins(1.334) {
		t.Errorf("low alpha should produce at least as many line joins as high alpha")
	}
}

func TestEmptyBitmap(t *testing.T) {
	bm := Bitmap{W: 8, H: 8, Bits: make([]bool, 64)}
	paths, err := Trace(bm, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Errorf("all-off bitmap must yield no paths, got %d", len(paths))
	}
}

func TestAllOnBitmap(t *testing.T) {
	bm := filledRect(6, 6, 0, 0, 6, 6)
	paths, err := Trace(bm, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("all-on bitmap must yield 1 path, got %d", len(paths))
	}
	p := paths[0]
	if p.IsHole {
		t.Errorf("all-on outline must not be a hole")
	}
	if k := countKinds(p.Commands); k[LineTo] != 4 || k[CubicTo] != 0 {
		t.Errorf("all-on outline should be a rectangle (4 lines, 0 cubics), got line=%d cubic=%d",
			k[LineTo], k[CubicTo])
	}
	poly := polyOfAnchors(p)
	if a := math.Abs(shoelace(poly)); !approx(a, 36, 1e-9) {
		t.Errorf("all-on 6x6 area = %v, want 36", a)
	}
}

func TestSinglePixel(t *testing.T) {
	bm := filledRect(3, 3, 1, 1, 2, 2) // one pixel at (1,1)

	// Kept when TurdSize allows area-1 contours.
	kept, err := Trace(bm, Config{TurdSize: 1, AlphaMax: 1.0})
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 {
		t.Fatalf("1px with TurdSize=1 want 1 path, got %d", len(kept))
	}
	if a := math.Abs(shoelace(polyOfAnchors(kept[0]))); !approx(a, 1, 1e-9) {
		t.Errorf("single pixel area = %v, want 1", a)
	}

	// Removed at the default TurdSize of 2.
	gone, err := Trace(bm, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 0 {
		t.Errorf("1px with TurdSize=2 want 0 paths, got %d", len(gone))
	}
}

func TestMultipleRegions(t *testing.T) {
	bm := Bitmap{W: 30, H: 12, Bits: make([]bool, 30*12)}
	set := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				bm.Bits[y*30+x] = true
			}
		}
	}
	set(2, 2, 8, 8)
	set(12, 2, 18, 8)
	set(22, 2, 28, 8)
	paths, err := Trace(bm, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 {
		t.Fatalf("three disjoint squares must give 3 paths, got %d", len(paths))
	}
	for _, p := range paths {
		if p.IsHole {
			t.Errorf("disjoint square must not be a hole")
		}
		if !isClosed(p) {
			t.Errorf("square path must be closed")
		}
	}
}

func TestThinLine(t *testing.T) {
	// 1px-tall horizontal line, length 10.
	bm := filledRect(14, 6, 2, 3, 12, 4)
	paths, err := Trace(bm, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("thin line must give 1 path, got %d", len(paths))
	}
	p := paths[0]
	if k := countKinds(p.Commands); k[LineTo] != 4 {
		t.Errorf("thin line outline should be a rectangle with 4 lines, got %d", k[LineTo])
	}
	// Area equals the line length (10 pixels).
	if a := math.Abs(shoelace(polyOfAnchors(p))); !approx(a, 10, 1e-9) {
		t.Errorf("thin-line area = %v, want 10", a)
	}
}

func TestTableEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		bm        Bitmap
		cfg       Config
		wantPaths int
	}{
		{"1x1-off", Bitmap{W: 1, H: 1, Bits: []bool{false}}, DefaultConfig(), 0},
		{"1x1-on-kept", Bitmap{W: 1, H: 1, Bits: []bool{true}}, Config{TurdSize: 1}, 1},
		{"1x1-on-removed", Bitmap{W: 1, H: 1, Bits: []bool{true}}, DefaultConfig(), 0},
		{"zero-width", Bitmap{W: 0, H: 5, Bits: nil}, DefaultConfig(), 0},
		{"zero-height", Bitmap{W: 5, H: 0, Bits: nil}, DefaultConfig(), 0},
		{"empty", Bitmap{W: 4, H: 4, Bits: make([]bool, 16)}, DefaultConfig(), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, err := Trace(tt.bm, tt.cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(paths) != tt.wantPaths {
				t.Errorf("got %d paths, want %d", len(paths), tt.wantPaths)
			}
		})
	}
}

func TestMalformedBitmapErrors(t *testing.T) {
	if _, err := Trace(Bitmap{W: 3, H: 3, Bits: make([]bool, 4)}, DefaultConfig()); err == nil {
		t.Errorf("mismatched Bits length must return an error")
	}
	if _, err := Trace(Bitmap{W: -1, H: 3, Bits: nil}, DefaultConfig()); err == nil {
		t.Errorf("negative dimension must return an error")
	}
}

func TestAlphaMaxClamp(t *testing.T) {
	// Out-of-range AlphaMax must not panic and must behave like the clamp ends.
	bm := filledDisk(30, 30, 15, 15, 10)
	for _, a := range []float64{-5, 0, 100} {
		if _, err := Trace(bm, Config{TurdSize: 2, AlphaMax: a}); err != nil {
			t.Errorf("AlphaMax=%v returned error: %v", a, err)
		}
	}
}

// TestOneCornerContour is a regression test for a defect where a contour with
// exactly one detected corner collapsed to a degenerate zero-area
// MoveTo/LineTo/Close path, silently dropping the shape. The bug was in arc():
// arc(poly, a, a) returned a single point (its walk condition `i != a` was false
// immediately), so traceContour's <=2-point branch emitted a line to the same
// point.
//
// This union of a radius-2 disk at (15,15) and a radius-4 disk at (16,17) yields
// a single contour whose corner detection at the default alpha marks exactly one
// vertex as a corner. The fix makes arc walk the full loop back to the corner so
// the whole boundary is fitted as one smooth run; the densely sampled path must
// enclose an area close to the source polygon's, not ~0.
func TestOneCornerContour(t *testing.T) {
	bm := twoDiskUnion(40, 40, 15, 15, 2, 16, 17, 4)

	contours := extractContours(bm)
	if len(contours) != 1 {
		t.Fatalf("want 1 contour, got %d", len(contours))
	}
	polyArea := math.Abs(shoelace(contours[0]))

	// Guard the premise: this bitmap must exercise the one-corner path.
	corners := 0
	for _, c := range detectCorners(contours[0], DefaultConfig().AlphaMax) {
		if c {
			corners++
		}
	}
	if corners != 1 {
		t.Fatalf("test premise broken: want exactly 1 detected corner, got %d", corners)
	}

	paths, err := Trace(bm, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %d", len(paths))
	}
	p := paths[0]
	if !isClosed(p) {
		t.Errorf("one-corner path must be closed")
	}
	// The degenerate output was exactly MoveTo, LineTo, Close (3 commands); the
	// fixed output traces the real boundary and has more.
	if len(p.Commands) <= 3 {
		t.Errorf("one-corner path collapsed to %d commands: %+v", len(p.Commands), p.Commands)
	}

	// The densely sampled path must enclose an area close to the source
	// polygon's, not ~0 as the degenerate path did.
	sampled := math.Abs(shoelace(samplePolyline(p, 32)))
	if sampled < 0.5*polyArea {
		t.Errorf("sampled area %.2f collapsed relative to polygon area %.2f", sampled, polyArea)
	}
	if !approx(sampled, polyArea, 0.25*polyArea) {
		t.Errorf("sampled area %.2f not within 25%% of polygon area %.2f", sampled, polyArea)
	}
}

func TestConcurrentSafe(t *testing.T) {
	bm := filledDonut(40, 40, 20, 20, 15, 6)
	done := make(chan int, 8)
	for i := 0; i < 8; i++ {
		go func() {
			p, err := Trace(bm, DefaultConfig())
			if err != nil {
				done <- -1
				return
			}
			done <- len(p)
		}()
	}
	for i := 0; i < 8; i++ {
		if n := <-done; n != 2 {
			t.Errorf("concurrent Trace returned %d paths, want 2", n)
		}
	}
}
