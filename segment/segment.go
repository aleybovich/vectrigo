// Package segment implements graph-based image segmentation as an alternative
// front-end to colour quantization for the vectrigo raster→SVG pipeline.
//
// Where quantization (internal/quantize) clusters pixels globally by colour —
// so one palette colour scatters across the whole image and photographic
// content posterizes into blobby, speckled planes — segmentation partitions
// the image into many small, spatially connected regions (a cheek, a shadow, a
// brick), each of which can then be given its own mean colour and traced. This
// preserves local detail: a face becomes hundreds of coherent tiles rather
// than a handful of quantized colour islands.
//
// The algorithm is Felzenszwalb & Huttenlocher's efficient graph-based
// segmentation (IJCV 2004). Pixels are nodes; 8-connected neighbours are joined
// by edges weighted by Euclidean RGB distance. Edges are processed in
// non-decreasing weight order and greedily merged under an adaptive predicate
// that compares each boundary edge against the internal contrast already
// present in the two components it would join. The result is a per-pixel region
// labelling.
//
// # Determinism
//
// Segment is fully deterministic: identical (img, Options) always yields an
// identical Labels slice. Edges are sorted by a total order (weight, then the
// ordered endpoint pair) so equal-weight edges always process in the same
// sequence; union-by-size ties break on the lower root index; and dense region
// ids are assigned in row-major order of first appearance. No maps drive any
// order-dependent step, and there is no use of math/rand or the wall clock.
// Optional boundary smoothing is likewise deterministic: each mode-filter pass
// is a pure function of the previous label buffer (double-buffered, fixed
// row-major traversal, lowest-label-id tie-break) and the final
// connected-component relabel assigns ids in row-major first-appearance order.
//
// # Tuning region count
//
// Three knobs in Options control the partition:
//
//   - K (scale): the dominant control. The merge predicate allows a boundary
//     edge of weight w to merge two components only while w stays within
//     K/|component| of their internal contrast, so K acts as a preference for
//     larger components. Larger K ⇒ larger, fewer regions; smaller K ⇒ more,
//     finer regions. Region count varies roughly inversely with K over a wide
//     range. To target a region count, sweep K (e.g. bisect) — for a ~1024×559
//     photo, K in the low hundreds typically yields low-thousands of regions.
//   - MinSize (pixels): a post-merge floor. Any component smaller than MinSize
//     is absorbed into the neighbour across its lowest-weight boundary edge.
//     Raising MinSize removes small regions (speckle, thin slivers) and lowers
//     the count without much changing the large-region structure.
//   - PreFilter (pre-smoothing): the filter applied before the graph is built,
//     suppressing pixel noise and texture so genuine boundaries dominate.
//     PreFilterGaussian blurs everything uniformly (the legacy Sigma field
//     selects this when PreFilter is unset). PreFilterBilateral is
//     edge-preserving: it denoises within regions while keeping strong edges
//     (text, feature lines) crisp — the right choice for region-first
//     vectorization of photos. PreFilterKuwahara flattens interiors into
//     painterly patches. More smoothing tends to reduce region count. See the
//     PreFilter constants and BilateralFilter for parameter guidance.
package segment

import (
	"image"
	"image/color"
	"math"
	"sort"
)

// opaqueAlpha is the alpha threshold (matching the quantize stage) at or above
// which a pixel is treated as opaque and eligible for segmentation.
const opaqueAlpha = 128

// TransparentLabel is the sentinel region id assigned to pixels with
// alpha < 128. Such pixels take part in no region: they form no graph edges and
// are never merged into opaque regions, so segmentation of a partly transparent
// image cannot bleed a region across a transparent gap. Transparent pixels are
// not counted in Result.NumRegions and have no entry in MeanColors' output. For
// fully opaque images no pixel ever receives this label.
const TransparentLabel = -1

// Options configures Segment. See the package documentation for guidance on
// trading these knobs off against a target region count.
type Options struct {
	// K is the scale/threshold parameter of the FH merge predicate. Larger K
	// yields larger, fewer regions; smaller K yields more, finer regions. It
	// must be > 0 for a meaningful segmentation (values <= 0 are treated as a
	// very small positive scale, giving a maximally fine partition).
	K float64

	// MinSize is the minimum region size in pixels after the main merge pass.
	// Components smaller than MinSize are absorbed into the neighbouring region
	// across their lowest-weight boundary edge. 0 or 1 disables the pass.
	MinSize int

	// Sigma is the legacy Gaussian pre-smoothing control, retained for
	// backward compatibility. It is honoured ONLY when PreFilter is left at
	// its zero value (PreFilterNone): Sigma > 0 then applies a Gaussian blur
	// of that sigma before the graph is built (exactly the historical
	// behaviour) and Sigma == 0 disables smoothing. When any explicit
	// PreFilter is selected, Sigma is ignored except as a fallback radius for
	// PreFilterGaussian when SpatialSigma is unset. Region colours reported by
	// MeanColors are always computed from the original, unsmoothed image.
	Sigma float64

	// PreFilter selects the edge/pre-smoothing filter applied to the working
	// image before the FH edge graph is built. The zero value (PreFilterNone)
	// preserves the legacy Sigma-driven behaviour, so Options{} and any
	// Sigma-only Options are unaffected by this field's existence. See
	// PreFilter's constants for the available filters and their parameters.
	PreFilter PreFilter

	// SpatialSigma is the spatial parameter of the selected PreFilter: the
	// bilateral/Gaussian blur radius sigma (σ_s), or, for Kuwahara, the window
	// radius in pixels (via int(SpatialSigma)). Ignored when PreFilter is
	// PreFilterNone.
	SpatialSigma float64

	// RangeSigma is the range (colour-difference) parameter σ_r of the
	// bilateral filter: smaller values preserve more edges, larger values
	// blend across bigger colour differences. Only PreFilterBilateral uses it.
	//
	// It is the primary "detail vs smoothness" dial for photo vectorization.
	// The reasonable range is roughly [8, 40]; DefaultRangeSigma (12) is the
	// balanced recommendation:
	//   - ~28-40: soft, abstract output — low-contrast facial shading and small
	//     text get blended away.
	//   - ~12 (default): preserves facial contrast and small-sign lettering
	//     while still denoising smooth gradients into clean regions.
	//   - ~8: even punchier, but region count climbs and faces can start to look
	//     harsh/over-segmented.
	//   - below ~8: approaches no filtering — noise and pixel-jaggies return and
	//     the region count (and file size) grows sharply.
	// Values ≤ 0 make the bilateral filter a no-op (identity).
	RangeSigma float64

	// BoundarySmooth is the number of boundary-smoothing iterations applied to
	// the region label map AFTER region formation and the MinSize merge, to
	// round off the pixel-staircase jaggies (tiny 1-2px protrusions and notches)
	// that otherwise make traced region outlines look distorted. Each iteration
	// is a deterministic label-map mode (majority) filter: a boundary pixel may
	// flip to the dominant neighbouring region label, so convex teeth erode and
	// concave notches fill, leaving smoother outlines. See smooth.go for the
	// technique, the small-region freeze that protects sign lettering, and the
	// connected-component re-labelling that guarantees a valid partition.
	//
	// The zero value (the default) disables smoothing entirely, so Options with
	// BoundarySmooth unset produce byte-for-byte the same Labels as before this
	// field existed. A few iterations (1-5) suffice; cost is O(BoundarySmooth ·
	// W · H).
	BoundarySmooth int
}

// DefaultRangeSigma is the recommended bilateral RangeSigma (σ_r) for photo
// vectorization: the balanced point between preserving fine contrast (punchy
// faces, legible small text) and denoising smooth gradients into clean
// regions. See [Options.RangeSigma] for the full range and trade-offs.
const DefaultRangeSigma = 12

// DefaultOptions returns the recommended settings for region-first
// vectorization of a photographic image, tuned on a ~1024×559 image: an
// edge-preserving bilateral pre-filter, fine regions, and a few boundary-
// smoothing iterations. Callers adjust from here — most often [Options.K]
// (region count) and [Options.RangeSigma] (detail vs smoothness, held at
// [DefaultRangeSigma]).
func DefaultOptions() Options {
	return Options{
		K:              100,
		MinSize:        4,
		PreFilter:      PreFilterBilateral,
		SpatialSigma:   2,
		RangeSigma:     DefaultRangeSigma,
		BoundarySmooth: 3,
	}
}

// Result holds a per-pixel region labelling produced by Segment.
type Result struct {
	// Labels has length W*H, row-major (index = y*W + x). Labels[i] is the
	// dense region id of pixel i in [0, NumRegions), or TransparentLabel (-1)
	// for a pixel excluded as transparent. Every region is spatially connected
	// by construction.
	Labels []int

	// NumRegions is the number of distinct opaque regions, i.e. the number of
	// valid label values (transparent pixels are not counted).
	NumRegions int

	// W and H are the working dimensions of the segmented image.
	W, H int
}

// edge is a weighted graph edge between two pixels. a < b always holds, so the
// pair (a, b) is a canonical, unique identifier used for deterministic
// tie-breaking when weights are equal.
type edge struct {
	a, b int32
	w    float32
}

// Segment partitions img's opaque pixels into spatially connected regions of
// similar colour using Felzenszwalb-Huttenlocher graph segmentation.
//
// It is deterministic: identical (img, opt) inputs always produce identical
// Labels. Pixels with alpha < 128 are excluded and labelled TransparentLabel.
func Segment(img *image.NRGBA, opt Options) Result {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	n := w * h
	res := Result{Labels: make([]int, n), W: w, H: h}
	if n == 0 {
		return res
	}

	// Opacity is decided on the ORIGINAL alpha channel; smoothing must not turn
	// a transparent pixel opaque or vice versa.
	pix := img.Pix

	// Edge weights are computed on the pre-filtered image (bilateral, Kuwahara,
	// Gaussian, or none) selected by opt; MeanColors still reads the original
	// img so region colours are true, not smoothed. preFilterPix returns the
	// original pix unchanged when no smoothing is requested.
	spix := preFilterPix(img, opt)

	opaque := make([]bool, n)
	for i := 0; i < n; i++ {
		opaque[i] = pix[i*4+3] >= opaqueAlpha
	}

	edges := buildEdges(spix, opaque, w, h)

	// Total order: weight ascending, then the canonical endpoint pair. This is
	// the mandated deterministic tie-break for equal-weight edges.
	sort.Sort(edgesByWeight(edges))

	k := opt.K
	if k <= 0 {
		k = math.SmallestNonzeroFloat64
	}
	dsu := newDisjoint(n)
	mergeFH(dsu, edges, k)
	if opt.MinSize > 1 {
		mergeMinSize(dsu, edges, opt.MinSize)
	}

	res.NumRegions = relabel(dsu, opaque, n, res.Labels)

	// Optional boundary smoothing (technique A, label-map mode filter). Applied
	// after region formation / MinSize merge and after the dense relabel, then
	// re-densified by connected component so the output stays a valid, connected
	// partition. Skipped entirely when BoundarySmooth == 0, preserving the exact
	// pre-existing output. MeanColors is computed by the caller on these final
	// labels, so region colours match the smoothed regions.
	if opt.BoundarySmooth > 0 {
		smoothBoundaries(res.Labels, w, h, res.NumRegions, opt.MinSize, opt.BoundarySmooth)
		res.NumRegions = relabelConnected(res.Labels, w, h)
	}
	return res
}

// buildEdges constructs the 8-connected pixel graph. Every opaque pixel is
// linked to its opaque right, down, down-right and down-left neighbours; each
// undirected edge is emitted exactly once with the lower pixel index as a.
// 8-connectivity (rather than 4) is used because it lets diagonally adjacent
// same-colour pixels form a single region and reduces staircase fragmentation
// along diagonal boundaries — the connectivity FH themselves use for images.
// Edges touching a transparent pixel are never created, so transparent pixels
// stay isolated.
func buildEdges(spix []uint8, opaque []bool, w, h int) []edge {
	edges := make([]edge, 0, 4*w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if !opaque[i] {
				continue
			}
			if x+1 < w { // right
				if j := i + 1; opaque[j] {
					edges = append(edges, edge{int32(i), int32(j), colorDist(spix, i, j)})
				}
			}
			if y+1 < h { // down
				if j := i + w; opaque[j] {
					edges = append(edges, edge{int32(i), int32(j), colorDist(spix, i, j)})
				}
			}
			if x+1 < w && y+1 < h { // down-right
				if j := i + w + 1; opaque[j] {
					edges = append(edges, edge{int32(i), int32(j), colorDist(spix, i, j)})
				}
			}
			if x-1 >= 0 && y+1 < h { // down-left
				if j := i + w - 1; opaque[j] {
					edges = append(edges, edge{int32(i), int32(j), colorDist(spix, i, j)})
				}
			}
		}
	}
	return edges
}

// colorDist is the Euclidean distance in RGB between pixels i and j of a flat
// RGBA8 buffer. Alpha is ignored (opacity is handled separately).
func colorDist(pix []uint8, i, j int) float32 {
	oi, oj := i*4, j*4
	dr := float64(pix[oi]) - float64(pix[oj])
	dg := float64(pix[oi+1]) - float64(pix[oj+1])
	db := float64(pix[oi+2]) - float64(pix[oj+2])
	return float32(math.Sqrt(dr*dr + dg*dg + db*db))
}

// edgesByWeight imposes the deterministic total order on edges: non-decreasing
// weight, tie-broken by the lower endpoint a, then the higher endpoint b.
type edgesByWeight []edge

func (e edgesByWeight) Len() int      { return len(e) }
func (e edgesByWeight) Swap(i, j int) { e[i], e[j] = e[j], e[i] }
func (e edgesByWeight) Less(i, j int) bool {
	if e[i].w != e[j].w {
		return e[i].w < e[j].w
	}
	if e[i].a != e[j].a {
		return e[i].a < e[j].a
	}
	return e[i].b < e[j].b
}

// mergeFH runs the main FH merge pass over edges (which must already be sorted
// by non-decreasing weight). For each edge joining two distinct components C1,
// C2 across weight w, the components merge iff
//
//	w <= min( Int(C1) + k/|C1| , Int(C2) + k/|C2| )
//
// where Int(C) is the component's internal difference (largest MST edge) and
// k/|C| is the FH threshold function τ(C). Because edges are processed in
// weight order, w is the largest edge in the merged component's spanning tree
// and becomes its new internal difference.
func mergeFH(dsu *disjoint, edges []edge, k float64) {
	for _, e := range edges {
		ra := dsu.find(e.a)
		rb := dsu.find(e.b)
		if ra == rb {
			continue
		}
		w := float64(e.w)
		thrA := float64(dsu.intDiff[ra]) + k/float64(dsu.size[ra])
		thrB := float64(dsu.intDiff[rb]) + k/float64(dsu.size[rb])
		if w <= thrA && w <= thrB {
			dsu.union(ra, rb, e.w)
		}
	}
}

// mergeMinSize is the FH post-processing pass: it absorbs every component
// smaller than minSize into an adjacent component. Edges are revisited in the
// same non-decreasing weight order, so each undersized component is joined
// across its lowest-weight boundary edge first. The merge weight is irrelevant
// here (segmentation is already fixed), so intDiff is updated with the edge
// weight only for bookkeeping consistency.
func mergeMinSize(dsu *disjoint, edges []edge, minSize int) {
	ms := int32(minSize)
	for _, e := range edges {
		ra := dsu.find(e.a)
		rb := dsu.find(e.b)
		if ra == rb {
			continue
		}
		if dsu.size[ra] < ms || dsu.size[rb] < ms {
			dsu.union(ra, rb, e.w)
		}
	}
}

// relabel assigns dense region ids in [0, NumRegions) to opaque pixels by their
// component root, in row-major order of first appearance, and writes them into
// labels. Transparent pixels receive TransparentLabel. It returns the number of
// distinct regions.
func relabel(dsu *disjoint, opaque []bool, n int, labels []int) int {
	remap := make([]int, n)
	for i := range remap {
		remap[i] = -1
	}
	next := 0
	for i := 0; i < n; i++ {
		if !opaque[i] {
			labels[i] = TransparentLabel
			continue
		}
		r := dsu.find(int32(i))
		id := remap[r]
		if id < 0 {
			id = next
			remap[r] = id
			next++
		}
		labels[i] = id
	}
	return next
}

// MeanColors returns the mean opaque colour of each region, indexed by region
// id: the returned slice has length r.NumRegions and element k is the average
// R,G,B,A of the pixels labelled k in img. Colours are computed from img
// (typically the original, unsmoothed image). Transparent pixels
// (TransparentLabel) contribute to no region. A region always has at least one
// pixel, so no divide-by-zero occurs.
func MeanColors(img *image.NRGBA, r Result) []color.RGBA {
	pix := img.Pix
	var sumR, sumG, sumB, sumA, cnt []uint64
	sumR = make([]uint64, r.NumRegions)
	sumG = make([]uint64, r.NumRegions)
	sumB = make([]uint64, r.NumRegions)
	sumA = make([]uint64, r.NumRegions)
	cnt = make([]uint64, r.NumRegions)

	for i, lb := range r.Labels {
		if lb < 0 {
			continue
		}
		o := i * 4
		sumR[lb] += uint64(pix[o])
		sumG[lb] += uint64(pix[o+1])
		sumB[lb] += uint64(pix[o+2])
		sumA[lb] += uint64(pix[o+3])
		cnt[lb]++
	}

	out := make([]color.RGBA, r.NumRegions)
	for k := 0; k < r.NumRegions; k++ {
		c := cnt[k]
		if c == 0 {
			out[k] = color.RGBA{A: 255}
			continue
		}
		out[k] = color.RGBA{
			R: uint8((sumR[k] + c/2) / c),
			G: uint8((sumG[k] + c/2) / c),
			B: uint8((sumB[k] + c/2) / c),
			A: uint8((sumA[k] + c/2) / c),
		}
	}
	return out
}
