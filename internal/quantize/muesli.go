package quantize

import (
	"fmt"

	"github.com/muesli/clusters"
	"github.com/muesli/kmeans"
)

// muesliAdapter wraps github.com/muesli/kmeans behind the [clusterer]
// interface as an approved-dependency reference backend.
//
// It is NOT the default. muesli/kmeans reseeds the process-global math/rand
// source from the wall clock inside clusters.New and uses global rand.Intn for
// empty-cluster reseeding, so it is neither deterministic nor free of
// package-level mutable state — both of which collide with vectrigo's hard
// constraints. It is retained only so the pipeline can be pointed at the
// approved third-party clusterer (e.g. for comparison) with a one-line change
// to defaultClusterer.
type muesliAdapter struct{}

func (muesliAdapter) fit(pix []uint8, w, h, k int) ([][3]float64, []int, error) {
	n := w * h
	labels := make([]int, n)

	opaque := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if pix[i*4+3] >= 128 {
			opaque = append(opaque, i)
		} else {
			labels[i] = -1
		}
	}
	if len(opaque) == 0 {
		return nil, labels, nil
	}
	if k > len(opaque) {
		k = len(opaque)
	}

	// muesli documents observations as floats; using [0,1] keeps the distance
	// metric well conditioned. Centroids are scaled back to [0,255].
	obs := make(clusters.Observations, len(opaque))
	for i, idx := range opaque {
		o := idx * 4
		obs[i] = clusters.Coordinates{
			float64(pix[o]) / 255,
			float64(pix[o+1]) / 255,
			float64(pix[o+2]) / 255,
		}
	}

	cl, err := kmeans.New().Partition(obs, k)
	if err != nil {
		return nil, nil, fmt.Errorf("muesli kmeans: %w", err)
	}

	centroids := make([][3]float64, len(cl))
	for c := range cl {
		center := cl[c].Center
		centroids[c] = [3]float64{center[0] * 255, center[1] * 255, center[2] * 255}
	}

	// Assign every opaque pixel to its nearest centroid (muesli's cluster
	// membership is over the observation slice, not the pixel grid).
	for _, idx := range opaque {
		labels[idx] = nearest(centroids, rgbAt(pix, idx))
	}
	return centroids, labels, nil
}
