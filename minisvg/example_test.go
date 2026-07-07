package minisvg_test

import (
	"fmt"
	"os"

	"github.com/aleybovich/minisvg"
)

// Building and writing a simple document with a single filled path.
func Example() {
	doc := minisvg.New(100, 100)
	doc.Path("M0 0 L100 0 L100 100 L0 100 Z", "#ff0000")
	doc.WriteTo(os.Stdout)
	// Output:
	// <svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100">
	//   <path d="M0 0 L100 0 L100 100 L0 100 Z" fill="#ff0000"/>
	// </svg>
}

// PathBuilder assembles a "d" attribute value from move/line/cubic commands.
func ExamplePathBuilder() {
	pb := new(minisvg.PathBuilder).
		MoveTo(0, 0).
		LineTo(10, 0).
		CubicTo(15, 0, 20, 5, 20, 10).
		Close()
	fmt.Println(pb.String())
	// Output:
	// M0 0 L10 0 C15 0 20 5 20 10 Z
}

// Groups nest paths (and further groups) under a shared <g> element,
// optionally carrying a fill that children inherit.
func ExampleDocument_Group() {
	doc := minisvg.New(10, 10)
	g := doc.Group("blue")
	g.Path("M0 0 L10 0 L10 10 Z", "")
	doc.WriteTo(os.Stdout)
	// Output:
	// <svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 10 10">
	//   <g fill="blue">
	//     <path d="M0 0 L10 0 L10 10 Z"/>
	//   </g>
	// </svg>
}

// StrokedPath emits a path with both a fill and a same-or-different colored
// stroke; a full-canvas background is added with SetBackground and always
// renders first.
func ExampleDocument_StrokedPath() {
	doc := minisvg.New(10, 10)
	doc.SetBackground("#202020")
	doc.StrokedPath("M0 0 L10 0 L10 10 Z", "#ff0000", "#ff0000", 0.5)
	doc.WriteTo(os.Stdout)
	// Output:
	// <svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 10 10">
	//   <rect width="10" height="10" fill="#202020"/>
	//   <path d="M0 0 L10 0 L10 10 Z" fill="#ff0000" stroke="#ff0000" stroke-width="0.5"/>
	// </svg>
}

// SetViewBox overrides the default "0 0 width height" viewBox.
func ExampleDocument_SetViewBox() {
	doc := minisvg.New(100, 100)
	doc.SetViewBox(-10, -10, 120, 120)
	doc.WriteTo(os.Stdout)
	// Output:
	// <svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="-10 -10 120 120">
	// </svg>
}

// WriteToOpts controls minification and coordinate rounding independently of
// the default (pretty-printed, unrounded) WriteTo behavior.
func ExampleDocument_WriteToOpts() {
	doc := minisvg.New(10, 10)
	doc.Path("M0 0 L10.256 0 L10 10 Z", "#000")
	doc.WriteToOpts(os.Stdout, minisvg.WriteOptions{Minify: true, Precision: 1})
	// Output:
	// <svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 10 10"><path d="M0 0 L10.3 0 L10 10 Z" fill="#000"/></svg>
}
