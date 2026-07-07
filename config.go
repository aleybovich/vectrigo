package vectrigo

import (
	"math"
	"runtime"

	"github.com/aleybovich/segment"
)

const (
	// defaultAutoKTau is the residual-distortion threshold auto-K uses when
	// [Config.AutoKTau] is unset (<= 0) or NaN. It preserves auto-K's historical
	// output. See [Config.AutoKTau].
	defaultAutoKTau = 0.02

	// maxAutoKTau is the upper clamp for [Config.AutoKTau]. Beyond this even
	// simple images start under-counting real colours.
	maxAutoKTau = 0.5

	// defaultPhotoDetail is the bilateral range-sigma (σ_r) used for photo mode
	// when [Config.PhotoDetail] is unset (<= 0) or NaN. It equals
	// segment.DefaultRangeSigma (12), the balanced detail-vs-smoothness point.
	// See [Config.PhotoDetail].
	defaultPhotoDetail = segment.DefaultRangeSigma

	// minPhotoDetail and maxPhotoDetail bound [Config.PhotoDetail] to a sane
	// band. Below ~4 the bilateral filter barely smooths and region count (and
	// file size) explode; above ~60 the image dissolves into a few soft blobs.
	minPhotoDetail = 4
	maxPhotoDetail = 60
)

// Dimensions is a width/height pair in pixels.
type Dimensions struct {
	Width  int
	Height int
}

// PhotoEdge selects how photo mode (see [Config.Photo]) finishes region edges.
// It has NO effect when Photo is false.
type PhotoEdge int

const (
	// PhotoEdgeCrisp disables edge anti-aliasing (shape-rendering=crispEdges):
	// crispest, flat-vector look, perfectly seam-free. The default.
	PhotoEdgeCrisp PhotoEdge = iota
	// PhotoEdgeStroke keeps anti-aliasing and seals the sub-pixel seams with a
	// thin same-colour stroke on each region: slightly softer edges.
	PhotoEdgeStroke
)

// Config configures a vectorization. The primary knob is [Config.Sensitivity];
// the remaining fields are advanced overrides that default cleanly from zero.
//
// Build a Config from [DefaultConfig] and adjust from there. A bare Config{}
// means Sensitivity 0 (maximum posterization), NOT the recommended defaults —
// Sensitivity's zero is a legitimate setting and so cannot double as "unset".
type Config struct {
	// Sensitivity is the primary 0-100 detail dial (integer percent). It drives
	// the derived (K, TurdSize) pair: higher Sensitivity raises the colour
	// count K while easing TurdSize down. Clamped to [0,100].
	//
	// Because 0 is a real value (max posterization), a bare Config{} means
	// Sensitivity 0, not the default — start from DefaultConfig().
	Sensitivity int

	// AutoK selects the cluster count K automatically from the image's colour
	// complexity instead of deriving it from Sensitivity. Default false.
	//
	// When true, K is chosen by [github.com/aleybovich/vectrigo] via a k-means
	// distortion "knee" (see quantize.SelectK): a flat, few-colour image yields
	// a small K and a complex/gradient image a larger one. It is bounded by the
	// existing safety clamps — maxKForPixels(W*H) and the image's distinct-colour
	// count — and by an internal auto-selection ceiling (currently 64 colours)
	// that keeps the multi-K distortion scan fast; it is NOT bounded by
	// Sensitivity. Under AutoK, Sensitivity has NO effect on K whatsoever; it is
	// not a ceiling. TurdSize likewise stops tracking Sensitivity and is derived
	// from the chosen K (see [Config.TurdSize]).
	//
	// Precedence: an explicit K (> 0) is a hard override and wins over AutoK.
	// Otherwise AutoK, when set, wins over Sensitivity for choosing K — the
	// library raises NO error if both AutoK and a Sensitivity are set; the
	// Sensitivity is simply ignored for K. (A front-end may still choose to
	// expose AutoK and Sensitivity as a mutually exclusive choice.)
	AutoK bool

	// AutoKTau is the residual-distortion threshold for auto-K's "knee": the
	// smallest cluster count K whose within-cluster distortion has fallen to
	// this fraction of the single-cluster (K=1) distortion is chosen. It tunes
	// the fidelity/differentiation trade-off. Smaller ⇒ the knee trips later ⇒
	// MORE colours / higher fidelity; larger ⇒ the knee trips earlier ⇒ FEWER
	// colours, which lets complex photos (that otherwise all saturate at the
	// auto-selection ceiling) differentiate into distinct, smaller K values that
	// reflect their complexity, at the cost of coarser output.
	//
	// Only applies when [Config.AutoK] is true AND no explicit K (> 0) override
	// is set (explicit K bypasses auto-K entirely). With AutoK false it has NO
	// effect and output is byte-identical regardless of its value.
	//
	// Zero value means the default: AutoKTau <= 0 (including a bare Config{}) and
	// NaN resolve to 0.02 (the value that preserves auto-K's historical output).
	// It is clamped to a maximum of 0.5; beyond that even simple images start
	// losing real colours.
	AutoKTau float64

	// K forces an exact cluster count, overriding the value derived from
	// Sensitivity. 0 means derive. When set it is clamped to
	// [2, maxKForPixels(W*H)] and never exceeds the image's distinct-colour
	// count.
	//
	// An explicit K (> 0) is a hard override: it wins over [Config.AutoK] as
	// well as over Sensitivity.
	K int

	// TurdSize forces the speckle-area threshold in pixels, passed to bitrace.
	// 0 means derive; a negative value force-disables speckle removal (passes 0
	// to bitrace); a positive value is used as-is. (The sign convention resolves
	// the "0 = derive" vs "0 = disable" ambiguity.)
	//
	// When derived (0): with Sensitivity-driven K it follows the Sensitivity
	// curve; under [Config.AutoK] it is derived from the auto-chosen K instead
	// (floor(32/K), the same inverse K-noise coupling re-expressed through K), so
	// it never depends on Sensitivity. An explicit override behaves identically
	// in both modes.
	TurdSize int

	// AlphaMax is the corner/smoothness axis, independent of detail. Passed to
	// bitrace, which clamps it to [0,1.334]. Default 1.0. Lower is more
	// angular; higher is smoother.
	AlphaMax float64

	// Optimize enables bitrace's looser curve fitting and minisvg's minify +
	// coordinate-rounding pass. Default true.
	Optimize bool

	// MaxDimensions is the downsample ceiling that bounds memory. Inputs larger
	// than this on either axis are high-quality downsampled first. Any axis
	// <= 0 uses the 2048 default.
	MaxDimensions Dimensions

	// Workers is the tracing concurrency. <= 0 means runtime.NumCPU(); it is
	// further capped to the number of layers.
	Workers int

	// Precision is the coordinate decimal-place count used when Optimize is on.
	// Default 2. Clamped to [0,6].
	Precision int

	// Photo selects the region-first PHOTO pipeline (Felzenszwalb graph
	// segmentation via github.com/aleybovich/segment) instead of the default
	// colour-quantization pipeline. Default false.
	//
	// Quantization clusters pixels globally by colour and is crisper on flat /
	// logo art, so it stays the default. Photo mode partitions the image into
	// many small spatially-connected regions, each given its own mean colour and
	// traced, which preserves local detail far better on PHOTOGRAPHIC content.
	//
	// Photo is an EITHER/OR with the quantization knobs: when true, Sensitivity,
	// K, AutoK, AutoKTau and TurdSize have NO effect (there is no colour
	// clustering). The detail dial for photo mode is [Config.PhotoDetail]
	// instead. AlphaMax, Optimize, Precision, Workers and MaxDimensions still
	// apply. When Photo is false the output is byte-identical to the historical
	// quantization output regardless of PhotoDetail.
	Photo bool

	// PhotoDetail is the bilateral range-sigma (σ_r) detail dial for photo mode
	// (see [Config.Photo]); it is the primary detail-vs-smoothness knob and has
	// NO effect when Photo is false.
	//
	// 0 (a bare Config{}, and DefaultConfig's value) means the default,
	// segment.DefaultRangeSigma = 12. In [Config.normalized] it is resolved
	// (0/NaN → 12) and clamped to [4, 60]. Guidance (see segment's RangeSigma
	// doc):
	//   - ~8: punchy but region count climbs; faces can look over-segmented.
	//   - ~12 (default): balanced — preserves facial contrast and small text
	//     while denoising smooth gradients into clean regions.
	//   - ~28+: soft / abstract — low-contrast shading and small text blend away.
	PhotoDetail float64

	// PhotoEdge selects the anti-aliasing finish for photo mode (see
	// [Config.Photo]); it has NO effect when Photo is false.
	//
	// The regions tile the plane exactly (shared boundaries), so no background
	// is ever needed. The zero value, [PhotoEdgeCrisp] (a bare Config{}, and
	// DefaultConfig's value), disables edge anti-aliasing (shape-rendering=
	// crispEdges) for the crispest, perfectly seam-free flat-vector look — the
	// recommended default. [PhotoEdgeStroke] instead keeps anti-aliasing and
	// seals the residual sub-pixel seams with a thin same-colour stroke on each
	// region, for slightly softer edges. Any out-of-range value is clamped to
	// PhotoEdgeCrisp in [Config.normalized].
	PhotoEdge PhotoEdge
}

// DefaultConfig returns the recommended defaults and is the intended entry
// point: build from it, then tweak. Sensitivity is 50 (balanced); K and
// TurdSize are left 0 so they stay derived from Sensitivity.
func DefaultConfig() Config {
	return Config{
		Sensitivity:   50,
		AutoK:         false,
		AutoKTau:      defaultAutoKTau,
		K:             0,
		TurdSize:      0,
		AlphaMax:      1.0,
		Optimize:      true,
		MaxDimensions: Dimensions{Width: 2048, Height: 2048},
		Workers:       0,
		Precision:     2,
		Photo:         false,
		PhotoDetail:   defaultPhotoDetail,
		PhotoEdge:     PhotoEdgeCrisp,
	}
}

// normalized returns a copy of c with zero-valued fields defaulted and clamps
// applied. It does not resolve K/TurdSize — those depend on the working pixel
// dimensions and are computed by resolveDetail.
func (c Config) normalized() Config {
	// Sensitivity: 0 is legal; only clamp.
	c.Sensitivity = clampInt(c.Sensitivity, 0, 100)

	// AlphaMax: bitrace re-clamps, but normalize NaN here for predictability.
	if math.IsNaN(c.AlphaMax) {
		c.AlphaMax = 1.0
	}
	c.AlphaMax = clampFloat(c.AlphaMax, 0, 1.334)

	if c.MaxDimensions.Width <= 0 {
		c.MaxDimensions.Width = 2048
	}
	if c.MaxDimensions.Height <= 0 {
		c.MaxDimensions.Height = 2048
	}

	if c.Workers <= 0 {
		c.Workers = runtime.NumCPU()
	}

	c.Precision = clampInt(c.Precision, 0, 6)

	// AutoKTau: zero value (and NaN) means "use the default"; clamp an
	// unreasonably large value to a sane maximum.
	if math.IsNaN(c.AutoKTau) || c.AutoKTau <= 0 {
		c.AutoKTau = defaultAutoKTau
	} else if c.AutoKTau > maxAutoKTau {
		c.AutoKTau = maxAutoKTau
	}

	// K < 0 is meaningless; fold to derive. TurdSize < 0 is preserved as the
	// force-disable sentinel.
	if c.K < 0 {
		c.K = 0
	}

	// PhotoDetail: zero value (and NaN) means "use the default"; then clamp to
	// the sane band. Inert when Photo is false, but resolved unconditionally so
	// a photo-mode Engine always sees a valid σ_r.
	if math.IsNaN(c.PhotoDetail) || c.PhotoDetail <= 0 {
		c.PhotoDetail = defaultPhotoDetail
	}
	c.PhotoDetail = clampFloat(c.PhotoDetail, minPhotoDetail, maxPhotoDetail)

	// PhotoEdge: clamp any out-of-range value to the crisp default. Inert when
	// Photo is false, but resolved unconditionally for predictability.
	if c.PhotoEdge != PhotoEdgeCrisp && c.PhotoEdge != PhotoEdgeStroke {
		c.PhotoEdge = PhotoEdgeCrisp
	}

	return c
}

// resolveDetail returns the effective (K, TurdSize) for a working image of
// W×H pixels, deriving from Sensitivity unless an explicit override is set.
func (c Config) resolveDetail(W, H int) (k, turd int) {
	if c.K > 0 {
		k = c.K
	} else {
		// Reference curve: 4·2^(S/25) => 4,8,16,32,64 at S=0,25,50,75,100.
		k = int(math.Round(4 * math.Pow(2, float64(c.Sensitivity)/25.0)))
	}
	k = clampInt(k, 2, maxKForPixels(W*H))

	switch {
	case c.TurdSize < 0:
		turd = 0
	case c.TurdSize > 0:
		turd = c.TurdSize
	default:
		// Reference curve: 8·2^(-S/25) => 8,4,2,1,0 at S=0..100.
		turd = int(math.Floor(8 * math.Pow(2, -float64(c.Sensitivity)/25.0)))
		if turd < 0 {
			turd = 0
		}
	}
	return k, turd
}

// turdForK returns the effective TurdSize for a given cluster count K, honouring
// an explicit [Config.TurdSize] override and otherwise deriving the speckle
// threshold from K alone (independent of Sensitivity). The derived value,
// floor(32/K), is the Sensitivity curve's K-to-noise coupling re-expressed
// through K: at K = 4, 8, 16, 32, 64 it yields 8, 4, 2, 1, 0 — matching the
// Sensitivity path exactly. Used for the [Config.AutoK] detail resolution.
func (c Config) turdForK(k int) int {
	switch {
	case c.TurdSize < 0:
		return 0
	case c.TurdSize > 0:
		return c.TurdSize
	default:
		if k < 1 {
			return 0
		}
		return 32 / k
	}
}

// maxKForPixels bounds K relative to resolution so clusters are not
// degenerately small on tiny images and memory stays bounded on large ones:
// clamp(px/1024, 2, 256).
func maxKForPixels(px int) int { return clampInt(px/1024, 2, 256) }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
