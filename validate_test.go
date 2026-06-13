package rowpipe

import (
	"strings"
	"testing"
)

func TestParseValidateSpec(t *testing.T) {
	rules, err := ParseValidateSpec(" id:int:required ; email:email:required ; amount:number:min=0 ; name:string ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rules) != 4 {
		t.Fatalf("got %d rules, want 4", len(rules))
	}
	if rules[0].Column != "id" || rules[0].Type != TypeInt || !rules[0].Required {
		t.Errorf("rule[0] = %+v", rules[0])
	}
	if rules[2].Min == nil || *rules[2].Min != 0 {
		t.Errorf("rule[2].Min = %v, want 0", rules[2].Min)
	}
	if rules[3].Type != TypeString || rules[3].Required {
		t.Errorf("rule[3] = %+v", rules[3])
	}
}

func TestParseValidateSpecErrors(t *testing.T) {
	for _, spec := range []string{"", "id", "id:bogus", "amount:number:min=abc", "id:int:huh"} {
		if _, err := ParseValidateSpec(spec); err == nil {
			t.Errorf("ParseValidateSpec(%q) = nil error, want error", spec)
		}
	}
}

func TestValidateQuarantinesInvalidRows(t *testing.T) {
	in := "id,email,amount\n" +
		"1,a@b.com,100\n" + // valid
		",bad,-5\n" + // id required, bad email, amount < 0  -> 3 errors
		"3,c@d.com,\n" + // amount optional + empty -> valid
		"x,e@f.com,5\n" // id not an integer -> 1 error

	rules, err := ParseValidateSpec("id:int:required;email:email:required;amount:number:min=0")
	if err != nil {
		t.Fatal(err)
	}
	v := NewValidate(rules)
	r, err := NewReader(strings.NewReader(in), ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	st, err := Run(r, []Stage{v}, NewWriter(&out, WriterOptions{}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	wantOut := "id,email,amount\n1,a@b.com,100\n3,c@d.com,\n"
	if out.String() != wantOut {
		t.Errorf("clean output:\n%q\nwant:\n%q", out.String(), wantOut)
	}
	if st.RowsIn != 4 || st.RowsOut != 2 || st.Dropped != 2 {
		t.Errorf("stats = %+v, want in=4 out=2 dropped=2", st)
	}

	rep := v.Report()
	if rep.Invalid != 2 || rep.Errors != 4 {
		t.Errorf("report invalid=%d errors=%d, want 2 and 4", rep.Invalid, rep.Errors)
	}
	if len(rep.Sample) != 4 {
		t.Fatalf("sample len = %d, want 4", len(rep.Sample))
	}
	first := rep.Sample[0]
	if first.Row != 2 || first.Column != "id" || first.Message != "required" {
		t.Errorf("sample[0] = %+v, want row 2 / id / required", first)
	}
	if last := rep.Sample[3]; last.Row != 4 || last.Column != "id" || last.Message != "not an integer" {
		t.Errorf("sample[3] = %+v, want row 4 / id / not an integer", last)
	}
}

func TestValidateOptionalEmptyPasses(t *testing.T) {
	in := "id,note\n1,\n2,hi\n"
	rules, _ := ParseValidateSpec("id:int:required;note:string")
	v := NewValidate(rules)
	r, _ := NewReader(strings.NewReader(in), ReaderOptions{})
	var out strings.Builder
	st, err := Run(r, []Stage{v}, NewWriter(&out, WriterOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	if st.RowsOut != 2 || v.Report().Invalid != 0 {
		t.Errorf("optional empty must pass; out=%d invalid=%d", st.RowsOut, v.Report().Invalid)
	}
}

func TestValidateInitUnknownColumn(t *testing.T) {
	stages, err := Compile([]StageSpec{spec("validate", "missing:int:required")})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	r, _ := NewReader(strings.NewReader("id,name\n1,Alice\n"), ReaderOptions{})
	if _, err := Run(r, stages, NewWriter(&strings.Builder{}, WriterOptions{})); err == nil {
		t.Fatal("expected error for validating a missing column")
	}
}

func TestCollectReportsAcrossStages(t *testing.T) {
	in := "id,email\n1,a@b.com\nx,bad\n2,bad2\n"
	stages, err := Compile([]StageSpec{
		spec("validate", "id:int:required"),
		spec("validate", "email:email:required"),
	})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := NewReader(strings.NewReader(in), ReaderOptions{})
	var out strings.Builder
	if _, err := Run(r, stages, NewWriter(&out, WriterOptions{})); err != nil {
		t.Fatal(err)
	}

	merged := CollectReports(stages)
	// row "x,bad" fails id at stage 1 and never reaches stage 2; row "2,bad2"
	// passes id but fails email at stage 2. Each invalid row counted once.
	if merged.Invalid != 2 || merged.Errors != 2 {
		t.Errorf("merged invalid=%d errors=%d, want 2 and 2", merged.Invalid, merged.Errors)
	}
	if out.String() != "id,email\n1,a@b.com\n" {
		t.Errorf("clean output:\n%q", out.String())
	}
	if !HasValidate(stages) {
		t.Error("HasValidate = false, want true")
	}
}

func TestReportSampleCapTruncates(t *testing.T) {
	var b strings.Builder
	b.WriteString("id\n")
	for range defaultSampleCap + 50 {
		b.WriteString("x\n") // every row fails id:int
	}
	rules, _ := ParseValidateSpec("id:int:required")
	v := NewValidate(rules)
	r, _ := NewReader(strings.NewReader(b.String()), ReaderOptions{})
	if _, err := Run(r, []Stage{v}, NewWriter(&strings.Builder{}, WriterOptions{})); err != nil {
		t.Fatal(err)
	}
	rep := v.Report()
	if rep.Errors != int64(defaultSampleCap+50) {
		t.Errorf("errors = %d, want %d (counts are exact)", rep.Errors, defaultSampleCap+50)
	}
	if len(rep.Sample) != defaultSampleCap || !rep.Truncated {
		t.Errorf("sample len=%d truncated=%v, want %d and true", len(rep.Sample), rep.Truncated, defaultSampleCap)
	}
}
