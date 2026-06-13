# rowpipe/parquet

`github.com/rowpipe/rowpipe/parquet` adapts a Parquet file to a `rowpipe.Source`,
streaming one row at a time so an arbitrarily large file never loads whole. Every
value is stringified to match rowpipe's all-strings row model; a nested or
repeated column is rejected (flatten the schema or use `jsonl`).

- **Dep:** `github.com/parquet-go/parquet-go`
- **Module:** a sibling submodule of the rowpipe core, with its own `go.mod`.

## Pre-publish replace

The submodule resolves the core locally via `replace github.com/rowpipe/rowpipe => ../`.
**Before publishing:** drop that `replace` line and pin a tagged core version.

## Test

```sh
cd parquet && go test ./...
```
