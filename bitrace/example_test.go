package bitrace_test

import (
	"fmt"
	"strings"

	"github.com/aleybovich/bitrace"
)

// ExampleTrace traces a small filled rectangle and reports the resulting
// contour.
func ExampleTrace() {
	// A 6×5 filled rectangle inside an 8×8 bitmap.
	bm := bitrace.Bitmap{W: 8, H: 8, Bits: make([]bool, 8*8)}
	for y := 1; y < 6; y++ {
		for x := 1; x < 7; x++ {
			bm.Bits[y*8+x] = true
		}
	}

	paths, err := bitrace.Trace(bm, bitrace.DefaultConfig())
	if err != nil {
		panic(err)
	}

	fmt.Printf("paths: %d\n", len(paths))
	fmt.Printf("hole: %v\n", paths[0].IsHole)
	fmt.Printf("commands: %d\n", len(paths[0].Commands))
	// Output:
	// paths: 1
	// hole: false
	// commands: 6
}

// ExampleTrace_svgPath shows how to turn the returned commands into an SVG path
// "d" attribute.
func ExampleTrace_svgPath() {
	bm := bitrace.Bitmap{W: 5, H: 5, Bits: make([]bool, 5*5)}
	for y := 1; y < 4; y++ {
		for x := 1; x < 4; x++ {
			bm.Bits[y*5+x] = true
		}
	}

	paths, _ := bitrace.Trace(bm, bitrace.DefaultConfig())

	var d strings.Builder
	for _, c := range paths[0].Commands {
		switch c.Kind {
		case bitrace.MoveTo:
			fmt.Fprintf(&d, "M%g,%g ", c.P.X, c.P.Y)
		case bitrace.LineTo:
			fmt.Fprintf(&d, "L%g,%g ", c.P.X, c.P.Y)
		case bitrace.CubicTo:
			fmt.Fprintf(&d, "C%g,%g %g,%g %g,%g ", c.C1.X, c.C1.Y, c.C2.X, c.C2.Y, c.P.X, c.P.Y)
		case bitrace.Close:
			d.WriteString("Z")
		}
	}
	fmt.Println(strings.TrimSpace(d.String()))
	// Output:
	// M1,1 L1,4 L4,4 L4,1 L1,1 Z
}

// ExampleTrace_holes demonstrates that a shape with a hole yields two contours,
// one of them flagged as a hole.
func ExampleTrace_holes() {
	const w, h = 20, 20
	bm := bitrace.Bitmap{W: w, H: h, Bits: make([]bool, w*h)}
	// Outer 12×12 block.
	for y := 4; y < 16; y++ {
		for x := 4; x < 16; x++ {
			bm.Bits[y*w+x] = true
		}
	}
	// Punch a 4×4 hole in the middle.
	for y := 8; y < 12; y++ {
		for x := 8; x < 12; x++ {
			bm.Bits[y*w+x] = false
		}
	}

	paths, _ := bitrace.Trace(bm, bitrace.DefaultConfig())

	var outer, hole int
	for _, p := range paths {
		if p.IsHole {
			hole++
		} else {
			outer++
		}
	}
	fmt.Printf("outer=%d hole=%d\n", outer, hole)
	// Output:
	// outer=1 hole=1
}
