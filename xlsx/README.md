# rowpipe/xlsx

`github.com/rowpipe/rowpipe/xlsx` adapts a single worksheet of an `.xlsx`
workbook to a `rowpipe.Source`, streaming row by row via excelize so a whole
sheet is never loaded at once. Cell values are excelize's formatted strings
(dates render per cell style).

- **Dep:** `github.com/xuri/excelize/v2`
- **Module:** a sibling submodule of the rowpipe core, with its own `go.mod`.

## Pre-publish replace

The submodule resolves the core locally via `replace github.com/rowpipe/rowpipe => ../`.
**Before publishing:** drop that `replace` line and pin a tagged core version.

## Test

```sh
cd xlsx && go test ./...
```
