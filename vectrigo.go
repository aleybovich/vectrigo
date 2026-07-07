package vectrigo

import (
	"fmt"
	"io"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/segment"
	"github.com/aleybovich/vectrigo/internal/assemble"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/pipeline"
	"github.com/aleybovich/vectrigo/internal/quantize"
	"github.com/aleybovich/vectrigo/internal/regiontrace"
)

// photoBoundarySmooth is the number of Laplacian smoothing iterations applied to
// the shared region-boundary corner graph in photo mode (see
// [regiontrace.Options.Smooth]). It is a tuned internal constant, not a public
// knob: 3 iterations relax the pixel staircase into smooth region edges while
// keeping the exact shared-boundary tiling that makes photo mode seam-free.
const photoBoundarySmooth = 3

// Vectorize reads a raster image (PNG/JPEG/WEBP) from r, converts it to SVG
// using cfg, and writes the SVG document to w. It is stateless and safe for
// concurrent use.
//
// Build cfg from [DefaultConfig] and adjust from there; a bare Config{} means
// Sensitivity 0 (maximum posterization), not the defaults.
func Vectorize(r io.Reader, w io.Writer, cfg Config) error {
	return NewEngine(cfg).Convert(r, w)
}

// Engine is a reusable, stateless converter. It holds only validated,
// immutable configuration and no per-call mutable state, so a single Engine
// may be shared across goroutines.
type Engine struct {
	cfg Config
}

// NewEngine returns an Engine with cfg's zero fields defaulted and its values
// clamped (see [Config]). The returned Engine is safe to share across
// goroutines.
func NewEngine(cfg Config) *Engine {
	return &Engine{cfg: cfg.normalized()}
}

// Convert runs the full four-stage pipeline: it reads a raster from r and
// writes the resulting SVG to w. All working state is local to the call.
//
// Errors are wrapped with a "vectrigo: <stage>: " prefix. Degenerate inputs
// (all-transparent or single-colour) are not errors: a well-formed <svg> with
// the correct dimensions is still written.
func (e *Engine) Convert(r io.Reader, w io.Writer) error {
	img, err := normalize.Decode(r, e.cfg.MaxDimensions.Width, e.cfg.MaxDimensions.Height)
	if err != nil {
		return fmt.Errorf("vectrigo: normalize: %w", err)
	}

	if e.cfg.Photo {
		return e.convertPhoto(img, w)
	}

	b := img.NRGBA.Bounds()
	var k, turd int
	if e.cfg.AutoK && e.cfg.K <= 0 {
		// Auto-K: choose K from the image's colour complexity, ignoring
		// Sensitivity. An explicit K (> 0) is a hard override and takes the
		// resolveDetail path below instead. TurdSize is derived from the chosen
		// K (or an explicit override), never from Sensitivity.
		k = quantize.SelectK(img, maxKForPixels(b.Dx()*b.Dy()), e.cfg.AutoKTau)
		turd = e.cfg.turdForK(k)
	} else {
		k, turd = e.cfg.resolveDetail(b.Dx(), b.Dy())
	}

	layers, err := quantize.Quantize(img, k)
	if err != nil {
		return fmt.Errorf("vectrigo: quantize: %w", err)
	}

	traceCfg := bitrace.Config{
		TurdSize: turd,
		AlphaMax: e.cfg.AlphaMax,
		Optimize: e.cfg.Optimize,
	}
	traced, err := pipeline.TraceLayers(layers, traceCfg, e.cfg.Workers)
	if err != nil {
		return fmt.Errorf("vectrigo: trace: %w", err)
	}

	if err := assemble.WriteSVG(w, traced, img, assemble.Options{
		Optimize:  e.cfg.Optimize,
		Precision: e.cfg.Precision,
	}); err != nil {
		return fmt.Errorf("vectrigo: assemble: %w", err)
	}
	return nil
}

// convertPhoto runs the region-first PHOTO pipeline: Felzenszwalb graph
// segmentation → per-region mean colour → whole-label-map planar trace (via
// [regiontrace.Trace], which shares boundary geometry between adjacent regions
// so they tile the plane with no seams) → stacked painter's assembly. It is
// selected by [Config.Photo]; the quantize path in [Engine.Convert] is left
// entirely untouched, so Photo=false output is byte-identical to the historical
// engine. Errors keep the "vectrigo: <stage>:" convention.
func (e *Engine) convertPhoto(img normalize.Image, w io.Writer) error {
	// DefaultOptions is the segment library's tuned baseline (K=100, MinSize=4,
	// SpatialSigma=2, BoundarySmooth=3); only the range-sigma detail dial is
	// exposed to callers, via PhotoDetail (already normalized/clamped).
	opt := segment.DefaultOptions()
	opt.RangeSigma = e.cfg.PhotoDetail

	res := segment.Segment(img.NRGBA, opt)
	colors := segment.MeanColors(img.NRGBA, res)

	// Trace the whole label map as one planar subdivision: adjacent regions share
	// exact boundary geometry, so filled regions tile the plane gapless — no
	// background is needed.
	regions := regiontrace.Trace(res.Labels, res.W, res.H, res.NumRegions, regiontrace.Options{Smooth: photoBoundarySmooth})

	// Per-region pixel area (region id -> pixel count) drives the largest-first
	// paint order in the assembler.
	areas := make([]int, res.NumRegions)
	for _, lb := range res.Labels {
		if lb >= 0 && lb < res.NumRegions {
			areas[lb]++
		}
	}

	crisp := e.cfg.PhotoEdge == PhotoEdgeCrisp
	if err := assemble.WriteRegions(w, regions, colors, areas, img, crisp, assemble.Options{
		Optimize:  e.cfg.Optimize,
		Precision: e.cfg.Precision,
	}); err != nil {
		return fmt.Errorf("vectrigo: assemble: %w", err)
	}
	return nil
}
