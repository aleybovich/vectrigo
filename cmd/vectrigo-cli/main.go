// Command vectrigo-cli vectorizes a raster image (PNG/JPEG/WEBP) into an SVG
// file using the vectrigo engine.
//
// Usage:
//
//	vectrigo-cli <image-path> <sensitivity>
//
// <image-path> is the path to a PNG, JPEG, or WEBP raster image.
// <sensitivity> is an integer in [0,100] controlling the primary detail knob
// (see [vectrigo.Config.Sensitivity]); higher values produce more detail.
//
// The resulting SVG is written next to the input file, with the input's
// extension replaced by ".<sensitivity>.svg". For example, "photos/street.png"
// at sensitivity 70 produces "photos/street.70.svg".
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

const usage = `Usage: vectrigo-cli <image-path> <sensitivity>

Vectorize a raster image (PNG/JPEG/WEBP) into an SVG file.

Arguments:
  image-path    Path to the input raster image (PNG, JPEG, or WEBP).
  sensitivity   Integer in [0,100]; the primary detail knob (higher = more detail).

The output SVG is written next to the input file, with the input's
extension replaced by ".<sensitivity>.svg".

Example:
  vectrigo-cli photos/street.png 70   =>  photos/street.70.svg

Options:
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
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Fprint(stdout, usage)
			return nil
		}
	}

	if len(args) != 2 {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("expected 2 arguments, got %d", len(args))
	}

	inputPath := args[0]
	sensitivity, err := parseSensitivity(args[1])
	if err != nil {
		return err
	}

	in, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("opening input image: %w", err)
	}
	defer in.Close()

	outPath := outputPath(inputPath, sensitivity)

	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer out.Close()

	cfg := vectrigo.DefaultConfig()
	cfg.Sensitivity = sensitivity

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
