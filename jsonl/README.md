# rowpipe/jsonl

`github.com/rowpipe/rowpipe/jsonl` adapts JSON-Lines input (one JSON object per
line) to a `rowpipe.Source`. **Flatten mode only:** each object is flattened one
level — a nested object becomes dotted keys (`a.b`), deeper structures stay
raw-JSON strings — and projected onto a header locked from the first line or an
explicit column list.

- **Dep:** none (stdlib only)
- **Module:** a sibling submodule of the rowpipe core, with its own `go.mod`.

## Pre-publish replace

The submodule resolves the core locally via `replace github.com/rowpipe/rowpipe => ../`.
**Before publishing:** drop that `replace` line and pin a tagged core version.

## Test

```sh
cd jsonl && go test ./...
```
