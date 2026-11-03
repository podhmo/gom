---
name: gom-agent-guide
description: Use when writing, checking, or debugging `.gom` or `.gomi` files, using the `gom` toolchain (`gom check`/`build`/`dump`/`fmt`), or following Gom-specific conventions (curried functions, `|>`/`|?>` pipes, `try`/`Result`, exhaustive `match`, `import go` interop, `.gomi` interface files) — even when Gom is only implied by file extensions or `gom` commands rather than named.
---

# Gom Agent Guide

Gom is a statically typed functional language that **transpiles to
readable Go**. Sum types, exhaustive `match`, expression-oriented
syntax, curried functions, and direct calls into Go packages. A
`.gom` file compiles to a `.go` file via `gom build`; `.gomi` files
are OCaml-`.mli`-style interface files that define a package's public
API.

**Your training data contains almost no Gom.** The syntax borrows
from ML and Go but differs in exactly the places where OCaml / F# /
Haskell / Go habits fire: application has no commas, there is no
`let ... in`, labeled arguments exist, `Result` + `try` replaces
`if err != nil`, and `match` must be exhaustive. **Do not guess — use
this file first**, then the `references/` files for depth, then
`docs/spec.md` (the normative spec, in Japanese) when a question
remains.

Every `gom`-fenced block in this skill is verified by `gom check` in
CI; blocks marked `fail` are verified to produce a diagnostic.

# Agent Workflow

1. **Locate the package.** One directory = one Gom package = one Go
   package. Gom compiles *per file* today; a `package main` needs a
   `fn main () -> Unit` to be a runnable entrypoint. Non-`main`
   packages need exactly one `.gomi` to be usable.
2. **Read before writing.** New syntax? Check `docs/spec.md` (or the
   matching `references/` file here) and copy a working pattern from
   `tutorial/NN-*/main.gom`. Never improvise surface syntax.
3. **Write the smallest possible `.gom`.** Prefer editing an existing
   compiling file over generating a large new one.
4. **Check early and often:** `gom check file.gom` — read the JSON
   diagnostic, or `-format text` for a human rendering. Fix the
   first error; later phases only run on error-free input, so later
   messages often vanish once the first is fixed.
5. **Build:** `gom build file.gom` writes `file.go`; run it with the
   real Go toolchain (`go run .` in a module, or `go build`).
6. **Finish with `gom fmt -w`** — the canonical formatter has no
   configuration; whatever it prints is correct.

# Fast Task Playbooks

## Write a new `.gom` program

Start from the smallest skeleton and grow it:

```gom
package main

import go "fmt"

fn main () -> Unit = {
  ignore fmt.Println "hello, gom"
  ()
}
```

- `import go "fmt"` imports the real Go package — no declarations
  needed.
- `fmt.Println` returns `(int, error)` in Go; Gom sees
  `Result Int GoError`. A mid-block expression must be `Unit`, so
  discard it explicitly with `ignore`.
- A block's value is its last expression: `()` (Unit) ends a
  `-> Unit` function.

Then `gom build main.gom && go run .` in a Go module that can resolve
`github.com/podhmo/gom` (each `tutorial/` step has a `go.mod` you can
copy).

## Fix a `.gom` that fails `gom check`

1. Run `gom check -format text file.gom`; work top-down from the
   first `error[E0xxx]` line.
2. Look the code up in `docs/error-codes.md` (or
   `references/diagnostics.md` here) — each entry states the phase,
   spec section, and a minimal example.
3. Many diagnostics carry `fixes` in the JSON: `applicability:
   "placeholder"` inserts `todo ()` holes (compiles, panics at run
   time); `"safe"` edits are mechanical.
4. Most newcomer errors are habit collisions — check
   `# Common LLM Mistakes` below before debugging deeply.

## Add or edit a compiler spec-fixture (this repo)

A spec rule → a case in `testdata/spec/`: `<name>.gom` plus
`<name>.expected.json` holding the expected diagnostics. Regenerate
with `go test ./internal/build -run Spec -update`. Generated-Go
changes → `testdata/golden/`. See `references/project-layout.md`.

## Read generated Go

`gom build` output is ordinary, readable Go: `fn` → curried
`func(A) func(B) C`, ADTs → sealed interface + variant structs,
`match` → `switch` on a tag/type, `try`/`|?>` → `if err != nil`
early-returns, `//line` directives map back to `.gom`. Runtime
helpers live in `rt/`; builtin-type representations in `stdlib/`.
`gom dump -phase=core file.gom` shows the Core IR the output was
generated from — useful when generated code surprises you.

# Gom Language Tour

## Core facts

- **Expression-oriented.** Everything is an expression; a `{ }`
  block's value is its last expression. No `return`, `break`,
  `continue`, or statements. No offside rule — newlines are
  *token-driven* (see `# Newlines`), indentation never matters.
- **Files & modules.** A file starts `package <name>`; top-level
  declarations are `fn`, `let`, `type`, `test`, `import go`,
  `extern go`. Visibility is controlled by the package's `.gomi`
  file — there is **no `pub` keyword**.
- **`fn` signatures are mandatory and complete:**
  `fn add (a: Int) (b: Int) -> Int = a + b`. The `-> T` return type
  is never elided. Parameters are parenthesized, possibly labeled or
  patterns.
- **All functions are curried.** `add 1` is a perfectly good
  `Int -> Int`. Application is juxtaposition `f x y`, left-assoc,
  binds tighter than every binary operator: `f x + g y` is
  `(f x) + (g y)`. Field/module access binds tighter still:
  `f p.name` is `f (p.name)`.
- **Lambdas:** `fn x -> x + 1`, `fn a b -> a + b` — parameters are
  bare identifiers, `_`, or `()` only (no types, patterns, or labels
  in lambda params). The body extends as far right as possible.
- **Unit takes the place of `void`.** A no-arg function is
  `fn f () -> Unit` and is *called* `f ()`.
- **Pipes and composition** (lowest-precedence binary ops, left
  assoc):

  | Op | Meaning |
  | --- | --- |
  | `x \|> f` | `f x` — value flows left→right; `x \|> f y` is `f y x` |
  | `f <\| x` | `f x` — for a long final argument / lambda |
  | `f >> g` | `fn x -> g (f x)` |
  | `g << f` | `fn x -> g (f x)` |
  | `r \|?> f` | Result-chaining pipe — see below |

  Unlike F#, `|` alone is not a pipe; unlike shell, `<|` reads
  "apply right to left". No `$` (Haskell), no `@@`/`|>`-variants
  beyond this table.

- **Labeled required arguments.** `(~to: Account)` in a signature
  means call sites must write `~to:`; labeled args are order-free,
  `~to` puns `~to: to`. Omitting a label is *partial application*,
  not an error — it bites silently: see the mistakes section.
- **Types** (all nominal): `Int Int32 Int64 Float Bool Char String
  Unit`, tuples `(a, b)`, `List a`, `Option a` (`Some`/`None`),
  `Result a e` (`Ok`/`Err`), `Ref a`, `Array a`, `Ptr T`, `Chan a` /
  `RecvChan a` / `SendChan a`, `Task a`, `GoError`, `Go.Slice a`,
  `Go.Map k v`, `Go.Any`. Type application is juxtaposed:
  `Result Int GoError`, `Option (List Int)`. Type variables are
  lowercase (`a`, `b`).
- **Sum types:** `type Shape = | Circle (radius: Float) | Rect (w:
  Float) (h: Float) | Empty` — variant args take the same forms as
  `fn` params (positional, `(name: T)`, `(~name: T)`). Records:
  `type Point = { x: Int, y: Int }`. Constructors are curried
  functions you can pass around.
- **`match` is exhaustive** — a missing case is E0301 with concrete
  counterexamples; an unreachable arm is E0302. Guards (`p if cond`)
  never count toward coverage. Bind with variables/constructors;
  `let` accepts only irrefutable patterns.
- **Errors are values.** `try e` unwraps `Ok v → v` or returns `Err`
  from the innermost enclosing function — which must return
  `Result _ e`. `r |?> f` chains `Result` steps; on `GoError` it
  auto-wraps failures as `fmt.Errorf("<step>: %w", err)`. No
  exceptions; Go panics propagate (no `recover`).
- **Go interop is direct:** `import go "strconv"` then
  `strconv.Atoi s`. `(T, error)` → `Result T GoError`, `(T, bool)` →
  `(T, Bool)`, `error` → `Result Unit GoError`. Go calls are *not*
  curried: `f(a, b)` applies one tuple. Methods `x.M(args)` on Go
  values only. `_` in a Go call makes a one-hole function.
- **Equality is structural:** `==` / `!=` compare contents on any
  type containing no function — including ADTs, records, `List`
  (E0409 otherwise; `Float` is IEEE so `NaN != NaN`). Type variables
  cannot be `==` in v1.
- **Loops are recursion or `for x in xs { ... }`** (`: Unit`,
  iterates `List` or `Chan`). Self-tail-recursion lowers to a Go
  loop; `@tailrec` on a `fn` *requires* it and errors otherwise.
- **Tests live in the same file:** `test "name" { ... }` body is
  `Unit` or `Result Unit e`; `assert`/`assert_eq` exist only inside
  `test`; `@test` marks test-only helpers. `gom test FILE|DIR ...`
  writes the `.go` plus a `_test.go` beside each input and runs the
  package's tests through `go test -json`; a failed `assert_eq`
  reports both values in the assertion message.
- **Stdlib modules exist:** `List.map`, `List.filter`, `List.fold`,
  `List.sum`, `List.rev`, `List.head`, `List.get`, `Option.get`,
  `Option.map`, `Option.and_then`, `Result.map`, `Result.and_then`,
  `Result.map_err` and friends resolve without an import — the
  worked-on value is the last parameter so `xs |> List.map f`
  pipelines. See `stdlib/{list,option,result}/*.gomi` for the full
  surface. Still unimplemented: `Chan.*`, `Task.*`, `Ref.*`,
  `Array.*` — those member tables are empty and report E0201
  "exposes no members yet"; get channels from function parameters.

## Newlines (no indentation rules)

Inside `{ }` a newline separates expressions **unless** the line ends
in something that cannot end an expression (`|>`, `<|`, `->`, `=`,
`,`, `~label:`, `(` …) or the next line starts with a binary
operator or `else`. Inside `( )`/`[ ]` a newline is just whitespace.
Consequence: `if`/`match` heads keep `{` on the **same line** (like
Go — a `{` on the next line is E0101), and long pipelines start each
line with `|>`.

```gom
package main

fn classify (n: Int) -> String =
  if n > 40 { "big" } else { "small" }

fn sum_list (xs: List Int) -> Int = match xs {
  [] -> 0
  [x, ..rest] -> x + sum_list rest
}

let summary =
  [1, 2, 3]
  |> sum_list
  |> classify
```

## The one-file program

```gom
package main

import go "fmt"
import go "strconv"

type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)
  | Empty

fn area (s: Shape) -> Float = match s {
  Circle r -> 3.14159 * r * r
  Rect w h -> w * h
  Empty -> 0.0
}

fn parse (s: String) -> Result Float GoError =
  strconv.ParseFloat (s, 64) |?> fn v -> Ok v

fn describe (r: Result Float GoError) -> String = match r {
  Ok v -> "area ${v}"
  Err e -> "error: ${e.Error ()}"
}

fn main () -> Unit = {
  ignore (fmt.Println (area (Rect 2.0 3.0)))
  ignore (fmt.Println (describe (parse "2.5")))
  ()
}
```

Compile: `gom build shapes.gom` → `shapes.go` → `go run .`

## try and |?>

```gom
package main

import go "strconv"

fn checked_div (a: Int) (b: Int) -> Result Int String =
  if b == 0 { Err "division by zero" } else { Ok (a / b) }

// try escapes from the enclosing function on the first Err.
fn ratio (a: Int) (b: Int) (c: Int) -> Result Int String = {
  let x = try checked_div a b
  let y = try checked_div x c
  Ok y
}

// |?> is the same short-circuit written as a pipeline; a non-Result
// head is wrapped in Ok automatically.
fn ratio_pipe (a: Int) (b: Int) (c: Int) -> Result Int String =
  checked_div a b |?> fn x -> checked_div x c
```

- `try` escapes to the **innermost enclosing `fn`/lambda/`test`** —
  its return type must be `Result _ e` with the same `e` (E0410 /
  E0411 otherwise). `try` does NOT `%w`-wrap — `|?>` does.
- `|?>` names each step from the source (`os.ReadFile`, or the
  file:line for a lambda) and wraps *the failing step only*, once.
- A failed `|?>` produces an `Err` **value** — surrounding code
  continues. Only `try` escapes.

# Common LLM Mistakes

Every NG below is verified to fail today with the code shown; the OK
form compiles. When in doubt, write the OK form — never "adapt" an NG
one.

## Function application

```gom fail=E0401
package main

fn add (a: Int) (b: Int) -> Int = a + b

fn main () -> Unit = {
  ignore (add(1, 2))   // NG: applies add to the tuple (1, 2)
  ()
}
```

```gom
package main

fn add (a: Int) (b: Int) -> Int = a + b

fn main () -> Unit = {
  ignore (add 1 2)     // OK: juxtaposition, curried
  ()
}
```

`f(a, b)` *is* legal — it applies `f` to one tuple argument, which is
exactly what Go calls want: `strconv.Atoi(s)`, `fmt.Println(a, b)`.
For a Gom `fn`, use `f x y`.

## Assignment and mutation

```gom fail=E0101
package main

fn main () -> Unit = {
  let x = 1
  x = 2                // NG: no assignment operator
  ()
}
```

```gom
package main

fn main () -> Unit = {
  let x = 1
  let x = x + 1        // OK: shadow, never reassign
  ()
}
```

`var`, `:=`, `+=`, `++` do not exist (E0101/E0201). Mutable state is
explicit: `Ref a` / `Array a` (stdlib members pending) — default to
immutability.

## `let ... in`, `return`, `else if`

```gom fail=E0101
package main

fn main () -> Unit = {
  let x = 1 in x       // NG: OCaml has `in`; Gom blocks don't
}
```

```gom fail=E0101
package main

fn main () -> Unit = {
  if true { () } else if false { () } else { () }
}
```

```gom
package main

fn main () -> Unit = {
  let x = 1
  if x > 0 { () } else { if x < 0 { () } else { () } }
}
```

`else` takes a block — nest `if` inside it. `return`/`break` don't
exist (`return` is an undefined name, E0201); the last expression is
the result, early escape is `try`.

## Records: `:` vs `=` — the asymmetry trap

```gom fail=E0101
package main

type Point = { x: Int, y: Int }

fn main () -> Unit = {
  let p = { x: 1, y: 2 }   // NG: record VALUES use `=`, not `:`
  ignore p
  ()
}
```

```gom
package main

type Point = { x: Int, y: Int }

fn main () -> Unit = {
  let p = { x = 1, y = 2 }         // OK: literal fields bind with =
  let q = { p with x = 3 }         // OK: functional update
  ignore (p.x + q.y)
  ()
}
```

Record *types* use `:` (`{ x: Int }`); record *values*, updates, and
record *patterns* (`{ x = a, y = b }`) use `=`. `:` is type
ascription in expression position — `x : Int` is legal in `let`
annotations and `Go.conv` signatures, not inside `{ }`.

## Type declarations are not aliases

```gom
package main

// TRAP: compiles, but defines a variant named `Int` of type `Meters`
// — NOT a type alias. `type X = T` has no alias production.
type Meters = Int

fn main () -> Unit = {
  let m = Int            // a nullary variant constructor, not `Int` 3
  match m { Int -> () }
}
```

There is no type-alias syntax in v1. `type X = | A | B` is a sum;
`type P = { f: T }` is a record. Anything else after `=` parses as
variant syntax (`type X = List Int` is variant `List` with payload
`Int`).

## List patterns and indexing

```gom fail=E0101
package main

fn head (xs: List Int) -> Int = match xs {
  [] -> 0
  x :: rest -> x        // NG: OCaml cons syntax does not exist
}
```

```gom
package main

fn head (xs: List Int) -> Int = match xs {
  [] -> 0
  [x, ..rest] -> x      // OK: literal-style pattern
}
```

```gom fail=E0401
package main

fn first (xs: List Int) -> Int = xs[0]   // NG: no index syntax
```

```gom
package main

fn prepend (x: Int) (xs: List Int) -> List Int =
  [x, ..xs]                // OK: spread literal — cons, a fresh list
```

No `xs[i]`, no `::`, no `h::t`, no `[]` access — destructure with
`match` on `[x, ..rest]`. List *literals* are `[a, b, c]` or
`[x, ..xs]` (a spread tail splices a whole list; `..rest` must be
the last element). `List.cons`, `List.append`, `List.rev_append`
and the other `List.*` stdlib functions cover the rest.

## Punctuation from other languages

```gom fail=E0101
package main

fn f (x: Int) -> Int = negate $ x   // NG: no $ operator

fn negate (x: Int) -> Int = 0 - x
```

```gom fail=E0101
package main

fn flip (b: Bool) -> Bool = !b   // NG: no `!`; write `not b`
```

```gom
package main

fn flip (b: Bool) -> Bool = not b   // OK: `not` is a builtin
```

`!`, `$`, `?`, `&`, `#`, `::`, `->>` are not Gom tokens.
`<<`/`>>` are function composition, **not** bit shifts (Gom has no
infix bit ops). Operators: `+ - * / %  == != < <= > >=  && ||  |> <|
|?> >> <<` and that's it.

## Method calls on Gom values

```gom fail=E0401
package main

type Shape = | Circle (r: Float)

fn main () -> Unit = {
  let s = Circle 1.0
  ignore (s.Area ())     // NG: "Shape has no field or method `Area`"
  ()
}
```

`x.M(args)` is only for values of Go types; on a Gom type the
selector is a field lookup that fails (E0401), and calling a
Gom-typed receiver's method reports E0502. Gom functions are plain
values: write `area s`. Conversely, Go values only expose their own
Go methods — you cannot add methods to them.

## Missing labels are partial application, not an error

```gom fail=E0401
package main

fn transfer (from: Int) (~to: Int) (~amount: Int) -> Unit = {
  ignore from; ignore to; ignore amount; ()
}

fn main () -> Unit = {
  transfer 1 2 3       // NG silently: = `transfer 1` partially applied;
                       // labeled params can't take positional args
}
```

```gom
package main

fn transfer (from: Int) (~to: Int) (~amount: Int) -> Unit = {
  ignore from; ignore to; ignore amount; ()
}

fn main () -> Unit = {
  transfer 1 ~to: 2 ~amount: 3   // OK: labels mandatory, order-free
}
```

The failure surfaces downstream as `error[E0402]: a mid-block
expression must have type Unit, got ~to: Int -> ~amount: Int ->
Unit` (a partial application where a statement was needed). If you
see a function type where a call "should" have returned a value,
check for missing labels first.

## Discarding results

```gom fail=E0402
package main

import go "fmt"

fn main () -> Unit = {
  fmt.Println "x"   // NG: mid-block non-Unit (returns Result Int GoError)
  ()
}
```

```gom
package main

import go "fmt"

fn main () -> Unit = {
  ignore fmt.Println "x"   // OK: explicit discard
  ()
}
```

`ignore e` binds tighter than binary operators but looser than
application: `ignore fmt.Println "x"` is `ignore (fmt.Println "x")`,
but `ignore a + b` is `(ignore a) + b` — parenthesize composite
expressions: `ignore (fmt.Println (x + y))`. Deliberate discard also
discards the error — `match`/`try` when failure matters.

## try and go/select boundaries

```gom fail=E0410
package main

import go "strconv"

fn parse (s: String) -> Int =
  try strconv.Atoi(s)   // NG: parse must return Result Int GoError
```

```gom fail=E0702
package main

fn compute () -> Result Int GoError = Ok 1

fn main () -> Unit = {
  go { let r = try compute (); ignore r; () }   // NG: `go` is not a fn boundary
}
```

Wrap the `try` in a lambda inside the goroutine (or use `Task.spawn`
once stdlib lands), and make the `try`ing function's return type a
`Result` whose error type matches the tried value's.

## Things that don't exist

```gom fail=E0101
package main

pub fn f () -> Int = 1   // NG: no `pub`; visibility lives in the .gomi

fn main () -> Unit = ()
```

```gom fail=E0203
package main

import "fmt"             // NG: Go packages need `import go "fmt"`

fn main () -> Unit = ()
```

- No `pub`/`private`/`internal` — a package's `.gomi` file decides
  what is public.
- `import "pkg"` loads a Gom package through its `.gomi` (the
  importing file must be inside the same Go module); Go packages
  use `import go "pkg"`.
- No `switch`/`case` → `match`; no `enum` → `type`; no
  `interface`/`struct`/`class` → records and ADTs; no `nil`/`null`/
  `None`-free references → `Option`; no `func`/`def`/`let fun` →
  `fn`; no `type X = T` alias → real sum/record only; no `while`/
  `for(;;)` → `for x in xs` or recursion; no `[]`-indexing; no raw
  strings/backquotes; no `;;`, `;;`-style block ends.

# Toolchain

Implemented today: `build`, `check`, `dump`, `fmt`. Planned (not
yet): `test`, `fix`, `gomi`/`info`, `explain`, `schema`.

| Command | Use |
| --- | --- |
| `gom check FILE...` | Parse → resolve → type-check → pattern check; no output files. Directories expand to their `*.gom`/`*.gomi`. |
| `gom build [-o out] FILE...` | Full pipeline; writes one `.go` per `.gom` next to the input (or under `-o` dir/file). |
| `gom dump -phase=syntax\|resolve\|check\|core FILE` | Print one phase's IR (diagnostics go to stderr; stdout is the artifact). |
| `gom fmt [-w\|-d\|-l\|--check] FILE...` | Canonical formatter; `-` reads stdin. stdout carries the artifact; message stream to stderr. |
| `gom check -format text` | Human rendering instead of JSON Lines. |

Diagnostics are JSON Lines: one object per line, `kind:
"diagnostic"` entries then a final `kind: "summary"`. Exit codes:
`0` ok, `1` diagnostics, `2` usage/internal error. Details:
`references/diagnostics.md`, `docs/error-codes.md`.

Useful: `gom check f.gom | jq -r 'select(.kind=="diagnostic") |
.code + " " + .message'` — list codes first, fix top-down.

# What to trust

- **`docs/spec.md`** is normative for user-visible behavior — it is
  written in Japanese; this skill is the English operational summary.
- **`docs/grammar.ebnf`** is the formal grammar.
- **`docs/error-codes.md`** is the generated catalog of every
  diagnostic code.
- **`tutorial/01-hello` … `12-result-values`** are executable,
  current examples — prefer copying them over inventing syntax.
- **`docs/implementation-gaps.md`** lists known spec↔implementation
  divergences — check it when "the spec says X but check says Y".
- **`docs/problems/`** catalogs unresolved ambiguities an LLM will
  hit — several mistakes above come from there.

# References (load on demand)

| Task | File |
| --- | --- |
| Syntax, expressions, currying, labels, types, match, tests | [references/language-fundamentals.md](references/language-fundamentals.md) |
| `import go`, type mapping, methods, `_`, `Go.conv`, extern | [references/go-interop.md](references/go-interop.md) |
| `go`/`select`/`Chan`/`Task`, `for`, sharing rules | [references/concurrency.md](references/concurrency.md) |
| `.gomi` files, visibility, abstract types | [references/interface-files.md](references/interface-files.md) |
| JSON diagnostics, error codes, fixes, jq recipes | [references/diagnostics.md](references/diagnostics.md) |
| Repo layout, testdata suites, tutorial, dev commands | [references/project-layout.md](references/project-layout.md) |
