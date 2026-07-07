# Vectrigo

Vectrigo is a portable, high-performance, **pure-Go** engine that converts raster
images (**PNG / JPEG / WEBP**) into clean, scalable **SVG vector paths**.

It reads from an `io.Reader` and writes to an `io.Writer`, holds no global state,
and is safe to invoke concurrently from many goroutines.

## Non-negotiable constraints

These are **hard requirements**, not trade-offs. Any change that violates one is a
defect.

### Pure Go only — no cgo

- The build must succeed with `CGO_ENABLED=0`.
- No `os/exec`, no shell-outs, no spawning external binaries or processes.
- No external binary or system-library dependency of any kind (no ImageMagick,
  no libpng, no Potrace binary, etc.).
- Must **cross-compile cleanly** for any `GOOS`/`GOARCH` with a single `go build`
  (e.g. `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`, `js/wasm`).

### Permissive licenses only

Every dependency — direct or transitive — carries a **permissive** license. The
allow-list is: MIT, BSD-2-Clause / BSD-3-Clause, Apache-2.0, ISC, zlib, and
Unlicense / public-domain code dedications. Copyleft licenses (GPL / LGPL / AGPL,
any version) and non-software licenses (e.g. Creative Commons such as CC-BY-4.0)
are **forbidden**. Keeping the whole dependency graph permissive means Vectrigo
is free to set its own license terms (see [License](#license)) without inheriting
any copyleft obligations.

### Stateless engine, streaming I/O

- Input is an `io.Reader`; output is an `io.Writer`. No temp files, no on-disk
  caches.
- No global mutable state. All state lives on the stack or in per-call structs, so
  a single `Engine` is safe to share and use concurrently across goroutines.

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
	cfg.Sensitivity = 70 // primary detail knob, 0–100

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
`Sensitivity` (0–100) is the primary knob; `AutoK`, `K`, `TurdSize`, `AlphaMax`,
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

### Photo mode (`Photo` / `PhotoDetail` / `PhotoEdge`)

The default pipeline **quantizes** colours — it clusters pixels globally by
colour, which is crisp on flat / logo art and is the right choice there. For
**photographic** content, `Photo` mode instead **segments** the image into many
small, spatially-connected regions (Felzenszwalb graph segmentation), each given
its own mean colour. The whole label map is then traced as **one planar
subdivision**, so adjacent regions share their exact boundary geometry and the
filled regions **tile the plane with no seams** — no background is needed.

```go
cfg := vectrigo.DefaultConfig()
cfg.Photo = true                          // segmentation photo pipeline (best for photos)
cfg.PhotoDetail = 8                        // optional σ_r detail dial; 0 keeps the default (12)
cfg.PhotoEdge = vectrigo.PhotoEdgeCrisp    // edge finish; crisp is the default
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
- **`PhotoEdge` is the anti-aliasing finish** (applies only under `Photo`).
  `PhotoEdgeCrisp` (the zero value, and `DefaultConfig`'s value) disables edge
  anti-aliasing via `shape-rendering="crispEdges"` for the crispest, perfectly
  seam-free flat-vector look. `PhotoEdgeStroke` keeps anti-aliasing and seals the
  residual sub-pixel seams with a thin same-colour stroke on each region, for
  slightly softer edges. Any out-of-range value is clamped to crisp.

On the CLI these are `--photo`, `--sigma` and `--edge`:

```sh
vectrigo-cli -i photo.png --photo                    # => photo.photo.svg (σ_r = 12, crisp)
vectrigo-cli -i photo.png --photo --sigma 8          # => photo.photo.svg (σ_r = 8)
vectrigo-cli -i photo.png --photo --edge stroke      # => photo.photo.svg (stroked seams)
```

`--photo` is the third mutually-exclusive mode alongside `--sensitivity` and
`--auto-k` — exactly one is required. `--sigma` and `--edge` are only valid with
`--photo` (`--edge` takes `crisp` or `stroke`; unset means crisp), and the output
is written next to the input with a `.photo.svg` extension.

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
| `Sensitivity` | `int` | `50` | Primary 0–100 detail dial (see [Usage](#usage)). Drives the derived `(K, TurdSize)` pair when those are left `0`. Clamped to `[0, 100]`. `0` is a legitimate value (maximum posterization) — it is *not* a sentinel for "unset." No effect when `Photo` is `true`. |
| `AutoK` | `bool` | `false` | Selects `K` automatically from the image's colour complexity instead of deriving it from `Sensitivity` (see [Automatic colour count (`AutoK`)](#automatic-colour-count-autok)). When `true`, `Sensitivity` has no effect on `K`. Superseded by an explicit `K > 0`. No effect when `Photo` is `true`. |
| `AutoKTau` | `float64` | `0.02` | Residual-distortion "knee" threshold used by `AutoK` (see [Automatic colour count (`AutoK`)](#automatic-colour-count-autok)). Smaller ⇒ more colours / higher fidelity; larger ⇒ fewer colours. Zero value (and NaN, and a bare `Config{}`) resolves to the default `0.02`; clamped to a maximum of `0.5`. Only applies when `AutoK` is `true` and no explicit `K` override is set; otherwise inert. |
| `K` | `int` | `0` | Forces an exact cluster (colour) count, overriding both `Sensitivity`- and `AutoK`-derived values. `0` means derive. When `> 0` it is a hard override, clamped to `[2, maxKForPixels(W×H)]`, and never exceeds the image's distinct-colour count. Wins over `AutoK` as well as over `Sensitivity`. No effect when `Photo` is `true`. |
| `TurdSize` | `int` | `0` | Forces the speckle-area removal threshold in pixels (passed to `bitrace`). `0` means derive (from `Sensitivity`, or from the auto-selected `K` under `AutoK`); a **negative** value force-disables speckle removal entirely; a **positive** value is used as-is. No effect when `Photo` is `true`. |
| `AlphaMax` | `float64` | `1.0` | Corner/smoothness axis, independent of detail. Passed to `bitrace`, which clamps it to `[0, 1.334]`. Lower is more angular; higher is smoother. Applies in both quantization and `Photo` mode. |
| `Optimize` | `bool` | `true` | Enables `bitrace`'s looser curve fitting and `minisvg`'s minify + coordinate-rounding pass. Applies in both quantization and `Photo` mode. |
| `MaxDimensions` | `Dimensions{Width, Height}` | `{2048, 2048}` | Downsample ceiling bounding memory use. Inputs larger than this on either axis are high-quality downsampled first. Either axis `<= 0` falls back to the `2048` default for that axis. Applies in both quantization and `Photo` mode. |
| `Workers` | `int` | `0` (→ `runtime.NumCPU()`) | Tracing concurrency. `<= 0` resolves to `runtime.NumCPU()`; the effective value is further capped to the number of layers being traced. Applies in both quantization and `Photo` mode. |
| `Precision` | `int` | `2` | Coordinate decimal-place count used when `Optimize` is on. Clamped to `[0, 6]`. Applies in both quantization and `Photo` mode. |
| `Photo` | `bool` | `false` | Selects the region-first segmentation pipeline (see [Photo mode](#photo-mode-photo-photodetail-photoedge)) instead of the default colour-quantization pipeline. When `true`, `Sensitivity`, `K`, `AutoK`, `AutoKTau`, and `TurdSize` have no effect. When `false`, output is byte-identical to the historical quantization output regardless of `PhotoDetail`/`PhotoEdge`. |
| `PhotoDetail` | `float64` | `12` (`segment.DefaultRangeSigma`) | Bilateral range-sigma (σ_r), the primary detail-vs-smoothness dial for `Photo` mode (see [Photo mode](#photo-mode-photo-photodetail-photoedge)). `0` (and NaN, and a bare `Config{}`) resolves to the default `12`; clamped to `[4, 60]`. Lower = punchier / more detail (region count climbs); higher = softer / more abstract. No effect when `Photo` is `false`. |
| `PhotoEdge` | `PhotoEdge` | `PhotoEdgeCrisp` (zero value) | Anti-aliasing finish for `Photo` mode region edges (see [Photo mode](#photo-mode-photo-photodetail-photoedge)): `PhotoEdgeCrisp` (crisp, seam-free, `shape-rendering="crispEdges"`) or `PhotoEdgeStroke` (anti-aliased, seams sealed with a thin same-colour stroke). Any out-of-range value is clamped to `PhotoEdgeCrisp`. No effect when `Photo` is `false`. |

## License

Vectrigo — the engine and the `vectrigo-cli` command — is licensed under the
[PolyForm Noncommercial License 1.0.0](LICENSE). **It is free for any
noncommercial use.** Commercial use is not granted by that license and requires a
separate commercial license from the copyright holder — contact Andrey Leybovich.

This split is possible precisely because every dependency is permissively
licensed, which lets Vectrigo set its own terms. Third-party components and their
licenses are reproduced in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).

It bundles two in-house libraries, each independently licensed with its own
`LICENSE` file: [`bitrace`](bitrace/) (PolyForm Noncommercial 1.0.0) and
[`minisvg`](minisvg/) (MIT).
