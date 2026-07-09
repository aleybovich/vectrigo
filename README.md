# Vectrigo

Vectrigo is a portable, high-performance, **pure-Go** engine that converts raster
images (**PNG / JPEG / WEBP**) into clean, scalable **SVG vector paths**.

## Features

- **Pure Go, zero dependencies on C or system libraries.** Builds with
  `CGO_ENABLED=0` and cross-compiles to any `GOOS`/`GOARCH` with a single
  `go build` — Linux, macOS, Windows, ARM, WASM, no extra toolchain needed.
- **Stateless, streaming engine.** Reads from an `io.Reader`, writes to an
  `io.Writer`. No temp files, no global state — a single `Engine` instance is
  safe to share and call concurrently from many goroutines.
- **Two pipelines** — colour-quantization for crisp logos/icons and
  region-segmentation for photographic content (see [Architecture](#architecture-two-pipelines)).

## Install

```sh
go get github.com/aleybovich/vectrigo
```

Build with cgo disabled to enforce the pure-Go constraint:

```sh
CGO_ENABLED=0 go build ./...
```

## Usage

The simplest path — read a raster, write SVG, with recommended defaults:

```go
package main

import (
	"os"

	"github.com/aleybovich/vectrigo"
)

func main() {
	in, err := os.Open("input.png")
	if err != nil {
		panic(err)
	}
	defer in.Close()

	out, err := os.Create("output.svg")
	if err != nil {
		panic(err)
	}
	defer out.Close()

	cfg := vectrigo.DefaultConfig()
	cfg.Sensitivity = 70 // primary detail knob, 0-100

	if err := vectrigo.Vectorize(in, out, cfg); err != nil {
		panic(err)
	}
}
```

Reuse a single, concurrency-safe `Engine` across many conversions:

```go
eng := vectrigo.NewEngine(vectrigo.DefaultConfig())

// Safe to call from multiple goroutines simultaneously.
err := eng.Convert(reader, writer)
```

Start from `DefaultConfig()` and adjust from there. A bare `Config{}` means
`Sensitivity` 0 (maximum posterization), **not** the recommended defaults —
`Sensitivity`'s zero is a legitimate setting, so it cannot double as "unset".
`Sensitivity` (0-100) is the primary knob; `AutoK`, `K`, `TurdSize`, `AlphaMax`,
`Optimize`, `MaxDimensions`, `Workers`, and `Precision` are advanced overrides.

### Automatic colour count (`AutoK`)

By default the colour count `K` is derived from `Sensitivity`. As an alternative,
`AutoK` lets Vectrigo pick `K` **automatically** from the image's own colour
complexity — a flat, few-colour logo collapses to a small `K`, while a rich,
gradient-heavy photo gets a larger one.

```go
cfg := vectrigo.DefaultConfig()
cfg.AutoK = true // choose K automatically from the image; ignore Sensitivity
```

- **Off by default.** `AutoK` is `false` in both `DefaultConfig()` and the zero
  `Config{}`; leaving it off preserves today's exact behaviour.
- **Supersedes `Sensitivity`.** Set a `Sensitivity` or turn on `AutoK` — the two
  are meant as an either/or. When `AutoK` is on, **`Sensitivity` has no effect on
  `K`**: it is not even a ceiling. `K` is bounded by the usual safety clamps (a
  resolution-based maximum and the number of distinct colours present) and by an
  internal auto-selection ceiling (currently 64 colours) that keeps the multi-`K`
  scan fast. The library raises no error if both are set — `AutoK` simply wins for
  `K`, and `Sensitivity` is ignored for it. The `vectrigo-cli` tool presents the
  two as a mutually exclusive choice: `-i <img> --auto-k` for auto-K, `-i <img>
  -s <sensitivity>` for the manual knob, and it errors if you pass both.
- **`TurdSize` follows the chosen `K`.** Under `AutoK` the speckle threshold is
  derived from the auto-selected `K` (not from `Sensitivity`), preserving the
  usual "more colours ⇒ less speckle removal" coupling. An explicit `TurdSize`
  override still applies.
- **Explicit `K` still wins.** Setting `cfg.K > 0` is a hard override that beats
  `AutoK` (and `Sensitivity`).
- **Deterministic.** Auto-K uses the same seeded k-means as the rest of the
  pipeline, so a given image always yields the same `K` and byte-identical SVG.

How it works: Vectrigo measures the k-means distortion (within-cluster
sum-of-squares) for increasing `K` and stops at the "knee" — the smallest `K`
that already explains the bulk of the image's colour variation, so extra colours
would add detail with diminishing returns.

- **Tuning the knee with `AutoKTau`.** `AutoKTau` (a `float64`) is the residual
  distortion threshold for that knee: the smallest `K` whose distortion has
  dropped to this fraction of the single-cluster distortion is chosen. **Smaller
  ⇒ more colours / higher fidelity; larger ⇒ fewer colours.** The zero value
  (and a bare `Config{}`) means the default, `0.02`, which preserves today's
  auto-K output; it is clamped to a maximum of `0.5`. It only applies under
  `AutoK` and has no effect otherwise. At the default `0.02`, complex photos
  rarely reach the knee and saturate near the internal colour ceiling, so they
  all pick a similar large `K`; raising it (around `0.05`) trips the knee earlier
  so different complex photos **differentiate** into distinct, smaller `K` values
  that reflect their complexity — at the cost of coarser output. Push it too high
  and even simple images start losing real colours.

### Photo mode (`Photo` / `PhotoDetail` / `PhotoSimplify` / `PhotoEdge`)

The default pipeline **quantizes** colours — it clusters pixels globally by
colour, which is crisp on flat / logo art and is the right choice there. For
**photographic** content, `Photo` mode instead **segments** the image into many
small, spatially-connected regions (Felzenszwalb graph segmentation), each given
its own mean colour. The whole label map is then traced as **one planar
subdivision**, so adjacent regions share their exact boundary geometry and the
filled regions **tile the plane with no seams** — no background is needed.

```go
cfg := vectrigo.DefaultConfig()
cfg.Photo = true                                   // segmentation photo pipeline (best for photos)
cfg.PhotoDetail = 8                                 // optional σ_r detail dial; 0 keeps the default (12)
cfg.PhotoSimplify = vectrigo.PhotoSimplifySubtle    // optional node reduction; 0 (default) keeps every node
cfg.PhotoEdge = vectrigo.PhotoEdgeCrisp             // edge finish; crisp is the default
```

- **Off by default.** `Photo` is `false` in both `DefaultConfig()` and the zero
  `Config{}`; the quantization output is byte-identical while it stays off.
- **Either/or with the quantization knobs.** When `Photo` is on, `Sensitivity`,
  `K`, `AutoK`, `AutoKTau` and `TurdSize` have **no effect** (there is no colour
  clustering). `AlphaMax`, `Optimize`, `Precision`, `Workers` and `MaxDimensions`
  still apply.
- **`PhotoDetail` is the detail dial** (the bilateral range-sigma, σ_r). `0` (a
  bare `Config{}`, and `DefaultConfig`'s value) means the default `12`; it is
  clamped to `[4, 60]`. **Lower = punchier / more detail, higher = softer:**
  `~8` punchy (region count climbs, faces can over-segment), `12` balanced
  (default), `28+` soft / abstract (low-contrast shading and small text blend
  away).
- **`PhotoSimplify` is the node-count / file-size dial** (applies only under
  `Photo`), and is **opt-in**. Boundary smoothing leaves straight edges as long
  runs of *nearly* collinear corners, so with simplification off every boundary
  pixel of a straight edge becomes an output node. `PhotoSimplify` is the maximum
  perpendicular deviation, in pixels, tolerated when straightening those runs:
  `0` (the zero value, and `DefaultConfig`'s value; also any value `<= 0` or NaN)
  means **off** — maximum fidelity, every corner kept. A positive tolerance
  (clamped to `5.0`) enables it; use the tuned presets
  `vectrigo.PhotoSimplifySubtle` (`0.35`, visually near-lossless, ~3× fewer
  nodes) or `vectrigo.PhotoSimplifyAggressive` (`1.0`, smallest files, visibly
  coarser shapes). Simplification runs **once on the shared boundary graph**, so
  the two regions flanking any edge always simplify identically and the gapless
  tiling is preserved at every setting — no seams ever open.
- **`PhotoEdge` is the anti-aliasing finish** (applies only under `Photo`).
  `PhotoEdgeCrisp` (the zero value, and `DefaultConfig`'s value) disables edge
  anti-aliasing via `shape-rendering="crispEdges"` for the crispest, perfectly
  seam-free flat-vector look. `PhotoEdgeStroke` keeps anti-aliasing and seals the
  residual sub-pixel seams with a thin same-colour stroke on each region, for
  slightly softer edges. Any out-of-range value is clamped to crisp.

On the CLI these are `--photo`, `--sigma`, `--simplify` and `--edge`:

```sh
vectrigo-cli -i photo.png --photo                       # => photo.photo.svg (σ_r = 12, crisp, no simplify)
vectrigo-cli -i photo.png --photo --sigma 8             # => photo.photo.svg (σ_r = 8)
vectrigo-cli -i photo.png --photo --simplify subtle     # => photo.photo.svg (fewer nodes, near-lossless)
vectrigo-cli -i photo.png --photo --simplify aggressive # => photo.photo.svg (smallest file, coarser)
vectrigo-cli -i photo.png --photo --edge stroke         # => photo.photo.svg (stroked seams)
```

`--photo` is the third mutually-exclusive mode alongside `--sensitivity` and
`--auto-k` — exactly one is required. `--sigma`, `--simplify` and `--edge` are
only valid with `--photo` (`--simplify` takes `subtle` or `aggressive`, unset
means off; `--edge` takes `crisp` or `stroke`, unset means crisp), and the output
is written next to the input with a `.photo.svg` extension.

### Gapless mode (`Gapless`)

The quantization pipeline traces each palette colour's bitmap **independently**
(via `bitrace`). That has two visible consequences on complex images: adjacent
shapes can disagree by sub-pixel amounts along shared edges, letting the
background bleed through as dark seam streaks, and each colour becomes **one**
SVG path aggregating every same-coloured area across the whole image. `Gapless`
keeps the quantization (same `Sensitivity` / `AutoK` / `K` palette, same crisp
posterized detail) but traces the result with photo mode's shared-boundary
planar tracer instead: the k-means label map is split into spatially-connected
regions and traced as one planar subdivision, so adjacent shapes share exact
boundary geometry and **tile the plane with no seams**, and **every path is one
contiguous area**.

```go
cfg := vectrigo.DefaultConfig()
cfg.AutoK = true   // or Sensitivity / K — all quantization knobs apply as usual
cfg.Gapless = true // trace gapless: no seams, contiguous shapes
```

- **Off by default.** `Gapless` is `false` in both `DefaultConfig()` and the
  zero `Config{}`; while it stays off the quantization output is byte-identical
  to the historical engine.
- **All quantization knobs apply** — `Sensitivity`, `AutoK`, `AutoKTau`, `K`
  and `TurdSize` behave exactly as in the default pipeline. `TurdSize` keeps
  its meaning with one twist: a sub-threshold speck is **absorbed** into the
  neighbouring shape with the most similar colour (recoloured, not deleted),
  since deleting it would tear a hole in the tiling.
- **Built-in near-lossless denoise.** Quantizing photographic gradients
  scatters huge numbers of 1-2px specks between near-identical adjacent
  clusters — invisible noise that would otherwise dominate the path count.
  Gapless absorbs sub-3px specks whose nearest neighbour is colour-close
  (Euclidean RGB ≤ 25) — typically 2-5× fewer paths, and up to ~16× on flat
  art whose anti-aliased edges quantize into fragment noise — at no visible
  cost: higher-contrast specks (eye highlights, letter fragments, fine
  low-contrast text) are always kept. Set a negative `TurdSize` to disable
  all absorption for maximum fidelity.
- **`PhotoSimplify` and `PhotoEdge` apply** (they tune the shared region
  tracer); `PhotoDetail` does not (detail is governed by the quantization
  knobs). `AlphaMax` has no effect — boundaries are smoothed polylines, like
  photo mode, not fitted Bézier curves.
- **Precedence:** `Photo` wins when both `Photo` and `Gapless` are set (photo
  mode is already gapless).
- **File size:** maximum-fidelity gapless output has one path per connected
  area, which on a complex image at a high `K` (e.g. auto-K's ceiling of 64)
  can mean tens of thousands of paths. Lower `Sensitivity`, an explicit
  `TurdSize`, or `PhotoSimplify` all shrink it.

On the CLI, `--gapless` is a modifier on the two quantization modes and inserts
a `gapless` segment into the output name:

```sh
vectrigo-cli -i photo.png --auto-k --gapless             # => photo.gapless.svg
vectrigo-cli -i photo.png -s 70 --gapless                # => photo.70.gapless.svg
vectrigo-cli -i photo.png --auto-k --gapless --simplify subtle
```

It is an error together with `--photo`; `--simplify` and `--edge` are valid
with `--photo` or `--gapless`.

## Architecture: two pipelines

Vectrigo has **two vectorization pipelines**. `Convert` (in `vectrigo.go`)
decodes the raster once, then branches on `Config.Photo`:

- **Quantization — colour-first (default).** Cluster the pixels globally into
  `K` palette colours, then trace each colour's mask. Crisp on flat / logo /
  icon art. This is the default; on it, `Sensitivity` / `AutoK` / `K` etc. apply.
- **Segmentation — region-first (`--photo`, opt-in).** Split the image into many
  small, spatially-connected regions, colour each by its mean, then trace the
  whole label map as **one planar subdivision** (adjacent regions share their
  exact boundary, so fills tile with no seams). Far better on photographic /
  painterly images. Enabled by `Config.Photo` / `--photo`.

A third, hybrid configuration — **gapless** (`Config.Gapless` / `--gapless`,
see [Gapless mode](#gapless-mode-gapless)) — quantizes colours like the first
pipeline but traces the result with the second pipeline's shared-boundary
tracer (`internal/regionize` splits the k-means label map into connected
regions, `internal/regiontrace` traces them seam-free).

Both share the front-end (decode/normalize/optional downsample) and the SVG
writer (`minisvg`). Stage-by-stage:

| Stage | Quantization (default) | Segmentation (`Photo`) |
|---|---|---|
| Decode / normalize | `internal/normalize` | `internal/normalize` |
| Colour → regions | `internal/quantize` (seeded k-means → `K` colours) | `segment/` (Felzenszwalb graph segmentation → regions) |
| Trace to paths | `internal/pipeline` → `bitrace/` (one mask per colour) | `internal/regiontrace` (shared-boundary planar subdivision) |
| Assemble SVG | `internal/assemble.WriteSVG` | `internal/assemble.WriteRegions` |
| Output | `minisvg` | `minisvg` |

The repo is four modules: the root engine `github.com/aleybovich/vectrigo`
(PolyForm NC) plus three sibling libraries — [`bitrace`](bitrace/) (bitmap
tracer, PolyForm NC), [`segment`](segment/) (image segmentation, MIT), and
[`minisvg`](minisvg/) (SVG writer, MIT).

## Configuration reference

Every field of `vectrigo.Config`, with its type, default (as set by
`DefaultConfig()`), and effect. Build from `DefaultConfig()`, not a bare
`Config{}` — see the [zero-value caveat](#usage) above: a bare `Config{}`
leaves `Sensitivity` and `Precision` at `0` (not the recommended defaults),
even though for most *other* fields `0` conveniently means "derive" or "use
the default."

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `Sensitivity` | `int` | `50` | Primary 0-100 detail dial (see [Usage](#usage)). Drives the derived `(K, TurdSize)` pair when those are left `0`. Clamped to `[0, 100]`. `0` is a legitimate value (maximum posterization) — it is *not* a sentinel for "unset." No effect when `Photo` is `true`. |
| `AutoK` | `bool` | `false` | Selects `K` automatically from the image's colour complexity instead of deriving it from `Sensitivity` (see [Automatic colour count (`AutoK`)](#automatic-colour-count-autok)). When `true`, `Sensitivity` has no effect on `K`. Superseded by an explicit `K > 0`. No effect when `Photo` is `true`. |
| `AutoKTau` | `float64` | `0.02` | Residual-distortion "knee" threshold used by `AutoK` (see [Automatic colour count (`AutoK`)](#automatic-colour-count-autok)). Smaller ⇒ more colours / higher fidelity; larger ⇒ fewer colours. Zero value (and NaN, and a bare `Config{}`) resolves to the default `0.02`; clamped to a maximum of `0.5`. Only applies when `AutoK` is `true` and no explicit `K` override is set; otherwise inert. |
| `K` | `int` | `0` | Forces an exact cluster (colour) count, overriding both `Sensitivity`- and `AutoK`-derived values. `0` means derive. When `> 0` it is a hard override, clamped to `[2, maxKForPixels(W×H)]`, and never exceeds the image's distinct-colour count. Wins over `AutoK` as well as over `Sensitivity`. No effect when `Photo` is `true`. |
| `TurdSize` | `int` | `0` | Forces the speckle-area removal threshold in pixels (passed to `bitrace`). `0` means derive (from `Sensitivity`, or from the auto-selected `K` under `AutoK`); a **negative** value force-disables speckle removal entirely; a **positive** value is used as-is. No effect when `Photo` is `true`. |
| `AlphaMax` | `float64` | `1.0` | Corner/smoothness axis, independent of detail. Passed to `bitrace`, which clamps it to `[0, 1.334]`. Lower is more angular; higher is smoother. Applies in both quantization and `Photo` mode. |
| `Optimize` | `bool` | `true` | Enables `bitrace`'s looser curve fitting and `minisvg`'s minify + coordinate-rounding pass. Applies in both quantization and `Photo` mode. |
| `MaxDimensions` | `Dimensions{Width, Height}` | `{2048, 2048}` | Downsample ceiling bounding memory use. Inputs larger than this on either axis are high-quality downsampled first. Either axis `<= 0` falls back to the `2048` default for that axis. Applies in both quantization and `Photo` mode. |
| `Workers` | `int` | `0` (→ `runtime.NumCPU()`) | Tracing concurrency. `<= 0` resolves to `runtime.NumCPU()`; the effective value is further capped to the number of layers being traced. Applies in both quantization and `Photo` mode. |
| `Precision` | `int` | `2` | Coordinate decimal-place count used when `Optimize` is on. Clamped to `[0, 6]`. Applies in both quantization and `Photo` mode. |
| `Photo` | `bool` | `false` | Selects the region-first segmentation pipeline (see [Photo mode](#photo-mode-photo-photodetail-photosimplify-photoedge)) instead of the default colour-quantization pipeline. When `true`, `Sensitivity`, `K`, `AutoK`, `AutoKTau`, and `TurdSize` have no effect. When `false`, output is byte-identical to the historical quantization output regardless of `PhotoDetail`/`PhotoSimplify`/`PhotoEdge`. |
| `Gapless` | `bool` | `false` | Selects the gapless hybrid pipeline (see [Gapless mode](#gapless-mode-gapless)): same k-means quantization (all quantization knobs apply; `TurdSize` absorbs specks into their longest-border neighbour instead of deleting them), but traced with photo mode's shared-boundary tracer — no seams between shapes, every path one contiguous area. `PhotoSimplify`/`PhotoEdge` apply; `AlphaMax` and `PhotoDetail` do not. `Photo` wins if both are set. When `false`, quantization output is byte-identical to the historical engine. |
| `PhotoDetail` | `float64` | `12` (`segment.DefaultRangeSigma`) | Bilateral range-sigma (σ_r), the primary detail-vs-smoothness dial for `Photo` mode (see [Photo mode](#photo-mode-photo-photodetail-photosimplify-photoedge)). `0` (and NaN, and a bare `Config{}`) resolves to the default `12`; clamped to `[4, 60]`. Lower = punchier / more detail (region count climbs); higher = softer / more abstract. No effect when `Photo` is `false`. |
| `PhotoSimplify` | `float64` | `0` (off) | **Opt-in** boundary-simplification tolerance (px) for `Photo` mode (see [Photo mode](#photo-mode-photo-photodetail-photosimplify-photoedge)): the node-count / file-size vs fidelity dial. `0` (the default; and any value `<= 0` or NaN) means **off** — every boundary corner kept, maximum fidelity. A positive tolerance is used as-is, clamped to `5.0`. Tuned presets: `PhotoSimplifySubtle` (`0.35`, near-lossless, ~3× fewer nodes) and `PhotoSimplifyAggressive` (`1.0`, smallest files, coarser). Runs once on the shared boundary graph, so the gapless tiling is preserved at every setting. Applies in `Photo` and `Gapless` modes; no effect when both are `false`. |
| `PhotoEdge` | `PhotoEdge` | `PhotoEdgeCrisp` (zero value) | Anti-aliasing finish for `Photo` mode region edges (see [Photo mode](#photo-mode-photo-photodetail-photosimplify-photoedge)): `PhotoEdgeCrisp` (crisp, seam-free, `shape-rendering="crispEdges"`) or `PhotoEdgeStroke` (anti-aliased, seams sealed with a thin same-colour stroke). Any out-of-range value is clamped to `PhotoEdgeCrisp`. Applies in `Photo` and `Gapless` modes; no effect when both are `false`. |

## License

Vectrigo — the engine and the `vectrigo-cli` command — is licensed under the
[PolyForm Noncommercial License 1.0.0](LICENSE). **It is free for any
noncommercial use.** Commercial use is not granted by that license and requires a
separate commercial license from the copyright holder — contact Andrey Leybovich.

Third-party components and their licenses are reproduced in
[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).

It bundles two in-house libraries, each independently licensed with its own
`LICENSE` file: [`bitrace`](bitrace/) (PolyForm Noncommercial 1.0.0) and
[`minisvg`](minisvg/) (MIT).
