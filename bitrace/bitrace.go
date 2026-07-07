package bitrace

import "math"

// Bitmap is a binary mask: a rectangular grid of on/off pixels.
//
// Bits is a flat, row-major slice of length W*H. The pixel at column x, row y
// is stored at index y*W + x and is "on" when the value is true. Using a flat
// slice (rather than a nested [][]bool) keeps the data cache-friendly.
type Bitmap struct {
	// W is the width of the bitmap in pixels.
	W int
	// H is the height of the bitmap in pixels.
	H int
	// Bits holds W*H on/off values in row-major order; index = y*W + x.
	Bits []bool
}

// At reports whether the pixel at (x, y) is on. Coordinates outside the bitmap
// are reported as off, which lets border tracing treat the image edge as a
// boundary without special-casing.
func (b Bitmap) At(x, y int) bool {
	if x < 0 || y < 0 || x >= b.W || y >= b.H {
		return false
	}
	return b.Bits[y*b.W+x]
}

// Point is a 2-D coordinate in pixel space.
type Point struct {
	X float64
	Y float64
}

// CmdKind identifies the kind of a path [Command].
type CmdKind int

const (
	// MoveTo starts a new subpath at Command.P without drawing.
	MoveTo CmdKind = iota
	// LineTo draws a straight line from the current point to Command.P.
	LineTo
	// CubicTo draws a cubic Bézier from the current point using control
	// points Command.C1 and Command.C2 and ending at Command.P.
	CubicTo
	// Close closes the current subpath back to its starting point. Its point
	// fields are unused.
	Close
)

// Command is a single drawing instruction within a [Path].
//
// Which point fields are meaningful depends on Kind:
//
//   - MoveTo, LineTo: P is the target point; C1 and C2 are unused.
//   - CubicTo:        C1 and C2 are the control points, P is the end point.
//   - Close:          no points are used.
type Command struct {
	// Kind selects how the point fields are interpreted.
	Kind CmdKind
	// C1 is the first control point (CubicTo only).
	C1 Point
	// C2 is the second control point (CubicTo only).
	C2 Point
	// P is the end/target point (MoveTo, LineTo, CubicTo).
	P Point
}

// Path is a single closed contour produced by [Trace].
//
// Commands always begins with a MoveTo and ends with a Close. IsHole reports
// whether the contour is an inner boundary (a hole) rather than an outer
// boundary; see the package documentation for the winding convention.
type Path struct {
	// Commands is the ordered drawing program for the contour.
	Commands []Command
	// IsHole is true when the contour bounds a hole (inner boundary).
	IsHole bool
}

// Config controls tracing behaviour.
type Config struct {
	// TurdSize is the speckle-area threshold in pixels. Contours enclosing
	// fewer than TurdSize pixels are discarded. A value of 0 disables speckle
	// removal. The recommended default is 2.
	TurdSize int

	// AlphaMax is the corner/smoothness threshold. Lower values treat more
	// vertices as corners (more angular output); higher values treat more
	// vertices as smooth (more curves). It is clamped to [0, 1.334]. The
	// recommended default is 1.0.
	AlphaMax float64

	// Optimize enables looser curve fitting, which yields fewer, larger
	// Bézier segments (smaller output) at a small cost in fidelity. The
	// recommended default is true.
	Optimize bool
}

// DefaultConfig returns the recommended default configuration: TurdSize 2,
// AlphaMax 1.0, Optimize true.
func DefaultConfig() Config {
	return Config{
		TurdSize: 2,
		AlphaMax: 1.0,
		Optimize: true,
	}
}

// maxAlphaMax is the upper clamp for Config.AlphaMax.
const maxAlphaMax = 1.334

// Trace converts a binary mask into vector paths made of straight lines and
// cubic Bézier curves.
//
// It extracts the contours of the bitmap's "on" regions (outer boundaries and
// holes), removes speckles smaller than cfg.TurdSize, detects corners according
// to cfg.AlphaMax, fits cubic Bézier curves to the smooth runs, and returns the
// results as an ordered slice of [Path]. Hole contours have [Path.IsHole] set.
//
// Trace never panics on well-formed input. It returns an error only when the
// bitmap is malformed (negative dimensions, or a Bits slice whose length does
// not match W*H). An empty or all-off bitmap yields a nil slice and no error.
//
// Trace does not retain or mutate bm; it is safe for concurrent use.
func Trace(bm Bitmap, cfg Config) ([]Path, error) {
	if bm.W < 0 || bm.H < 0 {
		return nil, errBadDimensions
	}
	if len(bm.Bits) != bm.W*bm.H {
		return nil, errBitsLength
	}
	if bm.W == 0 || bm.H == 0 {
		return nil, nil
	}

	alpha := cfg.AlphaMax
	if alpha < 0 {
		alpha = 0
	}
	if alpha > maxAlphaMax {
		alpha = maxAlphaMax
	}

	accuracy := 0.3
	if cfg.Optimize {
		accuracy = 1.0
	}

	contours := extractContours(bm)

	var paths []Path
	for _, c := range contours {
		area := shoelace(c)
		if math.Abs(area) < float64(cfg.TurdSize) {
			continue
		}
		cmds := traceContour(c, alpha, accuracy)
		if len(cmds) == 0 {
			continue
		}
		paths = append(paths, Path{
			Commands: cmds,
			IsHole:   area > 0,
		})
	}
	return paths, nil
}

// shoelace returns the signed area of the closed polygon poly using the
// shoelace formula. The polygon is treated as closed (poly[len-1] connects back
// to poly[0]); poly must not repeat its first vertex at the end.
func shoelace(poly []Point) float64 {
	n := len(poly)
	if n < 3 {
		return 0
	}
	sum := 0.0
	for i := 0; i < n; i++ {
		j := i + 1
		if j == n {
			j = 0
		}
		sum += poly[i].X*poly[j].Y - poly[j].X*poly[i].Y
	}
	return sum / 2
}
