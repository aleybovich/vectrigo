// Package vectrigo converts raster images (PNG, JPEG, WEBP) into clean,
// scalable SVG vector paths.
//
// It is a pure-Go, stateless, streaming engine: it reads an encoded image from
// an io.Reader and writes an SVG document to an io.Writer, with no disk state,
// no global mutable state, and no external process dependency. The same code
// runs identically on a laptop, a server, or inside a scratch container on any
// platform, and a single [Engine] is safe to share across goroutines.
//
// # Pipeline
//
// Conversion runs in four stages:
//
//  1. Normalize — decode the stream (format detected by content), convert to
//     non-premultiplied RGBA, and downsample so neither axis exceeds
//     Config.MaxDimensions.
//  2. Quantize — cluster the opaque pixels into at most K colours with a
//     deterministic, seeded k-means and produce one binary mask per colour.
//  3. Trace — vectorize each mask concurrently with the in-house bitrace
//     tracer, yielding cubic-Bézier contours.
//  4. Assemble — order the layers containment-aware (largest area behind, but
//     any layer spatially enclosed by another is painted on top so it is never
//     occluded), merge each colour's contours into a single winding-aware path,
//     and serialize with the in-house minisvg writer.
//
// # Usage
//
// Build a [Config] from [DefaultConfig] and adjust the primary knob,
// [Config.Sensitivity] (0–100):
//
//	cfg := vectrigo.DefaultConfig()
//	cfg.Sensitivity = 70 // more detail
//	if err := vectrigo.Vectorize(in, out, cfg); err != nil {
//		log.Fatal(err)
//	}
//
// A bare Config{} means Sensitivity 0 (maximum posterization), not the
// defaults — always start from [DefaultConfig].
package vectrigo
