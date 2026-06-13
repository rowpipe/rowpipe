package rowpipe

import (
	"io"
	"testing"
	"time"
)

// typedSource is a SchemaSource over a fixed schema and rows, for exercising the
// schema-threading independent of any concrete reader.
type typedSource struct {
	schema Schema
	rows   []Row
	i      int
}

func (s *typedSource) Header() Header { return s.schema.Header() }
func (s *typedSource) Schema() Schema { return s.schema }
func (s *typedSource) Read() (Row, error) {
	if s.i >= len(s.rows) {
		return nil, io.EOF
	}
	r := s.rows[s.i]
	s.i++
	return r, nil
}

func types(s Schema) map[string]ColumnType {
	m := make(map[string]ColumnType, len(s))
	for _, c := range s {
		m[c.Name] = c.Type
	}
	return m
}

func TestFinalSchemaCarriesSourceTypesThroughReshaping(t *testing.T) {
	src := &typedSource{schema: Schema{
		{Name: "id", Type: ColInt},
		{Name: "name", Type: ColString},
		{Name: "amt", Type: ColFloat},
	}}
	stages, err := Compile([]StageSpec{
		{Verb: VerbSelect, Arg: "name,amt"},
		{Verb: VerbRename, Arg: "amt=amount"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := FinalSchema(src, stages)
	if err != nil {
		t.Fatal(err)
	}
	if h := got.Header(); len(h) != 2 || h[0] != "name" || h[1] != "amount" {
		t.Fatalf("header = %v", h)
	}
	ty := types(got)
	if ty["name"] != ColString || ty["amount"] != ColFloat {
		t.Fatalf("types = %v, want name:string amount:float", ty)
	}
}

func TestFinalSchemaValidatePromotes(t *testing.T) {
	// A plain (untyped) source: types come only from the validate rules.
	src := &typedSource{schema: AllString(Header{"qty", "price", "email"})}
	stages, err := Compile([]StageSpec{
		{Verb: VerbValidate, Arg: "qty:int; price:number; email:email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := FinalSchema(src, stages)
	if err != nil {
		t.Fatal(err)
	}
	ty := types(got)
	if ty["qty"] != ColInt || ty["price"] != ColFloat || ty["email"] != ColString {
		t.Fatalf("types = %v, want qty:int price:float email:string", ty)
	}
}

func TestFinalSchemaDropCarriesType(t *testing.T) {
	src := &typedSource{schema: Schema{
		{Name: "a", Type: ColInt},
		{Name: "b", Type: ColString},
		{Name: "c", Type: ColBool},
	}}
	stages, _ := Compile([]StageSpec{{Verb: VerbDrop, Arg: "b"}})
	got, err := FinalSchema(src, stages)
	if err != nil {
		t.Fatal(err)
	}
	if h := got.Header(); len(h) != 2 || h[0] != "a" || h[1] != "c" {
		t.Fatalf("header = %v", h)
	}
	ty := types(got)
	if ty["a"] != ColInt || ty["c"] != ColBool {
		t.Fatalf("types = %v", ty)
	}
}

func TestFinalSchemaUntypedSourceIsAllString(t *testing.T) {
	// plainSource does NOT implement SchemaSource → all-text default.
	src := plainSource{header: Header{"x", "y"}}
	got, err := FinalSchema(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got {
		if c.Type != ColString {
			t.Fatalf("col %q = %v, want string", c.Name, c.Type)
		}
	}
}

// addCol changes the header (adds a column) but does NOT implement SchemaStage,
// so the threaded schema diverges from the real header — FinalSchema must detect
// that and degrade to all-text rather than emit a mismatched typed schema.
type addCol struct{ out Row }

func (a *addCol) Init(in Header) (Header, error) {
	return append(append(Header(nil), in...), "extra"), nil
}
func (a *addCol) Process(in Row) (Row, bool, error) {
	a.out = append(a.out[:0], in...)
	a.out = append(a.out, "x")
	return a.out, true, nil
}

func TestFinalSchemaDegradesOnHeaderMismatch(t *testing.T) {
	src := &typedSource{schema: Schema{{Name: "a", Type: ColInt}}}
	got, err := FinalSchema(src, []Stage{&addCol{}})
	if err != nil {
		t.Fatal(err)
	}
	if h := got.Header(); len(h) != 2 || h[0] != "a" || h[1] != "extra" {
		t.Fatalf("header = %v", h)
	}
	for _, c := range got {
		if c.Type != ColString {
			t.Fatalf("degraded schema must be all-string, got %q=%v", c.Name, c.Type)
		}
	}
}

func TestParseTimestampMicrosRoundTrip(t *testing.T) {
	want := time.Date(2026, 6, 13, 1, 2, 3, 456000000, time.UTC)
	us, err := ParseTimestampMicros(want.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	if got := time.UnixMicro(us).UTC(); !got.Equal(want) {
		t.Fatalf("round-trip = %s, want %s", got, want)
	}
	// A zoned input normalizes to the same UTC instant.
	zoned := "2026-06-13T10:02:03.456+09:00"
	zus, err := ParseTimestampMicros(zoned)
	if err != nil {
		t.Fatal(err)
	}
	if zus != us {
		t.Fatalf("zoned micros %d != utc micros %d", zus, us)
	}
	if _, err := ParseTimestampMicros("not-a-time"); err == nil {
		t.Fatal("expected error on malformed timestamp")
	}
}

type plainSource struct {
	header Header
	rows   []Row
	i      int
}

func (s plainSource) Header() Header { return s.header }
func (s plainSource) Read() (Row, error) {
	if s.i >= len(s.rows) {
		return nil, io.EOF
	}
	return s.rows[s.i], nil
}
