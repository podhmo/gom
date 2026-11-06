package build_test

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
)

// diff_initorder_test.go pins the eval-vs-compiled comparison for
// package initialization order (spec 5.1, decisions/
// 2026-11-06-eval-init-ready-order.md): the evaluator's ready
// frontier must pick the same next variable Go's package init does —
// the earliest-declared one whose dependencies have all initialized.
// The probes place `fmt.Println` inside top-level initializers so the
// *order* itself is observable stdout — the only channel the pick is
// visible through — and compare eval's captured stream against the
// compiled binary's, like the TestDiff* probes.
//
// Before the frontier, lazy forcing made the order diverge exactly
// here: an earlier-declared let depending on a later one dragged the
// dependency's effects ahead of an independent ready let declared
// between them.

// initOrderOneFile is the single-file probe: `a` waits on `b`
// (directly, and through zed's body); `c` and `d`'s own dependency on
// `c` are ready vars declared between them. Both sides must print
// init:c, init:d, init:b, init:a, then main.
const initOrderOneFile = `package main

import go "fmt"

let a = { ignore (fmt.Println ("init:a")); zed () + b }
let c = { ignore (fmt.Println ("init:c")); 0 }
let d = { ignore (fmt.Println ("init:d")); c }
let b = { ignore (fmt.Println ("init:b")); 1 }

fn zed () -> Int = b

fn main () -> Unit = ignore (fmt.Println ("main"))
`

// initOrderFiles is the same probe split across the unit: a.gom
// declares a and c, b.gom declares d, b and zed — the frontier's
// declaration order is lexical file order, so the ready picks are
// identical to the one-file program.
var initOrderFiles = map[string]string{
	"a.gom": `package main

import go "fmt"

let a = { ignore (fmt.Println ("init:a")); zed () + b }
let c = { ignore (fmt.Println ("init:c")); 0 }

fn main () -> Unit = ignore (fmt.Println ("main"))
`,
	"b.gom": `package main

import go "fmt"

let d = { ignore (fmt.Println ("init:d")); c }
let b = { ignore (fmt.Println ("init:b")); 1 }

fn zed () -> Int = b
`,
}

// initOrderTestDecl interleaves a `test` block between lets. eval's
// Run models the compiled `go test` binary — test entry points start
// after package init completes — so the eval side must print both
// init lines before the test's. (The compiled side is not compared
// here: `go run` emits no test functions, so only eval's own log is
// asserted — the matching compiled fact is pinned by
// TestGeneratedTestFileRuns.)
const initOrderTestDecl = `package main

import go "fmt"

let x = { ignore (fmt.Println ("init:x")); 1 }

test "t" {
  ignore (fmt.Println ("test:t"))
}

let y = { ignore (fmt.Println ("init:y")); 2 }

fn main () -> Unit = ignore (fmt.Println ("main"))
`

const initOrderWant = "init:c\ninit:d\ninit:b\ninit:a\nmain\n"

// assertInitOrder compiles src, runs the eval and compiled sides of
// the diff harness, and requires both to agree byte-for-byte on
// stdout and to equal want — the probe for a single-file program's
// initialization order.
func assertInitOrder(t *testing.T, path, src, want string) {
	t.Helper()
	opts, _ := compileOracles(t)
	if opts == nil {
		t.Skip("no go toolchain on PATH — cannot typecheck the probe")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH — the compiled side cannot run")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	r, cerr := build.Compile(build.Input{Path: path, Src: []byte(src)}, opts)
	if cerr != nil {
		t.Fatalf("compile: %v", cerr)
	}
	if r.Diagnostics.HasError() || r.Output == nil {
		t.Fatalf("probe program rejected: %v", r.Diagnostics)
	}
	eo := evalDiff(t, r, src)
	if eo.class == dUnsupported {
		t.Skipf("eval cannot stub the probe's Go calls: %s", eo.detail)
	}
	co, nondeterministic, buildErr := compiledDiff(t, gobin, root, r.Output.Source)
	if buildErr != "" {
		t.Fatalf("generated Go failed go build:\n%s", buildErr)
	}
	if nondeterministic {
		t.Skip("compiled output is nondeterministic")
	}
	if div := diffCompare(eo, co); div != "" {
		t.Fatalf("eval/compiled divergence on init-order program: %s\neval=%s %q compiled=%s %q\n--- program ---\n%s",
			div, className(eo.class), eo.stdout, className(co.class), co.stdout, src)
	}
	if eo.stdout != want {
		t.Fatalf("stdout = %q, want %q", eo.stdout, want)
	}
}

func TestDiffInitReadyOrder(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test, skipping in short mode")
	}
	t.Parallel()
	assertInitOrder(t, "initorder.gom", initOrderOneFile, initOrderWant)
}

// The ValueAlias residual decision 2026-11-06-eval-init-ready-order
// §5 recorded is closed by sharing lower's static-alias
// classification with eval's init graph: the three programs below
// are the shapes the residual was measured on. (a) An effectful
// alias initializer keeps its hidden temp var while the binding
// resolves to the wrapper — a's `f ()` call waits on what the
// wrapper body reaches (count → w), not on f's var node: compiled
// w,a,f-tmp. (b) A pure alias on a function-path loop is a FuncDecl
// alone — no var node, so g's reference back to f is a function edge
// and w is ready first: compiled w,z. (c) An aggregate-bound alias
// (destructure) routes the bound name to the wrapper while the
// shared temp still runs — the recursion path count → again → count
// stays inside functions, so tmp's own slot is the only frontier
// cost: tmp,x,z with tmp silent.
func TestDiffInitReadyOrderValueAlias(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test, skipping in short mode")
	}
	t.Parallel()

	t.Run("effectful alias initializer", func(t *testing.T) {
		t.Parallel()
		assertInitOrder(t, "initorder_aliasinit.gom", `package main

import go "fmt"

let a = { ignore (fmt.Println ("init:a")); f () }
let f = { ignore (fmt.Println ("init:f")); count }
fn count () -> Int = w
let w = { ignore (fmt.Println ("init:w")); 1 }

fn main () -> Unit = ignore (fmt.Println ("main"))
`, "init:w\ninit:a\ninit:f\nmain\n")
	})

	t.Run("pure alias on a function-path loop", func(t *testing.T) {
		t.Parallel()
		assertInitOrder(t, "initorder_aliasloop.gom", `package main

import go "fmt"

let w = { ignore (fmt.Println ("init:w")); f () }
let f = g
fn g () -> (() -> Int) = { ignore (f); count }
fn count () -> Int = 1
let z = { ignore (fmt.Println ("init:z")); 0 }

fn main () -> Unit = ignore (fmt.Println ("main"))
`, "init:w\ninit:z\nmain\n")
	})

	t.Run("aggregate-bound alias", func(t *testing.T) {
		t.Parallel()
		assertInitOrder(t, "initorder_aliaspat.gom", `package main

import go "fmt"

fn count (n: Int) -> Int = if n == 0 { 0 } else { again (n - 1) + 1 }
let (again, _) = (count, 0)
let x = { ignore (fmt.Println ("init:x")); again 2 }
let z = { ignore (fmt.Println ("init:z")); 0 }

fn main () -> Unit = ignore (fmt.Println ("main"))
`, "init:x\ninit:z\nmain\n")
	})

	// One pattern mixes an alias with a plain binding: f routes to
	// the wrapper while x keeps the projection var — w's `f () + x`
	// waits on the temp through x but treats f as a function.
	t.Run("mixed alias and plain binding", func(t *testing.T) {
		t.Parallel()
		assertInitOrder(t, "initorder_aliasmixed.gom", `package main

import go "fmt"

fn count () -> Int = 1
let (f, x) = (count, { ignore (fmt.Println ("init:tmp")); 0 })
let w = { ignore (fmt.Println ("init:w")); f () + x }
let z = { ignore (fmt.Println ("init:z")); 0 }

fn main () -> Unit = ignore (fmt.Println ("main"))
`, "init:tmp\ninit:w\ninit:z\nmain\n")
	})

	// A two-link alias chain composes: `f ()` inside a's
	// initializer reaches w transitively — f's element is g's
	// Var, g's is h's, and h's body reads w — so a waits on w
	// the way the compiled `var a` waits through the wrapper
	// call chain. A single-hop fn walk would leave a ready
	// first and print init:a before init:w.
	t.Run("alias chain reaches through two links", func(t *testing.T) {
		t.Parallel()
		assertInitOrder(t, "initorder_aliaschain.gom", `package main

import go "fmt"

let a = { ignore (fmt.Println ("init:a")); f () }
let f = g
let g = h
fn h () -> Int = w
let w = { ignore (fmt.Println ("init:w")); 1 }

fn main () -> Unit = ignore (fmt.Println ("main"))
`, "init:w\ninit:a\nmain\n")
	})
}

// assertInitOrderFiles is the multi-file form of assertInitOrder:
// srcs is the package unit keyed by file name (declaration order is
// the lexical name order spec 5.1 fixes), and the eval and compiled
// sides must agree and equal want.
func assertInitOrderFiles(t *testing.T, srcs map[string]string, want string) {
	t.Helper()
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	var ss []string
	names := make([]string, 0, len(srcs))
	for name, src := range srcs {
		ss = append(ss, src)
		names = append(names, name)
	}
	sort.Strings(names)
	gl := sharedSuiteLoader(t, ss...)

	var ins []build.Input
	for _, name := range names {
		ins = append(ins, build.Input{Path: name, Src: []byte(srcs[name])})
	}
	rs, err := build.CompileFiles(ins, suiteOptionsWith(gl))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var typed []*check.File
	outs := make(map[string][]byte, len(rs))
	for i, r := range rs {
		for _, d := range r.Diagnostics {
			if d.Severity == diag.SeverityError {
				t.Fatalf("%s: %s: %s", ins[i].Path, d.Code, d.Message)
			}
		}
		if r.Output == nil {
			t.Fatalf("%s: no generated output", ins[i].Path)
		}
		typed = append(typed, r.Typed)
		name := strings.TrimSuffix(filepath.Base(ins[i].Path), ".gom") + ".go"
		outs[name] = r.Output.Source
	}

	// eval side: the same shape evalDiff drives for one file —
	// NewFiles over every typed file, stubbed Go members, captured
	// stdout.
	var out strings.Builder
	capture := &diffCapture{out: &out}
	ev := eval.NewFiles(typed)
	for path, src := range srcs {
		ev.SetLineIndexFor(path, diag.NewLineIndex([]byte(src)))
	}
	ev.SetMaxSteps(diffEvalFuel)
	bindGoStubs(t, ev, capture, typed...)
	res := ev.Run()
	if capture.unfaithful {
		t.Skip("a Go call observed a value the harness cannot render faithfully")
	}
	if diag.List(res.Diags).HasError() {
		t.Fatalf("eval failed: %v", res.Diags)
	}
	eo := out.String()

	bout, err := runCompiledFilesCmd(t, gobin, outs)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s", err, bout)
	}
	if eo != bout {
		t.Fatalf("init-order stdout diverges: eval=%q compiled=%q", eo, bout)
	}
	if eo != want {
		t.Fatalf("stdout = %q, want %q", eo, want)
	}
}

func TestDiffInitReadyOrderFiles(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test, skipping in short mode")
	}
	t.Parallel()
	assertInitOrderFiles(t, initOrderFiles, initOrderWant)
}

// TestDiffInitReadyOrderAliasAcrossFiles pins the classification's
// file-local boundary: a.gom's `let h = { …; f }` names a sibling
// file's alias binding, but h's own file never sees f's binding
// shape — lower keeps h an ordinary `var` (the block is its whole
// initializer, element or not), so eval must too. A classification
// that scanned the unit's lets indiscriminately would mark h an
// effectful alias and let v call it before h's effects ran —
// v,h,z against the compiled h,v,z.
func TestDiffInitReadyOrderAliasAcrossFiles(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test, skipping in short mode")
	}
	t.Parallel()
	assertInitOrderFiles(t, map[string]string{
		"a.gom": `package main

import go "fmt"

let v = { ignore (fmt.Println ("init:v")); h () }
let h = { ignore (fmt.Println ("init:h")); f }

fn main () -> Unit = ignore (fmt.Println ("main"))
`,
		"b.gom": `package main

import go "fmt"

let f = g
fn g () -> Int = 1
let z = { ignore (fmt.Println ("init:z")); 0 }
`,
	}, "init:h\ninit:v\ninit:z\nmain\n")
}

// TestDiffInitReadyOrderTestDecl pins the eval side of the test-decl
// half of the model: a `test` block runs after every package let has
// initialized, not at its declaration position. Written as a real
// source through build.Compile so the TestDecl comes from the
// checker, not a hand-built AST.
func TestDiffInitReadyOrderTestDecl(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test, skipping in short mode")
	}
	t.Parallel()

	opts, _ := compileOracles(t)
	if opts == nil {
		t.Skip("no go toolchain on PATH — cannot typecheck the probe")
	}
	r, cerr := build.Compile(build.Input{Path: "initorder_test.gom", Src: []byte(initOrderTestDecl)}, opts)
	if cerr != nil {
		t.Fatalf("compile: %v", cerr)
	}
	if r.Diagnostics.HasError() || r.Output == nil {
		t.Fatalf("probe program rejected: %v", r.Diagnostics)
	}
	eo := evalDiff(t, r, initOrderTestDecl)
	if eo.class == dUnsupported {
		t.Skipf("eval cannot stub the probe's Go calls: %s", eo.detail)
	}
	if eo.class != dClean {
		t.Fatalf("eval class = %s (%s), want clean", className(eo.class), eo.detail)
	}
	if want := "init:x\ninit:y\ntest:t\nmain\n"; eo.stdout != want {
		t.Fatalf("stdout = %q, want %q", eo.stdout, want)
	}
}
