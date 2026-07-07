package imageutil

import (
	"image/color"
	"math"
	"testing"
)

func TestRound8(t *testing.T) {
	cases := []struct {
		in   float64
		want uint8
	}{
		{-5, 0},
		{0, 0},
		{0.4, 0},
		{0.5, 1},
		{127.5, 128},
		{254.6, 255},
		{255, 255},
		{300, 255},
		{math.NaN(), 0},
	}
	for _, c := range cases {
		if got := Round8(c.in); got != c.want {
			t.Errorf("Round8(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestPack(t *testing.T) {
	if got := Pack(color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}); got != 0x123456 {
		t.Errorf("Pack = %#x, want 0x123456", got)
	}
	// Alpha ignored.
	if Pack(color.RGBA{R: 1, G: 2, B: 3, A: 0}) != Pack(color.RGBA{R: 1, G: 2, B: 3, A: 255}) {
		t.Error("Pack should ignore alpha")
	}
}

func TestHex(t *testing.T) {
	cases := []struct {
		c    color.RGBA
		want string
	}{
		{color.RGBA{R: 0, G: 0, B: 0, A: 255}, "#000000"},
		{color.RGBA{R: 255, G: 255, B: 255, A: 255}, "#ffffff"},
		{color.RGBA{R: 0x1a, G: 0x2b, B: 0x3c, A: 255}, "#1a2b3c"},
	}
	for _, c := range cases {
		if got := Hex(c.c); got != c.want {
			t.Errorf("Hex(%v) = %s, want %s", c.c, got, c.want)
		}
	}
}
