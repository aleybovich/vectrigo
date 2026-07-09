// Command webapp is an interactive browser front-end for the vectrigo engine.
//
// It serves a single-page UI that lets you upload a PNG/JPEG/WEBP image, tune
// every conversion lever (with irrelevant levers hidden per the selected
// pipeline), convert to SVG, inspect the result with zoom/pan and
// click-to-outline, read back stats (path and colour counts, dimensions,
// timing), and download the SVG.
//
// Run it from the repo root:
//
//	go run ./examples/webapp
//
// then open http://localhost:8080. Change the port with -addr.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"github.com/aleybovich/vectrigo"
)

//go:embed index.html
var indexHTML []byte

// maxUpload caps the multipart request body so a huge upload cannot exhaust
// memory. 32 MiB comfortably holds any reasonable raster.
const maxUpload = 32 << 20

func main() {
	addr := flag.String("addr", ":8080", "host:port to listen on")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/convert", handleConvert)
	// Answer the browser's automatic favicon request quietly.
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	log.Printf("vectrigo webapp listening on http://localhost%s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

// convertResponse is the JSON payload returned by /convert.
type convertResponse struct {
	SVG      string   `json:"svg"`
	Paths    int      `json:"paths"`
	Colors   int      `json:"colors"`
	Palette  []string `json:"palette"`
	Width    int      `json:"width"`
	Height   int      `json:"height"`
	ViewBox  string   `json:"viewBox"`
	Bytes    int      `json:"bytes"`
	Millis   int64    `json:"millis"`
	Pipeline string   `json:"pipeline"`
	Error    string   `json:"error,omitempty"`
}

func handleConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		writeErr(w, http.StatusBadRequest, "upload too large or malformed: "+err.Error())
		return
	}

	file, _, err := r.FormFile("image")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "no image uploaded")
		return
	}
	defer file.Close()
	var raw bytes.Buffer
	if _, err := raw.ReadFrom(file); err != nil {
		writeErr(w, http.StatusBadRequest, "reading upload: "+err.Error())
		return
	}

	cfg, pipeline, err := buildConfig(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	var out bytes.Buffer
	start := time.Now()
	if err := vectrigo.Vectorize(bytes.NewReader(raw.Bytes()), &out, cfg); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "conversion failed: "+err.Error())
		return
	}
	elapsed := time.Since(start)

	svg := out.String()
	paths, palette := scanSVG(svg)
	width, height, viewBox := scanFrame(svg)

	resp := convertResponse{
		SVG:      svg,
		Paths:    paths,
		Colors:   len(palette),
		Palette:  palette,
		Width:    width,
		Height:   height,
		ViewBox:  viewBox,
		Bytes:    out.Len(),
		Millis:   elapsed.Milliseconds(),
		Pipeline: pipeline,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// buildConfig translates the posted form fields into a vectrigo.Config,
// starting from DefaultConfig and applying only the levers relevant to the
// selected pipeline. It returns the config and the resolved pipeline label.
func buildConfig(r *http.Request) (vectrigo.Config, string, error) {
	cfg := vectrigo.DefaultConfig()

	pipeline := r.FormValue("pipeline")
	switch pipeline {
	case "sensitivity", "autok", "photo":
	default:
		return cfg, "", fmt.Errorf("unknown pipeline %q", pipeline)
	}
	gapless := formBool(r, "gapless") && pipeline != "photo"

	// General levers apply to every pipeline.
	cfg.Optimize = formBool(r, "optimize")
	if v, ok := formInt(r, "precision"); ok {
		cfg.Precision = v
	}
	if v, ok := formInt(r, "maxW"); ok && v > 0 {
		cfg.MaxDimensions.Width = v
	}
	if v, ok := formInt(r, "maxH"); ok && v > 0 {
		cfg.MaxDimensions.Height = v
	}
	if v, ok := formInt(r, "workers"); ok {
		cfg.Workers = v
	}

	switch pipeline {
	case "photo":
		cfg.Photo = true
		if v, ok := formFloat(r, "photoDetail"); ok {
			cfg.PhotoDetail = v
		}
		applyRegionTracing(r, &cfg)
		cfg.AlphaMax = formFloatOr(r, "alphaMax", cfg.AlphaMax)
	case "autok":
		cfg.AutoK = true
		cfg.Gapless = gapless
		if v, ok := formFloat(r, "autoKTau"); ok {
			cfg.AutoKTau = v
		}
		applyQuantLevers(r, &cfg, gapless)
	case "sensitivity":
		cfg.Gapless = gapless
		if v, ok := formInt(r, "sensitivity"); ok {
			cfg.Sensitivity = v
		}
		applyQuantLevers(r, &cfg, gapless)
	}

	if gapless {
		pipeline += "+gapless"
	}
	return cfg, pipeline, nil
}

// applyQuantLevers sets the levers shared by both quantization pipelines
// (sensitivity and auto-k). AlphaMax has no effect under gapless (its
// boundaries are smoothed polylines), so it is only applied to the mask path.
func applyQuantLevers(r *http.Request, cfg *vectrigo.Config, gapless bool) {
	if v, ok := formInt(r, "k"); ok && v > 0 {
		cfg.K = v
	}
	if v, ok := formInt(r, "turdSize"); ok {
		cfg.TurdSize = v
	}
	if gapless {
		applyRegionTracing(r, cfg)
	} else {
		cfg.AlphaMax = formFloatOr(r, "alphaMax", cfg.AlphaMax)
	}
}

// applyRegionTracing sets the levers shared by the region-traced finishes
// (photo mode and gapless): boundary simplification and the edge finish.
func applyRegionTracing(r *http.Request, cfg *vectrigo.Config) {
	if v, ok := formFloat(r, "photoSimplify"); ok {
		cfg.PhotoSimplify = v
	}
	if r.FormValue("photoEdge") == "stroke" {
		cfg.PhotoEdge = vectrigo.PhotoEdgeStroke
	}
}

var (
	reFill    = regexp.MustCompile(`fill="(#[0-9a-fA-F]{3,8})"`)
	rePath    = regexp.MustCompile(`<path\b`)
	reWidth   = regexp.MustCompile(`\bwidth="([^"]*)"`)
	reHeight  = regexp.MustCompile(`\bheight="([^"]*)"`)
	reViewBox = regexp.MustCompile(`\bviewBox="([^"]*)"`)
)

// scanSVG returns the number of drawn paths and the sorted set of distinct
// fill colours used.
func scanSVG(svg string) (paths int, palette []string) {
	paths = len(rePath.FindAllStringIndex(svg, -1))
	seen := map[string]bool{}
	for _, m := range reFill.FindAllStringSubmatch(svg, -1) {
		c := strings.ToLower(m[1])
		if !seen[c] {
			seen[c] = true
			palette = append(palette, c)
		}
	}
	// Deterministic order for a stable swatch strip.
	sortStrings(palette)
	return paths, palette
}

// scanFrame extracts the document width/height and viewBox from the <svg> tag.
func scanFrame(svg string) (width, height int, viewBox string) {
	head := svg
	if i := strings.IndexByte(svg, '>'); i > 0 {
		head = svg[:i+1]
	}
	if m := reWidth.FindStringSubmatch(head); m != nil {
		width = atoiLoose(m[1])
	}
	if m := reHeight.FindStringSubmatch(head); m != nil {
		height = atoiLoose(m[1])
	}
	if m := reViewBox.FindStringSubmatch(head); m != nil {
		viewBox = m[1]
	}
	return width, height, viewBox
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(convertResponse{Error: msg})
}

// --- small form helpers -----------------------------------------------------

func formBool(r *http.Request, key string) bool {
	switch strings.ToLower(r.FormValue(key)) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

func formInt(r *http.Request, key string) (int, bool) {
	s := r.FormValue(key)
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	return v, true
}

func formFloat(r *http.Request, key string) (float64, bool) {
	s := r.FormValue(key)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) {
		return 0, false
	}
	return v, true
}

func formFloatOr(r *http.Request, key string, def float64) float64 {
	if v, ok := formFloat(r, key); ok {
		return v
	}
	return def
}

func atoiLoose(s string) int {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9') {
		end++
	}
	if end == 0 {
		return 0
	}
	v, _ := strconv.Atoi(s[:end])
	return v
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
