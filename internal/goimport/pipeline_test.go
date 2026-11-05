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
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/codegen"
	"github.com/podhmo/gom/internal/core"
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
	dd = append(dd, lower.CheckInitCycles([]*core.File{xf})...)
	dd = append(dd, lower.CheckTailRecMutual([]*core.File{xf})...)
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
	return runGeneratedWith(t, map[string]string{"main.go": string(src)})
}

// runGeneratedWith is runGenerated with extra files alongside the
// generated main.go — a Go caller exercising the generated API is
// one caller.go away.
func runGeneratedWith(t *testing.T, files map[string]string) (string, error) {
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
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
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

// A GoError value carries the universe `error` interface's method
// set: `e.Error()` resolves through MethodOf and emits `e.Error()`.
// Regression: it used to fail E0401 "GoError has no field or method".
func TestE2EGoErrorMethod(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "errors"
import go "fmt"
import go "strconv"

fn main () -> Unit = {
  match strconv.Atoi("x") {
    Ok n -> { let _ = fmt.Println(n); () }
    Err e -> {
      let _ = fmt.Println(errors.Is(e, strconv.ErrSyntax))
      let _ = fmt.Println(e.Error())
      ()
    }
  }
}
`
	gen := compileSource(t, l, src)
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	want := "true\nstrconv.Atoi: parsing \"x\": invalid syntax\n"
	if out != want {
		t.Fatalf("output = %q, want %q\n---\n%s", out, want, gen)
	}
}

// Go package vars and consts lower to qualified references — no
// "not in scope" E0901: `math.Pi` (untyped float const),
// `strconv.IntSize` (untyped int const), `os.O_RDONLY` (a typed
// `int` const the int64 conversion wraps), `time.Nanosecond` (a Go
// named type's const) and `os.Args` (a real var).
func TestE2EGoPkgVars(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "fmt"
import go "math"
import go "os"
import go "strconv"
import go "strings"
import go "time"

fn main () -> Unit = {
  let _ = fmt.Println(math.Pi)
  let _ = fmt.Println(strconv.IntSize)
  let _ = fmt.Println(os.O_RDONLY)
  let _ = fmt.Println(time.Nanosecond)
  let _ = fmt.Println(strings.Join(os.Args, " "))
  ()
}
`
	gen := compileSource(t, l, src)
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	// The int64 conversion must surround the typed `int` const.
	if !strings.Contains(string(gen), "int64(os.O_RDONLY)") {
		t.Errorf("generated source lacks int64(os.O_RDONLY):\n%s", gen)
	}
	want := "3.141592653589793\n64\n0\n1ns\n"
	if !strings.HasPrefix(out, want) {
		t.Fatalf("output = %q, want prefix %q\n---\n%s", out, want, gen)
	}
	// The remaining line is os.Args joined — the exe path, whose
	// only stable property is being non-empty.
	if rest := out[len(want):]; len(rest) == 0 {
		t.Fatalf("output ends too early: %q\n---\n%s", out, gen)
	}
}

// An untyped int constant bound by `let` still behaves as `Int`:
// the untyped default `int` is not `int64`, so the emitted
// reference is `int64(strconv.IntSize)` — without the conversion
// `sz` would bind a Go `int` and `id sz` would not compile. The
// untyped `math.MaxInt`/`math.MinInt` take the same path.
func TestE2EUntypedIntConstLet(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "fmt"
import go "math"
import go "strconv"

fn id (x: Int) -> Int = x

fn main () -> Unit = {
  let sz = strconv.IntSize
  let _ = fmt.Println(id sz + id math.MaxInt + id math.MinInt)
  ()
}
`
	gen := compileSource(t, l, src)
	for _, want := range []string{"int64(strconv.IntSize)", "int64(math.MaxInt)", "int64(math.MinInt)"} {
		if !strings.Contains(string(gen), want) {
			t.Errorf("generated source lacks %s:\n%s", want, gen)
		}
	}
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "63\n" {
		t.Fatalf("output = %q, want \"63\\n\"\n---\n%s", out, gen)
	}
}

// The headline extern case of P3-5: `flag.Lookup` returns `*Flag` —
// a nil-able pointer — and the extern declares the Gom view
// `Option (Ptr Flag)`. The generated call is the plain
// `v := flag.Lookup(name)` followed by `option.FromGo(v, v != nil)`.
func TestE2EExternPtrOption(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "flag"
import go "fmt"

extern go "flag" {
  fn Lookup (name: String) -> Option (Ptr Flag)
}

fn main () -> Unit = {
  let _ = flag.String("greeting", "hello", "doc")
  match flag.Lookup("greeting") {
    Some _ -> { let _ = fmt.Println("found"); () }
    None -> { let _ = fmt.Println("missing"); () }
  }
  match flag.Lookup("absent") {
    Some _ -> { let _ = fmt.Println("found"); () }
    None -> { let _ = fmt.Println("missing"); () }
  }
  let lookup = flag.Lookup
  match lookup("again") {
    Some _ -> { let _ = fmt.Println("found"); () }
    None -> { let _ = fmt.Println("missing"); () }
  }
}
`
	gen := compileSource(t, l, src)
	if !strings.Contains(string(gen), "option.FromGo") {
		t.Errorf("generated source lacks option.FromGo:\n%s", gen)
	}
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "found\nmissing\nmissing\n" {
		t.Fatalf("output = %q, want \"found\\nmissing\\nmissing\\n\"\n---\n%s", out, gen)
	}
}

// An extern may keep the derived view of a `(T, bool)` result too:
// declaring a tuple leaves the pair shape a plain import gives —
// the override only renames what the Gom side sees.
func TestE2EExternPairTuple(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "os"
import go "fmt"

extern go "os" {
  fn LookupEnv (key: String) -> (String, Bool)
}

fn main () -> Unit = {
  let (v, ok) = os.LookupEnv("PATH")
  if ok {
    let _ = fmt.Println("set")
  }
}
`
	gen := compileSource(t, l, src)
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "set\n" {
		t.Fatalf("output = %q, want \"set\\n\"\n---\n%s", out, gen)
	}
}

// The comma-ok override: `os.LookupEnv`'s `(string, bool)` declared
// `Option String` emits `v, ok := os.LookupEnv(...)` and
// `option.FromGo(v, ok)` — the other half of the Option rule
// (spec section 8.4).
func TestE2EExternCommaOKOption(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "os"
import go "fmt"

extern go "os" {
  fn LookupEnv (key: String) -> Option String
}

fn main () -> Unit = {
  match os.LookupEnv("PATH") {
    Some _ -> { let _ = fmt.Println("set"); () }
    None -> { let _ = fmt.Println("unset"); () }
  }
}
`
	gen := compileSource(t, l, src)
	if !strings.Contains(string(gen), ", ok") {
		t.Errorf("generated source lacks the comma-ok binding:\n%s", gen)
	}
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "set\n" {
		t.Fatalf("output = %q, want \"set\\n\"\n---\n%s", out, gen)
	}
}

// The zero-argument spelling: `fn Getpid ()` binds `func() int`
// and `os.Getpid ()` emits the plain `os.Getpid()` — the `()`
// pattern is not a counted Go parameter.
func TestE2EExternZeroArg(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "os"
import go "fmt"

extern go "os" {
  fn Getpid () -> Int
}

fn main () -> Unit = {
  let _ = fmt.Println(os.Getpid () > 0)
  ()
}
`
	gen := compileSource(t, l, src)
	if !strings.Contains(string(gen), "os.Getpid()") {
		t.Errorf("generated source lacks the niladic call:\n%s", gen)
	}
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "true\n" {
		t.Fatalf("output = %q, want \"true\\n\"\n---\n%s", out, gen)
	}
}

// The spec's second extern example: `io.Copy` redeclared with named
// labels and `Result Int64 GoError` over its (int64, error) Go
// signature. `*bytes.Buffer` values pass the interface parameters
// through the assignability conversion (spec sections 8.4, 8.5).
func TestE2EExternIoCopy(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "bytes"
import go "io"
import go "fmt"

extern go "io" {
  fn Copy (~dst: Writer) (~src: Reader) -> Result Int64 GoError
}

fn main () -> Unit = {
  let dst = bytes.NewBufferString("")
  let src = bytes.NewBufferString("hello")
  match io.Copy ~dst: dst ~src: src {
    Ok n -> { let _ = fmt.Println(n); () }
    Err _ -> { let _ = fmt.Println("err"); () }
  }
  let _ = fmt.Println(dst.String())
}
`
	gen := compileSource(t, l, src)
	if !strings.Contains(string(gen), "io.Copy(") {
		t.Errorf("generated source lacks io.Copy call:\n%s", gen)
	}
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "5\nhello\n" {
		t.Fatalf("output = %q, want \"5\\nhello\\n\"\n---\n%s", out, gen)
	}
}

// A declared List result keeps the boundary copy the derived view
// gives a `[]string` Go result — the extern restates what the
// default mapping would say (spec section 8.5's boundary copies).
func TestE2EExternSliceBoundary(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "strings"
import go "fmt"

extern go "strings" {
  fn Fields (s: String) -> List String
}

fn main () -> Unit = {
  match strings.Fields("go fast") {
    [a, b] -> { let _ = fmt.Println(a, b); () }
    _ -> { let _ = fmt.Println("nope"); () }
  }
}
`
	gen := compileSource(t, l, src)
	out, err := runGenerated(t, gen)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "go fast\n" {
		t.Fatalf("output = %q, want \"go fast\\n\"\n---\n%s", out, gen)
	}
}

// The generated API stays ordinary Go: a Go file in the same
// package calls the Gom wrapper of the extern-bound lookup and
// reads the Option result through the stdlib option type (the
// Go → generated-Gom direction of the interop test pair).
func TestE2EExternGoCaller(t *testing.T) {
	l := loaderFor(t)
	src := `package main

import go "flag"
import go "fmt"

extern go "flag" {
  fn Lookup (name: String) -> Option (Ptr Flag)
}

fn lookup_flag (name: String) -> Option (Ptr flag.Flag) = flag.Lookup(name)

fn main () -> Unit = {
  ()
}
`
	gen := compileSource(t, l, src)
	caller := `package main

import (
	"flag"
	"fmt"

	"github.com/podhmo/gom/stdlib/option"
)

func init() {
	flag.String("greeting", "hello", "doc")
	got := lookupFlag("greeting")
	switch o := got.(type) {
	case option.Option_Some[*flag.Flag]:
		fmt.Println(o.V0.Name)
	default:
		fmt.Println("none")
	}
}
`
	out, err := runGeneratedWith(t, map[string]string{"main.go": string(gen), "caller.go": caller})
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, gen)
	}
	if out != "greeting\n" {
		t.Fatalf("output = %q, want \"greeting\\n\"\n---\n%s", out, gen)
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
