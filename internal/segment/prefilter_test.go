package segment

import (
	"image"
	"image/color"
	"reflect"
	"testing"

	"github.com/disintegration/imaging"
)

// coordNoise returns a deterministic per-pixel noise value in [-10,10] derived
// purely from the pixel coordinates, so tests never depend on math/rand or the
// wall clock.
func coordNoise(x, y int) int {
	return (x*13+y*7)%21 - 10
}

// stepNoisyImage builds a w×h image of two solid half-planes split at x=w/2
// (left colour lo, right colour hi on every channel) with coordNoise added to
// every channel and clamped to [0,255].
func stepNoisyImage(w, h int, lo, hi uint8) *image.NRGBA {
	return solidImage(w, h, func(x, y int) color.NRGBA {
		base := lo
		if x >= w/2 {
			base = hi
		}
		v := clampInt(int(base) + coordNoise(x, y))
		return color.NRGBA{v, v, v, 255}
	})
}

func clampInt(v int) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// meanVar returns the mean and variance of the green channel over the columns
// [x0,x1) and all rows of img.
func meanVar(img *image.NRGBA, x0, x1 int) (mean, variance float64) {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	var sum, sum2, n float64
	for y := 0; y < h; y++ {
		for x := x0; x < x1; x++ {
			v := float64(img.Pix[(y*w+x)*4+1])
			sum += v
			sum2 += v * v
			n++
		}
	}
	mean = sum / n
	variance = sum2/n - mean*mean
	return
}

// TestBilateralEdgePreservation asserts that on a noisy step-edge image the
// bilateral filter (a) preserves the sharp edge — deep-interior pixels stay
// near their block colour and no bleeding pushes them toward the midpoint, and
// the transition stays narrow — while (b) reducing within-region noise.
func TestBilateralEdgePreservation(t *testing.T) {
	const w, h = 60, 40
	const lo, hi = 60, 200
	in := stepNoisyImage(w, h, lo, hi)

	// σ_r = 25 is far below the 140-level step, so the range weight collapses
	// across the edge; σ_s = 2 gives a modest smoothing window.
	out := BilateralFilter(in, 2.0, 25.0)

	// (a1) Edge preserved: deep-left mean stays near lo and deep-right near hi,
	// each well clear of the 130 midpoint (no cross-edge bleed).
	lMean, _ := meanVar(out, 5, 25)
	rMean, _ := meanVar(out, 35, 55)
	if lMean > 100 {
		t.Fatalf("left region mean %.1f too high; edge not preserved (want < 100, near %d)", lMean, lo)
	}
	if rMean < 160 {
		t.Fatalf("right region mean %.1f too low; edge not preserved (want > 160, near %d)", rMean, hi)
	}

	// (a2) Transition stays narrow: the column just left of the boundary stays
	// low and the column at the boundary stays high — a one-pixel step.
	colLeft, _ := meanVar(out, w/2-1, w/2)
	colRight, _ := meanVar(out, w/2, w/2+1)
	if colLeft > 110 {
		t.Fatalf("column left of boundary mean %.1f too high; edge smeared", colLeft)
	}
	if colRight < 150 {
		t.Fatalf("column right of boundary mean %.1f too low; edge smeared", colRight)
	}

	// (b) Noise reduced: within-region variance drops after filtering. Measure
	// on the deep-left interior, away from the boundary.
	_, inVar := meanVar(in, 5, 25)
	_, outVar := meanVar(out, 5, 25)
	if !(outVar < inVar) {
		t.Fatalf("within-region variance not reduced: in=%.2f out=%.2f", inVar, outVar)
	}
	if inVar == 0 {
		t.Fatal("test setup error: input has no within-region variance to reduce")
	}
}

// TestBilateralDeterminism verifies identical output for identical input and,
// end-to-end, identical Segment labels across two runs.
func TestBilateralDeterminism(t *testing.T) {
	const w, h = 50, 40
	in := stepNoisyImage(w, h, 50, 210)

	a := BilateralFilter(in, 2.5, 30.0)
	b := BilateralFilter(in, 2.5, 30.0)
	if !reflect.DeepEqual(a.Pix, b.Pix) {
		t.Fatal("BilateralFilter output differs across identical runs")
	}

	opt := Options{K: 150, MinSize: 4, PreFilter: PreFilterBilateral, SpatialSigma: 2.5, RangeSigma: 30.0}
	s1 := Segment(in, opt)
	s2 := Segment(in, opt)
	if s1.NumRegions != s2.NumRegions || !reflect.DeepEqual(s1.Labels, s2.Labels) {
		t.Fatal("Segment with bilateral pre-filter is not deterministic")
	}
}

// TestKuwaharaDeterminism verifies the optional Kuwahara filter is deterministic
// and edge-preserving enough to keep the two blocks distinct.
func TestKuwaharaDeterminism(t *testing.T) {
	const w, h = 50, 40
	in := stepNoisyImage(w, h, 50, 210)
	a := KuwaharaFilter(in, 3)
	b := KuwaharaFilter(in, 3)
	if !reflect.DeepEqual(a.Pix, b.Pix) {
		t.Fatal("KuwaharaFilter output differs across identical runs")
	}
	lMean, _ := meanVar(a, 5, 20)
	rMean, _ := meanVar(a, 30, 50)
	if !(lMean < 130 && rMean > 130) {
		t.Fatalf("Kuwahara did not preserve the step: left=%.1f right=%.1f", lMean, rMean)
	}
}

// TestBackCompatSigmaMapping guards the backward-compatibility contract by
// exercising preFilterPix directly (same package): the zero-value Options must
// leave the image untouched, and a Sigma-only Options must route through the
// exact legacy imaging.Blur path.
func TestBackCompatSigmaMapping(t *testing.T) {
	const w, h = 32, 24
	img := solidImage(w, h, func(x, y int) color.NRGBA {
		v := uint8((x*11 + y*17) % 256)
		return color.NRGBA{v, uint8((x * 5) % 256), uint8((y * 3) % 256), 255}
	})

	// Zero value: no smoothing, original buffer returned unchanged.
	if got := preFilterPix(img, Options{}); !reflect.DeepEqual(got, img.Pix) {
		t.Fatal("Options{} must not alter the working pixels")
	}

	// Sigma-only (PreFilterNone): must equal the legacy imaging.Blur output.
	wantBlur := imaging.Blur(img, 0.8).Pix
	if got := preFilterPix(img, Options{Sigma: 0.8}); !reflect.DeepEqual(got, wantBlur) {
		t.Fatal("Sigma-only Options must map to the legacy Gaussian blur")
	}

	// Explicit PreFilterGaussian with SpatialSigma must match the same blur,
	// and it must equal the legacy Sigma path for the same radius.
	if got := preFilterPix(img, Options{PreFilter: PreFilterGaussian, SpatialSigma: 0.8}); !reflect.DeepEqual(got, wantBlur) {
		t.Fatal("PreFilterGaussian SpatialSigma must match imaging.Blur")
	}
}

// TestBackCompatSegmentUnchanged asserts that Options{} and Sigma-only Options
// produce the SAME Segment result whether expressed via the legacy Sigma field
// or the explicit Gaussian pre-filter, so existing callers are unaffected.
func TestBackCompatSegmentUnchanged(t *testing.T) {
	const w, h = 40, 40
	img := solidImage(w, h, func(x, y int) color.NRGBA {
		v := uint8((x*7 + y*13) % 256)
		return color.NRGBA{v, uint8((x * 3) % 256), uint8((y * 5) % 256), 255}
	})

	// Zero value must be a stable, reproducible partition (no smoothing path).
	z1 := Segment(img, Options{K: 200, MinSize: 4})
	z2 := Segment(img, Options{K: 200, MinSize: 4})
	if z1.NumRegions != z2.NumRegions || !reflect.DeepEqual(z1.Labels, z2.Labels) {
		t.Fatal("zero-value Options segmentation is not stable")
	}

	// Legacy Sigma field and explicit Gaussian with equal radius must agree.
	legacy := Segment(img, Options{K: 200, MinSize: 4, Sigma: 0.8})
	explicit := Segment(img, Options{K: 200, MinSize: 4, PreFilter: PreFilterGaussian, SpatialSigma: 0.8})
	if legacy.NumRegions != explicit.NumRegions || !reflect.DeepEqual(legacy.Labels, explicit.Labels) {
		t.Fatal("legacy Sigma and explicit PreFilterGaussian must yield identical segmentation")
	}
}
