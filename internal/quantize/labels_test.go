package quantize

import (
	"image"
	"image/color"
	"reflect"
	"testing"

	"github.com/aleybovich/vectrigo/internal/normalize"
)

// labelsImage builds a normalize.Image from an NRGBA for Labels tests.
func labelsImage(img *image.NRGBA) normalize.Image {
	b := img.Bounds()
	return normalize.Image{NRGBA: img, OrigW: b.Dx(), OrigH: b.Dy()}
}

// TestLabelsMatchesQuantize checks that Labels is the same clustering as
// Quantize seen through a different lens: rebuilding per-cluster masks from
// the label map must reproduce Quantize's layers (same colours, same areas,
// same member pixels).
func TestLabelsMatchesQuantize(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 4))
	// Left half red, right half blue, one transparent pixel.
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			c := color.NRGBA{R: 220, G: 30, B: 30, A: 255}
			if x >= 4 {
				c = color.NRGBA{R: 30, G: 30, B: 220, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	img.SetNRGBA(0, 0, color.NRGBA{})

	ni := labelsImage(img)
	labels, palette, err := Labels(ni, 2)
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}
	layers, err := Quantize(ni, 2)
	if err != nil {
		t.Fatalf("Quantize: %v", err)
	}

	if len(labels) != 8*4 {
		t.Fatalf("labels length = %d, want %d", len(labels), 8*4)
	}
	if labels[0] != -1 {
		t.Errorf("transparent pixel label = %d, want -1", labels[0])
	}

	// Rebuild masks per cluster id from labels and match each against the
	// Quantize layer with the same colour.
	for c := range palette {
		mask := make([]bool, len(labels))
		area := 0
		for i, lb := range labels {
			if lb == c {
				mask[i] = true
				area++
			}
		}
		if area == 0 {
			continue // empty clusters keep a palette slot but have no layer
		}
		found := false
		for _, ly := range layers {
			if ly.Color == palette[c] {
				found = true
				if ly.Area != area {
					t.Errorf("cluster %d: area %d via labels, %d via Quantize", c, area, ly.Area)
				}
				if !reflect.DeepEqual(ly.Mask.Bits, mask) {
					t.Errorf("cluster %d: mask rebuilt from labels differs from Quantize's", c)
				}
			}
		}
		if !found {
			t.Errorf("cluster %d (colour %v, area %d) has no matching Quantize layer", c, palette[c], area)
		}
	}
}

// TestLabelsPaletteOpaque checks every palette entry is forced fully opaque.
func TestLabelsPaletteOpaque(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < 16; i++ {
		img.Pix[i*4+0] = uint8(i * 16)
		img.Pix[i*4+1] = 100
		img.Pix[i*4+2] = 200
		img.Pix[i*4+3] = 255
	}
	_, palette, err := Labels(labelsImage(img), 3)
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}
	if len(palette) == 0 {
		t.Fatal("empty palette for opaque input")
	}
	for c, col := range palette {
		if col.A != 255 {
			t.Errorf("palette[%d].A = %d, want 255", c, col.A)
		}
	}
}

// TestLabelsDegenerate pins the no-error contracts: zero-size or k < 1 yields
// nils; an all-transparent image yields all -1 labels and a nil palette.
func TestLabelsDegenerate(t *testing.T) {
	empty := image.NewNRGBA(image.Rect(0, 0, 0, 0))
	if l, p, err := Labels(labelsImage(empty), 4); err != nil || l != nil || p != nil {
		t.Errorf("empty image: got (%v, %v, %v), want nils", l, p, err)
	}

	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	if l, p, err := Labels(labelsImage(img), 0); err != nil || l != nil || p != nil {
		t.Errorf("k=0: got (%v, %v, %v), want nils", l, p, err)
	}

	// All-transparent: labels exist but are all -1.
	labels, palette, err := Labels(labelsImage(img), 4)
	if err != nil {
		t.Fatalf("all-transparent: %v", err)
	}
	if len(palette) != 0 {
		t.Errorf("all-transparent palette = %v, want empty", palette)
	}
	for i, lb := range labels {
		if lb != -1 {
			t.Errorf("all-transparent label[%d] = %d, want -1", i, lb)
		}
	}
}
