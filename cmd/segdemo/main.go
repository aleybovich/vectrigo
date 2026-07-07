// Command segdemo exercises the internal/segment Felzenszwalb–Huttenlocher
// segmentation library end-to-end on a real image and renders the result to
// SVG, so the segmentation front-end can be evaluated visually against the
// existing colour-quantization path.
//
// Usage:
//
//	segdemo <input.png> <K> <minSize> [sigma] [out.svg]
//
// It decodes the image, segments it into regions, gives each region its mean
// colour, traces every region's mask with bitrace, and writes a two-layer SVG
// (svgstorm's technique): all regions as coloured strokes first, then all
// regions as fills, both largest-area-first. Painting a matching-colour stroke
// under the fills seals the sub-pixel seams between adjacent region tiles so
// gaps show a colour "grout" instead of white.
package main

import (
	"bufio"
	"fmt"
	"image/color"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/minisvg"
	"github.com/aleybovich/vectrigo/internal/imageutil"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/segment"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "segdemo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: segdemo <input.png> <K> <minSize> [sigma] [out.svg]")
	}
	inPath := args[0]
	k, err := strconv.ParseFloat(args[1], 64)
	if err != nil {
		return fmt.Errorf("parsing K %q: %w", args[1], err)
	}
	minSize, err := strconv.Atoi(args[2])
	if err != nil {
		return fmt.Errorf("parsing minSize %q: %w", args[2], err)
	}
	sigma := 0.0
	if len(args) >= 4 {
		if sigma, err = strconv.ParseFloat(args[3], 64); err != nil {
			return fmt.Errorf("parsing sigma %q: %w", args[3], err)
		}
	}
	outPath := "seg.svg"
	if len(args) >= 5 {
		outPath = args[4]
	}

	f, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Large ceiling so the image is segmented at (near) full resolution.
	img, err := normalize.Decode(f, 2048, 2048)
	if err != nil {
		return err
	}
	b := img.NRGBA.Bounds()
	w, h := b.Dx(), b.Dy()

	start := time.Now()
	res := segment.Segment(img.NRGBA, segment.Options{K: k, MinSize: minSize, Sigma: sigma})
	segElapsed := time.Since(start)

	colors := segment.MeanColors(img.NRGBA, res)

	// Build a mask per region and the region areas (pixel counts).
	plane := w * h
	masks := make([][]bool, res.NumRegions)
	areas := make([]int, res.NumRegions)
	buf := make([]bool, res.NumRegions*plane)
	for r := 0; r < res.NumRegions; r++ {
		masks[r] = buf[r*plane : (r+1)*plane : (r+1)*plane]
	}
	for i, lb := range res.Labels {
		if lb < 0 {
			continue
		}
		masks[lb][i] = true
		areas[lb]++
	}

	// Trace every region, producing one "d" path string per non-empty region.
	cfg := bitrace.Config{TurdSize: 0, AlphaMax: 1.0, Optimize: true}
	regions := make([]region, 0, res.NumRegions)
	pathCount := 0
	for r := 0; r < res.NumRegions; r++ {
		paths, err := bitrace.Trace(bitrace.Bitmap{W: w, H: h, Bits: masks[r]}, cfg)
		if err != nil {
			return fmt.Errorf("tracing region %d: %w", r, err)
		}
		d := pathData(paths)
		if d == "" {
			continue
		}
		pathCount++
		regions = append(regions, region{d: d, fill: imageutil.Hex(toRGBA(colors[r])), area: areas[r]})
	}

	// Largest-area-first ordering, tie-broken by fill for determinism.
	sort.SliceStable(regions, func(i, j int) bool {
		if regions[i].area != regions[j].area {
			return regions[i].area > regions[j].area
		}
		return regions[i].fill < regions[j].fill
	})

	if err := writeSVG(outPath, regions, img.OrigW, img.OrigH, w, h); err != nil {
		return err
	}

	fi, err := os.Stat(outPath)
	if err != nil {
		return err
	}

	fmt.Printf("regions:     %d\n", res.NumRegions)
	fmt.Printf("paths:       %d\n", pathCount)
	fmt.Printf("output:      %s (%d bytes)\n", outPath, fi.Size())
	fmt.Printf("dimensions:  working %dx%d, original %dx%d\n", w, h, img.OrigW, img.OrigH)
	fmt.Printf("segment k=%.3g minSize=%d sigma=%.3g\n", k, minSize, sigma)
	fmt.Printf("segment time: %s\n", segElapsed)
	return nil
}

// pathData concatenates every traced contour of a region into a single SVG "d"
// string (outer boundaries and holes together), matching the assemble stage so
// the nonzero fill-rule renders holes correctly.
func pathData(paths []bitrace.Path) string {
	var pb minisvg.PathBuilder
	for _, p := range paths {
		for _, c := range p.Commands {
			switch c.Kind {
			case bitrace.MoveTo:
				pb.MoveTo(c.P.X, c.P.Y)
			case bitrace.LineTo:
				pb.LineTo(c.P.X, c.P.Y)
			case bitrace.CubicTo:
				pb.CubicTo(c.C1.X, c.C1.Y, c.C2.X, c.C2.Y, c.P.X, c.P.Y)
			case bitrace.Close:
				pb.Close()
			}
		}
	}
	return pb.String()
}

// toRGBA drops the alpha channel to opaque; region fills are always rendered
// solid (transparency in the source becomes SVG background, not a region).
func toRGBA(c color.RGBA) color.RGBA {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255}
}

// region is one traced region: its concatenated path data, opaque fill colour,
// and pixel area (used for z-ordering).
type region struct {
	d    string
	fill string
	area int
}

// writeSVG emits the two-layer stroke-then-fill document. width/height are the
// original image dimensions; the viewBox is the working coordinate space.
func writeSVG(path string, regions []region, origW, origH, workW, workH int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)

	fmt.Fprintf(bw, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`,
		origW, origH, workW, workH)
	bw.WriteByte('\n')

	// Layer 1: coloured strokes (grout) under everything, largest-area-first.
	for _, r := range regions {
		fmt.Fprintf(bw, `<path d="%s" fill="none" stroke="%s" stroke-width="0.4"/>`, r.d, r.fill)
		bw.WriteByte('\n')
	}
	// Layer 2: fills, largest-area-first.
	for _, r := range regions {
		fmt.Fprintf(bw, `<path d="%s" fill="%s"/>`, r.d, r.fill)
		bw.WriteByte('\n')
	}

	bw.WriteString("</svg>\n")
	return bw.Flush()
}
