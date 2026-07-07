package minisvg

import (
	"io"
	"strconv"
	"strings"
)

// svgNamespace is the required namespace declaration for the root <svg>
// element.
const svgNamespace = "http://www.w3.org/2000/svg"

// Color is an SVG fill value, e.g. "#1a2b3c", "red", or "none".
//
// An empty Color ("") is treated specially: it means "do not emit a fill
// attribute at all", letting the element fall back to SVG's own defaults (or
// to a fill inherited from an ancestor element). Pass an explicit value such
// as "none" to force no fill rather than inheriting one.
type Color string

// attr is a single XML attribute, stored with its insertion order preserved
// so serialized output is deterministic.
type attr struct {
	name  string
	value string
}

// node is an internal representation of a single XML element (either a <g>
// or a <path>). The root <svg> element is handled separately by Document
// since it carries width/height/viewBox rather than a fill.
type node struct {
	tag      string
	attrs    []attr
	children []*node
}

// Document is the root SVG builder. Construct one with [New], add content
// with [Document.Path] and [Document.Group], then serialize it with
// [Document.WriteTo] or [Document.WriteToOpts].
//
// A Document is a plain builder, not safe for concurrent use by multiple
// goroutines without external synchronization.
type Document struct {
	width, height int

	viewBoxSet bool
	vbMinX     float64
	vbMinY     float64
	vbW        float64
	vbH        float64

	children []*node
}

// New returns a Document with the given pixel dimensions. The viewBox
// defaults to "0 0 width height"; override it with [Document.SetViewBox].
//
// width and height are written verbatim as the <svg> element's width/height
// attributes (unitless, i.e. interpreted as pixels per the SVG spec).
func New(width, height int) *Document {
	return &Document{
		width:  width,
		height: height,
	}
}

// SetViewBox overrides the document's default viewBox (which is otherwise
// "0 0 width height"). It returns the Document so calls can be chained.
func (d *Document) SetViewBox(minX, minY, w, h float64) *Document {
	d.viewBoxSet = true
	d.vbMinX = minX
	d.vbMinY = minY
	d.vbW = w
	d.vbH = h
	return d
}

// Path appends a <path> element with the given path-data string (typically
// produced by [PathBuilder]) and fill color to the document's root. It
// returns the Document so calls can be chained.
//
// If fill is "" no fill attribute is written at all (see [Color]).
func (d *Document) Path(data string, fill Color) *Document {
	d.children = append(d.children, newPathNode(data, fill))
	return d
}

// Group creates a new <g> element, appends it to the document's root, and
// returns a [Group] wrapping it; paths (and further nested groups) added to
// the returned Group are nested inside that <g>.
//
// If fill is "" no fill attribute is written on the <g> element, and its
// children fall back to their own fill (or SVG's default).
func (d *Document) Group(fill Color) *Group {
	g := newGroupNode(fill)
	d.children = append(d.children, g)
	return &Group{n: g}
}

// Group represents a nested <g> element. Obtain one via [Document.Group] or
// [Group.Group]; it mirrors the path/group-adding API of [Document] so
// content can be nested to arbitrary depth.
type Group struct {
	n *node
}

// Path appends a <path> element with the given path-data string and fill
// color inside this group. It returns the Group so calls can be chained.
//
// If fill is "" no fill attribute is written at all (see [Color]).
func (g *Group) Path(data string, fill Color) *Group {
	g.n.children = append(g.n.children, newPathNode(data, fill))
	return g
}

// Group creates a nested <g> element inside this group and returns a
// [Group] wrapping it, allowing content to be nested to arbitrary depth.
//
// If fill is "" no fill attribute is written on the nested <g> element.
func (g *Group) Group(fill Color) *Group {
	child := newGroupNode(fill)
	g.n.children = append(g.n.children, child)
	return &Group{n: child}
}

func newPathNode(data string, fill Color) *node {
	n := &node{tag: "path"}
	n.attrs = append(n.attrs, attr{"d", data})
	if fill != "" {
		n.attrs = append(n.attrs, attr{"fill", string(fill)})
	}
	return n
}

func newGroupNode(fill Color) *node {
	n := &node{tag: "g"}
	if fill != "" {
		n.attrs = append(n.attrs, attr{"fill", string(fill)})
	}
	return n
}

// WriteOptions controls how [Document.WriteToOpts] serializes a document.
type WriteOptions struct {
	// Minify strips non-essential whitespace and newlines from the output,
	// producing a single compact line instead of an indented, one-tag-per-line
	// document.
	Minify bool

	// Precision is the number of decimal places coordinates in the "d" and
	// "viewBox" attributes are rounded to (e.g. 12.345678 at precision 2
	// becomes 12.35). Rounding is round-half-away-from-zero and trims
	// trailing fractional zeros (12.996 at precision 2 becomes 13, not
	// 13.00).
	//
	// A negative Precision disables rounding entirely, leaving numbers
	// exactly as supplied.
	Precision int
}

// WriteTo serializes the document to w with default formatting: indented,
// human-readable output and no coordinate rounding. It implements
// io.WriterTo.
func (d *Document) WriteTo(w io.Writer) (int64, error) {
	return d.WriteToOpts(w, WriteOptions{Minify: false, Precision: -1})
}

// WriteToOpts serializes the document to w using the given [WriteOptions],
// applying minification and/or coordinate-precision rounding as requested.
// It implements io.WriterTo-like semantics, returning the number of bytes
// written and any write error.
func (d *Document) WriteToOpts(w io.Writer, opt WriteOptions) (int64, error) {
	var sb strings.Builder
	d.render(&sb, opt)
	n, err := io.WriteString(w, sb.String())
	return int64(n), err
}

func (d *Document) render(sb *strings.Builder, opt WriteOptions) {
	nl, indentUnit := "\n", "  "
	if opt.Minify {
		nl, indentUnit = "", ""
	}

	sb.WriteString("<svg xmlns=\"")
	sb.WriteString(svgNamespace)
	sb.WriteString("\" width=\"")
	sb.WriteString(strconv.Itoa(d.width))
	sb.WriteString("\" height=\"")
	sb.WriteString(strconv.Itoa(d.height))
	sb.WriteString("\" viewBox=\"")
	sb.WriteString(escapeAttr(applyPrecision(d.viewBoxValue(), "viewBox", opt.Precision)))
	sb.WriteString("\">")
	sb.WriteString(nl)

	for _, c := range d.children {
		writeNode(sb, c, 1, indentUnit, nl, opt.Precision)
	}

	sb.WriteString("</svg>")
}

// viewBoxValue returns the (unrounded) viewBox attribute value.
func (d *Document) viewBoxValue() string {
	if !d.viewBoxSet {
		return strconv.Itoa(0) + " " + strconv.Itoa(0) + " " + strconv.Itoa(d.width) + " " + strconv.Itoa(d.height)
	}
	return fmtNum(d.vbMinX) + " " + fmtNum(d.vbMinY) + " " + fmtNum(d.vbW) + " " + fmtNum(d.vbH)
}

func writeNode(sb *strings.Builder, n *node, depth int, indentUnit, nl string, precision int) {
	indent := strings.Repeat(indentUnit, depth)

	sb.WriteString(indent)
	sb.WriteString("<")
	sb.WriteString(n.tag)
	for _, a := range n.attrs {
		sb.WriteString(" ")
		sb.WriteString(a.name)
		sb.WriteString("=\"")
		sb.WriteString(escapeAttr(applyPrecision(a.value, a.name, precision)))
		sb.WriteString("\"")
	}

	if len(n.children) == 0 {
		sb.WriteString("/>")
		sb.WriteString(nl)
		return
	}

	sb.WriteString(">")
	sb.WriteString(nl)
	for _, c := range n.children {
		writeNode(sb, c, depth+1, indentUnit, nl, precision)
	}
	sb.WriteString(indent)
	sb.WriteString("</")
	sb.WriteString(n.tag)
	sb.WriteString(">")
	sb.WriteString(nl)
}

// applyPrecision rounds coordinate values found in "d" and "viewBox"
// attributes to the given precision. It is a no-op for any other attribute
// name, or when precision is negative.
func applyPrecision(value, attrName string, precision int) string {
	if precision < 0 {
		return value
	}
	if attrName != "d" && attrName != "viewBox" {
		return value
	}
	return roundNumbers(value, precision)
}

// escapeAttr escapes an attribute value for safe inclusion in double-quoted
// XML attribute syntax.
func escapeAttr(s string) string {
	return attrEscaper.Replace(s)
}

var attrEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&apos;",
)

// fmtNum formats a float64 using the shortest decimal representation that
// round-trips exactly (no exponent notation, no forced trailing zeros).
func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// PathBuilder builds the "d" attribute value of a <path> element from a
// sequence of MoveTo/LineTo/CubicTo/Close commands, so callers never need to
// hand-format path-data strings.
//
// The zero value is ready to use: new(PathBuilder) or var b PathBuilder both
// work. Methods return the builder so calls can be chained.
type PathBuilder struct {
	cmds []string
}

// MoveTo appends an "M x y" moveto command.
func (b *PathBuilder) MoveTo(x, y float64) *PathBuilder {
	b.cmds = append(b.cmds, "M"+fmtNum(x)+" "+fmtNum(y))
	return b
}

// LineTo appends an "L x y" lineto command.
func (b *PathBuilder) LineTo(x, y float64) *PathBuilder {
	b.cmds = append(b.cmds, "L"+fmtNum(x)+" "+fmtNum(y))
	return b
}

// CubicTo appends a "C x1 y1 x2 y2 x y" cubic Bézier curve command, with
// (x1,y1) and (x2,y2) as control points and (x,y) as the curve's end point.
func (b *PathBuilder) CubicTo(x1, y1, x2, y2, x, y float64) *PathBuilder {
	b.cmds = append(b.cmds, "C"+fmtNum(x1)+" "+fmtNum(y1)+" "+fmtNum(x2)+" "+fmtNum(y2)+" "+fmtNum(x)+" "+fmtNum(y))
	return b
}

// Close appends a "Z" closepath command.
func (b *PathBuilder) Close() *PathBuilder {
	b.cmds = append(b.cmds, "Z")
	return b
}

// String returns the accumulated "d" attribute value, e.g.
// "M0 0 L100 0 L100 100 L0 100 Z".
func (b *PathBuilder) String() string {
	return strings.Join(b.cmds, " ")
}
