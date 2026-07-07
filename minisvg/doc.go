// Package minisvg is a minimal, zero-dependency SVG builder and writer.
//
// minisvg exists because the obvious off-the-shelf choice for programmatic SVG
// generation in Go (ajstarks/svgo) is licensed CC-BY-4.0, which is unsuitable
// for redistributable software and forbidden for use in commercial,
// closed-source products. minisvg depends on nothing but the Go standard
// library, so it carries no licensing risk of its own and can be vendored,
// forked, or shipped independently without concern.
//
// # Building a document
//
// Construct a [Document] with [New], giving it pixel dimensions. The
// document's viewBox defaults to "0 0 width height" and can be overridden
// with [Document.SetViewBox]. Add shapes with [Document.Path], or group
// related shapes together with [Document.Group]; groups nest arbitrarily and
// mirror the top-level path-adding API through [Group.Path] and
// [Group.Group].
//
//	doc := minisvg.New(100, 100)
//	doc.Path("M0 0 L100 0 L100 100 L0 100 Z", "#ff0000")
//
// # Building path data
//
// [PathBuilder] assembles the `d` attribute of a <path> element from a
// sequence of move/line/cubic-curve commands, so callers never need to
// hand-format path-data strings:
//
//	pb := new(minisvg.PathBuilder).
//		MoveTo(0, 0).
//		LineTo(10, 0).
//		CubicTo(15, 0, 20, 5, 20, 10).
//		Close()
//	doc.Path(pb.String(), "#000")
//
// # Writing output
//
// [Document.WriteTo] streams the finished document to an [io.Writer] using
// io.WriterTo semantics with sane default formatting. [Document.WriteToOpts]
// exposes explicit control over minification (stripping non-essential
// whitespace and newlines) and coordinate precision (rounding numbers found
// inside `d` and other numeric-looking attributes to a fixed number of
// decimal places) via [WriteOptions].
//
// All attribute values are XML-escaped on output, and the root element
// always carries the correct SVG namespace declaration
// (xmlns="http://www.w3.org/2000/svg").
package minisvg
