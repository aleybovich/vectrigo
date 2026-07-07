package segment

import (
	"image"
	"math"
)

// PreFilter selects the optional pre-smoothing applied to the working image
// before the Felzenszwalb-Huttenlocher edge graph is built. The filter affects
// only the edge weights that drive segmentation; region colours reported by
// MeanColors are always taken from the original, unfiltered image.
type PreFilter int

const (
	// PreFilterNone applies no dedicated pre-filter. For backward
	// compatibility, when PreFilter is left at this zero value the legacy
	// Options.Sigma field is honoured: Sigma > 0 applies a Gaussian blur of
	// that sigma (exactly the historical behaviour) and Sigma == 0 leaves the
	// image untouched. Any explicit PreFilter value ignores Sigma.
	PreFilterNone PreFilter = iota

	// PreFilterGaussian applies a separable Gaussian blur. It smooths
	// everything uniformly, including edges, so it blurs text and facial
	// detail as readily as noise. The radius sigma is SpatialSigma (falling
	// back to the legacy Sigma field when SpatialSigma <= 0).
	PreFilterGaussian

	// PreFilterBilateral applies an edge-preserving bilateral filter (see
	// BilateralFilter). It denoises within homogeneous regions while keeping
	// strong edges crisp, which is the right tool for region-first
	// vectorization of photos: facial shading is smoothed into clean zones
	// while text strokes and feature lines stay sharp. Controlled by
	// SpatialSigma (blur radius) and RangeSigma (colour-difference tolerance).
	PreFilterBilateral

	// PreFilterKuwahara applies a Kuwahara filter (see KuwaharaFilter), an
	// edge-preserving smoother that produces flat, painterly regions with
	// preserved edges — well suited to cel-shaded vectorization. The window
	// radius is int(SpatialSigma); a radius below 1 disables the filter
	// (identity copy), matching KuwaharaFilter.
	PreFilterKuwahara
)

// preFilterPix returns the RGBA8 pixel buffer of the working image after the
// pre-filter selected by opt has been applied. It never mutates img. When no
// smoothing is requested it returns img.Pix directly (no copy), matching the
// pre-existing behaviour where the original buffer feeds the graph.
//
// Back-compat rule: a zero-value PreFilter routes through the legacy Sigma
// field (Sigma > 0 ⇒ Gaussian, else identity), so Options{} and any
// Sigma-only Options behave exactly as before this filter existed.
func preFilterPix(img *image.NRGBA, opt Options) []uint8 {
	switch opt.PreFilter {
	case PreFilterNone:
		if opt.Sigma > 0 {
			return GaussianBlur(img, opt.Sigma).Pix
		}
		return img.Pix
	case PreFilterGaussian:
		s := opt.SpatialSigma
		if s <= 0 {
			s = opt.Sigma
		}
		if s <= 0 {
			return img.Pix
		}
		return GaussianBlur(img, s).Pix
	case PreFilterBilateral:
		return BilateralFilter(img, opt.SpatialSigma, opt.RangeSigma).Pix
	case PreFilterKuwahara:
		return KuwaharaFilter(img, int(opt.SpatialSigma)).Pix
	default:
		return img.Pix
	}
}

// clampRound rounds a non-negative weighted average to the nearest uint8,
// clamping to [0,255]. Inputs are weighted averages of channel values already
// in [0,255], so v is in range; the clamp only guards floating-point rounding
// at the top of the range.
func clampRound(v float64) uint8 {
	n := int(v + 0.5)
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return uint8(n)
}

// BilateralFilter applies a bilateral (edge-preserving) filter to img and
// returns a new *image.NRGBA of the same bounds. The alpha channel is copied
// through unchanged; only R,G,B are filtered.
//
// For each output pixel the result is a normalized weighted average of the
// pixels in a bounded square window centred on it, with each neighbour's
// weight being the product of two Gaussians:
//
//	weight = exp(-(Δx²+Δy²) / (2·σ_s²)) · exp(-(ΔR²+ΔG²+ΔB²) / (2·σ_r²))
//
// The spatial Gaussian (σ_s) weights nearby pixels more; the range Gaussian
// (σ_r) weights similarly-coloured pixels more. Across a strong edge the RGB
// difference is large, the range weight collapses toward zero, and the edge is
// preserved; within a flat or noisy region colours are similar, the range
// weight stays near one, and the window averages away noise.
//
// Guidance:
//   - σ_s (spatialSigma) sets the smoothing radius: larger ⇒ a wider window
//     and stronger blur within regions. The window half-width is bounded to
//     ⌈2.5·σ_s⌉ pixels (beyond which the spatial weight is negligible) to keep
//     cost bounded.
//   - σ_r (rangeSigma) sets how large a colour difference (in 0-255 RGB
//     Euclidean terms) still gets blended: smaller σ_r preserves more edges
//     (only near-identical colours mix); larger σ_r behaves more like a plain
//     Gaussian. A σ_r of roughly 20-40 preserves typical photographic edges
//     while smoothing shading.
//
// A full 2D bounded window is used (not a separable two-pass approximation):
// the range weight is jointly non-separable, so a separable pass would leak
// colour across edges on the second axis. Border pixels are handled by
// clamping neighbour coordinates to the image bounds. The computation is
// deterministic (fixed traversal order, no randomness). Non-positive σ_s or
// σ_r yield an identity copy.
//
// Performance: cost is O(W·H·(2r+1)²) with r = ⌈2.5·σ_s⌉. Range weights are
// looked up from a precomputed table indexed by integer squared RGB distance
// and spatial weights from a per-offset table, so the inner loop is a few
// multiply-adds. On ~600k pixels with σ_s≈3 (r=8, 17×17 window) it runs in a
// few seconds.
func BilateralFilter(img *image.NRGBA, spatialSigma, rangeSigma float64) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(b)
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return out
	}
	if spatialSigma <= 0 || rangeSigma <= 0 {
		copy(out.Pix, img.Pix)
		return out
	}

	radius := int(math.Ceil(2.5 * spatialSigma))
	if radius < 1 {
		radius = 1
	}
	diam := 2*radius + 1

	// Precompute spatial weights over the window, indexed row-major by
	// (dy+radius, dx+radius).
	sw := make([]float64, diam*diam)
	s2 := 2 * spatialSigma * spatialSigma
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			sw[(dy+radius)*diam+(dx+radius)] = math.Exp(-float64(dx*dx+dy*dy) / s2)
		}
	}

	// Precompute range weights indexed by squared RGB Euclidean distance, which
	// is an integer in [0, 3·255²].
	const maxSq = 3 * 255 * 255
	rw := make([]float64, maxSq+1)
	r2 := 2 * rangeSigma * rangeSigma
	for d := 0; d <= maxSq; d++ {
		rw[d] = math.Exp(-float64(d) / r2)
	}

	pix := img.Pix
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ci := (y*w + x) * 4
			cr := int(pix[ci])
			cg := int(pix[ci+1])
			cb := int(pix[ci+2])

			var sumR, sumG, sumB, sumWt float64
			for dy := -radius; dy <= radius; dy++ {
				ny := y + dy
				if ny < 0 {
					ny = 0
				} else if ny >= h {
					ny = h - 1
				}
				rowSW := (dy + radius) * diam
				rowPix := ny * w
				for dx := -radius; dx <= radius; dx++ {
					nx := x + dx
					if nx < 0 {
						nx = 0
					} else if nx >= w {
						nx = w - 1
					}
					ni := (rowPix + nx) * 4
					nr := int(pix[ni])
					ng := int(pix[ni+1])
					nb := int(pix[ni+2])
					ddr := nr - cr
					ddg := ng - cg
					ddb := nb - cb
					sq := ddr*ddr + ddg*ddg + ddb*ddb
					wt := sw[rowSW+(dx+radius)] * rw[sq]
					sumR += wt * float64(nr)
					sumG += wt * float64(ng)
					sumB += wt * float64(nb)
					sumWt += wt
				}
			}
			// sumWt >= centre weight (1) > 0, so no divide-by-zero.
			out.Pix[ci] = clampRound(sumR / sumWt)
			out.Pix[ci+1] = clampRound(sumG / sumWt)
			out.Pix[ci+2] = clampRound(sumB / sumWt)
			out.Pix[ci+3] = pix[ci+3]
		}
	}
	return out
}

// KuwaharaFilter applies a Kuwahara filter of the given window radius to img and
// returns a new *image.NRGBA of the same bounds. The alpha channel is copied
// through unchanged; only R,G,B are filtered.
//
// For each pixel the (2r+1)×(2r+1) neighbourhood is split into four overlapping
// (r+1)×(r+1) quadrants that meet at the centre pixel. The luminance variance
// of each quadrant is measured and the pixel is replaced by the mean colour of
// the lowest-variance quadrant. Because a quadrant straddling an edge has high
// variance, the filter prefers the flat side, so edges are preserved while
// interiors flatten into near-uniform patches — a painterly, cel-shaded look.
//
// Ties on variance are broken toward the lowest quadrant index (top-left,
// top-right, bottom-left, bottom-right) so the result is deterministic. Border
// pixels clamp their neighbourhood to the image bounds. A radius < 1 yields an
// identity copy.
func KuwaharaFilter(img *image.NRGBA, radius int) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(b)
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return out
	}
	if radius < 1 {
		copy(out.Pix, img.Pix)
		return out
	}

	pix := img.Pix
	// Quadrant corner offsets relative to the centre pixel: {minDx, maxDx,
	// minDy, maxDy}. Quadrants overlap on the centre row/column.
	quads := [4][4]int{
		{-radius, 0, -radius, 0}, // top-left
		{0, radius, -radius, 0},  // top-right
		{-radius, 0, 0, radius},  // bottom-left
		{0, radius, 0, radius},   // bottom-right
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var bestVar float64
			var bestR, bestG, bestB float64
			for qi := 0; qi < 4; qi++ {
				q := quads[qi]
				var sumR, sumG, sumB, sumL, sumL2 float64
				var cnt float64
				for dy := q[2]; dy <= q[3]; dy++ {
					ny := y + dy
					if ny < 0 {
						ny = 0
					} else if ny >= h {
						ny = h - 1
					}
					rowPix := ny * w
					for dx := q[0]; dx <= q[1]; dx++ {
						nx := x + dx
						if nx < 0 {
							nx = 0
						} else if nx >= w {
							nx = w - 1
						}
						ni := (rowPix + nx) * 4
						pr := float64(pix[ni])
						pg := float64(pix[ni+1])
						pb := float64(pix[ni+2])
						// Rec.601 luma for the variance measure.
						l := 0.299*pr + 0.587*pg + 0.114*pb
						sumR += pr
						sumG += pg
						sumB += pb
						sumL += l
						sumL2 += l * l
						cnt++
					}
				}
				mean := sumL / cnt
				variance := sumL2/cnt - mean*mean
				if variance < 0 {
					variance = 0
				}
				if qi == 0 || variance < bestVar {
					bestVar = variance
					bestR = sumR / cnt
					bestG = sumG / cnt
					bestB = sumB / cnt
				}
			}
			ci := (y*w + x) * 4
			out.Pix[ci] = clampRound(bestR)
			out.Pix[ci+1] = clampRound(bestG)
			out.Pix[ci+2] = clampRound(bestB)
			out.Pix[ci+3] = pix[ci+3]
		}
	}
	return out
}

// GaussianBlur applies a separable Gaussian blur of standard deviation sigma to
// img and returns a new *image.NRGBA of the same bounds. Only R,G,B are
// smoothed; the alpha channel is passed through unchanged. It is a pure-Go,
// zero-dependency reimplementation of the legacy Gaussian pre-filter (used by
// PreFilterGaussian and by the back-compat Sigma path); it is not guaranteed
// byte-identical to any particular third-party blur, only to be a correct,
// deterministic Gaussian.
//
// The kernel is a normalized 1-D Gaussian of radius ⌈3·σ⌉ (beyond which the
// Gaussian weight is negligible). The blur is applied in two passes —
// horizontal then vertical — which is exactly equivalent to a full 2-D
// Gaussian convolution because the 2-D Gaussian is separable. Border pixels are
// handled by clamping neighbour coordinates to the image bounds (edge
// extension). The computation uses a fixed traversal order and no randomness,
// so it is fully deterministic. A sigma <= 0 yields an identity copy.
func GaussianBlur(img *image.NRGBA, sigma float64) *image.NRGBA {
	b := img.Bounds()
	out := image.NewNRGBA(b)
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return out
	}
	if sigma <= 0 {
		copy(out.Pix, img.Pix)
		return out
	}

	// Normalized 1-D Gaussian kernel of radius ⌈3·σ⌉.
	radius := int(math.Ceil(3 * sigma))
	if radius < 1 {
		radius = 1
	}
	kernel := make([]float64, 2*radius+1)
	twoS2 := 2 * sigma * sigma
	var sum float64
	for i := -radius; i <= radius; i++ {
		v := math.Exp(-float64(i*i) / twoS2)
		kernel[i+radius] = v
		sum += v
	}
	for i := range kernel {
		kernel[i] /= sum
	}

	// Horizontal pass: img -> tmp. tmp holds the blurred R,G,B (as float64 is
	// unnecessary; uint8 round-trip between passes loses too much precision, so
	// carry intermediate channel values in a float buffer). Alpha is copied
	// through from the source untouched.
	tmpR := make([]float64, w*h)
	tmpG := make([]float64, w*h)
	tmpB := make([]float64, w*h)
	pix := img.Pix
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			var accR, accG, accB float64
			for k := -radius; k <= radius; k++ {
				nx := x + k
				if nx < 0 {
					nx = 0
				} else if nx >= w {
					nx = w - 1
				}
				o := (row + nx) * 4
				wt := kernel[k+radius]
				accR += wt * float64(pix[o])
				accG += wt * float64(pix[o+1])
				accB += wt * float64(pix[o+2])
			}
			p := row + x
			tmpR[p] = accR
			tmpG[p] = accG
			tmpB[p] = accB
		}
	}

	// Vertical pass: tmp -> out. Alpha passed through from the original image.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var accR, accG, accB float64
			for k := -radius; k <= radius; k++ {
				ny := y + k
				if ny < 0 {
					ny = 0
				} else if ny >= h {
					ny = h - 1
				}
				p := ny*w + x
				wt := kernel[k+radius]
				accR += wt * tmpR[p]
				accG += wt * tmpG[p]
				accB += wt * tmpB[p]
			}
			o := (y*w + x) * 4
			out.Pix[o] = clampRound(accR)
			out.Pix[o+1] = clampRound(accG)
			out.Pix[o+2] = clampRound(accB)
			out.Pix[o+3] = pix[o+3]
		}
	}
	return out
}
