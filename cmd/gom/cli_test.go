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
		"pkg/a.gom":  "package pkg\n\nfn a () -> Int = 1\n",
		"pkg/b.gom":  "package pkg\n\nfn b () -> Int = nope\n",
		"pkg/i.gomi": "package pkg\n\nfn c (x: Int) -> Int\n",
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

func TestBuildDefaultOutputBesideInput(t *testing.T) {
	dir := writeFiles(t, map[string]string{"m.gom": "package m\n\nfn f () -> Int = 1\n"})
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

// strconvGom is the canonical P1 program (P1-13, the shape the V-1
// tutorial builds on): `import go "strconv"`, `|?>` sequencing of
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
