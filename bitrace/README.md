# bitrace

`bitrace` is a pure-Go, permissively-licensed **bitmap-to-vector tracer**. It
converts a binary bitmap (a mask of on/off pixels) into smooth vector paths made
of straight lines and **cubic Bézier curves**, in the spirit of tools like
Potrace but implemented independently.

- **Pure Go, no cgo.** Builds with `CGO_ENABLED=0` and cross-compiles to any
  `GOOS`/`GOARCH`. No external binaries, no `os/exec`.
- **Dependency-light.** The only third-party dependency is
  [`honnef.co/go/curve`](https://honnef.co/go/curve) (Apache-2.0), used for
  cubic Bézier curve fitting.
- **Handles holes.** Outer boundaries and inner boundaries (holes) are both
  extracted, with holes flagged and a documented winding convention.

## Install

```sh
go get github.com/aleybovich/bitrace
```

## Usage

```go
package main

import (
	"fmt"

	"github.com/aleybovich/bitrace"
)

func main() {
	// Build a binary mask: true == "on". Index is y*W + x.
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

	for _, p := range paths {
		fmt.Printf("hole=%v commands=%d\n", p.IsHole, len(p.Commands))
	}
}
```

`Trace` returns an ordered slice of `Path`, each a closed contour expressed as a
sequence of `Command` values (`MoveTo`, `LineTo`, `CubicTo`, `Close`). A `Path`
carries an `IsHole` flag so callers can nest holes correctly. Turning the
commands into an SVG `d` attribute is a direct mapping — see the runnable
examples in `example_test.go`.

## Configuration

| Field      | Meaning                                                                 | Default |
| ---------- | ----------------------------------------------------------------------- | ------- |
| `TurdSize` | Speckle-area threshold (px). Contours enclosing fewer pixels are dropped. `0` disables removal. | `2`     |
| `AlphaMax` | Corner/smoothness threshold, clamped to `[0, 1.334]`. Lower = more angular (more corners); higher = smoother (more curves). | `1.0`   |
| `Optimize` | Looser curve fitting: fewer, larger Bézier segments (smaller output).   | `true`  |

Start from `bitrace.DefaultConfig()` and adjust from there.

## Algorithm

The pipeline is built entirely from general, public-domain techniques:

1. **Contour extraction** — crack-following border tracing on the pixel-edge
   lattice produces closed integer polygons for outer boundaries and holes.
2. **Noise suppression** — contours whose shoelace area is below `TurdSize` are
   discarded.
3. **Corner detection** — a windowed turning-angle metric, thresholded by
   `AlphaMax`, separates sharp corners from smooth runs.
4. **Curve fitting** — smooth runs are fitted with cubic Bézier curves via
   `honnef.co/go/curve`; corner joins and straight runs stay as line segments.
5. **Output** — the fitted commands are returned as an ordered slice of `Path`.

### Winding convention

Bitmaps use image coordinates (x right, y down). Outer contours are emitted with
a **negative** signed area (shoelace); hole contours with a **positive** signed
area and `IsHole == true`. The magnitude of the signed area equals the pixel
count the contour encloses.

## Clean-room provenance

**bitrace contains no Potrace lineage.** It is an independent, clean-room
implementation. It is **not** derived from, copied from, translated from, or
transpiled out of Potrace (which is GPL-2.0) or any Potrace port. It was built
solely from general, public-domain algorithm descriptions (border following,
the shoelace area formula, curvature-based corner detection) together with the
permissively-licensed `honnef.co/go/curve` package for cubic Bézier fitting.
This keeps bitrace free of copyleft obligations so it can be embedded in
proprietary software.

## License

MIT. See [LICENSE](LICENSE).
