// Package normalize implements Stage I of the vectrigo pipeline: decode an
// encoded raster (PNG/JPEG/WEBP) from a stream, normalize it to
// non-premultiplied RGBA, and downsample it so neither axis exceeds a caller
// supplied ceiling.
package normalize

import (
	"errors"
	"fmt"
	"image"
	"io"
	"math"

	// Standard-library decoders. imaging also registers these, but importing
	// them here documents the requirement and is harmless (idempotent).
	_ "image/jpeg"
	_ "image/png"

	// REQUIRED: registers the WEBP decoder with image.Decode's format registry
	// so content-sniffed WEBP streams decode. Without this blank import, only
	// .webp inputs would fail.
	_ "golang.org/x/image/webp"

	"github.com/disintegration/imaging"
)

// Image is a normalized, size-bounded, non-premultiplied RGBA raster produced
// by [Decode].
//
// NRGBA is imaging's native format; its Pix field is a flat, row-major
// []uint8 buffer of length 4*W*H (channels R,G,B,A per pixel), which the
// quantize stage consumes directly. OrigW and OrigH record the decoded
// dimensions before any downsampling, so later stages can map the SVG viewBox
// back to the source's apparent size.
type Image struct {
	// NRGBA is the normalized pixel buffer at working (post-downsample) dims.
	NRGBA *image.NRGBA
	// OrigW is the decoded width before downsampling.
	OrigW int
	// OrigH is the decoded height before downsampling.
	OrigH int
}

// ErrEmptyInput is returned by [Decode] when r yields no bytes.
var ErrEmptyInput = errors.New("empty input")

// Decode reads an encoded image from r, normalizes it to non-premultiplied
// RGBA, and downsamples it so neither axis exceeds (maxW, maxH), preserving
// aspect ratio. The image is never upscaled. Format is detected by content
// (magic bytes) via image.Decode's registry, not by any filename.
//
// It returns [ErrEmptyInput] (wrapped) when the stream is empty, and a wrapped
// decoder error for corrupt or unsupported data.
func Decode(r io.Reader, maxW, maxH int) (Image, error) {
	if maxW <= 0 {
		maxW = 2048
	}
	if maxH <= 0 {
		maxH = 2048
	}

	// Peek one byte so a genuinely empty stream produces a clear error rather
	// than image.Decode's generic "unknown format".
	br := newPeekReader(r)
	if empty, err := br.isEmpty(); err != nil {
		return Image{}, fmt.Errorf("read: %w", err)
	} else if empty {
		return Image{}, ErrEmptyInput
	}

	src, _, err := image.Decode(br)
	if err != nil {
		return Image{}, fmt.Errorf("decode: %w", err)
	}

	// Clone to a concrete *image.NRGBA regardless of the source's concrete
	// type. NRGBA (non-premultiplied) is the correct space for colour
	// clustering; premultiplied channels would bias centroids toward black on
	// semi-transparent pixels.
	nr := imaging.Clone(src)

	b := nr.Bounds()
	origW, origH := b.Dx(), b.Dy()

	if origW > maxW || origH > maxH {
		s := math.Min(float64(maxW)/float64(origW), float64(maxH)/float64(origH))
		nw := int(math.Round(float64(origW) * s))
		nh := int(math.Round(float64(origH) * s))
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		nr = imaging.Resize(nr, nw, nh, imaging.Lanczos)
	}

	return Image{NRGBA: nr, OrigW: origW, OrigH: origH}, nil
}

// peekReader wraps an io.Reader so the first byte can be inspected for
// emptiness without consuming it from the decode stream.
type peekReader struct {
	r       io.Reader
	buf     [1]byte
	hasByte bool
	eof     bool
}

func newPeekReader(r io.Reader) *peekReader { return &peekReader{r: r} }

// isEmpty reports whether the underlying reader is empty (immediate EOF). A
// successfully peeked byte is retained and re-served by Read.
func (p *peekReader) isEmpty() (bool, error) {
	n, err := io.ReadFull(p.r, p.buf[:])
	if n == 1 {
		p.hasByte = true
		return false, nil
	}
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		p.eof = true
		return true, nil
	}
	return false, err
}

func (p *peekReader) Read(b []byte) (int, error) {
	if p.hasByte {
		if len(b) == 0 {
			return 0, nil
		}
		b[0] = p.buf[0]
		p.hasByte = false
		return 1, nil
	}
	if p.eof {
		return 0, io.EOF
	}
	return p.r.Read(b)
}
