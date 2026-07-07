package vectrigo

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aleybovich/vectrigo/internal/imageutil"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/quantize"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// svgDoc is a minimal parsed view of an emitted SVG for assertions.
type svgDoc struct {
	xmlns, width, height, viewBox string
	fills                         []string
	ds                            []string
}

func parse(t *testing.T, b []byte) svgDoc {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(b))
	var d svgDoc
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("SVG is not well-formed XML: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "svg":
			d.xmlns = se.Name.Space
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "width":
					d.width = a.Value
				case "height":
					d.height = a.Value
				case "viewBox":
					d.viewBox = a.Value
				}
			}
		case "path":
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "fill":
					d.fills = append(d.fills, a.Value)
				case "d":
					d.ds = append(d.ds, a.Value)
				}
			}
		}
	}
	return d
}

// palette returns the set of #rrggbb centroid colours the engine would compute
// for the given input under cfg (used to assert emitted fills come from the
// engine's own palette).
func palette(t *testing.T, data []byte, cfg Config) map[string]bool {
	t.Helper()
	nc := cfg.normalized()
	img, err := normalize.Decode(bytes.NewReader(data), nc.MaxDimensions.Width, nc.MaxDimensions.Height)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	b := img.NRGBA.Bounds()
	k, _ := nc.resolveDetail(b.Dx(), b.Dy())
	layers, err := quantize.Quantize(img, k)
	if err != nil {
		t.Fatalf("quantize: %v", err)
	}
	set := make(map[string]bool)
	for _, l := range layers {
		set[imageutil.Hex(l.Color)] = true
	}
	return set
}

func TestEndToEndAllDecoders(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Sensitivity = 50

	for _, name := range []string{"shapes.png", "shapes.jpg", "shapes.webp"} {
		t.Run(name, func(t *testing.T) {
			data := fixture(t, name)

			var buf bytes.Buffer
			if err := Vectorize(bytes.NewReader(data), &buf, cfg); err != nil {
				t.Fatalf("Vectorize: %v", err)
			}
			doc := parse(t, buf.Bytes())

			if doc.xmlns != "http://www.w3.org/2000/svg" {
				t.Errorf("xmlns = %q", doc.xmlns)
			}
			if doc.width != "96" || doc.height != "64" {
				t.Errorf("width/height = %s/%s, want 96/64", doc.width, doc.height)
			}
			if doc.viewBox != "0 0 96 64" {
				t.Errorf("viewBox = %q, want \"0 0 96 64\"", doc.viewBox)
			}

			// Effective K for a 96x64 image (clamped by maxKForPixels).
			k, _ := cfg.normalized().resolveDetail(96, 64)
			if len(doc.fills) < 1 || len(doc.fills) > k {
				t.Errorf("path count = %d, want in [1,%d]", len(doc.fills), k)
			}

			for _, d := range doc.ds {
				if !strings.HasPrefix(d, "M") {
					t.Errorf("d does not start with M: %q", d)
				}
				if !strings.HasSuffix(d, "Z") {
					t.Errorf("d does not end with Z: %q", d)
				}
			}

			pal := palette(t, data, cfg)
			for _, f := range doc.fills {
				if len(f) != 7 || f[0] != '#' {
					t.Errorf("fill %q is not #rrggbb", f)
				}
				if !pal[f] {
					t.Errorf("fill %q not one of the engine's centroids", f)
				}
			}
		})
	}
}

func TestEndToEndDeterministic(t *testing.T) {
	data := fixture(t, "shapes.png")
	cfg := DefaultConfig()

	var a, b bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &a, cfg); err != nil {
		t.Fatal(err)
	}
	if err := Vectorize(bytes.NewReader(data), &b, cfg); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("output is not byte-identical across runs (determinism broken)")
	}
}

func TestEngineShareableConcurrent(t *testing.T) {
	data := fixture(t, "shapes.png")
	eng := NewEngine(DefaultConfig())

	// Reference single-run output.
	var ref bytes.Buffer
	if err := eng.Convert(bytes.NewReader(data), &ref); err != nil {
		t.Fatal(err)
	}

	const g = 8
	results := make([][]byte, g)
	errs := make([]error, g)
	done := make(chan int, g)
	for i := 0; i < g; i++ {
		go func(i int) {
			var buf bytes.Buffer
			errs[i] = eng.Convert(bytes.NewReader(data), &buf)
			results[i] = buf.Bytes()
			done <- i
		}(i)
	}
	for i := 0; i < g; i++ {
		<-done
	}
	for i := 0; i < g; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if !bytes.Equal(results[i], ref.Bytes()) {
			t.Fatalf("goroutine %d produced different output (engine not concurrency-safe)", i)
		}
	}
}

func TestErrorCases(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		err := Vectorize(bytes.NewReader(nil), io.Discard, DefaultConfig())
		if err == nil || !strings.Contains(err.Error(), "vectrigo: normalize") {
			t.Fatalf("err = %v, want wrapped normalize error", err)
		}
	})
	t.Run("corrupt", func(t *testing.T) {
		err := Vectorize(strings.NewReader("\x89PNG\r\n\x1a\nnot really a png"), io.Discard, DefaultConfig())
		if err == nil {
			t.Fatal("expected error for corrupt PNG")
		}
		if !strings.Contains(err.Error(), "vectrigo: normalize") {
			t.Errorf("err = %v, want wrapped normalize error", err)
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		err := Vectorize(strings.NewReader("GIF89a-ish plain text not an image"), io.Discard, DefaultConfig())
		if err == nil {
			t.Fatal("expected error for unsupported data")
		}
	})
}

// TestZeroValueConfigSemantics verifies a bare Config{} runs (Sensitivity 0 =
// max posterization) and still yields a valid SVG.
func TestZeroValueConfigSemantics(t *testing.T) {
	data := fixture(t, "shapes.png")
	var buf bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &buf, Config{}); err != nil {
		t.Fatalf("Vectorize(Config{}): %v", err)
	}
	doc := parse(t, buf.Bytes())
	if doc.viewBox != "0 0 96 64" {
		t.Errorf("viewBox = %q", doc.viewBox)
	}
	// Sensitivity 0 => derived K = 4 (clamped by maxKForPixels(6144)=6, so 4).
	if len(doc.fills) < 1 || len(doc.fills) > 4 {
		t.Errorf("path count = %d, want in [1,4] at Sensitivity 0", len(doc.fills))
	}
}

func TestHeavyIntegrationStreetMarket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy integration test in -short")
	}
	data := fixture(t, "street_market.png")

	cfg := DefaultConfig()
	cfg.MaxDimensions = Dimensions{Width: 512, Height: 512} // downscale for speed

	var buf bytes.Buffer
	if err := Vectorize(bytes.NewReader(data), &buf, cfg); err != nil {
		t.Fatalf("Vectorize: %v", err)
	}
	doc := parse(t, buf.Bytes())

	// Working dims: 1024x559 fit into 512x512 => 512x280.
	if doc.viewBox != "0 0 512 280" {
		t.Errorf("viewBox = %q, want \"0 0 512 280\"", doc.viewBox)
	}
	if doc.width != "1024" || doc.height != "559" {
		t.Errorf("width/height = %s/%s, want 1024/559", doc.width, doc.height)
	}

	// Path count within a broad sane band. Real photos fragment: [K/2, 40K].
	k, _ := cfg.normalized().resolveDetail(512, 280)
	if len(doc.fills) < 1 || len(doc.fills) > 40*k {
		t.Errorf("path count = %d, outside sane band for K=%d", len(doc.fills), k)
	}

	// No NaN/Inf in any coordinate.
	for _, d := range doc.ds {
		low := strings.ToLower(d)
		if strings.Contains(low, "nan") || strings.Contains(low, "inf") {
			t.Errorf("coordinate contains NaN/Inf: %q", d)
		}
	}
}
