package parquet

import (
	"bytes"
	"io"
	"testing"

	pq "github.com/parquet-go/parquet-go"
	"github.com/rowpipe/rowpipe"
)

type rec struct {
	Name string `parquet:"name"`
	Age  int64  `parquet:"age"`
}

type typedRow struct {
	Name  string  `parquet:"name"`
	Count int64   `parquet:"count"`
	Score float64 `parquet:"score"`
	OK    bool    `parquet:"ok"`
}

// TestSourceSchemaPreservesTypes proves the source no longer stringifies-and-
// forgets: it reports each column's physical type so a typed Sink downstream can
// preserve it. The cells still stream as strings (TestRoundTripStringifies).
func TestSourceSchemaPreservesTypes(t *testing.T) {
	var buf bytes.Buffer
	w := pq.NewGenericWriter[typedRow](&buf)
	if _, err := w.Write([]typedRow{{Name: "a", Count: 1, Score: 2.5, OK: true}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	src, err := NewSource(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	got := map[string]rowpipe.ColumnType{}
	for _, c := range src.Schema() {
		got[c.Name] = c.Type
	}
	want := map[string]rowpipe.ColumnType{
		"name":  rowpipe.ColString,
		"count": rowpipe.ColInt,
		"score": rowpipe.ColFloat,
		"ok":    rowpipe.ColBool,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("col %q type = %v, want %v", k, got[k], v)
		}
	}
}

func TestRoundTripStringifies(t *testing.T) {
	var buf bytes.Buffer
	w := pq.NewGenericWriter[rec](&buf)
	if _, err := w.Write([]rec{{"ann", 30}, {"bob", 31}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	data := buf.Bytes()
	s, err := NewSource(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if h := s.Header(); h[0] != "name" || h[1] != "age" {
		t.Fatalf("header = %v", h)
	}
	var rows [][]string
	for {
		r, err := s.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, append([]string(nil), r...))
	}
	if len(rows) != 2 || rows[0][0] != "ann" || rows[0][1] != "30" || rows[1][1] != "31" {
		t.Fatalf("rows = %v", rows)
	}
}
