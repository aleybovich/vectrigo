// Package bitrace is a pure-Go, permissively-licensed bitmap-to-vector tracer.
//
// It converts a binary bitmap (a mask of on/off pixels) into smooth vector
// paths built from straight lines and cubic Bézier curves, in the spirit of
// tools such as Potrace but implemented independently.
//
// # Overview
//
// The entry point is [Trace], which takes a [Bitmap] and a [Config] and returns
// an ordered slice of [Path]. Each [Path] is a closed contour expressed as a
// sequence of [Command] values (move, line, cubic, close) and carries an
// [Path.IsHole] flag so callers can nest holes correctly.
//
// The pipeline has five stages:
//
//  1. Contour extraction. The boundary between "on" and "off" regions is traced
//     as a set of closed integer polygons using a crack-following border
//     algorithm on the pixel-edge lattice. Both outer boundaries and holes are
//     produced (see the winding convention below).
//  2. Noise suppression. Contours whose enclosed area (shoelace formula) is
//     smaller than [Config.TurdSize] are discarded ("speckle" removal).
//  3. Corner detection. Each contour is scanned for high-curvature vertices.
//     Whether a vertex counts as a corner is governed by [Config.AlphaMax]:
//     lower values keep more corners (angular output), higher values keep fewer
//     (smoother output).
//  4. Curve fitting. Smooth runs between corners are fitted with cubic Bézier
//     curves using the Apache-2.0 licensed honnef.co/go/curve package (a Go port
//     of Raph Levien's kurbo). Straight runs and corner joins are emitted as
//     line segments.
//  5. Output. The fitted commands are returned as an ordered slice of [Path].
//
// # Winding convention
//
// Bitmaps use image coordinates: x increases to the right, y increases
// downward, and the pixel at (x, y) occupies the unit cell whose top-left
// corner is the lattice point (x, y). With that convention, this package emits:
//
//   - Outer contours with a negative signed area (shoelace), i.e. the filled
//     region lies to the left of the traversal direction.
//   - Hole contours with a positive signed area, wound in the opposite
//     direction. Hole paths have [Path.IsHole] set to true.
//
// The magnitude of the signed area equals the number of pixels the contour
// encloses, which is exactly what [Config.TurdSize] thresholds against.
//
// # Clean-room provenance
//
// bitrace is an independent, clean-room implementation. It is not derived from,
// copied from, translated from, or transpiled out of Potrace (which is
// GPL-2.0) or any Potrace port. It is built only from general, public-domain
// algorithm descriptions (border following, the shoelace area formula,
// curvature-based corner detection) plus the permissively-licensed
// honnef.co/go/curve package for cubic Bézier fitting. This keeps bitrace free
// of copyleft lineage so it can be embedded in proprietary software.
package bitrace
