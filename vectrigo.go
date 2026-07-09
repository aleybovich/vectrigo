package vectrigo

import (
	"fmt"
	"image/color"
	"io"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/segment"
	"github.com/aleybovich/vectrigo/internal/assemble"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/pipeline"
	"github.com/aleybovich/vectrigo/internal/quantize"
	"github.com/aleybovich/vectrigo/internal/regionize"
	"github.com/aleybovich/vectrigo/internal/regiontrace"
)

// photoBoundarySmooth is the number of Laplacian smoothing iterations applied to
// the shared region-boundary corner graph in photo mode (see
// [regiontrace.Options.Smooth]). It is a tuned internal constant, not a public
// knob: 3 iterations relax the pixel staircase into smooth region edges while
// keeping the exact shared-boundary tiling that makes photo mode seam-free.
const photoBoundarySmooth = 3

// gaplessDenoiseSize and gaplessDenoiseMaxDist tune gapless mode's
// colour-conditional speckle absorption ([regionize.Options]): components
// smaller than gaplessDenoiseSize pixels are absorbed into their
// nearest-colour neighbour when that neighbour's palette colour is within
// gaplessDenoiseMaxDist (Euclidean RGB). They are tuned internal constants,
// not public knobs: quantizing photographic gradients scatters enormous
// numbers of 1-2px specks between near-identical adjacent clusters —
// invisible noise that can multiply the region count tenfold — while
// high-contrast specks (eye highlights, letter fragments) carry real detail.
// The size bound keeps this strictly a speckle pass and the colour bound
// keeps it visually near-lossless. Both bounds are deliberately conservative,
// validated on three content types: a busy painting (1.8x fewer paths; small
// faces, glasses and signage all survive), a synthetic low-contrast scene
// dense with fine detail (4.5x; single-pixel-stroke text of ~20 RGB contrast
// survives — at dist 35 it visibly erodes, at 60 it is destroyed), and a flat
// anti-aliased logo (16x, the same 32 paths at dist 25 as at 60: its noise is
// 1-2px fragments of the anti-aliasing bands, whose nearest neighbour is the
// adjacent band step, always colour-close). Larger sizes blotch
// small low-contrast features (a 60px face's wrinkles ARE 3-8px specks of
// adjacent skin clusters). An explicit negative Config.TurdSize (the
// "force-disable speckle removal" sentinel) turns the pass off.
const (
	gaplessDenoiseSize    = 3
	gaplessDenoiseMaxDist = 25
)

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
	if e.cfg.Gapless {
		return e.convertGapless(img, w)
	}

	b := img.NRGBA.Bounds()
	k, turd := e.resolveKTurd(b.Dx(), b.Dy(), img)

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

// resolveKTurd returns the effective colour count K and speckle threshold for
// the quantization-based pipelines (mask and gapless) on a W×H working image:
// the auto-K path when [Config.AutoK] is set without an explicit K override,
// the Sensitivity/K derivation otherwise. Both pipelines share it so a given
// configuration always quantizes identically regardless of the tracer.
func (e *Engine) resolveKTurd(W, H int, img normalize.Image) (k, turd int) {
	if e.cfg.AutoK && e.cfg.K <= 0 {
		// Auto-K: choose K from the image's colour complexity, ignoring
		// Sensitivity. An explicit K (> 0) is a hard override and takes the
		// resolveDetail path below instead. TurdSize is derived from the chosen
		// K (or an explicit override), never from Sensitivity.
		k = quantize.SelectK(img, maxKForPixels(W*H), e.cfg.AutoKTau)
		return k, e.cfg.turdForK(k)
	}
	return e.cfg.resolveDetail(W, H)
}

// convertGapless runs the GAPLESS quantization pipeline, the hybrid of the two
// others: colours come from the same k-means quantization as the mask pipeline
// (so Sensitivity / AutoK / K / TurdSize all apply and the posterized palette
// is identical), but tracing goes through photo mode's shared-boundary planar
// tracer instead of per-colour masks. The k-means label map is first split
// into 4-connected components with sub-speckle components absorbed into their
// longest-border neighbour ([regionize.Regionize] — the gapless analogue of
// bitrace's TurdSize), then the whole region map is traced as ONE planar
// subdivision ([regiontrace.Trace]) so adjacent shapes share exact boundary
// geometry: no seams between shapes, and every SVG path is one contiguous
// area rather than a whole colour scattered across the image. It is selected
// by [Config.Gapless] (Photo wins if both are set); with Gapless false the
// mask pipeline is untouched and output stays byte-identical.
func (e *Engine) convertGapless(img normalize.Image, w io.Writer) error {
	b := img.NRGBA.Bounds()
	k, turd := e.resolveKTurd(b.Dx(), b.Dy(), img)

	labels, palette, err := quantize.Labels(img, k)
	if err != nil {
		return fmt.Errorf("vectrigo: quantize: %w", err)
	}

	// The unconditional pass mirrors TurdSize; the colour-conditional denoise
	// pass collapses the low-contrast quantization dither that dominates the
	// region count on photographic content while keeping high-contrast specks
	// (see regionize). An explicit negative TurdSize — the historical
	// "force-disable speckle removal" sentinel — disables both.
	opt := regionize.Options{MinSize: turd, Palette: palette}
	if e.cfg.TurdSize >= 0 {
		opt.DenoiseSize = gaplessDenoiseSize
		opt.DenoiseMaxDist = gaplessDenoiseMaxDist
	}
	res := regionize.Regionize(labels, b.Dx(), b.Dy(), opt)

	// Region colours come straight from the cluster palette: same posterized
	// look as the mask pipeline, just split into per-area shapes.
	colors := make([]color.RGBA, res.NumRegions)
	for id, c := range res.Cluster {
		colors[id] = palette[c]
	}

	// Same boundary finish as photo mode: shared Laplacian smoothing relaxes
	// the pixel staircase, opt-in simplification collapses straight-edge node
	// runs, and both preserve the exact tiling (see regiontrace).
	traceOpt := regiontrace.Options{Smooth: photoBoundarySmooth, Simplify: e.cfg.PhotoSimplify}
	regions := regiontrace.Trace(res.Labels, b.Dx(), b.Dy(), res.NumRegions, traceOpt)

	crisp := e.cfg.PhotoEdge == PhotoEdgeCrisp
	if err := assemble.WriteRegions(w, regions, colors, res.Areas, img, crisp, assemble.Options{
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
	// DefaultOptions is the segment library's tuned baseline (K=60, MinSize=6,
	// SpatialSigma=2, BoundarySmooth=3); only the range-sigma detail dial is
	// exposed to callers, via PhotoDetail (already normalized/clamped).
	opt := segment.DefaultOptions()
	opt.RangeSigma = e.cfg.PhotoDetail

	res := segment.Segment(img.NRGBA, opt)
	colors := segment.MeanColors(img.NRGBA, res)

	// Trace the whole label map as one planar subdivision: adjacent regions share
	// exact boundary geometry, so filled regions tile the plane gapless — no
	// background is needed. Boundary simplification is the OPT-IN
	// node-count/fidelity dial [Config.PhotoSimplify]: 0 (the default) keeps
	// every corner; a positive tolerance collapses near-collinear runs.
	traceOpt := regiontrace.Options{Smooth: photoBoundarySmooth, Simplify: e.cfg.PhotoSimplify}
	regions := regiontrace.Trace(res.Labels, res.W, res.H, res.NumRegions, traceOpt)

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
