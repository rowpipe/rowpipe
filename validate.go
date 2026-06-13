package rowpipe

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FieldType is the value domain a column is checked against.
type FieldType string

const (
	TypeString FieldType = "string"
	TypeInt    FieldType = "int"
	TypeNumber FieldType = "number"
	TypeEmail  FieldType = "email"
)

// Rule constrains one column. A blank cell fails only when Required; otherwise it
// is skipped, so optional columns may be empty but must be well-formed when set.
type Rule struct {
	Column   string
	Type     FieldType
	Required bool
	Min      *float64
	Max      *float64
}

// RowError is one failing cell: the 1-based position of the row in the stream
// reaching the validator, the column, the offending value, and why it failed.
type RowError struct {
	Row     int64  `json:"row"`
	Column  string `json:"column"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

const defaultSampleCap = 1000

// Report accumulates validation outcomes. Counts are exact; the stored RowError
// sample is capped so memory stays bounded no matter how many rows fail.
type Report struct {
	Invalid   int64      // rows with at least one failing cell
	Errors    int64      // total failing cells
	Sample    []RowError // first cap errors, for display
	Truncated bool       // true when more errors occurred than Sample holds
	cap       int
}

func (r *Report) add(e RowError) {
	if r.cap == 0 {
		r.cap = defaultSampleCap
	}
	if len(r.Sample) < r.cap {
		r.Sample = append(r.Sample, e)
		return
	}
	r.Truncated = true
}

// Validate checks named columns against a schema and quarantines any row with a
// failing cell (Process returns keep=false), so a downstream Writer receives only
// clean rows. Every failure is tallied in the Report, readable after the run.
type Validate struct {
	rules  []Rule
	idxs   []int
	report *Report
	row    int64
}

// NewValidate builds a validation stage from rules.
func NewValidate(rules []Rule) *Validate {
	return &Validate{rules: rules, report: &Report{cap: defaultSampleCap}}
}

// Report returns the live report; read it after Run completes.
func (v *Validate) Report() *Report { return v.report }

func (v *Validate) Init(in Header) (Header, error) {
	if len(v.rules) == 0 {
		return nil, fmt.Errorf("validate: no rules given")
	}
	v.idxs = make([]int, len(v.rules))
	for i, r := range v.rules {
		j := in.index(r.Column)
		if j < 0 {
			return nil, columnNotFound(r.Column, in)
		}
		v.idxs[i] = j
	}
	return in, nil
}

func (v *Validate) Process(in Row) (Row, bool, error) {
	v.row++
	bad := false
	for i, r := range v.rules {
		var val string
		if j := v.idxs[i]; j < len(in) {
			val = in[j]
		}
		if msg := r.check(val); msg != "" {
			bad = true
			v.report.Errors++
			v.report.add(RowError{Row: v.row, Column: r.Column, Value: val, Message: msg})
		}
	}
	if bad {
		v.report.Invalid++
		return in, false, nil
	}
	return in, true, nil
}

// OutSchema promotes each validated column to the physical type its rule proves:
// int → ColInt, number → ColFloat. Because Validate quarantines every row whose
// cell fails its rule, a surviving cell in a promoted column is guaranteed to
// parse (an empty optional cell becomes null at the sink), so a typed Sink can
// trust the type without re-checking. email and string rules pin the column to
// text. Columns without a rule keep whatever type they arrived with.
func (v *Validate) OutSchema(in Schema) (Schema, error) {
	if len(v.rules) == 0 {
		return nil, fmt.Errorf("validate: no rules given")
	}
	out := append(Schema(nil), in...)
	for _, r := range v.rules {
		j := out.index(r.Column)
		if j < 0 {
			return nil, columnNotFound(r.Column, in.Header())
		}
		switch r.Type {
		case TypeInt:
			out[j].Type = ColInt
		case TypeNumber:
			out[j].Type = ColFloat
		case TypeEmail, TypeString:
			out[j].Type = ColString
		}
	}
	return out, nil
}

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// check returns "" when value satisfies the rule, else a short reason.
func (r Rule) check(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		if r.Required {
			return "required"
		}
		return ""
	}
	switch r.Type {
	case TypeInt:
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return "not an integer"
		}
	case TypeNumber:
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return "not a number"
		}
	case TypeEmail:
		if !emailRe.MatchString(v) {
			return "invalid email"
		}
	case TypeString, "":
	}
	if (r.Type == TypeInt || r.Type == TypeNumber) && (r.Min != nil || r.Max != nil) {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			if r.Min != nil && n < *r.Min {
				return fmt.Sprintf("must be ≥ %v", *r.Min)
			}
			if r.Max != nil && n > *r.Max {
				return fmt.Sprintf("must be ≤ %v", *r.Max)
			}
		}
	}
	return ""
}

// ParseValidateSpec parses a schema of the form
//
//	COL:TYPE[:required][:min=N][:max=N]; COL:TYPE...
//
// e.g. "id:int:required; email:email:required; amount:number:min=0". TYPE is one
// of string, int, number, email. Rules are separated by ';'.
func ParseValidateSpec(spec string) ([]Rule, error) {
	var rules []Rule
	for raw := range strings.SplitSeq(spec, ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.Split(raw, ":")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("validate %q: expected COL:TYPE[:opt...]", raw)
		}
		r := Rule{Column: parts[0], Type: FieldType(parts[1])}
		switch r.Type {
		case TypeString, TypeInt, TypeNumber, TypeEmail:
		default:
			return nil, fmt.Errorf("validate %q: unknown type %q", raw, parts[1])
		}
		for _, opt := range parts[2:] {
			switch {
			case opt == "":
			case opt == "required":
				r.Required = true
			case strings.HasPrefix(opt, "min="):
				n, err := strconv.ParseFloat(strings.TrimPrefix(opt, "min="), 64)
				if err != nil {
					return nil, fmt.Errorf("validate %q: bad min: %w", raw, err)
				}
				r.Min = &n
			case strings.HasPrefix(opt, "max="):
				n, err := strconv.ParseFloat(strings.TrimPrefix(opt, "max="), 64)
				if err != nil {
					return nil, fmt.Errorf("validate %q: bad max: %w", raw, err)
				}
				r.Max = &n
			default:
				return nil, fmt.Errorf("validate %q: unknown option %q", raw, opt)
			}
		}
		rules = append(rules, r)
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("validate: no rules given")
	}
	return rules, nil
}

// CollectReports merges the reports of every Validate stage in the pipeline.
// Because a row dropped by an earlier validator never reaches a later one,
// summing Invalid and Errors across stages does not double-count.
func CollectReports(stages []Stage) Report {
	merged := Report{cap: defaultSampleCap}
	for _, s := range stages {
		v, ok := s.(*Validate)
		if !ok {
			continue
		}
		merged.Invalid += v.report.Invalid
		merged.Errors += v.report.Errors
		for _, e := range v.report.Sample {
			merged.add(e)
		}
		if v.report.Truncated {
			merged.Truncated = true
		}
	}
	return merged
}

// HasValidate reports whether the pipeline contains a validation stage.
func HasValidate(stages []Stage) bool {
	for _, s := range stages {
		if _, ok := s.(*Validate); ok {
			return true
		}
	}
	return false
}
