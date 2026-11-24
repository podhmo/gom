# Gom language fundamentals

Condensed from `docs/spec.md` chapters 1–6 and 11 (normative, in
Japanese). Everything here describes today's implementation; the
code blocks are verified by `gom check`.

## File structure

```gom
package main            // mandatory first clause; one dir = one package

import go "fmt"         // Go package import
import go "os"

type Expr =             // type declaration (sum or record;
                        // `type alias X = T` gives a transparent alias)
  | Num Int
  | Add Expr Expr

let answer = 42         // top-level value binding

fn eval (e: Expr) -> Int = match e {   // function: full signature required
  Num n -> n
  Add l r -> eval l + eval r
}

test "smoke" {          // test block, same file
  assert_eq (eval (Num 1)) 1
}

fn main () -> Unit = () // package main + `fn main () -> Unit` = Go entrypoint
```

Declaration order: `package`, then `import`/`extern`, then `fn` /
`let` / `type` / `test` in any order. Top-level names are visible
throughout the file regardless of order (forward references work).
Sibling `.gom` files in the same directory are one package — their
top-level names are shared without imports (and must declare the
same `package` name, E0605). `import "mod/path"` (no `go` keyword)
pulls in another Gom package through its `.gomi` — resolved
through the module graph (any module in the build list); the
stdlib also spells `import "list"`/`"option"`/`"result"` (see
interface-files.md); `import go "path"` pulls in a real Go
package.

Names: the 22 reserved words (spec §1.8 — `as default else extern
false fn for go if ignore import in let match package select send
test true try type with`) cannot bind names — a keyword in a
binding position reports E0106. Member names after `.` accept all
keywords (`Chan.send`), as do `@` attribute names (`@test`).

## Expressions

Everything is an expression. There are no statements, no `return`,
no `break`/`continue`, no mutation of `let` bindings (shadowing via a
fresh `let` is fine), no `while`. A block `{ e1; e2; ...; en }` has
value `en`; `e1`..`e(n-1)` must have type `Unit` (E0402) — discard
deliberately with `ignore e`.

```gom
fn sign (n: Int) -> Int = {
  let abs = if n < 0 { 0 - n } else { n }   // if is an expression
  if abs == 0 { ignore abs; () } else { () } // else required unless then is Unit
  abs
}
```

- `if c { a } else { b }` — `else` may be omitted **only** when the
  then-branch is `Unit` (E0403). `else` takes a block;
  `else { if ... }` for chains.
- Literals: `42`, `0x2A`, `3.14`, `true`, `'a'` (Char), `"str"` with
  `"${expr}` interpolation. No multiline or raw strings; escapes
  `\n \t \r \0 \\ \' \" \$ \uXXXX \u{X...}`. A splice renders the
  value in the evaluator's display form: scalars print bare
  (`String` unquoted, `Char` as its code point), composites use the
  composite notation (`Ok 42`, `{ name = "n" }`, `[1, 2]`,
  `(1, 2)`, `Ctor ~name: v`), and Go values fall back to `%v`.
  A composite still mentioning a type parameter prints with Go
  formatting instead (`{1 2}` for a generic tuple element).
- `()` is the `Unit` value. Tuples `(a, b, c)` — a spelled
  tuple beyond 8 elements emits an anonymous `struct{ V0..Vn }`
  rather than `rt.TupleN`, so wide tuples still typecheck and
  destructure the same. Lists `[1, 2, 3]`
  (fixed length only). Records `{ x = 1, y = 2 }`, update
  `{ p with x = 3 }`, access `p.x`.
- Operators (tightest first): field/module access `a.b`, application
  `f x`, unary `-` `try` `ignore`, `* / %`, `+ -`, `== != < <= > >=`,
  `&&`, `||`, and lowest `|> <| |?> >> <<`. Binary ops are
  left-assoc, except `== != < <= > >=` are non-associative:
  `a < b < c` is E0101 "comparison operators do not chain" — write
  `a < b && b < c` or `(a < b) == c` instead. `f x + g y` is
  `(f x) + (g y)`; `f (-1)` needs parens (a leading `-` after an
  operand is binary minus).
- Ascription: `let x : Int = e` annotates bindings; `Go.conv e : T`
  converts (see go-interop). Bare `e : T` in expression position is
  reserved/limited — prefer `let` annotations.

## Functions and currying

```gom
fn add (a: Int) (b: Int) -> Int = a + b

let add40 = add 40                    // partial application: Int -> Int

fn twice (f: Int -> Int) (x: Int) -> Int = f (f x)

fn apply () -> Int = twice (fn n -> n + 1) 10
```

- Signature grammar: `fn name (p: T) (~label: T) ({pat fields}) ->
  T = body`. The `-> T` is mandatory. Parameter groups may be
  patterns (tuples, records, single-ctor ADTs — irrefutable only).
- All Gom functions curry; there is no arity in the type —
  `add : Int -> Int -> Int`. Partial application is just
  under-application.
- Lambdas: `fn x -> e`, `fn a b -> e`, `fn () -> e`, `fn _ -> e`.
  Parameters are bare identifiers/`_`/`()`, `(x: T)` (annotated),
  `~x` or `(~x: T)` (labeled — the call site writes `f ~x: v`).
  No patterns in lambda position.
- Local `let` never generalizes: `let g = f` is monomorphic even
  when `f` is polymorphic (uses fix the type). Top-level `fn`s,
  top-level `let` bound to a lambda or a syntactic value
  generalize (value restriction: `let r = f x` does not).
- `@tailrec` on a `fn` requires all self-calls in tail position
  (block tail / if/match arm results / outermost `|>`/`<|`
  application); violations are E0902–E0905, checked at `gom build`
  (lowering), not `gom check`.
- Unused `let` bindings are legal (no unused-variable error yet —
  undecided), but write `ignore e`/`let _ = e` to show intent.

## Labeled arguments

```gom
fn transfer (from: Int) (~to: Int) (~amount: Int) -> Int =
  from + to + amount

let r1 = transfer 0 ~to: 1 ~amount: 2
let r2 = transfer 0 ~amount: 2 ~to: 1   // labels are order-free
let to = 1
let r3 = transfer 0 ~to ~amount: 2      // `~to` puns `~to: to`
let pending = transfer 0 ~to: 1         // partial: ~amount: Int -> Int

type Args = { to: Int, amount: Int }
let args : Args = { to = 1, amount = 2 }
let r4 = transfer 0 ~..args             // record spread: fields fill
                                        // labels by name
```

The spread value must be a *named* record — field lookup goes
through declared record types, so an anonymous `{ to = 1, … }`
literal reports E0401 "no record type has fields …".

- Positional args fill positional params in order; labeled args go
  anywhere, in any order, but each declared label exactly once —
  missing/duplicate/unknown labels on *Go* calls are errors
  (E0404/5/6); on *Gom* calls a missing label is partial application.
- `~name:` takes one simple expression (identifier, literal,
  parenthesized): `~amount: (base * 2)`.
- Labels are part of the function type; a labeled call requires the
  callee's type to be known at the call site (E0408).
- Variants take labels too: `| Transfer (~to: T) (~amount: T)`,
  constructed `Transfer ~to: a ~amount: b`.

## Types

All types are nominal. Builtin names (`Int`, `Int32`, `Int64`,
`Byte`, `List`, `Option`, `Result`, `Bool`, `String`, `Unit`,
`Float`, `Char`, `Ref`, `Array`, `Ptr`, `Chan`, `RecvChan`,
`SendChan`, `Task`, `GoError`, `Go.Slice`, `Go.Map`, `Go.Any`)
live in the builtin scope — a file-local `type` may shadow them,
and then constructs needing the builtin report E0415.

```gom
type Option2 a =          // type parameters are lowercase, juxtaposed
  | Some2 a
  | None2

type Tree =
  | Leaf
  | Node Tree Int Tree     // recursion through a variant: fine

type Point = { x: Int, y: Int }
type Config = { name: String, retries: Int, endpoint: String }

type alias Meters = Int          // transparent alias (Go `type X = T`)
type alias Wrap a = List a       // forwards its parameters
```

- `type alias X = T` introduces no new type — `Meters` IS `Int`.
  `alias` is a contextual keyword: it only applies between `type`
  and a `UIdent`, so `let alias = …` still binds a variable. A
  `type X = T` without `alias` always declares a new type —
  `type Meters = Int` is a single-variant sum whose constructor is
  named `Int`.
- A single-field record emits a Go defined type (`type UserId
  int64`), so its value is interchangeable with the field type at
  the Go boundary only through explicit conversion; generic
  one-field records stay structs. Scalar literals assign to it
  like Go's untyped constants — numeric (`let x : UserId = 0`,
  a literal argument where `UserId` is expected) as well as
  `String`/`Char`/`Bool` spellings — and its own values admit
  the arithmetic/comparison the underlying allows (`u + 0`,
  `u < 100` — multi-field records reject these as E0401).

- Variant payloads: positional `Num Int`, named `Let (name: String)
  (value: Expr)`, labeled `(~to: T)`. A *uniquely* named payload
  slot is readable through the sum type: `s.radius` typechecks when
  exactly one variant declares `radius` (two variants declaring the
  same name → E0401). The read does not check the runtime variant —
  a wrong-variant value panics — so prefer `match` patterns
  (`Circle { radius = r, .. }`). Positional slots have no `.N`
  access; destructure with patterns.
- Constructors are curried functions: `List.map Num xs`-style usage
  works; `Add (Num 0) r` needs parens for
  nested ctor applications in patterns.
- Records are stored inline → a record cannot contain itself by
  value (E0413); recurse through variants, function fields,
  `List`/`Ref`/`Ptr`/`Array`/`Chan`/`Go.*` instead.
- `==`/`!=`/`assert_eq`: structural equality on function-free types
  (E0409): scalars, `Ref`/`Ptr`/channels by identity, `List`/tuples/
  records elementwise, ADTs by variant+payload, `GoError` by message
  string. `Float` is IEEE (NaN ≠ NaN). Not on type variables (v1).
- Arithmetic `+ - * / %` overloads over numeric builtins and
  resolves late: ambiguous use is E0407 (annotate). `/`/`%` by a
  divisor that folds to a constant `0` is E0414 (Go rejects it
  too; `Float` allows `x / 0.0` but not `10.0 / 0.0`) — this
  covers folded expressions like `x / (1 / 2)`, and gom is
  stricter than Go where a variable expression folds to zero:
  `x / (n * 0)` flags E0414 while Go accepts it.

## match and patterns

```gom
type Expr =
  | Num Int
  | Add Expr Expr

fn eval (e: Expr) -> Int = match e {
  Num n -> n
  Add (Num 0) r -> eval r             // nested ctor pattern: parens required
  Add l r -> eval l + eval r
}
```

Pattern forms (spec 4.1):

| Pattern | Example | Notes |
| --- | --- | --- |
| wildcard / variable | `_`, `x` | |
| literal | `0`, `"ok"`, `'a'`, `true` | infinite types need `_` fallback |
| constructor | `Some x`, `Add (Num 0) r` | nest with parens |
| named fields | `Let { name = n, .. }` | `..` drops the rest |
| tuple / record | `(a, b)`, `{ x = a, y = 0 }` | records: single ctor; bare names pun (`{ x, y }` binds `x`/`y`) |
| list | `[]`, `[x]`, `[x, ..rest]` | Nil/Cons 2-ctor view |
| or | `Circle _ \| Empty` | same binds, same types |
| as | `Add _ _ as whole` | |
| guard | `p if cond` | never counts for coverage |

- Exhaustive required: E0301 lists up to 5 counterexamples and
  usually offers a `todo ()` fix. Unreachable arms: E0302. Refutable
  pattern in `let`/`for`/param: E0303. Or-pattern bind mismatch:
  E0304. Guards count for nothing in either direction — an arm after
  `p if g` is *not* redundant.
- Malformed values from Go (a `nil` ADT) panic at `match` time with
  the type name — contract violation, spec 8.6.
- A `-` literal pattern on a non-first arm needs parens
  (`(-80) -> e`): a line-leading `-` continues the previous arm's
  body as binary minus (grammar trap).
- An ascription in the arm head — e.g. a guard `p if x : T` —
  swallows the arm's `->` (`T -> e` parses as a function type), so
  the arm looks unterminated: parenthesize, `p if (x : T) -> e`.
- Type-test arms `p : T` / `_ : T` (spec 4.1) only apply to a
  match over `Go.Any` or a named Go interface — they emit a Go
  `switch v := x.(type)`. A `_`/variable catch-all is mandatory
  and must be last; guards are not allowed; `p : Go.Any` is itself
  a catch-all that keeps the box's recorded type. Targets dedupe
  by emitted Go repr — `Char` and `Int32` collide (E0309). A
  function-type target needs parens: `f : (Int -> Int)`.

## try, Result, |?>

```gom
fn checked_div (a: Int) (b: Int) -> Result Int String =
  if b == 0 { Err "division by zero" } else { Ok (a / b) }

fn ratio (a: Int) (b: Int) -> Result Int String = {
  let x = try checked_div a b    // Ok v → v; Err → return Err from `ratio`
  let y = try checked_div x 2
  Ok (x + y)
}
```

- `try` escapes to the **innermost enclosing function or lambda**
  (a `test` block counts; `go { }` does not — E0702). The
  function's return type must be `Result _ e` and its `e` must equal
  the tried expression's error type (E0410/E0411).
- `r |?> f`: `r : Result a e`, `f : a -> Result b e` → `Result b e`.
  `Ok v → f v`, `Err →` skips the rest. Mixes freely with `|>`.
- Auto-wrap: when `e` is `GoError`, a failing step wraps in a
  `*rt.StepError` frame — `Error()` renders `<stepname>: <message>`,
  `Unwrap()` returns the original, so `errors.Is`/`As` still work;
  `errors.As` into `*rt.StepError` reads `.Step` back as data. Once,
  at the failing step. Step name = the source spelling of the
  step's head (`strconv.ParseInt(_, 10, 64)` → `strconv.ParseInt`;
  nameless lambda → `file.gom:NN`). Non-`GoError` errors are not
  wrapped.
- A non-`Result` head is wrapped in `Ok`: `path |?> os.ReadFile`.
  Writing `path |> os.ReadFile |?> ...` instead pipes the *Result
  value* — the call's error is not auto-wrapped.

## for loops

`for x in xs { body } : Unit` iterates a `List`, a `Go.Slice`
(index order, nil iterates zero times), or drains a `Chan`
(until closed). The pattern is irrefutable-only. The body is `Unit`;
accumulation goes through recursion or `Ref`/`Array` (stdlib
pending — today, prefer recursion):

```gom
package main

fn echo (xs: List Int) -> Unit = {
  for x in xs { ignore x }
  ()
}

fn sum (xs: List Int) -> Int = match xs {
  [] -> 0
  [x, ..rest] -> x + sum rest
}
```

## Tests

```gom
fn add (a: Int) (b: Int) -> Int = a + b

@test
fn fixture () -> Int = 3          // visible only to tests/@test defs

test "add works" {
  assert (add 1 1 == 2)
  assert_eq (add 1 (fixture ())) 4
}

test "may fail" {
  let n = try add_res 1 2         // test is a function boundary for try
  assert_eq n 3
  Ok ()
}

fn add_res (a: Int) (b: Int) -> Result Int String = Ok (a + b)
```

- `test "name" { ... }`: body type `Unit` or `Result Unit e`
  (`Err` fails the test); anything else is E0801.
- `assert cond`, `assert_eq actual expected` — only inside `test`
  (E0803); `assert_eq` needs `==`-able types.
- `@test` defs are test-scope-only (E0802), share the package
  namespace, never appear in `.gomi`.
- `gom build`/`gom check` type-check tests but emit no test code;
  `gom test` writes `<name>_test.go` next to the generated `.go`
  and runs the whole package's tests via `go test -json`.

## Things that look plausible but don't exist

`Ref.*`, `Array.*` — module names are reserved, member tables are
empty → E0201 "exposes no members yet" (`Chan.*`/`Task.*` —
see `references/concurrency.md` — and `Ptr.*`/`Go.Map.*` —
see `references/go-interop.md` — do exist). The `List.*`,
`Option.*`, `Result.*` stdlib modules do exist (see
`stdlib/{list,option,result}/*.gomi`). `let ... in`, `pub`, `var`,
`return`, `break`, `while`,
`switch`, `enum`, `interface`, `class`, `nil`, `null`, `void`,
`func`, `def`, bare `type X = T` aliases (write `type alias X = T`),
`::`, `$`, `!`, `??`, `?.`,
raw strings, `xs[i]` indexing, and doc
notations other than `///`.

Comments do exist — `//` line comments, `/* */` block comments,
and `///` doc comments on declarations (spec 1.2); `////` and
beyond stay plain line comments.
