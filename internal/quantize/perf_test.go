package quantize

import (
	"bytes"
	"os"
	"testing"

	"github.com/aleybovich/vectrigo/internal/normalize"
)

// loadFixture decodes a repo-root testdata image downsampled to maxDim on each
// axis, for deterministic, fixture-based benchmarks.
func loadFixture(tb testing.TB, name string, maxDim int) normalize.Image {
	tb.Helper()
	data, err := os.ReadFile("../../testdata/" + name)
	if err != nil {
		tb.Fatalf("read %s: %v", name, err)
	}
	img, err := normalize.Decode(bytes.NewReader(data), maxDim, maxDim)
	if err != nil {
		tb.Fatalf("decode %s: %v", name, err)
	}
	return img
}

// BenchmarkQuantize benchmarks the fixed-K quantization path (k-means fit,
// final assignment, and layer assembly) on the downsampled photo fixture.
func BenchmarkQuantize(b *testing.B) {
	img := loadFixture(b, "street_market.png", 256)
	const k = 16
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		layers, err := Quantize(img, k)
		if err != nil {
			b.Fatal(err)
		}
		if len(layers) == 0 {
			b.Fatal("no layers")
		}
	}
}

// BenchmarkAssembleLayers isolates the layer-assembly step (area count,
// per-layer mask allocation, and mask fill) from clustering.
func BenchmarkAssembleLayers(b *testing.B) {
	img := loadFixture(b, "street_market.png", 256)
	bnd := img.NRGBA.Bounds()
	w, h := bnd.Dx(), bnd.Dy()
	const k = 16
	centroids, labels, err := defaultClusterer.fit(img.NRGBA.Pix, w, h, k)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		layers := assembleLayers(centroids, labels, w, h)
		if len(layers) == 0 {
			b.Fatal("no layers")
		}
	}
}

// BenchmarkSelectK benchmarks the auto-K distortion scan on the downsampled
// photo fixture.
func BenchmarkSelectK(b *testing.B) {
	img := loadFixture(b, "street_market.png", 256)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if k := SelectK(img, 256, 0.02); k < 2 {
			b.Fatalf("SelectK = %d", k)
		}
	}
}
