# rowpipe/jsonl

`github.com/rowpipe/rowpipe/jsonl` adapts JSON-Lines input (one JSON object per
line) to a `rowpipe.Source`. **Flatten mode only:** each object is flattened one
level — a nested object becomes dotted keys (`a.b`), deeper structures stay
raw-JSON strings — and projected onto a header locked from the first line or an
explicit column list.

- **Dep:** none (stdlib only)
- **Module:** a sibling submodule of the rowpipe core, with its own `go.mod`.

## Local development

The repo-root `go.work` workspace resolves the core locally, so this submodule
builds against the in-repo core from anywhere in the repo. **Before publishing:**
pin a tagged core version in `require` (replacing the `v0.0.0` placeholder).

## Test

```sh
cd jsonl && go test ./...
```
