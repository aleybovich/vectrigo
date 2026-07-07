package vectrigo

import (
	"runtime"
	"testing"
)

func TestResolveDetailCurve(t *testing.T) {
	// Large image so maxKForPixels does not clamp (2000*2000/1024 ~ 3906 -> 256).
	const W, H = 2000, 2000
	cases := []struct {
		s        int
		wantK    int
		wantTurd int
	}{
		{0, 4, 8},
		{25, 8, 4},
		{50, 16, 2},
		{75, 32, 1},
		{100, 64, 0},
	}
	for _, c := range cases {
		cfg := DefaultConfig()
		cfg.Sensitivity = c.s
		cfg = cfg.normalized()
		k, turd := cfg.resolveDetail(W, H)
		if k != c.wantK {
			t.Errorf("S=%d: K=%d, want %d", c.s, k, c.wantK)
		}
		if turd != c.wantTurd {
			t.Errorf("S=%d: TurdSize=%d, want %d", c.s, turd, c.wantTurd)
		}
	}
}

func TestSensitivityClamp(t *testing.T) {
	lo := Config{Sensitivity: -50}.normalized()
	if lo.Sensitivity != 0 {
		t.Errorf("Sensitivity clamp low = %d, want 0", lo.Sensitivity)
	}
	hi := Config{Sensitivity: 500}.normalized()
	if hi.Sensitivity != 100 {
		t.Errorf("Sensitivity clamp high = %d, want 100", hi.Sensitivity)
	}
}

func TestZeroValueConfig(t *testing.T) {
	c := Config{}.normalized()
	if c.Sensitivity != 0 {
		t.Errorf("bare Config{} Sensitivity = %d, want 0 (not the default 50)", c.Sensitivity)
	}
	if c.Workers != runtime.NumCPU() {
		t.Errorf("Workers = %d, want NumCPU %d", c.Workers, runtime.NumCPU())
	}
	if c.MaxDimensions.Width != 2048 || c.MaxDimensions.Height != 2048 {
		t.Errorf("MaxDimensions = %v, want 2048x2048", c.MaxDimensions)
	}
	if c.Precision != 0 {
		t.Errorf("Precision = %d, want 0 (bare zero clamps to 0)", c.Precision)
	}
	if c.AlphaMax != 0 {
		t.Errorf("AlphaMax = %v, want 0 (legal, max-angular)", c.AlphaMax)
	}
}

func TestDefaultConfigValues(t *testing.T) {
	c := DefaultConfig()
	if c.Sensitivity != 50 {
		t.Errorf("Sensitivity = %d, want 50", c.Sensitivity)
	}
	if c.AlphaMax != 1.0 {
		t.Errorf("AlphaMax = %v, want 1.0", c.AlphaMax)
	}
	if !c.Optimize {
		t.Error("Optimize = false, want true")
	}
	if c.Precision != 2 {
		t.Errorf("Precision = %d, want 2", c.Precision)
	}
}

func TestOverridesWin(t *testing.T) {
	// Explicit K survives (resolution large enough not to clamp).
	cfg := DefaultConfig()
	cfg.K = 7
	k, _ := cfg.normalized().resolveDetail(2000, 2000)
	if k != 7 {
		t.Errorf("explicit K = %d, want 7", k)
	}

	// TurdSize -1 => disabled (0).
	cfg = DefaultConfig()
	cfg.TurdSize = -1
	_, turd := cfg.normalized().resolveDetail(2000, 2000)
	if turd != 0 {
		t.Errorf("TurdSize=-1 => %d, want 0 (disabled)", turd)
	}

	// TurdSize 5 => used as-is.
	cfg = DefaultConfig()
	cfg.TurdSize = 5
	_, turd = cfg.normalized().resolveDetail(2000, 2000)
	if turd != 5 {
		t.Errorf("TurdSize=5 => %d, want 5", turd)
	}
}

func TestKClampBounds(t *testing.T) {
	// K below 2 is raised to 2.
	cfg := DefaultConfig()
	cfg.K = 1
	k, _ := cfg.normalized().resolveDetail(2000, 2000)
	if k != 2 {
		t.Errorf("K=1 => %d, want clamped to 2", k)
	}

	// Tiny image caps K via maxKForPixels (px/1024, min 2).
	cfg = DefaultConfig()
	cfg.K = 100
	k, _ = cfg.normalized().resolveDetail(32, 32) // 1024 px -> maxK 2... actually 1024/1024=1 -> clamp 2
	if k != 2 {
		t.Errorf("tiny image K=100 => %d, want 2", k)
	}

	// AlphaMax clamps to [0,1.334].
	cfg = DefaultConfig()
	cfg.AlphaMax = 99
	if got := cfg.normalized().AlphaMax; got != 1.334 {
		t.Errorf("AlphaMax clamp = %v, want 1.334", got)
	}
}
