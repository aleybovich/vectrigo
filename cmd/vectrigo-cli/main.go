// Command vectrigo-cli vectorizes a raster image (PNG/JPEG/WEBP) into an SVG
// file using the vectrigo engine.
//
// Usage:
//
//	vectrigo-cli -i <image-path> -s <sensitivity>
//	vectrigo-cli -i <image-path> --auto-k
//	vectrigo-cli -i <image-path> --photo [--sigma <n>] [--simplify <subtle|aggressive>] [--edge <crisp|stroke>]
//
// The input image is given with --input/-i and is required. It must be a PNG,
// JPEG, or WEBP raster image.
//
// The mode is chosen in one of three mutually exclusive ways:
//
//   - --sensitivity/-s takes an integer in [0,100] controlling the primary
//     detail knob for the default colour-quantization pipeline (see
//     [vectrigo.Config.Sensitivity]); higher values produce more detail. Best
//     for flat / logo art.
//   - --auto-k lets the engine choose the colour count K automatically from the
//     image (see [vectrigo.Config.AutoK]); no sensitivity is then used. Still
//     the quantization pipeline.
//   - --photo selects the segmentation PHOTO pipeline (see [vectrigo.Config.Photo]),
//     which partitions the image into small spatially-connected regions and is
//     best for photographic content. Its detail dial is --sigma (the bilateral
//     σ_r "detail" knob, see [vectrigo.Config.PhotoDetail]): ~8 punchy, 12 the
//     default, 28+ soft. --sigma is only valid together with --photo. Its
//     node-count/fidelity dial is --simplify (see
//     [vectrigo.Config.PhotoSimplify]), an OPT-IN boundary simplification:
//     unset means OFF — maximum fidelity, every boundary corner kept, at the
//     cost of many nodes on straight edges. "subtle" (0.35px tolerance) is
//     visually near-lossless while collapsing straight-edge node runs;
//     "aggressive" (1px) produces the smallest files at visibly coarser
//     shapes. Seams never open at any setting. --simplify is only valid
//     together with --photo. Its edge finish is --edge (see [vectrigo.Config.PhotoEdge]):
//     "crisp" (the default) disables edge anti-aliasing for a seam-free
//     flat-vector look, "stroke" keeps anti-aliasing and seals the sub-pixel
//     seams with a thin same-colour stroke. --edge is only valid together with
//     --photo.
//
// Exactly one of --sensitivity, --auto-k, or --photo must be supplied. Passing
// more than one, or none, is an error; so is --sigma, --simplify or --edge
// without --photo.
//
// The resulting SVG is written next to the input file. The output name depends
// on the mode:
//
//   - With a sensitivity, the input's extension is replaced by
//     ".<sensitivity>.svg". For example, "photos/street.png" at sensitivity 70
//     produces "photos/street.70.svg".
//   - With --auto-k, the input's extension is replaced by ".svg" (no
//     sensitivity segment). For example, "photos/street.png" produces
//     "photos/street.svg".
//   - With --photo, the input's extension is replaced by ".photo.svg". For
//     example, "photos/street.png" produces "photos/street.photo.svg".
package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/aleybovich/vectrigo"
)

const usage = `Usage:
  vectrigo-cli -i <image-path> -s <sensitivity>
  vectrigo-cli -i <image-path> --auto-k
  vectrigo-cli -i <image-path> --photo [--sigma <n>] [--simplify <subtle|aggressive>] [--edge <crisp|stroke>]

Vectorize a raster image (PNG/JPEG/WEBP) into an SVG file.

Options:
  -i, --input <path>          Path to the input raster image (PNG, JPEG, or WEBP). Required.
  -s, --sensitivity <0-100>   Integer detail knob for quantization (higher = more detail).
      --auto-k                Auto-select the colour count K from the image (no sensitivity).
      --photo                 Segmentation photo mode, for photographic images.
      --sigma <n>             Photo-mode detail dial (bilateral σ_r): ~8 punchy, 12 default,
                              28+ soft. Only valid with --photo; unset uses the default (12).
      --simplify <strength>   Photo-mode boundary simplification (opt-in; unset = OFF,
                              maximum fidelity). "subtle" is visually near-lossless with
                              far fewer nodes on straight edges; "aggressive" gives the
                              smallest files at visibly coarser shapes. Seams never open
                              at any setting. Only valid with --photo.
      --edge <crisp|stroke>   Photo-mode edge finish: crisp (default) disables edge
                              anti-aliasing for a seam-free flat look; stroke keeps
                              anti-aliasing and seals seams with a thin same-colour stroke.
                              Only valid with --photo; unset uses the default (crisp).
  -h, --help                  Show this help message.

Modes (mutually exclusive):
  Supply --sensitivity for a fixed quantization detail level (best for flat / logo
  art), OR --auto-k to let the engine choose the colour count K automatically, OR
  --photo for the segmentation pipeline (best for photographic images). Exactly one
  is required: passing more than one, or none, is an error. --sigma, --simplify and
  --edge require --photo.

The output SVG is written next to the input file:
  - with a sensitivity, the input's extension is replaced by ".<sensitivity>.svg".
  - with --auto-k, the input's extension is replaced by ".svg".
  - with --photo, the input's extension is replaced by ".photo.svg".

Examples:
  vectrigo-cli -i photos/street.png -s 70        =>  photos/street.70.svg
  vectrigo-cli -i photos/street.png --auto-k     =>  photos/street.svg
  vectrigo-cli -i photos/street.png --photo      =>  photos/street.photo.svg
  vectrigo-cli -i photos/street.png --photo --sigma 8  =>  photos/street.photo.svg
  vectrigo-cli -i photos/street.png --photo --simplify subtle  =>  photos/street.photo.svg
  vectrigo-cli -i photos/street.png --photo --edge stroke  =>  photos/street.photo.svg
`

// outputPath derives the output SVG path for a given input image path and
// sensitivity value: the input's directory and base name (with its final
// extension stripped), followed by ".<sensitivity>.svg".
func outputPath(inputPath string, sensitivity int) string {
	dir := filepath.Dir(inputPath)
	base := filepath.Base(inputPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, fmt.Sprintf("%s.%d.svg", stem, sensitivity))
}

// autoOutputPath derives the output SVG path for a given input image path in
// auto-K mode: the input's directory and base name (with its final extension
// stripped), followed by ".svg" (no sensitivity segment).
func autoOutputPath(inputPath string) string {
	dir := filepath.Dir(inputPath)
	base := filepath.Base(inputPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, stem+".svg")
}

// photoOutputPath derives the output SVG path for a given input image path in
// photo (segmentation) mode: the input's directory and base name (with its
// final extension stripped), followed by ".photo.svg".
func photoOutputPath(inputPath string) string {
	dir := filepath.Dir(inputPath)
	base := filepath.Base(inputPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, stem+".photo.svg")
}

// validateSensitivity checks that v is a valid sensitivity in [0,100].
func validateSensitivity(v int) error {
	if v < 0 || v > 100 {
		return fmt.Errorf("sensitivity must be in [0,100], got %d", v)
	}
	return nil
}

// run implements the CLI: it parses args, runs the vectorization, and reports
// the outcome via stdout/stderr. It returns a non-nil error on any failure.
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("vectrigo-cli", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// Print our own usage instead of flag's auto-generated one on error.
	fs.Usage = func() { fmt.Fprint(stderr, usage) }

	var (
		inputPath   string
		sensitivity int
		autoK       bool
		photo       bool
		sigma       float64
		simplify    string
		edge        string
		help        bool
	)

	// Register long and short names for each flag against the same variable,
	// the standard Go idiom for short/long aliases.
	fs.StringVar(&inputPath, "input", "", "path to the input raster image (required)")
	fs.StringVar(&inputPath, "i", "", "path to the input raster image (required) (shorthand)")
	// Default sensitivity of -1 lets us detect whether the flag was supplied
	// without conflating it with the valid value 0.
	fs.IntVar(&sensitivity, "sensitivity", -1, "integer detail knob in [0,100]")
	fs.IntVar(&sensitivity, "s", -1, "integer detail knob in [0,100] (shorthand)")
	fs.BoolVar(&autoK, "auto-k", false, "auto-select the colour count K from the image")
	fs.BoolVar(&photo, "photo", false, "segmentation photo mode, for photographic images")
	// Default of NaN lets us detect whether --sigma was supplied without
	// conflating it with any in-band value; an unset --sigma leaves PhotoDetail
	// at the engine default.
	fs.Float64Var(&sigma, "sigma", math.NaN(), "photo-mode detail dial (bilateral σ_r)")
	// Default "" lets us detect whether --simplify was supplied; an unset
	// --simplify leaves PhotoSimplify at the engine default: OFF.
	fs.StringVar(&simplify, "simplify", "", "photo-mode boundary simplification: subtle or aggressive (unset = off)")
	// Default "" lets us detect whether --edge was supplied; an unset --edge
	// leaves PhotoEdge at the engine default (crisp).
	fs.StringVar(&edge, "edge", "", "photo-mode edge finish: crisp (default) or stroke")
	fs.BoolVar(&help, "help", false, "show this help message")
	fs.BoolVar(&help, "h", false, "show this help message (shorthand)")

	if err := fs.Parse(args); err != nil {
		// flag already wrote its error and our usage to stderr.
		return err
	}

	if help {
		fmt.Fprint(stdout, usage)
		return nil
	}

	// Reject stray positional arguments rather than silently ignoring them:
	// flag parsing stops at the first non-flag token, so a trailing typo would
	// otherwise be discarded (or, if it precedes a flag, swallow that flag).
	if fs.NArg() > 0 {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("unexpected argument(s): %v (all options are named, e.g. -i <path> -s <n>)", fs.Args())
	}

	// Whether --sensitivity/-s and --sigma were actually supplied on the command
	// line (both use sentinel defaults, so a value alone can't tell us).
	sensitivitySet := false
	sigmaSet := false
	simplifySet := false
	edgeSet := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "sensitivity", "s":
			sensitivitySet = true
		case "sigma":
			sigmaSet = true
		case "simplify":
			simplifySet = true
		case "edge":
			edgeSet = true
		}
	})

	if inputPath == "" {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("an input image (-i) is required")
	}

	// --sigma only tunes photo mode; it is meaningless without --photo.
	if sigmaSet && !photo {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("--sigma requires --photo")
	}

	// --simplify only tunes photo mode; it is meaningless without --photo.
	if simplifySet && !photo {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("--simplify requires --photo")
	}

	// --edge only tunes photo mode; it is meaningless without --photo.
	if edgeSet && !photo {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("--edge requires --photo")
	}

	// Exactly one of the three modes must be selected.
	modes := 0
	if sensitivitySet {
		modes++
	}
	if autoK {
		modes++
	}
	if photo {
		modes++
	}
	if modes > 1 {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("--sensitivity, --auto-k and --photo are mutually exclusive; choose exactly one")
	}
	if modes == 0 {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("a mode is required: pass one of --sensitivity (-s), --auto-k, or --photo")
	}

	if sensitivitySet {
		if err := validateSensitivity(sensitivity); err != nil {
			return err
		}
	}

	// Reject an obviously invalid non-finite σ_r early with a clear message; a
	// finite (even out-of-band) value is left for the engine to clamp.
	if photo && sigmaSet && (math.IsNaN(sigma) || math.IsInf(sigma, 0)) {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("--sigma must be a finite number")
	}

	// --simplify accepts only the two named strengths; leaving it unset means
	// no simplification at all.
	if photo && simplifySet {
		switch simplify {
		case "subtle", "aggressive":
		default:
			fmt.Fprint(stderr, usage)
			return fmt.Errorf("--simplify must be \"subtle\" or \"aggressive\", got %q", simplify)
		}
	}

	// --edge accepts only the two named finishes.
	if photo && edgeSet {
		switch edge {
		case "crisp", "stroke":
		default:
			fmt.Fprint(stderr, usage)
			return fmt.Errorf("--edge must be \"crisp\" or \"stroke\", got %q", edge)
		}
	}

	in, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("opening input image: %w", err)
	}
	defer in.Close()

	var outPath string
	cfg := vectrigo.DefaultConfig()
	switch {
	case photo:
		cfg.Photo = true
		if sigmaSet {
			cfg.PhotoDetail = sigma
		}
		switch simplify {
		case "subtle":
			cfg.PhotoSimplify = vectrigo.PhotoSimplifySubtle
		case "aggressive":
			cfg.PhotoSimplify = vectrigo.PhotoSimplifyAggressive
		}
		// unset leaves the DefaultConfig default: simplification off.
		if edgeSet && edge == "stroke" {
			cfg.PhotoEdge = vectrigo.PhotoEdgeStroke
		}
		// crisp (or unset) leaves the DefaultConfig default, PhotoEdgeCrisp.
		outPath = photoOutputPath(inputPath)
	case autoK:
		cfg.AutoK = true
		outPath = autoOutputPath(inputPath)
	default:
		cfg.Sensitivity = sensitivity
		outPath = outputPath(inputPath, sensitivity)
	}

	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer out.Close()

	if err := vectrigo.Vectorize(in, out, cfg); err != nil {
		out.Close()
		os.Remove(outPath)
		return fmt.Errorf("vectorizing image: %w", err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("closing output file: %w", err)
	}

	fmt.Fprintln(stdout, outPath)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "vectrigo-cli:", err)
		os.Exit(1)
	}
}
