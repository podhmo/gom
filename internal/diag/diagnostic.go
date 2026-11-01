package diag

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Severity is the level of a diagnostic. Higher values are more
// severe; the zero value is invalid. The string forms are part of the
// message format (docs/spec.md section 12).
type Severity int

const (
	// SeverityNote is a remark that is neither a warning nor an error.
	SeverityNote Severity = iota + 1
	// SeverityWarning marks suspicious code that does not stop the build.
	SeverityWarning
	// SeverityError marks a diagnostic that fails the compilation.
	SeverityError
)

// String returns the severity token of the message format: "note",
// "warning" or "error". Invalid values render as "Severity(<n>)".
func (s Severity) String() string {
	switch s {
	case SeverityNote:
		return "note"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// Code is a stable diagnostic identifier such as "E0301"
// (docs/spec.md section 12). Codes never change meaning and are never
// reused, even after being retired. The registry of defined codes is
// maintained in codes.go.
type Code string

// Source identifies which tool produced a diagnostic. The zero value
// marks diagnostics produced by gom itself and is omitted from output.
type Source string

// SourceGo marks diagnostics produced by the Go toolchain (go build /
// go vet) and mapped back to .gom positions; it serializes as
// "source": "go" (docs/spec.md section 12, item 6).
const SourceGo Source = "go"

// Applicability classifies how safely a Fix can be applied
// (docs/spec.md section 12, item 5). The zero value is invalid.
type Applicability int

const (
	// ApplicabilitySafe means the fix can be applied as is; gom fix
	// applies only safe fixes automatically.
	ApplicabilitySafe Applicability = iota + 1
	// ApplicabilityPlaceholder means the fix inserts holes such as
	// `todo ()` that panic at run time.
	ApplicabilityPlaceholder
	// ApplicabilityUnsafe means applying the fix may change the meaning
	// of the program.
	ApplicabilityUnsafe
)

// String returns the applicability token of the message format:
// "safe", "placeholder" or "unsafe". Invalid values render as
// "Applicability(<n>)".
func (a Applicability) String() string {
	switch a {
	case ApplicabilitySafe:
		return "safe"
	case ApplicabilityPlaceholder:
		return "placeholder"
	case ApplicabilityUnsafe:
		return "unsafe"
	default:
		return fmt.Sprintf("Applicability(%d)", int(a))
	}
}

// Edit is a single replacement offered by a Fix: the byte range
// Edit.Span is replaced by Text. An insertion is a zero-width span and
// a deletion has an empty Text.
type Edit struct {
	Span Span
	Text string
}

// Fix is a named set of edits offered to repair a diagnostic, with an
// applicability classification (docs/spec.md section 12, item 5).
type Fix struct {
	Title         string
	Applicability Applicability
	Edits         []Edit
}

// Diagnostic is the internal representation of one compiler message —
// the source of truth from which the JSON Lines, text and LSP
// renderings are derived (docs/design.md section 2.3).
//
// Code is a stable identifier. Message is a single line of English and
// carries no detail worth parsing — structured detail goes into Data so
// consumers never parse the message text. Notes are short
// human-readable lines. Fixes are machine-applicable repair
// candidates. Span is the primary location; its zero value means the
// diagnostic has no source location.
//
// Diagnostics are values: deriving one from another (via At, Set,
// AddNote, AddFix, ...) clones the shared Data, Notes and Fixes so
// derived diagnostics stay independent.
type Diagnostic struct {
	Severity Severity
	Code     Code
	Message  string
	Span     Span
	Source   Source
	Data     map[string]any
	Notes    []string
	Fixes    []Fix
}

// New returns a Diagnostic with the given severity, code and message.
// The fluent methods At, From, Set, AddNote and AddFix fill in the
// remaining fields:
//
//	diag.Error("E0301", "non-exhaustive match on `Expr`").
//		At(diag.SpanIn("eval.gom", 41, 228)).
//		Set("scrutinee_type", "Expr").
//		AddNote("arms with guards do not count toward exhaustiveness")
func New(severity Severity, code Code, message string) Diagnostic {
	return Diagnostic{Severity: severity, Code: code, Message: message}
}

// Error returns a new error-severity Diagnostic.
func Error(code Code, message string) Diagnostic {
	return New(SeverityError, code, message)
}

// Warning returns a new warning-severity Diagnostic.
func Warning(code Code, message string) Diagnostic {
	return New(SeverityWarning, code, message)
}

// Note returns a new note-severity Diagnostic.
func Note(code Code, message string) Diagnostic {
	return New(SeverityNote, code, message)
}

// At sets the primary span of d and returns it.
func (d Diagnostic) At(sp Span) Diagnostic {
	d.Span = sp
	return d
}

// From sets the source of d (e.g. SourceGo for diagnostics mapped back
// from the Go toolchain) and returns it.
func (d Diagnostic) From(src Source) Diagnostic {
	d.Source = src
	return d
}

// Set adds a structured-data entry to d and returns it. The map is
// cloned before writing so that diagnostics derived from a common base
// do not share it (Diagnostic is otherwise a value type).
func (d Diagnostic) Set(key string, value any) Diagnostic {
	d.Data = maps.Clone(d.Data)
	if d.Data == nil {
		d.Data = make(map[string]any, 1)
	}
	d.Data[key] = value
	return d
}

// AddNote appends a human-readable note line to d and returns it. The
// slice is cloned before appending so that diagnostics derived from a
// common base do not share its backing array.
func (d Diagnostic) AddNote(note string) Diagnostic {
	d.Notes = append(slices.Clone(d.Notes), note)
	return d
}

// AddFix appends a repair candidate to d and returns it. The slice is
// cloned before appending so that diagnostics derived from a common
// base do not share its backing array.
func (d Diagnostic) AddFix(f Fix) Diagnostic {
	d.Fixes = append(slices.Clone(d.Fixes), f)
	return d
}

// Validate checks the invariants of the message format
// (docs/spec.md section 12): a known severity, a non-empty code, a
// non-empty single-line message, a valid span when one is present, and
// well-formed fixes. It reports the first violation found.
func (d Diagnostic) Validate() error {
	switch d.Severity {
	case SeverityNote, SeverityWarning, SeverityError:
	default:
		return fmt.Errorf("diag: invalid severity %d", int(d.Severity))
	}
	if d.Code == "" {
		return errors.New("diag: empty code")
	}
	if d.Message == "" {
		return errors.New("diag: empty message")
	}
	if strings.ContainsAny(d.Message, "\r\n") {
		return fmt.Errorf("diag: message must be a single line: %q", d.Message)
	}
	if !d.Span.IsZero() && !d.Span.Valid() {
		return fmt.Errorf("diag: invalid span {File:%q Start:%d End:%d}",
			d.Span.File, d.Span.Start, d.Span.End)
	}
	for i := range d.Fixes {
		f := &d.Fixes[i]
		if f.Title == "" {
			return fmt.Errorf("diag: fix %d: empty title", i)
		}
		switch f.Applicability {
		case ApplicabilitySafe, ApplicabilityPlaceholder, ApplicabilityUnsafe:
		default:
			return fmt.Errorf("diag: fix %d: invalid applicability %d",
				i, int(f.Applicability))
		}
		for j, e := range f.Edits {
			if !e.Span.Valid() {
				return fmt.Errorf(
					"diag: fix %d edit %d: invalid span {File:%q Start:%d End:%d}",
					i, j, e.Span.File, e.Span.Start, e.Span.End)
			}
		}
	}
	return nil
}

// List is a collection of diagnostics accumulated across compiler
// phases (docs/design.md section 2: phases 4 and 5 collect errors and
// report them together).
type List []Diagnostic

// Add appends d to the list.
func (l *List) Add(d Diagnostic) {
	*l = append(*l, d)
}

// HasError reports whether the list contains an error-severity
// diagnostic.
func (l List) HasError() bool {
	return l.MaxSeverity() >= SeverityError
}

// MaxSeverity returns the highest severity in the list, or zero if the
// list is empty.
func (l List) MaxSeverity() Severity {
	var max Severity
	for _, d := range l {
		if d.Severity > max {
			max = d.Severity
		}
	}
	return max
}

// Counts returns the numbers of error and warning diagnostics — the two
// counters reported by the "summary" message (docs/spec.md section 12).
func (l List) Counts() (errors, warnings int) {
	for _, d := range l {
		switch d.Severity {
		case SeverityError:
			errors++
		case SeverityWarning:
			warnings++
		}
	}
	return errors, warnings
}

// Sort orders the list deterministically by position (file, start,
// end), then by descending severity, then by code and message. It sorts
// in place; diagnostics without a location (zero Span) come first.
func (l List) Sort() {
	slices.SortStableFunc(l, func(a, b Diagnostic) int {
		return cmp.Or(
			strings.Compare(a.Span.File, b.Span.File),
			cmp.Compare(a.Span.Start, b.Span.Start),
			cmp.Compare(a.Span.End, b.Span.End),
			cmp.Compare(b.Severity, a.Severity), // errors first
			cmp.Compare(a.Code, b.Code),
			strings.Compare(a.Message, b.Message),
		)
	})
}
