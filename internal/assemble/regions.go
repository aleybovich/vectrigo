package assemble

import (
	"image/color"
	"io"
	"sort"

	"github.com/aleybovich/minisvg"
	"github.com/aleybovich/vectrigo/internal/imageutil"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/regiontrace"
)

// SeamStrokeWidth is the same-colour stroke width [WriteRegions] paints on each
// region in stroke mode (crisp == false) to seal the residual sub-pixel seams
// between adjacent region tiles. It is deliberately small: it only bridges the
// sub-pixel gaps so no "grout" shows through, not to fatten the shapes.
const SeamStrokeWidth = 0.8

// WriteRegions serializes the photo-mode result of [regiontrace.Trace] to w:
// every region painted as a single filled <path>, largest-area-first.
//
// Because regiontrace produces a planar subdivision — adjacent regions share
// exact boundary geometry, so the regions TILE THE PLANE with no gaps and no
// overlap — every opaque pixel is covered by exactly one region and NO
// background rect is emitted (nor needed); transparent areas correctly show
// through.
//
// It mirrors [WriteSVG]'s document framing: the <svg> width/height are the
// image's original dimensions and the viewBox is the working (post-downsample)
// coordinate space. Regions are ordered purely largest-area-first (area desc,
// then packed colour asc, then region id asc — fully deterministic), the
// stacked painter's model where big background regions paint first and small
// detail lands on top. This skips [Order]'s O(n²) containment analysis, which is
// prohibitive at the thousands of regions photo mode produces and unnecessary
// here (regions tile rather than nest).
//
// The edge finish is selected by crisp:
//
//   - crisp == true: shape-rendering=crispEdges disables edge anti-aliasing and
//     each region is a fill-only <path>. The crispest, perfectly seam-free look.
//   - crisp == false: each region is emitted with a thin same-colour stroke
//     (fill == stroke, width [SeamStrokeWidth]) and anti-aliasing is kept, so
//     the residual sub-pixel seams are sealed at the cost of slightly softer
//     edges.
//
// Each region's loops (outer boundaries and holes together) are concatenated
// into one "d" string so the nonzero fill-rule renders holes correctly:
// regiontrace winds outer loops positive and holes negative, so within a single
// path their windings cancel inside holes. Optimize/Precision are honoured via
// minisvg.WriteToOpts so coordinates (and the stroke width) are rounded —
// critical for file size.
//
// colors and areas are indexed by region id (length = numRegions); each
// region's fill is colors[region.ID] forced fully opaque.
func WriteRegions(w io.Writer, regions []regiontrace.Region, colors []color.RGBA, areas []int, img normalize.Image, crisp bool, opt Options) error {
	b := img.NRGBA.Bounds()
	workW, workH := b.Dx(), b.Dy()

	doc := minisvg.New(img.OrigW, img.OrigH)
	doc.SetViewBox(0, 0, float64(workW), float64(workH))
	if crisp {
		doc.SetShapeRendering("crispEdges")
	}

	ordered := orderRegionsByArea(regions, colors, areas)
	for _, rg := range ordered {
		d := regionPathData(rg)
		if d == "" {
			continue
		}
		fill := minisvg.Color(imageutil.Hex(opaqueColor(colors, rg.ID)))
		if crisp {
			doc.Path(d, fill)
		} else {
			doc.StrokedPath(d, fill, fill, SeamStrokeWidth)
		}
	}

	if opt.Optimize {
		_, err := doc.WriteToOpts(w, minisvg.WriteOptions{Minify: true, Precision: opt.Precision})
		return err
	}
	_, err := doc.WriteTo(w)
	return err
}

// orderRegionsByArea returns a copy of regions in paint order: largest area
// first (painted furthest back), so small foreground detail lands on top. Ties
// break on packed opaque colour ascending, then on region id ascending, making
// the result fully deterministic. The input slice is not mutated.
func orderRegionsByArea(regions []regiontrace.Region, colors []color.RGBA, areas []int) []regiontrace.Region {
	out := make([]regiontrace.Region, len(regions))
	copy(out, regions)
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := areaOf(areas, out[i].ID), areaOf(areas, out[j].ID)
		if ai != aj {
			return ai > aj
		}
		pi := imageutil.Pack(opaqueColor(colors, out[i].ID))
		pj := imageutil.Pack(opaqueColor(colors, out[j].ID))
		if pi != pj {
			return pi < pj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// regionPathData concatenates all of a region's loops into one SVG "d" string:
// each loop is MoveTo(first) + LineTo(rest) + Close, so the nonzero fill-rule
// renders holes (outer loops wind positive, holes negative).
func regionPathData(rg regiontrace.Region) string {
	var pb minisvg.PathBuilder
	for _, loop := range rg.Loops {
		if len(loop) == 0 {
			continue
		}
		pb.MoveTo(loop[0].X, loop[0].Y)
		for _, p := range loop[1:] {
			pb.LineTo(p.X, p.Y)
		}
		pb.Close()
	}
	return pb.String()
}

// opaqueColor returns colors[id] forced fully opaque. It is defensive against a
// short colors slice (returns opaque black) though callers pass length ==
// numRegions.
func opaqueColor(colors []color.RGBA, id int) color.RGBA {
	if id < 0 || id >= len(colors) {
		return color.RGBA{A: 255}
	}
	c := colors[id]
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255}
}

// areaOf returns areas[id], or 0 if id is out of range (defensive).
func areaOf(areas []int, id int) int {
	if id < 0 || id >= len(areas) {
		return 0
	}
	return areas[id]
}
