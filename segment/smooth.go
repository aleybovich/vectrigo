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
//   - Shrinking away: small regions and THIN regions (mostly-boundary strokes,
//     see the freeze rules below) are FROZEN so they are preserved bit-for-bit,
//     and a region large enough to have any interior pixel can never vanish
//     because interior pixels never flip.
//   - Splitting: after smoothing the labels are re-densified by 8-connected
//     component (relabelConnected), so a split region simply becomes two valid,
//     each-connected regions rather than one disconnected id.
//
// Guarantee summary. After smoothing + relabelConnected: every opaque pixel has
// exactly one dense label in [0,NumRegions); no gaps or overlaps; every region
// is a single 8-connected component; and every region present before smoothing
// that is frozen (small by area, or thin) is present afterwards with its exact
// pixels. Determinism: passes are double-buffered pure functions of the
// previous buffer with a lowest-label-id tie-break, and relabelConnected
// numbers components in row-major first-appearance order.

// defaultSmoothProtect is the minimum region-area floor honoured by boundary
// smoothing even when Options.MinSize is unset. Any region whose area is at or
// below max(MinSize, defaultSmoothProtect) is "frozen": its pixels never flip,
// and no neighbouring pixel flips into it, so it survives smoothing bit-for-bit.
// This protects small features — sign lettering only a few pixels across — from
// being dissolved by the mode filter. It is deliberately generous enough to
// cover tiny glyph blobs while staying well below any genuinely large region.
const defaultSmoothProtect = 16

// thinInteriorRatio is the thinness freeze rule: a region is frozen when its
// interior pixel count (pixels whose full 8-neighbourhood shares their label)
// is under 1/thinInteriorRatio of its area. Such a region is essentially all
// boundary — a 1-2px letter stroke, a feature line, a wire — exactly the
// features the mode filter otherwise erodes because every pixel is outvoted by
// the surrounding region. Area alone cannot catch these: a letter is well above
// any sane area floor yet still thin everywhere. Blobby regions are unaffected
// (a disc's interior is well over a quarter of its area at any size the filter
// could plausibly damage).
const thinInteriorRatio = 4

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

	// Freeze small regions (by area) and thin regions (by interior/area ratio).
	// Areas and interiors are taken from the initial labelling; frozen regions
	// are inert (neither lose nor gain pixels), so their geometry never changes
	// and a static freeze set is sufficient and deterministic.
	area := make([]int, numRegions)
	for _, lb := range labels {
		if lb >= 0 {
			area[lb]++
		}
	}
	interior := make([]int, numRegions)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			lb := labels[i]
			if lb < 0 {
				continue
			}
			if labels[i-1] == lb && labels[i+1] == lb &&
				labels[i-w-1] == lb && labels[i-w] == lb && labels[i-w+1] == lb &&
				labels[i+w-1] == lb && labels[i+w] == lb && labels[i+w+1] == lb {
				interior[lb]++
			}
		}
	}
	frozen := make([]bool, numRegions)
	for id, a := range area {
		if a <= protect || interior[id]*thinInteriorRatio < a {
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
