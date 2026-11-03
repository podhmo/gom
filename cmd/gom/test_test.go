package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Command-level tests of `gom test` (docs/spec.md sections 11–12):
// the command compiles in test mode, writes the .go and _test.go
// files beside the input, and folds `go test -json` into the message
// stream — "test" objects plus toolchain diagnostics, closed by the
// summary's test counts.

// testGom exercises the shapes the stream must carry: a pass, an
// assert_eq failure with composite display, a Result-Err test
// failure, and an @test helper that must not reach production
// output.
const testGom = `package m

type Point = { x: Float, y: Float }

@test
fn origin () -> Point = { x = 0.0, y = 0.0 }

fn bump (p: Point) -> Point = { x = p.x + 1.0, y = p.y }

test "bump" {
  assert_eq (bump (origin ())) { x = 1.0, y = 0.0 }
}

test "mismatch" {
  assert_eq (bump (origin ())) { x = 1.0, y = 1.0 }
}
`

// testModule writes a go.mod pointing at this worktree so the
// generated code resolves gom's rt/stdlib packages under the real Go
// toolchain.
func testModule(t *testing.T, dir string) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module m\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOTOOLCHAIN", "local")
}

func haveGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}
}

// TestTestEmitsStream: `gom test` writes both generated files, runs
// `go test -json`, and reports one "test" object per test — mapped
// back to the `test "name"` position — then the summary's counts.
func TestTestEmitsStream(t *testing.T) {
	haveGo(t)
	dir := writeFiles(t, map[string]string{"m.gom": testGom})
	testModule(t, dir)

	code, out, errout := invoke("test", filepath.Join(dir, "m.gom"))
	if errout != "" {
		t.Fatalf("stderr = %q", errout)
	}
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (a test fails); out=%q", code, out)
	}
	for _, f := range []string{"m.go", "m_test.go"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
	}
	// The @test helper lives only in the test half.
	main, err := os.ReadFile(filepath.Join(dir, "m.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(main), "origin") {
		t.Errorf("@test helper leaked into production output:\n%s", main)
	}

	lines := jsonLines(t, out)
	var tests []map[string]any
	for _, l := range lines {
		if l["kind"] == "test" {
			tests = append(tests, l)
		}
	}
	if len(tests) != 2 {
		t.Fatalf("stream carried %d test objects, want 2: %v", len(tests), lines)
	}
	if tests[0]["name"] != "bump" || tests[0]["func"] != "TestBump" || tests[0]["ok"] != true {
		t.Errorf("pass result = %v", tests[0])
	}
	if tests[0]["span"] == nil {
		t.Errorf("pass result lacks the test decl's span: %v", tests[0])
	}
	fail := tests[1]
	if fail["name"] != "mismatch" || fail["ok"] != false {
		t.Errorf("fail result = %v", fail)
	}
	out2, _ := fail["output"].(string)
	if !strings.Contains(out2, "m.gom:") || !strings.Contains(out2, "actual:") || !strings.Contains(out2, "expected:") {
		t.Errorf("fail output lacks position and values: %q", out2)
	}
	sum := lastSummary(t, out)
	tc, _ := sum["tests"].(map[string]any)
	if tc["run"] != float64(2) || tc["failed"] != float64(1) {
		t.Errorf("summary tests = %v", tc)
	}
	if sum["ok"] != false {
		t.Errorf("summary ok = %v on a failing run", sum["ok"])
	}
}

// TestTestResultBodyFailure: a test body that returns `Err _` fails
// through result.AssertOk, and the failure line maps back to the .gom
// position of the `try`/tail.
func TestTestResultBodyFailure(t *testing.T) {
	haveGo(t)
	src := `package m

fn may_fail (n: Int) -> Result Int String =
  if n > 0 { Ok n } else { Err "nonpositive" }

test "fails" {
  let _n = try (may_fail 0)
  Ok ()
}
`
	dir := writeFiles(t, map[string]string{"m.gom": src})
	testModule(t, dir)

	code, out, _ := invoke("test", filepath.Join(dir, "m.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%q", code, out)
	}
	lines := jsonLines(t, out)
	var fail map[string]any
	for _, l := range lines {
		if l["kind"] == "test" {
			fail = l
		}
	}
	if fail == nil || fail["ok"] != false {
		t.Fatalf("no failing test object: %v", lines)
	}
	out2, _ := fail["output"].(string)
	if !strings.Contains(out2, "nonpositive") || !strings.Contains(out2, "m.gom:") {
		t.Errorf("result failure output lacks error and position: %q", out2)
	}
}

// TestTestCompileErrorSkipsRun: with error diagnostics, `gom test`
// emits the diagnostics, never invokes go test, and reports a
// zero-count tests object.
func TestTestCompileErrorSkipsRun(t *testing.T) {
	haveGo(t)
	dir := writeFiles(t, map[string]string{"bad.gom": badGom})
	testModule(t, dir)

	code, out, _ := invoke("test", filepath.Join(dir, "bad.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%q", code, out)
	}
	lines := jsonLines(t, out)
	for _, l := range lines {
		if l["kind"] == "test" {
			t.Fatalf("a test ran despite compile errors: %v", l)
		}
	}
	sum := lastSummary(t, out)
	tc, _ := sum["tests"].(map[string]any)
	if tc["run"] != float64(0) {
		t.Errorf("tests ran despite compile errors: %v", tc)
	}
}

// TestTestStaleTestFileRemoved: when a .gom stops declaring tests,
// `gom test` removes the stale generated _test.go — but never a
// hand-written one (no generated marker).
func TestTestStaleTestFileRemoved(t *testing.T) {
	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"m.gom":        "package m\n\nfn f () -> Int = 1\n",
		"m_test.go":    "// Code generated by gom. DO NOT EDIT.\n\npackage m\n\nimport \"testing\"\n\nfunc TestStale(t *testing.T) { t.Fail() }\n",
		"hand_test.go": "package m\n\nimport \"testing\"\n\nfunc TestHand(t *testing.T) {}\n",
	})
	testModule(t, dir)

	code, _, errout := invoke("test", filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("exit = %d; stderr=%q", code, errout)
	}
	if _, err := os.Stat(filepath.Join(dir, "m_test.go")); !os.IsNotExist(err) {
		t.Errorf("stale generated m_test.go not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hand_test.go")); err != nil {
		t.Errorf("hand-written test file was removed: %v", err)
	}
}

// TestTestFormatText: the text format renders test verdicts and the
// failing-run trailer.
func TestTestFormatText(t *testing.T) {
	haveGo(t)
	dir := writeFiles(t, map[string]string{"m.gom": testGom})
	testModule(t, dir)

	code, out, _ := invoke("test", "-format", "text", filepath.Join(dir, "m.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%q", code, out)
	}
	for _, part := range []string{
		`test "bump" ok`, `test "mismatch" FAIL`, "1 of 2 tests failed",
	} {
		if !strings.Contains(out, part) {
			t.Errorf("text output lacks %q:\n%s", part, out)
		}
	}
}

// TestTestGoBuildFailure: a generated test file that does not compile
// — here a stale handwritten-free _test.go referencing an undefined
// symbol — folds go test's build-output into E0001 diagnostics with
// source "go".
func TestTestGoBuildFailure(t *testing.T) {
	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"m.gom":     "package m\n\nfn f () -> Int = 1\n",
		"z_test.go": "package m\n\nimport \"testing\"\n\nfunc TestZ(t *testing.T) { undefined_symbol() }\n",
	})
	testModule(t, dir)

	code, out, _ := invoke("test", filepath.Join(dir, "m.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%q", code, out)
	}
	lines := jsonLines(t, out)
	var diags []map[string]any
	for _, l := range lines {
		if l["kind"] == "diagnostic" {
			diags = append(diags, l)
		}
	}
	if len(diags) == 0 {
		t.Fatalf("build failure produced no diagnostics: %v", lines)
	}
	d := diags[0]
	if d["code"] != "E0001" || d["source"] != "go" {
		t.Errorf("go build failure diagnostic = %v", d)
	}
	if !strings.Contains(d["message"].(string), "undefined") {
		t.Errorf("diagnostic message = %v", d["message"])
	}
}
