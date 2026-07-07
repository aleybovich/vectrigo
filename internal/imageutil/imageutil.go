// Package imageutil holds small, dependency-light helpers shared by the
// vectrigo pipeline stages: rounding/clamping colour channels, packing an
// RGB triple into a comparable integer, and formatting a colour as an SVG
// hex string. Centralising these avoids duplicated index/rounding math across
// the quantize, pipeline, and assemble stages.
package imageutil

import (
	"image/color"
	"math"
)

// Round8 clamps f to [0,255] and rounds it (half away from zero) to a uint8.
// NaN maps to 0. It is used to turn floating-point k-means centroids into
// concrete 8-bit colour channels.
func Round8(f float64) uint8 {
	if math.IsNaN(f) {
		return 0
	}
	f = math.Round(f)
	if f <= 0 {
		return 0
	}
	if f >= 255 {
		return 255
	}
	return uint8(f)
}

// Pack encodes the R, G, B channels of c into a single 0xRRGGBB integer. The
// alpha channel is ignored. The result is a stable, comparable key used for
// deterministic tie-breaking when ordering layers.
func Pack(c color.RGBA) int {
	return int(c.R)<<16 | int(c.G)<<8 | int(c.B)
}

// Hex formats c as a lowercase "#rrggbb" string (alpha ignored), suitable for
// an SVG fill attribute.
func Hex(c color.RGBA) string {
	const digits = "0123456789abcdef"
	buf := [7]byte{'#'}
	buf[1] = digits[c.R>>4]
	buf[2] = digits[c.R&0x0f]
	buf[3] = digits[c.G>>4]
	buf[4] = digits[c.G&0x0f]
	buf[5] = digits[c.B>>4]
	buf[6] = digits[c.B&0x0f]
	return string(buf[:])
}
