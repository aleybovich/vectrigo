package vectrigo

import (
	"fmt"
	"image"
	"image/color"
	"io"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/segment"
	"github.com/aleybovich/vectrigo/internal/assemble"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/pipeline"
	"github.com/aleybovich/vectrigo/internal/quantize"
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

// convertPhoto runs the region-first PHOTO pipeline (Stage II+III+IV replaced by
// Felzenszwalb graph segmentation → per-region mean colour → per-region trace →
// stacked painter's assembly). It is selected by [Config.Photo]; the quantize
// path in [Engine.Convert] is left entirely untouched, so Photo=false output is
// byte-identical to the historical engine. Errors keep the "vectrigo: <stage>:"
// convention.
func (e *Engine) convertPhoto(img normalize.Image, w io.Writer) error {
	// DefaultOptions is the segment library's tuned baseline (K=100, MinSize=4,
	// SpatialSigma=2, BoundarySmooth=3); only the range-sigma detail dial is
	// exposed to callers, via PhotoDetail (already normalized/clamped).
	opt := segment.DefaultOptions()
	opt.RangeSigma = e.cfg.PhotoDetail

	res := segment.Segment(img.NRGBA, opt)
	colors := segment.MeanColors(img.NRGBA, res)

	b := img.NRGBA.Bounds()
	traceCfg := bitrace.Config{
		// TurdSize 0: regions are already size-floored by segment's MinSize, so no
		// additional speckle removal. AlphaMax/Optimize honour the shared config.
		TurdSize: 0,
		AlphaMax: e.cfg.AlphaMax,
		Optimize: e.cfg.Optimize,
	}
	regions, err := pipeline.TraceRegions(res.Labels, res.NumRegions, b.Dx(), b.Dy(), colors, traceCfg, e.cfg.Workers)
	if err != nil {
		return fmt.Errorf("vectrigo: trace: %w", err)
	}

	meanBG := meanOpaqueColor(img.NRGBA)
	if err := assemble.WriteSegmented(w, regions, img, meanBG, assemble.Options{
		Optimize:  e.cfg.Optimize,
		Precision: e.cfg.Precision,
	}); err != nil {
		return fmt.Errorf("vectrigo: assemble: %w", err)
	}
	return nil
}

// meanOpaqueColor returns the mean R,G,B of img's opaque pixels (alpha >= 128,
// matching segment's opacity threshold), as an opaque colour. It is the full-
// canvas backdrop that seals sub-pixel seams in photo mode. The computation is
// deterministic (fixed row-major traversal, integer rounding half-up). An image
// with no opaque pixels yields opaque black — the backdrop is then cosmetic
// since there are no regions to seam.
func meanOpaqueColor(img *image.NRGBA) color.RGBA {
	pix := img.Pix
	var sumR, sumG, sumB, cnt uint64
	for o := 0; o+3 < len(pix); o += 4 {
		if pix[o+3] < 128 {
			continue
		}
		sumR += uint64(pix[o])
		sumG += uint64(pix[o+1])
		sumB += uint64(pix[o+2])
		cnt++
	}
	if cnt == 0 {
		return color.RGBA{A: 255}
	}
	return color.RGBA{
		R: uint8((sumR + cnt/2) / cnt),
		G: uint8((sumG + cnt/2) / cnt),
		B: uint8((sumB + cnt/2) / cnt),
		A: 255,
	}
}
