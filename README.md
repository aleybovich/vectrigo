# Vectrigo

Vectrigo is a portable, high-performance, **pure-Go** engine that converts raster
images (**PNG / JPEG / WEBP**) into clean, scalable **SVG vector paths**.

It reads from an `io.Reader` and writes to an `io.Writer`, holds no global state,
and is safe to invoke concurrently from many goroutines.

## Non-negotiable constraints

These are **hard requirements**, not trade-offs. Any change that violates one is a
defect.

### Pure Go only — no cgo

- The build must succeed with `CGO_ENABLED=0`.
- No `os/exec`, no shell-outs, no spawning external binaries or processes.
- No external binary or system-library dependency of any kind (no ImageMagick,
  no libpng, no Potrace binary, etc.).
- Must **cross-compile cleanly** for any `GOOS`/`GOARCH` with a single `go build`
  (e.g. `linux/amd64`, `linux/arm64`, `darwin/arm64`, `windows/amd64`, `js/wasm`).

### Permissive licenses only

Every dependency — direct or transitive — carries a **permissive** license. The
allow-list is: MIT, BSD-2-Clause / BSD-3-Clause, Apache-2.0, ISC, zlib, and
Unlicense / public-domain code dedications. Copyleft licenses (GPL / LGPL / AGPL,
any version) and non-software licenses (e.g. Creative Commons such as CC-BY-4.0)
are **forbidden**. Vectrigo targets commercial / closed-source use, so the whole
dependency graph stays permissive and free of legal friction.

### Stateless engine, streaming I/O

- Input is an `io.Reader`; output is an `io.Writer`. No temp files, no on-disk
  caches.
- No global mutable state. All state lives on the stack or in per-call structs, so
  a single `Engine` is safe to share and use concurrently across goroutines.

## Install

```sh
go get github.com/aleybovich/vectrigo
```

Build with cgo disabled to enforce the pure-Go constraint:

```sh
CGO_ENABLED=0 go build ./...
```

## Usage

The simplest path — read a raster, write SVG, with recommended defaults:

```go
package main

import (
	"os"

	"github.com/aleybovich/vectrigo"
)

func main() {
	in, err := os.Open("input.png")
	if err != nil {
		panic(err)
	}
	defer in.Close()

	out, err := os.Create("output.svg")
	if err != nil {
		panic(err)
	}
	defer out.Close()

	cfg := vectrigo.DefaultConfig()
	cfg.Sensitivity = 70 // primary detail knob, 0–100

	if err := vectrigo.Vectorize(in, out, cfg); err != nil {
		panic(err)
	}
}
```

Reuse a single, concurrency-safe `Engine` across many conversions:

```go
eng := vectrigo.NewEngine(vectrigo.DefaultConfig())

// Safe to call from multiple goroutines simultaneously.
err := eng.Convert(reader, writer)
```

Start from `DefaultConfig()` and adjust from there. A bare `Config{}` means
`Sensitivity` 0 (maximum posterization), **not** the recommended defaults —
`Sensitivity`'s zero is a legitimate setting, so it cannot double as "unset".
`Sensitivity` (0–100) is the primary knob; `K`, `TurdSize`, `AlphaMax`,
`Optimize`, `MaxDimensions`, `Workers`, and `Precision` are advanced overrides.

## License

Vectrigo is released under the [MIT License](LICENSE) — a permissive license
compatible with its permissive dependency graph. It bundles two in-house,
independently-licensed libraries, `bitrace` and `minisvg` (both MIT), which carry
their own `LICENSE` files.
