// Package xlsx adapts a single worksheet of an .xlsx workbook to a
// rowpipe.Source, streaming row by row via excelize so a whole sheet is never
// loaded at once. Cell values are excelize's formatted strings (dates render
// per cell style).
package xlsx

import (
	"errors"
	"fmt"
	"io"

	"github.com/rowpipe/rowpipe"
	"github.com/xuri/excelize/v2"
)

// Options select the sheet and header handling for a workbook.
type Options struct {
	// Sheet is the worksheet to read; "" picks the first visible sheet.
	Sheet string
	// NoHeader synthesizes column names c0,c1,… and treats the first row as
	// data; otherwise the first row is consumed as the header.
	NoHeader bool
}

// Source streams one worksheet row by row. It owns the workbook handle and the
// row iterator; call Close when the Source is exhausted or abandoned.
type Source struct {
	f       *excelize.File
	rows    *excelize.Rows
	header  rowpipe.Header
	out     rowpipe.Row
	pending rowpipe.Row
	hasPend bool
}

// Open opens the workbook at path. The returned Source owns the file handle.
func Open(path string, opts Options) (*Source, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("xlsx: %w", err)
	}
	return newSource(f, opts)
}

// OpenReader opens a workbook from r. excelize buffers the whole stream, so r is
// read in full before the first row is yielded.
func OpenReader(r io.Reader, opts Options) (*Source, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("xlsx: %w", err)
	}
	return newSource(f, opts)
}

func newSource(f *excelize.File, opts Options) (*Source, error) {
	sheet := opts.Sheet
	if sheet == "" {
		sheet = firstVisibleSheet(f)
	}
	if sheet == "" {
		_ = f.Close()
		return nil, errors.New("xlsx: workbook has no sheets")
	}
	rows, err := f.Rows(sheet)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("xlsx: sheet %q: %w", sheet, err)
	}

	s := &Source{f: f, rows: rows}
	first, ok, err := nextRow(rows)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	if !ok {
		_ = s.Close()
		return nil, fmt.Errorf("xlsx: sheet %q is empty", sheet)
	}
	if len(first) == 0 {
		// Excel trims trailing empty cells, so a blank leading row arrives as a
		// zero-cell slice. A zero-width header would silently drop every data
		// row, committing an empty dataset.
		_ = s.Close()
		return nil, fmt.Errorf("xlsx: sheet %q starts with a blank row; the first row must hold the header (or the data, under NoHeader)", sheet)
	}

	if opts.NoHeader {
		s.header = synthHeader(len(first))
		s.out = make(rowpipe.Row, len(s.header))
		s.pending = make(rowpipe.Row, len(s.header))
		padInto(s.pending, first)
		s.hasPend = true
	} else {
		s.header = rowpipe.Header(append([]string(nil), first...))
		s.out = make(rowpipe.Row, len(s.header))
	}
	return s, nil
}

func (s *Source) Header() rowpipe.Header { return s.header }

func (s *Source) Read() (rowpipe.Row, error) {
	if s.hasPend {
		s.hasPend = false
		return s.pending, nil
	}
	cols, ok, err := nextRow(s.rows)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, io.EOF
	}
	padInto(s.out, cols)
	return s.out, nil
}

// Close releases the row iterator and the workbook handle.
func (s *Source) Close() error {
	var first error
	if s.rows != nil {
		if err := s.rows.Close(); err != nil {
			first = err
		}
		s.rows = nil
	}
	if s.f != nil {
		if err := s.f.Close(); err != nil && first == nil {
			first = err
		}
		s.f = nil
	}
	return first
}

// nextRow advances the iterator one row. Excel omits trailing empty cells, so
// rows arrive ragged; the caller pads/truncates to header width.
func nextRow(rows *excelize.Rows) ([]string, bool, error) {
	if !rows.Next() {
		if err := rows.Error(); err != nil {
			return nil, false, fmt.Errorf("xlsx: %w", err)
		}
		return nil, false, nil
	}
	cols, err := rows.Columns()
	if err != nil {
		return nil, false, fmt.Errorf("xlsx: %w", err)
	}
	return cols, true, nil
}

// firstVisibleSheet returns the first non-hidden sheet, falling back to the
// first sheet if visibility can't be determined.
func firstVisibleSheet(f *excelize.File) string {
	list := f.GetSheetList()
	for _, name := range list {
		if vis, err := f.GetSheetVisible(name); err == nil && vis {
			return name
		}
	}
	if len(list) > 0 {
		return list[0]
	}
	return ""
}

func synthHeader(n int) rowpipe.Header {
	h := make(rowpipe.Header, n)
	for i := range h {
		h[i] = fmt.Sprintf("c%d", i)
	}
	return h
}

// padInto copies src into the reused row dst, truncating or zero-filling to
// dst's width so a ragged worksheet row always matches the locked header width.
func padInto(dst rowpipe.Row, src []string) {
	for i := range dst {
		if i < len(src) {
			dst[i] = src[i]
		} else {
			dst[i] = ""
		}
	}
}
