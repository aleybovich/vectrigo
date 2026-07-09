// Package regionize turns a per-pixel colour-cluster label map (the k-means
// assignment from internal/quantize) into a per-pixel REGION label map suitable
// for internal/regiontrace: every region is one 4-connected area of a single
// cluster, and speckle-sized regions are absorbed into a neighbour instead of
// being dropped.
//
// It is the bridge that lets the quantization pipeline reuse photo mode's
// gapless shared-boundary tracer. The two pipelines differ in what a "shape"
// is:
//
//   - The mask pipeline (bitrace) traces each cluster's whole bitmap at once,
//     so one SVG path aggregates every same-coloured area across the image, and
//     independently traced neighbours leave sub-pixel seams between paths.
//   - regiontrace instead wants one label per spatially-connected region and
//     traces them all as a single planar subdivision — but the k-means labels
//     identify colours, not regions.
//
// Regionize closes that gap: it splits each cluster into its 4-connected
// components, then runs up to two ABSORPTION passes. Absorbing (not deleting)
// is the gapless analogue of bitrace's TurdSize speckle removal: dropping a
// speck would tear a hole in the tiling, while absorbing recolours the speck
// into a neighbour exactly as paint-over does in the stacked mask pipeline.
// The two passes differ in when they fire:
//
//   - The MinSize pass is unconditional, mirroring bitrace's TurdSize
//     convention: every component with pixel area < MinSize is absorbed;
//     MinSize <= 1 disables the pass.
//   - The denoise pass is conditional on COLOUR: a component with area <
//     DenoiseSize is absorbed only when its best neighbour's palette colour is
//     within DenoiseMaxDist. Quantizing photographic gradients scatters vast
//     numbers of 1-3px specks between ADJACENT (near-identical) clusters —
//     invisible noise that dominates the path count — while the rare
//     high-contrast specks (eye highlights, letter fragments) carry real
//     detail. The colour condition merges the former and keeps the latter.
//
// Both passes pick the target the same way: the adjacent region with the most
// similar palette colour, tie-broken by longest shared border. (Without a
// palette, distances are all zero and the ranking degrades to longest border.)
// Colour-similarity ranking matters for quality even in the unconditional
// pass — absorbing a skin-tone speck into adjacent hair rather than adjacent
// skin is what makes naive speckle merging look blotchy.
//
// Regionize is deterministic: components are numbered in row-major discovery
// order, absorption processes candidates in ascending component order with
// fixed tie-breaks, and the final region ids are compacted in row-major
// first-appearance order. Identical inputs always yield identical output.
package regionize

import (
	"image/color"
	"math"
	"sort"
)

// Options configures Regionize's absorption passes. The zero value disables
// both passes (pure connected-component splitting).
type Options struct {
	// MinSize is the unconditional speckle threshold: components with pixel
	// area < MinSize are absorbed into a neighbour regardless of colour,
	// matching bitrace's TurdSize convention. <= 1 disables the pass.
	MinSize int

	// Palette maps cluster id -> colour. It drives the colour-similarity
	// ranking of absorption targets and the denoise pass's colour condition.
	// A nil palette leaves ranking to border length and disables denoise.
	Palette []color.RGBA

	// DenoiseSize is the conditional threshold: components with pixel area <
	// DenoiseSize are absorbed only if their best neighbour's colour is within
	// DenoiseMaxDist. <= 1 disables the pass.
	DenoiseSize int

	// DenoiseMaxDist is the maximum Euclidean RGB distance to the absorbing
	// neighbour's palette colour for the denoise pass. <= 0 disables the pass.
	DenoiseMaxDist float64
}

// Result is Regionize's output, shaped for internal/regiontrace.Trace and
// internal/assemble.WriteRegions.
type Result struct {
	// Labels is the per-pixel region id map, length w*h row-major: a region id
	// in [0,NumRegions) for every pixel whose input cluster was >= 0, and -1
	// where the input was negative (transparent).
	Labels []int
	// NumRegions is the number of distinct regions in Labels.
	NumRegions int
	// Cluster maps region id -> input cluster id, so callers can colour each
	// region from the cluster palette. A region absorbed into a neighbour takes
	// the neighbour's cluster.
	Cluster []int
	// Areas maps region id -> pixel count (after absorption).
	Areas []int
}

// Regionize splits the cluster label map into 4-connected components and runs
// the configured absorption passes (see the package documentation). clusters
// has length w*h, row-major; negative entries are transparent and belong to no
// region.
func Regionize(clusters []int, w, h int, opt Options) Result {
	n := w * h
	if w <= 0 || h <= 0 || len(clusters) != n {
		return Result{}
	}

	comp, compArea, compCluster := components(clusters, w, h)

	parent := make([]int, len(compArea))
	for i := range parent {
		parent[i] = i
	}
	if opt.MinSize > 1 {
		absorb(comp, compArea, compCluster, parent, w, h, opt.MinSize, math.Inf(1), opt.Palette)
	}
	if opt.DenoiseSize > 1 && opt.DenoiseMaxDist > 0 && opt.Palette != nil {
		absorb(comp, compArea, compCluster, parent, w, h, opt.DenoiseSize,
			opt.DenoiseMaxDist*opt.DenoiseMaxDist, opt.Palette)
	}

	return compact(clusters, comp, compArea, compCluster, parent)
}

// components labels the 4-connected components of the cluster map in row-major
// discovery order. It returns the per-pixel component id (-1 for transparent
// pixels) plus each component's pixel area and cluster id.
func components(clusters []int, w, h int) (comp, compArea, compCluster []int) {
	n := w * h
	comp = make([]int, n)
	for i := range comp {
		comp[i] = -1
	}

	var stack []int
	for i := 0; i < n; i++ {
		if clusters[i] < 0 || comp[i] >= 0 {
			continue
		}
		id := len(compArea)
		cl := clusters[i]
		comp[i] = id
		stack = append(stack[:0], i)
		area := 0
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			area++
			x := p % w
			if x > 0 && comp[p-1] < 0 && clusters[p-1] == cl {
				comp[p-1] = id
				stack = append(stack, p-1)
			}
			if x+1 < w && comp[p+1] < 0 && clusters[p+1] == cl {
				comp[p+1] = id
				stack = append(stack, p+1)
			}
			if p >= w && comp[p-w] < 0 && clusters[p-w] == cl {
				comp[p-w] = id
				stack = append(stack, p-w)
			}
			if p+w < n && comp[p+w] < 0 && clusters[p+w] == cl {
				comp[p+w] = id
				stack = append(stack, p+w)
			}
		}
		compArea = append(compArea, area)
		compCluster = append(compCluster, cl)
	}
	return comp, compArea, compCluster
}

// border is one directed "tiny component -> neighbouring component" adjacency:
// the length (in unit pixel edges) of their shared border and the squared RGB
// distance between their clusters' palette colours.
type border struct {
	tiny, next, length int
	distSq             float64
}

// paletteDistSq returns the squared Euclidean RGB distance between clusters a
// and b, or 0 when the palette does not cover them (degrading the target
// ranking to border length alone).
func paletteDistSq(palette []color.RGBA, a, b int) float64 {
	if a < 0 || b < 0 || a >= len(palette) || b >= len(palette) {
		return 0
	}
	dr := float64(palette[a].R) - float64(palette[b].R)
	dg := float64(palette[a].G) - float64(palette[b].G)
	db := float64(palette[a].B) - float64(palette[b].B)
	return dr*dr + dg*dg + db*db
}

// absorb repeatedly merges component groups whose total area is below size
// into their best neighbouring group — nearest palette colour first, longest
// shared border as the tie-break — provided that neighbour's colour is within
// maxDistSq (pass math.Inf(1) for the unconditional pass). It mutates parent
// (a union-find forest over component ids; the absorbing root survives, so it
// keeps its own cluster) and compArea (per-root accumulated area). It runs in
// rounds — a merged group may still be tiny, or two tiny groups may merge and
// cross the threshold — until no eligible merge remains. Groups with no opaque
// neighbour (isolated islands), and groups whose closest neighbour is beyond
// maxDistSq, are kept as-is.
func absorb(comp, compArea, compCluster, parent []int, w, h, size int, maxDistSq float64, palette []color.RGBA) {
	find := func(c int) int {
		for parent[c] != c {
			parent[c] = parent[parent[c]]
			c = parent[c]
		}
		return c
	}

	for {
		// Tally shared-border lengths between adjacent roots where at least one
		// side is tiny, from a full snapshot scan of the pixel grid.
		lengths := make(map[[2]int]int)
		tally := func(a, b int) {
			ra, rb := find(a), find(b)
			if ra == rb {
				return
			}
			if compArea[ra] < size {
				lengths[[2]int{ra, rb}]++
			}
			if compArea[rb] < size {
				lengths[[2]int{rb, ra}]++
			}
		}
		for y := 0; y < h; y++ {
			row := y * w
			for x := 0; x < w; x++ {
				p := row + x
				if comp[p] < 0 {
					continue
				}
				if x+1 < w && comp[p+1] >= 0 {
					tally(comp[p], comp[p+1])
				}
				if y+1 < h && comp[p+w] >= 0 {
					tally(comp[p], comp[p+w])
				}
			}
		}
		if len(lengths) == 0 {
			return
		}

		// Deterministic merge order: sort the snapshot's adjacencies so every
		// tiny root's candidates are contiguous and its best (nearest colour,
		// then longest border, then lowest neighbour id) comes first.
		borders := make([]border, 0, len(lengths))
		for k, l := range lengths {
			borders = append(borders, border{
				tiny:   k[0],
				next:   k[1],
				length: l,
				distSq: paletteDistSq(palette, compCluster[k[0]], compCluster[k[1]]),
			})
		}
		sort.Slice(borders, func(i, j int) bool {
			if borders[i].tiny != borders[j].tiny {
				return borders[i].tiny < borders[j].tiny
			}
			if borders[i].distSq != borders[j].distSq {
				return borders[i].distSq < borders[j].distSq
			}
			if borders[i].length != borders[j].length {
				return borders[i].length > borders[j].length
			}
			return borders[i].next < borders[j].next
		})

		merged := false
		for i := 0; i < len(borders); {
			t := borders[i].tiny
			// The first entry for t is its best candidate from the snapshot.
			best := borders[i].next
			bestDistSq := borders[i].distSq
			for i < len(borders) && borders[i].tiny == t {
				i++
			}
			// The candidates are colour-ranked, so if the best is out of colour
			// range every other neighbour is too: the speck is kept (it carries
			// contrast the denoise pass must not erase).
			if bestDistSq > maxDistSq {
				continue
			}
			// Re-resolve through this round's earlier merges: skip roots already
			// absorbed (or grown past the threshold), and never self-merge.
			rt := find(t)
			if rt != t || compArea[rt] >= size {
				continue
			}
			rb := find(best)
			if rb == rt {
				continue
			}
			parent[rt] = rb
			compArea[rb] += compArea[rt]
			merged = true
		}
		if !merged {
			return
		}
	}
}

// compact renumbers the surviving union-find roots into dense region ids in
// row-major first-appearance order and materializes the Result.
func compact(clusters, comp, compArea, compCluster, parent []int) Result {
	find := func(c int) int {
		for parent[c] != c {
			parent[c] = parent[parent[c]]
			c = parent[c]
		}
		return c
	}

	labels := make([]int, len(clusters))
	remap := make([]int, len(compArea))
	for i := range remap {
		remap[i] = -1
	}
	num := 0
	for i := range clusters {
		if clusters[i] < 0 {
			labels[i] = -1
			continue
		}
		r := find(comp[i])
		if remap[r] < 0 {
			remap[r] = num
			num++
		}
		labels[i] = remap[r]
	}

	cluster := make([]int, num)
	areas := make([]int, num)
	for r, m := range remap {
		if m >= 0 {
			cluster[m] = compCluster[r]
			areas[m] = compArea[r]
		}
	}
	return Result{Labels: labels, NumRegions: num, Cluster: cluster, Areas: areas}
}
