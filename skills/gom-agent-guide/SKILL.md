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
   package: the `.gom` files in a directory compile together as one
   unit and share scope (a top-level name in `b.gom` is visible in
   `a.gom` with no import). A `package main` needs a
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
   **Run:** `gom run file.gom -- args...` compiles to a temp binary
   and executes it directly — child argv/exit codes pass through
   (signal kills report 128+signal). On success the program owns
   stdout/stderr; on compile or link failure gom emits the usual
   JSON message stream.
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

Then `gom run main.gom` — or `gom build main.gom && go run .` — in a
Go module that can resolve `github.com/podhmo/gom` (each `tutorial/`
step has a `go.mod` you can copy).

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
`<name>.expected.json` holding the expected diagnostics. A
package-unit rule (sibling files sharing scope) → a directory case:
`testdata/spec/<name>/` holding the member `.gom` files plus a bare
`expected.json` — all members compile as one unit. Regenerate
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

One shape to recognize: a record declared with exactly one field
compiles to a Go defined type, not a struct — `type UserId = {
value: Int }` emits `type UserId int64`, so reads/writes show up
as casts (`int64(u)`, `UserId(v)`). Generic one-field records
stay structs (Go forbids `type X[T] T`).

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
  bare identifiers, `_`, `()`, `(x: T)` (an annotated parameter),
  `~x`, or `(~x: T)` (a labeled parameter — the caller fills it as
  `f ~x: v`; no patterns in lambda params). The body extends as far
  right as possible.
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
  An unlabeled function still fills a labeled position — labels are
  introduced positionally (in `apply (fn x -> x)`, the `fn x -> x`
  argument conforms to the `~x: Int -> Int` parameter); a labeled
  function never conforms to an unlabeled one.
- **Types** (all nominal): `Int Int32 Int64 Byte Float Bool Char String
  Unit`, tuples `(a, b)`, `List a`, `Option a` (`Some`/`None`),
  `Result a e` (`Ok`/`Err`), `Ref a`, `Array a`, `Ptr T`, `Chan a` /
  `RecvChan a` / `SendChan a`, `Task a`, `GoError`, `Go.Slice a`,
  `Go.Map k v`, `Go.Any`, `Go.FuncN p0 … r` (raw view of a Go func
  type, params 0–3 / results 0–8 — adapt a Gom fn in with
  `Go.funcN f`, out with `Go.fnN f`). Type
  application is juxtaposed:
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
  auto-wraps each failure in a `*rt.StepError` frame — text
  `<step>: <message>`, `Unwrap()` keeps `errors.Is`/`As` working.
  No exceptions; Go panics propagate (no `recover`).
- **Go interop is direct:** `import go "strconv"` then
  `strconv.Atoi s`. `(T, error)` → `Result T GoError`, `(T, bool)` →
  `(T, Bool)`, `error` → `Result Unit GoError`. 3+ results map onto
  a tuple — trailing `error` folds to `Result (T, U, ...) GoError`,
  otherwise `(T, U, ...)`; 9+ results stay hidden. Go calls are
  *not* curried: `f(a, b)` applies one tuple. Methods `x.M(args)` on Go
  values only. `_` in a Go call makes a one-hole function.
- **Equality is structural:** `==` / `!=` compare contents on any
  type containing no function — including ADTs, records, `List`
  (E0409 otherwise; `Float` is IEEE so `NaN != NaN`). Type variables
  cannot be `==` in v1.
- **Loops are recursion or `for x in xs { ... }`** (`: Unit`,
  iterates `List`, `Go.Slice`, `View`, or `Chan`).
  Self-tail-recursion
  lowers to a Go loop; `@tailrec` on a `fn` *requires* it and
  errors otherwise.
- **Tests live in the same file:** `test "name" { ... }` body is
  `Unit` or `Result Unit e`; `assert`/`assert_eq` exist only inside
  `test`; `@test` marks test-only helpers. `@method` on a `fn`
  adds a Go method to the receiver record so it satisfies Go
  interfaces (see go-interop). `@extern` on a `fn`
  declares signature-only — the body is a same-named Go function
  hand-written in the generated package (stdlib internals use it;
  see go-interop). `gom test FILE|DIR ...`
  writes the `.go` plus a `_test.go` beside each input and runs the
  package's tests through `go test -json`; a failed `assert_eq`
  reports both values in the assertion message.
- **Stdlib modules exist:** `List.map`, `List.filter`, `List.fold`,
  `List.sum`, `List.sum_int`, `List.rev`, `List.head`, `List.get`, `List.take`,
  `List.drop`, `List.last`, `List.last_n`, `List.sort_by`,
  `Option.get`,
  `Option.map`, `Option.and_then`, `Result.map`, `Result.and_then`,
  `Result.map_err` and friends resolve without an import — the
  worked-on value is the last parameter so `xs |> List.map f`
  pipelines. See `stdlib/{list,option,result}/*.gomi` for the full
  surface. **`View a`** is a read-only sub-view of a `List`
  (`List.view`, `List.slice xs lo hi`; `View.length`/`get`/`slice`/
  `fold`/`to_list`, `for x in v` also works) — sub-slicing and
  indexing are O(1) because the view shares the immutable backing.
  The View surface is compiler-registered (no `.gomi`). Concurrency builtins are compiler-provided (no `.gomi`,
  they forward to `rt/`): `Chan.make`,
  `Chan.send`, `Chan.recv` (`Option a` — `None` once closed),
  `Chan.close`, `Chan.recvOnly`, `Chan.sendOnly`, `Task.spawn`
  (`(Unit -> a) -> Task a`), `Task.await` (`Task a -> a`,
  re-panics a captured panic). `Array a` is the mutable build
  buffer (compiler-provided like `Chan`): `Array.make n init` (n copies of init — ADT elements have no zero value),
  `Array.length a`, `Array.get a i`, `Array.set a i v`,
  `Array.to_list a` (zero-copy freeze — do not write the array
  after freezing, spec §8.7), `Array.from_list xs` (copies).
  `Ref a` is the mutable cell: `Ref.new v`, `Ref.get r`,
  `Ref.set r v` — always initialized, no nil form.

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
- To control the `step: message` wording yourself: read `Step` off
  `*rt.StepError` via `errors.As` (with
  `import go "github.com/podhmo/gom/rt"`, see
  `testdata/realworld/errwalk`), wrap errors in your own frame with
  `rt.StepErr` via `Result.map_err` (declare it as
  `fn StepErr (step: String) (err: GoError) -> GoError` in an
  `extern go` block — the standard view returns
  `Result Unit GoError`), or skip `|?>` and use `try` + `match` on
  the raw `Result`.

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

```gom fail=E0511
package main

fn main () -> Unit = {
  let x = 1
  x = 2                // NG: `=` assigns only `ptr.Field` on a `Ptr T`
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

`var`, `:=`, `+=`, `++` do not exist (E0101/E0201). The one `=`
assignment form writes an exported Go struct field through `Ptr T`
— `client.Timeout = 30 * time.Second` — the `Ptr.new ()` +
field-writes pattern replaces Go composite literals; anything else
as the target is E0511. Other mutable state is explicit: `Array a`
is the build buffer (`Array.make`/`get`/`set`, freeze with
`Array.to_list`); `Ref a` is the mutable cell (`Ref.new`/`get`/
`set`). Imported types whose implementation is invisible
(extern-backed or abstract-exported) are marked mutable via a
`//gom:mutable Name` directive in their `.gomi` (spec §10).
Default to immutability.

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

Bare field names pun the same-named variable — in literals,
updates, and patterns alike: `{ x, y }` is `{ x = x, y = y }`,
and `match p { { x, y } -> ... }` binds `x`/`y`.

A trailing raw-string literal on a record *type* field is the Go
struct tag, verbatim: `{ name: String `json:"name"` }`
emits `Name string `json:"name"``.

A whole record spreads into labeled arguments: `f ~..opts` passes
`opts`' fields by name (`opts.x` fills `~x:`, etc.) — the callee's
type must be known and every field must match a declared label
(E0418 non-record, E0406 unknown label).

## Type declarations are not aliases — `type alias` is

```gom
package main

// TRAP: still compiles — but defines a variant named `Int` of type
// `Meters`, NOT an alias. `type X = T` always introduces a new type.
type Meters = Int

// For an alias (a transparent synonym, Go's `type X = T`), spell it
type alias MetersAlias = Int

fn main () -> Unit = {
  let m = Int            // a nullary variant constructor, not `Int` 3
  match m { Int -> () }
}
```

`type alias X = T` (spec 3.2) names an existing type without
introducing a new one — `type alias Wrap a = List a` forwards
parameters, forward references work, a cycle is E0401. `alias` is
contextual: it only acts as a keyword between `type` and a
`UIdent`, so `let alias = …` still binds a plain variable. Without
`alias`, anything after `=` is still variant syntax — `type X =
List Int` stays variant `List` with payload `Int`.

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

Building a list costs O(n) per cons or `List.append` step — a
`match`-cons self-recursion compiles to a buffer loop, but
accumulating through `List.append` (e.g. in a `fold` folder or a
call chain) or through mutual recursion stays O(n²) in generated
code. For a large or hot build, accumulate into `Array` and
freeze with `Array.to_list` instead.

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
`<<`/`>>` are function composition, **not** bit shifts — the bitwise
operators use three-character spellings: `&&& &&^ ||| ^^^ <<< >>>`
(Go `& &^ | ^ << >>`; `&&&`/`&&^`/`<<<`/`>>>` bind at the `*`-level,
`|||`/`^^^` at the `+`-level). There is no unary bit-NOT; `x ^^^ -1`
flips all bits on signed types, `y &&^ x` clears bits. Operators:
`+ - * / %  &&& &&^ ||| ^^^ <<< >>>  == != < <= > >=  && ||  |> <|
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

`List` has no members either: `xs.fold` / `xs.map` are E0401.
Use the module functions — `List.fold (fn acc v -> ...) acc xs`,
`List.map f xs` — or the pipe: `xs |> List.map f`.

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

## Flat parameters defer partial applications

When a declared parameter is a function type with 2+ arrows
(`fn g (f: A -> B -> C)`), `f` is passed under the flat calling
convention. A partial application `f a` does **not** run the
callee's first chunk — the call fires only once the flat arity is
met, and a residual that is never applied never fires at all. This
holds in both the generated code and the evaluator.

```gom
fn mk (x: Int) -> (Int -> Int) = {
  let _ : Int = todo ()   // fires when mk's first chunk runs
  fn y -> x + y
}

fn stage (f: Int -> Int -> Int) -> (Int -> Int) = f 20

fn main () -> Unit = {
  let p = stage mk     // NO panic: `f 20` only builds a residual
  ignore p               // residual never applied → todo never fires
  ()
}
```

For ordinary (`fn`-level, single-arrow) parameters `f a` still runs
the callee's first chunk immediately — only declared 2+-arrow
parameter slots carry the flat convention.

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

Wrap the `try` in a lambda inside the goroutine (or use
`Task.spawn`), and make the `try`ing function's return type a
`Result` whose error type matches the tried value's.

## Scoped cleanup: `Scope` instead of `defer`

There is no `defer` statement. Open a scope with `Scope.run` (or
`Scope.run_result` for a `Result _ GoError` body) and register
cleanups on its handle — from inside an `if`, a `match` arm or a loop
too; they all run when the body ends (normal end, `try` escape or
panic), last registered first.

```gom
package main

import go "fmt"

fn say (m: String) -> Unit = ignore (fmt.Println m)

fn work (need: Bool) -> Result Int GoError =
  Scope.run_result <| fn s -> {
    Scope.defer s (fn () -> say "always")
    if need { Scope.on_err s (fn () -> say "rollback") }
    Scope.defer_err s (fn () -> Ok ())
    Ok 1
  }

fn main () -> Unit = ignore (work true)
```

- No method syntax: `Scope.defer s f`, never `s.defer f`.
- `defer` always runs; `on_err` runs only on an `Err` body or a
  panic; `defer_err` (GoError only) joins cleanup errors into an
  `Ok` result with `errors.Join` — the body's own `Err` wins.
- The handle may not leave its body (E0419): no returning it, no
  storing it in a record/constructor/`Ref`, no `Scope` type
  argument, no capturing it in a lambda that escapes (`Task.spawn`,
  Go functions, a cleanup of an outer scope). A lambda given to a
  non-escaping parameter must not *return* `s` through its own
  result either — `run_cb (fn x -> fn () -> s)` is E0419 when the
  call's result type carries a function. Passing `s` to a
  helper `fn` is fine; so is a lambda given to a non-escaping
  parameter (a same-package `fn` inferred so, or an imported fn
  whose `.gomi` says so).
- A `.gomi` promises non-escaping parameters with a
  `//gom:noescape <name> <index>...` directive comment; `gom info`
  drafts it. No directive means "assume escaping". The directive
  attaches to a `let` member name too, not only `fn`. The mirror
  `//gom:result-notaint <name> <index>...` lists the parameters
  whose products CANNOT reach the result — a non-escaping lambda
  parameter whose products DO reach the result re-taints the
  result position, so both directives ride the same escape
  analysis. `//gom:` directives only count at line start — a
  trailing `x + //gom:...` is inert — and `gom fmt` moves
  in-expression ones to the file's end on their own lines rather
  than inlining them as `/* */`. The full directive index —
  allow/embed/noescape/result-notaint/mutable with arguments and
  attachment rules — is spec 付録 A.
- Cleanup failures join as `GoError` only. To fold them into a
  domain error type `E`, bridge the `run_result` result with
  `|> Result.map_err f`; a cleanup that itself yields `E` can
  convert `E -> GoError` inside its `defer_err` callback and still
  ride the `Close` join.
- Fetch-and-register sugar is a user helper — `s` can be passed to
  a non-retaining call:

```gom
package main

// closing is fetch-and-register sugar as a user helper: the scope
// handle `s` flows through a non-retaining call, so the escape
// check (E0419) accepts it.
fn closing (s: Scope) (open: Unit -> r) (close: (r -> Unit)) -> r = {
  let x = open ()
  Scope.defer s (fn () -> close x)
  x
}
```

```gom fail=E0419
package main

fn leak () -> Scope = Scope.run <| fn s -> s   // NG: the handle escapes

fn main () -> Unit = ()
```

## Unparenthesized lambdas inside a `|?>` chain

A lambda body extends as far right as possible, so a bare
`|?> fn x -> ...` step swallows every later step into its body.
It still type-checks, but the later steps become a nested pipeline
and the error text changes (an extra `file.gom:LINE:` prefix).
Parenthesize a lambda that only transforms one step:

```gom
package main

import go "strconv"
import go "strings"

fn port (s: String) -> Result Int GoError =
  Ok s
  |?> (fn x -> Ok (strings.TrimSpace(x)))   // OK: the lambda ends here
  |?> strconv.Atoi
```

Better still, put non-failing steps before the first `|?>` with `|>`
(`s |> strings.TrimSpace |?> strconv.Atoi`), and after it with
`|> Result.map f`. Leave the lambda bare only when you mean to capture
the earlier result for the rest of the chain (`|?> fn x -> ... x ...`).

## Fresh `GoError` values

`errors.New` and `fmt.Errorf` are standard bindings — they hand you
a `GoError` value directly (the never-nil claim is verified at the
call boundary). `errors.Join`/`errors.Unwrap` are `Option GoError` —
match `Some`/`None`:

```gom
package main

import go "errors"

fn fail (msg: String) -> Result Int GoError = Err (errors.New msg)

fn inner (e: GoError) -> Option GoError = errors.Unwrap e
```

Any other lone-`error`-result Go function defaults to
`Result Unit GoError`; a file's own `extern` may re-declare it
`-> GoError` (never-nil claim — a nil return panics at the
boundary) or `-> Option GoError` (nil-able value). `errors.As`
needs an out-pointer: `Ptr.new ()` at `Ptr (Ptr T)` (a `**T`) or at
`Ptr GoError` (a `*error`) is the target to pass.

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
- No `Type.new` constructor for records — a record value is a
  literal (`{ x = 1, iter = fn n -> [n] }`) or a `with` update.
  E0201 says `Box` has no constructor `new`.
- A lambda parameter's own type variable must be bound —
  `fn (x: a) -> x` is E0412 unless the enclosing signature or a
  generalizing top-level `let` owns `a` (spec 3.3).
- `&&`/`||` are NOT short-circuit in generated Go — both operands
  evaluate. Use `if` when evaluation order or side effects matter.
- `import "mod/path"` loads a Gom package through its `.gomi` —
  spelled as a full module path (`import "example.com/m/shapes"`)
  and resolved through the module graph (any module in the build
  list, vendoring honored). `import "list"`/`"option"`/`"result"`/`"cmp"`
  are the stdlib short names; Go packages use `import go "pkg"`.
- No `switch`/`case` → `match`; no `enum` → `type`; no
  `interface`/`struct`/`class` → records and ADTs; no `nil`/`null`/
  `None`-free references → `Option`; no `func`/`def`/`let fun` →
  `fn`; no bare `type X = T` alias (write `type alias X = T`; `=`
  alone always declares a new sum/record); no `while`/
  `for(;;)` → `for x in xs` or recursion; no `[]`-indexing; no raw
  strings/backquotes; no `;;`, `;;`-style block ends.

# Toolchain

Implemented today: `build`, `check`, `dump`, `test`, `run`, `fmt`,
`fix`, `info`, `list`, `explain`, `schema`. `gomi` was renamed `info` (`gom gomi`
prints the pointer and exits 2).

| Command | Use |
| --- | --- |
| `gom check FILE\|DIR...` | Parse → resolve → type-check → pattern check; no output files. A directory's `*.gom`/`*.gomi` compile as one package. `--strict` fails on any warning (`--strict=CODES` limits to the listed codes). |
| `gom build [-o out] FILE\|DIR...` | Full pipeline; writes one `.go` per `.gom` next to the input (or under `-o` dir/file). `-go` additionally runs `go build` on the written package and folds its errors back to `.gom` spans. |
| `gom dump -phase=syntax\|resolve\|check\|core\|emit FILE` | Print one phase's IR — sibling `.gom` files compile along so package members resolve; only the named file's IR prints (diagnostics go to stderr; stdout is the artifact). `emit` lists the generated Go names (`kind`, Gom name, emitted name — `(base X)` marks a suffix eviction). |
| `gom test FILE\|DIR... [-- FLAGS]` | Also writes `_test.go` beside the `.go` and runs the package's `go test -json` — FLAGS after `--` pass through (`-race`, `-run`). Library packages need `<pkg>.gomi` (the conformance gate runs here too); `-q`/`--quiet` drops the raw half of panic stacks. |
| `gom fmt [-w\|-d\|-l\|--check] FILE...` | Canonical formatter; `-` reads stdin. stdout carries the artifact; message stream to stderr. |
| `gom fix FILE...` | Apply the `safe` fixes diagnostics carry. |
| `gom info [-o OUT] FILE\|DIR...` | Draft `.gomi` interfaces from implementations; `-o -` prints to stdout. |
| `gom explain [CODE]` | Error-code explanations; no argument lists them all. |
| `gom schema` | JSON Schema of the diagnostic stream. |
| `gom list` | Importable stdlib short names (`list`, `option`, `result`, `cmp`), one per line. |
| `gom check -format text` | Human rendering instead of JSON Lines. |

Diagnostics are JSON Lines: one object per line, `kind:
"diagnostic"` entries then a final `kind: "summary"`. Exit codes:
`0` ok, `1` diagnostics, `2` usage/internal error. Details:
`references/diagnostics.md`, `docs/error-codes.md`.

`import go` answers are file-cached across `gom` invocations
(`os.UserCacheDir()/gom/goexport/`) — `GOM_GOIMPORT_NOCACHE=1`
bypasses it, `GOM_GOIMPORT_CACHE_DIR=DIR` relocates it.

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
| Typeclass-like polymorphism (dictionary passing, `Ord`/`Monoid`) | [references/ad-hoc-polymorphism.md](references/ad-hoc-polymorphism.md) |
| JSON diagnostics, error codes, fixes, jq recipes | [references/diagnostics.md](references/diagnostics.md) |
| Repo layout, testdata suites, tutorial, dev commands | [references/project-layout.md](references/project-layout.md) |
