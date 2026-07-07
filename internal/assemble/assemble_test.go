package assemble

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"io"
	"strings"
	"testing"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/pipeline"
)

// rectPath builds a closed rectangular contour as bitrace commands.
func rectPath(x0, y0, x1, y1 float64, hole bool) bitrace.Path {
	p := func(x, y float64) bitrace.Point { return bitrace.Point{X: x, Y: y} }
	return bitrace.Path{
		IsHole: hole,
		Commands: []bitrace.Command{
			{Kind: bitrace.MoveTo, P: p(x0, y0)},
			{Kind: bitrace.LineTo, P: p(x1, y0)},
			{Kind: bitrace.LineTo, P: p(x1, y1)},
			{Kind: bitrace.LineTo, P: p(x0, y1)},
			{Kind: bitrace.Close},
		},
	}
}

func testImage(w, h int) normalize.Image {
	return normalize.Image{
		NRGBA: image.NewNRGBA(image.Rect(0, 0, w, h)),
		OrigW: w * 2, // simulate a 2x downsample so viewBox != width
		OrigH: h * 2,
	}
}

// svgAttrs parses the root <svg> element's attributes and returns the ordered
// list of (fill, d) for each <path>, plus whether the whole doc is well-formed.
type parsed struct {
	width, height, viewBox string
	fills                  []string
	ds                     []string
}

func parseSVG(t *testing.T, b []byte) parsed {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(b))
	var out parsed
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("not well-formed XML: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "svg":
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "width":
					out.width = a.Value
				case "height":
					out.height = a.Value
				case "viewBox":
					out.viewBox = a.Value
				}
			}
		case "path":
			var fill, d string
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "fill":
					fill = a.Value
				case "d":
					d = a.Value
				}
			}
			out.fills = append(out.fills, fill)
			out.ds = append(out.ds, d)
		}
	}
	return out
}

func TestWriteSVGStructure(t *testing.T) {
	big := color.RGBA{R: 200, G: 200, B: 200, A: 255}  // large background
	holed := color.RGBA{R: 10, G: 120, B: 200, A: 255} // medium, with a hole
	fg := color.RGBA{R: 250, G: 10, B: 10, A: 255}     // small foreground

	traced := []pipeline.Traced{
		{Color: fg, Area: 25, Paths: []bitrace.Path{rectPath(40, 40, 50, 50, false)}},
		{Color: big, Area: 10000, Paths: []bitrace.Path{rectPath(0, 0, 100, 100, false)}},
		{Color: holed, Area: 2500, Paths: []bitrace.Path{
			rectPath(10, 10, 60, 60, false),
			rectPath(20, 20, 40, 40, true),
		}},
	}

	var buf bytes.Buffer
	if err := WriteSVG(&buf, traced, testImage(100, 100), Options{Optimize: true, Precision: 2}); err != nil {
		t.Fatalf("WriteSVG: %v", err)
	}
	p := parseSVG(t, buf.Bytes())

	if p.width != "200" || p.height != "200" {
		t.Errorf("width/height = %s/%s, want 200/200 (original dims)", p.width, p.height)
	}
	if p.viewBox != "0 0 100 100" {
		t.Errorf("viewBox = %q, want \"0 0 100 100\" (working dims)", p.viewBox)
	}

	// One path per colour (3 colours -> 3 paths).
	if len(p.fills) != 3 {
		t.Fatalf("path count = %d, want 3 (one per colour)", len(p.fills))
	}

	// Z-order: largest area (big, #c8c8c8) first.
	if p.fills[0] != "#c8c8c8" {
		t.Errorf("first fill = %s, want #c8c8c8 (largest area first)", p.fills[0])
	}

	// The holed layer's path must contain two M subcommands (outer + hole in
	// one path).
	var holedD string
	for i, f := range p.fills {
		if f == "#0a78c8" {
			holedD = p.ds[i]
		}
	}
	if holedD == "" {
		t.Fatal("holed layer fill #0a78c8 not found")
	}
	if n := strings.Count(holedD, "M"); n < 2 {
		t.Errorf("holed d has %d M subcommands, want >= 2: %q", n, holedD)
	}

	// Every fill is a #rrggbb string.
	for _, f := range p.fills {
		if len(f) != 7 || f[0] != '#' {
			t.Errorf("fill %q is not #rrggbb", f)
		}
	}
}

func TestWriteSVGMinifyToggle(t *testing.T) {
	traced := []pipeline.Traced{
		{Color: color.RGBA{R: 1, G: 2, B: 3, A: 255}, Area: 100,
			Paths: []bitrace.Path{rectPath(0.123456, 0.987654, 10, 10, false)}},
	}

	var min bytes.Buffer
	if err := WriteSVG(&min, traced, testImage(20, 20), Options{Optimize: true, Precision: 2}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(min.String(), "\n") {
		t.Error("minified output should contain no newlines")
	}
	if strings.Contains(min.String(), "0.123456") {
		t.Errorf("coords should be rounded to precision 2: %s", min.String())
	}

	var pretty bytes.Buffer
	if err := WriteSVG(&pretty, traced, testImage(20, 20), Options{Optimize: false}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pretty.String(), "\n") {
		t.Error("pretty output should contain newlines")
	}
	if !strings.Contains(pretty.String(), "0.123456") {
		t.Errorf("pretty output should keep unrounded coords: %s", pretty.String())
	}
}

func TestWriteSVGEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSVG(&buf, nil, testImage(30, 20), Options{Optimize: true, Precision: 2}); err != nil {
		t.Fatalf("WriteSVG(nil): %v", err)
	}
	p := parseSVG(t, buf.Bytes())
	if len(p.fills) != 0 {
		t.Errorf("path count = %d, want 0 for empty input", len(p.fills))
	}
	if p.viewBox != "0 0 30 20" {
		t.Errorf("viewBox = %q, want \"0 0 30 20\"", p.viewBox)
	}
}
