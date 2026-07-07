# Vectrigo — Handoff Summary

## Repo & branch state
- **Branch:** `go-vectorization` (all work pushed). Commit as **Andrey Leybovich `<muzzzy@gmail.com>`** (git config already set locally). Never push to another branch without permission.
- **Commit trailers** (per environment): end messages with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01MkPe6kYjdSK8i3rHrfCvTs`. Never put model IDs in committed artifacts.
- **Working tree is clean.** Latest commits (newest first): `e2b38c7` layer-ordering · `4202d72` CI · `9f1358f` PolyForm relicense · `0c9ec22` engine · `19ae485` CLI · `272573e` bitrace+minisvg · `1e8baf2` Sensitivity · `7640926` plan.

## What exists (all committed, green, reviewed)
Three Go modules in one repo, all pure-Go / `CGO_ENABLED=0`, permissive dep graph:
- **`minisvg/`** — MIT, zero-dep SVG writer.
- **`bitrace/`** — PolyForm Noncommercial 1.0.0, clean-room bitmap tracer (dep: `honnef.co/go/curve` Apache-2.0).
- **root = `github.com/aleybovich/vectrigo`** — PolyForm Noncommercial 1.0.0, the engine + `cmd/vectrigo-cli`. Uses `replace` directives to the two local modules. Deps: imaging (MIT), muesli/kmeans+clusters (MIT), x/image (BSD-3).
- **CI** at `.github/workflows/ci.yml`: runs on any branch + PR + manual `workflow_dispatch`; jobs = test / race / cross-compile matrix / `go-licenses` audit across all 3 modules.
- Engine package layout: `config.go` (Config, DefaultConfig, `resolveDetail`, `maxKForPixels`), `vectrigo.go` (Vectorize, Engine), `internal/{normalize,quantize,pipeline,assemble,imageutil}`, `docs/engine-design.md`. Stage II quantize uses an **in-house seeded k-means++** (`internal/quantize/kmeans.go`); `muesli.go` is a documented non-default reference (muesli reseeds global `rand`).
- Test fixtures in `testdata/`: `street_market.png` (1024×559 real image), `shapes.{png,jpg,webp}` (96×64, owned, one per decoder).

## Baseline: how Sensitivity → K works today (committed)
`Config.Sensitivity` (int **0-100**, the primary knob) derives K and TurdSize in `config.go resolveDetail(W,H)`:
- `K = round(4·2^(S/25))` → 4, 8, 16, 32, **64** at S = 0, 25, 50, 75, 100. (So S=100 targets K=64, never 100.)
- `TurdSize = floor(8·2^(−S/25))` → 8, 4, 2, 1, 0.
- K then clamped to `[2, maxKForPixels(W·H)]`, `maxKForPixels = clamp(px/1024, 2, 256)`, and effective layers are also capped by **distinct colors present** (`TestQuantizeKClampToDistinctColors`).
- `Config{}` zero-value = Sensitivity 0; users build from `DefaultConfig()` (Sensitivity 50). Other fields: K, TurdSize (0=derive), AlphaMax, Optimize, MaxDimensions, Workers, Precision.

---

## STILL TO DO

### Category A — 2 of 3 items remain
Pattern the user wants: **Opus implements → Fable reviews (strict, zero findings incl. nits) → Opus fixes, ≤3 cycles; unit-tested; keep the full suite green including `-race`.** If an item can't pass in 3 cycles, stop and ask the user.

**1. Layer ordering — ✅ DONE, committed `e2b38c7`.** Caveat: verified green independently (build/vet/test/`-race` all pass, incl. new `internal/assemble/containment.go` + tests), but its **formal Fable review verdict was never captured** (the workflow ended abruptly). A review pass is advisable, not blocking.

**2. Auto-K — ❌ NOT started. Build to DESIGN A (below).** The big one.

**3. Performance — ❌ NOT started.** Correctness-preserving hot-path optimizations (flat buffers, buffer reuse, packed-bitset masks) in `internal/quantize`, `internal/pipeline`, possibly `bitrace`. **Hard bar: byte-identical output** to before + add Go benchmarks (before/after ns/op, B/op, allocs/op). Add an end-to-end golden test if none exists.

### THE key decision — Sensitivity vs Auto-K = **Design A (mutually exclusive)**
The user decided: **either you set a sensitivity, or you use auto-K — never both. Sensitivity has NO effect when auto-K is on.**

**Auto-K engine spec (design A):**
- Add `AutoK bool` to `Config`, default **false**.
- `AutoK == false` → **byte-identical** to today.
- `AutoK == true` → K chosen **automatically** from the image's color complexity (elbow method / silhouette), bounded **only** by the existing safety clamps (`maxKForPixels` + distinct colors). **Sensitivity is IGNORED for K** — it is *not* a ceiling and has no effect.
- `TurdSize` under AutoK must **not** depend on Sensitivity — derive it from the chosen K (mirror the existing K↔noise coupling) or a documented default; explicit `Config.TurdSize` override still works.
- Library level: if both `AutoK=true` and a Sensitivity are set, AutoK wins / Sensitivity ignored (document; **no error** at lib level — the CLI enforces the user-facing either/or).
- Deterministic (seeded); no new deps.
- **Tests:** synthetic images with known distinct-color counts (e.g. 3 and 6) → AutoK picks ~that count; **prove Sensitivity-independence** (same image ⇒ same auto-K at Sensitivity 0 and 100); gradient/complex ⇒ higher K than flat; `AutoK=false` ⇒ byte-identical golden. Full suite green with `-race`.
- **Document `AutoK` in godoc AND the root `README.md`** (the user explicitly asked for README docs of auto-K).

### CLI `--auto-k` flag — ❌ NOT started (depends on the `AutoK` field existing)
`cmd/vectrigo-cli` today: `vectrigo-cli <image-path> <sensitivity>` → writes `<name>.<sensitivity>.svg`; logic factored into `run()/outputPath()/parseSensitivity()` with tests. Required design-A behavior:

| Invocation | Behavior | Output file |
|---|---|---|
| `vectrigo-cli <img> <sensitivity>` | fixed K (today) | `<name>.<sensitivity>.svg` |
| `vectrigo-cli --auto-k <img>` | auto-K; no sensitivity | **`<name>.svg`** |
| `vectrigo-cli <img>` (no sensitivity, no `--auto-k`) | **error** (sensitivity required unless `--auto-k`) | — |
| `vectrigo-cli --auto-k <img> <sensitivity>` | **error** (mutually exclusive) | — |

`--auto-k` sets `cfg.AutoK=true`. **Sensitivity appears in the filename only when a sensitivity was provided.** Update `--help`/usage + package godoc; add tests for each row.

## Reusable assets
- **Category A workflow script** (edit + re-run via `Workflow({scriptPath})`): `/root/.claude/projects/-home-user-vectrigo/3d899354-d573-5549-a37a-7d056fe95811/workflows/scripts/category-a-improvements-wf_9492f53e-ba0.js`. It still contains the **old design-B (ceiling) auto-K** prompt — must be rewritten to design A. Item 1 (layer-ordering) is done, so drop it from a re-run. The perf item is fine as-is.
- **Ready-to-use design-A auto-K prompts** (drop into the workflow's `ITEMS` auto-k entry):

```
IMPLEMENT (opus, high):
"ITEM: Automatic K selection (internal/quantize + config), mode 'either auto OR sensitivity'.
Add Config.AutoK bool (default false). AutoK==false => byte-identical to today.
AutoK==true => K chosen automatically from the image's colour complexity (elbow/silhouette),
bounded ONLY by maxKForPixels + distinct colours. Sensitivity is IGNORED for K (NOT a ceiling,
no effect). TurdSize under AutoK must not depend on Sensitivity (derive from chosen K or a
documented default; explicit Config.TurdSize override still works). If both AutoK and Sensitivity
are set, AutoK wins (no error at lib level). Deterministic; no new deps. Document AutoK in godoc
AND the root README. Tests: known-colour synthetic images (3 and 6) -> ~that count; PROVE
Sensitivity-independence (same auto-K at Sensitivity 0 and 100); gradient -> higher K than flat;
AutoK=false -> byte-identical golden. Keep full suite green incl. -race."

REVIEW (fable, schema): verify OFF==byte-identical; ON picks K from image AND is independent of
Sensitivity (test proves same K at S=0 and S=100); TurdSize independent of Sensitivity under AutoK;
deterministic; documented in godoc+README; non-vacuous tests; strict pass (zero findings).
```

- Environment: Go 1.26.2 via `GOTOOLCHAIN=auto` (installed 1.24.7 auto-upgrades). Deps fetchable through the proxy. WEBP fixture was made with pure-Go `nativewebp` in scratch only (NOT a dependency).

## Working rules the user enforces
- **Only commit reviewed/approved code — never drafts.** (The layer-ordering and this handoff commit were explicit exceptions requested by the user.)
- Model routing they like: **Opus** for implement/fix + hard reviews; **Fable** for reviews (but they said use **Opus, not Fable, for simple reviews** like minisvg to save Fable); **Sonnet** for simple tasks (the CLI). They've opted into multi-agent workflows.
- A stop-hook nags about uncommitted files every turn.

## Other open items
- **plan.md is stale on licensing** — still says in-house libs "MIT recommended" / project "targets commercial/closed-source." Update to the PolyForm split (engine+CLI+bitrace = PolyForm NC; minisvg = MIT).
- **Category B (§15) quality items** — corner-detection robustness, simplification-vs-fidelity, anti-aliasing/edge-fringing — deferred; these need a **visual before/after harness**, not the pure unit-test loop (unit tests can't judge visual quality).
- **Engine LICENSE** commercial contact is name-only (no email hardcoded, by choice).
- **Old remote branch** `claude/vectrigo-go-vectorization-t81kn0` — user to delete (my delete got a 403 from the proxy).

---

**Critical path for the next agent:** auto-K (design A) → its README/godoc docs → CLI `--auto-k` → performance item → optional review pass on layer-ordering.
