// Package assemble implements Stage IV of the vectrigo pipeline: order the
// traced layers by area, merge each colour's contours into a single winding-
// aware path, and serialize the document with minisvg.
package assemble

import (
	"io"
	"sort"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/minisvg"
	"github.com/aleybovich/vectrigo/internal/imageutil"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/pipeline"
)

// Options controls SVG serialization.
type Options struct {
	// Optimize enables minisvg's minify + coordinate-rounding pass.
	Optimize bool
	// Precision is the number of coordinate decimal places used when Optimize
	// is set.
	Precision int
}

// WriteSVG orders the traced layers (largest area first, so big background
// shapes render behind small foreground detail), serializes them via minisvg,
// and streams the document to w.
//
// The <svg> width/height are the image's original dimensions and the viewBox
// is the working (post-downsample) coordinate space, so a renderer scales the
// vector content back to the source's apparent size with no per-point math.
//
// Each colour becomes exactly one <path>: every contour for that colour (outer
// boundaries and holes together) is concatenated into a single "d" string so
// SVG's default nonzero fill-rule renders holes correctly — bitrace winds
// outer and hole contours oppositely, so within a single path their windings
// cancel inside holes. Emitting a hole as its own sibling <path> would instead
// paint it solid, defeating the hole.
func WriteSVG(w io.Writer, traced []pipeline.Traced, img normalize.Image, opt Options) error {
	b := img.NRGBA.Bounds()
	workW, workH := b.Dx(), b.Dy()

	doc := minisvg.New(img.OrigW, img.OrigH)
	doc.SetViewBox(0, 0, float64(workW), float64(workH))

	// Copy before sorting so we do not mutate the caller's slice ordering.
	order := make([]pipeline.Traced, len(traced))
	copy(order, traced)
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].Area != order[j].Area {
			return order[i].Area > order[j].Area // largest first (painted first/behind)
		}
		return imageutil.Pack(order[i].Color) < imageutil.Pack(order[j].Color)
	})

	for _, t := range order {
		if len(t.Paths) == 0 {
			continue
		}
		var pb minisvg.PathBuilder
		for _, p := range t.Paths {
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
		d := pb.String()
		if d == "" {
			continue
		}
		doc.Path(d, minisvg.Color(imageutil.Hex(t.Color)))
	}

	if opt.Optimize {
		_, err := doc.WriteToOpts(w, minisvg.WriteOptions{Minify: true, Precision: opt.Precision})
		return err
	}
	_, err := doc.WriteTo(w)
	return err
}
