# Vectrigo v1.0.0

First stable release of **Vectrigo** — a portable, pure-Go engine that converts
raster images (**PNG / JPEG / WEBP**) into clean, scalable **SVG** vector paths.
It reads from an `io.Reader`, writes to an `io.Writer`, holds no global state,
and is safe to call concurrently from many goroutines.

## Highlights

### Two vectorization pipelines

- **Quantization (default)** — colour-first: clusters pixels into `K` palette
  colours with a deterministic seeded k-means, then traces each colour. Crisp on
  **flat / logo / icon art**.
- **Segmentation "photo mode"** (`Config.Photo` / `--photo`) — region-first:
  Felzenszwalb graph segmentation into spatially-coherent regions, each coloured
  by its mean, then traced as a **single planar subdivision** so adjacent regions
  share their exact boundaries and tile with **no seams**. Far better on
  **photographic / painterly images**.

### Detail & quality controls

- `Sensitivity` (0–100) — the primary quantization detail knob.
- `AutoK` / `AutoKTau` — automatic colour-count selection via a k-means
  distortion "knee".
- `PhotoDetail` (bilateral σ_r) — photo-mode detail dial; `PhotoEdge` — `crisp`
  (seam-free flat look) or `stroke` (anti-aliased).
- Advanced overrides: `K`, `TurdSize`, `AlphaMax`, `Optimize`, `MaxDimensions`,
  `Workers`, `Precision`.

### Deterministic

Identical input + config always yields **byte-identical** SVG.

## Command-line tool

`vectrigo-cli` exposes both pipelines:

```sh
vectrigo-cli -i logo.png  -s 70                            # quantization, fixed detail
vectrigo-cli -i logo.png  --auto-k                         # quantization, auto colour count
vectrigo-cli -i photo.jpg --photo --sigma 8 --edge stroke  # segmentation, tuned
```

## Modules & licensing

A four-module repo (pure Go, builds with `CGO_ENABLED=0`, cross-compiles to any
`GOOS`/`GOARCH`; every dependency permissively licensed):

| Module | Purpose | License |
|---|---|---|
| `github.com/aleybovich/vectrigo` | engine + `vectrigo-cli` | PolyForm Noncommercial 1.0.0 |
| `github.com/aleybovich/bitrace` | bitmap → Bézier tracer | PolyForm Noncommercial 1.0.0 |
| `github.com/aleybovich/segment` | image segmentation (+ bilateral/Kuwahara filters) | MIT |
| `github.com/aleybovich/minisvg` | zero-dependency SVG writer | MIT |

## Constraints (guarantees)

- **Pure Go, no cgo** — no shell-outs, no external binaries, no system libraries.
- **Streaming & stateless** — `io.Reader` → `io.Writer`, no temp files, no global
  state; one `Engine` is safe to share across goroutines.
- **Permissive dependency graph** (MIT / BSD / Apache-2.0 only), which is what
  lets the engine set its own PolyForm terms.

## Install

```sh
go get github.com/aleybovich/vectrigo
go install github.com/aleybovich/vectrigo/cmd/vectrigo-cli@v1.0.0
```

## License

Vectrigo (engine + CLI) is licensed under the
**PolyForm Noncommercial License 1.0.0** — free for noncommercial use; commercial
use requires a separate license from Andrey Leybovich. The `segment` and
`minisvg` libraries are MIT.
