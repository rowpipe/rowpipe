package rowpipe

import (
	"io"
	"testing"
)

// sliceSource is a non-Reader Source: it feeds rows straight from memory, the
// way the xlsx/parquet/jsonl submodule adapters feed decoded rows. It exists to
// pin that Run drives the Source interface, not the concrete *Reader.
type sliceSource struct {
	header Header
	rows   []Row
	i      int
}

func (s *sliceSource) Header() Header { return s.header }

func (s *sliceSource) Read() (Row, error) {
	if s.i >= len(s.rows) {
		return nil, io.EOF
	}
	r := s.rows[s.i]
	s.i++
	return r, nil
}

func TestRunDrivesArbitrarySource(t *testing.T) {
	src := &sliceSource{
		header: Header{"id", "status"},
		rows:   []Row{{"1", "active"}, {"2", "inactive"}, {"3", "active"}},
	}
	stages, err := Compile([]StageSpec{
		{Verb: VerbFilter, Arg: "status==active"},
		{Verb: VerbSelect, Arg: "id"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var sink collectSink
	stats, err := Run(src, stages, &sink)
	if err != nil {
		t.Fatal(err)
	}
	if stats.RowsIn != 3 || stats.RowsOut != 2 {
		t.Fatalf("stats = %+v, want in=3 out=2", stats)
	}
	want := [][]string{{"id"}, {"1"}, {"3"}}
	if len(sink.records) != len(want) {
		t.Fatalf("got %d records, want %d", len(sink.records), len(want))
	}
	for i, rec := range want {
		if len(sink.records[i]) != len(rec) || sink.records[i][0] != rec[0] {
			t.Errorf("record %d = %v, want %v", i, sink.records[i], rec)
		}
	}
}

// collectSink captures the streamed records (copying, since the source/stages
// reuse buffers) so the test can assert on them after the run.
type collectSink struct{ records [][]string }

func (s *collectSink) Write(rec []string) error {
	s.records = append(s.records, append([]string(nil), rec...))
	return nil
}

func (s *collectSink) Flush() error { return nil }
