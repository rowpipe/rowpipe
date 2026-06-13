package jsonl_test

import (
	"io"
	"strings"
	"testing"

	"github.com/rowpipe/rowpipe"
	"github.com/rowpipe/rowpipe/jsonl"
)

func collect(t *testing.T, s *jsonl.Source) (rowpipe.Header, [][]string) {
	t.Helper()
	h := s.Header()
	var rows [][]string
	for {
		r, err := s.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		rows = append(rows, append([]string(nil), r...))
	}
	return h, rows
}

func TestFlattenInfersHeaderAndNests(t *testing.T) {
	in := `{"id":1,"user":{"name":"ann","age":30}}
{"id":2,"user":{"name":"bob","age":31}}`
	s, err := jsonl.NewSource(strings.NewReader(in), jsonl.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, rows := collect(t, s)
	if got := strings.Join(h, ","); got != "id,user.name,user.age" {
		t.Fatalf("header = %q", got)
	}
	if len(rows) != 2 || rows[0][1] != "ann" || rows[1][2] != "31" {
		t.Fatalf("rows = %v", rows)
	}
}

func TestFlattenMissingKeyEmptyUnknownErrors(t *testing.T) {
	// Header locks from line 1 = {a,b}; line 2 omits b, which projects to empty.
	s, err := jsonl.NewSource(strings.NewReader("{\"a\":\"1\",\"b\":\"2\"}\n{\"a\":\"3\"}"), jsonl.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, rows := collect(t, s)
	if rows[1][1] != "" {
		t.Fatalf("missing key should project empty, got %q", rows[1][1])
	}

	// A key absent from the locked header is a row error.
	s2, err := jsonl.NewSource(strings.NewReader("{\"a\":\"1\"}\n{\"a\":\"1\",\"c\":\"x\"}"), jsonl.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Read(); err != nil {
		t.Fatalf("first row: %v", err)
	}
	if _, err := s2.Read(); err == nil {
		t.Fatal("expected unknown-key error")
	}
}

func TestColumnsPinAndDuplicateRejected(t *testing.T) {
	s, err := jsonl.NewSource(strings.NewReader(`{"b":"2","a":"1"}`), jsonl.Options{Columns: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	h, rows := collect(t, s)
	if h[0] != "a" || rows[0][0] != "1" || rows[0][1] != "2" {
		t.Fatalf("pin failed: h=%v rows=%v", h, rows)
	}
	if _, err := jsonl.NewSource(strings.NewReader("{}"), jsonl.Options{Columns: []string{"x", "x"}}); err == nil {
		t.Fatal("expected duplicate-column error")
	}
}

func TestEmptyInputErrors(t *testing.T) {
	if _, err := jsonl.NewSource(strings.NewReader("\n   \n"), jsonl.Options{}); err == nil {
		t.Fatal("expected empty-input error")
	}
}
