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
are **forbidden**. Keeping the whole dependency graph permissive means Vectrigo
is free to set its own license terms (see [License](#license)) without inheriting
any copyleft obligations.

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
`Sensitivity` (0–100) is the primary knob; `AutoK`, `K`, `TurdSize`, `AlphaMax`,
`Optimize`, `MaxDimensions`, `Workers`, and `Precision` are advanced overrides.

### Automatic colour count (`AutoK`)

By default the colour count `K` is derived from `Sensitivity`. As an alternative,
`AutoK` lets Vectrigo pick `K` **automatically** from the image's own colour
complexity — a flat, few-colour logo collapses to a small `K`, while a rich,
gradient-heavy photo gets a larger one.

```go
cfg := vectrigo.DefaultConfig()
cfg.AutoK = true // choose K automatically from the image; ignore Sensitivity
```

- **Off by default.** `AutoK` is `false` in both `DefaultConfig()` and the zero
  `Config{}`; leaving it off preserves today's exact behaviour.
- **Supersedes `Sensitivity`.** Set a `Sensitivity` or turn on `AutoK` — the two
  are meant as an either/or. When `AutoK` is on, **`Sensitivity` has no effect on
  `K`**: it is not even a ceiling. `K` is bounded by the usual safety clamps (a
  resolution-based maximum and the number of distinct colours present) and by an
  internal auto-selection ceiling (currently 64 colours) that keeps the multi-`K`
  scan fast. The library raises no error if both are set — `AutoK` simply wins for
  `K`, and `Sensitivity` is ignored for it. The `vectrigo-cli` tool presents the
  two as a mutually exclusive choice: `--auto-k <img>` for auto-K, `<img>
  <sensitivity>` for the manual knob, and it errors if you pass both.
- **`TurdSize` follows the chosen `K`.** Under `AutoK` the speckle threshold is
  derived from the auto-selected `K` (not from `Sensitivity`), preserving the
  usual "more colours ⇒ less speckle removal" coupling. An explicit `TurdSize`
  override still applies.
- **Explicit `K` still wins.** Setting `cfg.K > 0` is a hard override that beats
  `AutoK` (and `Sensitivity`).
- **Deterministic.** Auto-K uses the same seeded k-means as the rest of the
  pipeline, so a given image always yields the same `K` and byte-identical SVG.

How it works: Vectrigo measures the k-means distortion (within-cluster
sum-of-squares) for increasing `K` and stops at the "knee" — the smallest `K`
that already explains the bulk of the image's colour variation, so extra colours
would add detail with diminishing returns.

- **Tuning the knee with `AutoKTau`.** `AutoKTau` (a `float64`) is the residual
  distortion threshold for that knee: the smallest `K` whose distortion has
  dropped to this fraction of the single-cluster distortion is chosen. **Smaller
  ⇒ more colours / higher fidelity; larger ⇒ fewer colours.** The zero value
  (and a bare `Config{}`) means the default, `0.02`, which preserves today's
  auto-K output; it is clamped to a maximum of `0.5`. It only applies under
  `AutoK` and has no effect otherwise. At the default `0.02`, complex photos
  rarely reach the knee and saturate near the internal colour ceiling, so they
  all pick a similar large `K`; raising it (around `0.05`) trips the knee earlier
  so different complex photos **differentiate** into distinct, smaller `K` values
  that reflect their complexity — at the cost of coarser output. Push it too high
  and even simple images start losing real colours.

## License

Vectrigo — the engine and the `vectrigo-cli` command — is licensed under the
[PolyForm Noncommercial License 1.0.0](LICENSE). **It is free for any
noncommercial use.** Commercial use is not granted by that license and requires a
separate commercial license from the copyright holder — contact Andrey Leybovich.

This split is possible precisely because every dependency is permissively
licensed, which lets Vectrigo set its own terms. Third-party components and their
licenses are reproduced in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md).

It bundles two in-house libraries, each independently licensed with its own
`LICENSE` file: [`bitrace`](bitrace/) (PolyForm Noncommercial 1.0.0) and
[`minisvg`](minisvg/) (MIT).
