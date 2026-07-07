# Vectrigo — Project Plan & Specification

Vectrigo is a portable, high-performance, **pure-Go** service/engine that converts
raster images (PNG / JPEG / WEBP) into clean, scalable **SVG vector paths**.

It is **stateless**: it reads an image from an `io.Reader` and writes an SVG document
to an `io.Writer`. There is no disk state, no global mutable state, and no external
process dependency. The same binary runs identically on a laptop, a server, or inside
a scratch container on any platform.

This document is the authoritative specification for the project. It assumes the reader
has **zero** prior knowledge of any earlier discussion, research, or licensing
investigation. Every constraint and every design decision, including the rationale, is
captured here.

---

## Table of Contents

1. [Non-Negotiable Constraints](#1-non-negotiable-constraints)
2. [High-Level Architecture](#2-high-level-architecture)
3. [Approved Third-Party Dependencies](#3-approved-third-party-dependencies)
4. [Rejected Dependencies — Do NOT Use](#4-rejected-dependencies--do-not-use)
5. [Repository & Module Architecture](#5-repository--module-architecture)
6. [In-House Library: `minisvg`](#6-in-house-library-minisvg)
7. [In-House Library: `bitrace`](#7-in-house-library-bitrace)
8. [Engine Pipeline (Four Stages)](#8-engine-pipeline-four-stages)
9. [Engine Configuration](#9-engine-configuration)
10. [Public Engine API](#10-public-engine-api)
11. [Concurrency, Memory & Statelessness](#11-concurrency-memory--statelessness)
12. [Documentation & Licensing Deliverables](#12-documentation--licensing-deliverables)
13. [Build Order / Milestones](#13-build-order--milestones)
14. [Rationale / Design Decisions](#14-rationale--design-decisions)
15. [Open Questions / Tuning Notes](#15-open-questions--tuning-notes)

---

## 1. Non-Negotiable Constraints

These constraints are **hard requirements**. Any change that violates one of them is a
defect, not a trade-off. State them prominently in the top-level `README.md` as well.

### 1.1 Pure Go only — no cgo

- **No `cgo`.** The build must succeed with `CGO_ENABLED=0`.
- **No shell commands, no `os/exec`, no spawning external binaries or processes.**
- **No external binary or system-library dependency** of any kind (no ImageMagick,
  no libpng, no Potrace binary, etc.).
- Must **cross-compile cleanly** for any `GOOS`/`GOARCH` combination (e.g.
  `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`, `js/wasm`) with a
  single `go build`.

**Enforcement:** CI must run a matrix build with `CGO_ENABLED=0` across several
`GOOS`/`GOARCH` targets. A build that requires cgo fails the pipeline.

### 1.2 Permissive licenses ONLY

Every dependency — direct or transitive — must carry a **permissive** license. The
allow-list is:

- MIT
- BSD-2-Clause / BSD-3-Clause
- Apache-2.0
- ISC
- zlib
- Unlicense / public-domain (e.g. CC0-equivalent dedication for **code**)

The following are **FORBIDDEN**:

- **Copyleft:** GPL (any version), LGPL (any version), AGPL (any version).
- **Non-standard-for-software licenses:** e.g. Creative Commons licenses such as
  CC-BY-4.0. These lack the patent and warranty provisions expected of software
  licenses and are not on the permissive allow-list.

**Why:** Vectrigo is licensed under the **PolyForm Noncommercial License 1.0.0** — free
for noncommercial use; commercial use requires a separate license from Andrey Leybovich.
For Vectrigo to be free to set its own license terms this way, the entire dependency
graph must stay permissive: copyleft licenses would force source disclosure or
relicensing and would strip Vectrigo of the ability to choose its own terms. Creative
Commons licenses are inappropriate for redistributable software components. Keeping the
whole dependency graph permissive means Vectrigo (and its extractable sub-libraries) can
be licensed and shipped on its own terms without inheriting any copyleft obligation.

**Enforcement:** Add a license-audit step to CI (e.g. `go-licenses` or an equivalent
pure-Go scanner) that fails the build if any module in the graph resolves to a license
outside the allow-list. Review new dependencies against this list **before** adding
them.

### 1.3 Stateless engine, streaming I/O

- Input is an `io.Reader`; output is an `io.Writer`. No temp files, no caches on disk.
- No global mutable state. All state lives on the stack or in per-call structs so the
  engine is safe to invoke concurrently from many goroutines.

---

## 2. High-Level Architecture

```
                 ┌───────────────────────────────────────────────────────────┐
   io.Reader ──▶ │                    Vectrigo Engine                         │ ──▶ io.Writer
  (PNG/JPEG/WEBP)│                                                            │      (SVG)
                 │  I. Normalize   II. Quantize   III. Trace    IV. Assemble  │
                 │  ───────────    ───────────    ─────────     ───────────   │
                 │  decode +       k-means →      worker pool → order + fill  │
                 │  RGBA +         N binary       bitrace per   colors →      │
                 │  downsample     masks          mask          minisvg       │
                 └───────────────────────────────────────────────────────────┘
                        │                 │                │            │
                        ▼                 ▼                ▼            ▼
                  imaging          muesli/kmeans        bitrace      minisvg
                  (MIT)            (MIT)              (in-house)   (in-house)
                                                          │
                                                          ▼
                                                   honnef.co/go/curve
                                                      (Apache-2.0)
```

Three Go modules make up the project:

| Module | Role |
| --- | --- |
| `github.com/aleybovich/vectrigo` | The engine (root module): pipeline orchestration + public API. |
| `github.com/aleybovich/minisvg` | Zero-dependency SVG builder/writer (in-house, independently sellable). |
| `github.com/aleybovich/bitrace` | Pure-Go bitmap tracer (in-house, independently sellable). |

---

## 3. Approved Third-Party Dependencies

Only these external modules are approved. Each is permissively licensed. Do **not** add
anything else without re-running the license audit and updating this table.

| Import path | License | Purpose |
| --- | --- | --- |
| `github.com/disintegration/imaging` | MIT | Image loading, resizing, filtering, RGBA normalization. Used in Stage I. |
| `github.com/muesli/kmeans` | MIT | k-means clustering for color quantization. Used in Stage II. |
| `honnef.co/go/curve` | Apache-2.0 | High-quality 2D curve math; a Go port of Rust's `kurbo` (Raph Levien's curve-fitting lineage). Used for **cubic Bézier curve fitting** inside `bitrace`. |
| `golang.org/x/image` (incl. `golang.org/x/image/webp`) | BSD-3-Clause | WEBP decoding (and any supplementary image codecs) beyond the Go standard library. |
| Go standard library (`image`, `image/png`, `image/jpeg`, `image/draw`, `sync`, `io`, `runtime`, …) | BSD-3-Clause | Decoding PNG/JPEG, RGBA handling, concurrency primitives, streaming I/O. |

Notes:

- **WEBP is decode-only** via `golang.org/x/image/webp` (there is no pure-Go WEBP
  encoder needed — Vectrigo only *reads* WEBP; it *writes* SVG).
- Only `honnef.co/go/curve` is allowed as a dependency of `bitrace`. Keep `bitrace`
  otherwise dependency-light (see §7).
- `minisvg` must have **zero** third-party dependencies (stdlib only).

---

## 4. Rejected Dependencies — Do NOT Use

This section exists so that nobody re-adds a forbidden dependency later. If you are
tempted to pull in one of these, stop: the reason it was rejected is recorded here.

### 4.1 Potrace ports — FORBIDDEN (GPL-2.0)

- `github.com/gotranspile/gotrace`
- `github.com/dennwc/gotrace`

These are transpilations/ports of **Potrace** (originally C, licensed **GPL-2.0**).
Even though the resulting code is pure Go, **the GPL is inherited** through the port.
Using them would make Vectrigo a GPL derivative work — incompatible with Vectrigo's
PolyForm Noncommercial licensing, which depends on keeping the dependency graph
permissive and copyleft-free. **FORBIDDEN.**

> **This is the reason Vectrigo builds its own tracer (`bitrace`) instead of using an
> off-the-shelf Potrace port.**
>
> **IMPORTANT DESIGN RULE:** Vectrigo's own tracer must be an **independent,
> clean-room implementation** built from permissive building blocks and general,
> public-domain algorithms. It must **NOT** be derived from, copied from, translated
> from, or transpiled out of Potrace source (or any Potrace port), in whole or in
> part. Reading Potrace source to "see how it's done" contaminates provenance — do not
> do it. Build from published, general-purpose, public-domain algorithm descriptions
> only (see §7).

### 4.2 `github.com/ajstarks/svgo` — FORBIDDEN (CC-BY-4.0)

`svgo` is licensed **CC-BY-4.0**, a Creative Commons license that is not appropriate
for software: it provides no patent grant and no warranty disclaimer tailored to code,
and it is off the permissive allow-list in §1.2. **FORBIDDEN.**

> **This is the reason Vectrigo writes SVG using its own in-house `minisvg` library
> instead** (see §6).

---

## 5. Repository & Module Architecture

During development, all three modules live in the single `vectrigo` repository as
**separate Go modules**, wired together with a **`go.work` workspace** (and/or
`replace` directives). Each module sits in its own directory with its own `go.mod`,
`LICENSE`, and `README.md`. This layout lets `minisvg` and `bitrace` be **extracted
into standalone repositories later with no code changes** (their import paths already
match their intended public module paths).

### 5.1 Directory layout

```
vectrigo/                            # git repository root
├── go.work                          # ties the three modules together for dev
├── LICENSE                          # engine license: PolyForm Noncommercial 1.0.0
├── README.md                        # top-level project readme (states constraints)
├── plan.md                          # this document
│
├── go.mod                           # module github.com/aleybovich/vectrigo
├── vectrigo.go                      # public package `vectrigo`: Config, Vectorize/Engine
├── example_test.go                  # runnable godoc examples for the engine
├── internal/
│   ├── normalize/                   # Stage I: decode + RGBA + downsample
│   │   └── normalize.go
│   ├── quantize/                    # Stage II: k-means → N binary masks
│   │   └── quantize.go
│   ├── pipeline/                    # Stage III orchestration: worker pool over layers
│   │   └── pipeline.go
│   └── assemble/                    # Stage IV: order/group paths, fill colors, serialize
│       └── assemble.go
│
├── minisvg/                         # module github.com/aleybovich/minisvg
│   ├── go.mod                       # zero third-party deps
│   ├── LICENSE                      # MIT
│   ├── README.md
│   ├── doc.go                       # package doc
│   ├── minisvg.go                   # builder + writer
│   ├── minify.go                    # minification / coordinate rounding
│   ├── minisvg_test.go
│   └── example_test.go
│
└── bitrace/                         # module github.com/aleybovich/bitrace
    ├── go.mod                       # only dep: honnef.co/go/curve
    ├── LICENSE                      # PolyForm Noncommercial 1.0.0
    ├── README.md
    ├── doc.go
    ├── bitrace.go                   # public API: Trace(mask, cfg) → paths
    ├── contour.go                   # Stage 1: border following / marching squares
    ├── segment.go                   # Stage 3: corner detection & segmentation
    ├── fit.go                       # Stage 4: cubic Bézier fitting via honnef.co/go/curve
    ├── bitrace_test.go
    └── example_test.go
```

### 5.2 `go.work` example

```go
// go.work at repo root
go 1.22

use (
	.
	./minisvg
	./bitrace
)
```

With the workspace active, the engine imports `github.com/aleybovich/minisvg` and
`github.com/aleybovich/bitrace` by their real paths, but the local copies are used.
When the sub-libraries are later published to their own repos, delete their `use`
entries (or the whole `go.work`) and the engine resolves them from their published
tags with **no source changes**.

> Alternative for CI reproducibility: instead of (or in addition to) `go.work`, add
> `replace github.com/aleybovich/minisvg => ./minisvg` and
> `replace github.com/aleybovich/bitrace => ./bitrace` to the engine's `go.mod` during
> development. Remove the `replace` lines at extraction time.

---

## 6. In-House Library: `minisvg`

**Import path:** `github.com/aleybovich/minisvg`
**License:** MIT
**Dependencies:** **none** (Go standard library only).

A minimal, zero-dependency SVG builder/writer. It exists because the obvious
off-the-shelf choice (`svgo`) is CC-BY-4.0 and therefore forbidden (§4.2). `minisvg` is
designed to be **independently open-sourceable / sellable**, so its documentation and
test quality are first-class.

### 6.1 Responsibilities

- Construct an SVG document programmatically: a root `<svg>` element with `width`,
  `height`, and `viewBox`.
- Add `<path>` elements carrying `d` (path data) and `fill` (color).
- Add group elements `<g>` (optionally with a fill or transform), and nest paths inside
  them.
- Write the document to an `io.Writer` (streaming; no need to hold the full string in
  memory beyond what the writer buffers).
- **Built-in minification / optimization pass:**
  - Strip unnecessary whitespace and newlines.
  - Truncate/round coordinate decimals to a configurable precision
    (e.g. `12.345678` at precision 2 → `12.35`).

### 6.2 Sketch API

```go
package minisvg

// Color is an SVG fill value, e.g. "#1a2b3c" or "none".
type Color string

// Document is the root SVG builder.
type Document struct { /* width, height, viewBox, children */ }

// New returns a document with the given pixel dimensions. The viewBox defaults to
// "0 0 width height".
func New(width, height int) *Document

// SetViewBox overrides the default viewBox.
func (d *Document) SetViewBox(minX, minY, w, h float64) *Document

// Path appends a <path> with the given path-data string and fill color.
func (d *Document) Path(data string, fill Color) *Document

// Group creates and appends a <g>; paths added to the returned Group are nested in it.
func (d *Document) Group(fill Color) *Group

// Group mirrors Document's path-adding API for nested content.
type Group struct { /* ... */ }
func (g *Group) Path(data string, fill Color) *Group

// PathBuilder builds a `d` attribute from move/line/cubic commands so callers do not
// hand-format strings. (bitrace emits commands; the engine feeds them here.)
type PathBuilder struct { /* ... */ }
func (b *PathBuilder) MoveTo(x, y float64) *PathBuilder
func (b *PathBuilder) LineTo(x, y float64) *PathBuilder
func (b *PathBuilder) CubicTo(x1, y1, x2, y2, x, y float64) *PathBuilder
func (b *PathBuilder) Close() *PathBuilder
func (b *PathBuilder) String() string   // the `d` value

// WriteOptions controls serialization.
type WriteOptions struct {
	Minify    bool // strip whitespace/newlines
	Precision int  // decimal places to round coordinates to (e.g. 2)
}

// WriteTo serializes the document to w. Implements io.WriterTo semantics.
func (d *Document) WriteTo(w io.Writer) (int64, error)

// WriteToOpts serializes with explicit minification/precision options.
func (d *Document) WriteToOpts(w io.Writer, opt WriteOptions) (int64, error)
```

### 6.3 Requirements

- Fully documented: package doc (`doc.go`), godoc on every exported symbol, and at
  least one runnable `Example` in `example_test.go`.
- Independently unit-tested: builder output correctness, viewBox handling, group
  nesting, XML-escaping of attribute values, and the minifier (whitespace stripping +
  coordinate rounding at several precisions, including rounding-half cases and negative
  coordinates).
- Emit valid, well-formed XML/SVG (correct namespace `xmlns="http://www.w3.org/2000/svg"`,
  properly escaped attributes).
- **No third-party imports.** This is a hard rule for `minisvg`.
- Designed so it can be dropped into its own repository unchanged.

---

## 7. In-House Library: `bitrace`

**Import path:** `github.com/aleybovich/bitrace` (name is a placeholder the owner may
rename).
**License:** PolyForm Noncommercial License 1.0.0 (same as the engine).
**Dependencies:** only `honnef.co/go/curve` (Apache-2.0). Keep it otherwise
dependency-light.

`bitrace` is Vectrigo's permissive, pure-Go replacement for Potrace-style
functionality. It converts a **binary bitmap (mask)** into smooth vector paths made of
**cubic Bézier contours**. Because it may be sold separately, it must be **clean-room**:
independent implementation, well documented, well tested.

> **It must NOT be a Potrace port** (see the design rule in §4.1). Build it from
> general, public-domain algorithm descriptions only.

### 7.1 Input / output

- **Input:** a binary mask — a 2-D grid of on/off pixels. Represent it with a flat
  slice for cache-friendliness, e.g.:

  ```go
  type Bitmap struct {
      W, H int
      Bits []bool // len == W*H; index = y*W + x; true == "on"
  }
  ```

  (A packed `[]uint64` bitset is an acceptable optimization later; start with `[]bool`
  or `[]byte` for clarity and correctness.)

- **Output:** vector path data — a sequence of move/line/cubic commands per contour —
  returned in a neutral form the engine serializes with `minisvg`:

  ```go
  type CmdKind int
  const (
      MoveTo CmdKind = iota
      LineTo
      CubicTo
      Close
  )

  type Command struct {
      Kind CmdKind
      // Points are used per-kind:
      //   MoveTo/LineTo: P (end point)
      //   CubicTo:       C1, C2 control points, P end point
      C1, C2, P Point
  }

  type Point struct{ X, Y float64 }

  type Path struct {
      Commands []Command
      // Winding/hole info may be attached so the engine can nest holes correctly.
      IsHole bool
  }
  ```

### 7.2 Algorithm pipeline

Implement these five steps. All are standard, general, public-domain techniques —
**not** Potrace-specific code.

1. **Contour extraction.**
   Trace the boundaries of the "on" regions of the binary mask into ordered point
   polylines. Use a standard public-domain algorithm — **Moore-neighbor tracing /
   Suzuki-Abe border following**, or **marching squares**. Must handle **outer
   boundaries and holes** (inner contours), producing correctly-wound closed
   polylines (e.g. outer contours counter-clockwise, holes clockwise, or a documented
   convention with `IsHole` set).

2. **Noise suppression (`TurdSize`).**
   Discard contours whose **enclosed area** is smaller than the `TurdSize` threshold
   (speckle / "turd" removal). Compute area via the shoelace formula on the contour
   polygon. Removing a hole contour re-fills that region; removing an outer contour
   drops the speckle entirely.

3. **Corner detection & segmentation.**
   Split each contour polyline into **segments** at high-curvature "corner" vertices,
   separating them from smooth runs. The corner-vs-smooth decision is governed by
   **`AlphaMax`** (higher = treat more vertices as smooth → smoother output; lower =
   more vertices treated as corners → more angular output).

   > The general approach used by the public-domain **ImageTracerJS** project
   > (**Unlicense**) is a fine *conceptual* reference for the corner/segment logic —
   > **algorithm only**, and it is permissively licensed. Do not copy code; reimplement
   > the idea.

4. **Curve fitting.**
   Fit **cubic Bézier** curves to the smooth segments using `honnef.co/go/curve` (the
   kurbo port). Keep detected corners as **line joins** (`LineTo`) rather than forcing
   a curve through them. When `Optimize` is enabled, apply optional path
   simplification/decimation (e.g. merge near-collinear points, drop redundant control
   points) before/after fitting.

5. **Output.**
   Return the ordered `[]Path` (move/line/cubic commands, holes flagged). Vectrigo
   serializes these into SVG `d` strings via `minisvg`'s `PathBuilder`.

### 7.3 Config

```go
type Config struct {
	// TurdSize is the speckle-area threshold in pixels. Contours enclosing fewer
	// than TurdSize pixels are discarded. Default: 2.
	TurdSize int

	// AlphaMax is the corner/smoothness threshold. Lower = more angular (more
	// corners); higher = smoother (more curves). Default: 1.0.
	AlphaMax float64

	// Optimize enables path simplification/decimation and coordinate rounding.
	// Default: true.
	Optimize bool
}
```

### 7.4 Public API sketch

```go
package bitrace

// Trace converts a binary mask into vector paths (cubic Bézier contours).
func Trace(bm Bitmap, cfg Config) ([]Path, error)
```

### 7.5 Requirements

- **Independent / clean-room** implementation (no Potrace lineage; see §4.1).
- Fully documented: package doc, godoc on all exported symbols, runnable examples.
- Independently unit-tested, including **simple synthetic bitmaps** with known-correct
  outputs:
  - A filled rectangle → one closed rectangular contour.
  - A filled disk → a smooth near-circular contour (validates cubic fitting).
  - A donut (disk with a hole) → outer + inner (hole) contour with correct winding.
  - A single-pixel speckle → removed when `TurdSize` ≥ 2.
  - A staircase edge → corner behavior varies sensibly with `AlphaMax`.
- Dependency-light: **only** `honnef.co/go/curve`.
- Extraction-ready as its own repository.

---

## 8. Engine Pipeline (Four Stages)

The engine (`github.com/aleybovich/vectrigo`) orchestrates four stages. Each maps to an
`internal/` package.

### Stage I — Normalization & pre-processing (`internal/normalize`)

1. Read the encoded image bytes from the `io.Reader`.
2. Decode PNG / JPEG / WEBP. Use the standard library for PNG/JPEG and
   `golang.org/x/image/webp` for WEBP. Detect format by content (image decoder
   registration) rather than by filename (there is no filename — it's a stream).
3. **Normalize to RGBA** color space (via `imaging`), so downstream stages see one
   consistent pixel layout.
4. If either dimension exceeds `MaxDimensions` (default **2048 × 2048**),
   **high-quality downsample** with `imaging.Resize` (e.g. Lanczos) preserving aspect
   ratio, to bound memory and prevent OOM on large inputs. Record the scale factor so
   the output `viewBox` can map back to the original coordinate space if desired.

Output of Stage I: an in-memory RGBA image (flat pixel buffer) within the size ceiling.

### Stage II — Color quantization (`internal/quantize`)

1. Convert the pixel grid into a set of `(R, G, B)` feature points (one 3-D observation
   per pixel; alpha handled per policy — e.g. treat fully-transparent pixels as
   background/off).
2. Run **k-means** (`github.com/muesli/kmeans`) with `K` centroids to obtain `K`
   representative colors.
3. Produce `N` (= number of non-empty clusters, ≤ `K`) **binary masks**: for each
   centroid color, a mask where pixels assigned to that cluster are `1` (on) and all
   others are `0` (off). Store masks as **flat slices** (see §11).

Output of Stage II: `N` `bitrace.Bitmap` masks, each paired with its centroid color.

### Stage III — Vectorization / tracing (`internal/pipeline`)

1. Process the `N` layers **concurrently** with a **worker pool**
   (`sync.WaitGroup` + buffered channels; worker count defaults to `GOMAXPROCS`/
   `NumCPU`).
2. For each layer: hand its binary mask to `bitrace.Trace` with the configured
   `TurdSize` / `AlphaMax` / `Optimize`, receive back Bézier path data.
3. Collect results keyed by the layer's centroid color. Preserve enough per-layer
   metadata (centroid color, cluster size / total area) to drive ordering in Stage IV.

Output of Stage III: a set of `{color, []bitrace.Path}` results.

### Stage IV — SVG assembly (`internal/assemble`)

1. Aggregate all traced paths.
2. Map each layer's paths to its centroid color as the `fill`.
3. **Order / group paths by cluster so larger background shapes render *behind* smaller
   foreground ones.** SVG has no z-index; paint order == document order. Emit
   large-area clusters first (bottom of the stack) and smaller clusters later (on top)
   to avoid occlusion where small foreground shapes get hidden behind big fills. Group
   each cluster's paths in a `<g fill="...">` for compactness.
4. Serialize with `minisvg`. When `Optimize` is set, run the minification /
   coordinate-rounding pass (whitespace stripping + decimal precision).
5. Stream the resulting SVG to the caller's `io.Writer`.

---

## 9. Engine Configuration

```go
package vectrigo

type Dimensions struct {
	Width  int
	Height int
}

type Config struct {
	// Sensitivity is the PRIMARY, high-level detail dial and the one knob most
	// callers ever touch. It is an integer PERCENTAGE, 0-100. Default: 50.
	//
	//   - Raise it when meaningful detail disappeared (image looks too flat/merged).
	//   - Lower it when the output shows "strange artifacts" (banding, noise shards).
	//
	// Whole-number percent (not a 0.0-1.0 float, not decimals): it is unambiguous to
	// read/set, and finer precision would be false — Sensitivity maps down to an
	// integer color count K, which has fewer distinct steps than 0-100 already gives.
	//
	// NOTE ON THE ZERO VALUE: 0 is a real setting (0% = maximum posterization), so it
	// cannot double as "unset." Start from DefaultConfig() rather than a bare
	// Config{} — see DefaultConfig and §10.
	//
	// The engine maps Sensitivity onto a coupled (K, TurdSize) pair along a tuned
	// curve: higher Sensitivity raises the color count K, while TurdSize is chosen
	// automatically to suppress the noise speckles a given K would otherwise surface
	// — held firm through the low-to-mid range and eased off only as you approach
	// maximum detail. That coupling is what makes a single dial behave intuitively:
	// raising K on its own floods the output with noise shards, whereas Sensitivity
	// keeps the mid-range clean and exposes fine noise only near the very top —
	// exactly where the user learns to "back off a bit."
	//
	// If any of the advanced overrides below (K, TurdSize) are set to a non-zero
	// value, that explicit value wins and is NOT derived from Sensitivity.
	Sensitivity int

	// --- Advanced overrides (leave zero to derive from Sensitivity) ---

	// K forces an exact number of color clusters. More clusters = more colors/detail
	// and larger output. Zero => derived from Sensitivity. When set, it is still
	// clamped relative to input resolution (see bounds below).
	K int

	// TurdSize forces an exact speckle threshold: ignore contours smaller than this
	// many pixels. Passed through to bitrace. Zero => derived from Sensitivity.
	TurdSize int

	// AlphaMax is the corner threshold — a SEPARATE axis from detail/sensitivity.
	// Lower = more jagged/angular curves; higher = smoother. It does not change how
	// much detail is kept, only how the retained outlines are drawn. Passed through
	// to bitrace. Default: 1.0.
	AlphaMax float64

	// Optimize enables path simplification + coordinate decimal rounding (in bitrace
	// and in the minisvg minifier). Default: true.
	Optimize bool

	// MaxDimensions is the downsample ceiling used to bound memory. Inputs larger
	// than this (in either axis) are high-quality downsampled before processing.
	// Default: 2048 x 2048.
	MaxDimensions Dimensions

	// Workers is the tracing concurrency. Default: runtime.NumCPU() (equivalently
	// GOMAXPROCS(0)). Values <= 0 mean "use the default".
	Workers int

	// Precision is the number of decimal places for coordinate rounding when
	// Optimize is on. Default: 2.
	Precision int
}

// DefaultConfig returns the recommended defaults and is the intended entry point
// (build from it, then tweak). Sensitivity 50 maps to roughly K=16 / TurdSize=2 with
// the reference mapping; K and TurdSize are left zero so they stay derived from
// Sensitivity unless the caller overrides them.
func DefaultConfig() Config {
	return Config{
		Sensitivity:   50, // 0-100 percent
		K:             0, // 0 => derived from Sensitivity
		TurdSize:      0, // 0 => derived from Sensitivity
		AlphaMax:      1.0,
		Optimize:      true,
		MaxDimensions: Dimensions{Width: 2048, Height: 2048},
		Workers:       0, // 0 => runtime.NumCPU()
		Precision:     2,
	}
}
```

The reference `Sensitivity → (K, TurdSize)` mapping (calibrated during the tuning pass,
§13) — treat these as a starting curve, not final numbers:

| `Sensitivity` | Derived `K` | Derived `TurdSize` | Feel |
| --- | --- | --- | --- |
| 0 | ~4 | ~8 | Poster/flat: few colors, aggressive noise removal. |
| 25 | ~8 | ~4 | Simplified. |
| 50 (default) | ~16 | ~2 | Balanced detail vs. cleanliness. |
| 75 | ~32 | ~1 | Detailed. |
| 100 | ~64 | ~0 | Maximum detail; keeps small shapes (accept some noise). |

Note the coupling direction: as `Sensitivity` rises, `K` rises (more color detail) while
`TurdSize` starts high (aggressive cleanup at the flat/poster end) and relaxes toward 0
as you approach maximum detail. Because the speckle threshold stays firm through the
low-to-mid range, the extra micro-clusters a higher `K` surfaces are absorbed rather than
shown — visible noise appears only near `Sensitivity` 100, where the caller is
deliberately trading cleanliness for maximum detail.

### 9.1 Field reference & bounds

| Field | Meaning | Default | Bounds / clamping |
| --- | --- | --- | --- |
| `Sensitivity` | **Primary detail dial** — integer percent. Drives derived `K` + `TurdSize`. | 50 | Clamp to `[0, 100]`. This is the knob 99% of callers use. 0 is a real value (max posterization), so build from `DefaultConfig()` — a bare `Config{}` means 0%, not the default. |
| `K` | *Advanced override:* exact color-cluster count. | 0 (derive) | 0 → derive from `Sensitivity`. When set, min 2 and **clamped relative to input resolution** to prevent runaway memory: e.g. `K = min(K, maxKForPixels(W*H))`. Never exceeds the number of distinct colors present. |
| `TurdSize` | *Advanced override:* speckle area threshold (px). | 0 (derive) | 0 → derive from `Sensitivity`. Set explicitly to force a threshold; a forced 0 disables speckle removal. |
| `AlphaMax` | Corner/smoothness threshold (separate axis, not detail). | 1.0 | Typically `[0, ~1.334]`; clamp to a sane range. |
| `Optimize` | Simplification + coordinate rounding. | true | — |
| `MaxDimensions` | Downsample ceiling (memory bound). | 2048×2048 | Min 1×1. Values ≤ 0 mean "use default". |
| `Workers` | Tracing concurrency. | `NumCPU()` | ≤ 0 → default. Never spawn more workers than layers `N`. |
| `Precision` | Coordinate decimal places when `Optimize`. | 2 | 0-6 typical. |

A `Config.normalize()`/`validate()` helper should fill defaults for zero-valued fields
and apply the clamps above before the pipeline runs. Most zero fields default cleanly
(`K`/`TurdSize` derive from `Sensitivity`; `Workers` → `NumCPU()`; `MaxDimensions`/
`Precision` → their defaults).

**The one exception is `Sensitivity`:** 0 is a legitimate value (0% = maximum
posterization), so it cannot be auto-promoted to the default — a bare `Config{}` yields
`Sensitivity == 0`, not 50. Therefore the intended idiom is to **start from
`DefaultConfig()`** and adjust from there, not to hand-build a `Config{}`. (If a caller
truly wants `Config{}` to auto-default to 50%, the alternative is to model `Sensitivity`
as a `*int` where `nil` means "unset"; the plan chooses the plain-`int` + `DefaultConfig()`
idiom for simplicity.)

---

## 10. Public Engine API

Provide **both** a simple function and an `Engine` type (the function delegates to the
type). Keep the surface small and streaming.

```go
package vectrigo

import "io"

// Vectorize reads a raster image (PNG/JPEG/WEBP) from r, converts it to SVG using cfg,
// and writes the SVG document to w. It is stateless and safe for concurrent use.
// Build cfg from DefaultConfig() and adjust from there (a bare Config{} means
// Sensitivity 0% / max posterization, not the default — see §9.1).
func Vectorize(r io.Reader, w io.Writer, cfg Config) error

// Engine is a reusable, stateless converter. It holds validated configuration and no
// mutable per-call state, so a single Engine may be shared across goroutines.
type Engine struct {
	cfg Config
}

// NewEngine returns an Engine with the given config (zero values filled from defaults
// and clamped).
func NewEngine(cfg Config) *Engine

// Convert performs the full pipeline: r (raster) -> w (SVG).
func (e *Engine) Convert(r io.Reader, w io.Writer) error
```

Usage:

```go
cfg := vectrigo.DefaultConfig()
cfg.Sensitivity = 70 // the primary knob: more detail (0-100)
if err := vectrigo.Vectorize(inputReader, outputWriter, cfg); err != nil {
	log.Fatal(err)
}
```

Error handling: return wrapped, descriptive errors (`fmt.Errorf("vectrigo: decode: %w", err)`)
for decode failures, empty input, unsupported formats, and internal stage failures. Do
not panic on bad input.

---

## 11. Concurrency, Memory & Statelessness

### 11.1 Concurrency

- Trace the `N` color layers with a **worker pool**: a fixed number of worker
  goroutines (`Workers`, default `NumCPU()`), a **buffered channel** of layer jobs, a
  **buffered channel** of results, and a `sync.WaitGroup` to await completion.
- Never spawn more workers than there are layers.
- The pool is created and torn down **per `Convert` call** — no shared global pool, to
  preserve statelessness and concurrency safety.

Sketch:

```go
jobs := make(chan layer, len(layers))
results := make(chan traced, len(layers))
var wg sync.WaitGroup
for i := 0; i < workers; i++ {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for lyr := range jobs {
			paths, err := bitrace.Trace(lyr.mask, traceCfg)
			results <- traced{color: lyr.color, paths: paths, err: err}
		}
	}()
}
for _, lyr := range layers { jobs <- lyr }
close(jobs)
wg.Wait()
close(results)
```

### 11.2 Memory

- Prefer **flat slices** over nested structures for pixel data and masks
  (`[]uint8` / `[]bool` indexed by `y*W + x`, not `[][]T`). Flat buffers are
  cache-friendly and reduce allocation/GC pressure.
- **Monitor allocations during k-means** (the feature-point set is `W*H` observations);
  reuse buffers where possible; avoid per-pixel heap allocations.
- **Enforce strict bounds on `K` relative to resolution** (§9.1) — an unbounded `K` on
  a large image is the primary OOM risk after decode. The `MaxDimensions` downsample is
  the first line of defense; the `K` clamp is the second.
- Consider reusing a scratch mask buffer across layers rather than allocating `N` full
  masks at once, if profiling shows mask allocation dominates.

### 11.3 Statelessness

- No package-level mutable variables. All working state is local to a `Convert` call.
- Input strictly via `io.Reader`; output strictly via `io.Writer`. No temp files.
- An `Engine` holds only immutable, validated config → safe to share across goroutines.

---

## 12. Documentation & Licensing Deliverables

Documentation quality and **clean-room provenance** are first-class requirements — not
afterthoughts — because `minisvg` and `bitrace` are intended to be open-sourced and/or
commercially licensed independently.

Each of the three modules must ship:

- A `LICENSE` file. `minisvg` is licensed **MIT**. `bitrace` and the engine module
  (root `github.com/aleybovich/vectrigo`, plus `cmd/vectrigo-cli`) are licensed
  **PolyForm Noncommercial License 1.0.0** — free for noncommercial use; commercial use
  requires a separate license from Andrey Leybovich. All of these remain compatible with
  the permissive dependency graph.
- A `README.md` covering: what it does, install, a minimal usage example, the license,
  and (for `bitrace`) an explicit clean-room/provenance statement (no Potrace lineage).
- **Complete godoc:** a package-level doc comment (`doc.go`), godoc on every exported
  type/function/method, and **runnable `Example` functions** (`example_test.go`) that
  appear in godoc.

Project-wide:

- Top-level `README.md` states the non-negotiable constraints (§1) prominently.
- CI enforces: `CGO_ENABLED=0` cross-compile matrix, `go vet`, `go test ./...` across
  all modules, and a **license audit** against the §1.2 allow-list.

---

## 13. Build Order / Milestones

Build the leaf dependencies first so each layer can be tested in isolation.

1. **`minisvg`** (leaf; no dependencies). Builder + groups + `PathBuilder` +
   minification/coordinate-rounding + unit tests + full godoc/examples.
2. **`bitrace`** (depends only on `honnef.co/go/curve`). Contour extraction → noise
   suppression (`TurdSize`) → corner detection/segmentation (`AlphaMax`) → cubic Bézier
   fitting → output commands. Unit tests including the synthetic bitmaps in §7.5 +
   full godoc/examples.
3. **Vectrigo engine** (wired via `go.work`). Stage I normalization → Stage II
   quantization → Stage III worker-pool tracing → Stage IV SVG assembly. End-to-end
   tests on sample PNG/JPEG/WEBP images. Public API (`Vectorize`, `Engine`).
4. **Tuning pass.** Calibrate the `Sensitivity → (K, TurdSize)` mapping curve (§9) on a
   representative image set so the 0-100 dial feels linear and mid-range output stays
   clean; dial in defaults for `AlphaMax`; validate the minification output; verify
   memory bounds (downsample ceiling + `K` clamp) on large inputs; profile allocations
   in k-means and tracing.

Definition of done for each milestone: builds with `CGO_ENABLED=0`, passes `go vet` and
`go test ./...`, ships godoc + examples, and (for modules 1-2) is extraction-ready.

---

## 14. Rationale / Design Decisions

**Why two in-house libraries exist — license cleanliness.**
Vectrigo is licensed under the **PolyForm Noncommercial License 1.0.0** (free for
noncommercial use; commercial use requires a separate license). For Vectrigo to set its
own license terms this way, every dependency must be permissively licensed (§1.2) so no
copyleft is inherited. Two otherwise-obvious dependencies fail that test:

- The mature bitmap tracer everyone reaches for is **Potrace**, and its Go ports
  (`gotranspile/gotrace`, `dennwc/gotrace`) inherit Potrace's **GPL-2.0** — which would
  make Vectrigo a copyleft derivative. So Vectrigo ships its **own** tracer,
  **`bitrace`**, as an **independent, clean-room** implementation built from general,
  public-domain algorithms (border following / marching squares, area-based speckle
  removal, curvature-based corner detection, cubic Bézier fitting). It is explicitly
  **not** derived from Potrace source.

- The obvious SVG-writing helper, **`ajstarks/svgo`**, is **CC-BY-4.0** — a Creative
  Commons license inappropriate for software and off the permissive allow-list. So
  Vectrigo ships its own **`minisvg`**, a zero-dependency SVG builder/writer.

Both in-house libraries are deliberately structured as standalone Go modules so they can
be **extracted, open-sourced, or sold independently** with no code changes.

**Why cubic Bézier fitting via a permissive kurbo port.**
Potrace's output quality comes largely from fitting smooth curves to traced contours
rather than emitting jagged polylines. To reach a **comparable quality ceiling** while
staying **commercially usable**, `bitrace` fits **cubic Bézier** curves using
`honnef.co/go/curve` — an **Apache-2.0** Go port of Rust's `kurbo` (Raph Levien's
well-regarded curve-fitting lineage). This delivers high-quality curves from a
permissively-licensed, pure-Go building block, consistent with the no-cgo and
permissive-only constraints.

**Why stateless streaming + flat buffers + bounded `K`.**
Statelessness (`io.Reader` → `io.Writer`, no globals) makes the engine trivially
embeddable and safe under concurrency. Flat pixel/mask buffers and a hard downsample
ceiling plus a resolution-relative `K` clamp keep memory predictable and prevent OOM on
adversarial inputs.

---

## 15. Open Questions / Tuning Notes

Future work and areas to refine once the pipeline is end-to-end:

- **Corner-detection robustness (`AlphaMax`).** Validate the corner/smooth decision
  across diverse inputs (logos, photos, line art). Consider adaptive thresholds or a
  short pre-smoothing pass on noisy contours. Compare against Potrace *output* (not
  source) qualitatively to calibrate defaults.
- **Palette / layer ordering heuristics.** Ordering purely by cluster area is a
  starting point. Investigate ordering by luminance, by enclosing/containment
  relationships (holes and nested shapes), or by opacity so foreground detail is never
  occluded. Possibly detect and nest holes using `fill-rule="evenodd"` in `minisvg`.
- **WEBP decode support.** Confirm `golang.org/x/image/webp` covers the required WEBP
  variants (lossy vs. lossless, alpha). Decide behavior for animated WEBP (likely:
  first frame only, or an explicit error).
- **`K` selection.** Explore auto-selecting `K` (e.g. elbow method / silhouette) instead
  of a fixed value, and refine the resolution-relative clamp formula.
- **Simplification quality (`Optimize`).** Tune decimation aggressiveness vs. fidelity;
  measure output-size reduction against visual diff.
- **Performance.** Profile k-means (the dominant allocator) and tracing; evaluate a
  packed-bitset `Bitmap` representation and buffer reuse across layers.
- **Anti-aliasing / edge fringing.** Quantization on anti-aliased edges can create
  thin sliver clusters; consider an optional edge-cleanup / morphological step before
  tracing.

---

*End of specification.*
