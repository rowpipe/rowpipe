# rowpipe/parquet

`github.com/rowpipe/rowpipe/parquet` adapts a Parquet file to a `rowpipe.Source`,
streaming one row at a time so an arbitrarily large file never loads whole. Every
value is stringified to match rowpipe's all-strings row model; a nested or
repeated column is rejected (flatten the schema or use `jsonl`).

- **Dep:** `github.com/parquet-go/parquet-go`
- **Module:** a sibling submodule of the rowpipe core, with its own `go.mod`.

## Local development

The repo-root `go.work` workspace resolves the core locally, so this submodule
builds against the in-repo core from anywhere in the repo. **Before publishing:**
pin a tagged core version in `require` (replacing the `v0.0.0` placeholder).

## Test

```sh
cd parquet && go test ./...
```
