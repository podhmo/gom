package diag_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// TestMarshalTestResult renders a located "test" message — the wire
// shape the spec section 12 table assigns to `gom test`.
func TestMarshalTestResult(t *testing.T) {
	src := []byte("package shapes\ntest \"rect area\" {\n}\n")
	res := diag.NewMapResolver(map[string][]byte{"area.gom": src})
	// The name literal starts at offset 20 ("rect area").
	raw, err := diag.MarshalTestResult(diag.TestResult{
		Name:    "rect area",
		Func:    "TestRectArea",
		OK:      true,
		Elapsed: 0.001,
		Span:    diag.SpanIn("area.gom", 20, 31),
	}, res)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"test","name":"rect area","func":"TestRectArea","ok":true,"elapsed":0.001,` +
		`"span":{"file":"area.gom","start":{"line":2,"col":6,"offset":20},"end":{"line":2,"col":17,"offset":31}}}`
	if string(raw) != want {
		t.Errorf("marshal = %s\nwant    %s", raw, want)
	}
}

// TestMarshalTestResultFailure: a failing test carries its output and
// no span when gom did not generate it.
func TestMarshalTestResultFailure(t *testing.T) {
	res := diag.NewMapResolver(nil)
	raw, err := diag.MarshalTestResult(diag.TestResult{
		Name:   "TestForeign",
		Func:   "TestForeign",
		Output: "    x_test.go:5: boom\n",
	}, res)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		`"kind":"test"`, `"name":"TestForeign"`, `"ok":false`,
		`"output":"    x_test.go:5: boom\n"`,
	} {
		if !strings.Contains(string(raw), part) {
			t.Errorf("marshal %s lacks %s", raw, part)
		}
	}
	if strings.Contains(string(raw), `"span"`) {
		t.Errorf("spanless result emitted a span: %s", raw)
	}
	if strings.Contains(string(raw), `"skipped"`) {
		t.Errorf("non-skipped result emitted skipped: %s", raw)
	}
}

// TestSummaryTests: the summary's "tests" counts appear only when the
// report carried test results — nil Tests means the command never ran
// tests and the field stays absent.
func TestSummaryTests(t *testing.T) {
	r := diag.Report{}
	raw, err := r.Summary().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"tests"`) {
		t.Errorf("testless report emitted tests counts: %s", raw)
	}

	r.Tests = []diag.TestResult{
		{Name: "a", Func: "TestA", OK: true},
		{Name: "b", Func: "TestB", OK: false},
		{Name: "c", Func: "TestC", OK: true, Skipped: true},
	}
	raw, err = r.Summary().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"tests":{"run":3,"failed":1,"skipped":1}`) {
		t.Errorf("summary lacks tests counts: %s", raw)
	}
	if strings.Contains(string(raw), `"ok":true`) {
		t.Errorf("failing run reported ok: %s", raw)
	}

	// An empty non-nil Tests — `gom test` found no tests — still
	// reports the counts.
	r.Tests = []diag.TestResult{}
	raw, err = r.Summary().MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"tests":{"run":0,"failed":0,"skipped":0}`) {
		t.Errorf("empty run lacks zeroed tests counts: %s", raw)
	}
}

// TestExitCodeTests: a failing test fails the command the same way an
// error diagnostic does; skipped tests do not.
func TestExitCodeTests(t *testing.T) {
	if got := (diag.Report{}).ExitCode(); got != diag.ExitSuccess {
		t.Errorf("empty report exit = %d", got)
	}
	r := diag.Report{Tests: []diag.TestResult{{OK: true, Skipped: true}}}
	if got := r.ExitCode(); got != diag.ExitSuccess {
		t.Errorf("skipped-only run exit = %d", got)
	}
	r.Tests = append(r.Tests, diag.TestResult{OK: false})
	if got := r.ExitCode(); got != diag.ExitError {
		t.Errorf("failing run exit = %d", got)
	}
}

// TestWriteJSONLinesTests: test objects come after the diagnostics
// and before the closing summary (spec section 12's stream order).
func TestWriteJSONLinesTests(t *testing.T) {
	var out bytes.Buffer
	rep := diag.Report{
		Diagnostics: []diag.Diagnostic{
			diag.Error("E0001", "go tool failed").From(diag.SourceGo),
		},
		Tests: []diag.TestResult{{Name: "t", Func: "TestT", OK: true}},
	}
	if err := rep.WriteJSONLines(&out, diag.NewMapResolver(nil)); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("stream = %d lines, want diagnostic+test+summary:\n%s", len(lines), out.String())
	}
	for i, kind := range []string{`"kind":"diagnostic"`, `"kind":"test"`, `"kind":"summary"`} {
		if !strings.Contains(lines[i], kind) {
			t.Errorf("line %d = %s, want %s", i, lines[i], kind)
		}
	}
}

// TestWriteTextTests: the human format renders verdicts, positions
// and the failing-run trailer.
func TestWriteTextTests(t *testing.T) {
	src := []byte("package m\n\ntest \"fails\" {\n}\n")
	res := diag.NewMapResolver(map[string][]byte{"m.gom": src})
	rep := diag.Report{Tests: []diag.TestResult{
		{Name: "passes", Func: "TestPasses", OK: true, Span: diag.SpanIn("m.gom", 17, 24)},
		{Name: "fails", Func: "TestFails", Output: "    m.gom:4: gom: assertion failed:\n          actual:   1\n          expected: 2\n",
			Span: diag.SpanIn("m.gom", 17, 24)},
	}}
	var out bytes.Buffer
	if err := rep.WriteText(&out, res, diag.TextOptions{}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, part := range []string{
		`test "passes" ok (0.00s)`, `test "fails" FAIL (0.00s)`,
		"--> m.gom:3:7", "actual:   1", "error: 1 of 2 tests failed",
	} {
		if !strings.Contains(got, part) {
			t.Errorf("text output lacks %q:\n%s", part, got)
		}
	}
}
