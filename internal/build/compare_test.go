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
	"github.com/podhmo/gom/internal/lower"
)

var compareCases = []struct {
	name string
	src  string
	want string // the string `answer ()` produces — eval and binary must agree
}{
	{
		name: "poly_functions",
		src: `package main

import go "fmt"

fn choose (x: a) (y: a) -> a = x
let id = fn x -> x
let alias = choose

fn forward (x: Int) -> Int = forward_alias x
let forward_alias = id

fn captured (x: a) -> (a -> a) = {
  let value = x
  fn y -> value
}

fn answer () -> String = {
  let pick = choose 40
  let local = captured "captured"
  "${id "poly"}:${id 2}:${pick 2}:${alias 3 4}:${local "ignored"}:${forward 5}"
}

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "poly:2:40:3:captured:5",
	},
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
		name: "local_result_adt",
		src: `package main

import go "log"

// A file-local "type Result" shadows the builtin: its variants are
// an ordinary sealed ADT and match compiles to a Go type switch —
// the evaluator reads the same declaration, so both sides must
// agree on the ADT reading. (fmt.Println's (int, error) signature
// needs the shadowed builtin, so main prints through log.Print —
// a Unit-returning call the shadowing leaves intact.)
type Result = | Ok Int | Err String

fn describe (r: Result) -> String =
  match r {
    Ok v -> "ok ${v}"
    Err e -> "err ${e}"
  }

fn answer () -> String = {
  let e = Err "bad"
  "${describe (Ok 42)} / ${describe e}"
}

fn main () -> Unit = {
  let _ = log.SetFlags(0)
  let _ = log.Print(answer ())
  ()
}
`,
		want: "ok 42 / err bad",
	},
	{
		name: "local_option_and_builtin_result",
		src: `package main

import go "fmt"

// Shadowing is per name: a file-local "type Option" is an ordinary
// ADT while the builtin Result — undeclared here — keeps its own
// constructors, try, and |?>.
type Option = | Just Int | Nothing

fn show (o: Option) -> String =
  match o { Just v -> "just ${v}"; Nothing -> "nothing" }

fn checked (n: Int) -> Result Int String =
  if n > 0 { Ok n } else { Err "neg" }

fn describe (r: Result Int String) -> String =
  match r { Ok v -> "ok ${v}"; Err e -> "err ${e}" }

fn answer () -> String =
  "${show (Just 3)} / ${describe (checked 5)}"

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "just 3 / ok 5",
	},
	{
		name: "local_goerror_no_wrap",
		src: `package main

import go "log"

// A file-local "type GoError" shadows the builtin error channel:
// |?> must not wrap the Err payload of Result _ <local GoError> —
// check/codegen/validator gate on the declaration, and the
// evaluator must too (a name-structural read wraps and diverges).
// (fmt.Println's error-returning signature needs the shadowed
// builtin, so main prints through log.Print.)
type GoError = | MkE Int

fn tryStep (n: Int) -> Result Int GoError =
  if n > 0 { Ok n } else { Err (MkE (0 - n)) }

fn run (n: Int) -> Result Int GoError =
  Ok n
  |?> fn v -> tryStep (v - 4)

fn show (r: Result Int GoError) -> String =
  match r {
    Ok v -> "ok ${v}"
    Err (MkE c) -> "err ${c}"
  }

fn answer () -> String =
  "${show (run 5)} / ${show (run 1)}"

fn main () -> Unit = {
  let _ = log.SetFlags(0)
  let _ = log.Print(answer ())
  ()
}
`,
		want: "ok 1 / err 3",
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
		// The NaN triple (spec 3.3): scalar `==` keeps IEEE
		// semantics, so `n == n` is false — and elementwise list
		// comparison must preserve that reading even when both
		// operands are the *same slice*, where reflect.DeepEqual
		// shortcuts to true. The evaluator's eqValue always read
		// this way; the compiled comparators now agree.
		name: "structural_equality_nan",
		src: `package main

import go "fmt"

fn answer () -> String = {
  let z = 0.0
  let n = z / z
  let xs = [n]
  let a = n == n
  let b = xs == xs
  let c = [n] == [n]
  let d = xs == [n]
  "${a},${b},${c},${d}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "false,false,false,false",
	},
	{
		// `==` between narrowed operands (spec 3.3 again): `let a = A`
		// on a local sum and `(Some 1)` both bind through the ADT
		// interface, so the native `==` never meets two different
		// concrete variant structs — before the let-bound conversion
		// this compiled `Sh_A == Sh_B` literals, which Go rejects.
		name: "structural_equality_narrowed",
		src: `package main

import go "fmt"

type Sh = A | B Int

fn answer () -> String = {
  let a = A
  let b = B 1
  let x = a == b
  let y = a == A
  let z = b == B 1
  let p = (Some 1) == None
  let q = Some 1 != None
  let m = match b {
    A -> false
    B n -> n == 1
  }
  "${x},${y},${z},${p},${q},${m}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "false,true,true,false,true,true",
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
	{
		// A constructor-application scrutinee — `match (Some 1)` —
		// lowers through a `tmp := Option_Some[T]{...}` binding whose
		// `:=` used to infer the concrete variant struct, so the
		// emitted type switch was "not an interface" and the binary
		// failed to build. The binding must carry the static sum
		// interface — both for the direct scrutinee and for a ctor
		// value bound by `let`.
		name: "match_ctor_scrutinee",
		src: `package main

import go "fmt"

fn answer () -> String = {
  let x = Some 40
  let a = match (Some 1) { Some n -> n + 1; None -> 0 }
  let b = match x { Some n -> n; None -> 0 }
  "${a},${b}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "2,40",
	},
	{
		// A named variant payload read through the sum type used to
		// emit `s.R` on the sealed interface — not compilable Go.
		// check records the owning constructor on MemberExpr, so
		// codegen asserts the variant's concrete struct:
		// `s.(Shape_Circle).R`. Both a let-bound receiver (whose
		// `:=` stays interface-typed under letConv) and a parameter
		// receiver exercise the read; eval answers the same value.
		name: "variant_named_field",
		src: `package main

import go "fmt"

type Shape = | Circle (r: Float) | Empty

fn getr (s: Shape) -> Float = s.r

fn answer () -> String = {
  let s = Circle 40.0
  let t = Circle 2.0
  "${s.r + getr t}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "42",
	},
	{
		// A branching expression directly inside a variadic `...any`
		// Go argument used to have its node type unified to Go.Any,
		// which lower's invariants reject (E0901). The branch infers
		// its own type and conforms to `any`; the strconv.Itoa
		// argument pins that a concrete parameter type still reaches
		// the nested match the same way.
		name: "branch_in_any_arg",
		src: `package main

import go "fmt"
import go "strconv"

fn answer () -> String = "a b 42"

fn main () -> Unit = {
  let _ = fmt.Println (
    (match true { true -> "a"; false -> "x" }),
    (if false { "y" } else { "b" }),
    (strconv.Itoa (match 1 { 1 -> 42; _ -> 0 })))
  ()
}
`,
		want: "a b 42",
	},
	{
		// The canonical function-value representation
		// (docs/decisions/2026-11-02-function-value-representation.md):
		// `stage`'s flat parameter type `Int -> Int -> Int` and
		// `make`'s nested return type `Int -> (Int -> Int)` are the
		// same semantic type, and both emit `func(int64) func(int64)
		// int64` — `stage make` must compile. `make 1 2` adds the
		// over-application path: declared arity 1 saturates, then a
		// one-argument CallFunc takes the residual.
		name: "fn_value_repr",
		src: `package main

import go "fmt"

fn make (x: Int) -> (Int -> Int) = fn y -> x + y

fn stage (f: Int -> Int -> Int) -> (Int -> Int) = f 20

fn answer () -> String = {
  let p = stage make
  "${p 30} ${make 1 2}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "50 3",
	},
	{
		// Function values crossing every boundary a gom program
		// offers: a record field typed by the flat spelling, a
		// variant payload, a whole multi-arity declaration used as
		// a value (the eta adapter), and partial application.
		name: "fn_value_boundaries",
		src: `package main

import go "fmt"

type Rec = { f: Int -> Int -> Int, n: Int }
type Box = | Wrap (Int -> Int)

fn add2 (a: Int) (b: Int) -> Int = a + b

fn unwrap (b: Box) -> Int = match b {
  Wrap f -> f 1
}

fn answer () -> String = {
  let r = { f = add2, n = 0 }
  let g = r.f 40
  let h = add2
  let w = Wrap (add2 1)
  "${g 1} ${r.f 10 10} ${unwrap w} ${h 30 12}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "41 20 2 42",
	},
	{
		// A multi-parameter lambda whose body returns a function —
		// the flat signature has more arrows than written parameters.
		// The canonical lowering binds one written parameter per
		// level and leaves the remaining arrows to the body's own
		// lowering: a nested lambda (`fn a b -> fn c -> ...`), a
		// whole declaration value (`fn a b -> add2` — the adapter),
		// and a partial application (`fn a b -> add2 a`). The
		// degenerate path used to bind every written parameter at
		// one level, which E0901 rejects the moment a second
		// parameter is written.
		name: "lambda_returns_function",
		src: `package main

import go "fmt"

fn add2 (a: Int) (b: Int) -> Int = a + b

fn answer () -> String = {
  let f = fn a b -> fn c -> a + b + c
  let g = fn a b -> add2
  let h = fn a b -> add2 a
  "${f 1 2 3} ${g 10 20 1 2} ${h 5 6 7}"
}

fn main () -> Unit = {
  let _ = fmt.Println(answer ())
  ()
}
`,
		want: "6 3 12",
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

// lookupGo resolves the go toolchain, or reports why there is none.
func lookupGo(t *testing.T) (string, error) {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		return "", err
	}
	return gobin, nil
}

// runCompiledCmd drops the generated file into a scratch module that
// replaces github.com/podhmo/gom with the repository and runs
// `go run .`, returning the combined output and the run error — the
// caller decides whether a non-zero exit is a failure.
func runCompiledCmd(t *testing.T, gobin string, src []byte) (string, error) {
	t.Helper()
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
	return string(out), err
}

// runCompiled drops the generated file into a scratch module that
// replaces github.com/podhmo/gom with the repository, then `go run`s
// it. Skips when no Go toolchain is on PATH.
func runCompiled(t *testing.T, src []byte) string {
	t.Helper()
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	out, err := runCompiledCmd(t, gobin, src)
	if err != nil {
		t.Fatalf("generated program failed: %v\noutput: %s\n---\n%s", err, out, src)
	}
	return out
}

// TestGeneratedTestFileRuns compiles a test-mode file and runs the
// generated <pkg>_test.go under `go test` — the path the golden
// suite never exercises (it generates with Tests off). The
// assert_eq cases pin the precomputed-verdict signature of
// rt.AssertEq and the generated-comparator route for lists.
func TestGeneratedTestFileRuns(t *testing.T) {
	src := `package m

fn main () -> Unit = ()

test "scalars" {
  assert_eq (1 + 1) 2
}

test "lists" {
  let z = 0.0
  let n = z / z
  let xs = [1.0, 2.0]
  assert_eq xs xs
  assert (not (xs == [n]))
}

test "variants" {
  assert (Some 1 != None)
  assert (not ((Some 1) == None))
  match Some 1 {
    Some n -> assert (n == 1)
    None -> assert false
  }
}
`
	r, err := build.Compile(
		build.Input{Path: "m.gom", Src: []byte(src)},
		&build.Options{Lower: &lower.Env{Tests: true}},
	)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, d := range r.Diagnostics {
		if d.Severity == diag.SeverityError {
			t.Fatalf("%s: %s", d.Code, d.Message)
		}
	}
	if r.Output == nil || r.Output.TestSource == nil {
		t.Fatal("test mode produced no test source")
	}

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
	for name, src := range map[string][]byte{
		"go.mod":    []byte(gomod),
		"m.go":      r.Output.Source,
		"m_test.go": r.Output.TestSource,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(gobin, "test", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated test file failed: %v\noutput: %s\n---\n%s",
			err, out, r.Output.TestSource)
	}
}

// TestTwoFilePackageCompiles is the package-scope collision probe:
// `gom build dir/` compiles each .gom file independently but emits
// them into one Go package, so two files that both compare
// `List Int` mint the same-base `gomEq1` name — without the per-file
// tag the merged package has a redeclared function (and the wrong
// comparator could be picked cross-file). The test proves the minted
// names differ and the merged package builds.
func TestTwoFilePackageCompiles(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	compile := func(path, src string) []byte {
		r, err := build.Compile(build.Input{Path: path, Src: []byte(src)}, suiteOptions())
		if err != nil {
			t.Fatalf("compile %s: %v", path, err)
		}
		for _, d := range r.Diagnostics {
			if d.Severity == diag.SeverityError {
				t.Fatalf("%s: %s: %s", path, d.Code, d.Message)
			}
		}
		if r.Output == nil {
			t.Fatalf("%s: no generated output", path)
		}
		return r.Output.Source
	}
	// Both files compare `List Int` — the comparator signatures are
	// identical, so only the file tag keeps the names distinct.
	aSrc := compile("a.gom", `package main

import go "fmt"

fn eq (xs: List Int) (ys: List Int) -> Bool = xs == ys

fn main () -> Unit = {
  let _ = fmt.Println(eq [40, 2] [40, 2])
  ()
}
`)
	bSrc := compile("b.gom", `package main

fn beq (xs: List Int) (ys: List Int) -> Bool = xs == ys
`)

	eqName := func(src []byte) string {
		s := string(src)
		i := strings.Index(s, "func gomEq")
		if i < 0 {
			t.Fatalf("generated source mints no comparator:\n%s", s)
		}
		rest := s[i+len("func "):]
		return rest[:strings.IndexByte(rest, '(')]
	}
	an, bn := eqName(aSrc), eqName(bSrc)
	if an == bn {
		t.Fatalf("a.gom and b.gom both minted %s — package-scope collision", an)
	}
	if !strings.HasPrefix(an, "gomEq1") || !strings.HasPrefix(bn, "gomEq1") {
		t.Fatalf("comparator names %q, %q lack the gomEq<N> base", an, bn)
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gomod := "module e2e\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	for name, src := range map[string][]byte{
		"go.mod": []byte(gomod), "a.go": aSrc, "b.go": bSrc,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(gobin, "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("merged package failed: %v\noutput: %s\n---\n%s\n%s",
			err, out, aSrc, bSrc)
	} else if got := strings.TrimSpace(string(out)); got != "true" {
		t.Fatalf("program printed %q, want true", got)
	}
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
