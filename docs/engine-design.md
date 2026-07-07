# Vectrigo Engine — Architecture & Design

**Module:** `github.com/aleybovich/vectrigo` (repo root)
**Status:** Design (implementation-ready). This document is the authoritative
build spec for the engine. It refines `plan.md` §§1, 5, 8–11 with the *exact*
APIs of the two in-house libraries (`bitrace`, `minisvg`) and the approved
third-party dependencies as they actually exist in this tree.

> **Scope note.** This is a design document. It contains Go *signatures and
> sketches* so an implementer can build directly from it, but no production
> implementation. Do not change `go.mod` (module path, `go 1.26.2`, or the
> `replace` directives to `./bitrace` and `./minisvg`).

---

## 1. Hard constraints (recap, binding)

- **Pure Go, no cgo.** Must build with `CGO_ENABLED=0` and cross-compile to any
  `GOOS`/`GOARCH`. No `os/exec`, no external binaries.
- **Permissive deps only.** Engine may import, in addition to the two in-house
  modules and the stdlib:
  - `github.com/disintegration/imaging` v1.6.2 (MIT) — decode/resize/NRGBA.
  - `github.com/muesli/kmeans` v0.3.1 (MIT) + its dep
    `github.com/muesli/clusters` — color quantization (see §6 for a critical
    determinism/statelessness caveat and the recommended in-house alternative).
  - `golang.org/x/image` v0.43.0 (BSD-3): `golang.org/x/image/webp` (decode),
    and `golang.org/x/image/vector` + `golang.org/x/image/draw` (test-only
    round-trip rasterizer, §11).
- **Stateless / streaming.** Input `io.Reader`, output `io.Writer`. No temp
  files, no package-level mutable state, safe for concurrent use.

---

## 2. Package & file layout

Public façade in the root package `vectrigo`; each pipeline stage is an
`internal/` package so the surface stays tiny and the stages are independently
testable.

```
vectrigo/                              # module github.com/aleybovich/vectrigo
├── go.mod                             # DO NOT EDIT (path, go 1.26.2, replaces)
├── vectrigo.go                        # package vectrigo: Vectorize, Engine, Convert
├── config.go                          # Config, DefaultConfig, (*Config).normalized()
├── config_test.go                     # derivation + clamp + zero-value tests
├── vectrigo_test.go                   # end-to-end tests over testdata/*
├── example_test.go                    # runnable godoc examples
├── doc.go                             # package doc comment
├── testdata/                          # STAGED (do not regenerate)
│   ├── shapes.png / .jpg / .webp      # 96×64, one per decoder
│   └── street_market.png              # 1024×559, rich real image
└── internal/
    ├── imageutil/                     # shared flat-pixel helpers (RGBA, packing)
    │   └── imageutil.go
    ├── normalize/                     # Stage I: decode + NRGBA + downsample
    │   ├── normalize.go
    │   └── normalize_test.go
    ├── quantize/                      # Stage II: k-means → []Layer (color+mask)
    │   ├── quantize.go                # Quantizer iface + palette/mask assembly
    │   ├── kmeans.go                  # deterministic Lloyd/k-means++ (default)
    │   ├── muesli.go                  # optional muesli/kmeans adapter
    │   └── quantize_test.go
    ├── pipeline/                      # Stage III: worker pool over layers → bitrace
    │   ├── pipeline.go
    │   └── pipeline_test.go
    └── assemble/                      # Stage IV: order + fill + minisvg serialize
        ├── assemble.go
        └── assemble_test.go
```

Rationale for the extra `internal/imageutil` vs. plan.md’s bare four packages:
the flat-buffer pixel access (indexing an `*image.NRGBA.Pix`, packing/unpacking
`color.RGBA`, hex formatting) is shared by `quantize`, `pipeline`, and
`assemble`; centralizing it avoids three copies and one source of index-math
bugs. It is a leaf helper, not a stage.

Import graph (acyclic): `vectrigo` → `normalize`, `quantize`, `pipeline`,
`assemble`; all stages → `imageutil`; `quantize`/`pipeline`/`assemble` →
`bitrace`; `assemble` → `minisvg`.

---

## 3. Data types flowing between stages

All inter-stage types live in the stage packages and are plumbed by
`vectrigo.Convert`. Pixel and mask data are **flat, row-major** buffers
(`index = y*W + x`), per plan §11.2.

```go
// ---- Stage I output (internal/normalize) ----

// Image is a normalized, size-bounded, non-premultiplied RGBA raster.
type Image struct {
    NRGBA   *image.NRGBA // imaging's native format; Pix is []uint8, 4 bytes/px R,G,B,A
    OrigW   int          // original decoded width  (before downsample)
    OrigH   int          // original decoded height (before downsample)
    // Working dims are NRGBA.Bounds().Dx()/Dy(); scale = OrigW/W == OrigH/H (±rounding).
}

// ---- Stage II output (internal/quantize) ----

// Layer is one quantized color plane: a centroid color plus the binary mask of
// the pixels assigned to it. Area is the mask popcount, used for z-ordering.
type Layer struct {
    Color color.RGBA     // centroid color, channels rounded to [0,255], A=255
    Mask  bitrace.Bitmap // W,H == working dims; Bits[i] true == pixel i in this cluster
    Area  int            // number of set bits in Mask (== cluster pixel count)
}

// ---- Stage III output (internal/pipeline) ----

// Traced is a Layer after vectorization: its centroid color plus the contours
// bitrace produced for its mask. Order preserved from input Layer slice.
type Traced struct {
    Color color.RGBA
    Area  int
    Paths []bitrace.Path // outer contours (IsHole=false) + holes (IsHole=true)
}
```

**RGBA → `bitrace.Bitmap`.** For layer `k`, `Mask.Bits[y*W+x] = (label[y*W+x]==k)`
where `label` is the per-pixel nearest-centroid assignment from Stage II. `W,H`
are the working (post-downsample) dimensions. Fully-transparent pixels are never
set in any mask (see §5 alpha policy), so they render as empty background.

**`bitrace.Path` → SVG `d`.** `bitrace.Command` maps 1:1 onto
`minisvg.PathBuilder` (verified against both sources):

| `bitrace` command | fields used            | `PathBuilder` call                          |
| ----------------- | ---------------------- | ------------------------------------------- |
| `MoveTo`          | `P`                    | `.MoveTo(P.X, P.Y)`                         |
| `LineTo`          | `P`                    | `.LineTo(P.X, P.Y)`                         |
| `CubicTo`         | `C1, C2, P`            | `.CubicTo(C1.X,C1.Y, C2.X,C2.Y, P.X,P.Y)`   |
| `Close`           | none                   | `.Close()`                                  |

`bitrace` guarantees each `Path.Commands` begins with `MoveTo` and ends with
`Close` (per its `doc.go`), so no defensive re-opening is needed.

---

## 4. Public API (`package vectrigo`)

### 4.1 Config

Exactly the shape in plan §9; reproduced here with the binding zero-value and
clamp semantics this design commits to.

```go
package vectrigo

type Dimensions struct {
    Width  int
    Height int
}

type Config struct {
    // Sensitivity is the PRIMARY 0–100 detail dial (integer percent). Drives the
    // derived (K, TurdSize) pair. 0 is a real value (max posterization), so a
    // bare Config{} means Sensitivity 0, NOT the default — build from
    // DefaultConfig(). Clamped to [0,100].
    Sensitivity int

    // K forces an exact cluster count. 0 => derive from Sensitivity. When set,
    // clamped to [2, maxKForPixels(W*H)] and never exceeds distinct-color count.
    K int

    // TurdSize forces the speckle-area threshold (px), passed to bitrace.
    // 0 => derive from Sensitivity. <0 => force-disable speckle removal (0 to
    // bitrace). >0 => used as-is. (See §4.4 for why we use <0 as the sentinel.)
    TurdSize int

    // AlphaMax is the corner/smoothness axis (independent of detail). Passed to
    // bitrace, which clamps to [0,1.334]. Default 1.0.
    AlphaMax float64

    // Optimize enables bitrace curve looseness AND minisvg minify + rounding.
    // Default true.
    Optimize bool

    // MaxDimensions is the downsample ceiling (memory bound). Default 2048×2048.
    // Any axis <=0 => that axis uses the 2048 default.
    MaxDimensions Dimensions

    // Workers is the tracing concurrency. <=0 => runtime.NumCPU(). Never more
    // workers than layers N.
    Workers int

    // Precision is coordinate decimal places when Optimize. Default 2. Clamped
    // to [0,6]. (minisvg treats <0 as "no rounding"; we never pass <0 here.)
    Precision int
}

func DefaultConfig() Config {
    return Config{
        Sensitivity:   50,
        K:             0,
        TurdSize:      0,
        AlphaMax:      1.0,
        Optimize:      true,
        MaxDimensions: Dimensions{Width: 2048, Height: 2048},
        Workers:       0,
        Precision:     2,
    }
}
```

### 4.2 Entry points

```go
// Vectorize reads a raster (PNG/JPEG/WEBP) from r and writes SVG to w. Stateless,
// concurrency-safe. Delegates to Engine. Build cfg from DefaultConfig().
func Vectorize(r io.Reader, w io.Writer, cfg Config) error {
    return NewEngine(cfg).Convert(r, w)
}

// Engine is a reusable, stateless converter holding only validated config.
type Engine struct {
    cfg Config // already normalized/clamped; immutable after NewEngine
}

// NewEngine validates+clamps cfg (fills zero fields per §4.3/§4.4) and returns
// an Engine safe to share across goroutines.
func NewEngine(cfg Config) *Engine { return &Engine{cfg: cfg.normalized()} }

// Convert runs the full four-stage pipeline: r (raster) -> w (SVG).
func (e *Engine) Convert(r io.Reader, w io.Writer) error
```

`Convert` body (orchestration only; each call owns all state):

```go
func (e *Engine) Convert(r io.Reader, w io.Writer) error {
    img, err := normalize.Decode(r, e.cfg.MaxDimensions.Width, e.cfg.MaxDimensions.Height)
    if err != nil { return fmt.Errorf("vectrigo: normalize: %w", err) }

    // Resolve K/TurdSize now that working dimensions are known (K clamp depends on W*H).
    k, turd := e.cfg.resolveDetail(img.NRGBA.Bounds().Dx(), img.NRGBA.Bounds().Dy())

    layers, err := quantize.Quantize(img, k)          // Stage II
    if err != nil { return fmt.Errorf("vectrigo: quantize: %w", err) }

    traceCfg := bitrace.Config{TurdSize: turd, AlphaMax: e.cfg.AlphaMax, Optimize: e.cfg.Optimize}
    traced, err := pipeline.TraceLayers(layers, traceCfg, e.cfg.Workers) // Stage III
    if err != nil { return fmt.Errorf("vectrigo: trace: %w", err) }

    if err := assemble.WriteSVG(w, traced, img, assemble.Options{ // Stage IV
        Optimize: e.cfg.Optimize, Precision: e.cfg.Precision,
    }); err != nil {
        return fmt.Errorf("vectrigo: assemble: %w", err)
    }
    return nil
}
```

### 4.3 `normalized()` — zero-value handling & clamps

`Config.normalized()` returns a copy with defaults filled and clamps applied.
It does **not** resolve `K`/`TurdSize` (those need working pixel dimensions and
are done in `resolveDetail`, §4.4). Rules, in order:

```go
func (c Config) normalized() Config {
    // Sensitivity: 0 is legal; only clamp. (Bare Config{} => 0, by design.)
    c.Sensitivity = clampInt(c.Sensitivity, 0, 100)

    // AlphaMax: bitrace re-clamps, but normalize a NaN/neg here for predictability.
    if math.IsNaN(c.AlphaMax) { c.AlphaMax = 1.0 }
    c.AlphaMax = clampFloat(c.AlphaMax, 0, 1.334)

    // MaxDimensions: any axis <=0 => 2048.
    if c.MaxDimensions.Width  <= 0 { c.MaxDimensions.Width  = 2048 }
    if c.MaxDimensions.Height <= 0 { c.MaxDimensions.Height = 2048 }

    // Workers: <=0 => NumCPU (further capped to N inside the pool).
    if c.Workers <= 0 { c.Workers = runtime.NumCPU() }

    // Precision: clamp to [0,6]; we never emit negative (that would mean "no round").
    c.Precision = clampInt(c.Precision, 0, 6)

    // K/TurdSize left as-is (0 => derive later; K<0 impossible after clamp below;
    // TurdSize<0 preserved as the force-disable sentinel).
    if c.K < 0 { c.K = 0 }
    return c
}
```

> **The one asymmetry, restated for implementers:** every zero field auto-defaults
> *except* `Sensitivity`, whose 0 is a valid setting. That is precisely why the
> documented idiom is "start from `DefaultConfig()`," and why `AlphaMax==0` from a
> bare `Config{}` is left as legal (max-angular) rather than promoted to 1.0.

### 4.4 Sensitivity → (K, TurdSize) derivation

Computed in `resolveDetail(W, H)` once working dimensions are known:

```go
// resolveDetail returns the effective (K, TurdSize) for the pipeline.
func (c Config) resolveDetail(W, H int) (k, turd int) {
    // ---- K ----
    if c.K > 0 {
        k = c.K                       // explicit override
    } else {
        // Reference curve: K = round(4 * 2^(S/25)) => 4,8,16,32,64 at S=0,25,50,75,100.
        k = int(math.Round(4 * math.Pow(2, float64(c.Sensitivity)/25.0)))
    }
    // Clamp regardless of source: min 2, and bounded by resolution to cap memory.
    k = clampInt(k, 2, maxKForPixels(W*H))

    // ---- TurdSize ----
    switch {
    case c.TurdSize < 0:
        turd = 0                      // explicit "disable speckle removal"
    case c.TurdSize > 0:
        turd = c.TurdSize             // explicit override
    default:
        // Reference curve: TurdSize = floor(8 * 2^(-S/25)) => 8,4,2,1,0 at S=0..100.
        turd = int(math.Floor(8 * math.Pow(2, -float64(c.Sensitivity)/25.0)))
        if turd < 0 { turd = 0 }
    }
    return k, turd
}

// maxKForPixels bounds K so clusters aren't degenerately small on tiny images
// and memory stays bounded on large ones. Tunable; starting formula:
//   maxK = clamp( (W*H)/1024 , 2, 256 )
// e.g. 96×64=6144 -> 6 ; 1024×559≈572k -> 256 ; 2048×2048 -> 256.
func maxKForPixels(px int) int { return clampInt(px/1024, 2, 256) }
```

Notes:
- The `4·2^(S/25)` / `8·2^(-S/25)` pair reproduces plan §9's reference table
  exactly at the five tabulated points and is smooth in between. Treat the
  constants as the calibration starting point (plan §13 tuning pass).
- `k` is additionally capped by the number of **distinct opaque colors** in the
  image inside `quantize.Quantize` (muesli’s `Partition` errors if `k > len(data)`;
  the in-house k-means simply produces fewer non-empty clusters). Either way the
  emitted layer count `N ≤ k`.
- **Ambiguity resolution for `TurdSize==0`:** plan §9 says both "0 => derive"
  and "forced 0 disables removal." Those conflict. This design resolves it with a
  sign convention: `0` = derive, `<0` = force-disable. Since the derived value at
  `Sensitivity==100` is already `0`, callers who want speckle removal off can also
  just raise Sensitivity. Documented on the field. (Alternative considered:
  `*int` pointer field; rejected for API simplicity, matching plan’s choice for
  `Sensitivity`.)

### 4.5 Errors

Descriptive, wrapped, never panic on bad input (plan §10):

- Empty input / EOF before any bytes → `vectrigo: normalize: empty input`.
- Unknown/unsupported format (no registered decoder matched) → wrap
  `image.Decode`’s `image: unknown format` as `vectrigo: normalize: decode: %w`.
- Corrupt image → decoder error wrapped identically.
- Degenerate result (0 layers, e.g. fully-transparent input) → **not** an error;
  emit a valid empty `<svg>` with correct dimensions.
- Internal stage failures (bitrace malformed-bitmap — should be unreachable since
  masks are constructed W*H-consistent) → wrapped as `vectrigo: trace: %w`.

---

## 5. Stage I — Normalize (`internal/normalize`)

```go
// Decode reads an encoded image from r, normalizes it to non-premultiplied
// RGBA, and downsamples so neither axis exceeds (maxW,maxH). Content-sniffed:
// format is detected by magic bytes via image.Decode's registry, not by any
// filename (there is none — it's a stream).
func Decode(r io.Reader, maxW, maxH int) (normalize.Image, error)
```

**Decoders / content sniffing.** `imaging.Decode(r)` delegates to
`image.Decode`, which dispatches on registered magic-byte signatures (verified
in `imaging@v1.6.2/io.go`). `imaging` already registers PNG/JPEG/GIF/BMP/TIFF
via its own imports, but **not WEBP**. Therefore `normalize.go` must blank-import
the WEBP decoder so it self-registers:

```go
import (
    _ "image/jpeg"                 // belt-and-suspenders; imaging also imports it
    _ "image/png"
    _ "golang.org/x/image/webp"    // REQUIRED: registers WEBP for image.Decode
    "github.com/disintegration/imaging"
)
```

**Normalization to RGBA.** Use `imaging.Clone(img)` to obtain an `*image.NRGBA`
regardless of the source concrete type. NRGBA (non-premultiplied) is the correct
space for color clustering — premultiplied channels would bias centroids toward
black on semi-transparent pixels. `NRGBA.Pix` is the flat `[]uint8` buffer
(`4*W*H`, stride `4*W`, order R,G,B,A) consumed directly by Stage II.

**Downsample.** If `W > maxW || H > maxH`, scale down preserving aspect ratio
(never upscale):

```go
b := img.Bounds(); W, H := b.Dx(), b.Dy()
if W > maxW || H > maxH {
    s := math.Min(float64(maxW)/float64(W), float64(maxH)/float64(H))
    nw, nh := int(math.Round(float64(W)*s)), int(math.Round(float64(H)*s))
    img = imaging.Resize(img, nw, nh, imaging.Lanczos) // high-quality
}
```

Record `OrigW/OrigH` (pre-resize) so Stage IV can map the `viewBox` back to
original coordinates. `street_market.png` (1024×559) is under the 2048 default,
so it is *not* downsampled by default — downsample paths are exercised in tests
by passing a small `MaxDimensions`.

**Alpha policy (documented, binding).** A pixel with `A < 128` is treated as
transparent → excluded from k-means observations and left **off** in every mask
(renders as SVG background). `A >= 128` pixels use their (R,G,B) as the feature;
alpha is dropped from the feature vector (we cluster color only). Centroid colors
are emitted opaque (`A=255`). This keeps semi-transparent AA fringes from
spawning sliver clusters (plan §15 anti-aliasing note) without a morphological
pass in v1.

Output: `normalize.Image{NRGBA, OrigW, OrigH}`.

---

## 6. Stage II — Quantize (`internal/quantize`)

Goal: turn `W*H` opaque RGB observations into `K` centroids and produce `N ≤ K`
`Layer`s (centroid color + flat binary mask + area).

### 6.1 Interface & default implementation

```go
// Quantize clusters img's opaque pixels into <=k color layers. Deterministic for
// a given (img,k): see §6.3.
func Quantize(img normalize.Image, k int) ([]Layer, error)
```

Internally structured around a small interface so the clustering backend is
swappable without touching mask assembly:

```go
type clusterer interface {
    // fit returns k centroids (each a [3]float64 in [0,255]) and a per-pixel
    // label slice (len W*H; label[i] in [0,k) for opaque pixels, -1 for skipped
    // transparent pixels).
    fit(pix []uint8, w, h, k int) (centroids [][3]float64, labels []int, err error)
}
```

Two implementations, same interface:

1. **`kmeansLloyd` (default, recommended).** A ~60-line flat-buffer
   **k-means++** initialization + Lloyd iteration operating directly on
   `[]uint8` RGB (no interface boxing, no `[]float64` per pixel). Seeded from a
   **fixed constant** via an injected `*rand.Rand` (`rand.New(rand.NewSource(seed))`),
   giving byte-reproducible output and zero global state. This is the
   implementation the tests assert against.

2. **`muesliAdapter` (approved-dep reference).** Wraps
   `github.com/muesli/kmeans`. Each opaque pixel becomes a
   `clusters.Coordinates{R/255, G/255, B/255}` (the library documents
   observations as floats in [0,1]); `kmeans.New().Partition(obs, k)` yields
   `clusters.Clusters`, whose `.Center` values (×255) are the centroids.

> ### CRITICAL caveat on muesli/kmeans (why (1) is the default)
> Inspection of the vendored source shows `clusters.New` calls
> **`rand.Seed(time.Now().UnixNano())`** and draws initial centers from the
> **process-global** `math/rand` source; `kmeans.Partition` also uses global
> `rand.Intn` for empty-cluster reseeding. This has two consequences that
> collide head-on with our hard constraints:
> - **Statelessness (§1.3, plan §11.3):** it mutates process-global RNG state on
>   every call. Memory-safe (the global source is mutex-guarded) but it is
>   package-level mutable state we don’t control.
> - **Determinism:** because it reseeds from wall-clock time each call, initial
>   centroids — and thus the converged partition on non-separable data — vary
>   run to run. There is **no injection point** to fix the seed.
>
> The plan asks for "k-means … SEEDED for deterministic tests." muesli cannot be
> seeded. Therefore the design makes the in-house `kmeansLloyd` the default
> backend (satisfying determinism + statelessness with **no new dependency** —
> only stdlib `math/rand`), and keeps `muesliAdapter` behind the same interface
> as the approved-dependency reference / fallback. Selecting the backend is a
> one-line change in `Quantize`. If muesli is used in production, determinism is
> only *statistical* and tests must fall back to the tolerant assertions in §11.

### 6.2 Mask assembly (flat slices)

Given `centroids` and `labels`:

```go
W, H := img.NRGBA.Bounds().Dx(), img.NRGBA.Bounds().Dy()
layers := make([]Layer, 0, k)
for c := 0; c < k; c++ {
    bits := make([]bool, W*H)      // one flat mask per non-empty cluster
    area := 0
    for i, lb := range labels {
        if lb == c { bits[i] = true; area++ }
    }
    if area == 0 { continue }      // drop empty clusters => N <= k
    layers = append(layers, Layer{
        Color: color.RGBA{ round8(centroids[c][0]), round8(centroids[c][1]), round8(centroids[c][2]), 255 },
        Mask:  bitrace.Bitmap{W: W, H: H, Bits: bits},
        Area:  area,
    })
}
```

`round8` clamps+rounds a `[0,255]` float to `uint8`. Masks are exactly the flat
`[]bool` `bitrace.Bitmap` expects (`index=y*W+x`), so Stage III passes them to
`bitrace.Trace` with no conversion.

### 6.3 Determinism strategy

- `kmeansLloyd` is seeded from a package constant (`const kmeansSeed = 1`) → same
  input ⇒ same centroids ⇒ same masks ⇒ byte-identical SVG.
- **Canonical layer order** is *not* left to k-means label order (which is
  arbitrary). Before returning, sort `layers` by a stable key so downstream
  ordering is reproducible even if labels permute: primary `Area` (desc, for
  z-order, §8), tie-break by packed `0xRRGGBB` (asc). This guarantees stable
  output and stable test assertions independent of clustering label assignment.

### 6.4 Memory (the primary hotspot)

- **Observation set is `W*H`** — the dominant allocator (plan §11.2). The
  in-house `kmeansLloyd` avoids `muesli`’s per-pixel `clusters.Coordinates`
  (`[]float64` of len 3 + interface box ≈ 64 B/pixel ⇒ ~270 MB at 2048²). It
  operates on the existing `NRGBA.Pix` plus a single `[]int` label buffer
  (`W*H`) and a small `k×3` centroid/accumulator array — roughly `4·W·H` (label
  ints) over the pixel buffer we already hold.
- **Optional subsampling of the *fit* step** (recommended for large images):
  fit centroids on a deterministic stride sample (cap ~200k pixels), then assign
  *all* pixels to nearest centroid for masks. Cuts k-means cost ~20× on the big
  fixture with no visible palette change. Deterministic because the stride and
  seed are fixed.
- **K clamp** (`maxKForPixels`, §4.4) is the second memory guard after the
  `MaxDimensions` downsample; together they bound peak memory to
  `O(W·H + k·W·H_masks)`. Mask allocation is `N` `[]bool` of `W*H`; if profiling
  shows this dominates, reuse a scratch `[]bool` and hand off packed
  `[]uint64` masks later (deferred; plan §15).

---

## 7. Stage III — Trace (`internal/pipeline`)

```go
// TraceLayers vectorizes each layer's mask with bitrace, concurrently. Result
// order matches the input layers slice (so Stage II's canonical order survives).
func TraceLayers(layers []Layer, cfg bitrace.Config, workers int) ([]Traced, error)
```

**Worker pool** (plan §11.1) — created and torn down per call, no global pool:

```go
n := len(layers)
if n == 0 { return nil, nil }
if workers > n { workers = n }          // never more workers than layers
if workers < 1 { workers = 1 }

type job struct{ idx int; lyr Layer }
jobs := make(chan job, n)
out  := make([]Traced, n)               // indexed write => no result channel needed for ordering
errs := make([]error, n)
var wg sync.WaitGroup
for w := 0; w < workers; w++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        for jb := range jobs {
            paths, err := bitrace.Trace(jb.lyr.Mask, cfg) // bitrace is concurrency-safe (doc.go)
            if err != nil { errs[jb.idx] = err; continue }
            out[jb.idx] = Traced{Color: jb.lyr.Color, Area: jb.lyr.Area, Paths: paths}
        }
    }()
}
for i, l := range layers { jobs <- job{i, l} }
close(jobs)
wg.Wait()

if err := errors.Join(errs...); err != nil { return nil, err }
return out, nil
```

Design points:
- Writing results into a pre-sized `out[idx]` (rather than a results channel)
  keeps ordering deterministic without a post-sort and avoids a second buffered
  channel. Each index is written by exactly one goroutine → no race.
- `bitrace.Trace` documents concurrency-safety and never mutates its input, so
  sharing masks across goroutines is safe.
- `bitrace.Trace` only errors on a malformed `Bitmap` (`len(Bits)!=W*H` or
  negative dims); Stage II always builds consistent masks, so an error here is an
  internal invariant violation — surfaced (not swallowed) via `errors.Join`.
- Layers whose mask is all-off can't occur (Stage II drops `Area==0`), but if one
  did, `bitrace.Trace` returns `(nil,nil)` → an empty `Paths`, harmless.

---

## 8. Stage IV — Assemble (`internal/assemble`)

```go
type Options struct { Optimize bool; Precision int }

// WriteSVG orders the traced layers, serializes them via minisvg, and streams
// the document to w.
func WriteSVG(w io.Writer, traced []Traced, img normalize.Image, opt Options) error
```

### 8.1 Document setup & coordinate mapping

```go
Wd, Hd := img.NRGBA.Bounds().Dx(), img.NRGBA.Bounds().Dy() // working (traced) coords
doc := minisvg.New(img.OrigW, img.OrigH)                   // display size = original px
doc.SetViewBox(0, 0, float64(Wd), float64(Hd))             // content in working coords
```

Path coordinates from `bitrace` are in *working* (post-downsample) pixel space.
Setting `width/height` to the **original** dimensions and `viewBox` to the
**working** dimensions makes the renderer scale the vector content back up to the
source’s apparent size — the "map back to original coordinate space" from plan
§Stage I — with no per-point arithmetic. (If a caller prefers 1:1, working dims
could be used for both; original-dims is the default so output visually matches
the input size.)

### 8.2 Z-order (largest behind)

SVG paint order == document order (no z-index). Emit largest-area clusters first
(bottom) so small foreground shapes are not occluded (plan §8 Stage IV):

```go
sort.SliceStable(traced, func(i, j int) bool {
    if traced[i].Area != traced[j].Area { return traced[i].Area > traced[j].Area } // big first
    return pack(traced[i].Color) < pack(traced[j].Color)                            // stable tiebreak
})
```

The tie-break on packed color keeps output deterministic when two clusters have
equal area. (Stage II already applied the same canonical order; re-sorting here
is cheap and makes `assemble` self-contained/testable in isolation.)

### 8.3 Holes → one `<path>` per color via nonzero winding

**Key correctness decision.** `bitrace` emits outer contours and hole contours
with **opposite winding** (outer negative signed area, holes positive,
`IsHole=true` — verified in `bitrace/doc.go` and `bitrace.go`). SVG’s default
`fill-rule="nonzero"` renders exactly this correctly *iff* a color’s outer
contours and its holes live in the **same `d` string** (one `<path>`). Then the
opposite windings cancel inside holes, leaving them unfilled so the layer painted
behind shows through.

Therefore, **one `<path>` per color layer**, concatenating every contour (outer +
holes) as successive subpaths, single `fill`:

```go
for _, t := range traced {
    var b minisvg.PathBuilder
    for _, p := range t.Paths {          // outer + holes together, winding preserved
        for _, c := range p.Commands {
            switch c.Kind {
            case bitrace.MoveTo:  b.MoveTo(c.P.X, c.P.Y)
            case bitrace.LineTo:  b.LineTo(c.P.X, c.P.Y)
            case bitrace.CubicTo: b.CubicTo(c.C1.X, c.C1.Y, c.C2.X, c.C2.Y, c.P.X, c.P.Y)
            case bitrace.Close:   b.Close()
            }
        }
    }
    doc.Path(b.String(), minisvg.Color(hex(t.Color))) // hex => "#rrggbb"
}
```

Why not a `<g fill>` with one `<path>` per contour (plan §8’s phrasing)? Because
sibling `<path>` elements each fill independently — a hole emitted as its own
`<path>` would be painted **solid** with the layer color, defeating the hole.
Merging into one `d` is the correct construction and is *more* compact anyway.
(`minisvg`’s `Path`/`PathBuilder` support arbitrary multi-subpath `d`, verified.)
A `<g>` wrapper adds nothing here since we already have exactly one path per fill;
it’s reserved for a future split-by-connected-component mode. `minisvg` currently
exposes no `fill-rule` attribute, and we deliberately don’t need one — nonzero +
opposite winding suffices. (If an even-odd mode is ever wanted, `minisvg` needs a
tiny extension to set the attribute; noted as a risk in §12.)

### 8.4 Serialize + minify

```go
if opt.Optimize {
    _, err := doc.WriteToOpts(w, minisvg.WriteOptions{Minify: true, Precision: opt.Precision})
    return err
}
_, err := doc.WriteTo(w) // pretty, unrounded
return err
```

`minisvg` rounds only `d`/`viewBox` numerics, using exact decimal-string
arithmetic (immune to float error, per its source), and streams via
`io.WriteString`. The engine’s `Optimize` flag drives *both* bitrace’s looser
curve fitting and this minify/round pass, as plan §9 specifies.

---

## 9. Concurrency, memory & statelessness (consolidated)

- **Concurrency.** Only Stage III is parallel: a per-`Convert` worker pool
  (`sync.WaitGroup` + one buffered `jobs` channel; results written to a
  pre-sized slice by unique index). No shared pool, no globals. `Workers` capped
  to `N` layers. `bitrace.Trace` is documented reentrant/non-mutating.
- **Memory.** Flat buffers throughout (`NRGBA.Pix`, `[]bool` masks, `[]int`
  labels — all `index=y*W+x`). Peak ≈ decoded RGBA (`4·W·H`) + labels (`~W·H`
  ints) + `N` masks (`N·W·H` bools). Two hard guards: `MaxDimensions` downsample
  (first) and `maxKForPixels` K-clamp (second). k-means is the allocation hotspot;
  the in-house flat backend + optional fit-subsampling keep it bounded and
  GC-friendly.
- **Statelessness.** No package-level mutable variables anywhere in `vectrigo` or
  its `internal/*` stages. All working state is local to a `Convert` call.
  `Engine` holds only immutable, validated config → shareable across goroutines.
  The **one external statefulness risk is muesli/kmeans’ global `rand`** (§6.1),
  which is precisely why the in-house backend is the default.

---

## 10. Error handling (consolidated)

- Every stage boundary wraps with a `vectrigo: <stage>: %w` prefix (plan §10).
- No panics on caller input; malformed images → wrapped decoder errors.
- Empty/EOF input → explicit `empty input` error before decode.
- All-transparent or single-color input → **valid SVG**, not an error (0 or 1
  layer). Assemble emits a well-formed `<svg>` even with zero paths.
- Internal invariant breaches (bitrace malformed-bitmap) → surfaced via
  `errors.Join`, never swallowed.

---

## 11. Test strategy

Fixtures are pre-staged and must not be regenerated:
`testdata/shapes.{png,jpg,webp}` (96×64, one per decoder) and
`testdata/street_market.png` (1024×559).

### 11.1 Per-stage unit tests (fast, always run)

**normalize (`normalize_test.go`)**
- Decode each of `shapes.png/.jpg/.webp`; assert `err==nil`, result is
  `*image.NRGBA`, dims `96×64`, `OrigW/OrigH==96/64`. This is the concrete proof
  that the WEBP blank-import is wired (a missing import fails only on `.webp`).
- Downsample: decode `shapes.png` with `maxW=maxH=32`; assert both axes ≤32,
  aspect ratio preserved (`nw==32, nh==round(64* ... )` per the fit math), and
  **no upscale** when input already fits (`maxW=maxH=1000` ⇒ dims unchanged).
- Empty reader ⇒ error; garbage bytes ⇒ wrapped decode error.

**quantize (`quantize_test.go`)**
- Synthetic 4-quadrant image (4 saturated, well-separated colors), `k=4`:
  assert exactly 4 non-empty layers; each centroid within a small L2 tolerance
  of a source color; masks **partition** the opaque pixels (Σ`Area`==opaque
  count; no pixel set in two masks). Repeat the call and assert **identical**
  centroids+masks (determinism of `kmeansLloyd`).
- `k` clamp: tiny image, huge `k` ⇒ `N ≤ distinct colors`.
- Transparent-pixel policy: an image with an `A<128` region ⇒ those indices set
  in no mask.

**assemble (`assemble_test.go`)**
- Hand-built `[]Traced` (a filled square + a square-with-hole layer + a small
  foreground layer). Assert: output parses as well-formed XML
  (`xml.NewDecoder(...).Token()` loop to EOF, no error); root is `<svg>` with
  expected `width/height/viewBox`; **one `<path>` per color**; each `fill`
  equals the expected `#rrggbb`; the largest-area layer’s `<path>` precedes the
  smaller (z-order); the holed layer’s `d` contains ≥2 `M` subcommands (outer +
  hole in one path).
- Minify on/off: `Optimize=true` yields no newlines and coords rounded to
  `Precision`; `Optimize=false` yields indented, unrounded output.

**config (`config_test.go`)**
- Table-test `resolveDetail` at S=0/25/50/75/100 ⇒ K=4/8/16/32/64 (pre-clamp,
  large image) and TurdSize=8/4/2/1/0. Assert Sensitivity clamps at the rails.
- Zero-value: `Config{}` ⇒ `normalized()` gives Sensitivity 0 (not 50), Workers
  `NumCPU`, MaxDimensions 2048², Precision 0, AlphaMax 0 (legal). `DefaultConfig()`
  ⇒ Sensitivity 50, etc.
- Overrides win: `K=7` survives (clamped only by resolution); `TurdSize=-1` ⇒ 0
  (disabled); `TurdSize=5` ⇒ 5.

### 11.2 End-to-end over all three decoders (fast, always run)

For each of `shapes.png/.jpg/.webp`, `Vectorize(f, &buf, cfg)` with a mid
Sensitivity, then assert on `buf`:
- **Valid SVG:** parses as well-formed XML to EOF; root `<svg>` carries the
  `xmlns`, `width`, `height`, `viewBox` (== working dims).
- **Sensible structure:** path count in `[1, K]`; every `<path>` has a `d`
  starting with `M` and ending with `Z`; every `fill` is a `#rrggbb` string.
- **Centroid-matched fills:** collect the layer centroids (via a test hook that
  calls `quantize.Quantize` on the same normalized image) and assert every
  emitted `fill` is one of them. For **PNG/WEBP** (lossless) this is exact; for
  **JPEG** (lossy) assert count/validity and that fills are drawn from the
  *engine’s own* palette (the palette is computed from the decoded JPEG, so it is
  still exact against that) — do not assert against the PNG palette.
- Determinism: run twice, assert byte-identical output (holds because the default
  backend is seeded; §6.3).

### 11.3 Heavy integration on `street_market.png` (`testing.Short()`-gated)

```go
if testing.Short() { t.Skip("skipping heavy integration test in -short") }
```

- Optionally downscale via `MaxDimensions{512,512}` to keep it quick.
- Assert valid, well-formed SVG; `viewBox` == working dims; path count within a
  broad sane band (e.g. `[K/2, 4K]` — real photos fragment); no `NaN`/`Inf` in
  coordinates (regex/scan the `d` strings).
- **Optional rasterize-back round-trip (pure-Go, approved deps only).** Rather
  than parse+raster the SVG string, rasterize the *traced paths* directly with
  `golang.org/x/image/vector.Rasterizer` (BSD-3, already approved): for each
  layer in z-order, add its subpaths to a rasterizer sized to working dims and
  `Draw` a uniform centroid-color source through the coverage mask onto an
  accumulating RGBA canvas; then compare against the downsampled source with
  `golang.org/x/image/draw` for any needed scaling. Assert **mean absolute
  per-channel difference** below a documented, deliberately loose bound
  (starting point: `< 24/255`, i.e. ~9%). Vectorization is lossy, so this is a
  regression tripwire (catch gross breakage like wrong z-order, dropped layers,
  or mis-mapped coordinates), not a fidelity gate. Keep it behind `-short` and
  allow it to run on a small downscale. (Nonzero winding means the direct
  rasterization must respect the same one-path-per-color merge as §8.3 to
  reproduce holes.)

### 11.4 k-means seeding for test stability (summary)

- **Default backend (`kmeansLloyd`, seeded):** exact/byte-stable assertions are
  valid, including cross-run determinism and identical-output checks.
- **If muesli backend is selected:** its wall-clock reseed makes exact output
  unstable. Tests must degrade to *tolerant* assertions — cluster **count** in a
  band, centroids matched within L2 tolerance, SVG validity/structure — and drop
  the byte-identical and exact-palette checks. Because `shapes.*` colors are
  well-separated, even muesli converges to the same partition in practice, so the
  small-fixture count/centroid assertions still hold; only `street_market` and
  byte-equality are fragile. The design’s recommendation is to keep the seeded
  in-house backend as default precisely so the strong assertions above are usable.

---

## 12. Risks & open decisions

1. **muesli/kmeans global `rand` (statelessness + determinism).** Highest-signal
   finding. Mitigation baked into the design: in-house seeded flat-buffer
   k-means as the default backend behind a `clusterer` interface; muesli kept as
   an interchangeable reference. No new dependency (stdlib `math/rand` only).
2. **`TurdSize==0` ambiguity in plan §9.** Resolved via sign convention
   (`0`=derive, `<0`=force-disable). Flagged on the field doc; revisit if the
   API review prefers a `*int`/sentinel.
3. **Hole rendering depends on winding, not `fill-rule`.** Correct today
   (nonzero + one-path-per-color). If a future palette/containment heuristic
   (plan §15) wants explicit `fill-rule="evenodd"`, `minisvg` must grow a way to
   set that attribute (currently `Path` only writes `d`+`fill`). Minor,
   additive.
4. **Sensitivity curve constants** (`4·2^(S/25)`, `8·2^(-S/25)`, `maxKForPixels`)
   are calibration starting points (plan §13 tuning pass), not final; centralized
   in `resolveDetail`/`maxKForPixels` for easy retuning.
5. **JPEG lossiness in tests.** Palette is computed from the decoded JPEG, so
   fills are exact against the engine’s own palette; tests must not compare a
   JPEG run’s fills to the PNG palette.
6. **Animated/lossless WEBP variants.** `golang.org/x/image/webp` decodes still
   frames; animated WEBP behavior (first-frame vs. error) is deferred (plan §15).
   v1 decodes whatever the codec returns as a single frame.
7. **Fit-subsampling determinism.** If enabled, the sample stride must be fixed
   (not RNG) to preserve byte-reproducibility; documented in §6.4.

---

## 13. Build order (engine milestone; `minisvg`/`bitrace` already exist)

1. `internal/imageutil` + `internal/normalize` (+ tests) — get decode/NRGBA/
   downsample green across all three decoders first (proves the WEBP import).
2. `internal/quantize`: `kmeansLloyd` + mask assembly + canonical sort (+ tests);
   `muesliAdapter` second, behind the interface.
3. `internal/pipeline`: worker pool (+ race-detector test).
4. `internal/assemble`: winding-aware one-path-per-color + minify (+ tests).
5. `config.go` (`normalized`, `resolveDetail`) + `vectrigo.go`
   (`Vectorize`/`Engine`/`Convert`) + `doc.go` + `example_test.go`.
6. End-to-end + `-short`-gated integration + optional round-trip.

Definition of done (per plan §13): builds with `CGO_ENABLED=0`, `go vet` clean,
`go test ./...` green (incl. `-race` on the pipeline), godoc + runnable example
present.
```
