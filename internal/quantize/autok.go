package quantize

import (
	"github.com/aleybovich/vectrigo/internal/normalize"
)

// Auto-K tuning constants. These govern only the automatic cluster-count
// selection performed by [SelectK]; they have no effect on the fixed-K
// quantization path.
const (
	// autoKScanCeiling caps how many candidate cluster counts SelectK evaluates
	// (k = 2..autoKScanCeiling). It bounds the cost of the distortion scan and
	// is deliberately set to the top of the built-in detail curve's K range, so
	// auto-K spans the same useful colour-count range as the highest manual
	// Sensitivity. It is a performance ceiling, but it is also the user-visible
	// upper bound on the auto-selected K (documented on Config.AutoK): the
	// effective bound is min(autoKScanCeiling, maxK, distinctColours).
	autoKScanCeiling = 64

	// autoKMaxSamples caps the number of opaque pixels used to evaluate the
	// distortion curve. Centroids for each candidate k are fitted on a
	// deterministic stride subsample of at most this size, keeping the
	// multi-k scan fast while staying byte-reproducible. It is intentionally
	// smaller than the production fit's maxFitSamples because the scan fits
	// many values of k rather than one.
	autoKMaxSamples = 4096

	// autoKTau documents the default residual-distortion threshold for the knee.
	// SelectK picks the smallest k whose within-cluster distortion has fallen to
	// <= tau of the single-cluster (k=1) distortion — i.e. the k that already
	// explains (1 - tau) of the image's colour variation. 0.02 => 98% explained.
	// This const records the default only; the effective threshold is the tau
	// argument passed to SelectK (see Config.AutoKTau / defaultAutoKTau).
	autoKTau = 0.02
)

// SelectK automatically chooses a cluster count K for img from its colour
// complexity, for use when [Config.AutoK] is enabled. The result is clamped to
// [2, maxK] and never exceeds the number of distinct opaque colours present
// (for a 1-colour image it may return 1). Sensitivity plays no part: the value
// depends only on the pixels and maxK.
//
// Method. SelectK computes the k-means within-cluster sum-of-squares
// (distortion) for k = 1, 2, 3, … and stops at the "knee" — the smallest k
// whose distortion has dropped to tau of the single-cluster distortion,
// meaning further clusters yield only diminishing returns. A smaller tau
// demands more colours (higher fidelity); a larger tau trips the knee earlier,
// yielding fewer colours and differentiating complex images that otherwise
// saturate at the ceiling. Callers pass the (already defaulted/clamped) tau;
// see Config.AutoKTau. This
// explained-distortion / scree criterion is preferred over the geometric
// Kneedle distance-to-chord method because for images made of well-separated
// solid colour regions the distortion curve is piecewise-linear (its
// normalized points are colinear), so a geometric "maximum curvature" knee is
// ill-defined, whereas the threshold recovers the true region count exactly and
// still grows smoothly with image complexity (a gradient yields a larger K than
// a flat image). To keep the scan cheap, centroids are fitted on a deterministic
// stride subsample (see autoKMaxSamples) and candidates are capped at
// autoKScanCeiling.
//
// SelectK is deterministic: it reuses the seeded k-means core (kmeansSeed), so
// the same image always yields the same K.
func SelectK(img normalize.Image, maxK int, tau float64) int {
	b := img.NRGBA.Bounds()
	w, h := b.Dx(), b.Dy()
	n := w * h
	if n == 0 {
		return 2
	}
	pix := img.NRGBA.Pix

	// Opaque pixels only; transparent pixels never take part in clustering.
	opaque := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if pix[i*4+3] >= 128 {
			opaque = append(opaque, i)
		}
	}
	if len(opaque) == 0 {
		return 2
	}

	// Upper bound: the caller's safety clamp, our scan ceiling, and the number
	// of distinct colours actually present. hi may legitimately fall below 2
	// only when fewer than two distinct colours exist.
	hi := maxK
	if hi > autoKScanCeiling {
		hi = autoKScanCeiling
	}
	distinct := distinctColorsCapped(pix, opaque, hi)
	if distinct < hi {
		hi = distinct
	}
	if hi <= 2 {
		if hi < 1 {
			return 1
		}
		return hi
	}

	sample := strideSample(opaque, autoKMaxSamples)

	// Single-cluster distortion is the baseline "total colour variation". If it
	// is zero every opaque pixel shares one colour and there is nothing to
	// separate.
	base := sampleDistortion(pix, sample, 1)
	if base <= 0 {
		return 2
	}

	// Smallest k that explains (1 - tau) of the colour variation.
	for k := 2; k <= hi; k++ {
		if sampleDistortion(pix, sample, k)/base <= tau {
			return k
		}
	}
	return hi
}

// sampleDistortion fits k centroids on sample and returns the within-cluster
// sum of squared distances (the k-means distortion) over the sample.
func sampleDistortion(pix []uint8, sample []int, k int) float64 {
	centroids, _ := fitCentroids(pix, sample, k)
	sse := 0.0
	for _, idx := range sample {
		p := rgbAt(pix, idx)
		sse += sqDist(p, centroids[nearest(centroids, p)])
	}
	return sse
}

// distinctColorsCapped counts distinct RGB colours among the opaque pixels,
// stopping early once the count exceeds limit (the caller only needs to know
// whether distinct colours are scarcer than the candidate ceiling).
func distinctColorsCapped(pix []uint8, opaque []int, limit int) int {
	seen := make(map[uint32]struct{}, limit+1)
	for _, idx := range opaque {
		o := idx * 4
		key := uint32(pix[o])<<16 | uint32(pix[o+1])<<8 | uint32(pix[o+2])
		seen[key] = struct{}{}
		if len(seen) > limit {
			return len(seen)
		}
	}
	return len(seen)
}
