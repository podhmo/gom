# Gom language fundamentals

Condensed from `docs/spec.md` chapters 1–6 and 11 (normative, in
Japanese). Everything here describes today's implementation; the
code blocks are verified by `gom check`.

## File structure

```gom
package main            // mandatory first clause; one dir = one package

import go "fmt"         // Go package import
import go "os"

type Expr =             // type declaration (sum or record)
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
  `\n \t \r \0 \\ \' \" \$ \uXXXX \u{X...}`.
- `()` is the `Unit` value. Tuples `(a, b, c)`. Lists `[1, 2, 3]`
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
  Parameters are bare identifiers/`_`/`()` only — no type
  annotations, patterns, or labels in lambda position.
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
```

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

All types are nominal. Builtin names (`Int`, `List`, `Option`,
`Result`, `Bool`, `String`, `Unit`, `Float`, `Char`, `Int32`,
`Int64`, `Ref`, `Array`, `Ptr`, `Chan`, `RecvChan`, `SendChan`,
`Task`, `GoError`) live in the builtin scope — a file-local `type`
may shadow them, and then constructs needing the builtin report
E0415.

```gom
type Option2 a =          // type parameters are lowercase, juxtaposed
  | Some2 a
  | None2

type Tree =
  | Leaf
  | Node Tree Int Tree     // recursion through a variant: fine

type Point = { x: Int, y: Int }
type Config = { name: String, retries: Int, endpoint: String }
```

- Variant payloads: positional `Num Int`, named `Let (name: String)
  (value: Expr)`, labeled `(~to: T)`. A *uniquely* named payload
  slot is readable through the sum type: `s.radius` typechecks when
  exactly one variant declares `radius` (two variants declaring the
  same name → E0401). The read does not check the runtime variant —
  a wrong-variant value panics — so prefer `match` patterns
  (`Circle { radius = r, .. }`). Positional slots have no `.N`
  access; destructure with patterns.
- Constructors are curried functions: `List.map Num xs`-style usage
  works once `List.map` exists; `Add (Num 0) r` needs parens for
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
  constant `0` is E0414 (Go rejects it too; `Float` allows `x /
  0.0` but not `10.0 / 0.0`).

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
| tuple / record | `(a, b)`, `{ x = a, y = 0 }` | records: single ctor |
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
- Auto-wrap: when `e` is `GoError`, a failing step wraps as
  `fmt.Errorf("<stepname>: %w", err)` — once, at the failing step;
  `errors.Is`/`As` still work. Step name = the source spelling of
  the step's head (`strconv.ParseInt(_, 10, 64)` → `strconv.ParseInt`;
  nameless lambda → `file.gom:NN`). Non-`GoError` errors are not
  wrapped.
- A non-`Result` head is wrapped in `Ok`: `path |?> os.ReadFile`.
  Writing `path |> os.ReadFile |?> ...` instead pipes the *Result
  value* — the call's error is not auto-wrapped.

## for loops

`for x in xs { body } : Unit` iterates a `List` or drains a `Chan`
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

`List.map`, `List.filter`, `Option.get`, `Result.map`, `Chan.*`,
`Task.*`, `Ref.*`, `Array.*` — module names are reserved, member
tables are empty (stdlib task P4-8) → E0201 "exposes no members
yet". `let ... in`, `pub`, `var`, `return`, `break`, `while`,
`switch`, `enum`, `interface`, `class`, `nil`, `null`, `void`,
`func`, `def`, `type X = T` aliases, `::`, `$`, `!`, `??`, `?.`,
raw strings, `xs[i]` indexing, `[x, ..xs]` construction, and doc
notations other than `///`.

Comments do exist — `//` line comments, `/* */` block comments,
and `///` doc comments on declarations (spec 1.2); `////` and
beyond stay plain line comments.
