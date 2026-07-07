package bitrace

import (
	"errors"
	"math"
)

var (
	errBadDimensions = errors.New("bitrace: bitmap dimensions must be non-negative")
	errBitsLength    = errors.New("bitrace: len(Bits) must equal W*H")
)

// ivec is an integer lattice point (a corner of the pixel grid).
type ivec struct {
	X, Y int
}

// edge is a directed unit segment between two adjacent lattice points.
type edge struct {
	a, b ivec
}

// extractContours traces the boundaries between "on" and "off" regions of bm
// into closed integer polygons.
//
// It walks the pixel-edge lattice ("crack following"): every unit edge that
// separates an on pixel from an off pixel (or from outside the bitmap) is
// emitted as a directed segment oriented so the on region lies to its left. The
// segments are then chained head-to-tail into closed loops. Collinear runs are
// collapsed, so an axis-aligned rectangle yields a four-vertex polygon.
//
// Each returned polygon is a slice of [Point] with integer coordinates and does
// not repeat its first vertex at the end.
func extractContours(bm Bitmap) [][]Point {
	// Emit boundary edges. For an on pixel at (x, y) the cell corners are
	// (x, y), (x+1, y), (x+1, y+1), (x, y+1). Each side bordering an off cell
	// becomes a directed edge that keeps the on cell on its left (in image
	// coordinates, x right / y down).
	var edges []edge
	for y := 0; y < bm.H; y++ {
		for x := 0; x < bm.W; x++ {
			if !bm.At(x, y) {
				continue
			}
			if !bm.At(x, y-1) { // top border: (x+1,y) -> (x,y)
				edges = append(edges, edge{ivec{x + 1, y}, ivec{x, y}})
			}
			if !bm.At(x, y+1) { // bottom border: (x,y+1) -> (x+1,y+1)
				edges = append(edges, edge{ivec{x, y + 1}, ivec{x + 1, y + 1}})
			}
			if !bm.At(x-1, y) { // left border: (x,y) -> (x,y+1)
				edges = append(edges, edge{ivec{x, y}, ivec{x, y + 1}})
			}
			if !bm.At(x+1, y) { // right border: (x+1,y+1) -> (x+1,y)
				edges = append(edges, edge{ivec{x + 1, y + 1}, ivec{x + 1, y}})
			}
		}
	}
	if len(edges) == 0 {
		return nil
	}

	// Index edges by their start vertex so we can chain them.
	adj := make(map[ivec][]int, len(edges))
	for i, e := range edges {
		adj[e.a] = append(adj[e.a], i)
	}
	used := make([]bool, len(edges))

	var contours [][]Point
	for startIdx := range edges {
		if used[startIdx] {
			continue
		}
		loop := followLoop(edges, adj, used, startIdx)
		if len(loop) >= 3 {
			contours = append(contours, mergeCollinear(loop))
		}
	}
	return contours
}

// followLoop walks a single closed loop starting from edge startIdx, consuming
// edges as it goes, and returns the loop's lattice vertices (without repeating
// the start vertex at the end).
func followLoop(edges []edge, adj map[ivec][]int, used []bool, startIdx int) []Point {
	start := edges[startIdx].a
	cur := startIdx
	verts := []Point{{float64(start.X), float64(start.Y)}}

	for i := 0; i <= len(edges); i++ {
		used[cur] = true
		e := edges[cur]
		if e.b == start {
			break
		}
		verts = append(verts, Point{float64(e.b.X), float64(e.b.Y)})
		din := ivec{e.b.X - e.a.X, e.b.Y - e.a.Y}
		next := chooseNext(edges, adj, used, e.b, din)
		if next < 0 {
			break
		}
		cur = next
	}
	return verts
}

// chooseNext selects the next edge to follow from vertex v, having arrived along
// direction din. When several unused edges leave v (a junction where regions
// touch diagonally), it picks the one that turns most tightly toward the
// interior. This keeps each connected component's boundary a single simple loop
// and splits diagonal touches into separate loops. It returns -1 if no unused
// outgoing edge exists.
func chooseNext(edges []edge, adj map[ivec][]int, used []bool, v, din ivec) int {
	best := -1
	bestTurn := math.Inf(1)
	for _, idx := range adj[v] {
		if used[idx] {
			continue
		}
		e := edges[idx]
		dout := ivec{e.b.X - e.a.X, e.b.Y - e.a.Y}
		cross := float64(din.X*dout.Y - din.Y*dout.X)
		dot := float64(din.X*dout.X + din.Y*dout.Y)
		turn := math.Atan2(cross, dot)
		if turn < bestTurn {
			bestTurn = turn
			best = idx
		}
	}
	return best
}

// mergeCollinear removes vertices that lie on a straight run, i.e. a vertex
// whose two neighbours are collinear with it. The polygon is treated as closed.
func mergeCollinear(poly []Point) []Point {
	n := len(poly)
	if n < 3 {
		return poly
	}
	out := make([]Point, 0, n)
	for i := 0; i < n; i++ {
		prev := poly[(i-1+n)%n]
		cur := poly[i]
		next := poly[(i+1)%n]
		ax, ay := cur.X-prev.X, cur.Y-prev.Y
		bx, by := next.X-cur.X, next.Y-cur.Y
		if ax*by-ay*bx == 0 { // collinear: drop cur
			continue
		}
		out = append(out, cur)
	}
	if len(out) < 3 {
		return poly
	}
	return out
}
