# segment

`segment` is a minimal, **zero-dependency** graph-based image segmentation
library for Go.

It partitions an image into many small, spatially connected regions — a cheek,
a shadow, a brick — as a region-first alternative to global colour
quantization. Where quantization clusters pixels by colour (so one palette
colour scatters across the whole image and photographic content posterizes into
blobby, speckled planes), segmentation preserves local detail: a face becomes
hundreds of coherent tiles rather than a handful of quantized colour islands.
That makes it well suited to vectorizing detailed or photographic images. It
depends on nothing but the Go standard library, so it can be vendored, forked,
extracted into its own repository, or shipped independently without concern.

## What it does

- Implements **Felzenszwalb & Huttenlocher** efficient graph-based segmentation
  (IJCV 2004): pixels are nodes, 8-connected neighbours are edges weighted by
  Euclidean RGB distance, and edges are processed in non-decreasing weight order
  and greedily merged under an adaptive contrast predicate to yield a per-pixel
  region labelling. Every region is spatially connected by construction.
- Offers a selectable **pre-filter** applied before the graph is built, so
  genuine boundaries dominate pixel noise and texture:
  - **Bilateral** (the recommended default): edge-preserving; denoises within
    regions while keeping strong edges — text, feature lines — crisp. Best for
    photos.
  - **Kuwahara**: edge-preserving; flattens interiors into flat, painterly
    patches (a cel-shaded look).
  - **Gaussian**: a plain separable blur (legacy; smooths edges too).
- Applies optional deterministic **boundary smoothing** (a label-map mode
  filter) to round off the pixel-staircase jaggies that otherwise distort traced
  region outlines, with a small-region freeze that protects fine features such
  as sign lettering.
- Reports the mean colour of each region (`MeanColors`), computed from the
  original unfiltered image so region colours stay true.
- Handles transparency: pixels with alpha `< 128` take part in no region and are
  labelled `TransparentLabel`, so a region can never bleed across a transparent
  gap.

## Install

```sh
go get github.com/aleybovich/segment
```

## Usage

```go
package main

import (
	"fmt"
	"image"

	"github.com/aleybovich/segment"
)

func main() {
	var img *image.NRGBA // decode your image into an *image.NRGBA

	res := segment.Segment(img, segment.DefaultOptions())
	colors := segment.MeanColors(img, res)

	fmt.Printf("regions: %d\n", res.NumRegions)
	// res.Labels is row-major (index = y*W + x); Labels[i] is the region id in
	// [0, NumRegions), or segment.TransparentLabel (-1) for a transparent pixel.
	// colors[id] is the mean RGBA of region id.
	_ = colors
}
```

## API

### `func Segment(img *image.NRGBA, opt Options) Result`

Partitions `img`'s opaque pixels into spatially connected regions of similar
colour. It is deterministic: identical `(img, opt)` inputs always produce an
identical `Result`. Pixels with alpha `< 128` are excluded and labelled
`TransparentLabel`.

### `func MeanColors(img *image.NRGBA, r Result) []color.RGBA`

Returns the mean opaque colour of each region, indexed by region id: the slice
has length `r.NumRegions` and element `k` is the average R,G,B,A of the pixels
labelled `k`. Colours are computed from `img` (typically the original,
unsmoothed image), so region colours stay true regardless of any pre-filter.
Transparent pixels contribute to no region.

### `type Result`

| Field        | Type    | Meaning                                                                                                       |
| ------------ | ------- | ------------------------------------------------------------------------------------------------------------- |
| `Labels`     | `[]int` | Length `W*H`, row-major (`index = y*W + x`). Each entry is a dense region id in `[0, NumRegions)`, or `TransparentLabel` (`-1`) for an excluded transparent pixel. |
| `NumRegions` | `int`   | Number of distinct opaque regions (transparent pixels are not counted).                                       |
| `W`, `H`     | `int`   | Working dimensions of the segmented image.                                                                     |

### `const TransparentLabel = -1`

The sentinel region id assigned to pixels with alpha `< 128`. Such pixels form
no graph edges, are never merged into opaque regions, are not counted in
`NumRegions`, and have no entry in `MeanColors`' output. For fully opaque images
no pixel ever receives this label.

## Options

`Segment` is configured through `Options`. Start from `DefaultOptions()` and
adjust — most often `K` (region count) and `RangeSigma` (detail vs smoothness).

| Field            | Type        | Default (via `DefaultOptions`) | Meaning                                                                                                       |
| ---------------- | ----------- | ------------------------------ | ------------------------------------------------------------------------------------------------------------- |
| `K`              | `float64`   | `100`                          | Scale/threshold of the FH merge predicate. **The dominant control on region count.**                          |
| `MinSize`        | `int`       | `4`                            | Minimum region size in pixels after the main merge pass.                                                       |
| `Sigma`          | `float64`   | `0` (unset)                    | Legacy Gaussian pre-smoothing, honoured **only** when `PreFilter` is unset.                                    |
| `PreFilter`      | `PreFilter` | `PreFilterBilateral`           | Selects the pre-smoothing filter applied before the edge graph is built.                                       |
| `SpatialSigma`   | `float64`   | `2`                            | Spatial parameter (σ_s) of the selected pre-filter.                                                            |
| `RangeSigma`     | `float64`   | `DefaultRangeSigma` (`12`)     | Range/colour-difference parameter (σ_r) of the bilateral filter. **The primary detail-vs-smoothness dial.**    |
| `BoundarySmooth` | `int`       | `3`                            | Number of boundary-smoothing iterations applied after region formation.                                       |

### `K` (scale)

The dominant control. The merge predicate allows a boundary edge to merge two
components only while its weight stays within `K/|component|` of their internal
contrast, so `K` acts as a preference for larger components. Larger `K` ⇒
larger, fewer regions; smaller `K` ⇒ more, finer regions. Region count varies
roughly **inversely** with `K`. To target a region count, sweep `K` (e.g.
bisect) — for a ~1024×559 photo, `K` in the low hundreds typically yields
low-thousands of regions. Values `<= 0` are treated as a very small positive
scale (a maximally fine partition).

### `MinSize` (pixels)

A post-merge floor: any component smaller than `MinSize` is absorbed into the
neighbour across its lowest-weight boundary edge. Raising it removes small
regions (speckle, thin slivers) and lowers the count without much changing the
large-region structure. `0` or `1` disables the pass.

### `Sigma` (legacy Gaussian)

Retained for backward compatibility. It is honoured **only** when `PreFilter` is
left at its zero value (`PreFilterNone`): `Sigma > 0` then applies a Gaussian
blur of that sigma before the graph is built (the historical behaviour) and
`Sigma == 0` disables smoothing. When any explicit `PreFilter` is selected,
`Sigma` is ignored — except as a fallback radius for `PreFilterGaussian` when
`SpatialSigma` is unset. Region colours from `MeanColors` are always computed
from the original, unsmoothed image.

### `PreFilter` (selector)

Selects the pre-smoothing filter applied to the working image before the FH edge
graph is built. It affects only the edge weights that drive segmentation, not
the reported region colours. See [Pre-filters](#pre-filters) for the constants.

- `PreFilterNone` — no dedicated filter; routes through the legacy `Sigma` field
  (the zero value, so `Options{}` behaves exactly as before this field existed).
- `PreFilterGaussian` — separable Gaussian blur; smooths everything uniformly,
  including edges. Radius is `SpatialSigma` (falling back to `Sigma`).
- `PreFilterBilateral` — **edge-preserving**; denoises within regions while
  keeping strong edges crisp. The right choice for region-first vectorization of
  **photos**. Controlled by `SpatialSigma` and `RangeSigma`.
- `PreFilterKuwahara` — edge-preserving; flattens interiors into flat,
  **painterly** patches. Window radius is `int(SpatialSigma)`.

### `SpatialSigma` (σ_s)

The spatial parameter of the selected pre-filter: the bilateral/Gaussian blur
radius sigma, or, for Kuwahara, the window radius in pixels (via
`int(SpatialSigma)`). Larger ⇒ a wider window and stronger smoothing within
regions. Ignored when `PreFilter` is `PreFilterNone`.

### `RangeSigma` (σ_r)

The range (colour-difference) parameter of the bilateral filter: smaller values
preserve more edges, larger values blend across bigger colour differences. Only
`PreFilterBilateral` uses it. It is the primary **detail vs smoothness** dial for
photo vectorization. The reasonable range is roughly `[8, 40]`;
`DefaultRangeSigma` (`12`) is the balanced recommendation:

- **~28–40 (soft):** abstract output — low-contrast facial shading and small
  text get blended away.
- **~12 (default, balanced):** preserves facial contrast and small-sign
  lettering while still denoising smooth gradients into clean regions.
- **~8 (punchy):** even sharper, but region count climbs and faces can start to
  look harsh/over-segmented.
- **below ~8:** approaches no filtering — noise and pixel-jaggies return and the
  region count (and file size) grows sharply.

Values `<= 0` make the bilateral filter a no-op (identity).

### `BoundarySmooth` (iterations)

The number of boundary-smoothing iterations applied to the region label map
**after** region formation and the `MinSize` merge, to round off the
pixel-staircase jaggies (tiny 1–2px protrusions and notches) that otherwise make
traced region outlines look distorted. Each iteration is a deterministic
label-map mode (majority) filter: a boundary pixel may flip to the dominant
neighbouring region label, so convex teeth erode and concave notches fill. A
**small-region freeze** protects fine features (sign lettering only a few pixels
across) from being dissolved, and a connected-component relabel keeps the output
a valid, connected partition. The zero value disables smoothing entirely; a few
iterations (1–5) suffice. Cost is `O(BoundarySmooth · W · H)`.

### `func DefaultOptions() Options`

Returns the recommended settings for region-first vectorization of a photographic
image, tuned on a ~1024×559 image: an edge-preserving bilateral pre-filter, fine
regions, and a few boundary-smoothing iterations. It is the recommended starting
point.

```go
Options{
	K:              100,
	MinSize:        4,
	PreFilter:      PreFilterBilateral,
	SpatialSigma:   2,
	RangeSigma:     DefaultRangeSigma, // 12
	BoundarySmooth: 3,
}
```

### `const DefaultRangeSigma = 12`

The recommended bilateral `RangeSigma` (σ_r): the balanced point between
preserving fine contrast (punchy faces, legible small text) and denoising smooth
gradients into clean regions.

## Pre-filters

The pre-filter functions are also exported directly, so callers can smooth an
image independently of segmentation. Each returns a new `*image.NRGBA` of the
same bounds, passes the alpha channel through unchanged, filters only R,G,B, and
is fully deterministic.

### `type PreFilter int`

An `int`-based selector with four constants, used by `Options.PreFilter`:
`PreFilterNone` (the zero value), `PreFilterGaussian`, `PreFilterBilateral`, and
`PreFilterKuwahara`. See [`PreFilter`](#prefilter-selector) above for their
behaviour.

### `func BilateralFilter(img *image.NRGBA, spatialSigma, rangeSigma float64) *image.NRGBA`

Edge-preserving bilateral filter. Each output pixel is a normalized weighted
average over a bounded square window, each neighbour weighted by the product of a
spatial Gaussian (σ_s = `spatialSigma`) and a range Gaussian (σ_r =
`rangeSigma`); across a strong edge the range weight collapses toward zero, so
the edge is preserved. The window half-width is bounded to `⌈2.5·σ_s⌉`;
non-positive `spatialSigma` or `rangeSigma` yield an identity copy.

### `func KuwaharaFilter(img *image.NRGBA, radius int) *image.NRGBA`

Kuwahara filter: for each pixel the `(2r+1)×(2r+1)` neighbourhood is split into
four overlapping quadrants and the pixel is replaced by the mean colour of the
lowest-luminance-variance quadrant, preserving edges while flattening interiors
into painterly patches. A `radius < 1` yields an identity copy.

### `func GaussianBlur(img *image.NRGBA, sigma float64) *image.NRGBA`

Separable (horizontal-then-vertical) Gaussian blur of standard deviation `sigma`,
kernel radius `⌈3·σ⌉`. A pure-Go reimplementation of the legacy Gaussian
pre-filter; a `sigma <= 0` yields an identity copy.

## Determinism

`Segment` is fully deterministic and pure Go: identical `(img, Options)` inputs
always produce an identical `Labels` slice. Edges sort by a total order (weight,
then the ordered endpoint pair), unions break ties on the lower root index,
dense region ids are assigned in row-major first-appearance order, and no maps,
`math/rand`, or wall clock drive any order-dependent step. The pre-filters and
boundary smoothing are likewise deterministic (fixed traversal order,
double-buffered pure passes, lowest-label-id tie-break). It builds with
`CGO_ENABLED=0` on every platform and depends only on the Go standard library.

## License

MIT — see [LICENSE](./LICENSE).
