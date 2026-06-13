package rowpipe

import (
	"fmt"
	"strings"
	"time"
)

// ColumnType is the physical type a column's string cells encode. It travels
// beside the rows (in a Schema), never inside them: Row stays []string, so the
// constant-memory drive loop and every Stage/Source/Sink that speaks text are
// untouched. Only a typed Sink (Parquet, Iceberg) consults the type, parsing each
// cell into it at encode time; everything upstream stays type-agnostic.
type ColumnType uint8

const (
	// ColString is the zero value: an untyped text column. A Source or Stage that
	// declares no type lands here, so the default end-to-end behavior is all-text —
	// exactly the pre-typing convention.
	ColString    ColumnType = iota
	ColInt                  // 64-bit signed integer
	ColFloat                // 64-bit IEEE-754 float
	ColBool                 // boolean
	ColTimestamp            // instant; canonical cell form is RFC3339 (see ParseTimestampMicros)
)

func (t ColumnType) String() string {
	switch t {
	case ColInt:
		return "int"
	case ColFloat:
		return "float"
	case ColBool:
		return "bool"
	case ColTimestamp:
		return "timestamp"
	default:
		return "string"
	}
}

// Column pairs a column name with its physical type.
type Column struct {
	Name string
	Type ColumnType
}

// Schema is the typed, ordered column set a Source yields or a pipeline emits,
// positionally aligned to the Header. It is optional metadata: code that needs
// only names uses Header; a typed Sink reads the types to encode real columns.
type Schema []Column

// Header projects the schema to its column names in order, so a Schema stands in
// wherever a Header is expected.
func (s Schema) Header() Header {
	h := make(Header, len(s))
	for i, c := range s {
		h[i] = c.Name
	}
	return h
}

func (s Schema) index(name string) int {
	for i, c := range s {
		if c.Name == name {
			return i
		}
	}
	return -1
}

// matches reports whether the schema's names equal h in order — the guard
// FinalSchema uses to fall back to all-text on any divergence.
func (s Schema) matches(h Header) bool {
	if len(s) != len(h) {
		return false
	}
	for i := range s {
		if s[i].Name != h[i] {
			return false
		}
	}
	return true
}

// AllString builds an untyped (all-ColString) schema over h — the default a
// Source that declares no types gets, preserving the all-text behavior.
func AllString(h Header) Schema {
	s := make(Schema, len(h))
	for i, name := range h {
		s[i] = Column{Name: name, Type: ColString}
	}
	return s
}

// SchemaSource is an optional Source extension: a Source that knows its column
// types (e.g. the Parquet reader) implements it so a typed Sink can preserve
// them. A Source that does not is treated as all-ColString.
type SchemaSource interface {
	Source
	Schema() Schema
}

// SchemaStage is an optional Stage extension: a Stage that reshapes or retypes
// columns (select/drop/rename/validate) implements it so the output schema stays
// correct through the pipeline. A Stage that leaves the column set unchanged
// (filter, dedupe) need not — FinalSchema threads the schema past it unchanged.
type SchemaStage interface {
	Stage
	// OutSchema maps the incoming typed schema to the one this stage emits. It must
	// agree, name-for-name, with the Header that Init returns for the same input.
	OutSchema(in Schema) (Schema, error)
}

// sourceSchema returns src's declared schema, or an all-text schema over its
// header when src declares none.
func sourceSchema(src Source) Schema {
	if ss, ok := src.(SchemaSource); ok {
		return ss.Schema()
	}
	return AllString(src.Header())
}

// FinalSchema derives the typed schema a pipeline emits, threading the source
// schema through each stage while running every stage's Init — the same one-time
// Init the row drive then relies on, so callers that drive the stages without a
// further Init get correctly-initialized stages, and callers that use Run (which
// Inits again) are unaffected since Init is idempotent.
//
// It is best-effort and fail-safe: if the threaded types ever disagree with the
// authoritative output header — a stage that reshapes columns without a correct
// OutSchema — it degrades to an all-text schema over that header. The typed path
// can therefore never emit a schema that misdescribes the actual columns; the
// worst case is the pre-typing all-string behavior.
func FinalSchema(src Source, stages []Stage) (Schema, error) {
	sc := sourceSchema(src)
	header := src.Header()
	for i, s := range stages {
		next, err := s.Init(header)
		if err != nil {
			return nil, fmt.Errorf("stage %d (%T): %w", i, s, err)
		}
		header = next
		if ss, ok := s.(SchemaStage); ok {
			sc, err = ss.OutSchema(sc)
			if err != nil {
				return nil, fmt.Errorf("stage %d (%T) schema: %w", i, s, err)
			}
		}
		// A non-SchemaStage leaves the column set unchanged; sc threads past it.
	}
	if !sc.matches(header) {
		return AllString(header), nil
	}
	return sc, nil
}

// ParseTimestampMicros parses a ColTimestamp cell — canonical form RFC3339 (with
// optional sub-second precision) — into microseconds since the Unix epoch, the
// physical encoding a typed Parquet/Iceberg sink writes. It accepts RFC3339Nano,
// and the value is UTC-normalized so a zoned input and its UTC equivalent encode
// identically.
func ParseTimestampMicros(cell string) (int64, error) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(cell))
	if err != nil {
		return 0, err
	}
	return t.UTC().UnixMicro(), nil
}
