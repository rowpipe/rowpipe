package rowpipe

import (
	"fmt"
	"strings"
)

// Select projects rows down to the named columns, in the order given. Listing
// every column in a new order is therefore also how you reorder. Output rows are
// built in one reused buffer.
type Select struct {
	cols []string
	idxs []int
	out  Row
}

// NewSelect keeps cols (in order). It is an error for cols to be empty.
func NewSelect(cols []string) *Select { return &Select{cols: cols} }

func (s *Select) Init(in Header) (Header, error) {
	if len(s.cols) == 0 {
		return nil, fmt.Errorf("select: no columns given")
	}
	s.idxs = make([]int, len(s.cols))
	out := make(Header, len(s.cols))
	for i, c := range s.cols {
		j := in.index(c)
		if j < 0 {
			return nil, columnNotFound(c, in)
		}
		s.idxs[i] = j
		out[i] = c
	}
	s.out = make(Row, len(s.cols))
	return out, nil
}

func (s *Select) Process(in Row) (Row, bool, error) {
	for i, j := range s.idxs {
		if j < len(in) {
			s.out[i] = in[j]
		} else {
			s.out[i] = ""
		}
	}
	return s.out, true, nil
}

// OutSchema projects the typed schema to the selected columns, carrying each
// column's type from the input — mirroring Init's header projection.
func (s *Select) OutSchema(in Schema) (Schema, error) {
	if len(s.cols) == 0 {
		return nil, fmt.Errorf("select: no columns given")
	}
	out := make(Schema, len(s.cols))
	for i, c := range s.cols {
		j := in.index(c)
		if j < 0 {
			return nil, columnNotFound(c, in.Header())
		}
		out[i] = Column{Name: c, Type: in[j].Type}
	}
	return out, nil
}

// Drop removes the named columns, preserving the order of those that remain.
type Drop struct {
	cols map[string]struct{}
	keep []int
	out  Row
}

// NewDrop removes cols from every row.
func NewDrop(cols []string) *Drop {
	set := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		set[c] = struct{}{}
	}
	return &Drop{cols: set}
}

func (d *Drop) Init(in Header) (Header, error) {
	for c := range d.cols {
		if in.index(c) < 0 {
			return nil, columnNotFound(c, in)
		}
	}
	d.keep = d.keep[:0]
	var out Header
	for i, name := range in {
		if _, drop := d.cols[name]; drop {
			continue
		}
		d.keep = append(d.keep, i)
		out = append(out, name)
	}
	d.out = make(Row, len(d.keep))
	return out, nil
}

func (d *Drop) Process(in Row) (Row, bool, error) {
	for n, i := range d.keep {
		if i < len(in) {
			d.out[n] = in[i]
		} else {
			d.out[n] = ""
		}
	}
	return d.out, true, nil
}

// OutSchema removes the dropped columns, carrying the surviving columns' types
// through in order — mirroring Init's header reshape.
func (d *Drop) OutSchema(in Schema) (Schema, error) {
	for c := range d.cols {
		if in.index(c) < 0 {
			return nil, columnNotFound(c, in.Header())
		}
	}
	var out Schema
	for _, col := range in {
		if _, drop := d.cols[col.Name]; drop {
			continue
		}
		out = append(out, col)
	}
	return out, nil
}

// Rename changes column names in the header; row data passes through untouched,
// so it is allocation-free.
type Rename struct {
	pairs map[string]string
}

// NewRename parses "old=new,old2=new2" into a rename stage.
func NewRename(spec string) (*Rename, error) {
	pairs := map[string]string{}
	for _, p := range strings.Split(spec, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.IndexByte(p, '=')
		if eq <= 0 || eq == len(p)-1 {
			return nil, fmt.Errorf("rename %q: expected old=new", p)
		}
		old := strings.TrimSpace(p[:eq])
		nw := strings.TrimSpace(p[eq+1:])
		if old == "" || nw == "" {
			return nil, fmt.Errorf("rename %q: expected old=new", p)
		}
		pairs[old] = nw
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("rename: no pairs given")
	}
	return &Rename{pairs: pairs}, nil
}

func (r *Rename) Init(in Header) (Header, error) {
	for old := range r.pairs {
		if in.index(old) < 0 {
			return nil, columnNotFound(old, in)
		}
	}
	out := make(Header, len(in))
	for i, name := range in {
		if nw, ok := r.pairs[name]; ok {
			out[i] = nw
		} else {
			out[i] = name
		}
	}
	return out, nil
}

func (r *Rename) Process(in Row) (Row, bool, error) { return in, true, nil }

// OutSchema renames columns, preserving each column's type — a rename never
// changes the data, only its label, so the type rides along unchanged.
func (r *Rename) OutSchema(in Schema) (Schema, error) {
	for old := range r.pairs {
		if in.index(old) < 0 {
			return nil, columnNotFound(old, in.Header())
		}
	}
	out := make(Schema, len(in))
	for i, col := range in {
		if nw, ok := r.pairs[col.Name]; ok {
			out[i] = Column{Name: nw, Type: col.Type}
		} else {
			out[i] = col
		}
	}
	return out, nil
}
