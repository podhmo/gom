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
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
	"github.com/podhmo/gom/internal/gomtest"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/testbin"
)

var compareCases = []struct {
	name string
	src  string
	want string // the string `answer ()` produces — eval and binary must agree
}{
	{
		name: "static_function_alias",
		src: `package main

import go "fmt"

fn count (n: Int) -> Int = if n == 0 { 0 } else { again (n - 1) + 1 }
let again = count
let chained = again

fn add (x: Int) (y: Int) -> Int = if x == 0 { y } else { plus (x - 1) (y + 1) }
let plus = add
let add40 = plus 40

fn apply (f: Int -> Int) (x: Int) -> Int = f x
fn answer () -> String = "${again 3}:${chained 4}:${apply chained 5}:${add40 2}"
fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "3:4:5:42",
	},
	{
		// P2-2c: the same delayed-reference cycle through a
		// destructuring let — tuple, record, and a single-variant
		// constructor all bind `count` as a named wrapper.
		name: "aggregate_function_alias",
		src: `package main

import go "fmt"

fn count (n: Int) -> Int = if n == 0 { 0 } else { again (n - 1) + 1 }
let (again, _) = (count, 0)

type Pair = {f: Int -> Int, x: Int}
let {f = ragain, x = rx} = {f = count, x = 7}

type Box = | Box (Int -> Int)
let Box g = Box count

fn apply (f: Int -> Int) (x: Int) -> Int = f x
fn answer () -> String = "${again 3}:${ragain rx}:${g 2}:${apply g 5}"
fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "3:7:2:5",
	},
	{
		// P2-2c: the rest of the traceable shapes — `as` binds two
		// names to one element, the trace descends nested tuples,
		// and a block element supplies its tail.
		name: "aggregate_alias_shapes",
		src: `package main

import go "fmt"

fn count (n: Int) -> Int = if n == 0 { 0 } else { a (n - 1) + 1 }
let (f as g, _) = (count, 0)
let ((a, _), (_, m)) = ((count, 0), (0, 41))
let (h, _) = ({ (); count }, 0)

fn answer () -> String = "${f 3}:${g 3}:${a 3}:${h 3}:${m}"
fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "3:3:3:3:41",
	},
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
		name: "poly_adts",
		src: `package main

import go "fmt"

type Pair a b = | MkPair (fst: a) (snd: b)
type Box a = {v: a}
type Tree a = | Leaf a | Node (Tree a) (Tree a)

let none = None

fn fst (p: Pair a b) -> a = p.fst
fn unbox (b: Box a) -> a = b.v

fn depth (t: Tree a) -> Int =
  match t {
    Leaf x -> 0
    Node l r -> 1 + depth l + depth r
  }

fn answer () -> String = {
  let p = MkPair 1 "one"
  let q = MkPair "two" 2
  "${fst p}:${p.snd}:${fst q}:${q.snd}:${unbox {v = 42}}:${depth (Node (Leaf 1) (Leaf 2))}"
}

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "1:one:two:2:42:1",
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
	{
		name: "pipe_result_value",
		src: `package main
import go "fmt"
fn fail (n: Int) -> Result Int String = Err "bad"
fn skipped (n: Int) -> Result Int String = todo ()
let top = 1 |?> fail |?> skipped
fn describe (r: Result Int String) -> String = match r { Ok n -> "ok ${n}"; Err e -> e }
fn unit () -> Unit = { ignore (1 |?> fail); () }
fn run () -> Result Int Bool = { ignore (1 |?> fail); Ok 42 }
fn tried () -> Result Int String = { let x = try (1 |?> fail); Ok 99 }
fn answer () -> String = {
  ignore (unit ())
  let r = 1 |?> fail |?> skipped
  let x = match (run ()) { Ok n -> n; Err e -> 0 }
  "${describe top}/${describe r}/${x}/${describe (tried ())}/continued"
}
fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "bad/bad/42/bad/continued",
	},
	{
		name: "pipe_explicit_try_boundary",
		src: `package main
import go "fmt"
fn fail () -> Result Int String = Err "outer"
fn step (n: Int) (x: Int) -> Result Int Bool = Ok (n + x)
fn run () -> Result Int String = {
  let r = 1 |?> step (try (fail ()))
  Ok 99
}
fn bad () -> Result Int Bool = Err true
fn skipped () -> Result Int String = {
  let r = bad () |?> step (try (fail ()))
  Ok 42
}
fn nested () -> Result Int String = {
  let r = 1 |?> fn n -> { let x = try (fail ()); Ok x }
  Ok 7
}
fn answer () -> String = {
  let a = match (run ()) { Ok n -> "${n}"; Err e -> e }
  let b = match (skipped ()) { Ok n -> "${n}"; Err e -> e }
  let c = match (nested ()) { Ok n -> "${n}"; Err e -> e }
  "${a}/${b}/${c}"
}
fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "outer/42/7",
	},
	{
		// The segmented lowering for a chain that carries a `try` no
		// lambda shields (P2-4b): guarded steps fuse into shared
		// boundary segments while the unguarded step still applies
		// inside the user boundary — escape destinations, skip-on-Err
		// and evaluation order must match the evaluator exactly.
		name: "pipe_try_segments",
		src: `package main
import go "fmt"
fn fail () -> Result Int String = Err "bad"
fn keep (n: Int) -> Result Int String = Ok n
fn step (n: Int) (x: Int) -> Result Int String = Ok (n + x)
fn show (r: Result Int String) -> String = match r { Ok n -> "ok ${n}"; Err e -> "err ${e}" }

// guarded - unguarded - guarded: one segment each side of the try step.
fn mid (n: Int) (r: Result Int String) -> Result Int String =
  n |?> keep |?> step (try r) |?> keep

// the unguarded step is first / last.
fn first (r: Result Int String) -> Result Int String =
  r |?> step (try (fail ())) |?> keep
fn last (r: Result Int String) -> Result Int String =
  3 |?> keep |?> step (try r)

// two unguarded steps in a row.
fn two (a: Result Int String) (b: Result Int String) -> Result Int String =
  1 |?> step (try a) |?> step (try b) |?> keep

// a try under a lambda step is shielded — the chain fuses outright.
fn lam (n: Int) (r: Result Int String) -> Result Int String =
  n |?> keep |?> (fn x -> { let v = try r; Ok (x + v) }) |?> keep

// unguarded try in the inflow expression.
fn inflow (r: Result Int String) -> Result Int String =
  (step (try r) 0) |?> keep |?> step 100

// Err inflow: every step — including the try argument — is skipped, so
// bomb runs only on the Ok path, where its Err escapes skip.
fn bomb () -> Result Int String = Err "bomb"
fn skip (r: Result Int String) -> Result Int String =
  r |?> step (try (bomb ())) |?> keep

fn answer () -> String = {
  let parts = [
    show (mid 1 (Ok 10)), show (mid 1 (Err "e1")), show (mid 1 (fail ())),
    show (first (Ok 5)), show (first (fail ())),
    show (last (Ok 7)), show (last (fail ())),
    show (two (Ok 10) (Ok 20)), show (two (Err "ea") (Ok 20)), show (two (Ok 10) (Err "eb")),
    show (lam 3 (Ok 4)), show (lam 3 (Err "el")),
    show (inflow (Ok 7)), show (inflow (Err "ei")),
    show (skip (Err "skipped")), show (skip (Ok 1))
  ]
  join parts "/"
}
fn join (xs: List String) (sep: String) -> String = match xs {
  [] -> ""
  [x] -> x
  [x, ..rest] -> "${x}${sep}${join rest sep}"
}
fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "ok 11/err e1/err bad/err bad/err bad/ok 10/err bad/ok 31/err ea/err eb/ok 7/err el/ok 107/err ei/err skipped/err bomb",
	},
	{
		// hasUnguardedTry must descend through every non-boundary
		// construct — match arms AND guards, for bodies, ignores,
		// blocks, string interpolation, tuples, lists, records,
		// unary operands, if arms/conditions and member receivers.
		// (Spread and ascription share the same single-child descent;
		// their only surface forms — Go variadic args and `Go.conv`
		// — are not evaluable, and `go`/`select` cannot hold a try
		// the evaluator can observe.)
		// A missed descent would let the step ride inside a
		// synthesized segment, where the try would escape the
		// segment instead of the user function: each chain sits in
		// non-tail position ({ ignore (...); Ok 99 }) so that mistake
		// is observable as "ok 99" instead of the escape.
		name: "pipe_try_descent",
		src: `package main
import go "fmt"
fn keep (n: Int) -> Result Int String = Ok n
fn step (n: Int) (x: Int) -> Result Int String = Ok (n + x)
fn sarg (s: String) (x: Int) -> Result Int String = Ok (x + 1)
fn tstep (p: (Int, Int)) (x: Int) -> Result Int String = { let (a, b) = p; Ok (a + b + x) }
fn lstep (xs: List Int) (x: Int) -> Result Int String = match xs { [v] -> Ok (v + x); _ -> Ok x }
type Box = { v: Int }
fn rstep (b: Box) (x: Int) -> Result Int String = Ok (b.v + x)
fn show (r: Result Int String) -> String = match r { Ok n -> "ok ${n}"; Err e -> "err ${e}" }

// try inside a match arm of a step argument.
fn marm (n: Int) (r: Result Int String) -> Result Int String = {
  ignore (n |?> keep |?> step (match r { Ok v -> v; Err e -> try (Err e) }) |?> keep)
  Ok 99
}
// try inside a match guard.
fn mguard (n: Int) (r: Result Int String) -> Result Int String = {
  ignore (n |?> keep |?> step (match n { x if (try r) > 0 -> x; _ -> 0 }) |?> keep)
  Ok 99
}
// try inside a for body — for is not a boundary.
fn fory (r: Result Int String) -> Result Int String = {
  ignore (1 |?> step ({ for x in [r] { ignore (try x) }; 7 }) |?> keep)
  Ok 99
}
// try inside an ignore inside a block argument.
fn blk (n: Int) (r: Result Int String) -> Result Int String = {
  ignore (n |?> keep |?> step ({ ignore (try r); 5 }) |?> keep)
  Ok 99
}
// try inside a string interpolation.
fn sinterp (n: Int) (r: Result String String) -> Result Int String = {
  ignore (n |?> keep |?> sarg "${try r}" |?> keep)
  Ok 99
}
// try inside tuple / list / record / ascription arguments.
fn tup (n: Int) (r: Result Int String) -> Result Int String = {
  ignore (n |?> keep |?> tstep (try r, 2) |?> keep)
  Ok 99
}
fn listy (n: Int) (r: Result Int String) -> Result Int String = {
  ignore (n |?> keep |?> lstep [try r] |?> keep)
  Ok 99
}
fn recd (n: Int) (r: Result Int String) -> Result Int String = {
  ignore (n |?> keep |?> rstep { v = try r } |?> keep)
  Ok 99
}
fn neg (n: Int) (r: Result Int String) -> Result Int String = {
  ignore (n |?> keep |?> step (-(try r)) |?> keep)
  Ok 99
}
fn ify (n: Int) (r: Result Int String) -> Result Int String = {
  ignore (n |?> keep |?> step (if (try r) > 0 { try r } else { 0 }) |?> keep)
  Ok 99
}
fn fld (n: Int) (r: Result Box String) -> Result Int String = {
  ignore (n |?> keep |?> step ((try r).v) |?> keep)
  Ok 99
}
fn answer () -> String = {
  let parts = [
    show (marm 1 (Ok 10)), show (marm 1 (Err "mx")),
    show (mguard 5 (Ok 3)), show (mguard 5 (Err "gx")),
    show (fory (Ok 4)), show (fory (Err "fx")),
    show (blk 1 (Ok 4)), show (blk 1 (Err "bx")),
    show (sinterp 1 (Ok "hi")), show (sinterp 1 (Err "sx")),
    show (tup 1 (Ok 10)), show (tup 1 (Err "tx")),
    show (listy 1 (Ok 10)), show (listy 1 (Err "lx")),
    show (recd 1 (Ok 10)), show (recd 1 (Err "rx")),
    show (neg 1 (Ok 10)), show (neg 1 (Err "ux")),
    show (ify 1 (Ok 3)), show (ify 1 (Ok 0)), show (ify 1 (Err "ix")),
    show (fld 1 (Ok { v = 10 })), show (fld 1 (Err "vx"))
  ]
  join parts "/"
}
fn join (xs: List String) (sep: String) -> String = match xs {
  [] -> ""
  [x] -> x
  [x, ..rest] -> "${x}${sep}${join rest sep}"
}
fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "ok 99/err mx/ok 99/err gx/ok 99/err fx/ok 99/err bx/ok 99/err sx/ok 99/err tx/ok 99/err lx/ok 99/err rx/ok 99/err ux/ok 99/ok 99/err ix/ok 99/err vx",
	},
	{
		name: "labeled_args",
		src: `package main

import go "fmt"

// Labeled arguments: the evaluator scatters written arguments to
// declared parameters via CallInfo.ArgToParam while lowering does
// the same through declOrderArgs — the two must agree on which
// argument fills which parameter, or the strings below diverge.
// Punning, labeled partial application (both a labeled residual and
// a positional-only one), and labeled variant payload construction
// plus patterns share the same mapping.
fn transfer (from: String) (~to: String) (~amount: Int) -> String =
  "${from}->${to}:${amount}"

type Tx =
  | Send (~to: String) (~amount: Int)
  | Noop

fn describe (t: Tx) -> String = match t {
  Send ~to: who ~amount: n -> "send ${n} to ${who}"
  Noop -> "noop"
}

fn answer () -> String = {
  let to = "carol"
  let scrambled = transfer "alice" ~amount: 100 ~to: "bob"
  let lead = transfer ~amount: 9 ~to: "zed" "yuki"
  let punned = transfer "alice" ~to ~amount: 50
  let partial = transfer "alice" ~to: "bob"
  let positional_only = transfer ~to: "dave" ~amount: 1
  let t = Send ~amount: 5 ~to: "bob"
  "${scrambled}/${lead}/${punned}/${partial ~amount: 7}/${positional_only "mallory"}/${describe t}"
}

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "alice->bob:100/yuki->zed:9/alice->carol:50/alice->bob:7/mallory->dave:1/send 5 to bob",
	},
	{
		// Go method calls and the cute `_` placeholder (spec 8.1.3,
		// 8.2): `later`'s receiver `x` is an unannotated lambda
		// parameter, so `x.AddDate` defers its calling-convention
		// decision until `later t0` binds the receiver — then the
		// call replays as CallGo with the hole as a one-argument
		// function. The evaluator binds host stubs for the method
		// symbols (receiver first) and both sides must print the
		// same date.
		name: "go_method_cute",
		src: `package main

import go "fmt"
import go "time"

let later = fn x -> x.AddDate(_, 2, 3)

fn answer () -> String = {
  let t0 = time.Unix(0, 0)
  let t1 = (later t0) 5
  "${t1.Format "2006-01-02"}/${(t0.AddDate (1, 0, 0)).Format "2006-01-02"}"
}

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "1975-03-04/1971-01-01",
	},
	{
		// Chained method calls: `x.M(1).N(2)` selects `.N` on the
		// result of `x.M(1)` — left-associative like Go
		// (decisions/2026-11-05-post-arg-selector-binding). Before
		// the fix this mis-parsed as `x.M` applied to `(1).N` and
		// `(2)`. The eval side rides the same `time.Time.AddDate`
		// stub the single-call case uses.
		name: "go_method_chain",
		src: `package main

import go "fmt"
import go "time"

fn answer () -> String = {
  let t0 = time.Unix(0, 0)
  let chained = t0.AddDate(1, 0, 0).AddDate(0, 1, 0)
  let explicit = (t0.AddDate(1, 0, 0)).AddDate(0, 1, 0)
  "${chained.Format "2006-01-02"}/${explicit.Format "2006-01-02"}"
}

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "1971-02-01/1971-02-01",
	},
	{
		// `let n = 1` where `n` solves to Float through the later
		// `+ 0.5`: the literal's checked type is Float, so codegen
		// materializes `float64(1)` — before the fix it emitted
		// `n := 1` (Go `int`) and the file failed `go build` with
		// "0.5 truncated to int". `1/2` under a Float binder is
		// Float division (0.5), never Go's untyped integer
		// division — a whole-expression `float64(...)` wrap would
		// have produced `float64(1/2)` = 0.
		name: "float_inferred_int_literal",
		src: `package main

import go "fmt"

fn f () -> Float = { let n = 1; n + 0.5 }
fn g () -> Float = { let m = 1 / 2; m + 0.5 }
fn h () -> Float = { let k = 3 * 2; k + 0.5 }

fn answer () -> String = "${f ()}/${g ()}/${h ()}"

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "1.5/1/6.5",
	},
	{
		// A Float-annotated integer literal wider than int64:
		// codegen materializes float64(99999999999999999999)
		// and `go build` accepts the constant conversion, but
		// the evaluator used to run ParseInt first and die with
		// an internal "invalid int literal" diagnostic. It must
		// answer the same float the binary prints — a decimal
		// spelling and, through the big-integer fallback, a
		// base-prefixed one.
		name: "float_annotated_huge_int_literal",
		src: `package main

import go "fmt"

let m : Float = 99999999999999999999
let w : Float = 0x1FFFFFFFFFFFFFFFFF

fn answer () -> String = "${m}/${w}"

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "1e+20/5.902958103587057e+20",
	},
	{
		// Numeric arithmetic is per-operation machine arithmetic
		// (spec 3.3, decision
		// 2026-11-05-numeric-arithmetic-per-operation): a
		// literal-only subexpression emits runtime Go, so generated
		// code can no longer fold it exactly. These are the two
		// knownDivergences registrations — `"${(869.32)*(38.29)}"`
		// and `"lit${(24e1)/(959.07)}v="` — plus the comparison
		// edges of the same fold: exact `0.1 + 0.2` equals `0.3`,
		// and `MaxInt + 1` does not compile as a Go constant at
		// all. The let-bound `x` pins that literal and non-literal
		// spellings agree. The remaining pins cover the fold's
		// other edges: `0.3 == 0.29999999999999999` merges under
		// IEEE — both literals round to the same float64, while
		// exact folding keeps them distinct and answers `false`;
		// `x / 0.0` is legal (the dividend is not constant, so no
		// E0414) and yields +Inf; `1.5e308 * 1e308` overflows
		// float64 — an invalid Go constant the binary used to be
		// rejected for, now a runtime +Inf; and the Int `-`/`/`
		// literal pairs run their machine operation.
		name: "numeric_literal_arith",
		src: `package main

import go "fmt"

fn answer () -> String = {
  let a = 869.32 * 38.29
  let b = 24e1 / 959.07
  let c = 0.1 + 0.2 == 0.3
  let d = 0.1 + 0.2 <= 0.3
  let e = 9223372036854775807 + 1
  let x = 869.32
  let f = x * 38.29
  let g = 0.3 == 0.29999999999999999
  let h = x / 0.0
  let i = 1.5e308 * 1e308
  let j = 5 - 8
  let k = 7 / 2
  "${a}/${b}/${c}/${d}/${e}/${f}/${g}/${h}/${i}/${j}/${k}"
}

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "33286.262800000004/0.2502424223466483/false/false/-9223372036854775808/33286.262800000004/true/+Inf/+Inf/-3/3",
	},
	{
		// Int32 arithmetic wraps at 32 bits (spec 3.3, decision
		// 2026-11-05-eval-int-width-aware-ops): the evaluator used
		// to compute every integer op in int64 and answered
		// 2147483648 where the int32 binary answers -2147483648.
		// The pins cover every op at and around the boundary —
		// `m / neg1` and `-m` wrap like Go's int32 (MinInt32/-1 =
		// MinInt32, no panic), a wrapped result feeds `>`/`==`
		// correctly, and the width survives a let binding, a
		// function alias, and partial application.
		name: "int32_width_arith",
		src: `package main

import go "fmt"

fn f (x: Int32) -> Int32 = x + 1
fn add (x: Int32) (y: Int32) -> Int32 = x + y

let g = f
let a : Int32 = 2147483647
let m : Int32 = -2147483648
let neg1 : Int32 = 0 - 1

fn answer () -> String = {
  let addA = add a
  "${f 2147483647}/${f (-2147483648)}/${g 2147483647}/${a + 1}/${addA 1}/${m}/${m - 1}/${a * 2}/${(a + 1) > a}/${(a + 1) == m}/${-m}/${m / neg1}/${m % neg1}/${(0 - 7) % 3}"
}

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "-2147483648/-2147483647/-2147483648/-2147483648/-2147483648/-2147483648/2147483647/-2/false/true/-2147483648/-2147483648/0/-1",
	},
	{
		// Int/Int64 stay 64-bit and still wrap: the
		// -9223372036854775808 spellings are the values the
		// evaluator used to reject with an internal "invalid int
		// literal" diagnostic while the binary answered MinInt64 —
		// the magnitude exceeds int64 and must materialize through
		// uint64 wrap. `m / neg1` and `-m` wrap the same way Go's
		// int64 does (MinInt64/-1 = MinInt64).
		name: "int64_min_literal",
		src: `package main

import go "fmt"

let m : Int64 = -9223372036854775808
let neg1 : Int64 = 0 - 1
let big = -9223372036854775808

fn answer () -> String = "${m}/${m / neg1}/${m % neg1}/${-m}/${big}/${big + 1}"

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`,
		want: "-9223372036854775808/-9223372036854775808/0/-9223372036854775808/-9223372036854775808/-9223372036854775807",
	},
}

// compareUnitCases is the package-unit half of the comparison suite:
// one program spread over sibling .gom files, so the compared
// behavior depends on package-scope members a single-file evaluator
// could not see (the multi-file-eval open question, resolved by
// decisions/2026-11-06-eval-package-unit.md). Same convention as
// compareCases: `fn answer () -> String` is the surface the evaluator
// applies; `main` prints it.
var compareUnitCases = []struct {
	name  string
	files []gomtest.File
	want  string // the string `answer ()` produces — eval and binary must agree
}{
	{
		// The cross-file twin of toplevel_let_forward_ref: a.gom's
		// package var reads b.gom's z — a "later" declaration in
		// file order (a < b), so Go's dependency analysis
		// initializes z first and the evaluator must force the
		// same sibling dependency on first read.
		name: "sibling_let_init_order",
		files: []gomtest.File{
			{Path: "a.gom", Src: []byte(`package main

import go "fmt"

let y = z + 1

fn answer () -> String = "${y}"

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`)},
			{Path: "b.gom", Src: []byte(`package main

let z = 40
`)},
		},
		want: "41",
	},
	{
		// The dependency also closes through a sibling fn body and
		// a sibling type declaration: y's initializer calls b.gom's
		// zed (whose body reads b.gom's z), and answer matches on
		// w — b.gom's package var holding a b.gom ADT value. Both
		// sides must register the sibling constructors and order
		// the transitive initialization identically.
		name: "sibling_let_through_fn_and_type",
		files: []gomtest.File{
			{Path: "a.gom", Src: []byte(`package main

import go "fmt"

let y = zed () + 1

fn show (s: Score) -> String = match s { Score v -> "s${v}" }

fn answer () -> String = "${y}:${show w}"

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`)},
			{Path: "b.gom", Src: []byte(`package main

type Score = | Score (v: Int)

fn zed () -> Int = z

let z = 40
let w = Score 42
`)},
		},
		want: "41:s42",
	},
}

// evalAnswer applies the file's `answer` through internal/eval and
// returns the produced String's contents. Go members the typed file
// references and the audited stub table covers are bound first, so
// a case may call deterministic Go functions and methods (a cute
// placeholder call, a bound-method application) inside `answer`
// itself.
func evalAnswer(t *testing.T, r *build.Result, src string) string {
	t.Helper()
	return evalAnswerFiles(t, []*build.Result{r},
		map[string][]byte{r.Typed.Sp.File: []byte(src)})
}

// evalAnswerFiles is the package-unit form of evalAnswer: the
// evaluator sees every typed file of the unit (eval.NewFiles), so
// `answer` may read a sibling file's package let, call a sibling
// function or match on a sibling type's constructors. srcs maps each
// input path to its .gom source so |?> step labels resolve per file.
func evalAnswerFiles(t *testing.T, rs []*build.Result, srcs map[string][]byte) string {
	t.Helper()
	var typed []*check.File
	for _, r := range rs {
		// .gomi results carry a Typed file too, but interfaces stop
		// after check — they are not unit members and contribute no
		// declarations to the package scope.
		if !r.Interface && r.Typed != nil {
			typed = append(typed, r.Typed)
		}
	}
	ev := eval.NewFiles(typed)
	for path, src := range srcs {
		ev.SetLineIndexFor(path, diag.NewLineIndex(src))
	}
	var out strings.Builder
	capture := &diffCapture{out: &out}
	bindGoStubs(t, ev, capture, typed...)
	fn := ev.Global("answer")
	if fn == nil {
		t.Fatal("no top-level `answer` binding for the evaluator")
	}
	res := ev.Apply(fn, &eval.VUnit{})
	if capture.unfaithful {
		t.Fatal("a Go call observed a value the stub table cannot render faithfully")
	}
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
// replaces github.com/podhmo/gom with the repository, builds (or reuses)
// a native binary, and runs it. It returns the output and run error — the
// caller decides whether a non-zero exit is a failure.
func runCompiledCmd(t *testing.T, gobin string, src []byte) (string, error) {
	t.Helper()
	return runCompiledFilesCmd(t, gobin, map[string][]byte{"main.go": src})
}

// runCompiledFilesCmd is the package-unit form of runCompiledCmd:
// every generated file of the unit drops into the same scratch module
// — the shape `gom build dir/` produces — so a case whose behavior
// depends on sibling package members runs the merged package.
func runCompiledFilesCmd(t *testing.T, gobin string, files map[string][]byte) (string, error) {
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
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if mode := os.Getenv("GOM_TEST_BIN_CACHE"); mode == "off" || mode == "normalize" {
		args := []string{"run", "."}
		if mode == "normalize" {
			args = []string{"run", "-trimpath", "-buildvcs=false", "."}
		}
		cmd := exec.Command(gobin, args...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	ctx, cancel := context.WithTimeout(t.Context(), diffBuildTimeout)
	defer cancel()
	binary, err := testbin.Build(ctx, gobin, dir, env)
	if err != nil {
		return string(binary.Output), err
	}
	cmd := testbin.Command(binary.Path)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runCompiled drops the generated file into a scratch module that
// replaces github.com/podhmo/gom with the repository, then executes it.
// The binary may be reused, but the execution is never cached.
// Skips when no Go toolchain is on PATH.
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
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.85)
	}

	t.Parallel()

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
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.72)
	}

	t.Parallel()

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

// TestSpecPreambleRuns compiles the specification's opening example —
// `shapes |> List.map area |> List.sum` — and runs the generated
// binary, pinning the runtime answer 9.14159. The fixture
// testdata/spec/ok_stdlib_preamble.gom covers the diagnostic side;
// this is the executable half, proving the builtin-scope `List`
// members resolve into the Gom-written standard library end to end.
func TestSpecPreambleRuns(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.80)
	}

	t.Parallel()

	src := `package main

import go "fmt"

type Shape = Circle Float | Rect Float Float

fn area (s: Shape) -> Float =
  match s {
    Circle r -> 3.14159 * r * r
    Rect w h -> w * h
  }

fn main () -> Unit = {
  let shapes = [Circle 1.0, Rect 2.0 3.0]
  let total = shapes |> List.map area |> List.sum
  ignore (fmt.Println(total))
}
`
	r, err := build.Compile(
		build.Input{Path: "main.gom", Src: []byte(src)},
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
	got := strings.TrimSuffix(runCompiled(t, r.Output.Source), "\n")
	if got != "9.14159" {
		t.Fatalf("binary stdout = %q, want 9.14159", got)
	}
}

func TestEvalMatchesCompiled(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 5.37)
	}
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}

	// Prepare is serial and cheap once the loader is prewarmed:
	// compile+eval are in-process (~10ms per case) while a subprocess
	// `go run` costs ~0.3-0.9s. The runner harness therefore links all
	// cases into one binary and pays only ~10ms per case to execute —
	// see runner_test.go. Serial subtests keep t.Fatal attribution.
	var srcs []string
	for _, c := range compareCases {
		srcs = append(srcs, c.src)
	}
	gl := sharedSuiteLoader(t, srcs...)
	runner := newCaseRunner(t)
	prepared := map[string]runnerCase{}
	for i, c := range compareCases {
		t.Run(c.name, func(t *testing.T) {
			r, err := build.Compile(
				build.Input{Path: c.name + ".gom", Src: []byte(c.src)},
				suiteOptionsWith(gl),
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
			if got := evalAnswer(t, r, c.src); got != c.want {
				t.Fatalf("eval answer = %q, want %q", got, c.want)
			}
			prepared[c.name] = runner.add(t, i, c.name, r.Output.Source)
		})
	}
	bin := runner.build(t, gobin)

	for _, c := range compareCases {
		t.Run("exec/"+c.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := prepared[c.name]; !ok {
				t.Skipf("prepare failed; see the %s subtest", c.name)
			}
			out, err := runner.run(bin, c.name)
			got := strings.TrimSuffix(out, "\n")
			if err != nil {
				t.Fatalf("runner %s failed: %v\noutput: %s", c.name, err, out)
			}
			if got != c.want {
				t.Fatalf("binary stdout = %q, want %q", got, c.want)
			}
		})
	}
}

// TestEvalMatchesCompiledFiles is the package-unit twin of
// TestEvalMatchesCompiled: each case's files compile as one unit
// (build.CompileFiles), the evaluator sees every typed file
// (eval.NewFiles), and the generated sources drop into one scratch
// package. Two cases is too few to pay for the runner-binary link —
// each gets a plain scratch-module build+run.
func TestEvalMatchesCompiledFiles(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.20)
	}
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}

	var srcs []string
	for _, c := range compareUnitCases {
		for _, f := range c.files {
			srcs = append(srcs, string(f.Src))
		}
	}
	gl := sharedSuiteLoader(t, srcs...)

	for _, c := range compareUnitCases {
		t.Run(c.name, func(t *testing.T) {
			ins := make([]build.Input, len(c.files))
			srcs := make(map[string][]byte, len(c.files))
			for i, f := range c.files {
				ins[i] = build.Input{Path: f.Path, Src: f.Src}
				srcs[f.Path] = f.Src
			}
			rs, err := build.CompileFiles(ins, suiteOptionsWith(gl))
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
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
				name := strings.TrimSuffix(filepath.Base(ins[i].Path), ".gom") + ".go"
				outs[name] = r.Output.Source
			}
			if got := evalAnswerFiles(t, rs, srcs); got != c.want {
				t.Fatalf("eval answer = %q, want %q", got, c.want)
			}
			out, err := runCompiledFilesCmd(t, gobin, outs)
			if err != nil {
				t.Fatalf("generated program failed: %v\noutput: %s", err, out)
			}
			if got := strings.TrimSuffix(out, "\n"); got != c.want {
				t.Fatalf("binary stdout = %q, want %q", got, c.want)
			}
		})
	}
}

// TestWrongVariantFieldPanic pins the assertion semantics of spec
// 3.2 / decision 2026-11-05-variant-field-assert-owning-ctor: a
// uniquely named payload read through the sum type typechecks for
// any value of the ADT but panics at run time on a value holding a
// different variant. eval reports the contract violation as E9003;
// the binary's `s.(Shape_Circle).R` type assertion fails — both
// sides must land in the panic class (the divergent behavior read
// Rect's w slot and answered 2). A Circle value still reads
// normally — variant_named_field covers the ok path.
func TestWrongVariantFieldPanic(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.50)
	}

	t.Parallel()

	for _, tc := range []struct {
		name string
		arg  string
	}{
		{"wrong_variant", "Rect 2.0 3.0"},
		{"nullary_variant", "Empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `package main

import go "fmt"

type Shape = | Circle (r: Float) | Rect (w: Float) (h: Float) | Empty

fn getr (s: Shape) -> Float = s.r

fn answer () -> String = "${getr (` + tc.arg + `)}"

fn main () -> Unit = { ignore (fmt.Println (answer ())); () }
`
			r, err := build.Compile(
				build.Input{Path: tc.name + ".gom", Src: []byte(src)},
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

			ev := eval.New(r.Typed)
			ev.SetLineIndex(diag.NewLineIndex([]byte(src)))
			fn := ev.Global("answer")
			if fn == nil {
				t.Fatal("no top-level `answer` binding for the evaluator")
			}
			res := ev.Apply(fn, &eval.VUnit{})
			if res.OK() {
				t.Fatalf("eval answered %s, want a panic", res.Value)
			}
			var code diag.Code
			for _, d := range res.Diags {
				if d.Severity == diag.SeverityError {
					code = d.Code
				}
			}
			if code != "E9003" {
				t.Fatalf("eval diag code = %s, want E9003 (diags=%v)", code, res.Diags)
			}

			gobin, err := lookupGo(t)
			if err != nil {
				t.Skip("no go toolchain on PATH")
			}
			out, err := runCompiledCmd(t, gobin, r.Output.Source)
			if err == nil {
				t.Fatalf("binary exited cleanly with %q, want a panic", out)
			}
			if !strings.Contains(out, "interface conversion") {
				t.Fatalf("binary panic did not name the failed assertion:\n%s", out)
			}
		})
	}
}
