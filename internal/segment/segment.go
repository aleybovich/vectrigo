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
//   - Sigma (Gaussian pre-smoothing): blurs the image before building the
//     graph, suppressing pixel noise and JPEG texture so genuine boundaries
//     dominate. 0 disables it; ~0.5–1.0 is a mild, typical setting. More
//     smoothing tends to reduce region count.
package segment

import (
	"image"
	"image/color"
	"math"
	"sort"

	"github.com/disintegration/imaging"
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

	// Sigma controls optional Gaussian pre-smoothing of the image before the
	// graph is built (edge weights are computed on the smoothed image). 0
	// disables smoothing. Region colours reported by MeanColors are always
	// computed from the original, unsmoothed image.
	Sigma float64
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
// similar colour using Felzenszwalb–Huttenlocher graph segmentation.
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

	// Colour weights use the smoothed image when Sigma > 0. imaging.Blur is a
	// deterministic separable Gaussian convolution and is already a dependency.
	spix := pix
	if opt.Sigma > 0 {
		spix = imaging.Blur(img, opt.Sigma).Pix
	}

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
