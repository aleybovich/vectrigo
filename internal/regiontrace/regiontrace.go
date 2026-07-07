// Package regiontrace turns a per-pixel region label map into per-region
// boundary loops that TILE THE PLANE EXACTLY: the boundary between any two
// adjacent regions is represented by identical vertex geometry in both regions,
// so filled regions meet with no gaps and no overlap — and need no dilation.
//
// This is the shared-boundary alternative to tracing each region's mask
// independently (as internal/pipeline.TraceRegions does via bitrace). Two
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
//  4. Emit each region's loops through the smoothed map, collapsing collinear
//     runs and dropping degenerate loops.
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
}

// edge is a directed unit boundary edge from corner (sx,sy) to (ex,ey).
type edge struct {
	sx, sy, ex, ey int
}

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

	// Build the shared corner->smoothed-Point map over the global crack graph.
	pos := smoothedCorners(regionEdges, w, h, opt.Smooth)

	out := make([]Region, 0, numRegions)
	for r := 0; r < numRegions; r++ {
		edges := regionEdges[r]
		if len(edges) == 0 {
			continue
		}
		loops := loopsFor(edges, w, pos)
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

// smoothedCorners builds the global shared corner map: keyed by integer grid
// corner index, valued by the (optionally smoothed) float Point. Every corner
// that appears on any region boundary is present. Laplacian smoothing runs over
// the undirected crack graph (neighbours = corners reachable via one boundary
// unit-edge), pinning corners whose crack degree != 2 (junctions) and corners
// on the image border. Because the same map is returned for all regions, any
// two regions sharing a corner read the identical Point.
func smoothedCorners(regionEdges [][]edge, w, h, iters int) map[int]Point {
	// Intern boundary corners to dense ids and build undirected adjacency.
	dense := make(map[int]int)
	var xs, ys []int
	var adj [][]int
	getDense := func(x, y int) int {
		ci := cornerIdx(x, y, w)
		if d, ok := dense[ci]; ok {
			return d
		}
		d := len(xs)
		dense[ci] = d
		xs = append(xs, x)
		ys = append(ys, y)
		adj = append(adj, nil)
		return d
	}
	addNbr := func(a, b int) {
		for _, n := range adj[a] {
			if n == b {
				return
			}
		}
		adj[a] = append(adj[a], b)
	}
	for _, edges := range regionEdges {
		for _, e := range edges {
			a := getDense(e.sx, e.sy)
			b := getDense(e.ex, e.ey)
			addNbr(a, b)
			addNbr(b, a)
		}
	}

	n := len(xs)
	pos := make([]Point, n)
	pinned := make([]bool, n)
	for i := 0; i < n; i++ {
		pos[i] = Point{X: float64(xs[i]), Y: float64(ys[i])}
		if len(adj[i]) != 2 || xs[i] == 0 || xs[i] == w || ys[i] == 0 || ys[i] == h {
			pinned[i] = true
		}
	}

	if iters > 0 {
		// Fixed neighbour order so the averaging sum is bit-for-bit stable.
		for i := range adj {
			sort.Ints(adj[i])
		}
		buf := make([]Point, n)
		for it := 0; it < iters; it++ {
			copy(buf, pos) // carry pinned corners through unchanged
			for i := 0; i < n; i++ {
				if pinned[i] {
					continue
				}
				var sx, sy float64
				for _, nb := range adj[i] {
					sx += pos[nb].X
					sy += pos[nb].Y
				}
				c := float64(len(adj[i]))
				ax, ay := sx/c, sy/c
				buf[i] = Point{
					X: pos[i].X + 0.5*(ax-pos[i].X),
					Y: pos[i].Y + 0.5*(ay-pos[i].Y),
				}
			}
			pos, buf = buf, pos
		}
	}

	outMap := make(map[int]Point, n)
	for i := 0; i < n; i++ {
		outMap[cornerIdx(xs[i], ys[i], w)] = pos[i]
	}
	return outMap
}

// loopsFor links one region's directed edges into closed loops and emits them
// through the shared smoothed-corner map pos.
//
// Junction turn-rule: at a corner with more than one unused outgoing edge (a
// diagonal pinch where the region self-touches, e.g. two cells meeting only at a
// corner), the next edge is the one making the TIGHTEST CLOCKWISE turn from the
// incoming direction of travel — i.e. the smallest clockwise angle in image
// (y-down) coordinates, where continuing straight is 0. Because the interior is
// kept on the right of every directed edge, always hugging clockwise keeps each
// loop simple and makes touching loops separate instead of crossing (the two
// diagonal cells become two disjoint loops that merely kiss at the corner).
func loopsFor(edges []edge, w int, pos map[int]Point) []Loop {
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
			ring = append(ring, pos[cornerIdx(e.sx, e.sy, w)])
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
