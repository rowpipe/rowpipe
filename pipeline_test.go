package rowpipe

import (
	"strings"
	"testing"
)

const sample = `id,name,status,amount
1,Alice,active,100
2,Bob,inactive,250
3,Carol,active,90
2,Bob,inactive,250
4,Dave,active,1200
`

func runPipe(t *testing.T, input string, specs []StageSpec, ro ReaderOptions, wo WriterOptions) (string, Stats) {
	t.Helper()
	stages, err := Compile(specs)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	r, err := NewReader(strings.NewReader(input), ro)
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	var out strings.Builder
	st, err := Run(r, stages, NewWriter(&out, wo))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out.String(), st
}

func spec(verb, arg string) StageSpec { return StageSpec{Verb: verb, Arg: arg} }

func TestFilterKeepsMatching(t *testing.T) {
	got, st := runPipe(t, sample, []StageSpec{spec("filter", "status==active")}, ReaderOptions{}, WriterOptions{})
	want := "id,name,status,amount\n1,Alice,active,100\n3,Carol,active,90\n4,Dave,active,1200\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	if st.RowsIn != 5 || st.RowsOut != 3 || st.Dropped != 2 {
		t.Errorf("stats = %+v", st)
	}
}

func TestFilterNumericVsString(t *testing.T) {
	// >= is numeric because the threshold parses as a number: "90" < "1200"
	// numerically, but lexicographically "1200" < "90". Verify numeric wins.
	got, _ := runPipe(t, sample, []StageSpec{spec("filter", "amount>=100")}, ReaderOptions{}, WriterOptions{})
	if strings.Contains(got, "Carol") {
		t.Errorf("Carol (amount 90) should be excluded by amount>=100:\n%s", got)
	}
	for _, name := range []string{"Alice", "Bob", "Dave"} {
		if !strings.Contains(got, name) {
			t.Errorf("%s should pass amount>=100:\n%s", name, got)
		}
	}
}

func TestDedupeByColumn(t *testing.T) {
	got, st := runPipe(t, sample, []StageSpec{spec("dedupe", "id")}, ReaderOptions{}, WriterOptions{})
	if strings.Count(got, "Bob") != 1 {
		t.Errorf("Bob should appear once after dedupe id:\n%s", got)
	}
	if st.RowsOut != 4 {
		t.Errorf("RowsOut = %d, want 4", st.RowsOut)
	}
}

func TestDedupeWholeRow(t *testing.T) {
	// The two id=2 rows are identical, so whole-row dedupe also collapses them,
	// but it must NOT collapse rows that merely share one field.
	got, st := runPipe(t, sample, []StageSpec{spec("dedupe", "*")}, ReaderOptions{}, WriterOptions{})
	if st.RowsOut != 4 {
		t.Errorf("RowsOut = %d, want 4:\n%s", st.RowsOut, got)
	}
}

func TestDedupeMultiColumnNoFalseCollapse(t *testing.T) {
	in := "a,b\nx,1\nx,2\ny,1\n"
	got, st := runPipe(t, in, []StageSpec{spec("dedupe", "a,b")}, ReaderOptions{}, WriterOptions{})
	if st.RowsOut != 3 {
		t.Errorf("distinct (a,b) tuples must survive; RowsOut=%d:\n%s", st.RowsOut, got)
	}
}

func TestSelectReorders(t *testing.T) {
	got, _ := runPipe(t, sample, []StageSpec{spec("select", "name,id")}, ReaderOptions{}, WriterOptions{})
	if !strings.HasPrefix(got, "name,id\nAlice,1\n") {
		t.Errorf("select should project and reorder:\n%s", got)
	}
}

func TestDropColumns(t *testing.T) {
	got, _ := runPipe(t, sample, []StageSpec{spec("drop", "status,amount")}, ReaderOptions{}, WriterOptions{})
	if !strings.HasPrefix(got, "id,name\n1,Alice\n") {
		t.Errorf("drop should remove columns, keep order:\n%s", got)
	}
}

func TestRenameColumns(t *testing.T) {
	got, _ := runPipe(t, sample, []StageSpec{spec("rename", "amount=jpy,name=customer")}, ReaderOptions{}, WriterOptions{})
	if !strings.HasPrefix(got, "id,customer,status,jpy\n") {
		t.Errorf("rename should change only header:\n%s", got)
	}
	if !strings.Contains(got, "1,Alice,active,100\n") {
		t.Errorf("rename must leave row data untouched:\n%s", got)
	}
}

func TestPipelineOrderIsRespected(t *testing.T) {
	specs := []StageSpec{
		spec("filter", "status==active"),
		spec("dedupe", "id"),
		spec("select", "id,name"),
		spec("rename", "name=customer"),
	}
	got, st := runPipe(t, sample, specs, ReaderOptions{}, WriterOptions{})
	want := "id,customer\n1,Alice\n3,Carol\n4,Dave\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	if st.RowsOut != 3 {
		t.Errorf("RowsOut = %d, want 3", st.RowsOut)
	}
}

func TestNoHeaderSynthesizesNames(t *testing.T) {
	in := "1,Alice\n2,Bob\n"
	got, st := runPipe(t, in, []StageSpec{spec("select", "c1")}, ReaderOptions{NoHeader: true}, WriterOptions{})
	want := "c1\nAlice\nBob\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if st.RowsIn != 2 {
		t.Errorf("first line must be data under NoHeader; RowsIn=%d", st.RowsIn)
	}
}

func TestCustomDelimiterRoundTrip(t *testing.T) {
	in := "a;b\n1;2\n"
	got, _ := runPipe(t, in, []StageSpec{spec("select", "b")}, ReaderOptions{Delimiter: ';'}, WriterOptions{Delimiter: ';'})
	if got != "b\n2\n" {
		t.Errorf("got %q", got)
	}
}

func TestUnknownColumnIsError(t *testing.T) {
	stages, err := Compile([]StageSpec{spec("select", "nope")})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	r, err := NewReader(strings.NewReader(sample), ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if _, err := Run(r, stages, NewWriter(&out, WriterOptions{})); err == nil {
		t.Fatal("expected error for unknown column")
	}
}

func TestEmptyInputIsError(t *testing.T) {
	if _, err := NewReader(strings.NewReader(""), ReaderOptions{}); err == nil {
		t.Fatal("expected error on empty input")
	}
}

func TestCompileRejectsBadFilter(t *testing.T) {
	if _, err := Compile([]StageSpec{spec("filter", "garbage")}); err == nil {
		t.Fatal("expected compile error for malformed filter")
	}
}
