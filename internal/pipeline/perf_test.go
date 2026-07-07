package pipeline

import (
	"bytes"
	"os"
	"testing"

	"github.com/aleybovich/bitrace"
	"github.com/aleybovich/vectrigo/internal/normalize"
	"github.com/aleybovich/vectrigo/internal/quantize"
)

// BenchmarkTraceLayers benchmarks the concurrent per-layer tracing stage over
// the quantized layers of the downsampled photo fixture.
func BenchmarkTraceLayers(b *testing.B) {
	data, err := os.ReadFile("../../testdata/street_market.png")
	if err != nil {
		b.Fatal(err)
	}
	img, err := normalize.Decode(bytes.NewReader(data), 256, 256)
	if err != nil {
		b.Fatal(err)
	}
	layers, err := quantize.Quantize(img, 16)
	if err != nil {
		b.Fatal(err)
	}
	cfg := bitrace.Config{TurdSize: 2, AlphaMax: 1.0, Optimize: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		traced, err := TraceLayers(layers, cfg, 0)
		if err != nil {
			b.Fatal(err)
		}
		if len(traced) == 0 {
			b.Fatal("no traced layers")
		}
	}
}
