package quantize

import (
	"image"
	"testing"
)

// TestMuesliAdapter exercises the approved-dependency reference backend
// directly. Because muesli/kmeans reseeds process-global math/rand from the
// wall clock, its output is only statistically stable, so this test uses
// tolerant assertions on well-separated colours (not byte-equality).
func TestMuesliAdapter(t *testing.T) {
	img := quadImage(40, 40, quadColors)

	centroids, labels, err := muesliAdapter{}.fit(img.NRGBA.Pix, 40, 40, 4)
	if err != nil {
		t.Fatalf("muesli fit: %v", err)
	}
	if len(centroids) != 4 {
		t.Fatalf("centroid count = %d, want 4", len(centroids))
	}

	// Every opaque pixel got a valid label; masks partition the field.
	counts := make([]int, len(centroids))
	for _, lb := range labels {
		if lb < 0 || lb >= len(centroids) {
			t.Fatalf("invalid label %d", lb)
		}
		counts[lb]++
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	if total != 40*40 {
		t.Errorf("labelled pixels = %d, want %d", total, 40*40)
	}

	// Each source colour is matched by some centroid within a tolerant L2.
	for _, want := range quadColors {
		best := 1 << 30
		for _, c := range centroids {
			dr := int(c[0]) - int(want.R)
			dg := int(c[1]) - int(want.G)
			db := int(c[2]) - int(want.B)
			if d := dr*dr + dg*dg + db*db; d < best {
				best = d
			}
		}
		if best > 400 {
			t.Errorf("no muesli centroid near %v (min sq dist %d)", want, best)
		}
	}
}

func TestMuesliAdapterAllTransparent(t *testing.T) {
	nr := image.NewNRGBA(image.Rect(0, 0, 8, 8)) // all zero => transparent
	centroids, labels, err := muesliAdapter{}.fit(nr.Pix, 8, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	if centroids != nil {
		t.Errorf("centroids = %v, want nil for all-transparent", centroids)
	}
	for _, lb := range labels {
		if lb != -1 {
			t.Errorf("label = %d, want -1 for transparent pixel", lb)
		}
	}
}
