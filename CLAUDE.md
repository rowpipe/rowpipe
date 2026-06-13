# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`rowpipe` is a constant-memory streaming row-transform engine in Go. The pipeline is
**format-agnostic** (`Source` → `[]Stage` → `Sink`; see Architecture): CSV is only its built-in
`Source`/`Sink` and the origin of the name, not a limit. Non-CSV formats reach the same pipeline as
a `Source`: the xlsx/Parquet/JSONL adapters live in **sibling submodules** (`xlsx/`, `parquet/`,
`jsonl/`) — each its own Go module with its own heavy dep (see each submodule's `README.md`) — so
the core stays stdlib-only and a consumer pays only for the formats it imports. Module path
`github.com/rowpipe/rowpipe`; **Go 1.26** toolchain (uses recent stdlib, e.g. `strings.SplitSeq`).
The **core module has no external dependencies** (stdlib only).

## Repository layout (multi-module)

A Go multi-module repo. The **core** (`github.com/rowpipe/rowpipe`, repo root) is stdlib-only. Three
sibling submodules adapt heavy formats to `Source`, each with its own `go.mod` and `README.md`:

- `xlsx/`    — Excel `.xlsx` worksheets (see `xlsx/README.md`)
- `parquet/` — Parquet files (see `parquet/README.md`)
- `jsonl/`   — JSON-Lines, flatten mode (see `jsonl/README.md`)

Each submodule resolves the core locally via a `replace` directive pre-publish — see its `README.md`
for the per-module dep and the publish steps. Root `go build ./...` / `go test ./...` cover the
**core only**; test a submodule by `cd`-ing into it.

## Commands

```sh
go build ./...
go test ./...
go test -run TestDedupeByColumn -v
go test -bench . -benchmem -run '^$'
```

## Architecture

`Source` → `[]Stage` → `Sink`, driven by `Run` (`stage.go`); the concrete CSV `Reader`/`Writer` are
the default `Source`/`Sink`, so consumers can feed other inputs (the `xlsx/`, `parquet/`, `jsonl/`
submodules, or any custom `Source`) through the same pipeline. The `Stage` interface (`stage.go`) is the central seam: `Init(Header) → Header`
runs once to validate/reshape the header; `Process(Row) → (Row, keep, err)` runs per record. Stages
compose as **AND** in the order given.
Verbs are registered in `compile.go`: `filter` (`filter.go`), `dedupe` (`dedupe.go`),
`select`/`drop`/`rename` (`columns.go`), `validate` (`validate.go`).
Stages talk to the pipeline only through the row stream (keep/drop, reshape) — the one exception is
`validate`, which both **quarantines** every row with a failing cell (`Process` returns `keep=false`,
so the Sink receives only clean rows) and accumulates an out-of-band `Report` (exact counts + a
bounded error sample) read **after** `Run` via `Validate.Report()`, or merged across every validator
with `CollectReports(stages)` (`HasValidate(stages)` tests presence). `RowError.Row` is numbered
within the stream **reaching that validator**, so an earlier row-dropping stage shifts it off the
source line number.

## Invariants to respect

- **Row/Header ownership (the main footgun).** A `Row` passed to `Process` is **not owned** by the
  stage: the `Reader` reuses its backing array (`ReuseRecord`) and stages reuse output buffers.
  Finish with a row before the next `Read`; never retain it. Reshaping stages (`Select`/`Drop`) keep
  **one** reusable output buffer rather than allocating per row — match that pattern.
- **Constant memory is the product.** No per-row allocation or whole-file buffering in the hot path.
  The only stateful stage is `Dedupe` (its `seen` set grows with **distinct keys**, by design);
  `validate` keeps exact counts but a **bounded** error sample (`defaultSampleCap`).
- **Dedup keys are exact, never hashed** — joined with `\x1f` (U+001F) so distinct column tuples
  can't collide. Important for financial/HR data. Don't switch to hashing.

## Adding a stage

1. Implement the `Stage` (a new file), following the reusable-buffer convention.
2. Register it in `compile.go`: a `Verb*` constant, a case in `IsVerb`, a case in `compileOne`.
3. Front-end consumers accept any verb `IsVerb` recognizes; only a CLI needs a flag added there.

## Adding a format

A new input format is a new **submodule**, not a core change: a package exposing a constructor that
returns a type satisfying `rowpipe.Source` (`Header()` + `Read()`), with its own `go.mod` carrying the
format's dep. Follow `parquet/` for a `ReaderAt`-based source or `jsonl/` for a stdlib reader; honor
the row-ownership contract (reuse one output buffer, never retain a `Row`). Keep heavy deps out of the
core module. A consumer wires detection/transport/encoding to it.
