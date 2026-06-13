# rowpipe

A constant-memory streaming row-transform engine in Go. A pipeline reads one row
at a time from a `Source`, pushes it through an ordered list of `Stage`s, and
writes survivors straight to a `Sink` — so peak memory is independent of input
length, whether the file is a megabyte or a terabyte.

The pipeline is **format-agnostic**: `Source → []Stage → Sink`. CSV is only the
built-in `Source`/`Sink` and the origin of the name, not a limit. Excel, Parquet,
and JSON-Lines reach the same pipeline through adapter submodules (see
[Formats](#formats)).

- **Core has zero dependencies** — standard library only.
- **Constant memory** — no per-row allocation or whole-file buffering in the hot path.
- **Composable stages** — `filter`, `dedupe`, `select`, `drop`, `rename`, `validate`, applied in order as AND.

## Install

```sh
go get github.com/rowpipe/rowpipe
```

Requires **Go 1.26+**.

## Quick start

Filter, project, and de-duplicate a CSV stream — stdin to stdout, constant memory:

```go
package main

import (
	"log"
	"os"

	"github.com/rowpipe/rowpipe"
)

func main() {
	src, err := rowpipe.NewReader(os.Stdin, rowpipe.ReaderOptions{})
	if err != nil {
		log.Fatal(err)
	}

	stages, err := rowpipe.Compile([]rowpipe.StageSpec{
		{Verb: "filter", Arg: "status==active"},
		{Verb: "select", Arg: "id,email,amount"},
		{Verb: "dedupe", Arg: "email"},
	})
	if err != nil {
		log.Fatal(err)
	}

	sink := rowpipe.NewWriter(os.Stdout, rowpipe.WriterOptions{})

	stats, err := rowpipe.Run(src, stages, sink)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("in=%d out=%d dropped=%d", stats.RowsIn, stats.RowsOut, stats.Dropped)
}
```

## Pipeline model

```
Source ──Read()──▶ Stage ─▶ Stage ─▶ … ─▶ Sink
```

- **`Source`** yields a `Header` once, then one `Row` per `Read()` until `io.EOF`.
- **`Stage`** runs `Init(Header) → Header` once to validate/reshape the header, then `Process(Row) → (Row, keep, err)` per record. Returning `keep == false` drops the row.
- **`Sink`** receives the header, then one surviving row per call, then `Flush()`.

`Run` wires the three together and drives the stream to completion, flushing the
sink before it returns (even on error, so partial output is not lost).

## Stages

Stages are declared as `StageSpec{Verb, Arg}` and turned into runnable stages by
`Compile` (the same grammar a CLI or HTTP front-end would expose), or constructed
directly with the `New*` functions.

| Verb       | Arg grammar                              | Example                              |
|------------|------------------------------------------|--------------------------------------|
| `filter`   | `COL OP VALUE`                           | `amount >= 100`, `status==active`, `name ~ ^A`, `notes empty` |
| `dedupe`   | comma-separated key columns              | `email` or `first,last`              |
| `select`   | comma-separated columns (reorders)       | `id,email,amount`                    |
| `drop`     | comma-separated columns                  | `internal_notes`                     |
| `rename`   | `old=new,old2=new2`                      | `e=email,amt=amount`                 |
| `validate` | `COL:TYPE[:opt...]` joined by `;`        | `email:email:required;age:int:min=0` |

Filter operators: `==`, `!=`, `>`, `<`, `>=`, `<=`, `~` (regex), `!~` (negated
regex), and the unary `empty` / `notempty`. Validate types: `string`, `int`,
`number`, `email`; options: `required`, `min=N`, `max=N`.

### Validation reports

`validate` is the one stage that produces out-of-band output. It **quarantines**
every row with a failing cell (the `Sink` receives only clean rows) and
accumulates a `Report` with exact counts and a bounded error sample, read after
`Run`:

```go
rules, _ := rowpipe.ParseValidateSpec("email:email:required;age:int:min=0")
v := rowpipe.NewValidate(rules)

stats, err := rowpipe.Run(src, []rowpipe.Stage{v}, sink)
// ...
rep := v.Report() // rep.Invalid, rep.Errors, rep.Sample, rep.Truncated
```

Use `rowpipe.CollectReports(stages)` to merge reports across every validator in a
pipeline, and `rowpipe.HasValidate(stages)` to test for presence.

## Formats

Every non-CSV format is a **sibling submodule** with its own `go.mod`, so the
core's dependency set stays empty and a consumer pays only for the formats it
imports. The split is by **role**, not weight: CSV stays in the core because it's
the engine's built-in default `Source`/`Sink` (and namesake); every other format
is a pluggable input adapter. Dependency isolation is what makes it matter for the
heavy formats — xlsx and Parquet each pull in a large third-party tree — while
`jsonl` carries no external dependency and lives here only to keep the pattern
uniform:

| Module                                  | Format                | Dependency                          |
|-----------------------------------------|-----------------------|-------------------------------------|
| `github.com/rowpipe/rowpipe/xlsx`       | Excel `.xlsx` sheets  | `github.com/xuri/excelize/v2`       |
| `github.com/rowpipe/rowpipe/parquet`    | Parquet files         | `github.com/parquet-go/parquet-go`  |
| `github.com/rowpipe/rowpipe/jsonl`      | JSON-Lines (flatten)  | none (stdlib only)                  |

Each adapter returns a type satisfying `rowpipe.Source`, so it feeds the same
`Stage`/`Sink` pipeline. See each submodule's `README.md` for details.

## Extending

- **A new input format** is a new `Source` (`Header()` + `Read()`) — keep heavy deps in a submodule.
- **A new transform** is a new `Stage`; register a verb in `compile.go` if you want it reachable through `Compile`.
- **A new output** is a new `Sink` (`Write([]string)` + `Flush()`) — e.g. a Parquet or JSON encoder.

A `Source` or `Stage` that knows its column **types** can implement the optional
`SchemaSource` / `SchemaStage` interfaces, so a typed `Sink` preserves them
through the pipeline; everything that speaks only text is unaffected (rows stay
`[]string`).

### Row ownership (the main footgun)

A `Row` passed to `Process` is **not owned** by the stage: the reader reuses its
backing array and stages reuse output buffers. Finish with a row before the next
`Read`, never retain it, and reshaping stages should keep **one** reusable output
buffer rather than allocating per row.

## Development

```sh
go build ./...
go test ./...
go test -bench . -benchmem -run '^$'
```

Root `go build`/`go test` cover the core only; test a format adapter by `cd`-ing
into its submodule.

## License

[Apache License 2.0](LICENSE).
