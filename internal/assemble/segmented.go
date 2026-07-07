package assemble

import (
	"image/color"
	"io"
	"sort"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/minisvg"
	"github.com/aleybovich/vectrigo/internal/imageutil"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/pipeline"
)

// DefaultSegmentStrokeWidth is the seam-sealing stroke width [WriteSegmented]
// paints under each region fill (in the same colour) when [Options.StrokeWidth]
// is unset. It is deliberately small: it only needs to bridge the sub-pixel gaps
// between adjacent region tiles so no white "grout" shows through, not to fatten
// the shapes.
const DefaultSegmentStrokeWidth = 0.8

// WriteSegmented serializes the photo-mode (segmentation) result to w: a full-
// canvas mean-colour background with every traced region painted on top, each as
// a single same-colour filled-and-stroked <path>, largest-area-first.
//
// It mirrors [WriteSVG]'s document framing — the <svg> width/height are the
// image's original dimensions and the viewBox is the working (post-downsample)
// coordinate space — but differs in three photo-specific ways:
//
//   - meanBG (the mean of the image's opaque pixels) is set as a full-canvas
//     <rect> background via minisvg.SetBackground, so the sub-pixel seams
//     between region tiles never flash white.
//   - Regions are ordered purely largest-area-first (area desc, then packed
//     colour asc, stable on region id) — the stacked painter's model where big
//     background regions paint first and small detail lands on top. Unlike
//     [WriteSVG]'s [Order] this skips the O(n²) containment analysis, which is
//     prohibitive at the thousands of regions photo mode produces and
//     unnecessary here (regions tile the plane rather than nest).
//   - Each region is emitted with minisvg.StrokedPath using fill == stroke ==
//     the region's mean colour and a small stroke width (opt.StrokeWidth, or
//     [DefaultSegmentStrokeWidth]) to seal the seams.
//
// As in [WriteSVG], every contour of a region (outer boundaries and holes
// together) is concatenated into one "d" string so the nonzero fill-rule renders
// holes correctly. Optimize/Precision are honoured via minisvg.WriteToOpts so
// coordinates AND the stroke-width are rounded — critical for file size.
func WriteSegmented(w io.Writer, regions []pipeline.Traced, img normalize.Image, meanBG color.RGBA, opt Options) error {
	b := img.NRGBA.Bounds()
	workW, workH := b.Dx(), b.Dy()

	doc := minisvg.New(img.OrigW, img.OrigH)
	doc.SetViewBox(0, 0, float64(workW), float64(workH))
	doc.SetBackground(minisvg.Color(imageutil.Hex(meanBG)))

	strokeW := opt.StrokeWidth
	if strokeW <= 0 {
		strokeW = DefaultSegmentStrokeWidth
	}

	ordered := orderByArea(regions)
	for _, t := range ordered {
		if len(t.Paths) == 0 {
			continue
		}
		d := segmentPathData(t.Paths)
		if d == "" {
			continue
		}
		fill := minisvg.Color(imageutil.Hex(t.Color))
		doc.StrokedPath(d, fill, fill, strokeW)
	}

	if opt.Optimize {
		_, err := doc.WriteToOpts(w, minisvg.WriteOptions{Minify: true, Precision: opt.Precision})
		return err
	}
	_, err := doc.WriteTo(w)
	return err
}

// orderByArea returns a copy of regions sorted into paint order: largest area
// first (painted furthest back), so small foreground detail lands on top. Ties
// break on packed colour ascending, then — via the stable sort — on the caller's
// original (region-id) order, making the result fully deterministic. The input
// slice is not mutated.
func orderByArea(regions []pipeline.Traced) []pipeline.Traced {
	out := make([]pipeline.Traced, len(regions))
	copy(out, regions)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Area != out[j].Area {
			return out[i].Area > out[j].Area
		}
		return imageutil.Pack(out[i].Color) < imageutil.Pack(out[j].Color)
	})
	return out
}

// segmentPathData concatenates every traced contour of a region (outer
// boundaries and holes together) into one SVG "d" string, matching WriteSVG's
// per-colour concatenation so the nonzero fill-rule renders holes.
func segmentPathData(paths []bitrace.Path) string {
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
