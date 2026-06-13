package rowpipe

import "strings"

// keySep joins multi-column dedup keys. U+001F (unit separator) cannot appear in
// a parsed CSV field, so distinct column tuples can never collide into one key.
const keySep = "\x1f"

// Dedupe drops repeat records, keeping the first occurrence. With no columns it
// keys on the whole row; otherwise on the named columns.
//
// Unlike the other stages, Dedupe is stateful: it remembers every distinct key
// it has emitted, so its memory grows with the number of distinct keys (not the
// row count). Keys are stored exactly — never hashed — so two different rows can
// never be silently collapsed, which matters for financial/HR data.
type Dedupe struct {
	cols []string
	idxs []int
	seen map[string]struct{}
	buf  []byte
}

// NewDedupe builds a dedup stage. Empty cols (or a single "*") dedups whole rows.
func NewDedupe(cols []string) *Dedupe {
	if len(cols) == 1 && cols[0] == "*" {
		cols = nil
	}
	return &Dedupe{cols: cols, seen: make(map[string]struct{})}
}

func (d *Dedupe) Init(in Header) (Header, error) {
	d.idxs = d.idxs[:0]
	for _, c := range d.cols {
		i := in.index(c)
		if i < 0 {
			return nil, columnNotFound(c, in)
		}
		d.idxs = append(d.idxs, i)
	}
	return in, nil
}

func (d *Dedupe) Process(in Row) (Row, bool, error) {
	key := d.key(in)
	if _, dup := d.seen[key]; dup {
		return in, false, nil
	}
	d.seen[key] = struct{}{}
	return in, true, nil
}

// key builds the lookup key in a reusable byte buffer, then returns it as a
// string. The map insert copies the bytes, so reusing buf across rows is safe.
func (d *Dedupe) key(in Row) string {
	d.buf = d.buf[:0]
	if len(d.idxs) == 0 {
		for i, f := range in {
			if i > 0 {
				d.buf = append(d.buf, keySep...)
			}
			d.buf = append(d.buf, f...)
		}
	} else {
		for n, i := range d.idxs {
			if n > 0 {
				d.buf = append(d.buf, keySep...)
			}
			if i < len(in) {
				d.buf = append(d.buf, in[i]...)
			}
		}
	}
	return string(d.buf)
}

// splitCols parses a comma-separated column list, trimming spaces and dropping
// empties.
func splitCols(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
