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
// expected digest was captured by running Vectorize on the current pipeline via
// TestCaptureGoldenDigests, which logs the sha256 of the output for every case
// below. To re-capture after an intentional behaviour change, run:
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
		{name: "squirrel_s0", fixture: "squirrel.png", maxDim: 256, cfg: base(0), want: "ebe671f1c89e40dc5ebd78f4c8d7b20237b1f21e4c0707aa4616e2eb507caf40"},
		{name: "squirrel_s50", fixture: "squirrel.png", maxDim: 256, cfg: base(50), want: "cb90eb0b1c01d45283e0992f95ed2b5693500fb26558c975cab2e2703b99d7a6"},
		{name: "squirrel_s100", fixture: "squirrel.png", maxDim: 256, cfg: base(100), want: "cfe401d1139041fcb21eb653b885fe614e30b37bc7b34bc6a5e68b30850beb34"},
		{name: "street_s50_d256", fixture: "street_market.png", maxDim: 256, cfg: base(50), want: "3db92e3ac20d64d621e7c9881dd8e718b2e8435792ba4134bab0f7297fd32397"},
		{name: "squirrel_autok", fixture: "squirrel.png", maxDim: 256, cfg: func() Config {
			c := DefaultConfig()
			c.AutoK = true
			return c
		}, want: "de7637dfe5c9ab289bbe4ce9d8bc106ac3da8e2806f11ac62462777db469db43"},
		{name: "street_autok_d256", fixture: "street_market.png", maxDim: 256, cfg: func() Config {
			c := DefaultConfig()
			c.AutoK = true
			return c
		}, want: "8f8dbb25676d4882e801fbf3bda0fc4b008f939d1211ee0c433e9a84787d5dc6"},
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

// BenchmarkVectorizeSquirrel benchmarks the full engine on the squirrel fixture
// downsampled to 256px at the default sensitivity.
func BenchmarkVectorizeSquirrel(b *testing.B) {
	src, err := os.ReadFile("testdata/squirrel.png")
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
