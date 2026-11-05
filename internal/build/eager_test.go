package build_test

// Eager declaration-chunk timing
// (docs/decisions/2026-11-02-function-value-representation.md): a
// function declaration keeps its written arity, and applying its
// first chunk runs the body immediately — the returned function
// value only serves the *remaining* arrows. `stage make` therefore
// evaluates `make 20`'s body while `stage` runs: the `todo` inside
// `make` must fire even though `p`, the residual `Int -> Int`, is
// never applied. A lazy encoding (a `func()` thunk waiting for the
// last argument) would survive to print "after"; the compiled
// program and the evaluator must agree that it does not.

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
)

const eagerTimingSrc = `package main

import go "fmt"

fn make (x: Int) -> (Int -> Int) = {
  let _ : Int = todo ()
  fn y -> x + y
}

fn stage (f: Int -> Int -> Int) -> (Int -> Int) = f 20

fn answer () -> String = {
  let p = stage make
  "survived"
}

fn main () -> Unit = {
  let _ = fmt.Println "before"
  let _ = stage make
  let _ = fmt.Println "after"
  ()
}
`

func TestEagerFnChunkTiming(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.37)
	}

	r, err := build.Compile(build.Input{Path: "eager.gom", Src: []byte(eagerTimingSrc)}, suiteOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Diagnostics {
		if d.Severity == diag.SeverityError {
			t.Fatalf("compile: %s: %s", d.Code, d.Message)
		}
	}

	// The evaluator: `stage make` already runs make's body, so the
	// unused `p` binding still reaches `todo` — answer must fail
	// with the panic diagnostic, not return "survived".
	ev := eval.New(r.Typed)
	fn := ev.Global("answer")
	if fn == nil {
		t.Fatal("no top-level `answer` binding for the evaluator")
	}
	res := ev.Apply(fn, &eval.VUnit{})
	if res.OK() {
		t.Fatalf("eval answer = %v — the todo did not fire; the first chunk ran lazily", res.Value)
	}
	var sawPanic bool
	for _, d := range res.Diags {
		if d.Code == eval.CodePanic {
			sawPanic = true
		}
		t.Logf("diag %s %s", d.Code, d.Message)
	}
	if !sawPanic {
		t.Fatalf("eval answer failed without the todo panic: %+v", res.Diags)
	}

	// The compiled binary: `stage make_` calls `make_(20)` — whose
	// body panics — before "after" can print.
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip(err.Error())
	}
	out, runErr := runCompiledCmd(t, gobin, r.Output.Source)
	if runErr == nil {
		t.Fatalf("binary exited cleanly, output %q — the todo did not fire", out)
	}
	if !strings.Contains(out, "before") {
		t.Fatalf("binary output %q lacks the pre-call marker", out)
	}
	if strings.Contains(out, "after") {
		t.Fatalf("binary output %q reached past the stage call — the first chunk ran lazily", out)
	}
	if !strings.Contains(out, "gom: todo") {
		t.Fatalf("binary output %q lacks the todo panic", out)
	}
}

// Static aliases must not delay a declaration's first chunk or
// recompute a partial application's initializer each time it is used.
func TestStaticAliasEagerInitialization(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.35)
	}

	src := `package main
import go "fmt"

fn factory (x: Int) -> (Int -> Int) = {
  ignore (fmt.Println "factory ${x}")
  fn y -> x + y
}
let alias = factory
let partial = alias 40

fn main () -> Unit = {
  ignore (fmt.Println "main")
  ignore (fmt.Println (partial 2))
  ignore (fmt.Println (partial 3))
  let unused = alias 1
  ignore (fmt.Println "after")
  ()
}
`
	r, err := build.Compile(build.Input{Path: "alias_eager.gom", Src: []byte(src)}, suiteOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r.Output == nil {
		t.Fatalf("compile diagnostics: %+v", r.Diagnostics)
	}
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip(err.Error())
	}
	out, err := runCompiledCmd(t, gobin, r.Output.Source)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	const want = "factory 40\nmain\n42\n43\nfactory 1\nafter\n"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}
