package bitrace

import (
	"math"

	"honnef.co/go/curve"
)

// polyCurve adapts an open polyline to curve.FittableCurve so that
// honnef.co/go/curve can fit cubic Béziers to it. The polyline is parametrised
// by normalised arc length: parameter t in [0, 1] maps linearly to distance
// along the polyline.
//
// The polyline is assumed to be a smooth run (any true corners were split out
// beforehand), so BreakCusp reports no interior discontinuities.
type polyCurve struct {
	pts   []curve.Point
	cum   []float64 // cumulative arc length; len == len(pts); cum[0] == 0
	total float64
}

// newPolyCurve builds a polyCurve from pts (which must have at least two
// points). Consecutive duplicate points are collapsed so segment lengths are
// positive.
func newPolyCurve(pts []Point) polyCurve {
	cp := make([]curve.Point, 0, len(pts))
	for _, p := range pts {
		q := curve.Pt(p.X, p.Y)
		if len(cp) > 0 {
			last := cp[len(cp)-1]
			if last.X == q.X && last.Y == q.Y {
				continue
			}
		}
		cp = append(cp, q)
	}
	cum := make([]float64, len(cp))
	for i := 1; i < len(cp); i++ {
		d := math.Hypot(cp[i].X-cp[i-1].X, cp[i].Y-cp[i-1].Y)
		cum[i] = cum[i-1] + d
	}
	total := 0.0
	if len(cum) > 0 {
		total = cum[len(cum)-1]
	}
	return polyCurve{pts: cp, cum: cum, total: total}
}

// locate maps arc-length distance s to a segment index in [0, len(pts)-2] and a
// local parameter in [0, 1] within that segment.
func (pc polyCurve) locate(s float64) (int, float64) {
	if s <= 0 {
		return 0, 0
	}
	if s >= pc.total {
		return len(pc.pts) - 2, 1
	}
	// Binary search for the last index whose cumulative length is <= s.
	lo, hi := 0, len(pc.cum)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if pc.cum[mid] <= s {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	i := lo
	if i > len(pc.pts)-2 {
		i = len(pc.pts) - 2
	}
	segLen := pc.cum[i+1] - pc.cum[i]
	lt := 0.0
	if segLen > 0 {
		lt = (s - pc.cum[i]) / segLen
	}
	return i, lt
}

// SamplePtDeriv implements curve.FittableCurve.
func (pc polyCurve) SamplePtDeriv(t float64) (curve.Point, curve.Vec2) {
	s := t * pc.total
	i, lt := pc.locate(s)
	a, b := pc.pts[i], pc.pts[i+1]
	p := curve.Pt(a.X+(b.X-a.X)*lt, a.Y+(b.Y-a.Y)*lt)
	segLen := pc.cum[i+1] - pc.cum[i]
	scale := 0.0
	if segLen > 0 {
		scale = pc.total / segLen
	}
	deriv := curve.Vec(b.Sub(a).X*scale, b.Sub(a).Y*scale)
	return p, deriv
}

// SamplePtTangent implements curve.FittableCurve. At an interior vertex the sign
// parameter selects which adjacent segment provides the tangent.
func (pc polyCurve) SamplePtTangent(t float64, sign float64) curve.CurveFitSample {
	s := t * pc.total
	i, lt := pc.locate(s)
	a, b := pc.pts[i], pc.pts[i+1]
	p := curve.Pt(a.X+(b.X-a.X)*lt, a.Y+(b.Y-a.Y)*lt)

	// Choose the tangent segment, honouring sign at exact vertices.
	seg := i
	const eps = 1e-9
	if lt <= eps && sign < 0 && i > 0 {
		seg = i - 1
	} else if lt >= 1-eps && sign > 0 && i+2 < len(pc.pts) {
		seg = i + 1
	}
	ta, tb := pc.pts[seg], pc.pts[seg+1]
	tx, ty := normalize(tb.X-ta.X, tb.Y-ta.Y)
	return curve.CurveFitSample{Point: p, Tangent: curve.Vec(tx, ty)}
}

// BreakCusp implements curve.FittableCurve. Smooth runs contain no interior
// cusps, so this always reports none.
func (pc polyCurve) BreakCusp(start, end float64) (float64, bool) {
	return 0, false
}

// fitCubics fits cubic Béziers to the open polyline pts and returns the drawing
// commands, excluding the leading MoveTo (the caller has already positioned the
// pen at pts[0]). All emitted commands are CubicTo or LineTo.
func fitCubics(pts []Point, accuracy float64) []Command {
	pc := newPolyCurve(pts)
	if len(pc.pts) < 2 || pc.total == 0 {
		return nil
	}
	if len(pc.pts) == 2 {
		return []Command{{Kind: LineTo, P: pts[len(pts)-1]}}
	}

	var cmds []Command
	var cur curve.Point // current pen position (start of the next segment)
	for el := range curve.FitToBezPath(pc, accuracy) {
		cmds = appendElement(cmds, el, &cur)
	}
	return cmds
}

// appendElement converts a single fitted path element into drawing commands,
// appending them to cmds and advancing *cur to the element's end point (the
// start of the following segment). It returns the extended command slice.
func appendElement(cmds []Command, el curve.PathElement, cur *curve.Point) []Command {
	switch el.Kind {
	case curve.MoveToKind:
		// Skip emitting: the pen is already at the start point.
		*cur = el.P0
	case curve.LineToKind:
		cmds = append(cmds, Command{Kind: LineTo, P: pt(el.P0)})
		*cur = el.P0
	case curve.QuadToKind:
		// Elevate a quadratic to a cubic (the fitter does not emit these, but
		// handle it for completeness). The quad's start point is the current
		// pen position, its control is el.P0 and its end is el.P1.
		c1, c2 := quadToCubic(*cur, el.P0, el.P1)
		cmds = append(cmds, Command{Kind: CubicTo, C1: pt(c1), C2: pt(c2), P: pt(el.P1)})
		*cur = el.P1
	case curve.CubicToKind:
		cmds = append(cmds, Command{Kind: CubicTo, C1: pt(el.P0), C2: pt(el.P1), P: pt(el.P2)})
		*cur = el.P2
	case curve.ClosePathKind:
		// Ignored: closing is handled by the caller.
	}
	return cmds
}

// quadToCubic performs exact quadratic-to-cubic degree elevation. For a
// quadratic Bézier with endpoints p0, p2 and control q, the equivalent cubic
// has the same endpoints and control points:
//
//	c1 = p0 + (2/3)*(q - p0)
//	c2 = p2 + (2/3)*(q - p2)
func quadToCubic(p0, q, p2 curve.Point) (c1, c2 curve.Point) {
	c1 = curve.Point{X: p0.X + (2.0/3.0)*(q.X-p0.X), Y: p0.Y + (2.0/3.0)*(q.Y-p0.Y)}
	c2 = curve.Point{X: p2.X + (2.0/3.0)*(q.X-p2.X), Y: p2.Y + (2.0/3.0)*(q.Y-p2.Y)}
	return c1, c2
}

// pt converts a curve.Point to a bitrace.Point.
func pt(p curve.Point) Point { return Point{X: p.X, Y: p.Y} }

// Ensure polyCurve satisfies the interface at compile time.
var _ curve.FittableCurve = polyCurve{}
