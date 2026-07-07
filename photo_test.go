package vectrigo

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// solidPNG encodes a w×h fully-opaque single-colour PNG for degenerate-input
// tests.
func solidPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4+0] = c.R
		img.Pix[i*4+1] = c.G
		img.Pix[i*4+2] = c.B
		img.Pix[i*4+3] = c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode solid PNG: %v", err)
	}
	return buf.Bytes()
}

// TestPhotoModeStreetMarket exercises the full library API in photo mode on the
// photographic fixture: it must emit a well-formed, non-empty SVG with a <rect>
// background and same-colour stroked region paths, at a plausible region count,
// deterministically.
func TestPhotoModeStreetMarket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy photo integration test in -short")
	}
	data := fixture(t, "street_market.png")

	cfg := DefaultConfig()
	cfg.Photo = true
	cfg.MaxDimensions = Dimensions{Width: 256, Height: 256} // small for speed

	var buf bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &buf, cfg); err != nil {
		t.Fatalf("Vectorize photo: %v", err)
	}
	out := buf.Bytes()

	// Well-formed XML, correct framing. 1024x559 fits 256x256 => 256x140.
	doc := parse(t, out)
	if doc.viewBox != "0 0 256 140" {
		t.Errorf("viewBox = %q, want \"0 0 256 140\"", doc.viewBox)
	}
	if doc.width != "1024" || doc.height != "559" {
		t.Errorf("width/height = %s/%s, want 1024/559", doc.width, doc.height)
	}

	s := string(out)
	if !strings.Contains(s, "<rect") {
		t.Error("photo SVG has no <rect background")
	}
	if !strings.Contains(s, "stroke=") {
		t.Error("photo SVG has no stroked paths")
	}
	if !strings.Contains(s, "stroke-width=") {
		t.Error("photo SVG has no stroke-width")
	}

	// Region count: a real photo fragments into many regions. The <rect> is one
	// non-path element; paths are the regions.
	if len(doc.ds) < 300 {
		t.Errorf("photo region path count = %d, want > 300", len(doc.ds))
	}

	// No NaN/Inf coordinates.
	for _, d := range doc.ds {
		low := strings.ToLower(d)
		if strings.Contains(low, "nan") || strings.Contains(low, "inf") {
			t.Errorf("coordinate contains NaN/Inf: %q", d)
		}
	}

	// Deterministic: a second run is byte-identical.
	var buf2 bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &buf2, cfg); err != nil {
		t.Fatalf("Vectorize photo (2nd): %v", err)
	}
	if !bytes.Equal(out, buf2.Bytes()) {
		t.Error("photo mode is not deterministic: two runs differ")
	}
}

// TestPhotoDetailInertWhenPhotoFalse asserts PhotoDetail has NO effect on output
// while Photo is false — the quantization path must ignore it entirely.
func TestPhotoDetailInertWhenPhotoFalse(t *testing.T) {
	data := fixture(t, "shapes.png")

	base := DefaultConfig()
	base.Photo = false

	cfgA := base
	cfgA.PhotoDetail = 5
	cfgB := base
	cfgB.PhotoDetail = 55

	var a, b bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &a, cfgA); err != nil {
		t.Fatalf("Vectorize A: %v", err)
	}
	if err := Vectorize(bytes.NewReader(data), &b, cfgB); err != nil {
		t.Fatalf("Vectorize B: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("PhotoDetail changed quantization output while Photo=false; it must be inert")
	}
	// And the output must carry no stroke (quantization path never strokes).
	if strings.Contains(a.String(), "stroke=") {
		t.Error("quantization output unexpectedly contains a stroke")
	}
}

// TestPhotoModeDegenerate feeds tiny and single-colour images through photo mode:
// it must not panic and must emit a well-formed <svg> with the background rect.
func TestPhotoModeDegenerate(t *testing.T) {
	cases := []struct {
		name string
		png  []byte
	}{
		{"1x1", solidPNG(t, 1, 1, color.NRGBA{R: 200, G: 50, B: 25, A: 255})},
		{"single_colour_8x8", solidPNG(t, 8, 8, color.NRGBA{R: 10, G: 120, B: 240, A: 255})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Photo = true
			var buf bytes.Buffer
			if err := Vectorize(bytes.NewReader(tc.png), &buf, cfg); err != nil {
				t.Fatalf("Vectorize degenerate photo: %v", err)
			}
			doc := parse(t, buf.Bytes()) // parse fails the test if not well-formed
			if doc.xmlns != "http://www.w3.org/2000/svg" {
				t.Errorf("xmlns = %q", doc.xmlns)
			}
			if !strings.Contains(buf.String(), "<rect") {
				t.Error("degenerate photo SVG has no <rect background")
			}
		})
	}
}
