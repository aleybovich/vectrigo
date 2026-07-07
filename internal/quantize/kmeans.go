package quantize

import (
	"math"
	"math/rand"
)

// kmeansSeed fixes the RNG so a given (image, k) always yields identical
// centroids, masks, and therefore byte-identical SVG output.
const kmeansSeed = 1

// maxFitSamples caps the number of pixels used to *fit* centroids. On large
// images centroids are fitted on a deterministic stride sample of at most this
// many opaque pixels; every opaque pixel is then assigned to its nearest
// fitted centroid for the masks. The stride is fixed (not RNG-driven) so
// output stays byte-reproducible.
const maxFitSamples = 200000

// maxIter bounds Lloyd iterations.
const maxIter = 32

// kmeansLloyd is the default, deterministic clustering backend: a flat-buffer
// k-means++ initialization followed by Lloyd iteration, seeded from a fixed
// constant via an injected *rand.Rand (no global state).
type kmeansLloyd struct{}

func (kmeansLloyd) fit(pix []uint8, w, h, k int) ([][3]float64, []int, error) {
	n := w * h
	labels := make([]int, n)

	// Collect opaque pixel indices (alpha >= 128). Transparent pixels get
	// label -1 and take part in no cluster.
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

	// Deterministic stride sample for the fit step.
	sample := opaque
	if len(opaque) > maxFitSamples {
		stride := len(opaque)/maxFitSamples + 1
		sample = make([]int, 0, len(opaque)/stride+1)
		for i := 0; i < len(opaque); i += stride {
			sample = append(sample, opaque[i])
		}
	}

	rng := rand.New(rand.NewSource(kmeansSeed))
	centroids := kmeansPlusPlus(pix, sample, k, rng)

	// Lloyd iteration over the sample.
	sampLabels := make([]int, len(sample))
	for i := range sampLabels {
		sampLabels[i] = -1
	}
	sum := make([][3]float64, k)
	counts := make([]int, k)
	for iter := 0; iter < maxIter; iter++ {
		changed := false
		for si, idx := range sample {
			c := nearest(centroids, rgbAt(pix, idx))
			if c != sampLabels[si] {
				sampLabels[si] = c
				changed = true
			}
		}
		for c := range sum {
			sum[c] = [3]float64{}
			counts[c] = 0
		}
		for si, idx := range sample {
			c := sampLabels[si]
			r, g, b := rgb3(pix, idx)
			sum[c][0] += r
			sum[c][1] += g
			sum[c][2] += b
			counts[c]++
		}
		for c := 0; c < k; c++ {
			if counts[c] > 0 {
				centroids[c][0] = sum[c][0] / float64(counts[c])
				centroids[c][1] = sum[c][1] / float64(counts[c])
				centroids[c][2] = sum[c][2] / float64(counts[c])
			} else {
				// Empty cluster: reseed deterministically to a random sample
				// point so k centroids stay live.
				centroids[c] = rgbAt(pix, sample[rng.Intn(len(sample))])
			}
		}
		if !changed && iter > 0 {
			break
		}
	}

	// Final assignment: every opaque pixel to its nearest fitted centroid.
	for _, idx := range opaque {
		labels[idx] = nearest(centroids, rgbAt(pix, idx))
	}
	return centroids, labels, nil
}

// kmeansPlusPlus chooses k initial centroids from sample using the k-means++
// weighted seeding scheme, drawing from rng.
func kmeansPlusPlus(pix []uint8, sample []int, k int, rng *rand.Rand) [][3]float64 {
	centroids := make([][3]float64, 0, k)
	centroids = append(centroids, rgbAt(pix, sample[rng.Intn(len(sample))]))

	dist := make([]float64, len(sample))
	for i := range dist {
		dist[i] = math.MaxFloat64
	}

	for len(centroids) < k {
		last := centroids[len(centroids)-1]
		total := 0.0
		for i, idx := range sample {
			if d := sqDist(rgbAt(pix, idx), last); d < dist[i] {
				dist[i] = d
			}
			total += dist[i]
		}
		if total == 0 {
			// All sample points coincide with an existing centroid; pad with a
			// deterministic pick so the count still reaches k.
			centroids = append(centroids, rgbAt(pix, sample[rng.Intn(len(sample))]))
			continue
		}
		target := rng.Float64() * total
		acc := 0.0
		chosen := sample[len(sample)-1]
		for i, idx := range sample {
			acc += dist[i]
			if acc >= target {
				chosen = idx
				break
			}
		}
		centroids = append(centroids, rgbAt(pix, chosen))
	}
	return centroids
}

// rgbAt returns the RGB triple of the pixel at flat index idx.
func rgbAt(pix []uint8, idx int) [3]float64 {
	o := idx * 4
	return [3]float64{float64(pix[o]), float64(pix[o+1]), float64(pix[o+2])}
}

// rgb3 is rgbAt unpacked into three returns (avoids array copies in hot loops).
func rgb3(pix []uint8, idx int) (r, g, b float64) {
	o := idx * 4
	return float64(pix[o]), float64(pix[o+1]), float64(pix[o+2])
}

// nearest returns the index of the centroid closest (squared Euclidean) to p.
func nearest(centroids [][3]float64, p [3]float64) int {
	best := 0
	bestD := math.MaxFloat64
	for c := range centroids {
		if d := sqDist(centroids[c], p); d < bestD {
			bestD = d
			best = c
		}
	}
	return best
}

// sqDist is the squared Euclidean distance between two RGB triples.
func sqDist(a, b [3]float64) float64 {
	dr := a[0] - b[0]
	dg := a[1] - b[1]
	db := a[2] - b[2]
	return dr*dr + dg*dg + db*db
}
