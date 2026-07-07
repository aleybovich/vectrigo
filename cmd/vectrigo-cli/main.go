// Command vectrigo-cli vectorizes a raster image (PNG/JPEG/WEBP) into an SVG
// file using the vectrigo engine.
//
// Usage:
//
//	vectrigo-cli <image-path> <sensitivity>
//	vectrigo-cli --auto-k <image-path>
//
// <image-path> is the path to a PNG, JPEG, or WEBP raster image.
// <sensitivity> is an integer in [0,100] controlling the primary detail knob
// (see [vectrigo.Config.Sensitivity]); higher values produce more detail.
//
// The two invocation forms are mutually exclusive: supply a sensitivity, or
// pass --auto-k to let the engine choose the colour count K automatically from
// the image (see [vectrigo.Config.AutoK]). Passing both, or neither, is an
// error.
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
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aleybovich/vectrigo"
)

const usage = `Usage:
  vectrigo-cli <image-path> <sensitivity>
  vectrigo-cli --auto-k <image-path>

Vectorize a raster image (PNG/JPEG/WEBP) into an SVG file.

Arguments:
  image-path    Path to the input raster image (PNG, JPEG, or WEBP).
  sensitivity   Integer in [0,100]; the primary detail knob (higher = more detail).

Modes (mutually exclusive):
  Supply a sensitivity to use a fixed detail level, OR pass --auto-k to let the
  engine choose the colour count K automatically from the image (sensitivity is
  then ignored). Passing both, or neither, is an error.

The output SVG is written next to the input file:
  - with a sensitivity, the input's extension is replaced by ".<sensitivity>.svg".
  - with --auto-k, the input's extension is replaced by ".svg".

Examples:
  vectrigo-cli photos/street.png 70   =>  photos/street.70.svg
  vectrigo-cli --auto-k photos/street.png  =>  photos/street.svg

Options:
      --auto-k  Auto-select the colour count K from the image (no sensitivity).
  -h, --help    Show this help message.
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

// parseSensitivity parses s as an integer sensitivity in [0,100].
func parseSensitivity(s string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("sensitivity must be an integer, got %q", s)
	}
	if v < 0 || v > 100 {
		return 0, fmt.Errorf("sensitivity must be in [0,100], got %d", v)
	}
	return v, nil
}

// run implements the CLI: it parses args, runs the vectorization, and reports
// the outcome via stdout/stderr. It returns a non-nil error on any failure.
func run(args []string, stdout, stderr io.Writer) error {
	// Scan for flags in any position (matching how -h/--help is handled),
	// collecting the remaining positional arguments.
	autoK := false
	positional := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Fprint(stdout, usage)
			return nil
		case "--auto-k":
			autoK = true
		default:
			positional = append(positional, a)
		}
	}

	var (
		inputPath   string
		sensitivity int
	)

	if autoK {
		// auto-K mode: exactly one positional (the image path); a sensitivity
		// is not allowed because it is mutually exclusive with --auto-k.
		switch len(positional) {
		case 0:
			fmt.Fprint(stderr, usage)
			return fmt.Errorf("--auto-k requires an image path")
		case 1:
			inputPath = positional[0]
		default:
			fmt.Fprint(stderr, usage)
			return fmt.Errorf("--auto-k and a sensitivity value are mutually exclusive; --auto-k takes only an image path")
		}
	} else {
		// fixed-K mode: image path plus a required sensitivity.
		switch len(positional) {
		case 0, 1:
			fmt.Fprint(stderr, usage)
			return fmt.Errorf("sensitivity is required unless --auto-k is given")
		case 2:
			inputPath = positional[0]
			s, err := parseSensitivity(positional[1])
			if err != nil {
				return err
			}
			sensitivity = s
		default:
			fmt.Fprint(stderr, usage)
			return fmt.Errorf("expected 2 arguments, got %d", len(positional))
		}
	}

	in, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("opening input image: %w", err)
	}
	defer in.Close()

	var outPath string
	cfg := vectrigo.DefaultConfig()
	if autoK {
		cfg.AutoK = true
		outPath = autoOutputPath(inputPath)
	} else {
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
