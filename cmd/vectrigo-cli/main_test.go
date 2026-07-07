package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixturePath is the committed sample image used for the end-to-end test.
const fixturePath = "../../testdata/shapes.png"

func TestOutputPath(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		sensitivity int
		want        string
	}{
		{
			name:        "png with subdirectory",
			input:       "photos/street.png",
			sensitivity: 70,
			want:        "photos/street.70.svg",
		},
		{
			name:        "absolute jpeg path",
			input:       "/tmp/a.jpeg",
			sensitivity: 5,
			want:        "/tmp/a.5.svg",
		},
		{
			name:        "webp no directory",
			input:       "image.webp",
			sensitivity: 0,
			want:        "image.0.svg",
		},
		{
			name:        "sensitivity 100",
			input:       "/data/images/shapes.png",
			sensitivity: 100,
			want:        "/data/images/shapes.100.svg",
		},
		{
			name:        "multiple dots in name only strips final extension",
			input:       "archive/my.photo.v2.jpg",
			sensitivity: 42,
			want:        "archive/my.photo.v2.42.svg",
		},
		{
			name:        "relative path with dot-slash",
			input:       "./nested/dir/pic.jpg",
			sensitivity: 33,
			want:        filepath.Join("nested/dir", "pic.33.svg"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := outputPath(tt.input, tt.sensitivity)
			want := filepath.FromSlash(tt.want)
			if got != want {
				t.Errorf("outputPath(%q, %d) = %q, want %q", tt.input, tt.sensitivity, got, want)
			}
		})
	}
}

func TestAutoOutputPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "png with subdirectory",
			input: "photos/street.png",
			want:  "photos/street.svg",
		},
		{
			name:  "absolute jpeg path",
			input: "/tmp/a.jpeg",
			want:  "/tmp/a.svg",
		},
		{
			name:  "webp no directory",
			input: "image.webp",
			want:  "image.svg",
		},
		{
			name:  "multiple dots in name only strips final extension",
			input: "archive/my.photo.v2.jpg",
			want:  "archive/my.photo.v2.svg",
		},
		{
			name:  "relative path with dot-slash",
			input: "./nested/dir/pic.jpg",
			want:  filepath.Join("nested/dir", "pic.svg"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := autoOutputPath(tt.input)
			want := filepath.FromSlash(tt.want)
			if got != want {
				t.Errorf("autoOutputPath(%q) = %q, want %q", tt.input, got, want)
			}
		})
	}
}

func TestPhotoOutputPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "png with subdirectory",
			input: "photos/street.png",
			want:  "photos/street.photo.svg",
		},
		{
			name:  "absolute jpeg path",
			input: "/tmp/a.jpeg",
			want:  "/tmp/a.photo.svg",
		},
		{
			name:  "webp no directory",
			input: "image.webp",
			want:  "image.photo.svg",
		},
		{
			name:  "multiple dots in name only strips final extension",
			input: "archive/my.photo.v2.jpg",
			want:  "archive/my.photo.v2.photo.svg",
		},
		{
			name:  "relative path with dot-slash",
			input: "./nested/dir/pic.jpg",
			want:  filepath.Join("nested/dir", "pic.photo.svg"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := photoOutputPath(tt.input)
			want := filepath.FromSlash(tt.want)
			if got != want {
				t.Errorf("photoOutputPath(%q) = %q, want %q", tt.input, got, want)
			}
		})
	}
}

func TestValidateSensitivity(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		wantErr bool
	}{
		{name: "zero", input: 0},
		{name: "hundred", input: 100},
		{name: "typical", input: 70},
		{name: "negative", input: -1, wantErr: true},
		{name: "over 100", input: 150, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSensitivity(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("validateSensitivity(%d) = nil; want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateSensitivity(%d) unexpected error: %v", tt.input, err)
			}
		})
	}
}

// TestRunEndToEnd exercises the full CLI on the committed shapes.png fixture,
// copied into a temp dir so the generated output never lands in testdata.
func TestRunEndToEnd(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "shapes.png")
	copyFile(t, fixturePath, inputPath)

	var stdout, stderr bytes.Buffer
	err := run([]string{"-i", inputPath, "-s", "60"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() returned error: %v (stderr: %s)", err, stderr.String())
	}

	wantOut := filepath.Join(dir, "shapes.60.svg")
	gotOut := strings.TrimSpace(stdout.String())
	if gotOut != wantOut {
		t.Fatalf("stdout = %q, want %q", gotOut, wantOut)
	}

	assertSVG(t, wantOut)
}

// TestRunAutoKEndToEnd exercises the --auto-k invocation form, verifying it
// produces an "<name>.svg" output (no sensitivity segment), with the flag
// before and after the input flag to confirm order independence.
func TestRunAutoKEndToEnd(t *testing.T) {
	for _, form := range []struct {
		name string
		args func(input string) []string
	}{
		{"auto-k before input", func(in string) []string { return []string{"--auto-k", "-i", in} }},
		{"auto-k after input", func(in string) []string { return []string{"-i", in, "--auto-k"} }},
	} {
		t.Run(form.name, func(t *testing.T) {
			dir := t.TempDir()
			inputPath := filepath.Join(dir, "shapes.png")
			copyFile(t, fixturePath, inputPath)

			var stdout, stderr bytes.Buffer
			runArgs := form.args(inputPath)
			if err := run(runArgs, &stdout, &stderr); err != nil {
				t.Fatalf("run(%v) returned error: %v (stderr: %s)", runArgs, err, stderr.String())
			}

			wantOut := filepath.Join(dir, "shapes.svg")
			gotOut := strings.TrimSpace(stdout.String())
			if gotOut != wantOut {
				t.Fatalf("stdout = %q, want %q", gotOut, wantOut)
			}

			assertSVG(t, wantOut)
		})
	}
}

// TestRunModeMatrix covers every row of the flag-interface contract, asserting
// on the returned error (nil vs non-nil, plus the message substring for the
// error rows) and, for the error rows, that no output file is created.
func TestRunModeMatrix(t *testing.T) {
	tests := []struct {
		name        string
		args        func(input string) []string
		wantErr     bool
		errContains string
		wantOut     func(dir string) string // relative to temp dir; success rows only
	}{
		{
			name:    "fixed K with short flags",
			args:    func(in string) []string { return []string{"-i", in, "-s", "70"} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.70.svg") },
		},
		{
			name:    "fixed K with long flags",
			args:    func(in string) []string { return []string{"--input", in, "--sensitivity", "70"} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.70.svg") },
		},
		{
			name:    "fixed K flag order independence",
			args:    func(in string) []string { return []string{"-s", "70", "-i", in} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.70.svg") },
		},
		{
			name:    "auto-K with short input flag",
			args:    func(in string) []string { return []string{"-i", in, "--auto-k"} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.svg") },
		},
		{
			name:    "sensitivity zero is valid",
			args:    func(in string) []string { return []string{"-i", in, "-s", "0"} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.0.svg") },
		},
		{
			name:    "photo mode alone",
			args:    func(in string) []string { return []string{"-i", in, "--photo"} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.photo.svg") },
		},
		{
			name:    "photo mode with sigma",
			args:    func(in string) []string { return []string{"-i", in, "--photo", "--sigma", "8"} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.photo.svg") },
		},
		{
			name:    "photo mode flag order independence",
			args:    func(in string) []string { return []string{"--sigma", "8", "--photo", "-i", in} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.photo.svg") },
		},
		{
			name:    "photo mode with simplify subtle",
			args:    func(in string) []string { return []string{"-i", in, "--photo", "--simplify", "subtle"} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.photo.svg") },
		},
		{
			name:    "photo mode with simplify aggressive",
			args:    func(in string) []string { return []string{"-i", in, "--photo", "--simplify", "aggressive"} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.photo.svg") },
		},
		{
			name:    "photo mode with edge stroke",
			args:    func(in string) []string { return []string{"-i", in, "--photo", "--edge", "stroke"} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.photo.svg") },
		},
		{
			name:    "photo mode with edge crisp",
			args:    func(in string) []string { return []string{"-i", in, "--photo", "--edge", "crisp"} },
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.photo.svg") },
		},
		{
			name:        "no mode selected",
			args:        func(in string) []string { return []string{"-i", in} },
			wantErr:     true,
			errContains: "a mode is required",
		},
		{
			name:        "auto-k with sensitivity is mutually exclusive",
			args:        func(in string) []string { return []string{"-i", in, "-s", "70", "--auto-k"} },
			wantErr:     true,
			errContains: "mutually exclusive",
		},
		{
			name:        "photo with sensitivity is mutually exclusive",
			args:        func(in string) []string { return []string{"-i", in, "-s", "70", "--photo"} },
			wantErr:     true,
			errContains: "mutually exclusive",
		},
		{
			name:        "photo with auto-k is mutually exclusive",
			args:        func(in string) []string { return []string{"-i", in, "--auto-k", "--photo"} },
			wantErr:     true,
			errContains: "mutually exclusive",
		},
		{
			name:        "sigma without photo is rejected",
			args:        func(in string) []string { return []string{"-i", in, "--sigma", "8"} },
			wantErr:     true,
			errContains: "--sigma requires --photo",
		},
		{
			name:        "edge without photo is rejected",
			args:        func(in string) []string { return []string{"-i", in, "--edge", "stroke"} },
			wantErr:     true,
			errContains: "--edge requires --photo",
		},
		{
			name:        "simplify without photo is rejected",
			args:        func(in string) []string { return []string{"-i", in, "--simplify", "subtle"} },
			wantErr:     true,
			errContains: "--simplify requires --photo",
		},
		{
			name:        "unknown simplify strength is rejected",
			args:        func(in string) []string { return []string{"-i", in, "--photo", "--simplify", "medium"} },
			wantErr:     true,
			errContains: "--simplify must be \"subtle\" or \"aggressive\"",
		},
		{
			name:        "numeric simplify is rejected",
			args:        func(in string) []string { return []string{"-i", in, "--photo", "--simplify", "0.35"} },
			wantErr:     true,
			errContains: "--simplify must be \"subtle\" or \"aggressive\"",
		},
		{
			name:        "edge with bogus value is rejected",
			args:        func(in string) []string { return []string{"-i", in, "--photo", "--edge", "bogus"} },
			wantErr:     true,
			errContains: "--edge must be",
		},
		{
			name:        "missing input flag",
			args:        func(in string) []string { return []string{"-s", "70"} },
			wantErr:     true,
			errContains: "input image (-i) is required",
		},
		{
			name:        "sensitivity out of range",
			args:        func(in string) []string { return []string{"-i", in, "-s", "150"} },
			wantErr:     true,
			errContains: "[0,100]",
		},
		{
			name:        "sensitivity negative out of range",
			args:        func(in string) []string { return []string{"-i", in, "-s", "-5"} },
			wantErr:     true,
			errContains: "[0,100]",
		},
		{
			name:        "non-integer sensitivity",
			args:        func(in string) []string { return []string{"-i", in, "-s", "abc"} },
			wantErr:     true,
			errContains: "invalid value",
		},
		{
			name:        "nonexistent input file",
			args:        func(in string) []string { return []string{"-i", in + ".missing", "-s", "50"} },
			wantErr:     true,
			errContains: "opening input image",
		},
		{
			name:        "stray positional argument is rejected",
			args:        func(in string) []string { return []string{"-i", in, "-s", "70", "garbage"} },
			wantErr:     true,
			errContains: "unexpected argument",
		},
		{
			name:        "unknown flag is rejected",
			args:        func(in string) []string { return []string{"-i", in, "-s", "70", "--bogus"} },
			wantErr:     true,
			errContains: "bogus",
		},
		{
			name:        "sensitivity -1 collides with sentinel but is still rejected",
			args:        func(in string) []string { return []string{"-i", in, "-s", "-1"} },
			wantErr:     true,
			errContains: "[0,100]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			inputPath := filepath.Join(dir, "shapes.png")
			copyFile(t, fixturePath, inputPath)

			var stdout, stderr bytes.Buffer
			err := run(tt.args(inputPath), &stdout, &stderr)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("run(%v) = nil error, want non-nil", tt.args(inputPath))
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				// Error rows must not create any SVG output.
				entries, rerr := os.ReadDir(dir)
				if rerr != nil {
					t.Fatalf("reading temp dir: %v", rerr)
				}
				for _, e := range entries {
					if strings.HasSuffix(e.Name(), ".svg") {
						t.Fatalf("error row created an output file: %s", e.Name())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("run(%v) returned error: %v (stderr: %s)", tt.args(inputPath), err, stderr.String())
			}
			wantOut := tt.wantOut(dir)
			gotOut := strings.TrimSpace(stdout.String())
			if gotOut != wantOut {
				t.Fatalf("stdout = %q, want %q", gotOut, wantOut)
			}
			if _, serr := os.Stat(wantOut); serr != nil {
				t.Fatalf("expected output file %q: %v", wantOut, serr)
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run([]string{flag}, &stdout, &stderr)
			if err != nil {
				t.Fatalf("run([]string{%q}) returned error: %v", flag, err)
			}
			if !strings.Contains(stdout.String(), "Usage:") {
				t.Fatalf("help output missing usage text on stdout: %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("help wrote to stderr: %q", stderr.String())
			}
		})
	}
}

func assertSVG(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("output SVG file is empty")
	}

	var root struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(data, &root); err != nil {
		t.Fatalf("output is not well-formed XML: %v", err)
	}
	if root.XMLName.Local != "svg" {
		t.Fatalf("root element = %q, want %q", root.XMLName.Local, "svg")
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("opening fixture %q: %v", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("creating %q: %v", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copying fixture: %v", err)
	}
}
