// Package quantize implements Stage II of the vectrigo pipeline: cluster a
// normalized image's opaque pixels into at most K representative colours and
// emit one [Layer] per non-empty cluster, each pairing a centroid colour with
// the flat binary mask of the pixels assigned to it.
//
// Clustering is performed by a deterministic, seeded in-house k-means (see
// kmeans.go). A muesli/kmeans adapter is provided behind the same interface
// (muesli.go) as an approved-dependency reference, but is not the default
// because it cannot be seeded (it reseeds process-global math/rand from the
// wall clock) and so is neither deterministic nor free of global state.
package quantize

import (
	"image/color"
	"sort"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/vectrigo/internal/imageutil"
	"github.com/aleybovich/vectrigo/internal/normalize"
)

// Layer is one quantized colour plane: a centroid colour plus the binary mask
// of the pixels assigned to it. Area is the mask popcount, used for
// z-ordering downstream.
type Layer struct {
	// Color is the cluster centroid, opaque (A=255).
	Color color.RGBA
	// Mask has W,H equal to the working dimensions; Bits[i] is true when
	// pixel i belongs to this cluster.
	Mask bitrace.Bitmap
	// Area is the number of set bits in Mask (the cluster's pixel count).
	Area int
}

// clusterer is the pluggable clustering backend. fit returns k centroids (each
// an RGB triple with channels in [0,255]) and a per-pixel label slice of
// length w*h: label[i] in [0,k) for opaque pixels, -1 for skipped transparent
// pixels. The number of distinct labels used may be fewer than k.
type clusterer interface {
	fit(pix []uint8, w, h, k int) (centroids [][3]float64, labels []int, err error)
}

// defaultClusterer is the backend used by [Quantize]. It is the seeded
// in-house k-means, chosen for determinism and statelessness.
var defaultClusterer clusterer = kmeansLloyd{}

// Quantize clusters img's opaque pixels into at most k colour layers. Pixels
// with alpha < 128 are treated as transparent: they are excluded from
// clustering and left off in every mask (so they render as SVG background).
//
// The returned layers are in canonical order — primary by Area descending
// (largest first), tie-broken by packed 0xRRGGBB ascending — so downstream
// ordering is reproducible regardless of the clustering backend's arbitrary
// label assignment. An all-transparent input yields zero layers and no error.
func Quantize(img normalize.Image, k int) ([]Layer, error) {
	b := img.NRGBA.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 || k < 1 {
		return nil, nil
	}

	centroids, labels, err := defaultClusterer.fit(img.NRGBA.Pix, w, h, k)
	if err != nil {
		return nil, err
	}
	return assembleLayers(centroids, labels, w, h), nil
}

// assembleLayers builds one Layer per non-empty cluster from the centroid
// table and per-pixel labels, then sorts them into canonical order.
func assembleLayers(centroids [][3]float64, labels []int, w, h int) []Layer {
	k := len(centroids)
	if k == 0 {
		return nil
	}

	areas := make([]int, k)
	for _, lb := range labels {
		if lb >= 0 {
			areas[lb]++
		}
	}

	// Count non-empty clusters so all masks can share a single contiguous
	// backing allocation instead of one heap allocation per layer. Each layer's
	// Bits is a disjoint sub-slice of this buffer, so the bytes handed to
	// bitrace are identical to the previous per-layer make([]bool, w*h).
	nLayers := 0
	for c := 0; c < k; c++ {
		if areas[c] != 0 {
			nLayers++
		}
	}
	plane := w * h
	buf := make([]bool, nLayers*plane)

	// Pre-allocate one flat mask per non-empty cluster and map cluster index
	// to its Layer slot. maskBits gives the fill loop direct slice access,
	// avoiding a struct load per pixel.
	slot := make([]int, k)
	maskBits := make([][]bool, nLayers)
	layers := make([]Layer, 0, nLayers)
	for c := 0; c < k; c++ {
		if areas[c] == 0 {
			slot[c] = -1
			continue
		}
		s := len(layers)
		slot[c] = s
		bits := buf[s*plane : (s+1)*plane : (s+1)*plane]
		maskBits[s] = bits
		layers = append(layers, Layer{
			Color: color.RGBA{
				R: imageutil.Round8(centroids[c][0]),
				G: imageutil.Round8(centroids[c][1]),
				B: imageutil.Round8(centroids[c][2]),
				A: 255,
			},
			Mask: bitrace.Bitmap{W: w, H: h, Bits: bits},
			Area: areas[c],
		})
	}

	// One pass over labels to fill every mask.
	for i, lb := range labels {
		if lb < 0 {
			continue
		}
		if s := slot[lb]; s >= 0 {
			maskBits[s][i] = true
		}
	}

	sort.SliceStable(layers, func(i, j int) bool {
		if layers[i].Area != layers[j].Area {
			return layers[i].Area > layers[j].Area
		}
		return imageutil.Pack(layers[i].Color) < imageutil.Pack(layers[j].Color)
	})
	return layers
}
