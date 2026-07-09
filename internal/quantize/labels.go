package quantize

import (
	"image/color"

	"github.com/aleybovich/vectrigo/internal/imageutil"
	"github.com/aleybovich/vectrigo/internal/normalize"
)

// Labels clusters img's opaque pixels into at most k colours exactly like
// [Quantize] (same clusterer, same determinism), but returns the raw per-pixel
// assignment instead of per-cluster masks: labels has length W*H, row-major;
// labels[i] is the cluster id in [0,len(palette)) for an opaque pixel and -1
// for a transparent one (alpha < 128). palette[c] is cluster c's centroid,
// forced opaque; clusters that ended up with no pixels keep their palette slot
// (no label references them), so palette can be indexed directly by label.
//
// This is the entry point for the GAPLESS pipeline, which needs the label map
// itself (to split clusters into spatially-connected regions and trace them as
// one planar subdivision) rather than one overlapping mask per colour. An
// all-transparent or empty input yields labels of all -1 (or nil) and a nil
// palette, with no error.
func Labels(img normalize.Image, k int) (labels []int, palette []color.RGBA, err error) {
	b := img.NRGBA.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 || k < 1 {
		return nil, nil, nil
	}

	centroids, labels, err := defaultClusterer.fit(img.NRGBA.Pix, w, h, k)
	if err != nil {
		return nil, nil, err
	}

	palette = make([]color.RGBA, len(centroids))
	for c := range centroids {
		palette[c] = color.RGBA{
			R: imageutil.Round8(centroids[c][0]),
			G: imageutil.Round8(centroids[c][1]),
			B: imageutil.Round8(centroids[c][2]),
			A: 255,
		}
	}
	return labels, palette, nil
}
