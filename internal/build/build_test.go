package build_test

// Driver-level tests: error boundaries, artifact handoff, and the
// interface-file and check-only paths. The file suites in
// harness_test.go cover end-to-end fixture behavior; these pin the
// boundaries each testdata file cannot see on its own.

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/lower"
)

func compile(t *testing.T, src string, opts *build.Options) *build.Result {
	t.Helper()
	r, err := build.Compile(build.Input{Path: "t.gom", Src: []byte(src)}, opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return r
}

func codes(r *build.Result) []diag.Code {
	var out []diag.Code
	for _, d := range r.Diagnostics {
		out = append(out, d.Code)
	}
	return out
}

func TestCompileSuccessProducesAllArtifacts(t *testing.T) {
	r := compile(t, `package m

fn f (x: Int) -> Int = x + 1
`, nil)
	if len(r.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", r.Diagnostics)
	}
	if r.File == nil || r.Resolved == nil || r.Typed == nil || r.Core == nil || r.Output == nil {
		t.Fatalf("artifact missing: %+v", r)
	}
	if len(r.Output.Source) == 0 {
		t.Error("empty generated source")
	}
}

// TestTestsModeEmitsTestFile pins spec 11.6's `gom test` shape end
// to end: with lower.Env.Tests the pipeline keeps `test` decls and
// `@test` helpers, and codegen splits them into `<pkg>_test.go` —
// the rt.Assert*/AssertEq emission the golden suite never sees
// (it generates with Tests off).
func TestTestsModeEmitsTestFile(t *testing.T) {
	src := `package m

@test
fn helper (x: Int) -> Int = x + 1

fn main () -> Unit = ()

test "arith" {
  assert_eq (helper 1) 2
  assert true
}
`
	r := compile(t, src, &build.Options{Lower: &lower.Env{Tests: true}})
	for _, d := range r.Diagnostics {
		if d.Severity == diag.SeverityError {
			t.Fatalf("%s: %s", d.Code, d.Message)
		}
	}
	if r.Output == nil {
		t.Fatal("no generated output")
	}
	ts := string(r.Output.TestSource)
	for _, want := range []string{"func TestArith(t *testing.T)", "rt.AssertEq(t,", "rt.Assert(t,"} {
		if !strings.Contains(ts, want) {
			t.Errorf("TestSource missing %q:\n%s", want, ts)
		}
	}
	if !strings.Contains(string(r.Output.Source), "func helper(") {
		t.Error("main source missing the @test helper — gom test compiles it into the package")
	}

	// Build mode drops both halves instead.
	rb := compile(t, src, nil)
	if rb.Output == nil {
		t.Fatal("build mode: no generated output")
	}
	if rb.Output.TestSource != nil {
		t.Error("build mode emitted a test file")
	}
	if strings.Contains(string(rb.Output.Source), "func helper(") {
		t.Error("build mode kept the @test helper")
	}
}

func TestParseErrorStopsBeforeResolve(t *testing.T) {
	r := compile(t, "package m\n\nfn f (x: Int -> Int = x\n", nil)
	if len(r.Diagnostics) == 0 {
		t.Fatal("want diagnostics")
	}
	if r.File == nil {
		t.Error("parse always returns a partial file")
	}
	if r.Resolved != nil || r.Typed != nil || r.Core != nil || r.Output != nil {
		t.Errorf("downstream artifacts must be nil after a parse error: %+v", r)
	}
}

func TestResolveErrorStopsBeforeCheck(t *testing.T) {
	r := compile(t, "package m\n\nfn f () -> Int = y\n", nil)
	found := false
	for _, c := range codes(r) {
		if c == diag.Code("E0201") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want E0201, got %v", codes(r))
	}
	// resolve still returns its best-effort tree; everything below is nil.
	if r.Resolved == nil {
		t.Error("resolved artifact should be present")
	}
	if r.Typed != nil || r.Core != nil || r.Output != nil {
		t.Errorf("downstream artifacts must be nil: %+v", r)
	}
}

func TestCheckErrorStopsBeforeLower(t *testing.T) {
	r := compile(t, "package m\n\nfn f () -> Int = \"s\"\n", nil)
	if len(r.Diagnostics) == 0 {
		t.Fatal("want type error diagnostics")
	}
	if r.Typed == nil {
		t.Error("typed artifact should be present (best-effort)")
	}
	if r.Core != nil || r.Output != nil {
		t.Errorf("lower/codegen artifacts must be nil: %+v", r)
	}
}

func TestPatternErrorStopsBeforeLower(t *testing.T) {
	// Non-exhaustive match is a pattern-phase (E0301) diagnostic.
	r := compile(t, `package m

type B = | T | F

fn f (b: B) -> Int =
  match b {
    T -> 1
  }
`, nil)
	found := false
	for _, c := range codes(r) {
		if c == diag.Code("E0301") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want E0301, got %v", codes(r))
	}
	if r.Core != nil || r.Output != nil {
		t.Errorf("lower/codegen artifacts must be nil: %+v", r)
	}
}

func TestDiagnosticsAggregateAcrossPhases(t *testing.T) {
	// resolve reports the undefined value and, since it is
	// best-effort, later phases must not add to the list — the run
	// stops at the boundary, so the total equals resolve's own.
	r := compile(t, "package m\n\nfn f () -> Int = y\n", nil)
	if len(r.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v, want exactly the resolve error", r.Diagnostics)
	}
}

func TestCheckOnlyStopsBeforeLower(t *testing.T) {
	r := compile(t, "package m\n\nfn f (x: Int) -> Int = x\n", &build.Options{CheckOnly: true})
	if len(r.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", r.Diagnostics)
	}
	if r.Typed == nil {
		t.Error("check-only still type-checks")
	}
	if r.Core != nil || r.Output != nil {
		t.Errorf("check-only must not lower or generate: %+v", r)
	}
}

func TestInterfaceFileStopsAfterCheck(t *testing.T) {
	r, err := build.Compile(build.Input{
		Path: "t.gomi",
		Src:  []byte("package m\n\ntype Shape\nfn area (s: Shape) -> Float\n"),
	}, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !r.Interface {
		t.Error("result must mark the input as an interface file")
	}
	if len(r.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", r.Diagnostics)
	}
	if r.Typed == nil {
		t.Error("interface files still get checked")
	}
	if r.Core != nil || r.Output != nil {
		t.Errorf("interface files must not be lowered or generated: %+v", r)
	}
}

func TestInputSuffixImpliesInterface(t *testing.T) {
	r, err := build.Compile(build.Input{
		Path: "sig.gomi",
		Src:  []byte("package m\n\nfn f (x: Int) -> Int\n"),
	}, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !r.Interface {
		t.Error(".gomi suffix must imply Interface")
	}
}

func TestWarningsDoNotStopThePipeline(t *testing.T) {
	// Every phase so far either emits nothing or errors, so this
	// pins the boundary rule itself rather than a warning code.
	r := compile(t, "package m\n\nfn f (x: Int) -> Int = x\n", nil)
	if r.Output == nil {
		t.Fatalf("clean file must produce output: %v", r.Diagnostics)
	}
}
