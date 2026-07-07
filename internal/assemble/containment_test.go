package assemble

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/vectrigo/internal/pipeline"
	"golang.org/x/image/vector"
)

// solidRect builds a single solid (no-hole) rectangular layer with an explicit
// area, so tests can decouple painted geometry from the area used for ordering.
func solidRect(c color.RGBA, area int, x0, y0, x1, y1 float64) pipeline.Traced {
	return pipeline.Traced{
		Color: c,
		Area:  area,
		Paths: []bitrace.Path{rectPath(x0, y0, x1, y1, false)},
	}
}

// rasterizeDoc paints the ordered layers onto a WxH canvas in document order
// (index 0 first / behind, last on top), exactly mirroring how a renderer
// paints the emitted <path> stack, and returns the canvas.
func rasterizeDoc(order []pipeline.Traced, w, h int) *image.RGBA {
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for _, tr := range order {
		if len(tr.Paths) == 0 {
			continue
		}
		rz := vector.NewRasterizer(w, h)
		for _, p := range tr.Paths {
			for _, c := range p.Commands {
				switch c.Kind {
				case bitrace.MoveTo:
					rz.MoveTo(float32(c.P.X), float32(c.P.Y))
				case bitrace.LineTo:
					rz.LineTo(float32(c.P.X), float32(c.P.Y))
				case bitrace.CubicTo:
					rz.CubeTo(float32(c.C1.X), float32(c.C1.Y),
						float32(c.C2.X), float32(c.C2.Y),
						float32(c.P.X), float32(c.P.Y))
				case bitrace.Close:
					rz.ClosePath()
				}
			}
		}
		rz.Draw(canvas, canvas.Bounds(), image.NewUniform(tr.Color), image.Point{})
	}
	return canvas
}

func colorAt(t *testing.T, img *image.RGBA, x, y int) color.RGBA {
	t.Helper()
	r, g, b, a := img.RGBAAt(x, y).RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func indexOfColor(order []pipeline.Traced, c color.RGBA) int {
	for i, t := range order {
		if t.Color == c {
			return i
		}
	}
	return -1
}

// TestOrderEnclosedLargerAreaPaintsOnTop is the core fix: an enclosed layer
// with the LARGER area (which the old area-only ordering would paint first and
// thus occlude) must be emitted AFTER its solid encloser and remain visible.
func TestOrderEnclosedLargerAreaPaintsOnTop(t *testing.T) {
	blue := color.RGBA{R: 10, G: 30, B: 220, A: 255} // encloser, solid, SMALL area
	red := color.RGBA{R: 220, G: 20, B: 20, A: 255}  // enclosed, LARGER area

	traced := []pipeline.Traced{
		solidRect(red, 900, 40, 40, 60, 60),  // inner, bigger area
		solidRect(blue, 500, 0, 0, 100, 100), // outer solid square, smaller area
	}

	order := Order(traced)

	bi := indexOfColor(order, blue)
	ri := indexOfColor(order, red)
	if !(bi < ri) {
		t.Fatalf("document order: blue at %d, red at %d; want blue BEFORE red (encloser behind)", bi, ri)
	}

	// Rasterize-back: the inner shape must actually be visible, not overpainted.
	img := rasterizeDoc(order, 100, 100)
	if got := colorAt(t, img, 50, 50); got != red {
		t.Errorf("center pixel = %v, want red %v (inner shape occluded)", got, red)
	}
	// A point covered only by the encloser stays the encloser's colour.
	if got := colorAt(t, img, 5, 5); got != blue {
		t.Errorf("corner pixel = %v, want blue %v", got, blue)
	}
}

// TestOrderNestedDepthTwo covers two nesting levels with areas deliberately
// inverted (innermost largest), asserting document order is outer->middle->inner
// and every level is visible in the reconstruction.
func TestOrderNestedDepthTwo(t *testing.T) {
	white := color.RGBA{R: 245, G: 245, B: 245, A: 255} // outermost
	blue := color.RGBA{R: 20, G: 40, B: 200, A: 255}    // middle
	red := color.RGBA{R: 210, G: 20, B: 20, A: 255}     // innermost, LARGEST area

	traced := []pipeline.Traced{
		solidRect(red, 5000, 50, 50, 70, 70),   // innermost, biggest area
		solidRect(blue, 200, 20, 20, 100, 100), // middle
		solidRect(white, 3000, 0, 0, 120, 120), // outermost background
	}

	order := Order(traced)

	wi := indexOfColor(order, white)
	bi := indexOfColor(order, blue)
	ri := indexOfColor(order, red)
	if !(wi < bi && bi < ri) {
		t.Fatalf("document order white=%d blue=%d red=%d; want white<blue<red", wi, bi, ri)
	}

	img := rasterizeDoc(order, 120, 120)
	checks := []struct {
		x, y int
		want color.RGBA
		name string
	}{
		{60, 60, red, "inner"},
		{30, 30, blue, "middle"},
		{5, 5, white, "outer"},
	}
	for _, c := range checks {
		if got := colorAt(t, img, c.x, c.y); got != c.want {
			t.Errorf("%s pixel (%d,%d) = %v, want %v", c.name, c.x, c.y, got, c.want)
		}
	}
}

// TestOrderSiblingsUseArea confirms the refinement is conservative: two
// non-enclosing (side-by-side) layers keep the previous area-descending order.
func TestOrderSiblingsUseArea(t *testing.T) {
	small := color.RGBA{R: 200, G: 0, B: 0, A: 255}
	large := color.RGBA{R: 0, G: 0, B: 200, A: 255}

	traced := []pipeline.Traced{
		solidRect(small, 100, 0, 0, 10, 10),   // left, small
		solidRect(large, 900, 50, 50, 80, 80), // right, large, not enclosing
	}
	order := Order(traced)
	if indexOfColor(order, large) != 0 {
		t.Errorf("largest sibling should sort first (behind); order=%v", order)
	}
}

// TestOrderRespectsHolesNoFalseEnclosure verifies the spatial test uses the
// nonzero fill (holes read as outside): a shape sitting inside another layer's
// HOLE is not treated as enclosed by it, so area ordering is preserved.
func TestOrderHoleIsNotEnclosure(t *testing.T) {
	// "frame" is an annulus: big outer square with a central hole.
	frame := color.RGBA{R: 0, G: 150, B: 0, A: 255}
	dot := color.RGBA{R: 200, G: 0, B: 0, A: 255}

	traced := []pipeline.Traced{
		{Color: frame, Area: 800, Paths: []bitrace.Path{
			rectPath(0, 0, 100, 100, false),
			rectPath(30, 30, 70, 70, true), // hole in the middle
		}},
		// dot lives inside the frame's HOLE (not inside its painted region).
		solidRect(dot, 400, 40, 40, 60, 60),
	}
	order := Order(traced)
	// frame has larger area and does not enclose dot (dot is in its hole), so
	// frame sorts first purely by area — same as before.
	if indexOfColor(order, frame) != 0 {
		t.Errorf("frame (larger area, hole not enclosure) should sort first; order=%v", order)
	}
	// Sanity: the dot is visible either way (disjoint from frame's fill).
	img := rasterizeDoc(order, 100, 100)
	if got := colorAt(t, img, 50, 50); got != dot {
		t.Errorf("center = %v, want dot %v", got, dot)
	}
}

// TestOrderDeterministic runs Order repeatedly on a shuffled input and checks
// the emitted colour sequence is identical every time.
func TestOrderDeterministic(t *testing.T) {
	traced := []pipeline.Traced{
		solidRect(color.RGBA{R: 1, A: 255}, 900, 40, 40, 60, 60),
		solidRect(color.RGBA{B: 1, A: 255}, 500, 0, 0, 100, 100),
		solidRect(color.RGBA{G: 1, A: 255}, 700, 45, 45, 55, 55),
	}
	first := Order(traced)
	for i := 0; i < 20; i++ {
		got := Order(traced)
		for j := range first {
			if got[j].Color != first[j].Color {
				t.Fatalf("run %d differs at %d: %v vs %v", i, j, got[j].Color, first[j].Color)
			}
		}
	}
}

// TestOrderDoesNotMutateInput guards the documented no-mutation contract.
func TestOrderDoesNotMutateInput(t *testing.T) {
	traced := []pipeline.Traced{
		solidRect(color.RGBA{R: 1, A: 255}, 900, 40, 40, 60, 60),
		solidRect(color.RGBA{B: 1, A: 255}, 500, 0, 0, 100, 100),
	}
	before := []color.RGBA{traced[0].Color, traced[1].Color}
	_ = Order(traced)
	if traced[0].Color != before[0] || traced[1].Color != before[1] {
		t.Errorf("Order mutated its input slice")
	}
}

// Ensure the containment path integrates through WriteSVG end-to-end: the
// enclosed larger-area layer's <path> comes last in the document.
func TestWriteSVGContainmentDocumentOrder(t *testing.T) {
	blue := color.RGBA{R: 10, G: 30, B: 220, A: 255}
	red := color.RGBA{R: 220, G: 20, B: 20, A: 255}
	traced := []pipeline.Traced{
		solidRect(red, 900, 40, 40, 60, 60),
		solidRect(blue, 500, 0, 0, 100, 100),
	}
	var buf bytes.Buffer
	if err := WriteSVG(&buf, traced, testImage(100, 100), Options{Optimize: true, Precision: 2}); err != nil {
		t.Fatalf("WriteSVG: %v", err)
	}
	p := parseSVG(t, buf.Bytes())
	if len(p.fills) != 2 {
		t.Fatalf("want 2 paths, got %d", len(p.fills))
	}
	if p.fills[0] != "#0a1edc" { // blue encloser first
		t.Errorf("first fill = %s, want blue #0a1edc (encloser behind)", p.fills[0])
	}
	if p.fills[1] != "#dc1414" { // red enclosed last (on top)
		t.Errorf("last fill = %s, want red #dc1414 (enclosed on top)", p.fills[1])
	}
}
