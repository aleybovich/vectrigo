package normalize

import (
	"bytes"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdataDir resolves the repo-root testdata directory from this package.
func testdataPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", name)
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(testdataPath(t, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// TestDecodeAllDecoders proves each of the three decoders is wired, including
// the WEBP blank-import (a missing import fails only on the .webp case).
func TestDecodeAllDecoders(t *testing.T) {
	for _, name := range []string{"squirrel.png", "squirrel.jpg", "squirrel.webp"} {
		t.Run(name, func(t *testing.T) {
			img, err := Decode(bytes.NewReader(readFile(t, name)), 2048, 2048)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if _, ok := any(img.NRGBA).(*image.NRGBA); !ok {
				t.Fatalf("expected *image.NRGBA, got %T", img.NRGBA)
			}
			b := img.NRGBA.Bounds()
			if b.Dx() != 1408 || b.Dy() != 768 {
				t.Errorf("dims = %dx%d, want 1408x768", b.Dx(), b.Dy())
			}
			if img.OrigW != 1408 || img.OrigH != 768 {
				t.Errorf("orig = %dx%d, want 1408x768", img.OrigW, img.OrigH)
			}
			if len(img.NRGBA.Pix) != 4*1408*768 {
				t.Errorf("Pix len = %d, want %d", len(img.NRGBA.Pix), 4*1408*768)
			}
		})
	}
}

func TestDecodeDownsample(t *testing.T) {
	img, err := Decode(bytes.NewReader(readFile(t, "squirrel.png")), 32, 32)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	b := img.NRGBA.Bounds()
	if b.Dx() > 32 || b.Dy() > 32 {
		t.Errorf("downsampled dims = %dx%d, want both <= 32", b.Dx(), b.Dy())
	}
	// 1408x768 fit into 32x32 preserving aspect: scale = 32/1408, so 32x17.
	if b.Dx() != 32 || b.Dy() != 17 {
		t.Errorf("downsampled dims = %dx%d, want 32x17", b.Dx(), b.Dy())
	}
	// Original dims recorded pre-resize.
	if img.OrigW != 1408 || img.OrigH != 768 {
		t.Errorf("orig = %dx%d, want 1408x768", img.OrigW, img.OrigH)
	}
}

func TestDecodeNoUpscale(t *testing.T) {
	img, err := Decode(bytes.NewReader(readFile(t, "squirrel.png")), 2000, 2000)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	b := img.NRGBA.Bounds()
	if b.Dx() != 1408 || b.Dy() != 768 {
		t.Errorf("dims = %dx%d, want unchanged 1408x768 (no upscale)", b.Dx(), b.Dy())
	}
}

func TestDecodeEmptyInput(t *testing.T) {
	_, err := Decode(bytes.NewReader(nil), 2048, 2048)
	if !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("err = %v, want ErrEmptyInput", err)
	}
}

func TestDecodeGarbage(t *testing.T) {
	_, err := Decode(strings.NewReader("not an image, just text bytes here"), 2048, 2048)
	if err == nil {
		t.Fatal("expected decode error for garbage input")
	}
	if errors.Is(err, ErrEmptyInput) {
		t.Fatalf("garbage should not be reported as empty: %v", err)
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("err = %v, want wrapped decode error", err)
	}
}
