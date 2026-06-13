package rowpipe

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
)

const readBufBytes = 1 << 20

// Reader is a streaming CSV source. It pulls one record at a time and never
// holds more than a single row in memory.
type Reader struct {
	cr     *csv.Reader
	header Header
	// pending holds the first data row when NoHeader synthesized the header
	// from it; the row itself is still real data and must be returned first.
	pending Row
	hasPend bool
}

// ReaderOptions configure how raw bytes are decoded into records.
type ReaderOptions struct {
	// Delimiter is the field separator (default ',').
	Delimiter rune
	// NoHeader treats the first line as data and synthesizes column names
	// c0,c1,…; otherwise the first line is consumed as the header.
	NoHeader bool
	// LazyQuotes tolerates bare quotes inside fields instead of erroring.
	LazyQuotes bool
}

// NewReader wraps r and reads its header (or synthesizes one). It returns an
// error if the stream is empty or the header line is malformed.
func NewReader(r io.Reader, opts ReaderOptions) (*Reader, error) {
	cr := csv.NewReader(bufio.NewReaderSize(r, readBufBytes))
	if opts.Delimiter != 0 {
		cr.Comma = opts.Delimiter
	}
	cr.LazyQuotes = opts.LazyQuotes
	// ReuseRecord lets csv reuse the record slice across Read calls; the
	// pipeline writes each row before pulling the next, so this is safe and
	// removes a per-row allocation.
	cr.ReuseRecord = true

	first, err := cr.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("rowpipe: empty input")
	}
	if err != nil {
		return nil, fmt.Errorf("rowpipe: reading header: %w", err)
	}

	rd := &Reader{cr: cr}
	if opts.NoHeader {
		rd.header = make(Header, len(first))
		for i := range first {
			rd.header[i] = fmt.Sprintf("c%d", i)
		}
		// first is reused by csv on the next Read; copy it so the pending
		// data row survives until the caller pulls it.
		rd.pending = append(Row(nil), first...)
		rd.hasPend = true
	} else {
		rd.header = append(Header(nil), first...)
	}
	return rd, nil
}

// Header returns the column names for every row this Reader yields.
func (r *Reader) Header() Header { return r.header }

// Read returns the next record, or io.EOF when the stream is exhausted. The
// returned Row is only valid until the next Read.
func (r *Reader) Read() (Row, error) {
	if r.hasPend {
		r.hasPend = false
		return r.pending, nil
	}
	rec, err := r.cr.Read()
	if err != nil {
		return nil, err
	}
	return Row(rec), nil
}
