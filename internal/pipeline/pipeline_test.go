package pipeline

import (
	"image/color"
	"testing"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/vectrigo/internal/quantize"
)

// squareLayer builds a Layer whose mask is a filled rectangle inset by 1 px in
// an w×h field, coloured c with the given area label.
func squareLayer(w, h int, c color.RGBA) quantize.Layer {
	bits := make([]bool, w*h)
	area := 0
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			bits[y*w+x] = true
			area++
		}
	}
	return quantize.Layer{
		Color: c,
		Mask:  bitrace.Bitmap{W: w, H: h, Bits: bits},
		Area:  area,
	}
}

func TestTraceLayersOrderAndColor(t *testing.T) {
	layers := []quantize.Layer{
		squareLayer(20, 20, color.RGBA{R: 255, A: 255}),
		squareLayer(20, 20, color.RGBA{G: 255, A: 255}),
		squareLayer(20, 20, color.RGBA{B: 255, A: 255}),
	}
	traced, err := TraceLayers(layers, bitrace.DefaultConfig(), 4)
	if err != nil {
		t.Fatalf("TraceLayers: %v", err)
	}
	if len(traced) != len(layers) {
		t.Fatalf("traced count = %d, want %d", len(traced), len(layers))
	}
	for i := range layers {
		if traced[i].Color != layers[i].Color {
			t.Errorf("index %d: colour %v, want %v (order not preserved)", i, traced[i].Color, layers[i].Color)
		}
		if len(traced[i].Paths) == 0 {
			t.Errorf("index %d: no paths traced for a filled square", i)
		}
	}
}

func TestTraceLayersEmpty(t *testing.T) {
	traced, err := TraceLayers(nil, bitrace.DefaultConfig(), 4)
	if err != nil {
		t.Fatalf("TraceLayers(nil): %v", err)
	}
	if traced != nil {
		t.Errorf("traced = %v, want nil", traced)
	}
}

func TestTraceLayersSingleWorker(t *testing.T) {
	layers := []quantize.Layer{squareLayer(16, 16, color.RGBA{R: 10, G: 20, B: 30, A: 255})}
	traced, err := TraceLayers(layers, bitrace.DefaultConfig(), 0) // 0 -> clamped to 1
	if err != nil {
		t.Fatalf("TraceLayers: %v", err)
	}
	if len(traced) != 1 || len(traced[0].Paths) == 0 {
		t.Fatalf("unexpected result: %+v", traced)
	}
}

// TestTraceLayersConcurrent exercises many layers under -race to validate the
// per-index write pattern is data-race-free.
func TestTraceLayersConcurrent(t *testing.T) {
	const n = 64
	layers := make([]quantize.Layer, n)
	for i := range layers {
		layers[i] = squareLayer(24, 24, color.RGBA{R: uint8(i), G: 255, B: 0, A: 255})
	}
	traced, err := TraceLayers(layers, bitrace.DefaultConfig(), 8)
	if err != nil {
		t.Fatalf("TraceLayers: %v", err)
	}
	for i := range traced {
		if traced[i].Color.R != uint8(i) {
			t.Fatalf("index %d: colour.R = %d, want %d", i, traced[i].Color.R, i)
		}
	}
}
