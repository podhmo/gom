package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
//
// The fixtures that exercise `gom test` mechanics declare
// `package main` — a program root has no .gomi contract (spec
// section 7.4), so the interface gate these tests are not about
// stays out of the way. The gate itself is covered by
// TestTestWithholdsOnInterfaceFailure.
const testGom = `package main

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

// TestMain pins the Go toolchain environment once for the package:
// `gom test`/`go run` children spawn from inside `run`, so per-test
// t.Setenv is the only other reach — and it conflicts with t.Parallel.
func TestMain(m *testing.M) {
	os.Setenv("GOWORK", "off")
	os.Setenv("GOFLAGS", "-mod=mod")
	os.Setenv("GOTOOLCHAIN", "local")
	os.Exit(m.Run())
}

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
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.55)
	}

	t.Parallel()

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
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.15)
	}

	t.Parallel()

	haveGo(t)
	src := `package main

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
// zero-count tests object. badGom fails at resolve (`package ex`
// has no .gomi, but the run reports E0201, not E0601) — a file that
// never reaches type-checking forms no package group, so the
// interface gate does not fire on it at all.
func TestTestCompileErrorSkipsRun(t *testing.T) {
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{"bad.gom": badGom})
	testModule(t, dir)

	code, out, _ := invoke("test", filepath.Join(dir, "bad.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%q", code, out)
	}
	lines := jsonLines(t, out)
	var codes []string
	for _, l := range lines {
		switch l["kind"] {
		case "test":
			t.Fatalf("a test ran despite compile errors: %v", l)
		case "diagnostic":
			codes = append(codes, l["code"].(string))
		}
	}
	if !slices.Contains(codes, "E0201") {
		t.Errorf("diagnostic codes = %v, want E0201", codes)
	}
	if slices.Contains(codes, "E0601") {
		t.Errorf("diagnostic codes = %v — the interface gate fired on a file that never type-checked", codes)
	}
	sum := lastSummary(t, out)
	tc, _ := sum["tests"].(map[string]any)
	if tc["run"] != float64(0) {
		t.Errorf("tests ran despite compile errors: %v", tc)
	}
}

// TestTestSiblingPackage: `gom test` compiles same-directory inputs
// as one package unit — a test block in one file may reference a
// sibling file's declarations (the unit scope `compileInputs`
// establishes), and both members' outputs land beside the inputs.
func TestTestSiblingPackage(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.15)
	}

	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"a.gom": "package main\n\nlet base = 40\nfn bump (x: Int) -> Int = x + 1\n",
		"b.gom": "package main\n\ntest \"sibling refs\" {\n  assert_eq (bump base) 41\n}\n",
	})
	testModule(t, dir)

	code, out, errout := invoke("test",
		filepath.Join(dir, "a.gom"), filepath.Join(dir, "b.gom"))
	if errout != "" {
		t.Fatalf("stderr = %q", errout)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (the sibling-ref test passes); out=%q", code, out)
	}
	sum := lastSummary(t, out)
	tc, _ := sum["tests"].(map[string]any)
	if tc["run"] != float64(1) || tc["failed"] != float64(0) {
		t.Errorf("summary tests = %v", tc)
	}
	// Both members' production outputs land; only b, which declares
	// the test block, gets a _test.go.
	for _, f := range []string{"a.go", "b.go", "b_test.go"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not written: %v", f, err)
		}
	}
}

// TestTestStaleTestFileRemoved: when a .gom stops declaring tests,
// `gom test` removes the stale generated _test.go — but never a
// hand-written one (no generated marker).
func TestTestStaleTestFileRemoved(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.13)
	}

	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"m.gom":        "package main\n\nfn f () -> Int = 1\n",
		"m_test.go":    "// Code generated by gom. DO NOT EDIT.\n\npackage main\n\nimport \"testing\"\n\nfunc TestStale(t *testing.T) { t.Fail() }\n",
		"hand_test.go": "package main\n\nimport \"testing\"\n\nfunc TestHand(t *testing.T) {}\n",
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
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.14)
	}

	t.Parallel()

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
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"m.gom":     "package main\n\nfn f () -> Int = 1\n",
		"z_test.go": "package main\n\nimport \"testing\"\n\nfunc TestZ(t *testing.T) { undefined_symbol() }\n",
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

// TestTestWithholdsOnInterfaceFailure: `gom test` runs the same
// .gomi conformance gate as `gom build` (spec section 7.8) — a
// package with E0601 (no interface file) or E0603 (impl↔interface
// drift) writes no generated output and runs no tests, while a
// clean package in the same run still writes.
func TestTestWithholdsOnInterfaceFailure(t *testing.T) {
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		// E0601: a library package with no .gomi at all.
		"noiface/lib.gom": "package lib\n\nfn f () -> Int = 1\n\ntest \"never\" {\n  assert_eq 1 1\n}\n",
		// E0603: implementation drifts from the interface.
		"mismatch/mism.gom":  "package mism\n\nfn f () -> Int = 1\n\ntest \"never\" {\n  assert_eq 1 1\n}\n",
		"mismatch/mism.gomi": "package mism\n\nfn f () -> String\n",
		// A clean main package beside them still writes — the
		// failures are package-scoped, not run-scoped.
		"app/main.gom": "package main\n\nfn f () -> Int = 1\n",
	})
	testModule(t, dir)

	code, out, _ := invoke("test",
		filepath.Join(dir, "noiface", "lib.gom"),
		filepath.Join(dir, "mismatch", "mism.gom"),
		filepath.Join(dir, "app", "main.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%q", code, out)
	}
	var codes []string
	for _, l := range jsonLines(t, out) {
		switch l["kind"] {
		case "diagnostic":
			codes = append(codes, l["code"].(string))
		case "test":
			t.Errorf("a test ran despite interface failures: %v", l)
		}
	}
	if !slices.Contains(codes, "E0601") || !slices.Contains(codes, "E0603") {
		t.Errorf("diagnostic codes = %v, want E0601 and E0603", codes)
	}
	for _, rel := range []string{
		"noiface/lib.go", "noiface/lib_test.go",
		"mismatch/mism.go", "mismatch/mism_test.go",
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Errorf("%s was written despite interface failure: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "app", "main.go")); err != nil {
		t.Errorf("main.go missing — the clean package should still write: %v", err)
	}
	sum := lastSummary(t, out)
	tc, _ := sum["tests"].(map[string]any)
	if tc["run"] != float64(0) {
		t.Errorf("tests ran despite interface failures: %v", tc)
	}
}

// TestTestInterfaceGatePass: a library package whose .gomi matches
// its implementation passes the gate — `gom test` writes the .go
// and _test.go and runs the tests. `test` blocks are not interface
// members (spec section 11.5), so they need no .gomi entry.
func TestTestInterfaceGatePass(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.07)
	}

	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"p.gom":  "package p\n\nfn bump (x: Int) -> Int = x + 1\n\ntest \"bump\" {\n  assert_eq (bump 1) 2\n}\n",
		"p.gomi": "package p\n\nfn bump (x: Int) -> Int\n",
	})
	testModule(t, dir)

	code, out, errout := invoke("test", filepath.Join(dir, "p.gom"))
	if errout != "" {
		t.Fatalf("stderr = %q", errout)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0; out=%q", code, out)
	}
	var tests int
	for _, l := range jsonLines(t, out) {
		if l["kind"] == "test" {
			tests++
			if l["name"] != "bump" || l["ok"] != true {
				t.Errorf("test result = %v", l)
			}
		}
	}
	if tests != 1 {
		t.Errorf("stream carried %d test objects, want 1", tests)
	}
	sum := lastSummary(t, out)
	tc, _ := sum["tests"].(map[string]any)
	if tc["run"] != float64(1) || tc["failed"] != float64(0) {
		t.Errorf("summary tests = %v", tc)
	}
}

// TestTestInterfaceEquivalentPaths pins, for `gom test`, the same
// already-correct .gom grouping `gom check` shows:
// `pkg/../pkg/a.gom` and `pkg/b.gom` are one package (filepath.Dir
// already Cleans), so the member `h` p.gomi declares but no file
// implements is E0603, the group's outputs are withheld, and no
// test runs. Pre-existing behavior — the unclean-path bug was the
// .gomi input key, not the grouping.
func TestTestInterfaceEquivalentPaths(t *testing.T) {
	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{
		"pkg/a.gom":  "package p\n\nfn f (x: Int) -> Int = x\n",
		"pkg/b.gom":  "package p\n\nfn g (x: Int) -> Int = x\n\ntest \"never\" {\n  assert_eq 1 1\n}\n",
		"pkg/p.gomi": "package p\n\nfn f (x: Int) -> Int\nfn g (x: Int) -> Int\nfn h (x: Int) -> Int\n",
	})
	testModule(t, dir)

	// filepath.Join would clean the ".." away — spell the path
	// with an explicit separator instead.
	unclean := filepath.Join(dir, "pkg") + string(filepath.Separator) +
		".." + string(filepath.Separator) + filepath.Join("pkg", "a.gom")
	code, out, _ := invoke("test", unclean, filepath.Join(dir, "pkg", "b.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%q", code, out)
	}
	var codes []string
	for _, l := range jsonLines(t, out) {
		switch l["kind"] {
		case "diagnostic":
			codes = append(codes, l["code"].(string))
		case "test":
			t.Errorf("a test ran despite the interface failure: %v", l)
		}
	}
	if len(codes) != 1 || codes[0] != "E0603" {
		t.Errorf("diagnostic codes = %v, want [E0603] for the missing member `h`", codes)
	}
	for _, rel := range []string{"pkg/a.go", "pkg/b.go", "pkg/b_test.go"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
			t.Errorf("%s was written despite the interface failure: %v", rel, err)
		}
	}
	sum := lastSummary(t, out)
	tc, _ := sum["tests"].(map[string]any)
	if tc["run"] != float64(0) {
		t.Errorf("tests ran despite the interface failure: %v", tc)
	}
}

// panicGom dies inside the test body — `todo ()` compiles to a
// call that panics, killing the test binary mid-test.
const panicGom = `package main

fn boom () -> Int = todo ()

test "panics" {
  assert_eq (boom ()) 1
}
`

// panicOutput returns the stream output — a test's or a
// diagnostic's data.output — that carried a panic trace. A
// crashing test's dump lands in one of them.
func panicOutput(t *testing.T, out string) string {
	t.Helper()
	for _, l := range jsonLines(t, out) {
		data, _ := l["data"].(map[string]any)
		for _, v := range []any{l["output"], data["output"]} {
			if s, ok := v.(string); ok && strings.Contains(s, "panic:") {
				return s
			}
		}
	}
	t.Fatalf("no stream object carried a panic trace: %s", out)
	return ""
}

// TestTestPanicStackTwoPart: a panicking test's output takes the
// spec section 12 two-part shape — the .gom-position re-render
// first (PC offsets dropped, unresolvable toolchain frames
// verbatim), then the raw Go trace under the marker.
func TestTestPanicStackTwoPart(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.97)
	}

	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{"m.gom": panicGom})
	testModule(t, dir)

	code, out, _ := invoke("test", filepath.Join(dir, "m.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%q", code, out)
	}
	output := panicOutput(t, out)
	i := strings.Index(output, rawTraceMarker)
	if i < 0 {
		t.Fatalf("output lacks the raw-trace marker: %q", output)
	}
	rendered, raw := output[:i], output[i:]
	gomPath := filepath.Join(dir, "m.gom")
	for name, half := range map[string]string{"rendered": rendered, "raw": raw} {
		if !strings.Contains(half, "panic: gom: todo") || !strings.Contains(half, gomPath+":") {
			t.Errorf("%s half lacks the panic line or a gom frame: %q", name, half)
		}
	}
	for l := range strings.SplitSeq(rendered, "\n") {
		if strings.Contains(l, "m.gom:") && strings.Contains(l, "+0x") {
			t.Errorf("rendered frame kept its PC offset: %q", l)
		}
	}
	if !strings.Contains(raw, "+0x") {
		t.Errorf("raw half lost the PC offsets: %q", raw)
	}
}

// TestTestPanicStackQuiet: -q/--quiet suppresses the raw half of a
// panic output — the .gom-position trace still appears.
func TestTestPanicStackQuiet(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.88)
	}

	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{"m.gom": panicGom})
	testModule(t, dir)

	for _, flag := range []string{"-q", "--quiet"} {
		code, out, _ := invoke("test", flag, filepath.Join(dir, "m.gom"))
		if code != 1 {
			t.Fatalf("%s: exit = %d, want 1; out=%q", flag, code, out)
		}
		output := panicOutput(t, out)
		if strings.Contains(output, rawTraceMarker) {
			t.Errorf("%s: output kept the raw half:\n%s", flag, output)
		}
		if !strings.Contains(output, "panic: gom: todo") ||
			!strings.Contains(output, filepath.Join(dir, "m.gom")+":") {
			t.Errorf("%s: rendered trace lost its content: %q", flag, output)
		}
		if n := strings.Count(output, "panic: gom: todo"); n != 1 {
			t.Errorf("%s: panic line appears %d times, want 1 (raw half suppressed)", flag, n)
		}
	}
}

// TestTestPanicStackPackageLevel: a panic in package init — the
// test binary dies before any test runs — lands in the
// package-level E0001's output, which takes the same two-part
// rendering. (The package needs one test for `go test` to build
// and run a binary at all.)
func TestTestPanicStackPackageLevel(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.36)
	}

	t.Parallel()

	haveGo(t)
	src := `package main

fn boom () -> Int = todo ()

let crash = boom ()

test "never reached" {
  assert_eq 1 1
}
`
	dir := writeFiles(t, map[string]string{"m.gom": src})
	testModule(t, dir)

	code, out, _ := invoke("test", filepath.Join(dir, "m.gom"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1; out=%q", code, out)
	}
	var found bool
	for _, l := range jsonLines(t, out) {
		if l["kind"] == "test" {
			t.Fatalf("a test ran despite the init panic: %v", l)
		}
		data, _ := l["data"].(map[string]any)
		s, _ := data["output"].(string)
		if strings.Contains(s, "panic:") {
			found = true
			if !strings.Contains(s, rawTraceMarker) {
				t.Errorf("package-level output lacks the two-part marker: %q", s)
			}
		}
	}
	if !found {
		t.Fatalf("no diagnostic carried the init panic: %s", out)
	}
}

// TestTestGoFlagPassthrough: arguments after "--" reach `go test`
// verbatim — here -run selects one of two generated tests. A
// malformed flag surfaces as a go-tool E0001, not a gom usage
// error (docs/decisions/2026-11-05-gom-test-go-test-flags.md).
func TestTestGoFlagPassthrough(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 2.71)
	}

	t.Parallel()

	haveGo(t)
	src := `package main

test "alpha" {
  assert_eq 1 1
}

test "beta" {
  assert_eq 2 2
}
`
	dir := writeFiles(t, map[string]string{"m.gom": src})
	testModule(t, dir)

	code, out, errout := invoke("test", filepath.Join(dir, "m.gom"), "--", "-run", "TestAlpha")
	if errout != "" {
		t.Fatalf("stderr = %q", errout)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0; out=%q", code, out)
	}
	var names []string
	for _, l := range jsonLines(t, out) {
		if l["kind"] == "test" {
			names = append(names, l["name"].(string))
		}
	}
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("tests run = %v, want only [alpha] — -run did not reach go test", names)
	}

	// Only the FIRST "--" splits: a second one is passthrough data —
	// `go test` reads it as the start of test-binary arguments
	// (here -v), which it accepts.
	code, out, _ = invoke("test", filepath.Join(dir, "m.gom"), "--", "-run", "TestAlpha", "--", "-v")
	if code != 0 {
		t.Fatalf("second -- : exit = %d, want 0; out=%q", code, out)
	}
	names = names[:0]
	for _, l := range jsonLines(t, out) {
		if l["kind"] == "test" {
			names = append(names, l["name"].(string))
		}
	}
	if len(names) != 1 || names[0] != "alpha" {
		t.Errorf("second -- : tests run = %v, want only [alpha]", names)
	}

	// A "--" before the inputs leaves gom's input list empty — the
	// file after "--" is go test's argument, not a source path, so
	// the inputs default to "." (the test process's cwd, which has
	// no .gom files — the usage error pins the boundary).
	code, _, errout = invoke("test", "--", filepath.Join(dir, "m.gom"))
	if code != 2 || !strings.Contains(errout, "no .gom files") {
		t.Errorf("-- before inputs: exit = %d stderr = %q; want 2 and the empty-input error", code, errout)
	}

	// -json is gom's own channel: defeating it breaks the event
	// stream, which surfaces as a go-tool E0001 like any malformed
	// flag — not a gom usage error.
	code, out, _ = invoke("test", filepath.Join(dir, "m.gom"), "--", "-json=false")
	if code != 1 {
		t.Fatalf("-json=false: exit = %d, want 1; out=%q", code, out)
	}
	var sawE0001 bool
	for _, l := range jsonLines(t, out) {
		if l["code"] == "E0001" {
			sawE0001 = true
		}
	}
	if !sawE0001 {
		t.Errorf("-json=false produced no E0001: %s", out)
	}

	// An unknown flag rides the same path.
	code, out, _ = invoke("test", filepath.Join(dir, "m.gom"), "--", "-bogus-flag")
	if code != 1 {
		t.Fatalf("bogus flag: exit = %d, want 1; out=%q", code, out)
	}
	sawE0001 = false
	for _, l := range jsonLines(t, out) {
		if l["code"] == "E0001" {
			sawE0001 = true
		}
	}
	if !sawE0001 {
		t.Errorf("malformed passthrough flag produced no E0001: %s", out)
	}
}

// concGom exercises spec section 10 end to end through `gom test`:
// buffered sends and the closed-channel `None`, a `go` producer
// rendezvousing on an unbuffered channel, `for` draining until
// close, all three select arm kinds, the explicit direction views,
// implicit narrowing at a `chan<-`/`<-chan`/`<-chan struct{}` Go
// boundary, and Task.spawn/await. Every case is deterministic —
// channels are buffered or the test body is the sole receiver, and
// no select has more than one non-default arm ready.
const concGom = `package main

import go "github.com/podhmo/gom/testdata/goiface"

test "buffered channel" {
  let ch: Chan Int = Chan.make 2
  Chan.send ch 1
  Chan.send ch 2
  Chan.close ch
  assert_eq (Chan.recv ch) (Some 1)
  assert_eq (Chan.recv ch) (Some 2)
  assert_eq (Chan.recv ch) None
}

test "go rendezvous" {
  let ch: Chan Int = Chan.make 0
  go { Chan.send ch 42 }
  assert_eq (Chan.recv ch) (Some 42)
}

test "for drains until close" {
  let ch: Chan Int = Chan.make 0
  go {
    Chan.send ch 1
    Chan.send ch 2
    Chan.close ch
  }
  for v in ch {
    ignore v
  }
}

test "select default when empty" {
  let ch: Chan Int = Chan.make 0
  assert_eq (select {
    _ <- ch -> 1
    default -> -1
  }) (-1)
}

test "select buffered recv" {
  let ch: Chan Int = Chan.make 1
  Chan.send ch 7
  assert_eq (select {
    r <- ch -> match r {
      Some v -> v
      None -> -1
    }
    default -> -2
  }) 7
}

test "select send fires when buffered" {
  let ch: Chan Int = Chan.make 1
  assert_eq (select {
    send ch 5 -> true
    default -> false
  }) true
}

test "direction views" {
  let ch: Chan Int = Chan.make 1
  let out = Chan.sendOnly ch
  go { Chan.send out 1 }
  assert_eq (Chan.recv (Chan.recvOnly ch)) (Some 1)
}

test "go chan boundary" {
  let ch: Chan String = Chan.make 1
  goiface.SendOn(ch, "hi")
  assert_eq (goiface.RecvOn ch) ("hi", true)
}

test "ctx done shape" {
  let done = goiface.Done ()
  assert_eq (select {
    _ <- done -> true
    default -> false
  }) false
}

test "nil chan never ready" {
  let n = goiface.NilSig ()
  assert_eq (select {
    _ <- n -> true
    default -> false
  }) false
}

test "notify done" {
  let done: Chan Unit = Chan.make 0
  let out: Chan String = Chan.make 1
  go { goiface.NotifyDone(done, out, "ok") }
  Chan.close done
  assert_eq (Chan.recv out) (Some "ok")
}

test "task await" {
  let t = Task.spawn (fn () -> 6 * 7)
  assert_eq (Task.await t) 42
}
`

// TestTestConcurrency: the generated code for the section-10
// surface compiles and behaves under the real Go toolchain — every
// `test` in concGom reports ok.
func TestTestConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.39)
	}

	t.Parallel()

	haveGo(t)
	dir := writeFiles(t, map[string]string{"c.gom": concGom})
	testModule(t, dir)

	code, out, errout := invoke("test", filepath.Join(dir, "c.gom"))
	if errout != "" {
		t.Fatalf("stderr = %q", errout)
	}
	lines := jsonLines(t, out)
	var tests []map[string]any
	for _, l := range lines {
		if l["kind"] == "test" {
			tests = append(tests, l)
		}
	}
	if code != 0 || len(tests) != 12 {
		t.Fatalf("exit = %d, tests = %d, want 0 and 12; tests=%v out=%q",
			code, len(tests), tests, out)
	}
	for _, tc := range tests {
		if tc["ok"] != true {
			t.Errorf("test %v failed: %v", tc["name"], tc)
		}
	}
}
