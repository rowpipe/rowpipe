// Package parquet adapts a Parquet file to a rowpipe.Source, streaming one row
// at a time so an arbitrarily large file never loads whole. Every value is
// stringified to match rowpipe's all-strings row model.
package parquet

import (
	"fmt"
	"io"
	"strconv"

	pq "github.com/parquet-go/parquet-go"
	"github.com/rowpipe/rowpipe"
)

const batchSize = 256

// Source streams a Parquet file row by row, stringifying every value. It also
// reports the file's physical column types (Schema), so a typed Sink downstream
// preserves them instead of re-flattening to text — the cells are still streamed
// as strings, the schema rides alongside.
type Source struct {
	header rowpipe.Header
	schema rowpipe.Schema
	groups []pq.RowGroup
	gi     int
	rows   pq.Rows
	batch  []pq.Row
	n, i   int
	out    rowpipe.Row
}

// NewSource opens the Parquet file readable at r (of the given byte size) and
// returns a streaming Source over its rows. Parquet needs random access for its
// footer, hence io.ReaderAt rather than io.Reader. It rejects any nested or
// repeated column: the all-strings projection keeps one value per column, so a
// repeated column would silently lose data — flatten the schema or use jsonl.
func NewSource(r io.ReaderAt, size int64) (*Source, error) {
	pf, err := pq.OpenFile(r, size)
	if err != nil {
		return nil, fmt.Errorf("parquet: %w", err)
	}
	fields := pf.Schema().Fields()
	header := make(rowpipe.Header, 0, len(fields))
	schema := make(rowpipe.Schema, 0, len(fields))
	for _, fld := range fields {
		if !fld.Leaf() || fld.Repeated() {
			return nil, fmt.Errorf("parquet: column %q is nested or repeated; flatten to a flat scalar schema or use jsonl", fld.Name())
		}
		header = append(header, fld.Name())
		schema = append(schema, rowpipe.Column{Name: fld.Name(), Type: columnType(fld.Type())})
	}
	return &Source{
		header: header,
		schema: schema,
		groups: pf.RowGroups(),
		batch:  make([]pq.Row, batchSize),
		out:    make(rowpipe.Row, len(header)),
	}, nil
}

func (s *Source) Header() rowpipe.Header { return s.header }

// Schema reports the file's physical column types so a typed Sink preserves them.
func (s *Source) Schema() rowpipe.Schema { return s.schema }

// columnType maps a Parquet leaf's physical kind to the ColumnType its stringified
// cell round-trips into. Decimal/Date/Time/Timestamp logical columns stringify
// (via valueString) into forms the scalar parsers don't reconstruct, so they stay
// text for now — typed timestamp/date passthrough from Parquet is a follow-up.
func columnType(t pq.Type) rowpipe.ColumnType {
	if lt := t.LogicalType(); lt != nil {
		switch {
		case lt.Decimal != nil, lt.Date != nil, lt.Time != nil, lt.Timestamp != nil:
			return rowpipe.ColString
		}
	}
	switch t.Kind() {
	case pq.Boolean:
		return rowpipe.ColBool
	case pq.Int32, pq.Int64:
		return rowpipe.ColInt
	case pq.Float, pq.Double:
		return rowpipe.ColFloat
	default:
		return rowpipe.ColString
	}
}

// Close releases the active row-group reader. Read closes each reader as it is
// drained, so this only matters on early termination (a pipeline error or
// caller abandoning the stream before io.EOF).
func (s *Source) Close() error {
	if s.rows == nil {
		return nil
	}
	err := s.rows.Close()
	s.rows = nil
	return err
}

func (s *Source) Read() (rowpipe.Row, error) {
	for s.i >= s.n {
		if s.rows == nil {
			if s.gi >= len(s.groups) {
				return nil, io.EOF
			}
			s.rows = pq.NewRowGroupRowReader(s.groups[s.gi])
			s.gi++
		}
		n, err := s.rows.ReadRows(s.batch)
		s.n, s.i = n, 0
		if n == 0 {
			_ = s.rows.Close()
			s.rows = nil
			if err != nil && err != io.EOF {
				return nil, fmt.Errorf("parquet: read: %w", err)
			}
			continue
		}
		// n>0 with io.EOF: the EOF resurfaces on the next ReadRows call.
	}
	row := s.batch[s.i]
	s.i++
	clearRow(s.out)
	for _, v := range row {
		if ci := v.Column(); ci >= 0 && ci < len(s.out) {
			s.out[ci] = valueString(v)
		}
	}
	return s.out, nil
}

func valueString(v pq.Value) string {
	if v.IsNull() {
		return ""
	}
	switch v.Kind() {
	case pq.Boolean:
		return strconv.FormatBool(v.Boolean())
	case pq.Int32:
		return strconv.FormatInt(int64(v.Int32()), 10)
	case pq.Int64:
		return strconv.FormatInt(v.Int64(), 10)
	case pq.Float:
		return strconv.FormatFloat(float64(v.Float()), 'g', -1, 32)
	case pq.Double:
		return strconv.FormatFloat(v.Double(), 'g', -1, 64)
	case pq.ByteArray, pq.FixedLenByteArray:
		return string(v.ByteArray())
	default:
		return v.String()
	}
}

func clearRow(r rowpipe.Row) {
	for i := range r {
		r[i] = ""
	}
}
