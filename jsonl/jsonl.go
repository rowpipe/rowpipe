// Package jsonl adapts JSON-Lines input (one JSON object per line) to a
// rowpipe.Source. Each object is flattened one level — a nested object becomes
// dotted keys (a.b), deeper structures stay raw-JSON strings — and projected
// onto a header locked from the first line or an explicit column list.
package jsonl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/rowpipe/rowpipe"
)

// DefaultMaxLineBytes bounds a single JSONL line when Options.MaxLineBytes is 0;
// longer lines error rather than buffer without limit.
const DefaultMaxLineBytes = 16 << 20

// Options configure header handling and the per-line size cap.
type Options struct {
	// Columns pins the header explicitly; when empty the first line's flattened
	// keys lock the header (and that line is still emitted as the first row).
	Columns []string
	// MaxLineBytes caps one line; 0 uses DefaultMaxLineBytes.
	MaxLineBytes int
}

// Source streams JSONL in flatten mode. JSONL has no header line — every line
// is a record — so when Columns is empty the first line both locks the header
// and yields the first row (held in pending).
type Source struct {
	sc      *bufio.Scanner
	header  rowpipe.Header
	index   map[string]int
	out     rowpipe.Row
	pending rowpipe.Row
	hasPend bool
	line    int64
}

// NewSource reads r as JSONL and returns a streaming Source.
func NewSource(r io.Reader, opts Options) (*Source, error) {
	limit := opts.MaxLineBytes
	if limit <= 0 {
		limit = DefaultMaxLineBytes
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), limit)
	s := &Source{sc: sc}

	if len(opts.Columns) > 0 {
		if err := s.lockHeader(opts.Columns); err != nil {
			return nil, err
		}
		return s, nil
	}

	line, ok, err := s.nextLine()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("jsonl: empty input")
	}
	paths, values, err := flatten(line)
	if err != nil {
		return nil, fmt.Errorf("jsonl: line %d: %w", s.line, err)
	}
	if err := s.lockHeader(paths); err != nil {
		return nil, err
	}
	s.pending = rowpipe.Row(values)
	s.hasPend = true
	return s, nil
}

// lockHeader pins the header and its name→index map. It rejects duplicate
// column names — a repeated JSON key, or a literal dotted key that collides with
// a nested path (e.g. {"a.b":1,"a":{"b":2}}) — which would otherwise collapse a
// column while rows keep their original width.
func (s *Source) lockHeader(cols []string) error {
	if dup := firstDuplicate(cols); dup != "" {
		return fmt.Errorf("jsonl: flatten produced duplicate column %q; rename the key or pin an explicit column list", dup)
	}
	s.header = append(rowpipe.Header(nil), cols...)
	s.index = indexOf(s.header)
	s.out = make(rowpipe.Row, len(s.header))
	return nil
}

func (s *Source) Header() rowpipe.Header { return s.header }

func (s *Source) Read() (rowpipe.Row, error) {
	if s.hasPend {
		s.hasPend = false
		return s.pending, nil
	}
	line, ok, err := s.nextLine()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, io.EOF
	}
	paths, values, err := flatten(line)
	if err != nil {
		return nil, fmt.Errorf("jsonl: line %d: %w", s.line, err)
	}
	clearRow(s.out)
	for i, p := range paths {
		j, ok := s.index[p]
		if !ok {
			return nil, fmt.Errorf("jsonl: line %d: unknown key %q not in header", s.line, p)
		}
		s.out[j] = values[i]
	}
	return s.out, nil
}

// nextLine returns the next non-blank line, or ok=false at clean EOF. The slice
// aliases the scanner buffer and is valid only until the next call.
func (s *Source) nextLine() ([]byte, bool, error) {
	for s.sc.Scan() {
		s.line++
		b := bytes.TrimSpace(s.sc.Bytes())
		if len(b) == 0 {
			continue
		}
		return b, true, nil
	}
	if err := s.sc.Err(); err != nil {
		return nil, false, fmt.Errorf("jsonl: read: %w", err)
	}
	return nil, false, nil
}

// flatten walks a top-level object in insertion order, flattening one level
// (a.b) and stringifying scalars; deeper objects/arrays become raw-JSON strings.
func flatten(line []byte) (paths, values []string, err error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()
	if err := expectObjectStart(dec); err != nil {
		return nil, nil, err
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, nil, err
		}
		if isJSONObject(raw) {
			subPaths, subVals, err := flattenObject(raw)
			if err != nil {
				return nil, nil, err
			}
			for i := range subPaths {
				paths = append(paths, key+"."+subPaths[i])
				values = append(values, subVals[i])
			}
			continue
		}
		paths = append(paths, key)
		values = append(values, scalarString(raw))
	}
	return paths, values, nil
}

// flattenObject reads one level of a nested object, keeping insertion order;
// values that are themselves objects/arrays stay raw-JSON strings (depth-1).
func flattenObject(raw json.RawMessage) (keys, values []string, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := expectObjectStart(dec); err != nil {
		return nil, nil, err
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		var sub json.RawMessage
		if err := dec.Decode(&sub); err != nil {
			return nil, nil, err
		}
		keys = append(keys, keyTok.(string))
		values = append(values, scalarString(sub))
	}
	return keys, values, nil
}

func expectObjectStart(dec *json.Decoder) error {
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return errors.New("each line must be a JSON object")
	}
	return nil
}

func isJSONObject(raw json.RawMessage) bool {
	b := bytes.TrimSpace(raw)
	return len(b) > 0 && b[0] == '{'
}

// scalarString renders a JSON value as a cell: strings unquoted, numbers/bools
// verbatim, null empty, and nested objects/arrays as their raw JSON text.
func scalarString(raw json.RawMessage) string {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 {
		return ""
	}
	switch b[0] {
	case 'n':
		return ""
	case '"':
		var s string
		if err := json.Unmarshal(b, &s); err == nil {
			return s
		}
		return string(b)
	default:
		return string(b)
	}
}

func indexOf(h rowpipe.Header) map[string]int {
	m := make(map[string]int, len(h))
	for i, c := range h {
		m[c] = i
	}
	return m
}

// firstDuplicate returns the first column name that appears more than once, or
// "" when all names are unique.
func firstDuplicate(cols []string) string {
	seen := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		if _, ok := seen[c]; ok {
			return c
		}
		seen[c] = struct{}{}
	}
	return ""
}

// clearRow resets a reused output row to all-empty before the next projection.
func clearRow(r rowpipe.Row) {
	for i := range r {
		r[i] = ""
	}
}
