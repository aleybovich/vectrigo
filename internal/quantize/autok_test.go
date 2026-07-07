package quantize

import (
	"image"
	"image/color"
	"testing"

	"github.com/aleybovich/vectrigo/internal/normalize"
)

// stripeImage builds a w×h NRGBA split into len(cols) equal vertical stripes,
// giving an image with exactly len(cols) distinct, well-separated colours.
func stripeImage(w, h int, cols []color.NRGBA) normalize.Image {
	nr := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			nr.SetNRGBA(x, y, cols[x*len(cols)/w])
		}
	}
	return normalize.Image{NRGBA: nr, OrigW: w, OrigH: h}
}

// gradientImage builds a w×h horizontal grey gradient from black to white,
// i.e. a high-complexity image with many distinct colours.
func gradientImage(w, h int) normalize.Image {
	nr := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(x * 255 / (w - 1))
			nr.SetNRGBA(x, y, color.NRGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return normalize.Image{NRGBA: nr, OrigW: w, OrigH: h}
}

// flatImage builds a w×h single-colour image.
func flatImage(w, h int) normalize.Image {
	nr := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range nr.Pix {
		nr.Pix[i] = 0
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			nr.SetNRGBA(x, y, color.NRGBA{R: 120, G: 60, B: 200, A: 255})
		}
	}
	return normalize.Image{NRGBA: nr, OrigW: w, OrigH: h}
}

var sixColors = []color.NRGBA{
	{R: 230, G: 20, B: 20, A: 255},  // red
	{R: 20, G: 230, B: 20, A: 255},  // green
	{R: 20, G: 20, B: 230, A: 255},  // blue
	{R: 230, G: 230, B: 20, A: 255}, // yellow
	{R: 230, G: 20, B: 230, A: 255}, // magenta
	{R: 20, G: 230, B: 230, A: 255}, // cyan
}

func TestSelectKMatchesRegionCount(t *testing.T) {
	img3 := stripeImage(120, 40, sixColors[:3])
	img6 := stripeImage(120, 40, sixColors)

	k3 := SelectK(img3, 64, 0.02)
	k6 := SelectK(img6, 64, 0.02)

	// Each should recover approximately its region count.
	if k3 < 3 || k3 > 4 {
		t.Errorf("SelectK(3-colour) = %d, want ~3", k3)
	}
	if k6 < 5 || k6 > 7 {
		t.Errorf("SelectK(6-colour) = %d, want ~6", k6)
	}
	// The more complex image must demand more clusters.
	if k6 <= k3 {
		t.Errorf("SelectK 6-colour (%d) must exceed 3-colour (%d)", k6, k3)
	}
}

func TestSelectKGradientExceedsFlat(t *testing.T) {
	flat := SelectK(flatImage(128, 40), 64, 0.02)
	grad := SelectK(gradientImage(128, 40), 64, 0.02)

	if flat > 2 {
		t.Errorf("flat image SelectK = %d, want small (<=2)", flat)
	}
	if grad <= flat {
		t.Errorf("gradient SelectK (%d) must exceed flat (%d)", grad, flat)
	}
}

func TestSelectKClampToMaxK(t *testing.T) {
	// A complex gradient but a tight maxK ceiling: never exceed it.
	if got := SelectK(gradientImage(256, 40), 5, 0.02); got > 5 {
		t.Errorf("SelectK = %d, must be clamped to maxK=5", got)
	}
	// maxK below the [2, .] floor still yields a sane, in-range value.
	if got := SelectK(gradientImage(256, 40), 2, 0.02); got != 2 {
		t.Errorf("SelectK with maxK=2 = %d, want 2", got)
	}
}

func TestSelectKDistinctColourClamp(t *testing.T) {
	// Only two distinct colours present: cannot exceed 2 regardless of maxK.
	img := stripeImage(64, 16, sixColors[:2])
	if got := SelectK(img, 64, 0.02); got > 2 {
		t.Errorf("SelectK on 2-colour image = %d, want <= 2", got)
	}
}

func TestSelectKDeterministic(t *testing.T) {
	img := stripeImage(120, 40, sixColors)
	a := SelectK(img, 64, 0.02)
	b := SelectK(img, 64, 0.02)
	if a != b {
		t.Fatalf("SelectK not deterministic: %d vs %d", a, b)
	}
}

// TestSelectKTauReducesK proves that raising tau trips the distortion knee
// earlier, yielding a K that is <= the K at a smaller tau (and strictly smaller
// for a complex image). This is the mechanism that lets complex photos, which
// otherwise saturate at the scan ceiling, differentiate into smaller K values.
func TestSelectKTauReducesK(t *testing.T) {
	// Complex photo fixture (downsampled): at the default tau=0.02 the distortion
	// never reaches the knee, so K pegs at the scan ceiling; a larger tau trips
	// the knee well below it. See the empirically observed values in comments.
	img := loadFixture(t, "street_market.png", 256)
	kLow := SelectK(img, 64, 0.02)  // observed: 64 (ceiling)
	kHigh := SelectK(img, 64, 0.08) // observed: 13
	if kLow != 64 {
		t.Errorf("street_market at tau=0.02 K=%d, want ceiling 64", kLow)
	}
	if kHigh >= kLow {
		t.Errorf("raising tau must reduce K: tau=0.08 K=%d must be < tau=0.02 K=%d", kHigh, kLow)
	}
	if kHigh < 2 {
		t.Errorf("tau=0.08 K=%d, want a sane in-range value", kHigh)
	}

	// Monotone (non-increasing) direction across an ascending tau sweep on a
	// gradient: larger tau never demands more clusters.
	grad := gradientImage(256, 40)
	prev := SelectK(grad, 64, 0.02)
	for _, tau := range []float64{0.05, 0.08, 0.15} {
		cur := SelectK(grad, 64, tau)
		if cur > prev {
			t.Errorf("gradient: raising tau to %.2f raised K to %d (was %d); must not increase", tau, cur, prev)
		}
		prev = cur
	}
}

func TestSelectKAllTransparent(t *testing.T) {
	nr := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for i := range nr.Pix {
		nr.Pix[i] = 0
	}
	img := normalize.Image{NRGBA: nr, OrigW: 16, OrigH: 16}
	if got := SelectK(img, 64, 0.02); got != 2 {
		t.Errorf("SelectK all-transparent = %d, want 2", got)
	}
}
