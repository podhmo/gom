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
//
// `stage`'s `f` is a declared >=2-arrow parameter — it carries the
// flat repr (docs/decisions/2026-11-18-flat-fn-params.md), so its
// partial application `f 20` defers the whole flat call: main
// applies the residual (`(stage make) 0`) for the compiled run to
// reach `make`'s body. A partial app whose residual never runs now
// skips the first chunk's effects in compiled code while the
// evaluator still fires them — the residual repr mismatch the
// decision records.

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

fn probe () -> Int = (stage make) 0

fn probeLam () -> Int = {
  let stg : (Int -> Int -> Int) -> (Int -> Int) = fn f -> f 20
  let _ = stg make
  7
}

fn main () -> Unit = {
  let _ = fmt.Println "before"
  let _ = (stage make) 0
  let _ = fmt.Println "after"
  ()
}
`

func TestEagerFnChunkTiming(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.37)
	}

	t.Parallel()

	r, err := build.Compile(build.Input{Path: "eager.gom", Src: []byte(eagerTimingSrc)}, suiteOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Diagnostics {
		if d.Severity == diag.SeverityError {
			t.Fatalf("compile: %s: %s", d.Code, d.Message)
		}
	}

	// The evaluator: `stage make` partially applies the flat
	// parameter `f`, so make's body is deferred until the residual
	// is applied — the unused `p` binding never reaches `todo` and
	// answer returns "survived", matching the compiled code
	// (docs/decisions/2026-11-19-flat-partial-eval.md).
	ev := eval.New(r.Typed)
	fn := ev.Global("answer")
	if fn == nil {
		t.Fatal("no top-level `answer` binding for the evaluator")
	}
	res := ev.Apply(fn, &eval.VUnit{})
	if !res.OK() {
		t.Fatalf("eval answer failed — the deferred flat call fired early: %+v", res.Diags)
	}
	if res.Value.String() != `"survived"` {
		t.Fatalf("eval answer = %v, want \"survived\"", res.Value)
	}

	// Applying the residual does fire make's chunk: eval `probe`
	// runs `(stage make) 0`, which saturates the flat slot — the
	// todo must panic there.
	mn := ev.Global("probe")
	if mn == nil {
		t.Fatal("no top-level `probe` binding for the evaluator")
	}
	mres := ev.Apply(mn, &eval.VUnit{})
	var sawPanic bool
	for _, d := range mres.Diags {
		if d.Code == eval.CodePanic {
			sawPanic = true
		}
		t.Logf("diag %s %s", d.Code, d.Message)
	}
	if mres.OK() || !sawPanic {
		t.Fatalf("eval probe = %v diags %+v — the saturated flat call did not fire", mres.Value, mres.Diags)
	}

	// Lambda parameters keep the canonical repr — a `let`-bound
	// `fn f -> f 20` is not a flat slot, so `stg make` fires make's
	// chunk eagerly in both eval and generated code.
	lf := ev.Global("probeLam")
	if lf == nil {
		t.Fatal("no top-level `probeLam` binding for the evaluator")
	}
	lres := ev.Apply(lf, &eval.VUnit{})
	sawPanic = false
	for _, d := range lres.Diags {
		if d.Code == eval.CodePanic {
			sawPanic = true
		}
	}
	if lres.OK() || !sawPanic {
		t.Fatalf("eval probeLam = %v diags %+v — the canonical lambda call did not fire eagerly", lres.Value, lres.Diags)
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

	t.Parallel()

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
