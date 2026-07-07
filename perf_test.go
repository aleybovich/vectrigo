package vectrigo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// perfMatrixCase is one row of the end-to-end byte-identity matrix.
type perfMatrixCase struct {
	name    string
	fixture string
	maxDim  int // 0 => use DefaultConfig's 2048
	cfg     func() Config
	want    string // sha256 of Vectorize output, captured at HEAD before optimization
}

// perfMatrix is the byte-identity matrix guarding the performance pass. Each
// expected digest was captured by running Vectorize at git HEAD (commit
// 58f1795, before any optimization) via TestCaptureGoldenDigests, which logs
// the sha256 of the output for every case below. To re-capture after an
// intentional behaviour change, run:
//
//	go test -run TestCaptureGoldenDigests -v .
//
// and paste the logged digests back into the `want` fields.
func perfMatrix() []perfMatrixCase {
	base := func(sens int) func() Config {
		return func() Config {
			c := DefaultConfig()
			c.Sensitivity = sens
			return c
		}
	}
	return []perfMatrixCase{
		{name: "shapes_s0", fixture: "shapes.png", cfg: base(0), want: "40c121bacc037d96272762638a3b75f16cf797b36d55e68d5c221baad3b46ae3"},
		{name: "shapes_s50", fixture: "shapes.png", cfg: base(50), want: "39aac7a8e0c795fff4f61049757652559beb7011f8e883a9438613403c49436f"},
		{name: "shapes_s100", fixture: "shapes.png", cfg: base(100), want: "47b529a3a4724368fb097c86251b17f17bb83de326f48e6b4f9b24809ba1502d"},
		{name: "street_s50_d256", fixture: "street_market.png", maxDim: 256, cfg: base(50), want: "53cde2b54584cd8f0dc4941d4250194e2fa3f32d1a3cc8008349d0d24651776c"},
		{name: "shapes_autok", fixture: "shapes.png", cfg: func() Config {
			c := DefaultConfig()
			c.AutoK = true
			return c
		}, want: "bff6e629c360d2f509282406cf72f8df8a1408483b2d68bcc825572dc8d85613"},
		{name: "street_autok_d256", fixture: "street_market.png", maxDim: 256, cfg: func() Config {
			c := DefaultConfig()
			c.AutoK = true
			return c
		}, want: "427364ea3163670dfe936e00bbc452ce9f835b2ca066f059c5faa8f7d1e2c7d0"},
	}
}

func runMatrixCase(tb testing.TB, c perfMatrixCase) []byte {
	tb.Helper()
	src, err := os.ReadFile("testdata/" + c.fixture)
	if err != nil {
		tb.Fatalf("read fixture %s: %v", c.fixture, err)
	}
	cfg := c.cfg()
	if c.maxDim > 0 {
		cfg.MaxDimensions = Dimensions{Width: c.maxDim, Height: c.maxDim}
	}
	var out bytes.Buffer
	if err := Vectorize(bytes.NewReader(src), &out, cfg); err != nil {
		tb.Fatalf("Vectorize %s: %v", c.name, err)
	}
	return out.Bytes()
}

// TestCaptureGoldenDigests prints the sha256 of every matrix case. It is the
// tool used to (re)baseline the expected digests in perfMatrix. It never fails;
// run it with -v and copy the logged values into the `want` fields.
func TestCaptureGoldenDigests(t *testing.T) {
	for _, c := range perfMatrix() {
		out := runMatrixCase(t, c)
		sum := sha256.Sum256(out)
		t.Logf("%-20s %s (%d bytes)", c.name, hex.EncodeToString(sum[:]), len(out))
	}
}

// TestGoldenByteIdentityMatrix asserts the optimized code reproduces the
// HEAD-captured digests byte-for-byte across the matrix.
func TestGoldenByteIdentityMatrix(t *testing.T) {
	for _, c := range perfMatrix() {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if c.want == "PLACEHOLDER" {
				t.Skip("digest not yet baselined")
			}
			out := runMatrixCase(t, c)
			sum := sha256.Sum256(out)
			if got := hex.EncodeToString(sum[:]); got != c.want {
				t.Fatalf("%s output changed:\n got  %s (%d bytes)\n want %s",
					c.name, got, len(out), c.want)
			}
		})
	}
}

// BenchmarkVectorizeShapes benchmarks the full engine on the small shapes
// fixture at the default sensitivity.
func BenchmarkVectorizeShapes(b *testing.B) {
	src, err := os.ReadFile("testdata/shapes.png")
	if err != nil {
		b.Fatal(err)
	}
	cfg := DefaultConfig()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Vectorize(bytes.NewReader(src), &out, cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVectorizeStreetMarket benchmarks the full engine on the photo
// fixture downsampled to 256px at the default sensitivity.
func BenchmarkVectorizeStreetMarket(b *testing.B) {
	src, err := os.ReadFile("testdata/street_market.png")
	if err != nil {
		b.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.MaxDimensions = Dimensions{Width: 256, Height: 256}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Vectorize(bytes.NewReader(src), &out, cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVectorizeStreetMarketAutoK benchmarks the AutoK path (auto-K
// distortion scan) on the downsampled photo fixture.
func BenchmarkVectorizeStreetMarketAutoK(b *testing.B) {
	src, err := os.ReadFile("testdata/street_market.png")
	if err != nil {
		b.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.AutoK = true
	cfg.MaxDimensions = Dimensions{Width: 256, Height: 256}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out bytes.Buffer
		if err := Vectorize(bytes.NewReader(src), &out, cfg); err != nil {
			b.Fatal(err)
		}
	}
}
