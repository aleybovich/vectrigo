package quantize

import (
	"image"
	"image/color"
	"reflect"
	"testing"

	"github.com/aleybovich/vectrigo/internal/normalize"
)

// quadImage builds a w×h NRGBA split into four solid colour quadrants.
func quadImage(w, h int, cols [4]color.NRGBA) normalize.Image {
	nr := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			q := 0
			if x >= w/2 {
				q |= 1
			}
			if y >= h/2 {
				q |= 2
			}
			nr.SetNRGBA(x, y, cols[q])
		}
	}
	return normalize.Image{NRGBA: nr, OrigW: w, OrigH: h}
}

var quadColors = [4]color.NRGBA{
	{R: 230, G: 20, B: 20, A: 255},  // red
	{R: 20, G: 230, B: 20, A: 255},  // green
	{R: 20, G: 20, B: 230, A: 255},  // blue
	{R: 230, G: 230, B: 20, A: 255}, // yellow
}

func TestQuantizeFourQuadrants(t *testing.T) {
	img := quadImage(40, 40, quadColors)
	layers, err := Quantize(img, 4)
	if err != nil {
		t.Fatalf("Quantize: %v", err)
	}
	if len(layers) != 4 {
		t.Fatalf("layer count = %d, want 4", len(layers))
	}

	// Each source colour must be matched by some centroid within a small L2.
	for _, want := range quadColors {
		best := 1 << 30
		for _, l := range layers {
			dr := int(l.Color.R) - int(want.R)
			dg := int(l.Color.G) - int(want.G)
			db := int(l.Color.B) - int(want.B)
			if d := dr*dr + dg*dg + db*db; d < best {
				best = d
			}
		}
		if best > 100 { // L2 distance ~10 per channel
			t.Errorf("no centroid near %v (min sq dist %d)", want, best)
		}
	}

	// Masks must partition the opaque pixels: total area == opaque count and no
	// pixel is set in two masks.
	n := 40 * 40
	seen := make([]int, n)
	total := 0
	for _, l := range layers {
		if l.Color.A != 255 {
			t.Errorf("centroid alpha = %d, want 255", l.Color.A)
		}
		for i, on := range l.Mask.Bits {
			if on {
				seen[i]++
				total += 1
			}
		}
	}
	if total != n {
		t.Errorf("total mask area = %d, want %d", total, n)
	}
	for i, c := range seen {
		if c != 1 {
			t.Fatalf("pixel %d set in %d masks, want exactly 1", i, c)
		}
	}
}

func TestQuantizeDeterministic(t *testing.T) {
	img := quadImage(40, 40, quadColors)
	a, err := Quantize(img, 4)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Quantize(img, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("Quantize is not deterministic across calls")
	}
}

func TestQuantizeKClampToDistinctColors(t *testing.T) {
	// Only two distinct colours present but ask for many clusters.
	img := quadImage(8, 8, [4]color.NRGBA{
		{R: 0, G: 0, B: 0, A: 255},
		{R: 0, G: 0, B: 0, A: 255},
		{R: 255, G: 255, B: 255, A: 255},
		{R: 255, G: 255, B: 255, A: 255},
	})
	layers, err := Quantize(img, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) > 2 {
		t.Fatalf("layer count = %d, want <= 2 distinct colours", len(layers))
	}
}

func TestQuantizeTransparentExcluded(t *testing.T) {
	w, h := 10, 10
	nr := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < 5 {
				nr.SetNRGBA(x, y, color.NRGBA{R: 200, G: 10, B: 10, A: 255})
			} else {
				nr.SetNRGBA(x, y, color.NRGBA{R: 200, G: 10, B: 10, A: 10}) // transparent
			}
		}
	}
	img := normalize.Image{NRGBA: nr, OrigW: w, OrigH: h}
	layers, err := Quantize(img, 4)
	if err != nil {
		t.Fatal(err)
	}
	// No mask may include any transparent (x>=5) pixel.
	for _, l := range layers {
		for y := 0; y < h; y++ {
			for x := 5; x < w; x++ {
				if l.Mask.Bits[y*w+x] {
					t.Fatalf("transparent pixel (%d,%d) set in a mask", x, y)
				}
			}
		}
	}
}

func TestQuantizeAllTransparent(t *testing.T) {
	w, h := 8, 8
	nr := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range nr.Pix {
		nr.Pix[i] = 0 // fully transparent
	}
	img := normalize.Image{NRGBA: nr, OrigW: w, OrigH: h}
	layers, err := Quantize(img, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 0 {
		t.Fatalf("layer count = %d, want 0 for all-transparent input", len(layers))
	}
}

func TestQuantizeCanonicalOrder(t *testing.T) {
	// Larger area must come first.
	w, h := 10, 10
	nr := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if y < 8 {
				nr.SetNRGBA(x, y, color.NRGBA{R: 10, G: 10, B: 200, A: 255}) // 80 px
			} else {
				nr.SetNRGBA(x, y, color.NRGBA{R: 200, G: 10, B: 10, A: 255}) // 20 px
			}
		}
	}
	img := normalize.Image{NRGBA: nr, OrigW: w, OrigH: h}
	layers, err := Quantize(img, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 2 {
		t.Fatalf("layer count = %d, want 2", len(layers))
	}
	if layers[0].Area < layers[1].Area {
		t.Errorf("layers not ordered by area desc: %d then %d", layers[0].Area, layers[1].Area)
	}
}
