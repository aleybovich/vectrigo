package vectrigo

import (
	"fmt"
	"io"

	"github.com/aleybovich/bitrace"
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

	b := img.NRGBA.Bounds()
	k, turd := e.cfg.resolveDetail(b.Dx(), b.Dy())

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
