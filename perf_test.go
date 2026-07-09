package vectrigo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"runtime"
	"testing"
)

// perfMatrixCase is one row of the end-to-end byte-identity matrix.
type perfMatrixCase struct {
	name    string
	fixture string
	maxDim  int // 0 => use DefaultConfig's 2048
	cfg     func() Config
	// want holds the expected sha256 of Vectorize output keyed by GOARCH. The
	// SVG carries floating-point-derived coordinates whose last emitted decimal
	// can differ between architectures (e.g. amd64 vs arm64) because Go may
	// contract a*b+c into a fused multiply-add on some targets but not others.
	// The bytes are otherwise identical, so we baseline one digest per arch and
	// select the running one below. Capture new values with:
	//
	//	go test -run TestCaptureGoldenDigests -v .              # host arch
	//	GOARCH=amd64 go test -run TestCaptureGoldenDigests -v . # cross (Rosetta/qemu)
	want map[string]string
}

// wantDigest returns the expected digest for the running architecture, or ""
// if this case has no baseline for it yet.
func (c perfMatrixCase) wantDigest() string { return c.want[runtime.GOARCH] }

// perfMatrix is the byte-identity matrix guarding the performance pass. Each
// expected digest was captured by running Vectorize on the current pipeline via
// TestCaptureGoldenDigests, which logs the sha256 of the output for every case
// below and every architecture we baseline (see perfMatrixCase.want).
func perfMatrix() []perfMatrixCase {
	base := func(sens int) func() Config {
		return func() Config {
			c := DefaultConfig()
			c.Sensitivity = sens
			return c
		}
	}
	return []perfMatrixCase{
		{name: "squirrel_s0", fixture: "squirrel.png", maxDim: 256, cfg: base(0), want: map[string]string{
			"arm64": "ebe671f1c89e40dc5ebd78f4c8d7b20237b1f21e4c0707aa4616e2eb507caf40",
			"amd64": "d6193dd780d4260c890a8681f5d7aa99edca97cefb1fe4dc17f9c2e968abbbe0",
		}},
		{name: "squirrel_s50", fixture: "squirrel.png", maxDim: 256, cfg: base(50), want: map[string]string{
			"arm64": "cb90eb0b1c01d45283e0992f95ed2b5693500fb26558c975cab2e2703b99d7a6",
			"amd64": "bec508fdb800b2f2c3281c4cd667e17452fa1ad7c3d0a59f44308af60fbaa422",
		}},
		{name: "squirrel_s100", fixture: "squirrel.png", maxDim: 256, cfg: base(100), want: map[string]string{
			"arm64": "cfe401d1139041fcb21eb653b885fe614e30b37bc7b34bc6a5e68b30850beb34",
			"amd64": "b0cfd175cf57089f21d20f7c2741d7dc019c019cb1fc7288d2e7a421d43f899f",
		}},
		{name: "street_s50_d256", fixture: "street_market.png", maxDim: 256, cfg: base(50), want: map[string]string{
			"arm64": "3db92e3ac20d64d621e7c9881dd8e718b2e8435792ba4134bab0f7297fd32397",
			"amd64": "53cde2b54584cd8f0dc4941d4250194e2fa3f32d1a3cc8008349d0d24651776c",
		}},
		{name: "squirrel_autok", fixture: "squirrel.png", maxDim: 256, cfg: func() Config {
			c := DefaultConfig()
			c.AutoK = true
			return c
		}, want: map[string]string{
			"arm64": "de7637dfe5c9ab289bbe4ce9d8bc106ac3da8e2806f11ac62462777db469db43",
			"amd64": "de7637dfe5c9ab289bbe4ce9d8bc106ac3da8e2806f11ac62462777db469db43",
		}},
		{name: "street_autok_d256", fixture: "street_market.png", maxDim: 256, cfg: func() Config {
			c := DefaultConfig()
			c.AutoK = true
			return c
		}, want: map[string]string{
			"arm64": "8f8dbb25676d4882e801fbf3bda0fc4b008f939d1211ee0c433e9a84787d5dc6",
			"amd64": "427364ea3163670dfe936e00bbc452ce9f835b2ca066f059c5faa8f7d1e2c7d0",
		}},
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
			want := c.wantDigest()
			if want == "" || want == "PLACEHOLDER" {
				t.Skipf("digest not baselined for GOARCH=%s", runtime.GOARCH)
			}
			out := runMatrixCase(t, c)
			sum := sha256.Sum256(out)
			if got := hex.EncodeToString(sum[:]); got != want {
				t.Fatalf("%s output changed (GOARCH=%s):\n got  %s (%d bytes)\n want %s",
					c.name, runtime.GOARCH, got, len(out), want)
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
