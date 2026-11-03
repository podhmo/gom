package main

// Command-level tests: run() is exercised with real files in
// t.TempDir, and TestEndToEnd_BuildAndRun goes all the way through
// the real Go toolchain.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	// badGom has one resolvable diagnostic: undefined value `y`.
	badGom = `package ex

fn f () -> Int = y
`
	// goodGom compiles cleanly; the Int result lands in the binary.
	goodGom = `package main

type Expr =
  | Num Int
  | Add Expr Expr

fn eval (e: Expr) -> Int =
  match e {
    Num n -> n
    Add l r -> eval l + eval r
  }

fn answer () -> Int =
  eval (Add (Num 40) (Num 2))
`

	// mainGom is a runnable program: `fn main () -> Unit` in
	// `package main` lowers to `func main()`. fmt.Println's real
	// signature is `(int, error)` → Result Int GoError, so the call
	// is discarded under `let _`.
	mainGom = `package main

import go "fmt"

type Expr =
  | Num Int
  | Add Expr Expr

fn eval (e: Expr) -> Int =
  match e {
    Num n -> n
    Add l r -> eval l + eval r
  }

fn main () -> Unit = {
  let _ = fmt.Println("gom answer:", eval (Add (Num 40) (Num 2)))
  ()
}
`
)

// writeFiles creates dir/name for each entry and returns dir.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// invoke runs the CLI and returns (exit code, stdout, stderr).
func invoke(args ...string) (int, string, string) {
	var stdout, stderr strings.Builder
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// jsonLines parses each line of out into a generic object.
func jsonLines(t *testing.T, out string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line is not JSON: %q: %v", l, err)
		}
		lines = append(lines, m)
	}
	return lines
}

// lastSummary returns the trailing summary object of a JSON Lines stream.
func lastSummary(t *testing.T, out string) map[string]any {
	t.Helper()
	lines := jsonLines(t, out)
	if len(lines) == 0 {
		t.Fatal("expected at least a summary line")
	}
	sum := lines[len(lines)-1]
	if sum["kind"] != "summary" {
		t.Fatalf("last line kind = %v, want summary: %v", sum["kind"], sum)
	}
	return sum
}

func TestCheckDiagnosticsJSON(t *testing.T) {
	dir := writeFiles(t, map[string]string{"bad.gom": badGom})
	code, out, errout := invoke("check", filepath.Join(dir, "bad.gom"))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q stdout=%q", code, errout, out)
	}
	lines := jsonLines(t, out)
	if len(lines) != 2 {
		t.Fatalf("got %d stream lines, want 1 diagnostic + 1 summary: %v", len(lines), lines)
	}
	d := lines[0]
	if d["kind"] != "diagnostic" || d["code"] != "E0201" || d["severity"] != "error" {
		t.Errorf("unexpected diagnostic: %v", d)
	}
	if d["message"] != "undefined name `y`" {
		t.Errorf("message = %v", d["message"])
	}
	span, _ := d["span"].(map[string]any)
	start, _ := span["start"].(map[string]any)
	if start["line"] != float64(3) || start["col"] != float64(18) {
		t.Errorf("span.start = %v, want line 3 col 18", start)
	}
	sum := lastSummary(t, out)
	if sum["ok"] != false || sum["errors"] != float64(1) {
		t.Errorf("summary = %v", sum)
	}
	if errout != "" {
		t.Errorf("stderr = %q, want empty", errout)
	}
}

func TestCheckDiagnosticsText(t *testing.T) {
	dir := writeFiles(t, map[string]string{"bad.gom": badGom})
	code, out, _ := invoke("check", "-format", "text", filepath.Join(dir, "bad.gom"))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%q", code, out)
	}
	if !strings.Contains(out, "error[E0201]") || !strings.Contains(out, "undefined name `y`") {
		t.Errorf("text output missing diagnostic: %q", out)
	}
}

func TestCheckClean(t *testing.T) {
	dir := writeFiles(t, map[string]string{"a.gom": goodGom})
	code, out, _ := invoke("check", filepath.Join(dir, "a.gom"))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q", code, out)
	}
	lines := jsonLines(t, out)
	if len(lines) != 1 || lines[0]["kind"] != "summary" {
		t.Fatalf("want only a summary line, got %v", lines)
	}
	if lastSummary(t, out)["ok"] != true {
		t.Errorf("summary.ok = %v", lastSummary(t, out)["ok"])
	}
}

func TestCheckDirectory(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"pkg/a.gom":    "package pkg\n\nfn a () -> Int = 1\n",
		"pkg/b.gom":    "package pkg\n\nfn b () -> Int = nope\n",
		"pkg/pkg.gomi": "package pkg\n\nfn a () -> Int\nfn b () -> Int\n",
	})
	code, out, _ := invoke("check", filepath.Join(dir, "pkg"))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout=%q", code, out)
	}
	lines := jsonLines(t, out)
	if len(lines) != 2 { // b.gom's E0201 + summary
		t.Fatalf("got %d lines: %v", len(lines), lines)
	}
	span, _ := lines[0]["span"].(map[string]any)
	if !strings.HasSuffix(span["file"].(string), "b.gom") {
		t.Errorf("diagnostic file = %v, want b.gom", span["file"])
	}
	sum := lastSummary(t, out)
	files, _ := sum["files"].([]any)
	if len(files) != 3 { // a.gom + b.gom + i.gomi all processed
		t.Errorf("summary.files = %v, want 3 entries", files)
	}
}

func TestCheckInterfaceFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"i.gomi": "package shapes\n\ntype Shape\nfn area (s: Shape) -> Float\n",
	})
	code, out, errout := invoke("check", filepath.Join(dir, "i.gomi"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, out, errout)
	}
	if lastSummary(t, out)["ok"] != true {
		t.Errorf("summary = %v", lastSummary(t, out))
	}
}

func TestBuildWritesGo(t *testing.T) {
	dir := writeFiles(t, map[string]string{"m.gom": goodGom})
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	code, out, _ := invoke("build", "-o", outDir, filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q", code, out)
	}
	gen := filepath.Join(outDir, "m.go")
	src, err := os.ReadFile(gen)
	if err != nil {
		t.Fatalf("generated file missing: %v", err)
	}
	if !strings.Contains(string(src), "package main") || !strings.Contains(string(src), "func answer") {
		t.Errorf("generated source unexpected: %s", src)
	}
	sum := lastSummary(t, out)
	outputs, _ := sum["outputs"].([]any)
	if len(outputs) != 1 || outputs[0] != gen {
		t.Errorf("summary.outputs = %v, want [%s]", outputs, gen)
	}
}

// TestBuildSkipsTestCode is the spec 11.6 regression: `gom build`
// type- and pattern-checks `test` blocks (and the assert/assert_eq
// builtins inside them) but emits no test code — no lowering E0901,
// no _test.go output.
func TestBuildSkipsTestCode(t *testing.T) {
	src := `package m

@test
fn helper (x: Int) -> Int = x + 1

fn main () -> Unit = ()

test "arith" {
  assert_eq (helper 1) 2
  assert ((1 + 1) == 2)
}
`
	dir := writeFiles(t, map[string]string{
		"m.gom":  src,
		"m.gomi": "package m\n\nfn main () -> Unit\n",
	})
	code, out, _ := invoke("build", filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "m.go")); err != nil {
		t.Errorf("m.go not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "m_test.go")); !os.IsNotExist(err) {
		t.Errorf("m_test.go was written under gom build: %v", err)
	}
	gen, err := os.ReadFile(filepath.Join(dir, "m.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"TestArith", "helper", "AssertEq"} {
		if strings.Contains(string(gen), s) {
			t.Errorf("generated source contains test code %q:\n%s", s, gen)
		}
	}
}

// TestBuildEqBudgetTerminates is the mint-budget regression:
// `type W a = {next: List (W (W a))}` is a self-application whose
// expansion grows at every level — `x == y` on it used to mint
// comparators forever (gom check passed; gom build hung). The
// eqDeclLimit cap must answer E0906 inside the timeout instead.
func TestBuildEqBudgetTerminates(t *testing.T) {
	dir := writeFiles(t, map[string]string{"w.gom": `package ex

type W a = {next: List (W (W a))}

fn eq (x: W Int) (y: W Int) -> Bool = x == y
`})
	type result struct {
		code int
		out  string
	}
	done := make(chan result, 1)
	go func() {
		code, out, _ := invoke("build", filepath.Join(dir, "w.gom"))
		done <- result{code, out}
	}()
	select {
	case r := <-done:
		if r.code != 1 {
			t.Fatalf("exit code = %d, want 1 (diagnostic); out=%q", r.code, r.out)
		}
		if !strings.Contains(r.out, `"E0906"`) {
			t.Fatalf("output lacks E0906: %q", r.out)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("gom build did not return within 90s — comparator minting diverged")
	}
}

func TestBuildDefaultOutputBesideInput(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"m.gom":  "package m\n\nfn f () -> Int = 1\n",
		"m.gomi": "package m\n\nfn f () -> Int\n",
	})
	code, _, _ := invoke("build", filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "m.go")); err != nil {
		t.Errorf("m.go not written beside m.gom: %v", err)
	}
}

func TestBuildDoesNotWriteOnErrors(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"bad.gom":  badGom,
		"good.gom": "package m\n\nfn f () -> Int = 1\n",
	})
	code, out, _ := invoke("build", filepath.Join(dir, "bad.gom"), filepath.Join(dir, "good.gom"))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.go")); !os.IsNotExist(err) {
		t.Errorf("bad.go was written despite diagnostics: %v", err)
	}
	// The clean sibling still produces output.
	if _, err := os.Stat(filepath.Join(dir, "good.go")); err != nil {
		t.Errorf("good.go missing: %v", err)
	}
	sum := lastSummary(t, out)
	outputs, _ := sum["outputs"].([]any)
	if len(outputs) != 1 {
		t.Errorf("summary.outputs = %v, want 1 entry", outputs)
	}
}

func TestCheckRejectsNonGomFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{"note.txt": "not gom\n"})
	code, _, errout := invoke("check", filepath.Join(dir, "note.txt"))
	if code != 2 || !strings.Contains(errout, "not a .gom source file") {
		t.Errorf("exit = %d stderr = %q; want 2 and a not-a-.gom error", code, errout)
	}
}

func TestBuildRejectsInterfaceFile(t *testing.T) {
	dir := writeFiles(t, map[string]string{"i.gomi": "package p\n\nfn f (x: Int) -> Int\n"})
	code, _, errout := invoke("build", filepath.Join(dir, "i.gomi"))
	if code != 2 || !strings.Contains(errout, "interface file") {
		t.Errorf("exit = %d stderr = %q; want 2 and an interface-file error", code, errout)
	}
}

func TestBuildOutFileRequiresSingleInput(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.gom": "package m\n\nfn a () -> Int = 1\n",
		"b.gom": "package m\n\nfn b () -> Int = 2\n",
	})
	code, _, errout := invoke("build",
		"-o", filepath.Join(dir, "x.go"),
		filepath.Join(dir, "a.gom"), filepath.Join(dir, "b.gom"))
	if code != 2 || !strings.Contains(errout, "single input") {
		t.Errorf("exit = %d stderr = %q; want 2 and a single-input error", code, errout)
	}
}

func TestBuildUnknownFormat(t *testing.T) {
	dir := writeFiles(t, map[string]string{"a.gom": "package m\n\nfn a () -> Int = 1\n"})
	code, _, errout := invoke("build", "-format", "xml", filepath.Join(dir, "a.gom"))
	if code != 2 || !strings.Contains(errout, "unknown -format") {
		t.Errorf("exit = %d stderr = %q; want 2 and unknown-format error", code, errout)
	}
}

func TestDumpPhases(t *testing.T) {
	dir := writeFiles(t, map[string]string{"a.gom": goodGom})
	f := filepath.Join(dir, "a.gom")

	for phase, want := range map[string]string{
		"syntax":  "(file@",         // sexp over the surface AST
		"resolve": "(resolved",      // resolved wrapper node
		"check":   ":(Unit -> Int)", // typed signatures annotate nodes
		"core":    "(dswitch",       // decision tree, only in core
	} {
		code, out, _ := invoke("dump", "-phase", phase, f)
		if code != 0 {
			t.Errorf("dump -phase=%s: exit %d, want 0", phase, code)
			continue
		}
		if !strings.Contains(out, want) {
			t.Errorf("dump -phase=%s: stdout missing %q: %q", phase, want, out)
		}
	}

	// On a bad input the dump stops at the failing phase's boundary:
	// diagnostics go to stderr, stdout stays empty, exit is 1.
	badDir := writeFiles(t, map[string]string{"bad.gom": badGom})
	code, out, errout := invoke("dump", "-phase", "core", filepath.Join(badDir, "bad.gom"))
	if code != 1 {
		t.Errorf("dump on bad input: exit %d, want 1", code)
	}
	if out != "" {
		t.Errorf("dump on bad input: stdout = %q, want empty (no core)", out)
	}
	if !strings.Contains(errout, "E0201") {
		t.Errorf("dump on bad input: stderr missing diagnostic: %q", errout)
	}
	if _, _, e := invoke("dump", "-phase", "bogus", f); !strings.Contains(e, "unknown phase") {
		t.Errorf("unknown phase: stderr = %q", e)
	}

	// A .gomi reaches check but has bodiless fns — dump -phase=check
	// must not panic on the nil Body (regression: treedump.texpr used
	// to hit panic("unreachable") on it).
	gomiDir := writeFiles(t, map[string]string{"sig.gomi": "package p\n\nfn f (x: Int) -> Int\n"})
	code, out, _ = invoke("dump", "-phase", "check", filepath.Join(gomiDir, "sig.gomi"))
	if code != 0 {
		t.Errorf("dump -phase=check on .gomi: exit %d, want 0", code)
	}
	if !strings.Contains(out, "(?)") {
		t.Errorf("dump -phase=check on .gomi: stdout missing nil-body marker: %q", out)
	}
}

// TestEndToEnd_BuildAndRun is the full proof: gom build emits .go —
// `fn main () -> Unit` in `package main` becomes Go's `func main()`
// (core.FuncDecl.IsEntry) — the real Go toolchain compiles the module
// with no hand-written shim, and the binary runs and prints the
// answer.
func TestEndToEnd_BuildAndRun(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := writeFiles(t, map[string]string{"prog.gom": mainGom})
	outDir := filepath.Join(dir, "mod")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	code, out, errout := invoke("build", "-o", outDir, filepath.Join(dir, "prog.gom"))
	if code != 0 {
		t.Fatalf("gom build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}

	// The generated file carries the whole program — the module only
	// needs a go.mod pointing at the gom module (rt/stdlib) via this
	// worktree.
	gomod := "module e2e\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(outDir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(outDir, "prog")
	buildCmd := exec.Command(gobin, "build", "-o", bin, ".")
	buildCmd.Dir = outDir
	buildCmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if b, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	runCmd := exec.Command(bin)
	if b, err := runCmd.CombinedOutput(); err != nil {
		t.Fatalf("running binary: %v\n%s", err, b)
	} else if !strings.Contains(string(b), "gom answer: 42") {
		t.Fatalf("binary output %q, want %q", b, "gom answer: 42")
	}
}

// messyGom is deliberately unformatted: irregular spacing and no
// blank line after the package clause.
const messyGom = `package main
fn  f (x:Int) ->Int=x
`

// TestFmtStdout formats one file to stdout: the formatted source is
// the whole stdout — the summary goes to stderr so
// `gom fmt a.gom > a.gom` is safe.
func TestFmtStdout(t *testing.T) {
	dir := writeFiles(t, map[string]string{"m.gom": messyGom})
	code, out, errout := invoke("fmt", filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("gom fmt: exit %d; stderr=%q", code, errout)
	}
	if out != "package main\n\nfn f (x: Int) -> Int = x\n" {
		t.Errorf("stdout is not exactly the formatted source: %q", out)
	}
	if sum := lastSummary(t, errout); sum["ok"] != true {
		t.Errorf("stderr summary not ok: %v", sum)
	}
}

// TestFmtWrite rewrites a dirty file in place and lists it in the
// summary's outputs; a second run finds nothing to do.
func TestFmtWrite(t *testing.T) {
	dir := writeFiles(t, map[string]string{"m.gom": messyGom})
	p := filepath.Join(dir, "m.gom")
	code, out, errout := invoke("fmt", "-w", p)
	if code != 0 {
		t.Fatalf("gom fmt -w: exit %d; stderr=%q", code, errout)
	}
	if out != "" {
		t.Errorf("gom fmt -w: stdout = %q, want empty", out)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "fn f (x: Int) -> Int = x") {
		t.Fatalf("file not formatted: %q", got)
	}
	if sum := lastSummary(t, errout); sum["ok"] != true {
		t.Errorf("summary not ok: %v", sum)
	}
	code, _, errout = invoke("fmt", "-w", p)
	if code != 0 {
		t.Fatalf("second gom fmt -w: exit %d", code)
	}
	if sum := lastSummary(t, errout); len(sum["outputs"].([]any)) != 0 {
		t.Errorf("second run should report no outputs: %v", sum)
	}
}

// TestFmtListDiffCheck covers the reporting modes: -l prints the
// path, -d prints a diff, --check fails on dirty files.
func TestFmtListDiffCheck(t *testing.T) {
	dir := writeFiles(t, map[string]string{"m.gom": messyGom})
	p := filepath.Join(dir, "m.gom")

	// -l prints exactly the path list — the message stream stays on
	// stderr so the list pipes into xargs.
	code, out, _ := invoke("fmt", "-l", p)
	if code != 0 || out != p+"\n" {
		t.Errorf("gom fmt -l: exit %d, stdout %q; want 0 and just the path", code, out)
	}
	code, out, _ = invoke("fmt", "-d", p)
	if code != 0 || !strings.Contains(out, "-fn  f (x:Int) ->Int=x") {
		t.Errorf("gom fmt -d: exit %d, stdout %q; want a - line", code, out)
	}
	if strings.Contains(out, `"kind"`) {
		t.Errorf("gom fmt -d: stdout carries message-stream JSON: %q", out)
	}
	// --check: stdout stays empty; the unformatted messages and the
	// summary go to stderr.
	code, out, errout := invoke("fmt", "--check", p)
	if code != 1 {
		t.Errorf("gom fmt --check: exit %d; want 1 for a dirty file", code)
	}
	if out != "" {
		t.Errorf("gom fmt --check: stdout = %q, want empty", out)
	}
	if !strings.Contains(errout, "unformatted") {
		t.Errorf("gom fmt --check: stderr missing unformatted message: %q", errout)
	}
	// The summary's ok tracks error diagnostics (none here); the
	// dirty signal is the unformatted messages plus exit code 1.
	if sum := lastSummary(t, errout); sum["ok"] != true {
		t.Errorf("gom fmt --check: stderr summary = %v", sum)
	}
	if code, _, _ := invoke("fmt", "-w", p); code != 0 {
		t.Fatalf("gom fmt -w: exit %d", code)
	}
	code, out, _ = invoke("fmt", "-l", p)
	if code != 0 || out != "" {
		t.Errorf("gom fmt -l after -w: exit %d, stdout %q; want clean", code, out)
	}
	code, _, _ = invoke("fmt", "--check", p)
	if code != 0 {
		t.Errorf("gom fmt --check after -w: exit %d; want 0", code)
	}
}

// TestFmtStdin feeds "-" a source on stdin and gets the formatted
// text on stdout, the message stream on stderr.
func TestFmtStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	go func() {
		w.WriteString(messyGom)
		w.Close()
	}()
	var stdout, stderr strings.Builder
	code := run([]string{"fmt", "-"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("gom fmt -: exit %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "fn f (x: Int) -> Int = x") {
		t.Errorf("stdout missing formatted source: %q", stdout.String())
	}
}

// TestFmtParseError reports diagnostics and exits 1 without writing
// anything for an unparseable file.
func TestFmtParseError(t *testing.T) {
	dir := writeFiles(t, map[string]string{"bad.gom": "package main\n\nfn = (\n"})
	p := filepath.Join(dir, "bad.gom")
	code, out, errout := invoke("fmt", "-w", p)
	if code != 1 {
		t.Errorf("gom fmt -w on syntax error: exit %d; want 1", code)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "package main\n\nfn = (\n" {
		t.Errorf("errored file was modified: %q", got)
	}
	if out != "" {
		t.Errorf("errored run wrote to stdout: %q", out)
	}
	if sum := lastSummary(t, errout); sum["ok"] != false {
		t.Errorf("summary should not be ok: %v", sum)
	}
}

// strconvGom is the canonical P1 program (P1-13, the shape the V-1
// tutorial builds on): `import go "strconv"`, `|?> sequencing of
// Result-returning Go calls, and a match on the Result.
const strconvGom = `package main

import go "fmt"
import go "strconv"

fn parse_sum (a: String) (b: String) -> Result Int GoError =
  strconv.Atoi a
  |?> fn x -> strconv.Atoi b
  |?> fn y -> Ok (x + y)

fn main () -> Unit = {
  match parse_sum "40" "2" {
    Ok n -> { let _ = fmt.Println(strconv.Itoa(n)); () }
    Err _ -> { let _ = fmt.Println("parse failed"); () }
  }
}
`

// TestEndToEnd_Strconv proves the canonical P1 program survives the
// whole toolchain: `gom build` emits the .go file, `go vet` accepts
// it, `go build` compiles it, and the binary prints "42".
func TestEndToEnd_Strconv(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := writeFiles(t, map[string]string{"strconv_demo.gom": strconvGom})
	outDir := filepath.Join(dir, "mod")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	code, out, errout := invoke("build", "-o", outDir, filepath.Join(dir, "strconv_demo.gom"))
	if code != 0 {
		t.Fatalf("gom build: exit %d; stdout=%q stderr=%q", code, out, errout)
	}

	gomod := "module e2e\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(outDir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")

	vetCmd := exec.Command(gobin, "vet", ".")
	vetCmd.Dir = outDir
	vetCmd.Env = env
	if b, err := vetCmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet: %v\n%s", err, b)
	}

	bin := filepath.Join(outDir, "prog")
	buildCmd := exec.Command(gobin, "build", "-o", bin, ".")
	buildCmd.Dir = outDir
	buildCmd.Env = env
	if b, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
	runCmd := exec.Command(bin)
	if b, err := runCmd.CombinedOutput(); err != nil {
		t.Fatalf("running binary: %v\n%s", err, b)
	} else if strings.TrimSpace(string(b)) != "42" {
		t.Fatalf("binary output %q, want %q", b, "42")
	}
}

// midblockGom has one E0402: a non-Unit expression mid-block, which
// carries the safe "discard the value with `ignore`" fix.
const midblockGom = `package main

fn compute (n: Int) -> Int = n + 1

fn main () -> Unit = {
  let x = compute 1
  x + compute 2
  ()
}
`

// TestFixAppliesSafeFix is the gom fix end-to-end: the E0402 safe
// fix lands in the source as `ignore (...)`, the stream reports one
// "applied" object plus the summary, and the fixed file checks
// clean.
func TestFixAppliesSafeFix(t *testing.T) {
	dir := writeFiles(t, map[string]string{"m.gom": midblockGom})
	p := filepath.Join(dir, "m.gom")

	code, out, errout := invoke("fix", p)
	if code != 0 {
		t.Fatalf("gom fix: exit %d; stdout=%q stderr=%q", code, out, errout)
	}
	lines := jsonLines(t, out)
	if len(lines) != 2 {
		t.Fatalf("want applied + summary, got %v", lines)
	}
	a := lines[0]
	if a["kind"] != "applied" || a["code"] != "E0402" ||
		a["title"] != "discard the value with `ignore`" {
		t.Errorf("unexpected applied message: %v", a)
	}
	edits, _ := a["edits"].([]any)
	if len(edits) != 2 {
		t.Fatalf("applied edits = %v, want the two insertions", edits)
	}
	sum := lastSummary(t, out)
	if sum["ok"] != true {
		t.Errorf("summary = %v", sum)
	}
	outputs, _ := sum["outputs"].([]any)
	if len(outputs) != 1 || outputs[0] != p {
		t.Errorf("summary.outputs = %v, want [%s]", outputs, p)
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "ignore (x + compute 2)") {
		t.Fatalf("fix not applied: %q", got)
	}
	// The fixed file checks clean.
	if code, cout, _ := invoke("check", p); code != 0 {
		t.Fatalf("gom check after fix: exit %d; stdout=%q", code, cout)
	}
	// A second run has nothing left to apply.
	code, out, _ = invoke("fix", p)
	if code != 0 {
		t.Fatalf("second gom fix: exit %d", code)
	}
	if lines := jsonLines(t, out); len(lines) != 1 || lines[0]["kind"] != "summary" {
		t.Errorf("second run should emit only the summary: %v", lines)
	}
}

// TestFixSkipsNonSafe: placeholder fixes are not applied — the
// E0301 "add the missing arms" suggestion stays a suggestion and the
// stream is just the summary.
func TestFixSkipsNonSafe(t *testing.T) {
	src := `package main

type Opt = | Some Int | None

fn is_some (o: Opt) -> Bool =
  match o {
    Some _ -> true
  }
`
	dir := writeFiles(t, map[string]string{"m.gom": src})
	p := filepath.Join(dir, "m.gom")

	code, out, _ := invoke("fix", p)
	if code != 0 {
		t.Fatalf("gom fix: exit %d; stdout=%q", code, out)
	}
	if lines := jsonLines(t, out); len(lines) != 1 || lines[0]["kind"] != "summary" {
		t.Fatalf("want only a summary line, got %v", lines)
	}
	got, _ := os.ReadFile(p)
	if string(got) != src {
		t.Errorf("file was modified by a placeholder fix: %q", got)
	}
	if sum := lastSummary(t, out); len(sum["outputs"].([]any)) != 0 {
		t.Errorf("summary.outputs = %v, want empty", sum["outputs"])
	}
}

// TestFixText renders the applied stream in text format: an
// "applied[CODE]" block per fix ending in the text trailer.
func TestFixText(t *testing.T) {
	dir := writeFiles(t, map[string]string{"m.gom": midblockGom})
	code, out, _ := invoke("fix", "-format", "text", filepath.Join(dir, "m.gom"))
	if code != 0 {
		t.Fatalf("gom fix -format text: exit %d", code)
	}
	if !strings.Contains(out, "applied[E0402]: discard the value with `ignore`") {
		t.Errorf("text output missing applied block: %q", out)
	}
	if !strings.Contains(out, "--> ") || !strings.HasSuffix(out, "success\n") {
		t.Errorf("text output shape unexpected: %q", out)
	}
}

// TestExplainCode: `gom explain E0301` emits one explanation object
// plus the closing summary; lowercase codes are accepted.
func TestExplainCode(t *testing.T) {
	code, out, errout := invoke("explain", "e0301")
	if code != 0 {
		t.Fatalf("gom explain: exit %d; stderr=%q", code, errout)
	}
	lines := jsonLines(t, out)
	if len(lines) != 2 {
		t.Fatalf("want explanation + summary, got %v", lines)
	}
	e := lines[0]
	if e["kind"] != "explanation" || e["code"] != "E0301" ||
		e["severity"] != "error" || e["title"] != "non-exhaustive match" {
		t.Errorf("unexpected explanation: %v", e)
	}
	for _, k := range []string{"phase", "spec", "explanation", "example"} {
		if _, ok := e[k]; !ok {
			t.Errorf("explanation missing %q: %v", k, e)
		}
	}
	if _, ok := e["retired"]; ok {
		t.Errorf("active code should omit retired: %v", e)
	}
	if sum := lastSummary(t, out); sum["ok"] != true {
		t.Errorf("summary = %v", sum)
	}
}

// TestExplainCodeErrors: malformed and unregistered codes are usage
// errors (exit 2), not diagnostics.
func TestExplainCodeErrors(t *testing.T) {
	for _, tt := range []struct {
		arg  string
		want string
	}{
		{"bogus", "malformed code"},
		{"E9999", "no diagnostic code E9999"},
	} {
		code, out, errout := invoke("explain", tt.arg)
		if code != 2 || !strings.Contains(errout, tt.want) {
			t.Errorf("explain %s: exit %d stderr %q; want 2 and %q",
				tt.arg, code, errout, tt.want)
		}
		if out != "" {
			t.Errorf("explain %s: stdout = %q, want empty", tt.arg, out)
		}
	}
}

// TestExplainList: bare `gom explain` lists every registered code —
// full explanation objects in JSON, a compact index in text.
func TestExplainList(t *testing.T) {
	code, out, _ := invoke("explain")
	if code != 0 {
		t.Fatalf("gom explain: exit %d", code)
	}
	lines := jsonLines(t, out)
	if len(lines) < 2 {
		t.Fatalf("want explanations + summary, got %v", lines)
	}
	if sum := lastSummary(t, out); sum["kind"] != "summary" {
		t.Fatalf("stream must end with summary: %v", sum)
	}
	seen := map[string]bool{}
	for _, e := range lines[:len(lines)-1] {
		if e["kind"] != "explanation" {
			t.Fatalf("non-explanation line in the stream: %v", e)
		}
		seen[e["code"].(string)] = true
	}
	for _, c := range []string{"E0301", "E0402", "E0305"} {
		if !seen[c] {
			t.Errorf("index stream missing %s", c)
		}
	}

	code, out, _ = invoke("explain", "-format", "text")
	if code != 0 {
		t.Fatalf("gom explain -format text: exit %d", code)
	}
	if !strings.Contains(out, "E0301  non-exhaustive match\n") {
		t.Errorf("text index missing E0301: %q", out)
	}
}

// TestSchema emits the message format's JSON Schema on stdout: a
// versioned document covering every kind of the spec section 12
// vocabulary.
func TestSchema(t *testing.T) {
	code, out, errout := invoke("schema")
	if code != 0 {
		t.Fatalf("gom schema: exit %d; stderr=%q", code, errout)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	if doc["version"] != float64(1) || doc["$id"] != "gom:schema:message:1" {
		t.Errorf("schema identity = %v %v", doc["version"], doc["$id"])
	}
	defs, _ := doc["$defs"].(map[string]any)
	for _, k := range []string{"diagnostic", "summary", "applied", "explanation"} {
		if _, ok := defs[k]; !ok {
			t.Errorf("$defs missing %q", k)
		}
	}
	if code, _, errout := invoke("schema", "extra"); code != 2 || !strings.Contains(errout, "no arguments") {
		t.Errorf("gom schema extra: exit %d stderr %q; want usage error", code, errout)
	}
}

// TestInfoDraftWritesGomi exercises `gom info`: a package without an
// interface file gets a draft .gomi generated beside its sources, and
// a subsequent `gom check` accepts implementation and interface.
func TestInfoDraftWritesGomi(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"shapes/shapes.gom": `package shapes

type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)

fn area (s: Shape) -> Float = match s {
  Circle r -> r;
  Rect w h -> w;
}
`,
	})
	code, out, errout := invoke("info", filepath.Join(dir, "shapes"))
	if code != 0 {
		t.Fatalf("gom info: exit %d; stderr=%q stdout=%q", code, errout, out)
	}
	gomi, err := os.ReadFile(filepath.Join(dir, "shapes", "shapes.gomi"))
	if err != nil {
		t.Fatalf("shapes.gomi not written: %v", err)
	}
	for _, want := range []string{"package shapes", "| Circle (radius: Float)", "fn area (s: Shape) -> Float"} {
		if !strings.Contains(string(gomi), want) {
			t.Errorf("draft missing %q:\n%s", want, gomi)
		}
	}
	code, out, _ = invoke("check", filepath.Join(dir, "shapes"))
	if code != 0 {
		t.Fatalf("gom check after info: exit %d; stdout=%q", code, out)
	}
}

// TestCheckMissingInterface verifies spec 7 rule 4: a library package
// with no <package>.gomi is E0601.
func TestCheckMissingInterface(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"lib/lib.gom": "package lib\n\nfn f () -> Int = 1\n",
	})
	code, out, _ := invoke("check", filepath.Join(dir, "lib"))
	if code != 1 {
		t.Fatalf("exit %d, want 1; stdout=%q", code, out)
	}
	lines := jsonLines(t, out)
	if lines[0]["code"] != "E0601" {
		t.Errorf("first diagnostic = %v, want E0601", lines[0])
	}
}

// TestCheckInterfaceMismatch verifies spec 7 rule 6: implementation
// signatures must match the .gomi (type vars may only be renamed).
func TestCheckInterfaceMismatch(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"lib/lib.gom":  "package lib\n\nfn f () -> Int = 1\n",
		"lib/lib.gomi": "package lib\n\nfn f () -> String\nfn ghost () -> Int\n",
	})
	code, out, _ := invoke("check", filepath.Join(dir, "lib"))
	if code != 1 {
		t.Fatalf("exit %d, want 1; stdout=%q", code, out)
	}
	var codes []string
	for _, l := range jsonLines(t, out) {
		if c, ok := l["code"].(string); ok {
			codes = append(codes, c)
		}
	}
	if len(codes) != 2 || codes[0] != "E0603" || codes[1] != "E0603" {
		t.Errorf("diagnostic codes = %v, want [E0603 E0603]", codes)
	}
}
