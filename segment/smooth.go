package segment

// Boundary smoothing — technique (A): a label-map mode (majority) filter.
//
// Why (A) over (B) (contour/polygon smoothing): operating on the per-pixel
// region label map keeps the partition trivially valid. Every opaque pixel
// always holds exactly one region id, so smoothing can never open a gap or an
// overlap between neighbouring regions — the way independently smoothed boundary
// polygons (B) would, since two neighbours' smoothed shared edge would diverge
// and leave slivers/double-cover. The only failure modes a mode filter can
// introduce — a region shrinking away, or a region splitting into two blobs —
// are handled explicitly here:
//
//   - Shrinking away: small regions are FROZEN (see freeze floor below) so they
//     are preserved bit-for-bit, and a region large enough to have any interior
//     pixel can never vanish because interior pixels never flip.
//   - Splitting: after smoothing the labels are re-densified by 8-connected
//     component (relabelConnected), so a split region simply becomes two valid,
//     each-connected regions rather than one disconnected id.
//
// Guarantee summary. After smoothing + relabelConnected: every opaque pixel has
// exactly one dense label in [0,NumRegions); no gaps or overlaps; every region
// is a single 8-connected component; and every region present before smoothing
// whose area is <= the freeze floor is present afterwards with its exact pixels.
// Determinism: passes are double-buffered pure functions of the previous buffer
// with a lowest-label-id tie-break, and relabelConnected numbers components in
// row-major first-appearance order.
//
// Documented limit: a region above the freeze floor that is everywhere thin
// (has no interior pixel — e.g. a 1px-wide stroke longer than the floor) can
// still erode under many iterations. Two-pixel-wide strokes are preserved
// naturally (their pixels keep a same-label majority), and the recommended
// iteration count is small (1-5), so this is not a concern in practice.

// defaultSmoothProtect is the minimum region-area floor honoured by boundary
// smoothing even when Options.MinSize is unset. Any region whose area is at or
// below max(MinSize, defaultSmoothProtect) is "frozen": its pixels never flip,
// and no neighbouring pixel flips into it, so it survives smoothing bit-for-bit.
// This protects small features — sign lettering only a few pixels across — from
// being dissolved by the mode filter. It is deliberately generous enough to
// cover tiny glyph blobs while staying well below any genuinely large region.
const defaultSmoothProtect = 16

// smoothBoundaries runs iters passes of a deterministic 8-connected label-map
// mode filter over labels in place, rounding off staircase jaggies on region
// boundaries. labels holds dense ids in [0,numRegions) for opaque pixels and
// TransparentLabel (<0) for transparent pixels; minSize is Options.MinSize
// (reused, together with defaultSmoothProtect, as the small-region freeze
// floor).
//
// The result may contain emptied or split region ids and is NOT guaranteed
// dense — callers must re-densify with relabelConnected afterwards.
func smoothBoundaries(labels []int, w, h, numRegions, minSize, iters int) {
	n := w * h
	if iters <= 0 || n == 0 || numRegions == 0 {
		return
	}

	protect := minSize
	if protect < defaultSmoothProtect {
		protect = defaultSmoothProtect
	}

	// Freeze small regions. Areas are taken from the initial labelling; frozen
	// regions are inert (neither lose nor gain pixels), so their area never
	// changes and a static freeze set is sufficient and deterministic.
	area := make([]int, numRegions)
	for _, lb := range labels {
		if lb >= 0 {
			area[lb]++
		}
	}
	frozen := make([]bool, numRegions)
	for id, a := range area {
		if a <= protect {
			frozen[id] = true
		}
	}

	// Double buffer: each pass reads src and writes dst, so a pass is a pure
	// function of the previous state (order-independent, hence deterministic).
	src := labels
	dst := make([]int, n)

	// counts[label] tallies a pixel's neighbouring labels; touched lists the
	// labels seen so counts can be reset in O(neighbours) rather than
	// O(numRegions) per pixel.
	counts := make([]int, numRegions)
	touched := make([]int, 0, 8)

	for it := 0; it < iters; it++ {
		for i := 0; i < n; i++ {
			a := src[i]
			if a < 0 { // transparent: never changes
				dst[i] = a
				continue
			}
			if frozen[a] { // small region: preserved exactly
				dst[i] = a
				continue
			}

			x := i % w
			y := i / w
			touched = touched[:0]
			for dy := -1; dy <= 1; dy++ {
				ny := y + dy
				if ny < 0 || ny >= h {
					continue
				}
				row := ny * w
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					nx := x + dx
					if nx < 0 || nx >= w {
						continue
					}
					lb := src[row+nx]
					if lb < 0 { // transparent neighbours cast no vote
						continue
					}
					if counts[lb] == 0 {
						touched = append(touched, lb)
					}
					counts[lb]++
				}
			}

			// Pick the dominant non-frozen neighbour label (frozen labels are
			// excluded as flip targets so small regions do not grow either),
			// tie-broken toward the lowest label id for determinism.
			best := -1
			bestCount := 0
			for _, lb := range touched {
				if frozen[lb] {
					continue
				}
				c := counts[lb]
				if c > bestCount || (c == bestCount && (best < 0 || lb < best)) {
					bestCount = c
					best = lb
				}
			}

			// Flip only on a strict majority over the pixel's own label, so
			// straight edges stay put and only genuine jaggies (corners/teeth/
			// notches) move. counts[a] is a's own neighbour tally (>= 1 for any
			// non-frozen region, which is 8-connected with area > 1).
			if best >= 0 && best != a && bestCount > counts[a] {
				dst[i] = best
			} else {
				dst[i] = a
			}

			for _, lb := range touched {
				counts[lb] = 0
			}
		}
		src, dst = dst, src
	}

	// Land the final state back in labels. If iters was even, src already is
	// labels and this is a no-op self-copy.
	if &src[0] != &labels[0] {
		copy(labels, src)
	}
}

// relabelConnected re-densifies a (possibly smoothed) label buffer in place,
// assigning a fresh dense id in [0,NumRegions) to each 8-connected component of
// equal label. This guarantees every output region is a single 8-connected
// component — so a mode filter that split a region yields two valid regions
// rather than one disconnected id — and that ids are dense. Transparent pixels
// (label < 0) keep TransparentLabel. Ids are assigned in row-major order of
// first appearance for determinism. It returns the new region count.
func relabelConnected(labels []int, w, h int) int {
	n := w * h
	out := make([]int, n)
	for i := range out {
		out[i] = TransparentLabel
	}
	next := 0
	stack := make([]int, 0, 64)
	for start := 0; start < n; start++ {
		if labels[start] < 0 || out[start] != TransparentLabel {
			continue // transparent, or already assigned to a component
		}
		id := next
		next++
		cur := labels[start]
		out[start] = id
		stack = stack[:0]
		stack = append(stack, start)
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			px := p % w
			py := p / w
			for dy := -1; dy <= 1; dy++ {
				ny := py + dy
				if ny < 0 || ny >= h {
					continue
				}
				row := ny * w
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					nx := px + dx
					if nx < 0 || nx >= w {
						continue
					}
					q := row + nx
					if out[q] == TransparentLabel && labels[q] == cur {
						out[q] = id
						stack = append(stack, q)
					}
				}
			}
		}
	}
	copy(labels, out)
	return next
}
