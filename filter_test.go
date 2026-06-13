package rowpipe

import "testing"

func TestParseFilterExpr(t *testing.T) {
	cases := []struct {
		expr    string
		col     string
		op      filterOp
		val     string
		wantErr bool
	}{
		{expr: "status==active", col: "status", op: opEq, val: "active"},
		{expr: "status == active", col: "status", op: opEq, val: "active"},
		{expr: "amount >= 100", col: "amount", op: opGe, val: "100"},
		{expr: "amount>=100", col: "amount", op: opGe, val: "100"},
		{expr: "tier != A", col: "tier", op: opNe, val: "A"},
		{expr: "name ~ ^A", col: "name", op: opRegex, val: "^A"},
		{expr: "name !~ ^A", col: "name", op: opNotRegex, val: "^A"},
		{expr: "note contains urgent", col: "note", op: opContains, val: "urgent"},
		{expr: "email notempty", col: "email", op: opNotEmpty, val: ""},
		{expr: "email empty", col: "email", op: opEmpty, val: ""},
		{expr: "garbage", wantErr: true},
		{expr: "col badop x", wantErr: true},
	}
	for _, c := range cases {
		col, op, val, err := parseFilterExpr(c.expr)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: expected error", c.expr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.expr, err)
			continue
		}
		if col != c.col || op != c.op || val != c.val {
			t.Errorf("%q: got (%q,%d,%q) want (%q,%d,%q)", c.expr, col, op, val, c.col, c.op, c.val)
		}
	}
}

func TestFilterMatch(t *testing.T) {
	cases := []struct {
		expr string
		cell string
		want bool
	}{
		{"x==a", "a", true},
		{"x==a", "b", false},
		{"x!=a", "b", true},
		{"x>5", "10", true},
		{"x>5", "3", false},
		{"x>5", "5", false},
		{"x>=5", "5", true},
		{"x<5", "3", true},
		{"x<=5", "5", true},
		{"x contains oo", "foobar", true},
		{"x startswith foo", "foobar", true},
		{"x endswith bar", "foobar", true},
		{"x ~ ^[0-9]+$", "12345", true},
		{"x ~ ^[0-9]+$", "12a45", false},
		{"x empty", "", true},
		{"x notempty", "y", true},
		// non-numeric cell under a numeric op falls back to string order
		{"x>5", "abc", true}, // "abc" > "5" lexicographically
	}
	for _, c := range cases {
		f, err := NewFilter(c.expr)
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		if got := f.match(c.cell); got != c.want {
			t.Errorf("%q match %q = %v, want %v", c.expr, c.cell, got, c.want)
		}
	}
}
