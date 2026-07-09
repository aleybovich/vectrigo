# vectrigo-cli

`vectrigo-cli` is the command-line front-end for the
[Vectrigo](../../) engine: it vectorizes a raster image (**PNG / JPEG / WEBP**)
into an **SVG** file written next to the input.

It is a thin wrapper over the `github.com/aleybovich/vectrigo` library — see the
[root README](../../README.md) for the engine, the two pipelines
(quantization vs segmentation), and the full `Config` reference.

## Install / build

```sh
# install to $GOBIN
go install github.com/aleybovich/vectrigo/cmd/vectrigo-cli@latest

# or build a local binary (pure Go, cgo disabled)
CGO_ENABLED=0 go build -o bin/vectrigo-cli ./cmd/vectrigo-cli
```

Rebuild after pulling engine changes so the binary stays in sync with the flags.

## Usage

```
vectrigo-cli -i <image-path> -s <sensitivity>
vectrigo-cli -i <image-path> --auto-k
vectrigo-cli -i <image-path> --photo [--sigma <n>] [--simplify <subtle|aggressive>] [--edge <crisp|stroke>]
```

The input is given with `--input`/`-i` and is required. Exactly **one mode** must
be chosen: `--sensitivity`, `--auto-k`, or `--photo` (passing more than one, or
none, is an error). On success the output path is printed to stdout.

## Modes

Vectrigo has two pipelines; the mode flags select between them:

- **Quantization** (colour-first, best for **flat / logo / icon art**):
  - `--sensitivity`/`-s <0-100>` — fixed detail knob (higher = more colours / detail).
  - `--auto-k` — let the engine pick the colour count automatically (no sensitivity).
- **Segmentation** (region-first, best for **photographic / painterly images**):
  - `--photo` — enable the segmentation pipeline.

## Flags

| Flag | Applies to | Meaning |
|---|---|---|
| `-i`, `--input <path>` | all | Input raster image (PNG/JPEG/WEBP). **Required.** |
| `-s`, `--sensitivity <0-100>` | quantization | Fixed detail level. Mutually exclusive with `--auto-k` / `--photo`. |
| `--auto-k` | quantization | Auto-select the colour count `K` from the image; no sensitivity used. |
| `--photo` | segmentation | Use the region-first segmentation pipeline. |
| `--sigma <n>` | photo only | Detail dial (bilateral σ_r): `~8` punchy, `12` default, `28+` soft. Range clamped to `[4,60]`. Only valid with `--photo`; unset = `12`. |
| `--simplify <subtle\|aggressive>` | photo only | **Opt-in** boundary simplification (node-count / file-size reduction). Unset = **off** (maximum fidelity, most nodes). `subtle` is visually near-lossless with ~3× fewer nodes on straight edges; `aggressive` gives the smallest files at visibly coarser shapes. Seams never open at any setting. Only valid with `--photo`. |
| `--edge <crisp\|stroke>` | photo only | Edge finish: `crisp` (default) disables edge anti-aliasing for a seam-free flat look; `stroke` keeps anti-aliasing and seals sub-pixel seams with a thin same-colour stroke. Only valid with `--photo`. |
| `-h`, `--help` | — | Show help and exit. |

`--sigma`, `--simplify` and `--edge` require `--photo`; using them in another mode is an error.

## Output naming

The SVG is written next to the input, with the extension replaced per mode:

| Invocation | Output |
|---|---|
| `-i photos/street.png -s 70` | `photos/street.70.svg` |
| `-i photos/street.png --auto-k` | `photos/street.svg` |
| `-i photos/street.png --photo` | `photos/street.photo.svg` |

## Examples

```sh
# flat / logo art — fixed detail
vectrigo-cli -i logo.png -s 70                       # -> logo.70.svg

# flat art — automatic colour count
vectrigo-cli -i logo.png --auto-k                    # -> logo.svg

# photograph — segmentation, defaults (crisp edges, sigma 12, no simplification)
vectrigo-cli -i street.jpg --photo                   # -> street.photo.svg

# photograph — punchier detail, anti-aliased stroke edges
vectrigo-cli -i street.jpg --photo --sigma 8 --edge stroke

# photograph — fewer nodes / smaller file, near-lossless
vectrigo-cli -i street.jpg --photo --simplify subtle       # -> street.photo.svg

# photograph — smallest file (coarser shapes)
vectrigo-cli -i street.jpg --photo --simplify aggressive   # -> street.photo.svg
```

## Exit status

Returns non-zero and prints a `vectrigo-cli: <error>` message to stderr on any
failure (bad flags, missing/mutually-exclusive mode, unreadable input, write
error). No output file is created when an argument error is detected.

## License

Part of the Vectrigo engine module — [PolyForm Noncommercial License 1.0.0](../../LICENSE):
free for noncommercial use; commercial use requires a separate license from
Andrey Leybovich.
