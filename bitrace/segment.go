package bitrace

import "math"

// traceContour turns one closed integer polygon into a list of drawing
// [Command]s. It detects corners (governed by alpha), keeps straight runs and
// corner joins as line segments, and fits cubic Bézier curves to the smooth
// runs with the given fitting accuracy. The returned slice starts with a MoveTo
// and ends with a Close.
func traceContour(poly []Point, alpha, accuracy float64) []Command {
	m := len(poly)
	if m < 3 {
		return nil
	}

	corners := detectCorners(poly, alpha)

	// Collect corner indices in order.
	var idx []int
	for i, c := range corners {
		if c {
			idx = append(idx, i)
		}
	}

	// No corners: fit the whole closed contour as one smooth loop. The digital
	// boundary is a unit-pixel staircase; smoothing it periodically before
	// fitting removes that aliasing so the fitter produces a few clean cubics
	// rather than tracing every step.
	if len(idx) == 0 {
		sm := smoothClosed(poly, smoothPasses)
		closed := make([]Point, len(sm)+1)
		copy(closed, sm)
		closed[len(sm)] = sm[0]
		cmds := []Command{{Kind: MoveTo, P: sm[0]}}
		cmds = append(cmds, fitCubics(closed, accuracy)...)
		cmds = append(cmds, Command{Kind: Close})
		return cmds
	}

	// Walk arcs between consecutive corners. Corners are kept fixed; only the
	// smooth interior of each arc is smoothed before fitting.
	cmds := []Command{{Kind: MoveTo, P: poly[idx[0]]}}
	for k := 0; k < len(idx); k++ {
		a := idx[k]
		b := idx[(k+1)%len(idx)]
		seg := arc(poly, a, b)
		if len(seg) <= 2 {
			// Straight run (collinear points were already merged): a line.
			cmds = append(cmds, Command{Kind: LineTo, P: poly[b]})
			continue
		}
		seg = smoothOpen(seg, smoothPasses)
		cmds = append(cmds, fitCubics(seg, accuracy)...)
	}
	cmds = append(cmds, Command{Kind: Close})
	return cmds
}

// arc returns the vertices of the closed polygon poly from index a to index b
// inclusive, walking forward with wraparound. It always walks at least one step
// (do-while semantics): when a == b it returns the full loop from vertex a
// around back to itself (m+1 points), which is what a single-corner contour
// needs so the whole boundary is fitted as one smooth run pinned at the corner
// rather than collapsing to a degenerate two-point path.
func arc(poly []Point, a, b int) []Point {
	m := len(poly)
	out := []Point{poly[a]}
	i := a
	for {
		i = (i + 1) % m
		out = append(out, poly[i])
		if i == b {
			break
		}
	}
	return out
}

// detectCorners marks each vertex of the closed polygon as a corner or not.
//
// For each vertex it estimates the tangent just before and just after the
// vertex over a short arc-length window, then measures the turning angle
// between them. A vertex is a corner when that angle exceeds a threshold
// derived from alpha: cornerThreshold = alpha * 1.2 radians (≈68.75° at the
// default alpha of 1.0). Lower alpha ⇒ lower threshold ⇒ more corners; higher
// alpha ⇒ fewer.
//
// The window smooths over the single-pixel staircase of a digitised curve, so a
// disk reads as smooth while a true right-angle corner still exceeds the
// threshold at the default alpha.
func detectCorners(poly []Point, alpha float64) []bool {
	m := len(poly)
	corners := make([]bool, m)
	if m < 3 {
		return corners
	}

	// A small fixed arc-length window smooths over the single-pixel staircase of
	// a digitised curve (so disks read as smooth) while still resolving genuine
	// multi-pixel angular features (so staircases and rectangles keep corners).
	const window = 3.0

	// cornerThreshold maps alpha to the turning angle above which a vertex is a
	// corner. At the default alpha of 1.0 the threshold is ~69°, so true
	// right-angle (90°) corners are kept; as alpha rises toward its clamp the
	// threshold passes 90°, smoothing even right angles. Lower alpha lowers the
	// threshold, promoting gentler bends to corners.
	threshold := alpha * 1.2

	for i := 0; i < m; i++ {
		back := walkBack(poly, i, window)
		fwd := walkForward(poly, i, window)
		tmx, tmy := normalize(poly[i].X-back.X, poly[i].Y-back.Y)
		tpx, tpy := normalize(fwd.X-poly[i].X, fwd.Y-poly[i].Y)
		if (tmx == 0 && tmy == 0) || (tpx == 0 && tpy == 0) {
			continue
		}
		cross := tmx*tpy - tmy*tpx
		dot := tmx*tpx + tmy*tpy
		turn := math.Abs(math.Atan2(cross, dot))
		if turn > threshold {
			corners[i] = true
		}
	}
	return corners
}

// walkBack returns the point reached by walking backward around the closed
// polygon from vertex i until at least dist arc-length has been covered.
func walkBack(poly []Point, i int, dist float64) Point {
	m := len(poly)
	acc := 0.0
	cur := i
	for steps := 0; steps < m; steps++ {
		prev := (cur - 1 + m) % m
		acc += hypot(poly[cur].X-poly[prev].X, poly[cur].Y-poly[prev].Y)
		cur = prev
		if acc >= dist {
			break
		}
	}
	return poly[cur]
}

// walkForward is the forward counterpart of walkBack.
func walkForward(poly []Point, i int, dist float64) Point {
	m := len(poly)
	acc := 0.0
	cur := i
	for steps := 0; steps < m; steps++ {
		next := (cur + 1) % m
		acc += hypot(poly[next].X-poly[cur].X, poly[next].Y-poly[cur].Y)
		cur = next
		if acc >= dist {
			break
		}
	}
	return poly[cur]
}

func hypot(dx, dy float64) float64 { return math.Hypot(dx, dy) }

// smoothPasses is the number of [1,2,1]/4 averaging passes applied to smooth
// runs before curve fitting, to suppress single-pixel staircase aliasing.
const smoothPasses = 3

// smoothClosed applies passes of periodic [1,2,1]/4 averaging to a closed
// polygon, moving every vertex toward the average of its neighbours.
func smoothClosed(poly []Point, passes int) []Point {
	m := len(poly)
	if m < 3 {
		return poly
	}
	cur := make([]Point, m)
	copy(cur, poly)
	next := make([]Point, m)
	for p := 0; p < passes; p++ {
		for i := 0; i < m; i++ {
			a := cur[(i-1+m)%m]
			b := cur[i]
			c := cur[(i+1)%m]
			next[i] = Point{X: 0.25*a.X + 0.5*b.X + 0.25*c.X, Y: 0.25*a.Y + 0.5*b.Y + 0.25*c.Y}
		}
		cur, next = next, cur
	}
	return cur
}

// smoothOpen applies passes of [1,2,1]/4 averaging to an open polyline, holding
// the two endpoints fixed so corner joins stay exact.
func smoothOpen(poly []Point, passes int) []Point {
	m := len(poly)
	if m < 3 {
		return poly
	}
	cur := make([]Point, m)
	copy(cur, poly)
	next := make([]Point, m)
	for p := 0; p < passes; p++ {
		next[0] = cur[0]
		next[m-1] = cur[m-1]
		for i := 1; i < m-1; i++ {
			a := cur[i-1]
			b := cur[i]
			c := cur[i+1]
			next[i] = Point{X: 0.25*a.X + 0.5*b.X + 0.25*c.X, Y: 0.25*a.Y + 0.5*b.Y + 0.25*c.Y}
		}
		cur, next = next, cur
	}
	return cur
}

// normalize returns the unit vector of (x, y), or (0, 0) if the input is zero.
func normalize(x, y float64) (float64, float64) {
	h := math.Hypot(x, y)
	if h == 0 {
		return 0, 0
	}
	return x / h, y / h
}
