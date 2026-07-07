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
  overridden.
- Adds `<path>` elements (`d` + `fill`) and nested `<g>` groups (optionally
  with their own `fill`, inherited by children that don't set their own).
- Provides a `PathBuilder` that assembles a path's `d` attribute from
  `MoveTo` / `LineTo` / `CubicTo` / `Close` commands, so callers never
  hand-format path-data strings.
- Streams the finished document to an `io.Writer` (`WriteTo`, implementing
  `io.WriterTo`), with an options variant (`WriteToOpts`) for:
  - **Minification** — stripping non-essential whitespace/newlines.
  - **Coordinate rounding** — rounding decimal coordinates in `d` and
    `viewBox` attributes to a configurable precision (e.g. `12.345678` at
    precision `2` becomes `12.35`), using exact decimal-string arithmetic
    (not float64 math) so it is immune to binary floating-point rounding
    error.
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

	// A plain filled path.
	doc.Path("M0 0 L100 0 L100 100 L0 100 Z", "#ff0000")

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

## License

MIT — see [LICENSE](./LICENSE).
