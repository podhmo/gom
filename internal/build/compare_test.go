package build_test

// The semantics-comparison suite of docs/design.md section 7: the
// same program must mean the same thing to the evaluator and to the
// compiled pipeline. Each case declares `fn answer () -> String` —
// the evaluator applies it; `main` prints `answer ()` through
// fmt.Println so the compiled side's observable output is the same
// string. Go calls stay inside `main`: the evaluator cannot observe
// them, so they are not part of the compared surface.
//
// The cases double as regressions for bugs found while verifying
// P1: string-interpolation lowering (display/str_concat builtins),
// nested-lambda signatures hiding behind bound metas, `try`/`|?>`
// escapes through lambda boundaries, `let x = 6 * 7` binding an
// int literal to the `int64` Gom Int maps to, and applying a bound
// curried value (`let g = add3 40` then `g 2 0`) in arity chunks
// that match the emitted Go function shape.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
)

var compareCases = []struct {
	name string
	src  string
	want string // the string `answer ()` produces — eval and binary must agree
}{
	{
		name: "interp_and_int_let",
		src: `package main

import go "fmt"

fn classify (n: Int) -> String =
  if n > 40 { "big" } else { "small" }

fn answer () -> String = {
  let x = 6 * 7
  "${x} is ${classify x}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "42 is big",
	},
	{
		name: "curry_and_nested_lambda",
		src: `package main

import go "fmt"

fn add (a: Int) (b: Int) -> Int = a + b

fn answer () -> String = {
  let add40 = add 40
  let mk = fn k -> fn y -> k + y
  "${add40 2}, ${mk 1 1}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "42, 2",
	},
	{
		name: "result_escapes",
		src: `package main

import go "fmt"

fn checked (n: Int) -> Result Int String =
  if n > 100 { Err "too big" } else { Ok n }

fn accumulate (a: Int) (b: Int) -> Result Int String =
  checked a
  |?> fn x -> checked b
  |?> fn y -> checked (x + y)

fn total (a: Int) (b: Int) -> Result Int String = {
  let x = try (checked a)
  let y = try (checked b)
  Ok (x + y)
}

fn describe (r: Result Int String) -> String =
  match r {
    Ok n -> "ok ${n}"
    Err e -> "err ${e}"
  }

fn answer () -> String = {
  let good = describe (accumulate 40 2)
  let bad = describe (accumulate 40 100)
  let tried = describe (total 40 100)
  "${good} / ${bad} / ${tried}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "ok 42 / err too big / ok 140",
	},
	{
		name: "bound_curried_value",
		src: `package main

import go "fmt"

fn add3 (a: Int) (b: Int) (c: Int) -> Int = a + b + c

fn answer () -> String = {
  let g = add3 40
  let h = fn x -> g 1 x
  "${h 1} / ${g 2 0}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "42 / 42",
	},
	{
		name: "fn_returning_fn",
		src: `package main

import go "fmt"

fn add3 (a: Int) (b: Int) -> (Int -> Int) = fn c -> a + b + c

fn answer () -> String = {
  let add41 = add3 40 1
  "${add3 40 1 1} and ${add41 1}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "42 and 42",
	},
	{
		name: "match_list_recursion",
		src: `package main

import go "fmt"

fn sum (xs: List Int) -> Int =
  match xs {
    [] -> 0
    [x, ..rest] -> x + sum rest
  }

fn answer () -> String = "${sum [40, 2, 0]}"

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "42",
	},
	{
		name: "record_update",
		src: `package main

import go "fmt"

type Counter = { name: String, value: Int }

fn bump (c: Counter) -> Counter = { c with value = c.value + 2 }

fn answer () -> String = {
  let c = { name = "n", value = 40 }
  let c2 = bump c
  "${c2.name}:${c2.value}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "n:42",
	},
	{
		name: "match_guards",
		src: `package main

import go "fmt"

fn label (n: Int) -> String =
  match n {
    x if x < 0 -> "neg"
    0 -> "zero"
    x if x < 100 -> "mid"
    _ -> "big"
  }

fn answer () -> String = "${label 42}"

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "mid",
	},
	{
		// spec 3.3 structural equality: eval's eqValue and the
		// compiled rt.Equal / native == must answer the same on
		// every shape Go's `==` cannot compare natively — lists,
		// tuples holding them, records, and ADTs.
		name: "structural_equality",
		src: `package main

import go "fmt"

type Pair = { x: Int, xs: List Int }
type Shape = | Circle Int | Square (List Int)

fn answer () -> String = {
  let a = [1, 2] == [1, 2]
  let b = [1, 2] != [1, 3]
  let c = (1, [2]) == (1, [2])
  let d = { x = 1, xs = [2] } == { x = 1, xs = [2] }
  let e = Circle 1 == Circle 1
  let f = Square ([1]) != Square ([2])
  let g = [(Circle 1)] == [(Circle 1)]
  let h = (1, "x") == (1, "x")
  "${a},${b},${c},${d},${e},${f},${g},${h}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "true,true,true,true,true,true,true,true",
	},
	{
		// Top-level names are scoped to the whole file, so `let y`
		// may read `let z` declared after it. Compiled these are
		// package vars and Go initializes z first (y = 41); the
		// evaluator must force the same dependency order.
		name: "toplevel_let_forward_ref",
		src: `package main

import go "fmt"

let y = z + 1
let z = 40

fn answer () -> String = "${y}"

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "41",
	},
	{
		// The forward reference also resolves through a function
		// body: Go's package-init dependency analysis counts names
		// referenced by called functions, so z initializes before
		// y = zed () + 1. The evaluator forces z the same way.
		name: "toplevel_let_forward_ref_via_fn",
		src: `package main

import go "fmt"

fn zed () -> Int = z
let y = zed () + 1
let z = 40

fn answer () -> String = "${y}"

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "41",
	},
	{
		// Regression for the miscompile in
		// docs/problems/2026-11-02-tailloop-param-capture.md: the
		// `for` codegen rebinds parameters every iteration, so a
		// closure capturing one used to observe the final value
		// (0 below) rather than the value of the call that
		// created it (5).
		name: "tailloop_param_capture",
		src: `package main

import go "fmt"

fn keep (n: Int) (k: Unit -> Int) -> Int = {
  let kk = if n == 5 { fn () -> n } else { k }
  if n <= 0 { kk () } else { keep (n - 1) kk }
}

fn answer () -> String = "${keep 10 (fn () -> 99)}"

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "5",
	},
	{
		// P2-1 let polymorphism: `let id = fn x -> x` emits a Go
		// generic function called with explicit type arguments, and
		// `let e = []` a generic niladic thunk the evaluator forces
		// on lookup — eval and the binary must agree.
		name: "let_polymorphism",
		src: `package main

import go "fmt"

let id = fn x -> x
let e = []

fn sum (xs: List Int) -> Int = match xs {
  [] -> 0
  [x, ..rest] -> x + sum rest
}

fn answer () -> String = "${id "a"}${id 40 + id 2 + sum e}"

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "a42",
	},
	{
		// P2-1 review: a self-recursive let-lambda is *referenced*
		// (its own body consumes the symbol) yet stays monomorphic —
		// it must emit a func decl, not `var f = <func literal>`,
		// which Go's initialization-dependency analysis rejects as
		// "initialization cycle: f refers to itself".
		name: "let_self_recursion",
		src: `package main

import go "fmt"

let f = fn n -> if n == 0 { 1 } else { n * f (n - 1) }

fn answer () -> String = "${f 5}"

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "120",
	},
	{
		// A mutually-recursive pair: `even` is checked first (and
		// flips while unreferenced), `odd` is referenced by even's
		// body but stays monomorphic — both must emit func decls or
		// `var odd` initialization cycles back through even.
		name: "let_mutual_recursion",
		src: `package main

import go "fmt"

let even = fn n -> if n == 0 { "even" } else { odd (n - 1) }
let odd = fn n -> if n == 0 { "odd" } else { even (n - 1) }

fn answer () -> String = "${even 10} ${odd 10} ${even 7}"

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "even odd odd",
	},
	{
		// An annotated recursive let — `let f : Int -> Int` — is
		// referenced by its own body; the annotation binds no type
		// variables, so the monomorphic flip applies the same way.
		name: "let_annotated_recursion",
		src: `package main

import go "fmt"

let f : Int -> Int = fn n -> if n == 0 { 0 } else { 1 + f (n - 1) }

fn answer () -> String = "${f 40 + f 2}"

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "42",
	},
}

// evalAnswer applies the file's `answer` through internal/eval and
// returns the produced String's contents.
func evalAnswer(t *testing.T, r *build.Result) string {
	t.Helper()
	ev := eval.New(r.Typed)
	fn := ev.Global("answer")
	if fn == nil {
		t.Fatal("no top-level `answer` binding for the evaluator")
	}
	res := ev.Apply(fn, &eval.VUnit{})
	if !res.OK() {
		for _, d := range res.Diags {
			t.Errorf("eval: %s: %s", d.Code, d.Message)
		}
		t.FailNow()
	}
	s, ok := res.Value.(*eval.VStr)
	if !ok {
		t.Fatalf("answer evaluated to %s, want a String", res.Value)
	}
	return s.S
}

// runCompiled drops the generated file into a scratch module that
// replaces github.com/podhmo/gom with the repository, then `go run`s
// it. Skips when no Go toolchain is on PATH.
func runCompiled(t *testing.T, src []byte) string {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gomod := "module e2e\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(gobin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, src)
	}
	return string(out)
}

func TestEvalMatchesCompiled(t *testing.T) {
	for _, c := range compareCases {
		t.Run(c.name, func(t *testing.T) {
			r, err := build.Compile(
				build.Input{Path: c.name + ".gom", Src: []byte(c.src)},
				suiteOptions(),
			)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			for _, d := range r.Diagnostics {
				if d.Severity == diag.SeverityError {
					t.Fatalf("%s: %s", d.Code, d.Message)
				}
			}
			if r.Output == nil {
				t.Fatal("no generated output")
			}

			if got := evalAnswer(t, r); got != c.want {
				t.Fatalf("eval answer = %q, want %q", got, c.want)
			}
			got := strings.TrimSuffix(runCompiled(t, r.Output.Source), "\n")
			if got != c.want {
				t.Fatalf("binary stdout = %q, want %q", got, c.want)
			}
		})
	}
}
