package xlsx_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/rowpipe/rowpipe/xlsx"
	"github.com/xuri/excelize/v2"
)

func build(t *testing.T, fn func(f *excelize.File)) []byte {
	t.Helper()
	f := excelize.NewFile()
	fn(f)
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readAll(t *testing.T, s *xlsx.Source) [][]string {
	t.Helper()
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
	return rows
}

func TestBasicAndRaggedPadded(t *testing.T) {
	b := build(t, func(f *excelize.File) {
		_ = f.SetSheetRow("Sheet1", "A1", &[]any{"name", "age"})
		_ = f.SetSheetRow("Sheet1", "A2", &[]any{"ann", 30})
		_ = f.SetCellValue("Sheet1", "A3", "bob") // B3 omitted → ragged row
	})
	s, err := xlsx.OpenReader(bytes.NewReader(b), xlsx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if h := s.Header(); h[0] != "name" || h[1] != "age" {
		t.Fatalf("header = %v", h)
	}
	rows := readAll(t, s)
	if len(rows) != 2 || rows[0][0] != "ann" || rows[0][1] != "30" || rows[1][1] != "" {
		t.Fatalf("rows = %v", rows)
	}
}

func TestNoHeaderSynthesizes(t *testing.T) {
	b := build(t, func(f *excelize.File) {
		_ = f.SetSheetRow("Sheet1", "A1", &[]any{"x", "y"})
	})
	s, err := xlsx.OpenReader(bytes.NewReader(b), xlsx.Options{NoHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if h := s.Header(); h[0] != "c0" || h[1] != "c1" {
		t.Fatalf("header = %v", h)
	}
	if rows := readAll(t, s); len(rows) != 1 || rows[0][0] != "x" {
		t.Fatalf("rows = %v", rows)
	}
}
