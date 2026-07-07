// Package pipeline implements Stage III of the vectrigo pipeline: vectorize
// each quantized layer's mask with bitrace, concurrently, via a per-call
// worker pool.
package pipeline

import (
	"errors"
	"image/color"
	"sync"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/vectrigo/internal/quantize"
)

// Traced is a [quantize.Layer] after vectorization: its centroid colour and
// area plus the contours bitrace produced for its mask.
type Traced struct {
	// Color is the layer's centroid colour, carried through from quantize.
	Color color.RGBA
	// Area is the layer's pixel count, used for z-ordering in Stage IV.
	Area int
	// Paths holds the traced contours: outer boundaries (IsHole=false) and
	// holes (IsHole=true), in bitrace's winding convention.
	Paths []bitrace.Path
}

// TraceLayers vectorizes each layer's mask with bitrace using cfg, running up
// to workers goroutines. The result order matches the input layers slice, so
// the canonical order established by quantize survives. workers <= 0 is treated
// as 1; it is capped to len(layers) since there is never work for more workers
// than layers.
//
// The worker pool is created and torn down per call — there is no shared global
// pool — preserving statelessness and concurrency safety. bitrace.Trace is
// documented reentrant and non-mutating, so sharing masks across goroutines is
// safe.
func TraceLayers(layers []quantize.Layer, cfg bitrace.Config, workers int) ([]Traced, error) {
	n := len(layers)
	if n == 0 {
		return nil, nil
	}
	if workers > n {
		workers = n
	}
	if workers < 1 {
		workers = 1
	}

	out := make([]Traced, n)
	errs := make([]error, n)
	jobs := make(chan int, n)

	var wg sync.WaitGroup
	for wk := 0; wk < workers; wk++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				lyr := layers[idx]
				paths, err := bitrace.Trace(lyr.Mask, cfg)
				if err != nil {
					errs[idx] = err
					continue
				}
				out[idx] = Traced{Color: lyr.Color, Area: lyr.Area, Paths: paths}
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}
