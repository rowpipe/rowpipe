package rowpipe

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type filterOp int

const (
	opEq filterOp = iota
	opNe
	opGt
	opLt
	opGe
	opLe
	opContains
	opStartsWith
	opEndsWith
	opRegex
	opNotRegex
	opEmpty
	opNotEmpty
)

// symbolicOps are scanned longest-first so ">=" wins over ">".
var symbolicOps = []struct {
	tok string
	op  filterOp
}{
	{">=", opGe}, {"<=", opLe}, {"==", opEq}, {"!=", opNe},
	{"!~", opNotRegex}, {"~", opRegex}, {">", opGt}, {"<", opLt},
}

var wordOps = map[string]filterOp{
	"contains":   opContains,
	"startswith": opStartsWith,
	"endswith":   opEndsWith,
	"empty":      opEmpty,
	"notempty":   opNotEmpty,
}

// Filter keeps only rows whose named column satisfies a predicate. Multiple
// filters in a pipeline are independent stages, so they compose as AND.
type Filter struct {
	col   string
	op    filterOp
	val   string
	num   float64
	isNum bool
	re    *regexp.Regexp
	idx   int
}

// NewFilter parses a predicate of the form "COL OP VALUE", e.g. "status==active",
// "amount >= 100", "name ~ ^A", or the unary "notes empty" / "email notempty".
// Operators may be written with or without surrounding spaces.
func NewFilter(expr string) (*Filter, error) {
	col, op, val, err := parseFilterExpr(expr)
	if err != nil {
		return nil, err
	}
	f := &Filter{col: col, op: op, val: val}
	switch op {
	case opRegex, opNotRegex:
		re, err := regexp.Compile(val)
		if err != nil {
			return nil, fmt.Errorf("filter %q: invalid regex: %w", expr, err)
		}
		f.re = re
	case opGt, opLt, opGe, opLe:
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			f.num, f.isNum = n, true
		}
	}
	return f, nil
}

func parseFilterExpr(expr string) (col string, op filterOp, val string, err error) {
	for _, s := range symbolicOps {
		if i := strings.Index(expr, s.tok); i > 0 {
			col = strings.TrimSpace(expr[:i])
			val = strings.TrimSpace(expr[i+len(s.tok):])
			return col, s.op, val, nil
		}
	}
	fields := strings.Fields(expr)
	switch len(fields) {
	case 2:
		if o, ok := wordOps[fields[1]]; ok && (o == opEmpty || o == opNotEmpty) {
			return fields[0], o, "", nil
		}
	case 3:
		if o, ok := wordOps[fields[1]]; ok {
			return fields[0], o, fields[2], nil
		}
	}
	return "", 0, "", fmt.Errorf("filter %q: expected COL OP VALUE", expr)
}

func (f *Filter) Init(in Header) (Header, error) {
	f.idx = in.index(f.col)
	if f.idx < 0 {
		return nil, columnNotFound(f.col, in)
	}
	return in, nil
}

func (f *Filter) Process(in Row) (Row, bool, error) {
	if f.idx >= len(in) {
		return in, false, nil
	}
	return in, f.match(in[f.idx]), nil
}

func (f *Filter) match(cell string) bool {
	switch f.op {
	case opEq:
		return cell == f.val
	case opNe:
		return cell != f.val
	case opContains:
		return strings.Contains(cell, f.val)
	case opStartsWith:
		return strings.HasPrefix(cell, f.val)
	case opEndsWith:
		return strings.HasSuffix(cell, f.val)
	case opEmpty:
		return cell == ""
	case opNotEmpty:
		return cell != ""
	case opRegex:
		return f.re.MatchString(cell)
	case opNotRegex:
		return !f.re.MatchString(cell)
	case opGt, opLt, opGe, opLe:
		return f.compare(cell)
	}
	return false
}

// compare evaluates ordered operators numerically when both the threshold and
// the cell parse as numbers, and falls back to lexicographic order otherwise —
// so "amount > 100" is numeric but "tier > B" is string-ordered.
func (f *Filter) compare(cell string) bool {
	if f.isNum {
		if n, err := strconv.ParseFloat(cell, 64); err == nil {
			return cmpOrder(f.op, n > f.num, n == f.num)
		}
	}
	return cmpOrder(f.op, cell > f.val, cell == f.val)
}

func cmpOrder(op filterOp, gt, eq bool) bool {
	switch op {
	case opGt:
		return gt
	case opLt:
		return !gt && !eq
	case opGe:
		return gt || eq
	case opLe:
		return !gt
	}
	return false
}
