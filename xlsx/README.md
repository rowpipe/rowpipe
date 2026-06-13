# rowpipe/xlsx

`github.com/rowpipe/rowpipe/xlsx` adapts a single worksheet of an `.xlsx`
workbook to a `rowpipe.Source`, streaming row by row via excelize so a whole
sheet is never loaded at once. Cell values are excelize's formatted strings
(dates render per cell style).

- **Dep:** `github.com/xuri/excelize/v2`
- **Module:** a sibling submodule of the rowpipe core, with its own `go.mod`.

## Local development

The repo-root `go.work` workspace resolves the core locally, so this submodule
builds against the in-repo core from anywhere in the repo. **Before publishing:**
pin a tagged core version in `require` (replacing the `v0.0.0` placeholder).

## Test

```sh
cd xlsx && go test ./...
```
