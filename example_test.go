package vectrigo_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"github.com/aleybovich/vectrigo"
)

// makePNG returns a tiny two-colour PNG for the examples.
func makePNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if x < 4 {
				img.SetNRGBA(x, y, color.NRGBA{R: 220, G: 20, B: 20, A: 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{R: 20, G: 20, B: 220, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// Example shows the simplest use: read a raster from an io.Reader and write SVG
// to an io.Writer with the recommended defaults.
func Example() {
	in := bytes.NewReader(makePNG())
	var out bytes.Buffer

	cfg := vectrigo.DefaultConfig()
	cfg.Sensitivity = 70 // the primary knob: more detail (0–100)

	if err := vectrigo.Vectorize(in, &out, cfg); err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(strings.HasPrefix(out.String(), "<svg"))
	// Output: true
}

// ExampleEngine demonstrates reusing a single, concurrency-safe Engine across
// multiple conversions.
func ExampleEngine() {
	eng := vectrigo.NewEngine(vectrigo.DefaultConfig())

	var out bytes.Buffer
	if err := eng.Convert(bytes.NewReader(makePNG()), &out); err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Println(strings.Contains(out.String(), `xmlns="http://www.w3.org/2000/svg"`))
	// Output: true
}
