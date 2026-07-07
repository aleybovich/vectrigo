package segment

// disjoint is a union-find (disjoint-set) forest specialised for
// Felzenszwalb-Huttenlocher segmentation. In addition to the usual
// parent/size bookkeeping it tracks, per component, the internal difference
// (intDiff): the weight of the largest edge on the component's minimum
// spanning tree. Because FH processes edges in non-decreasing weight order,
// the weight of the edge that most recently merged a component is exactly that
// maximum, so intDiff is simply set to the merging edge's weight on each union.
//
// Union is by size (the smaller tree is attached under the larger) with path
// compression in find, giving near-linear total cost. No maps are used, so the
// structure introduces no iteration-order nondeterminism.
type disjoint struct {
	parent  []int32
	size    []int32
	intDiff []float32
}

// newDisjoint returns a forest of n singleton components, each of size 1 and
// internal difference 0.
func newDisjoint(n int) *disjoint {
	d := &disjoint{
		parent:  make([]int32, n),
		size:    make([]int32, n),
		intDiff: make([]float32, n),
	}
	for i := 0; i < n; i++ {
		d.parent[i] = int32(i)
		d.size[i] = 1
	}
	return d
}

// find returns the representative (root) of x's component, compressing the
// path from x to the root so future queries are faster. Path compression is
// order-independent and does not affect labels.
func (d *disjoint) find(x int32) int32 {
	root := x
	for d.parent[root] != root {
		root = d.parent[root]
	}
	// Second pass: point every node on the path directly at the root.
	for d.parent[x] != root {
		next := d.parent[x]
		d.parent[x] = root
		x = next
	}
	return root
}

// union merges the components rooted at ra and rb (which must be distinct
// roots), recording w as the internal difference of the merged component. The
// smaller tree is attached beneath the larger; on a size tie the lower-indexed
// root wins, keeping the merge deterministic. It returns the new root.
func (d *disjoint) union(ra, rb int32, w float32) int32 {
	if d.size[ra] < d.size[rb] || (d.size[ra] == d.size[rb] && rb < ra) {
		ra, rb = rb, ra
	}
	// ra is now the (larger, or lower-indexed on tie) surviving root.
	d.parent[rb] = ra
	d.size[ra] += d.size[rb]
	d.intDiff[ra] = w
	return ra
}
