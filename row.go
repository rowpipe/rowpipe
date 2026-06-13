// Package rowpipe is a constant-memory streaming row-transform engine.
//
// A pipeline reads one row at a time from a Source, pushes it through an
// ordered list of Stages, and writes survivors to a Sink. The built-in CSV
// Reader and Writer are the default Source and Sink; other input formats plug
// in as Sources (see the xlsx, parquet, and jsonl submodules). No stage buffers
// the whole input, so memory stays flat regardless of size — the one exception
// is Dedupe, whose memory scales with the number of distinct keys it has seen
// (it must remember them to detect repeats).
package rowpipe

// Header is the ordered set of column names flowing into a stage. Column stages
// rewrite it (select/drop/rename); row stages pass it through unchanged.
type Header []string

// Row is one record's fields, positionally aligned to the current Header.
//
// Rows are not owned by the stage that receives them: the Reader reuses its
// backing array across reads and stages reuse their output buffers, so a stage
// must finish with a Row (write it, hash it, copy what it needs) before the
// next record is pulled. Nothing downstream may retain a Row past its turn.
type Row []string

// index returns the position of name in h, or -1.
func (h Header) index(name string) int {
	for i, c := range h {
		if c == name {
			return i
		}
	}
	return -1
}
