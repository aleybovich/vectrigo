package pipeline

import (
	"errors"
	"image/color"
	"sync"

	"github.com/aleybovich/bitrace"
)

// TraceRegions vectorizes each segmentation region's mask with bitrace, running
// up to workers goroutines, and returns one [Traced] per region indexed by
// region id (result[r] describes region r), so the deterministic region-id
// order established by segment.Segment survives. It is the photo-mode analogue
// of [TraceLayers].
//
// labels is segment.Result.Labels: length w*h, row-major, with each entry the
// dense region id in [0,numRegions) or a negative sentinel for a transparent
// pixel. colors is segment.MeanColors' output (length numRegions). cfg is passed
// straight to bitrace (TurdSize should be 0 for photo mode — regions are already
// size-floored by segment's MinSize).
//
// Memory: this is deliberately NOT O(regions·w·h). Region membership is stored
// once as a CSR grouping of opaque pixel indices — O(w·h) total across all
// regions, independent of region count — and tracing uses a worker pool where
// each worker owns exactly ONE reusable w×h scratch []bool. A worker sets only
// its current region's pixels (O(area)), traces, then clears only those same
// pixels (O(area)) before the next region. Peak scratch memory is therefore
// workers·w·h bools, never regions·w·h, which matters at ~10k regions where the
// naive per-region mask would be gigabytes.
//
// The worker pool is created and torn down per call (no shared global pool),
// preserving statelessness and concurrency safety. bitrace.Trace is documented
// reentrant and non-mutating.
func TraceRegions(labels []int, numRegions, w, h int, colors []color.RGBA, cfg bitrace.Config, workers int) ([]Traced, error) {
	if numRegions <= 0 || w == 0 || h == 0 {
		return nil, nil
	}

	// CSR grouping: areas -> offsets (prefix sums) -> flat index buffer. flat
	// holds every opaque pixel index exactly once, partitioned by region, so its
	// total size is the opaque pixel count (<= w*h), not numRegions*w*h.
	areas := make([]int, numRegions)
	for _, lb := range labels {
		if lb >= 0 && lb < numRegions {
			areas[lb]++
		}
	}
	offsets := make([]int, numRegions+1)
	for r := 0; r < numRegions; r++ {
		offsets[r+1] = offsets[r] + areas[r]
	}
	flat := make([]int32, offsets[numRegions])
	cursor := make([]int, numRegions)
	copy(cursor, offsets[:numRegions])
	for i, lb := range labels {
		if lb < 0 || lb >= numRegions {
			continue
		}
		flat[cursor[lb]] = int32(i)
		cursor[lb]++
	}

	if workers > numRegions {
		workers = numRegions
	}
	if workers < 1 {
		workers = 1
	}

	out := make([]Traced, numRegions)
	errs := make([]error, numRegions)
	jobs := make(chan int, numRegions)

	var wg sync.WaitGroup
	for wk := 0; wk < workers; wk++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// One reusable w×h scratch buffer per worker: bounded memory.
			scratch := make([]bool, w*h)
			for r := range jobs {
				idxs := flat[offsets[r]:offsets[r+1]]
				for _, pi := range idxs {
					scratch[pi] = true
				}
				paths, err := bitrace.Trace(bitrace.Bitmap{W: w, H: h, Bits: scratch}, cfg)
				// Clear only the pixels we set — O(area), so scratch is clean for
				// the next region without an O(w*h) wipe.
				for _, pi := range idxs {
					scratch[pi] = false
				}
				if err != nil {
					errs[r] = err
					continue
				}
				c := color.RGBA{R: colors[r].R, G: colors[r].G, B: colors[r].B, A: 255}
				out[r] = Traced{Color: c, Area: len(idxs), Paths: paths}
			}
		}()
	}
	for r := 0; r < numRegions; r++ {
		jobs <- r
	}
	close(jobs)
	wg.Wait()

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}
