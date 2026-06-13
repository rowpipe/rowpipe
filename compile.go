package rowpipe

import "fmt"

// StageSpec is the wire/CLI form of a stage: a verb and its single argument
// string. Both the CLI and the HTTP API parse their input into an ordered slice
// of these and hand it to Compile, so the two front-ends share one grammar and
// behave identically.
type StageSpec struct {
	Verb string
	Arg  string
}

// Verbs recognized by Compile, for help text and front-end validation.
const (
	VerbFilter   = "filter"
	VerbDedupe   = "dedupe"
	VerbSelect   = "select"
	VerbDrop     = "drop"
	VerbRename   = "rename"
	VerbValidate = "validate"
)

// IsVerb reports whether s names a pipeline stage.
func IsVerb(s string) bool {
	switch s {
	case VerbFilter, VerbDedupe, VerbSelect, VerbDrop, VerbRename, VerbValidate:
		return true
	}
	return false
}

// Compile turns an ordered list of specs into runnable stages, preserving order.
func Compile(specs []StageSpec) ([]Stage, error) {
	stages := make([]Stage, 0, len(specs))
	for i, sp := range specs {
		s, err := compileOne(sp)
		if err != nil {
			return nil, fmt.Errorf("stage %d (%s): %w", i, sp.Verb, err)
		}
		stages = append(stages, s)
	}
	return stages, nil
}

func compileOne(sp StageSpec) (Stage, error) {
	switch sp.Verb {
	case VerbFilter:
		return NewFilter(sp.Arg)
	case VerbDedupe:
		return NewDedupe(splitCols(sp.Arg)), nil
	case VerbSelect:
		return NewSelect(splitCols(sp.Arg)), nil
	case VerbDrop:
		return NewDrop(splitCols(sp.Arg)), nil
	case VerbRename:
		return NewRename(sp.Arg)
	case VerbValidate:
		rules, err := ParseValidateSpec(sp.Arg)
		if err != nil {
			return nil, err
		}
		return NewValidate(rules), nil
	default:
		return nil, fmt.Errorf("unknown verb %q", sp.Verb)
	}
}
