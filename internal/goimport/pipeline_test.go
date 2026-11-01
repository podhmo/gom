package goimport

// End-to-end proof for `import go`: a .gom source is carried through
// syntax → resolve → check → pattern → lower → codegen and the
// generated Go is compiled and run in a scratch module wired to the
// repository (the rt/stdlib packages the output imports).
//
// This is deliberately a test harness, not the build command — the
// file-level driver is P1-11.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/codegen"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/pattern"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
)

// compileSource runs the whole pipeline on one source string and
// returns the generated Go main file. Diagnostics of severity error
// fail the case; warnings are returned for assertion.
func compileSource(t *testing.T, l *Loader, src string) []byte {
	t.Helper()

	sf, d := syntax.Parse([]byte(src), "main.gom")
	if requireNoErrors(t, d) {
		t.Fatal("parse failed")
	}
	res, d := resolve.File(sf, &resolve.Config{Loader: l})
	if requireNoErrors(t, d) {
		t.Fatal("resolve failed")
	}
	cf, d := check.Check(res, l)
	if requireNoErrors(t, d) {
		t.Fatal("check failed")
	}
	if d := pattern.CheckFile(cf, pattern.Options{}); requireNoErrors(t, d) {
		t.Fatal("pattern failed")
	}
	xf, dd := lower.File(cf, &lower.Env{GoSig: l.GoSigOf})
	if requireNoErrors(t, dd) {
		t.Fatal("lower failed")
	}
	out, err := codegen.File(xf, []byte(src))
	if err != nil {
		t.Fatalf("codegen failed: %v", err)
	}
	return out.Source
}

// requireNoErrors reports and counts error diagnostics.
func requireNoErrors(t *testing.T, ds diag.List) bool {
	t.Helper()
	bad := false
	for _, d := range ds {
		if d.Severity == diag.SeverityError {
			t.Errorf("%s: %s", d.Code, d.Message)
			bad = true
		}
	}
	return bad
}

// runGenerated drops the generated file into a scratch module that
// replaces github.com/podhmo/gom with the repository, then runs it.
func runGenerated(t *testing.T, src []byte) (string, error) {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	gomod := fmt.Sprintf(`module e2e

go 1.27

require github.com/podhmo/gom v0.0.0

replace github.com/podhmo/gom => %s
`, root)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for dir := cwd; ; {
		if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the test working directory")
		}
		dir = parent
	}
}

// The headline case of the task: `try strconv.Atoi` and
// `strconv.Itoa` compile end to end and behave — `int` results
// become `int64` payload values and `int64` arguments convert back.
func TestE2EStrconv(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "strconv"
import go "fmt"

fn parse_sum (a: String) (b: String) -> Result Int GoError = {
  let x = try strconv.Atoi(a)
  let y = try strconv.Atoi(b)
  Ok (x + y)
}

fn main () -> Unit = {
  match parse_sum "40" "2" {
    Ok n -> { let _ = fmt.Println(strconv.Itoa(n)); () }
    Err _ -> { let _ = fmt.Println("err"); () }
  }
}
`
	gen := compileSource(t, l, src)
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "42\n" {
		t.Fatalf("output = %q, want \"42\\n\"\n---\n%s", out, gen)
	}
}

// A variadic Go call boxes arbitrary Gom values into `any`.
func TestE2EVariadic(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "fmt"
import go "strconv"

fn main () -> Unit = {
  let _ = fmt.Println("sum:", strconv.Itoa(40), strconv.Itoa(2))
  ()
}
`
	gen := compileSource(t, l, src)
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "sum: 40 2\n" {
		t.Fatalf("output = %q, want \"sum: 40 2\\n\"\n---\n%s", out, gen)
	}
}

// A Go function in value position needs the `int`/`int64` adapter —
// Itoa's parameter is `int` while the Gom `Int` it sees is `int64`.
func TestE2EFuncValueAdapter(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "strconv"
import go "fmt"

fn main () -> Unit = {
  let itoa = strconv.Itoa
  let _ = fmt.Println(itoa(7))
  ()
}
`
	gen := compileSource(t, l, src)
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "7\n" {
		t.Fatalf("output = %q, want \"7\\n\"\n---\n%s", out, gen)
	}
}

// A multi-result Go function aliased through `let` stays adapted:
// the reference itself lowers to an adapter closure, so the alias is
// an ordinary Gom `String -> Result Int GoError` value end to end.
// Locks in the shape the retracted problem file
// docs/problems/2026-11-04-go-fn-value-multiresult-alias.md claimed
// was broken.
func TestE2EAliasResultAdapter(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "strconv"
import go "fmt"

fn main () -> Unit = {
  let atoi = strconv.Atoi
  match atoi "7" {
    Ok n -> { let _ = fmt.Println(strconv.Itoa(n)); () }
    Err _ -> { let _ = fmt.Println("err"); () }
  }
}
`
	gen := compileSource(t, l, src)
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "7\n" {
		t.Fatalf("output = %q, want \"7\\n\"\n---\n%s", out, gen)
	}
}

// A method discovered through the named-type method set: Buffer's
// WriteString returns (int, error) → Result, String returns String.
func TestE2EMethod(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "bytes"
import go "fmt"

fn main () -> Unit = {
  let buf = bytes.NewBufferString("a")
  let _ = buf.WriteString("bc")
  let _ = fmt.Println(buf.String())
}
`
	gen := compileSource(t, l, src)
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "abc\n" {
		t.Fatalf("output = %q, want \"abc\\n\"\n---\n%s", out, gen)
	}
}

// A (T, bool) Go result becomes the (T, Bool) tuple — the pair
// shape — and destructures through a tuple pattern.
func TestE2EPairResult(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "os"
import go "fmt"

fn main () -> Unit = {
  let (v, ok) = os.LookupEnv("PATH")
  if ok {
    let _ = fmt.Println(v)
  }
}
`
	gen := compileSource(t, l, src)
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out == "" || out == "err\n" {
		t.Fatalf("output = %q, want the PATH value\n---\n%s", out, gen)
	}
}
