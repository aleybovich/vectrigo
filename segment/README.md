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
  Euclidean RGB distance, and edges are greedily merged under an adaptive
  contrast predicate to yield a per-pixel region labelling.
- Offers edge-preserving **pre-filters** applied before the graph is built, so
  genuine boundaries dominate pixel noise and texture:
  - **Bilateral** (the recommended default): denoises within regions while
    keeping strong edges — text, feature lines — crisp.
  - **Kuwahara**: flattens interiors into flat, painterly patches.
  - **Gaussian**: a plain separable blur (legacy; smooths edges too).
- Applies optional deterministic **boundary smoothing** (a label-map mode
  filter) to round off the pixel-staircase jaggies that otherwise distort traced
  region outlines, with a small-region freeze that protects fine features such
  as sign lettering.
- Reports the mean colour of each region (`MeanColors`), computed from the
  original unfiltered image so region colours stay true.

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

## Key knobs

Tune the partition through `Options` (start from `DefaultOptions()`):

- **`K`** (scale) — the dominant control on region count. Larger `K` ⇒ larger,
  fewer regions; smaller `K` ⇒ more, finer regions. To hit a target count, sweep
  `K` (e.g. bisect); for a ~1024×559 photo, `K` in the low hundreds typically
  yields low-thousands of regions.
- **`RangeSigma`** (bilateral σ_r) — the primary detail-vs-smoothness dial. The
  reasonable range is roughly `[8, 40]`; `DefaultRangeSigma` (12) is balanced.
  Higher (~28–40) blends away low-contrast shading and small text; lower (~8)
  is punchier but region count climbs.
- **`MinSize`** (pixels) — a post-merge floor: any region smaller than `MinSize`
  is absorbed into a neighbour, removing speckle and thin slivers.
- **`BoundarySmooth`** — number of boundary-smoothing iterations (0 disables;
  1–5 suffice). Rounds off staircase jaggies while freezing small regions.

See the package documentation and the `Options` fields for the full guidance.

## Determinism

`Segment` is fully deterministic and pure Go: identical `(img, Options)` inputs
always produce an identical `Labels` slice. Edges sort by a total order, unions
break ties on the lower root index, region ids are assigned in row-major
first-appearance order, and no maps, `math/rand`, or wall clock drive any
order-dependent step. It builds with `CGO_ENABLED=0` on every platform.

## License

MIT — see [LICENSE](./LICENSE).
