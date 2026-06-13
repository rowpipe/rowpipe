package rowpipe

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// Stage is one step in a transform pipeline. Init runs once with the header
// produced by the previous stage and returns the header this stage emits;
// Process then runs per record.
//
// A Stage must not retain the Row passed to Process, nor the Row it returns,
// past the call — buffers upstream and downstream are reused. Stages that
// reshape rows (select/drop) should keep one reusable output buffer rather than
// allocating per row.
type Stage interface {
	// Init validates the stage against in and returns the outgoing header.
	Init(in Header) (out Header, err error)
	// Process transforms one record. keep reports whether out should continue
	// down the pipeline; when false, out is ignored and the record is dropped.
	Process(in Row) (out Row, keep bool, err error)
}

// Stats summarize one pipeline run.
type Stats struct {
	RowsIn  int64
	RowsOut int64
	// Dropped is rows removed by filtering or dedup (RowsIn - RowsOut).
	Dropped  int64
	Duration time.Duration
}

// Sink consumes the stream Run produces: the header on the first call, then one
// surviving row per call, with Flush draining buffered output at end of stream.
// The concrete CSV *Writer satisfies it; alternative sinks (e.g. a Parquet
// encoder) implement the same two methods to receive the same stream.
type Sink interface {
	Write(rec []string) error
	Flush() error
}

// Source is the input side of a run: it yields a Header once, then one Row per
// Read until io.EOF. The concrete CSV *Reader satisfies it; alternative sources
// (e.g. xlsx, Parquet, or JSONL reader adapters living in the consuming module)
// implement the same two methods to feed the same Stage/Sink pipeline. Sources
// inherit the row-ownership contract (row.go): a returned Row is valid only
// until the next Read, so nothing downstream may retain it.
type Source interface {
	Header() Header
	Read() (Row, error)
}

// Run wires source → stages → sink and drives the stream to completion. It
// reads one record at a time and writes survivors immediately, so peak memory
// is independent of input length (Dedupe aside). The sink is flushed before
// Run returns, including on error, so partial output is not lost.
func Run(src Source, stages []Stage, w Sink) (Stats, error) {
	start := time.Now()
	var st Stats

	header := src.Header()
	for i, s := range stages {
		next, err := s.Init(header)
		if err != nil {
			return st, fmt.Errorf("stage %d (%T): %w", i, s, err)
		}
		header = next
	}
	if err := w.Write(header); err != nil {
		return st, fmt.Errorf("writing header: %w", err)
	}

	for {
		row, err := src.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			st.finish(start)
			_ = w.Flush()
			return st, fmt.Errorf("reading row %d: %w", st.RowsIn+1, err)
		}
		st.RowsIn++

		cur, keep := row, true
		for _, s := range stages {
			cur, keep, err = s.Process(cur)
			if err != nil {
				st.finish(start)
				_ = w.Flush()
				return st, fmt.Errorf("row %d: %w", st.RowsIn, err)
			}
			if !keep {
				break
			}
		}
		if !keep {
			continue
		}
		if err := w.Write(cur); err != nil {
			st.finish(start)
			_ = w.Flush()
			return st, fmt.Errorf("writing row %d: %w", st.RowsIn, err)
		}
		st.RowsOut++
	}

	st.finish(start)
	if err := w.Flush(); err != nil {
		return st, fmt.Errorf("flushing output: %w", err)
	}
	return st, nil
}

func (st *Stats) finish(start time.Time) {
	st.Dropped = st.RowsIn - st.RowsOut
	st.Duration = time.Since(start)
}

// errColumnNotFound is returned by stage Init when a referenced column is
// absent from the incoming header.
var errColumnNotFound = errors.New("column not found")

func columnNotFound(name string, h Header) error {
	return fmt.Errorf("%w: %q (have %v)", errColumnNotFound, name, []string(h))
}
