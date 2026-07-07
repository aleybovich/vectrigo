// Command vectrigo-cli vectorizes a raster image (PNG/JPEG/WEBP) into an SVG
// file using the vectrigo engine.
//
// Usage:
//
//	vectrigo-cli -i <image-path> -s <sensitivity>
//	vectrigo-cli -i <image-path> --auto-k
//
// The input image is given with --input/-i and is required. It must be a PNG,
// JPEG, or WEBP raster image.
//
// The detail level is chosen in one of two mutually exclusive ways:
//
//   - --sensitivity/-s takes an integer in [0,100] controlling the primary
//     detail knob (see [vectrigo.Config.Sensitivity]); higher values produce
//     more detail.
//   - --auto-k lets the engine choose the colour count K automatically from the
//     image (see [vectrigo.Config.AutoK]); no sensitivity is then used.
//
// Exactly one of --sensitivity or --auto-k must be supplied. Passing both, or
// neither, is an error.
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
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aleybovich/vectrigo"
)

const usage = `Usage:
  vectrigo-cli -i <image-path> -s <sensitivity>
  vectrigo-cli -i <image-path> --auto-k

Vectorize a raster image (PNG/JPEG/WEBP) into an SVG file.

Options:
  -i, --input <path>          Path to the input raster image (PNG, JPEG, or WEBP). Required.
  -s, --sensitivity <0-100>   Integer detail knob (higher = more detail).
      --auto-k                Auto-select the colour count K from the image (no sensitivity).
  -h, --help                  Show this help message.

Modes (mutually exclusive):
  Supply --sensitivity to use a fixed detail level, OR pass --auto-k to let the
  engine choose the colour count K automatically from the image. Exactly one is
  required: passing both, or neither, is an error.

The output SVG is written next to the input file:
  - with a sensitivity, the input's extension is replaced by ".<sensitivity>.svg".
  - with --auto-k, the input's extension is replaced by ".svg".

Examples:
  vectrigo-cli -i photos/street.png -s 70   =>  photos/street.70.svg
  vectrigo-cli -i photos/street.png --auto-k  =>  photos/street.svg
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

	// Whether --sensitivity/-s was actually supplied on the command line.
	sensitivitySet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "sensitivity" || f.Name == "s" {
			sensitivitySet = true
		}
	})

	if inputPath == "" {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("an input image (-i) is required")
	}

	if autoK && sensitivitySet {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("--auto-k and --sensitivity are mutually exclusive")
	}

	if !autoK && !sensitivitySet {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("a sensitivity (-s) is required unless --auto-k is given")
	}

	if sensitivitySet {
		if err := validateSensitivity(sensitivity); err != nil {
			return err
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
