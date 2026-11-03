package diag

import (
	"fmt"
	"io"
)

// TestResult is the "test" message of the spec section 12 stream —
// one per Go test function that ran under `gom test`. Name is the
// test's name as written in `test "Name" { ... }` (the Go function
// name for tests gom did not generate), Func the Go function it ran
// as — a `go test -run` selector. OK reports pass/fail (a skipped
// test reports OK true plus Skipped); Elapsed is the wall-clock
// seconds go test reported. Span is the test declaration's source
// position when the test came from a `test` block; Output is the
// output the test produced — on failure the assertion's values and
// source position — with go test's ===/--- framing removed.
type TestResult struct {
	Name    string
	Func    string
	OK      bool
	Skipped bool
	Elapsed float64
	Span    Span
	Output  string
}

// wireTest is the JSON shape of a "test" object (docs/spec.md
// section 12).
type wireTest struct {
	Kind    string   `json:"kind"`
	Name    string   `json:"name"`
	Func    string   `json:"func"`
	OK      bool     `json:"ok"`
	Skipped bool     `json:"skipped,omitempty"`
	Elapsed float64  `json:"elapsed"`
	Span    *SpanPos `json:"span,omitempty"`
	Output  string   `json:"output,omitempty"`
}

// MarshalTestResult renders t as one spec section 12 "test" object —
// a single line of JSON with no trailing newline. The span is
// resolved to line and column through res; a result without a source
// location omits "span". An error is returned when a located span
// cannot be resolved.
func MarshalTestResult(t TestResult, res Resolver) ([]byte, error) {
	w := wireTest{
		Kind:    string(KindTest),
		Name:    t.Name,
		Func:    t.Func,
		OK:      t.OK,
		Skipped: t.Skipped,
		Elapsed: t.Elapsed,
		Output:  t.Output,
	}
	if !t.Span.IsZero() {
		rp, err := ResolveSpan(res, t.Span)
		if err != nil {
			return nil, fmt.Errorf("diag: cannot marshal test %q: cannot resolve span %s:%d-%d: %w",
				t.Name, t.Span.File, t.Span.Start, t.Span.End, err)
		}
		w.Span = &rp
	}
	return marshalJSON(w)
}

// testStats counts the report's test results for the "summary"
// object's "tests" field: run counts every reported test, failed
// the ones that neither passed nor skipped, skipped the rest.
func (r Report) testStats() (run, failed, skipped int) {
	for _, t := range r.Tests {
		run++
		switch {
		case t.Skipped:
			skipped++
		case !t.OK:
			failed++
		}
	}
	return run, failed, skipped
}

// TestsFailed reports whether the run has failing test results —
// `gom test` folds it into the process exit code the same way
// error-severity diagnostics do (ExitError).
func (r Report) TestsFailed() bool {
	for _, t := range r.Tests {
		if !t.OK && !t.Skipped {
			return true
		}
	}
	return false
}

// writeTestsJSON emits the report's test results as "test" objects —
// the run-phase messages of the stream, after the diagnostics.
func (r Report) writeTestsJSON(w io.Writer, res Resolver) error {
	for i, t := range r.Tests {
		raw, err := MarshalTestResult(t, res)
		if err != nil {
			return fmt.Errorf("diag: cannot emit tests[%d]: %w", i, err)
		}
		if err := writeLine(w, raw); err != nil {
			return err
		}
	}
	return nil
}
