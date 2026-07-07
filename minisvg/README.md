# minisvg

`minisvg` is a minimal, **zero-dependency** SVG builder and writer for Go.

It exists because the obvious off-the-shelf choice for programmatic SVG
generation (`ajstarks/svgo`) is licensed CC-BY-4.0 — a Creative Commons
license unsuitable for redistributable software and forbidden in
closed-source/commercial products. `minisvg` depends on nothing but the Go
standard library, so it carries no licensing risk of its own and can be
vendored, forked, extracted into its own repository, or shipped
independently without concern.

## What it does

- Builds an SVG document programmatically: a root `<svg>` with `width`,
  `height`, and a `viewBox` that defaults to `"0 0 width height"` and can be
  overridden with `SetViewBox`.
- Adds `<path>` elements (`d` + `fill`) and nested `<g>` groups (optionally
  with their own `fill`, inherited by children that don't set their own),
  to arbitrary nesting depth.
- Adds **stroked** `<path>` elements (`fill` + `stroke` + `stroke-width`) via
  `StrokedPath` — and its `Group` equivalent — supporting fill-only,
  stroke-only (`fill="none"`), and fill+stroke. The `stroke-width` is
  rounded like a coordinate under the configured precision.
- Sets a full-canvas **background** with `SetBackground`, emitted as a
  `<rect>` covering the whole user-space canvas (the viewBox extent when one
  is set via `SetViewBox`, otherwise the pixel dimensions passed to `New`)
  that always renders first (handy so any residual sub-pixel seams show a
  neutral backdrop instead of the white page).
- Sets the root `<svg>` element's **`shape-rendering`** attribute with
  `SetShapeRendering` (e.g. `"crispEdges"` to disable edge anti-aliasing, or
  `"auto"`/`"geometricPrecision"`); an empty value (the default) omits the
  attribute entirely.
- Provides a `PathBuilder` that assembles a path's `d` attribute from
  `MoveTo` / `LineTo` / `CubicTo` / `Close` commands, so callers never
  hand-format path-data strings.
- Streams the finished document to an `io.Writer` (`WriteTo`, implementing
  `io.WriterTo`), with an options variant (`WriteToOpts`) for:
  - **Minification** — stripping non-essential whitespace/newlines.
  - **Coordinate rounding** — rounding decimal coordinates in `d`,
    `viewBox`, and `stroke-width` attributes to a configurable precision
    (e.g. `12.345678` at precision `2` becomes `12.35`), using exact
    decimal-string arithmetic (not float64 math) so it is immune to binary
    floating-point rounding error.
- Emits valid, well-formed SVG/XML: the correct
  `xmlns="http://www.w3.org/2000/svg"` namespace and properly
  XML-escaped attribute values.

## Install

```sh
go get github.com/aleybovich/minisvg
```

## Usage

```go
package main

import (
	"os"

	"github.com/aleybovich/minisvg"
)

func main() {
	doc := minisvg.New(100, 100)

	// Override the default "0 0 width height" viewBox if needed.
	doc.SetViewBox(0, 0, 100, 100)

	// Disable edge anti-aliasing for crisp, pixel-aligned shapes.
	doc.SetShapeRendering("crispEdges")

	// A neutral full-canvas background (rendered first, before all content).
	doc.SetBackground("#202020")

	// A plain filled path.
	doc.Path("M0 0 L100 0 L100 100 L0 100 Z", "#ff0000")

	// A stroked path: same-color thin stroke seals anti-aliased seams
	// between adjacent region fills. Use fill "none" for a stroke-only path.
	doc.StrokedPath("M0 0 L100 0 L100 100 L0 100 Z", "#ff0000", "#ff0000", 0.5)

	// A group of paths sharing a fill.
	g := doc.Group("blue")
	pb := new(minisvg.PathBuilder).
		MoveTo(10, 10).
		LineTo(90, 10).
		CubicTo(95, 10, 95, 90, 90, 90).
		Close()
	g.Path(pb.String(), "")

	// Pretty-printed, unrounded (the default).
	doc.WriteTo(os.Stdout)

	// Minified with coordinates rounded to 2 decimal places.
	doc.WriteToOpts(os.Stdout, minisvg.WriteOptions{
		Minify:    true,
		Precision: 2,
	})
}
```

## API reference

### Color

```go
type Color string
```

`Color` is a plain string alias for an SVG color value, e.g. `"#1a2b3c"`,
`"red"`, or `"none"`. An empty `Color` (`""`) is special: it means "do not
emit this attribute at all", letting the element fall back to SVG's own
defaults or to a value inherited from an ancestor `<g>`. Pass an explicit
`"none"` to force *no* fill/stroke rather than inheriting one.

### Document

Construct a `Document` with `New`, then build it up with the methods below.
All mutator methods return the `Document` so calls can be chained.

| Signature | Description |
|---|---|
| `New(width, height int) *Document` | Creates a document with the given pixel dimensions, written verbatim as the `<svg>` `width`/`height` attributes. The `viewBox` defaults to `"0 0 width height"`. |
| `(*Document) SetViewBox(minX, minY, w, h float64) *Document` | Overrides the default `viewBox` (otherwise `"0 0 width height"`). |
| `(*Document) SetBackground(fill Color) *Document` | Sets a full-canvas background `<rect>` (viewBox-aware sizing/position) that is always emitted first, regardless of call order. Calling it again replaces the previous background. |
| `(*Document) SetShapeRendering(value string) *Document` | Sets the root `<svg>` element's `shape-rendering` attribute (e.g. `"crispEdges"`); `""` (the default) omits the attribute. |
| `(*Document) Path(data string, fill Color) *Document` | Appends a `<path d="..." fill="...">` to the document root. |
| `(*Document) StrokedPath(data string, fill, stroke Color, strokeWidth float64) *Document` | Appends a `<path>` with both fill and stroke. `stroke`/`stroke-width` are only emitted when `stroke != ""`; with `stroke == ""` the output is byte-identical to `Path`. |
| `(*Document) Group(fill Color) *Group` | Creates a `<g fill="...">`, appends it to the document root, and returns a `*Group` for adding nested content. |
| `(*Document) WriteTo(w io.Writer) (int64, error)` | Serializes the document with default formatting: indented, human-readable, no coordinate rounding. Implements `io.WriterTo`. |
| `(*Document) WriteToOpts(w io.Writer, opt WriteOptions) (int64, error)` | Serializes the document applying `WriteOptions` (minification and/or coordinate rounding). |

### Group

Obtained via `Document.Group` or `Group.Group`; mirrors the
path/group-adding API of `Document` so content nests to arbitrary depth. All
methods return the `*Group` so calls can be chained.

| Signature | Description |
|---|---|
| `(*Group) Path(data string, fill Color) *Group` | Appends a `<path d="..." fill="...">` inside this group. |
| `(*Group) StrokedPath(data string, fill, stroke Color, strokeWidth float64) *Group` | Appends a fill+stroke `<path>` inside this group; mirrors `Document.StrokedPath`. |
| `(*Group) Group(fill Color) *Group` | Creates a nested `<g fill="...">` inside this group and returns it. |

### PathBuilder

`PathBuilder` assembles a `<path>` element's `d` attribute from a sequence
of commands so callers never hand-format path-data strings. The zero value
is ready to use (`new(minisvg.PathBuilder)` or `var b minisvg.PathBuilder`).
All methods return the `*PathBuilder` so calls can be chained.

| Signature | Description |
|---|---|
| `(*PathBuilder) MoveTo(x, y float64) *PathBuilder` | Appends an `M x y` moveto command. |
| `(*PathBuilder) LineTo(x, y float64) *PathBuilder` | Appends an `L x y` lineto command. |
| `(*PathBuilder) CubicTo(x1, y1, x2, y2, x, y float64) *PathBuilder` | Appends a `C x1 y1 x2 y2 x y` cubic Bézier command, with `(x1,y1)`/`(x2,y2)` as control points and `(x,y)` as the end point. |
| `(*PathBuilder) Close() *PathBuilder` | Appends a `Z` closepath command. |
| `(*PathBuilder) String() string` | Returns the accumulated `d` value, e.g. `"M0 0 L100 0 L100 100 L0 100 Z"`. |

### WriteOptions

```go
type WriteOptions struct {
	Minify    bool
	Precision int
}
```

Controls `Document.WriteToOpts`:

- **`Minify`** — strips non-essential whitespace and newlines, producing a
  single compact line instead of an indented, one-tag-per-line document.
- **`Precision`** — number of decimal places that coordinates in the `d`,
  `viewBox`, and `stroke-width` attributes are rounded to (e.g. `12.345678`
  at precision `2` becomes `12.35`). Rounding is round-half-away-from-zero
  and trims trailing fractional zeros (`12.996` at precision `2` becomes
  `13`, not `13.00`). A negative `Precision` disables rounding entirely,
  leaving numbers exactly as supplied — this is what `Document.WriteTo`
  uses by default (`Precision: -1`).

## License

MIT — see [LICENSE](./LICENSE).
