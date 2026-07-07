package vectrigo

import (
	"bytes"
	"sort"
	"testing"

	"image"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/vectrigo/internal/imageutil"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/pipeline"
	"github.com/aleybovich/vectrigo/internal/quantize"
	"golang.org/x/image/vector"
)

// TestRoundTripStreetMarket rasterizes the *traced* paths directly (pure Go,
// approved deps only) and compares the reconstruction against the downsampled
// source. Vectorization is lossy, so this is a regression tripwire for gross
// breakage (wrong z-order, dropped layers, mis-mapped coordinates), not a
// fidelity gate; the bound is deliberately loose.
func TestRoundTripStreetMarket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping heavy round-trip test in -short")
	}
	data := fixture(t, "street_market.png")

	cfg := DefaultConfig()
	cfg.MaxDimensions = Dimensions{Width: 384, Height: 384}
	nc := cfg.normalized()

	img, err := normalize.Decode(bytes.NewReader(data), nc.MaxDimensions.Width, nc.MaxDimensions.Height)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	b := img.NRGBA.Bounds()
	W, H := b.Dx(), b.Dy()

	k, turd := nc.resolveDetail(W, H)
	layers, err := quantize.Quantize(img, k)
	if err != nil {
		t.Fatalf("quantize: %v", err)
	}
	traced, err := pipeline.TraceLayers(layers, bitrace.Config{
		TurdSize: turd, AlphaMax: nc.AlphaMax, Optimize: nc.Optimize,
	}, nc.Workers)
	if err != nil {
		t.Fatalf("trace: %v", err)
	}

	// Same z-order as assemble: largest area first, painted first (behind).
	sort.SliceStable(traced, func(i, j int) bool {
		if traced[i].Area != traced[j].Area {
			return traced[i].Area > traced[j].Area
		}
		return imageutil.Pack(traced[i].Color) < imageutil.Pack(traced[j].Color)
	})

	canvas := image.NewRGBA(image.Rect(0, 0, W, H))
	for _, tr := range traced {
		if len(tr.Paths) == 0 {
			continue
		}
		// One rasterizer per colour so outer + hole windings cancel (nonzero),
		// mirroring the one-path-per-colour SVG construction.
		rz := vector.NewRasterizer(W, H)
		for _, p := range tr.Paths {
			for _, c := range p.Commands {
				switch c.Kind {
				case bitrace.MoveTo:
					rz.MoveTo(float32(c.P.X), float32(c.P.Y))
				case bitrace.LineTo:
					rz.LineTo(float32(c.P.X), float32(c.P.Y))
				case bitrace.CubicTo:
					rz.CubeTo(float32(c.C1.X), float32(c.C1.Y),
						float32(c.C2.X), float32(c.C2.Y),
						float32(c.P.X), float32(c.P.Y))
				case bitrace.Close:
					rz.ClosePath()
				}
			}
		}
		src := image.NewUniform(tr.Color)
		rz.Draw(canvas, canvas.Bounds(), src, image.Point{})
	}

	// Mean absolute per-channel difference against the source, over opaque
	// source pixels.
	var sum, count float64
	pix := img.NRGBA.Pix
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			o := (y*W + x) * 4
			if pix[o+3] < 128 {
				continue
			}
			cr, cg, cb, _ := canvas.RGBAAt(x, y).RGBA()
			sum += absDiff(int(pix[o]), int(cr>>8))
			sum += absDiff(int(pix[o+1]), int(cg>>8))
			sum += absDiff(int(pix[o+2]), int(cb>>8))
			count += 3
		}
	}
	mean := sum / count
	t.Logf("mean absolute per-channel difference: %.2f / 255 (%.1f%%)", mean, 100*mean/255)

	// Loose bound: a 16-colour posterization of a rich photo. Observed ~16/255
	// (~6.3%); 32/255 (~12.5%) leaves ample margin and catches only gross
	// breakage (wrong z-order, dropped layers, mis-mapped coordinates).
	const bound = 32.0
	if mean > bound {
		t.Errorf("mean abs diff %.2f exceeds bound %.0f/255", mean, bound)
	}
}

func absDiff(a, b int) float64 {
	if a > b {
		return float64(a - b)
	}
	return float64(b - a)
}
