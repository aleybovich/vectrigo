// Package regiontrace turns a per-pixel region label map into per-region
// boundary loops that TILE THE PLANE EXACTLY: the boundary between any two
// adjacent regions is represented by identical vertex geometry in both regions,
// so filled regions meet with no gaps and no overlap — and need no dilation.
//
// This is the shared-boundary alternative to tracing each region's mask
// independently (as a per-region bitrace trace would). Two
// independently traced neighbours may disagree on a jagged/diagonal shared edge
// by ~1px, leaving real geometric gaps through which the background bleeds as
// seam artifacts. Here the label map is traced as ONE planar subdivision: every
// undirected boundary unit-edge is emitted once by each of the two regions that
// share it (in opposite directions), so the two regions reference the SAME
// corners and the SAME (optionally smoothed) positions.
//
// # Algorithm
//
//  1. Directed boundary edges. Treat pixel (x,y) as the unit cell
//     [x,x+1]×[y,y+1] on an integer corner grid. For each opaque cell (label
//     r>=0) and each of its 4 sides whose neighbour cell is not r (out of
//     bounds, a different label, or transparent), emit a directed unit edge with
//     a fixed winding (interior on the right, i.e. clockwise in image y-down
//     coords): top (x,y)->(x+1,y); right (x+1,y)->(x+1,y+1); bottom
//     (x+1,y+1)->(x,y+1); left (x,y+1)->(x,y).
//  2. Link edges into closed loops per region by following end corner to the
//     next edge's start corner. At a corner with more than one outgoing edge (a
//     diagonal pinch where the region self-touches) the next edge is chosen by
//     the tightest clockwise turn from the incoming direction; see loopsFor.
//  3. Global shared smoothing. A single corner->Point map (keyed by integer grid
//     corner) is Laplacian-smoothed over the undirected crack graph, pinning
//     junction corners (crack degree != 2) and image-border corners. Because the
//     map is shared, every region using a corner gets the identical smoothed
//     point, so shared edges stay shared.
//  4. Global shared simplification (Options.Simplify > 0). Each maximal chain of
//     unpinned degree-2 corners between pinned anchors is Douglas-Peucker
//     reduced against the smoothed positions, producing ONE global set of
//     dropped corners. Both regions flanking a chain traverse the same corners,
//     so both skip exactly the same dropped ones and the tiling stays exact.
//  5. Emit each region's loops through the smoothed map, skipping dropped
//     corners, collapsing collinear runs and dropping degenerate loops.
//
// Trace is deterministic: identical (labels, w, h, numRegions, opt) always yield
// identical output. Maps are used only for adjacency lookups (never iterated in
// a way that affects numeric results); updates are computed into a buffer and
// applied in a fixed corner order.
package regiontrace

import (
	"math"
	"sort"
)

// Point is a 2-D vertex on the pixel-corner grid, in working image coordinates.
type Point struct{ X, Y float64 }

// Loop is a closed ring of vertices (the final vertex connects back to the
// first; the first vertex is not repeated at the end).
type Loop []Point

// Region holds one region's boundary loops: its outer boundary plus any hole
// loops, all in the winding convention described in the package docs (outer
// loops wind so their signed area is positive, holes negative).
type Region struct {
	ID    int
	Loops []Loop
}

// Options configures Trace.
type Options struct {
	// Smooth is the number of Laplacian smoothing iterations applied to the
	// shared corner graph. 0 leaves the raw pixel staircase. Junction corners
	// (crack-graph degree != 2) and image-border corners are never moved.
	Smooth int

	// Simplify is the maximum perpendicular deviation, in pixels, allowed when
	// straightening boundary chains: runs of unpinned degree-2 corners between
	// pinned anchors are Douglas-Peucker reduced, dropping every corner whose
	// removal keeps the chain within Simplify of its original (smoothed)
	// polyline. 0 (the default) disables simplification and preserves the
	// historical output exactly.
	//
	// This is what keeps straight edges cheap: smoothing relaxes a pixel
	// staircase to NEARLY collinear corners, which exact collinear collapse
	// cannot remove, so without simplification every boundary pixel of a
	// straight edge survives as an output vertex. The drop set is computed once
	// on the shared corner graph, so the two regions flanking any chain skip
	// exactly the same corners and the tiling stays gapless. Pinned corners
	// (junctions, image border, tiny regions) are never dropped.
	Simplify float64
}

// edge is a directed unit boundary edge from corner (sx,sy) to (ex,ey).
type edge struct {
	sx, sy, ex, ey int
}

// tinyRegionPinEdges is the boundary-edge count at or below which a region's
// corners are pinned during smoothing. Laplacian smoothing shrinks a closed
// loop toward its centroid, and the smaller the loop the more of it is lost: a
// single-pixel region (4 edges) collapses to an eighth of its size after three
// iterations, turning eye highlights and glyph fragments into near-invisible
// specks. 12 edges corresponds to roughly a 3×3-pixel region — big enough that
// shrinkage stops being destructive. Pinning is per-corner and the corner map
// is shared, so a neighbour containing a pinned tiny region keeps the identical
// (unsmoothed) hole geometry and the tiling stays exact.
const tinyRegionPinEdges = 12

// Trace turns a per-pixel label map into per-region boundary loops that tile
// the plane exactly. labels has length w*h, row-major (index = y*w + x); each
// entry is a region id in [0,numRegions) or a negative sentinel for a
// none/transparent pixel. It returns one Region per id in [0,numRegions) that
// has at least one pixel, in ascending id order. Regions with no pixels are
// omitted. See the package documentation for the algorithm and guarantees.
func Trace(labels []int, w, h, numRegions int, opt Options) []Region {
	if w <= 0 || h <= 0 || numRegions <= 0 || len(labels) != w*h {
		return nil
	}

	regionEdges := boundaryEdges(labels, w, h, numRegions)

	// Build the shared crack graph once; smoothing and simplification both run
	// on it, so every region reads identical corner positions and an identical
	// drop set — the invariants that keep the tiling exact.
	g := buildCornerGraph(regionEdges, w, h)
	pos := g.smoothed(opt.Smooth)
	drop := g.dropSet(pos, opt.Simplify)

	posMap := make(map[int]Point, len(g.xs))
	var dropMap map[int]bool
	for i := range g.xs {
		posMap[cornerIdx(g.xs[i], g.ys[i], w)] = pos[i]
	}
	if drop != nil {
		dropMap = make(map[int]bool)
		for i, d := range drop {
			if d {
				dropMap[cornerIdx(g.xs[i], g.ys[i], w)] = true
			}
		}
	}

	out := make([]Region, 0, numRegions)
	for r := 0; r < numRegions; r++ {
		edges := regionEdges[r]
		if len(edges) == 0 {
			continue
		}
		loops := loopsFor(edges, w, posMap, dropMap)
		if len(loops) == 0 {
			continue
		}
		out = append(out, Region{ID: r, Loops: loops})
	}
	return out
}

// cornerIdx maps an integer grid corner (x,y) to a dense index. Corners span
// x in [0,w], y in [0,h].
func cornerIdx(x, y, w int) int { return y*(w+1) + x }

// boundaryEdges returns, per region id, the directed unit boundary edges of
// that region using the fixed interior-on-the-right winding. A shared boundary
// between regions A and B is emitted by A and by B as the same undirected
// segment in opposite directions, which is the algebraic basis for gapless
// tiling. Edges are generated in a fixed order (row-major pixel scan, sides in
// top,right,bottom,left order) so the whole tracer is deterministic.
func boundaryEdges(labels []int, w, h, numRegions int) [][]edge {
	regionEdges := make([][]edge, numRegions)
	label := func(x, y int) int {
		if x < 0 || y < 0 || x >= w || y >= h {
			return -1
		}
		return labels[y*w+x]
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r := labels[y*w+x]
			if r < 0 || r >= numRegions {
				continue
			}
			if label(x, y-1) != r { // top
				regionEdges[r] = append(regionEdges[r], edge{x, y, x + 1, y})
			}
			if label(x+1, y) != r { // right
				regionEdges[r] = append(regionEdges[r], edge{x + 1, y, x + 1, y + 1})
			}
			if label(x, y+1) != r { // bottom
				regionEdges[r] = append(regionEdges[r], edge{x + 1, y + 1, x, y + 1})
			}
			if label(x-1, y) != r { // left
				regionEdges[r] = append(regionEdges[r], edge{x, y + 1, x, y})
			}
		}
	}
	return regionEdges
}

// cornerGraph is the global undirected crack graph over boundary corners:
// dense corner ids with grid coordinates, sorted adjacency (neighbours =
// corners reachable via one boundary unit-edge) and the pinned set. It is
// built once per Trace and shared by smoothing and simplification, which is
// what guarantees the two regions flanking any boundary read identical
// positions and drop decisions.
type cornerGraph struct {
	xs, ys []int   // dense id -> integer grid corner
	adj    [][]int // dense id -> sorted neighbour dense ids
	pinned []bool  // junction (degree != 2), image-border, or tiny-region corner
}

// buildCornerGraph interns every boundary corner to a dense id, builds the
// undirected adjacency (sorted for determinism), and computes the pinned set:
// corners whose crack degree != 2 (junctions), corners on the image border,
// and every corner of a tiny region (see tinyRegionPinEdges), which Laplacian
// shrinkage would otherwise collapse to a speck.
func buildCornerGraph(regionEdges [][]edge, w, h int) *cornerGraph {
	dense := make(map[int]int)
	g := &cornerGraph{}
	var tiny []bool
	getDense := func(x, y int) int {
		ci := cornerIdx(x, y, w)
		if d, ok := dense[ci]; ok {
			return d
		}
		d := len(g.xs)
		dense[ci] = d
		g.xs = append(g.xs, x)
		g.ys = append(g.ys, y)
		g.adj = append(g.adj, nil)
		tiny = append(tiny, false)
		return d
	}
	addNbr := func(a, b int) {
		for _, n := range g.adj[a] {
			if n == b {
				return
			}
		}
		g.adj[a] = append(g.adj[a], b)
	}
	for _, edges := range regionEdges {
		isTiny := len(edges) > 0 && len(edges) <= tinyRegionPinEdges
		for _, e := range edges {
			a := getDense(e.sx, e.sy)
			b := getDense(e.ex, e.ey)
			addNbr(a, b)
			addNbr(b, a)
			if isTiny {
				tiny[a] = true
				tiny[b] = true
			}
		}
	}

	n := len(g.xs)
	g.pinned = make([]bool, n)
	for i := 0; i < n; i++ {
		// Fixed neighbour order so smoothing sums and chain walks are
		// bit-for-bit deterministic.
		sort.Ints(g.adj[i])
		if len(g.adj[i]) != 2 || tiny[i] || g.xs[i] == 0 || g.xs[i] == w || g.ys[i] == 0 || g.ys[i] == h {
			g.pinned[i] = true
		}
	}
	return g
}

// smoothed returns the corner positions after iters Laplacian iterations over
// the crack graph, indexed by dense id. Pinned corners never move; iters <= 0
// returns the raw integer grid positions.
func (g *cornerGraph) smoothed(iters int) []Point {
	n := len(g.xs)
	pos := make([]Point, n)
	for i := 0; i < n; i++ {
		pos[i] = Point{X: float64(g.xs[i]), Y: float64(g.ys[i])}
	}
	if iters <= 0 {
		return pos
	}
	buf := make([]Point, n)
	for it := 0; it < iters; it++ {
		copy(buf, pos) // carry pinned corners through unchanged
		for i := 0; i < n; i++ {
			if g.pinned[i] {
				continue
			}
			var sx, sy float64
			for _, nb := range g.adj[i] {
				sx += pos[nb].X
				sy += pos[nb].Y
			}
			c := float64(len(g.adj[i]))
			ax, ay := sx/c, sy/c
			buf[i] = Point{
				X: pos[i].X + 0.5*(ax-pos[i].X),
				Y: pos[i].Y + 0.5*(ay-pos[i].Y),
			}
		}
		pos, buf = buf, pos
	}
	return pos
}

// dropSet returns, indexed by dense id, whether each corner is dropped by
// chain simplification at tolerance eps against the positions pos. It returns
// nil (nothing dropped) when eps <= 0.
//
// Every maximal chain of unpinned degree-2 corners is walked exactly once —
// anchored chains from each pinned corner to the next pinned corner, then any
// remaining pure cycles (a boundary that meets no junction anywhere, e.g. a
// region strictly inside one neighbour) — and Douglas-Peucker marks which
// interior corners survive. Chain endpoints and pinned corners are never
// dropped. Each unpinned corner belongs to exactly one chain, so the drop
// decision is globally unique; both regions flanking a chain skip the same
// corners and the tiling stays exact. A region thinner than eps everywhere can
// legitimately simplify away entirely — its two flanking chains collapse onto
// the same anchors, its ring degenerates (< 3 vertices), and its neighbours'
// shared geometry closes over it with no gap.
//
// Determinism: anchors and cycle starts are scanned in ascending dense id,
// adjacency is sorted, and ties in the farthest-point scans resolve to the
// lowest index.
func (g *cornerGraph) dropSet(pos []Point, eps float64) []bool {
	if eps <= 0 {
		return nil
	}
	n := len(g.xs)
	drop := make([]bool, n)
	visited := make([]bool, n) // unpinned corners already assigned to a chain

	// Anchored chains: from each pinned corner, follow each unpinned neighbour
	// to the next pinned corner.
	for p := 0; p < n; p++ {
		if !g.pinned[p] {
			continue
		}
		for _, q := range g.adj[p] {
			if g.pinned[q] || visited[q] {
				continue
			}
			chain := []int{p, q}
			visited[q] = true
			prev, cur := p, q
			for !g.pinned[cur] {
				nxt := g.adj[cur][0]
				if nxt == prev {
					nxt = g.adj[cur][1]
				}
				chain = append(chain, nxt)
				if !g.pinned[nxt] {
					visited[nxt] = true
				}
				prev, cur = cur, nxt
			}
			dpMark(pos, chain, drop, eps)
		}
	}

	// Pure cycles: split at the start corner and its farthest cycle corner so
	// Douglas-Peucker has two non-degenerate anchor pairs.
	for s := 0; s < n; s++ {
		if g.pinned[s] || visited[s] {
			continue
		}
		cycle := []int{s}
		visited[s] = true
		prev, cur := s, g.adj[s][0]
		for cur != s {
			cycle = append(cycle, cur)
			visited[cur] = true
			nxt := g.adj[cur][0]
			if nxt == prev {
				nxt = g.adj[cur][1]
			}
			prev, cur = cur, nxt
		}
		far, fd := 0, -1.0
		for i, id := range cycle {
			dx := pos[id].X - pos[s].X
			dy := pos[id].Y - pos[s].Y
			if d := dx*dx + dy*dy; d > fd {
				fd, far = d, i
			}
		}
		if far == 0 {
			continue
		}
		dpMark(pos, cycle[:far+1], drop, eps)
		back := append([]int{}, cycle[far:]...)
		back = append(back, s)
		dpMark(pos, back, drop, eps)
	}
	return drop
}

// dpMark runs Douglas-Peucker over one chain of dense corner ids and marks the
// corners it eliminates in drop. The chain's first and last corners are always
// kept. A corner is kept iff somewhere in the recursion its perpendicular
// deviation from the current anchor segment exceeds eps.
func dpMark(pos []Point, chain []int, drop []bool, eps float64) {
	if len(chain) < 3 {
		return
	}
	keep := make([]bool, len(chain))
	keep[0], keep[len(chain)-1] = true, true
	type span struct{ a, b int }
	stack := []span{{0, len(chain) - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if s.b-s.a < 2 {
			continue
		}
		pa, pb := pos[chain[s.a]], pos[chain[s.b]]
		far, fd := -1, eps
		for i := s.a + 1; i < s.b; i++ {
			if d := perpDist(pos[chain[i]], pa, pb); d > fd {
				fd, far = d, i
			}
		}
		if far >= 0 {
			keep[far] = true
			stack = append(stack, span{s.a, far}, span{far, s.b})
		}
	}
	for i, id := range chain {
		if !keep[i] {
			drop[id] = true
		}
	}
}

// perpDist is the perpendicular distance from p to the segment's carrier line
// through a and b, or the distance to a when the segment is degenerate (the
// closed-chain case where both anchors are the same corner).
func perpDist(p, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	cross := dx*(p.Y-a.Y) - dy*(p.X-a.X)
	return math.Abs(cross) / math.Sqrt(l2)
}

// loopsFor links one region's directed edges into closed loops and emits them
// through the shared smoothed-corner map pos, omitting corners in dropped (the
// global simplification set; nil means keep everything). Because dropped is
// global, the two regions sharing a chain omit identical corners, and a region
// whose ring degenerates under simplification (< 3 vertices) is dropped in
// lockstep with its neighbours' matching hole geometry.
//
// Junction turn-rule: at a corner with more than one unused outgoing edge (a
// diagonal pinch where the region self-touches, e.g. two cells meeting only at a
// corner), the next edge is the one making the TIGHTEST CLOCKWISE turn from the
// incoming direction of travel — i.e. the smallest clockwise angle in image
// (y-down) coordinates, where continuing straight is 0. Because the interior is
// kept on the right of every directed edge, always hugging clockwise keeps each
// loop simple and makes touching loops separate instead of crossing (the two
// diagonal cells become two disjoint loops that merely kiss at the corner).
func loopsFor(edges []edge, w int, pos map[int]Point, dropped map[int]bool) []Loop {
	// Per-region adjacency: start corner -> indices of edges starting there.
	start := make(map[int][]int, len(edges))
	for i, e := range edges {
		sc := cornerIdx(e.sx, e.sy, w)
		start[sc] = append(start[sc], i)
	}

	used := make([]bool, len(edges))
	var loops []Loop
	for i := range edges {
		if used[i] {
			continue
		}
		startC := cornerIdx(edges[i].sx, edges[i].sy, w)
		cur := i
		var ring []Point
		for {
			used[cur] = true
			e := edges[cur]
			if sc := cornerIdx(e.sx, e.sy, w); !dropped[sc] {
				ring = append(ring, pos[sc])
			}
			vc := cornerIdx(e.ex, e.ey, w)
			if vc == startC {
				break
			}
			dx, dy := e.ex-e.sx, e.ey-e.sy
			next := -1
			best := math.Inf(1)
			for _, ci := range start[vc] {
				if used[ci] {
					continue
				}
				oe := edges[ci]
				ang := cwAngle(dx, dy, oe.ex-oe.sx, oe.ey-oe.sy)
				if next < 0 || ang < best {
					next = ci
					best = ang
				}
			}
			if next < 0 {
				break // defensive: open path (should not occur for closed boundaries)
			}
			cur = next
		}
		ring = collapseCollinear(ring)
		if len(ring) < 3 || math.Abs(signedArea(ring)) < 1e-9 {
			continue
		}
		loops = append(loops, Loop(ring))
	}
	return loops
}

// cwAngle returns the clockwise angle, in [0,2π), from the incoming direction
// (dx,dy) to the outgoing direction (ox,oy) in image (y-down) coordinates.
// Continuing straight is 0; a sharper clockwise (right) turn is smaller.
func cwAngle(dx, dy, ox, oy int) float64 {
	cross := float64(dx*oy - dy*ox)
	dot := float64(dx*ox + dy*oy)
	a := math.Atan2(cross, dot)
	if a < 0 {
		a += 2 * math.Pi
	}
	return a
}

// collapseCollinear removes vertices that lie on the straight segment between
// their two ring neighbours, reducing a straight run to its endpoints. It is
// geometrically exact (the polygon is unchanged). Comparison uses the original
// neighbours, which correctly drops every interior point of a straight run in a
// single pass.
func collapseCollinear(pts []Point) []Point {
	n := len(pts)
	if n < 3 {
		return pts
	}
	out := pts[:0:0]
	for i := 0; i < n; i++ {
		prev := pts[(i-1+n)%n]
		cur := pts[i]
		next := pts[(i+1)%n]
		cross := (cur.X-prev.X)*(next.Y-prev.Y) - (cur.Y-prev.Y)*(next.X-prev.X)
		if math.Abs(cross) < 1e-9 {
			continue
		}
		out = append(out, cur)
	}
	return out
}

// signedArea returns the shoelace signed area of the closed ring (positive for
// the outer-boundary winding, negative for holes).
func signedArea(pts []Point) float64 {
	n := len(pts)
	if n < 3 {
		return 0
	}
	sum := 0.0
	for i := 0; i < n; i++ {
		j := i + 1
		if j == n {
			j = 0
		}
		sum += pts[i].X*pts[j].Y - pts[j].X*pts[i].Y
	}
	return sum / 2
}
