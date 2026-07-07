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

func TestParseSensitivity(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{name: "zero", input: "0", want: 0},
		{name: "hundred", input: "100", want: 100},
		{name: "typical", input: "70", want: 70},
		{name: "negative", input: "-1", wantErr: true},
		{name: "over 100", input: "150", wantErr: true},
		{name: "non-integer", input: "abc", wantErr: true},
		{name: "float", input: "50.5", wantErr: true},
		{name: "empty", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSensitivity(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSensitivity(%q) = %d, nil; want error", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSensitivity(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseSensitivity(%q) = %d, want %d", tt.input, got, tt.want)
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
	err := run([]string{inputPath, "60"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() returned error: %v (stderr: %s)", err, stderr.String())
	}

	wantOut := filepath.Join(dir, "shapes.60.svg")
	gotOut := strings.TrimSpace(stdout.String())
	if gotOut != wantOut {
		t.Fatalf("stdout = %q, want %q", gotOut, wantOut)
	}

	data, err := os.ReadFile(wantOut)
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

// TestRunAutoKEndToEnd exercises the --auto-k invocation form, verifying it
// produces an "<name>.svg" output (no sensitivity segment).
func TestRunAutoKEndToEnd(t *testing.T) {
	for _, args := range [][]string{
		{"--auto-k", "PLACEHOLDER"}, // flag before path
		{"PLACEHOLDER", "--auto-k"}, // flag after path
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			dir := t.TempDir()
			inputPath := filepath.Join(dir, "shapes.png")
			copyFile(t, fixturePath, inputPath)

			// Substitute the placeholder with the real input path.
			runArgs := make([]string, len(args))
			for i, a := range args {
				if a == "PLACEHOLDER" {
					runArgs[i] = inputPath
				} else {
					runArgs[i] = a
				}
			}

			var stdout, stderr bytes.Buffer
			if err := run(runArgs, &stdout, &stderr); err != nil {
				t.Fatalf("run(%v) returned error: %v (stderr: %s)", runArgs, err, stderr.String())
			}

			wantOut := filepath.Join(dir, "shapes.svg")
			gotOut := strings.TrimSpace(stdout.String())
			if gotOut != wantOut {
				t.Fatalf("stdout = %q, want %q", gotOut, wantOut)
			}

			data, err := os.ReadFile(wantOut)
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
		})
	}
}

// TestRunModeMatrix covers each of the four rows of the --auto-k contract,
// asserting on the returned error (nil vs non-nil, plus the message substring
// for the two new error rows) and, for the error rows, that no output file is
// created.
func TestRunModeMatrix(t *testing.T) {
	tests := []struct {
		name        string
		args        func(input string) []string
		wantErr     bool
		errContains string
		wantOut     func(dir string) string // relative to temp dir; success rows only
	}{
		{
			name:    "fixed K with sensitivity",
			args:    func(in string) []string { return []string{in, "70"} },
			wantErr: false,
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.70.svg") },
		},
		{
			name:    "auto-K without sensitivity",
			args:    func(in string) []string { return []string{"--auto-k", in} },
			wantErr: false,
			wantOut: func(dir string) string { return filepath.Join(dir, "shapes.svg") },
		},
		{
			name:        "no sensitivity and no auto-k",
			args:        func(in string) []string { return []string{in} },
			wantErr:     true,
			errContains: "sensitivity is required",
		},
		{
			name:        "auto-k with sensitivity is mutually exclusive",
			args:        func(in string) []string { return []string{"--auto-k", in, "70"} },
			wantErr:     true,
			errContains: "mutually exclusive",
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

// TestRunAutoKNoImage verifies that --auto-k with no image path errors.
func TestRunAutoKNoImage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"--auto-k"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run([--auto-k]) = nil error, want non-nil")
	}
	if !strings.Contains(err.Error(), "requires an image path") {
		t.Fatalf("error %q does not mention missing image path", err.Error())
	}
}

func TestRunErrors(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "shapes.png")
	copyFile(t, fixturePath, inputPath)

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "wrong number of args - none",
			args: []string{},
		},
		{
			name: "wrong number of args - one",
			args: []string{inputPath},
		},
		{
			name: "wrong number of args - three",
			args: []string{inputPath, "50", "extra"},
		},
		{
			name: "non-integer sensitivity",
			args: []string{inputPath, "abc"},
		},
		{
			name: "out-of-range sensitivity",
			args: []string{inputPath, "150"},
		},
		{
			name: "negative sensitivity",
			args: []string{inputPath, "-5"},
		},
		{
			name: "nonexistent input file",
			args: []string{filepath.Join(dir, "does-not-exist.png"), "50"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tt.args, &stdout, &stderr)
			if err == nil {
				t.Fatalf("run(%v) = nil error, want non-nil", tt.args)
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
				t.Fatalf("help output missing usage text: %q", stdout.String())
			}
		})
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
