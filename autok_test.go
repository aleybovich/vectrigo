package vectrigo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

// stripePNG encodes a w×h image of len(cols) equal vertical stripes to PNG bytes.
func stripePNG(t *testing.T, w, h int, cols []color.NRGBA) []byte {
	t.Helper()
	nr := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			nr.SetNRGBA(x, y, cols[x*len(cols)/w])
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, nr); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

var autoKTestColors = []color.NRGBA{
	{R: 230, G: 20, B: 20, A: 255},
	{R: 20, G: 230, B: 20, A: 255},
	{R: 20, G: 20, B: 230, A: 255},
	{R: 230, G: 230, B: 20, A: 255},
	{R: 230, G: 20, B: 230, A: 255},
	{R: 20, G: 230, B: 230, A: 255},
}

func convertBytes(t *testing.T, src []byte, cfg Config) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Vectorize(bytes.NewReader(src), &out, cfg); err != nil {
		t.Fatalf("Vectorize: %v", err)
	}
	return out.Bytes()
}

// TestAutoKSensitivityIndependent proves that under AutoK the chosen K (and the
// full SVG output) is completely independent of Sensitivity: S=0 and S=100 must
// yield byte-identical documents.
func TestAutoKSensitivityIndependent(t *testing.T) {
	src := stripePNG(t, 240, 80, autoKTestColors)

	lo := DefaultConfig()
	lo.AutoK = true
	lo.Sensitivity = 0

	hi := DefaultConfig()
	hi.AutoK = true
	hi.Sensitivity = 100

	got0 := convertBytes(t, src, lo)
	got100 := convertBytes(t, src, hi)

	if !bytes.Equal(got0, got100) {
		t.Fatalf("AutoK output differs between Sensitivity 0 (%d bytes) and 100 (%d bytes); "+
			"Sensitivity must have no effect under AutoK", len(got0), len(got100))
	}
}

// TestAutoKDeterministic: AutoK on, same input twice => identical bytes.
func TestAutoKDeterministic(t *testing.T) {
	src := stripePNG(t, 240, 80, autoKTestColors)
	cfg := DefaultConfig()
	cfg.AutoK = true

	a := convertBytes(t, src, cfg)
	b := convertBytes(t, src, cfg)
	if !bytes.Equal(a, b) {
		t.Fatal("AutoK output is not deterministic across calls")
	}
}

// TestAutoKExplicitKWins: an explicit K (> 0) is a hard override that beats
// AutoK. AutoK=true,K=n must produce exactly what AutoK=false,K=n produces.
func TestAutoKExplicitKWins(t *testing.T) {
	src := stripePNG(t, 240, 80, autoKTestColors)

	withAuto := DefaultConfig()
	withAuto.AutoK = true
	withAuto.K = 3

	noAuto := DefaultConfig()
	noAuto.AutoK = false
	noAuto.K = 3

	if !bytes.Equal(convertBytes(t, src, withAuto), convertBytes(t, src, noAuto)) {
		t.Fatal("explicit K must override AutoK (outputs should match the fixed-K path)")
	}
}

// goldenShapesDefaultSHA256 is the sha256 of DefaultConfig()'s SVG output for
// testdata/shapes.png captured from the code base *before* the AutoK change.
// It guards the AutoK=false path against any byte-level regression.
const goldenShapesDefaultSHA256 = "39aac7a8e0c795fff4f61049757652559beb7011f8e883a9438613403c49436f"

func TestAutoKOffGoldenByteIdentical(t *testing.T) {
	src, err := os.ReadFile("testdata/shapes.png")
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig() // AutoK is false by default
	out := convertBytes(t, src, cfg)
	sum := sha256.Sum256(out)
	if got := hex.EncodeToString(sum[:]); got != goldenShapesDefaultSHA256 {
		t.Fatalf("AutoK=false output changed:\n got sha256 %s (%d bytes)\nwant sha256 %s",
			got, len(out), goldenShapesDefaultSHA256)
	}
}

// TestAutoKDefaultOff: the zero value and DefaultConfig both leave AutoK off.
func TestAutoKDefaultOff(t *testing.T) {
	if (Config{}).AutoK {
		t.Error("bare Config{} AutoK = true, want false")
	}
	if DefaultConfig().AutoK {
		t.Error("DefaultConfig AutoK = true, want false")
	}
}

// TestTurdForKIndependentOfSensitivity: under AutoK, TurdSize is derived from K
// only, never from Sensitivity, and it matches the Sensitivity curve at the
// curve's K values.
func TestTurdForKIndependentOfSensitivity(t *testing.T) {
	for _, s := range []int{0, 25, 50, 75, 100} {
		cfg := DefaultConfig()
		cfg.Sensitivity = s
		cfg = cfg.normalized()
		for _, tc := range []struct{ k, want int }{
			{4, 8}, {8, 4}, {16, 2}, {32, 1}, {64, 0}, {2, 16}, {6, 5},
		} {
			if got := cfg.turdForK(tc.k); got != tc.want {
				t.Errorf("S=%d turdForK(%d) = %d, want %d (must not depend on S)", s, tc.k, got, tc.want)
			}
		}
	}
}

// TestTurdForKOverrides: explicit TurdSize override still wins under the AutoK
// derivation path.
func TestTurdForKOverrides(t *testing.T) {
	pos := DefaultConfig()
	pos.TurdSize = 5
	if got := pos.turdForK(16); got != 5 {
		t.Errorf("explicit TurdSize=5 => %d, want 5", got)
	}
	neg := DefaultConfig()
	neg.TurdSize = -1
	if got := neg.turdForK(16); got != 0 {
		t.Errorf("TurdSize=-1 (disabled) => %d, want 0", got)
	}
}
