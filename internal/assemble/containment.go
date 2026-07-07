package assemble

import (
	"sort"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/vectrigo/internal/imageutil"
	"github.com/aleybovich/vectrigo/internal/pipeline"
)

// This file implements containment-aware layer ordering for Stage IV.
//
// SVG has no z-index: paint order is document order, so a later <path> paints
// on top of earlier ones. Ordering purely by cluster area (largest first) is a
// good default — big backgrounds land behind small foreground detail — but it
// gets the stack wrong whenever a colour layer that is spatially ENCLOSED by
// another layer happens to have the LARGER area (e.g. a big foreground object
// framed by a thin-but-wide surrounding colour, or an inner shape whose hole
// in the enclosing layer was dropped by speckle removal / smoothing so the
// enclosing layer paints solid over it). In those cases the enclosed layer,
// sorted earlier by area, gets painted over and disappears.
//
// Order fixes this by treating containment as a hard constraint and area only
// as the driver among unconstrained layers: if layer A spatially encloses
// layer B, A is always emitted before B (painted behind it), regardless of
// which has the larger area. Because this only ever ADDS ordering constraints
// where an enclosed layer would otherwise be occluded — and falls back to the
// exact previous area/colour ordering everywhere else — it is a strict
// refinement of the old behaviour and is the engine's default (see Order).
//
// Determinism: enclosure is derived from the traced geometry alone, and the
// topological sort breaks every tie by a fixed key (area desc, then packed
// colour asc, then original index), so a given set of traced layers always
// yields the identical order.

// pt is a 2-D point in working (post-downsample) pixel space.
type pt struct{ x, y float64 }

// rect is an axis-aligned bounding box.
type rect struct {
	minX, minY, maxX, maxY float64
	empty                  bool
}

// strictlyContains reports whether r fully contains o and is not identical to
// it (o's box is a proper subset of r's). Strict containment is antisymmetric
// and transitive, which is what guarantees the enclosure graph is a DAG (so the
// topological sort below can never cycle).
func (r rect) strictlyContains(o rect) bool {
	if r.empty || o.empty {
		return false
	}
	if o.minX < r.minX || o.maxX > r.maxX || o.minY < r.minY || o.maxY > r.maxY {
		return false
	}
	// Not identical: at least one bound is strictly tighter on o.
	return o.minX > r.minX || o.maxX < r.maxX || o.minY > r.minY || o.maxY < r.maxY
}

// cubicSubdiv is the number of straight segments a cubic Bézier is flattened
// into for the geometric containment tests. It only affects ordering (not the
// emitted curves), so a modest fixed value keeps the math cheap and fully
// deterministic while tracking the curve closely enough for point-in-polygon.
const cubicSubdiv = 12

// contour is a closed polygon (winding preserved from bitrace); the final
// vertex is implicitly joined back to the first.
type contour []pt

// geom is the per-layer geometry cached for ordering: every contour flattened
// to a polygon, the union bounding box, and a representative point guaranteed
// to lie inside the layer's painted (nonzero-fill) region.
type geom struct {
	contours   []contour
	bbox       rect
	interior   pt
	hasInside  bool
	hasContour bool
}

// flatten converts a layer's bitrace paths into closed polygons, subdividing
// cubic segments. Winding direction is preserved so the nonzero-fill winding
// test below matches how a renderer paints the one-path-per-colour output
// (outer and hole contours wound oppositely cancel inside holes).
func flatten(paths []bitrace.Path) []contour {
	var out []contour
	for _, p := range paths {
		var cur contour
		var prev pt
		for _, c := range p.Commands {
			switch c.Kind {
			case bitrace.MoveTo:
				if len(cur) > 0 {
					out = append(out, cur)
					cur = nil
				}
				prev = pt{c.P.X, c.P.Y}
				cur = append(cur, prev)
			case bitrace.LineTo:
				prev = pt{c.P.X, c.P.Y}
				cur = append(cur, prev)
			case bitrace.CubicTo:
				p0 := prev
				p1 := pt{c.C1.X, c.C1.Y}
				p2 := pt{c.C2.X, c.C2.Y}
				p3 := pt{c.P.X, c.P.Y}
				for i := 1; i <= cubicSubdiv; i++ {
					t := float64(i) / float64(cubicSubdiv)
					cur = append(cur, cubicAt(p0, p1, p2, p3, t))
				}
				prev = p3
			case bitrace.Close:
				// The polygon is closed implicitly by the winding test.
			}
		}
		if len(cur) > 0 {
			out = append(out, cur)
		}
	}
	return out
}

// cubicAt evaluates a cubic Bézier at parameter t in [0,1].
func cubicAt(p0, p1, p2, p3 pt, t float64) pt {
	u := 1 - t
	a := u * u * u
	b := 3 * u * u * t
	c := 3 * u * t * t
	d := t * t * t
	return pt{
		x: a*p0.x + b*p1.x + c*p2.x + d*p3.x,
		y: a*p0.y + b*p1.y + c*p2.y + d*p3.y,
	}
}

// boundsOf returns the union bounding box of the given contours.
func boundsOf(cs []contour) rect {
	r := rect{empty: true}
	for _, c := range cs {
		for _, v := range c {
			if r.empty {
				r = rect{minX: v.x, minY: v.y, maxX: v.x, maxY: v.y}
				continue
			}
			if v.x < r.minX {
				r.minX = v.x
			}
			if v.x > r.maxX {
				r.maxX = v.x
			}
			if v.y < r.minY {
				r.minY = v.y
			}
			if v.y > r.maxY {
				r.maxY = v.y
			}
		}
	}
	return r
}

// isLeft is the standard signed area of the triangle (a, b, p); >0 means p is
// left of the directed edge a->b.
func isLeft(a, b, p pt) float64 {
	return (b.x-a.x)*(p.y-a.y) - (p.x-a.x)*(b.y-a.y)
}

// insideNonzero reports whether p lies inside the region filled by cs under the
// nonzero winding rule — the same rule the SVG renderer applies to a colour's
// combined outer+hole path, so holes correctly read as "outside".
func insideNonzero(p pt, cs []contour) bool {
	w := 0
	for _, c := range cs {
		n := len(c)
		for i := 0; i < n; i++ {
			a := c[i]
			b := c[(i+1)%n]
			if a.y <= p.y {
				if b.y > p.y && isLeft(a, b, p) > 0 {
					w++
				}
			} else if b.y <= p.y && isLeft(a, b, p) < 0 {
				w--
			}
		}
	}
	return w != 0
}

// interiorPoint finds a point guaranteed to lie inside the layer's nonzero-fill
// region by scanning horizontal lines across the bounding box and returning the
// midpoint of the first interior span. Returns ok=false only for degenerate
// (zero-area) geometry, in which case the layer simply participates in ordering
// with no enclosure constraints.
func interiorPoint(cs []contour, bb rect) (pt, bool) {
	if bb.empty || bb.maxY <= bb.minY {
		return pt{}, false
	}
	const lines = 15
	for li := 1; li <= lines; li++ {
		y := bb.minY + (bb.maxY-bb.minY)*float64(li)/float64(lines+1)
		var xs []float64
		for _, c := range cs {
			n := len(c)
			for i := 0; i < n; i++ {
				a := c[i]
				b := c[(i+1)%n]
				if (a.y <= y) == (b.y <= y) {
					continue // edge does not cross this scanline
				}
				t := (y - a.y) / (b.y - a.y)
				xs = append(xs, a.x+t*(b.x-a.x))
			}
		}
		if len(xs) < 2 {
			continue
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i++ {
			if xs[i+1]-xs[i] < 1e-9 {
				continue
			}
			p := pt{x: (xs[i] + xs[i+1]) / 2, y: y}
			if insideNonzero(p, cs) {
				return p, true
			}
		}
	}
	return pt{}, false
}

// Order returns the traced layers in document (paint) order: index 0 is painted
// first (furthest back), the last is painted on top. It is containment-aware —
// a layer spatially enclosed by another is always emitted after (on top of) its
// encloser so it cannot be occluded — and uses cluster area as the base driver
// and deterministic tiebreak. See the file-level comment for the rationale.
//
// The input slice is not mutated; a new slice is returned.
func Order(traced []pipeline.Traced) []pipeline.Traced {
	n := len(traced)
	if n <= 1 {
		out := make([]pipeline.Traced, n)
		copy(out, traced)
		return out
	}

	// Precompute per-layer geometry.
	g := make([]geom, n)
	for i := range traced {
		cs := flatten(traced[i].Paths)
		if len(cs) == 0 {
			continue
		}
		bb := boundsOf(cs)
		ip, ok := interiorPoint(cs, bb)
		g[i] = geom{contours: cs, bbox: bb, interior: ip, hasInside: ok, hasContour: true}
	}

	// Build the enclosure DAG: edge j->i means layer j encloses layer i, so j
	// must be painted before (behind) i. Enclosure requires j's bbox to
	// strictly contain i's (a fast, cycle-free pre-filter) AND i's interior
	// point to fall inside j's nonzero fill (the decisive spatial test).
	indeg := make([]int, n)
	succ := make([][]int, n)
	for j := range g {
		if !g[j].hasContour {
			continue
		}
		for i := range g {
			if i == j || !g[i].hasContour || !g[i].hasInside {
				continue
			}
			if !g[j].bbox.strictlyContains(g[i].bbox) {
				continue
			}
			if insideNonzero(g[i].interior, g[j].contours) {
				succ[j] = append(succ[j], i)
				indeg[i]++
			}
		}
	}

	// Deterministic Kahn topological sort: among all layers whose enclosers are
	// already placed (indegree 0), always emit the largest-area one next, with
	// packed colour then original index as further tiebreakers — reproducing the
	// previous area/colour ordering wherever containment adds no constraint.
	less := func(a, b int) bool {
		if traced[a].Area != traced[b].Area {
			return traced[a].Area > traced[b].Area
		}
		pa, pb := imageutil.Pack(traced[a].Color), imageutil.Pack(traced[b].Color)
		if pa != pb {
			return pa < pb
		}
		return a < b
	}

	placed := make([]bool, n)
	out := make([]pipeline.Traced, 0, n)
	for len(out) < n {
		best := -1
		for i := 0; i < n; i++ {
			if placed[i] || indeg[i] > 0 {
				continue
			}
			if best == -1 || less(i, best) {
				best = i
			}
		}
		// The enclosure graph is a DAG, so a zero-indegree unplaced node always
		// exists here. This guard is purely defensive: if that invariant were
		// ever violated we still make progress by picking the best unplaced
		// node outright rather than looping forever.
		if best == -1 {
			for i := 0; i < n; i++ {
				if placed[i] {
					continue
				}
				if best == -1 || less(i, best) {
					best = i
				}
			}
		}
		placed[best] = true
		out = append(out, traced[best])
		for _, s := range succ[best] {
			indeg[s]--
		}
	}
	return out
}
