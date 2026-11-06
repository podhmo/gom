package build_test

import (
	"os/exec"
	"path/filepath"
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

func TestDiffInitReadyOrder(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test, skipping in short mode")
	}
	t.Parallel()

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
	r, cerr := build.Compile(build.Input{Path: "initorder.gom", Src: []byte(initOrderOneFile)}, opts)
	if cerr != nil {
		t.Fatalf("compile: %v", cerr)
	}
	if r.Diagnostics.HasError() || r.Output == nil {
		t.Fatalf("probe program rejected: %v", r.Diagnostics)
	}
	eo := evalDiff(t, r, initOrderOneFile)
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
			div, className(eo.class), eo.stdout, className(co.class), co.stdout, initOrderOneFile)
	}
	if eo.stdout != initOrderWant {
		t.Fatalf("stdout = %q, want %q", eo.stdout, initOrderWant)
	}
}

func TestDiffInitReadyOrderFiles(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test, skipping in short mode")
	}
	t.Parallel()

	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	var srcs []string
	for _, src := range initOrderFiles {
		srcs = append(srcs, src)
	}
	gl := sharedSuiteLoader(t, srcs...)

	var ins []build.Input
	for _, name := range []string{"a.gom", "b.gom"} {
		ins = append(ins, build.Input{Path: name, Src: []byte(initOrderFiles[name])})
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
	for path, src := range initOrderFiles {
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
	if eo != initOrderWant {
		t.Fatalf("stdout = %q, want %q", eo, initOrderWant)
	}
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
