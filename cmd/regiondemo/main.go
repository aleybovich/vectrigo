// Command regiondemo is a visual correctness proof for the shared-boundary
// region tracer in internal/regiontrace.
//
// Usage:
//
//	regiondemo <img.png> <smooth> [bgHex] [out.svg]
//
// It decodes the image, segments it (segment.DefaultOptions), computes each
// region's mean colour, traces the label map as ONE planar subdivision with
// regiontrace.Trace, and writes an SVG in which:
//
//   - the <svg> width/height are the original image dimensions and the viewBox
//     is the working (post-downsample) coordinate space, matching assemble;
//   - the FIRST element is a full-canvas <rect> in bgHex (default #00ff00, a
//     deliberately garish green) — the correctness proof: if the tiling is truly
//     shared and gapless, no green may show anywhere in the interior;
//   - every region follows as a FILL-ONLY <path> (fill = region mean colour, no
//     stroke), painted largest-area-first so small detail lands on top.
//
// It prints the region count and total loop/point counts and timings.
package main

import (
	"bufio"
	"fmt"
	"image/color"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/aleybovich/minisvg"
	"github.com/aleybovich/segment"
	"github.com/aleybovich/vectrigo/internal/imageutil"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/regiontrace"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "regiondemo:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: regiondemo <img.png> <smooth> [bgHex] [out.svg]")
	}
	inPath := args[0]
	smooth, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("parsing smooth %q: %w", args[1], err)
	}
	bgHex := "#00ff00"
	if len(args) >= 3 {
		bgHex = args[2]
	}
	outPath := "rt.svg"
	if len(args) >= 4 {
		outPath = args[3]
	}

	f, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer f.Close()

	img, err := normalize.Decode(f, 2048, 2048)
	if err != nil {
		return err
	}
	b := img.NRGBA.Bounds()
	w, h := b.Dx(), b.Dy()

	segStart := time.Now()
	res := segment.Segment(img.NRGBA, segment.DefaultOptions())
	segElapsed := time.Since(segStart)
	colors := segment.MeanColors(img.NRGBA, res)

	// Region areas (pixel counts) for paint ordering.
	areas := make([]int, res.NumRegions)
	for _, lb := range res.Labels {
		if lb >= 0 && lb < res.NumRegions {
			areas[lb]++
		}
	}

	traceStart := time.Now()
	regions := regiontrace.Trace(res.Labels, res.W, res.H, res.NumRegions, regiontrace.Options{Smooth: smooth})
	traceElapsed := time.Since(traceStart)

	// Assemble drawables, count loops/points.
	totalLoops, totalPoints := 0, 0
	type drawable struct {
		d    string
		fill string
		area int
	}
	draws := make([]drawable, 0, len(regions))
	for _, rg := range regions {
		var pb minisvg.PathBuilder
		for _, loop := range rg.Loops {
			if len(loop) == 0 {
				continue
			}
			totalLoops++
			totalPoints += len(loop)
			pb.MoveTo(loop[0].X, loop[0].Y)
			for _, p := range loop[1:] {
				pb.LineTo(p.X, p.Y)
			}
			pb.Close()
		}
		d := pb.String()
		if d == "" {
			continue
		}
		c := colors[rg.ID]
		fill := imageutil.Hex(color.RGBA{R: c.R, G: c.G, B: c.B, A: 255})
		draws = append(draws, drawable{d: d, fill: fill, area: areas[rg.ID]})
	}

	// Largest-area-first, tie-broken by fill for determinism.
	sort.SliceStable(draws, func(i, j int) bool {
		if draws[i].area != draws[j].area {
			return draws[i].area > draws[j].area
		}
		return draws[i].fill < draws[j].fill
	})

	doc := minisvg.New(img.OrigW, img.OrigH)
	doc.SetViewBox(0, 0, float64(w), float64(h))
	doc.SetBackground(minisvg.Color(bgHex))
	for _, dr := range draws {
		doc.Path(dr.d, minisvg.Color(dr.fill))
	}

	of, err := os.Create(outPath)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(of)
	if _, err := doc.WriteToOpts(bw, minisvg.WriteOptions{Minify: true, Precision: 3}); err != nil {
		of.Close()
		return err
	}
	if err := bw.Flush(); err != nil {
		of.Close()
		return err
	}
	if err := of.Close(); err != nil {
		return err
	}
	fi, _ := os.Stat(outPath)

	fmt.Printf("dimensions:   working %dx%d, original %dx%d\n", w, h, img.OrigW, img.OrigH)
	fmt.Printf("regions:      %d (segment), %d traced\n", res.NumRegions, len(regions))
	fmt.Printf("loops:        %d\n", totalLoops)
	fmt.Printf("points:       %d\n", totalPoints)
	fmt.Printf("smooth:       %d iters\n", smooth)
	fmt.Printf("segment time: %s\n", segElapsed)
	fmt.Printf("trace time:   %s\n", traceElapsed)
	if fi != nil {
		fmt.Printf("output:       %s (%d bytes)\n", outPath, fi.Size())
	}
	return nil
}
