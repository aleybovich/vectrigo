package vectrigo

import (
	"math"
	"runtime"
)

// Dimensions is a width/height pair in pixels.
type Dimensions struct {
	Width  int
	Height int
}

// Config configures a vectorization. The primary knob is [Config.Sensitivity];
// the remaining fields are advanced overrides that default cleanly from zero.
//
// Build a Config from [DefaultConfig] and adjust from there. A bare Config{}
// means Sensitivity 0 (maximum posterization), NOT the recommended defaults —
// Sensitivity's zero is a legitimate setting and so cannot double as "unset".
type Config struct {
	// Sensitivity is the primary 0–100 detail dial (integer percent). It drives
	// the derived (K, TurdSize) pair: higher Sensitivity raises the colour
	// count K while easing TurdSize down. Clamped to [0,100].
	//
	// Because 0 is a real value (max posterization), a bare Config{} means
	// Sensitivity 0, not the default — start from DefaultConfig().
	Sensitivity int

	// K forces an exact cluster count, overriding the value derived from
	// Sensitivity. 0 means derive. When set it is clamped to
	// [2, maxKForPixels(W*H)] and never exceeds the image's distinct-colour
	// count.
	K int

	// TurdSize forces the speckle-area threshold in pixels, passed to bitrace.
	// 0 means derive from Sensitivity; a negative value force-disables speckle
	// removal (passes 0 to bitrace); a positive value is used as-is. (The sign
	// convention resolves the "0 = derive" vs "0 = disable" ambiguity.)
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
}

// DefaultConfig returns the recommended defaults and is the intended entry
// point: build from it, then tweak. Sensitivity is 50 (balanced); K and
// TurdSize are left 0 so they stay derived from Sensitivity.
func DefaultConfig() Config {
	return Config{
		Sensitivity:   50,
		K:             0,
		TurdSize:      0,
		AlphaMax:      1.0,
		Optimize:      true,
		MaxDimensions: Dimensions{Width: 2048, Height: 2048},
		Workers:       0,
		Precision:     2,
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

	// K < 0 is meaningless; fold to derive. TurdSize < 0 is preserved as the
	// force-disable sentinel.
	if c.K < 0 {
		c.K = 0
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
