package vectrigo

import (
	"bytes"
	"image/color"
	"strings"
	"testing"
)

// TestGaplessStreetMarket exercises the full library API in gapless mode on
// the photographic fixture. Like photo mode it must emit a well-formed SVG
// that tiles the plane (crispEdges, no background <rect>, fill-only paths);
// unlike photo mode its colours come from the k-means quantization, so at
// Sensitivity 50 the number of DISTINCT fills is bounded by the derived K
// while the path count is far larger (one path per connected area).
func TestGaplessStreetMarket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy gapless integration test in -short")
	}
	data := fixture(t, "street_market.png")

	cfg := DefaultConfig()
	cfg.Gapless = true
	cfg.MaxDimensions = Dimensions{Width: 256, Height: 256} // small for speed

	var buf bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &buf, cfg); err != nil {
		t.Fatalf("Vectorize gapless: %v", err)
	}
	out := buf.Bytes()

	doc := parse(t, out)
	if doc.viewBox != "0 0 256 140" {
		t.Errorf("viewBox = %q, want \"0 0 256 140\"", doc.viewBox)
	}
	if doc.width != "1024" || doc.height != "559" {
		t.Errorf("width/height = %s/%s, want 1024/559", doc.width, doc.height)
	}

	s := string(out)
	if !strings.Contains(s, `shape-rendering="crispEdges"`) {
		t.Error("crisp gapless SVG missing shape-rendering=\"crispEdges\"")
	}
	if strings.Contains(s, "<rect") {
		t.Error("gapless SVG unexpectedly has a <rect background (tiling is gapless)")
	}
	if strings.Contains(s, "stroke=") {
		t.Error("crisp gapless SVG unexpectedly contains a stroke")
	}

	// Quantized palette: distinct fills bounded by the Sensitivity-derived K
	// (16 at Sensitivity 50); contiguous shapes: far more paths than colours.
	distinct := map[string]bool{}
	for _, f := range doc.fills {
		distinct[f] = true
	}
	if len(distinct) > 16 {
		t.Errorf("distinct fill count = %d, want <= 16 (K at Sensitivity 50)", len(distinct))
	}
	if len(doc.ds) <= len(distinct) {
		t.Errorf("path count = %d with %d colours; want one path per connected area (more paths than colours)",
			len(doc.ds), len(distinct))
	}

	// Deterministic: a second run is byte-identical.
	var buf2 bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &buf2, cfg); err != nil {
		t.Fatalf("Vectorize gapless (2nd run): %v", err)
	}
	if !bytes.Equal(out, buf2.Bytes()) {
		t.Error("gapless output is not deterministic")
	}
}

// TestGaplessAutoK checks the AutoK + Gapless combination runs end-to-end and
// stays deterministic on the small flat fixture.
func TestGaplessAutoK(t *testing.T) {
	data := fixture(t, "squirrel.png")

	cfg := DefaultConfig()
	cfg.AutoK = true
	cfg.Gapless = true

	var a, b bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &a, cfg); err != nil {
		t.Fatalf("Vectorize auto-K gapless: %v", err)
	}
	if err := Vectorize(bytes.NewReader(data), &b, cfg); err != nil {
		t.Fatalf("Vectorize auto-K gapless (2nd run): %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("auto-K gapless output is not deterministic")
	}
	doc := parse(t, a.Bytes())
	if len(doc.ds) == 0 {
		t.Error("auto-K gapless produced no paths")
	}
}

// TestGaplessStrokeEdge checks PhotoEdge applies under Gapless: stroke mode
// seals seams with same-colour strokes and drops crispEdges.
func TestGaplessStrokeEdge(t *testing.T) {
	data := fixture(t, "squirrel.png")

	cfg := DefaultConfig()
	cfg.Gapless = true
	cfg.PhotoEdge = PhotoEdgeStroke

	var buf bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &buf, cfg); err != nil {
		t.Fatalf("Vectorize gapless stroke: %v", err)
	}
	s := buf.String()
	if strings.Contains(s, "crispEdges") {
		t.Error("stroke-edge gapless SVG unexpectedly contains crispEdges")
	}
	if !strings.Contains(s, "stroke=") {
		t.Error("stroke-edge gapless SVG has no strokes")
	}
}

// TestPhotoWinsOverGapless pins the precedence contract: with both Photo and
// Gapless set, output is byte-identical to Photo alone.
func TestPhotoWinsOverGapless(t *testing.T) {
	data := fixture(t, "squirrel.png")

	photoOnly := DefaultConfig()
	photoOnly.Photo = true
	both := photoOnly
	both.Gapless = true

	var a, b bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &a, photoOnly); err != nil {
		t.Fatalf("Vectorize photo: %v", err)
	}
	if err := Vectorize(bytes.NewReader(data), &b, both); err != nil {
		t.Fatalf("Vectorize photo+gapless: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("Photo+Gapless output differs from Photo alone; Photo must win")
	}
}

// TestGaplessOffIsUntouched pins the compatibility contract: Gapless false
// leaves the mask pipeline byte-identical whether or not the other
// region-tracing knobs are set.
func TestGaplessOffIsUntouched(t *testing.T) {
	data := fixture(t, "squirrel.png")

	base := DefaultConfig()
	tweaked := base
	tweaked.PhotoSimplify = PhotoSimplifyAggressive
	tweaked.PhotoEdge = PhotoEdgeStroke

	var a, b bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &a, base); err != nil {
		t.Fatalf("Vectorize base: %v", err)
	}
	if err := Vectorize(bytes.NewReader(data), &b, tweaked); err != nil {
		t.Fatalf("Vectorize tweaked: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("PhotoSimplify/PhotoEdge changed mask-pipeline output with Gapless and Photo both false")
	}
}

// TestGaplessDegenerate checks the degenerate inputs still produce well-formed
// SVG under Gapless: single-colour and all-transparent images.
func TestGaplessDegenerate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Gapless = true

	solid := solidPNG(t, 8, 6, color.NRGBA{R: 40, G: 90, B: 200, A: 255})
	var buf bytes.Buffer
	if err := Vectorize(bytes.NewReader(solid), &buf, cfg); err != nil {
		t.Fatalf("Vectorize solid gapless: %v", err)
	}
	doc := parse(t, buf.Bytes())
	if len(doc.ds) != 1 {
		t.Errorf("solid image path count = %d, want 1", len(doc.ds))
	}

	transparent := solidPNG(t, 8, 6, color.NRGBA{})
	buf.Reset()
	if err := Vectorize(bytes.NewReader(transparent), &buf, cfg); err != nil {
		t.Fatalf("Vectorize transparent gapless: %v", err)
	}
	doc = parse(t, buf.Bytes())
	if len(doc.ds) != 0 {
		t.Errorf("transparent image path count = %d, want 0", len(doc.ds))
	}
}
