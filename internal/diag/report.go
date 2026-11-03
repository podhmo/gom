package diag

import (
	"slices"
)

// Kind identifies the type of a message object on the wire — the value
// of its "kind" field (docs/spec.md section 12). Every message stream
// ends with KindSummary; which other kinds a command emits depends on
// the command (spec section 12, table).
type Kind string

const (
	// KindDiagnostic is a compiler diagnostic: error, warning or note.
	KindDiagnostic Kind = "diagnostic"
	// KindSummary is the aggregate counts object that always closes a
	// message stream.
	KindSummary Kind = "summary"
	// KindTest reports one test's outcome (gom test).
	KindTest Kind = "test"
	// KindUnformatted reports a file that needs formatting
	// (gom fmt --check).
	KindUnformatted Kind = "unformatted"
	// KindApplied reports a fix edit applied by gom fix.
	KindApplied Kind = "applied"
	// KindExplanation carries an error-code explanation (gom explain).
	KindExplanation Kind = "explanation"
)

// Process exit codes of the gom commands (docs/spec.md section 12,
// item 1).
const (
	// ExitSuccess means the run completed without error-severity
	// diagnostics. Warnings do not affect it.
	ExitSuccess = 0
	// ExitError means error-severity diagnostics were emitted.
	ExitError = 1
	// ExitInternal means the compiler itself failed: a panic, an
	// internal error, or an output failure. It is assigned by the
	// driver, never derived from diagnostics.
	ExitInternal = 2
)

// Report is the unit of compiler output a gom command renders into
// messages (docs/spec.md section 12): the diagnostics the run produced
// plus the file sets reported by the trailing "summary" object.
//
// The emitters (WriteJSONLines, WriteText) render a copy of
// Diagnostics sorted by List.Sort (stable). Diagnostics that tie on
// every compared field keep their input order, so identical inputs
// always produce identical output, but ties are not canonicalized
// beyond that.
type Report struct {
	// Diagnostics emitted during the run, in any order.
	Diagnostics []Diagnostic
	// Tests are the test results the run produced — nil for
	// commands that do not run tests; a non-nil empty slice (gom
	// test that found no test) still reports the "tests" counts in
	// the summary. Order is preserved on the wire.
	Tests []TestResult
	// Files are the .gom source files the command processed.
	Files []string
	// Outputs are the files the command produced — generated .go
	// files for gom build; empty for gom check.
	Outputs []string
}

// ExitCode returns the process exit code for the run: ExitError when
// the report holds error-severity diagnostics or a failing test,
// ExitSuccess otherwise (warnings and skipped tests still succeed).
// ExitInternal is reserved for the driver and never returned — emit
// failures and compiler crashes are reported through it, not through
// the diagnostics.
func (r Report) ExitCode() int {
	if List(r.Diagnostics).HasError() || r.TestsFailed() {
		return ExitError
	}
	return ExitSuccess
}

// Summary builds the trailing "summary" object of the run's message
// stream (docs/spec.md section 12). OK reports whether the run
// succeeded — true exactly when no error-severity diagnostic was
// emitted and no test failed; warnings and skipped tests do not fail
// a run. Files and Outputs are deduplicated; Files is sorted, Outputs
// keeps the caller's order.
func (r Report) Summary() Summary {
	errors, warnings := List(r.Diagnostics).Counts()
	s := Summary{
		OK:       errors == 0 && !r.TestsFailed(),
		Errors:   errors,
		Warnings: warnings,
		Files:    sortedUnique(r.Files),
		Outputs:  unique(r.Outputs),
	}
	if r.Tests != nil {
		run, failed, skipped := r.testStats()
		s.Tests = &TestCounts{Run: run, Failed: failed, Skipped: skipped}
	}
	return s
}

// orderedDiagnostics returns a copy of the report's diagnostics in
// output order (List.Sort): by position, then descending severity,
// then code and message; positionless diagnostics come first.
func (r Report) orderedDiagnostics() []Diagnostic {
	ds := slices.Clone(r.Diagnostics)
	List(ds).Sort()
	return ds
}

// Summary is the aggregate object that closes every message stream
// (docs/spec.md section 12). Marshaling it produces the wire object
// including its "kind": "summary" field.
type Summary struct {
	// OK is true when the run produced no error-severity diagnostic.
	OK bool
	// Errors and Warnings count the emitted diagnostics by severity;
	// notes are counted in neither.
	Errors   int
	Warnings int
	// Files are the .gom sources the command processed.
	Files []string
	// Outputs are the files the command produced.
	Outputs []string
	// Tests counts the run's test results — nil for commands that
	// did not run tests, in which case "tests" is absent from the
	// wire object.
	Tests *TestCounts
}

// TestCounts is the summary's per-test tally (gom test).
type TestCounts struct {
	// Run is the number of tests go test reported.
	Run int `json:"run"`
	// Failed is the number that neither passed nor skipped.
	Failed int `json:"failed"`
	// Skipped is the number go test reported as skipped.
	Skipped int `json:"skipped"`
}

// MarshalJSON renders s as the wire object
// {"kind":"summary","ok":…,"errors":…,"warnings":…,"files":[…],"outputs":[…]},
// plus "tests" when the run reported test results. Files and Outputs
// marshal as empty arrays, never null.
func (s Summary) MarshalJSON() ([]byte, error) {
	type wire struct {
		Kind     string      `json:"kind"`
		OK       bool        `json:"ok"`
		Errors   int         `json:"errors"`
		Warnings int         `json:"warnings"`
		Files    []string    `json:"files"`
		Outputs  []string    `json:"outputs"`
		Tests    *TestCounts `json:"tests,omitempty"`
	}
	files, outputs := s.Files, s.Outputs
	if files == nil {
		files = []string{}
	}
	if outputs == nil {
		outputs = []string{}
	}
	return marshalJSON(wire{string(KindSummary), s.OK, s.Errors, s.Warnings, files, outputs, s.Tests})
}

// sortedUnique returns xs deduplicated and sorted, or an empty slice
// (never nil) when xs is empty.
func sortedUnique(xs []string) []string {
	out := unique(xs)
	slices.Sort(out)
	return out
}

// unique returns xs deduplicated keeping first-seen order, or an
// empty slice (never nil) when xs is empty.
func unique(xs []string) []string {
	out := make([]string, 0, len(xs))
	seen := make(map[string]bool, len(xs))
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
