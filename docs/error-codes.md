# Error codes

<!-- Code generated from internal/diag/codes.go by "go generate ./internal/diag". DO NOT EDIT. -->

This file is generated from the error-code table in
[internal/diag/codes.go](../internal/diag/codes.go). Regenerate it with:

    go generate ./internal/diag

Diagnostic codes are stable identifiers (spec section 12): a code never
changes meaning and is never reused, even after the diagnostic it named
is removed — such codes stay in the table marked **retired**.

## Numbering scheme

A code is `LCCNN` — five characters:

- `L` — the severity: `E` error, `W` warning, `N` note.
- `CC` — a two-digit category naming the area of the language the
  diagnostic belongs to (see the table below). Categories are assigned
  once and never renumbered; they do not mirror the pipeline order.
- `NN` — a serial inside the category, allocated lowest-first.

To allocate a new code, take the lowest free serial in the right
category. If a category is exhausted, add a new category rather than
renumbering.

## Categories

| Prefix | Category | Scope |
| --- | --- | --- |
| `00xx` | Compiler and toolchain | Driver-level diagnostics: failures of the compiler itself and Go toolchain messages mapped back to .gom sources. |
| `01xx` | Syntax | Lexing and parsing errors of .gom and .gomi sources (internal/syntax). |
| `02xx` | Name resolution | Undefined or duplicate names, imports, and attributes (internal/resolve). |
| `03xx` | Pattern matching | Exhaustiveness, redundancy, and pattern-shape rules of match, let, and function parameters (internal/pattern). |
| `04xx` | Type checking | Unification failures and the expression-level typing rules of the spec (internal/check). |
| `05xx` | Go interop | Go calls, the `_` placeholder, spreads, conversions, and symbols that Gom cannot represent (internal/check and internal/goimport). |
| `06xx` | Packages and interfaces | Package layout, .gomi presence and contents, and implementation-to-interface agreement (internal/iface, internal/build). |
| `07xx` | Concurrency | The go/select/channel rules of spec section 10 and the mutable-sharing lints. |
| `08xx` | Tests | Test declarations, the @test scope, and assertions (spec section 11). |
| `90xx` | Evaluation | Run-time diagnostics of the typed-AST evaluator (internal/eval): unsupported constructs, panics, and broken interpreter invariants. |

## 00xx — Compiler and toolchain

### E0001 — Go toolchain reported an error

**Severity:** error · **Phase:** driver · **Spec:** §12

A command from the Go toolchain (go build, go vet, go test) failed
on code generated from a .gom source. The diagnostic is mapped back to
the .gom position and marked with "source": "go" so it can be told apart
from diagnostics produced by gom itself (spec section 12 item 6). The
original tool output is carried in the diagnostic's structured data.

This usually indicates a compiler bug rather than a mistake in the .gom
source: generated code is expected to always pass Go's own checks.

## 01xx — Syntax

### E0101 — syntax error

**Severity:** error · **Phase:** syntax

The parser encountered a token that cannot appear at this position
— the generic fallback for inputs that do not match the grammar
(docs/grammar.ebnf). The structured data carries the found token and,
when known, the set of expected tokens. Frequent causes are a "{"
placed on the line after a control-expression head (the newline rules
of spec section 1.5 require it on the same line, as in Go), a match arm
whose "->" starts a new line, and characters such as "?", "!", "&" or
"#" that are not Gom tokens.

```gom
fn f (x: Int) -> Int =
  if x > 0
  { x }            // "{" must stay on the condition's line (spec 1.5)
  else { 0 }
```

### E0102 — unterminated string literal

**Severity:** error · **Phase:** syntax

A string literal was not closed by a '"' on the same line, or a
"${" interpolation inside it was never closed by a "}". Multi-line
strings do not exist; write an explicit escape or concatenate.

```gom
let msg = "hello, world
```

### E0103 — unterminated block comment

**Severity:** error · **Phase:** syntax

A "/*" comment was not closed by "*/" before the end of the file.
Block comments do not nest.

```gom
/* this comment is never closed
let x = 1
```

### E0104 — invalid escape sequence

**Severity:** error · **Phase:** syntax

The backslash sequence in a character or string literal is not one
of the escapes the lexer accepts (docs/grammar.ebnf section 2.3).
Supported escapes include \n, \t, \r, \0, \\, \', \", \$, \uXXXX and
\u{X...}.

```gom
let s = "column\qvalue"
```

## 02xx — Name resolution

### E0201 — undefined name

**Severity:** error · **Phase:** resolve · **Spec:** §2

The name does not resolve to any variable, constructor, type, or
module in scope. This also covers the label punning form "~name",
which is defined to reference a variable of the same name (spec
section 2.7): "~to" without a variable named "to" reports "undefined
variable `to`".

```gom
transfer alice ~to ~amount: 100   // error when no variable
                                    // named "to" is in scope
```

### E0202 — duplicate definition

**Severity:** error · **Phase:** resolve · **Spec:** §11

Two top-level definitions in one package use the same name.
Definitions marked @test share the package namespace, so a test
definition may not reuse the name of an ordinary definition either
(spec section 11.3).

```gom
fn area (s: Shape) -> Float = ...
fn area (s: Shape) -> Float = ...   // duplicate
```

### E0203 — cannot resolve import

**Severity:** error · **Phase:** resolve · **Spec:** §8

An "import" declaration names a package that cannot be loaded:
a Gom package whose .gomi is missing or unreadable, or a Go package
that the toolchain cannot find. Symbols inside an importable Go
package that Gom cannot represent are reported separately (E0501);
"import go" itself succeeds for them (spec section 8.5).

```gom
import go "example.com/no/such/pkg"
```

### E0204 — unknown attribute

**Severity:** error · **Phase:** resolve · **Spec:** §11

An "@name" attribute other than the defined "@test" was used.
The grammar accepts attributes syntactically and rejects unknown ones
during name resolution (docs/grammar.ebnf section 1).

```gom
@memo
fn fib (n: Int) -> Int = ...
```

## 03xx — Pattern matching

### E0301 — non-exhaustive match

**Severity:** error · **Phase:** pattern · **Spec:** §4

The match arms do not cover every value of the scrutinee type.
The diagnostic lists up to five counterexample patterns
("missing_patterns" in the structured data) and usually offers a
placeholder fix that adds arms with "todo ()" bodies. Arms with guards
never count toward exhaustiveness: a guarded arm covers nothing the
checker can rely on. Patterns over infinite types (Int, String) need
a wildcard or variable arm.

```gom
fn is_some (o: Option Int) -> Bool =
  match o {
    Some _ -> true
    // None is not covered
  }
```

### E0302 — unreachable match arm

**Severity:** error · **Phase:** pattern · **Spec:** §4

The arm can never match: earlier arms already cover every value
it would accept. Guarded arms are exempt — a guard may fail, so the
arm can still be reached. Remove the arm or reorder it before the
arms that shadow it.

```gom
match o {
  _ -> 0
  Some x -> x   // unreachable: the wildcard already covers Some x
}
```

### E0303 — refutable pattern in an irrefutable position

**Severity:** error · **Phase:** pattern · **Spec:** §4

Positions that must always bind — the left side of "let" and
"for ... in", and function parameters — accept only irrefutable
patterns (spec section 1.1.5): variables, wildcards, tuples, record
destructuring, and single-constructor types. A pattern that can fail
(a constructor of a multi-variant type, a literal, a non-empty list)
must be matched with "match" instead.

```gom
let Some x = option_value   // Some can fail; use match
```

### E0304 — or-pattern alternatives bind different variables

**Severity:** error · **Phase:** pattern · **Spec:** §4

Every alternative of an or-pattern ("p | q") must bind the same
set of variables with the same types, so the arm body sees identical
bindings whichever alternative matched. Give the differing variables
the same name and type, or split the arm.

```gom
match s {
  Circle r | Rect w h -> ...   // r vs w, h: different bindings
}
```

### E0305 — non-exhaustive select (retired)

**Severity:** error · **Phase:** pattern · **Status:** retired

This code was reserved for a select-specific exhaustiveness
check. It is retired and will never be reassigned: select receive
arms bind Option values and branch in their bodies with ordinary
match expressions, so coverage is reported by E0301 instead
(decisions/2026-11-01-select-receive-arms.md).

## 04xx — Type checking

### E0401 — type mismatch

**Severity:** error · **Phase:** check

The generic unification failure: an expression's inferred type
cannot be made equal to the type its context requires. This covers
wrong operand and argument types, arms of if/match/select that
disagree, "|?>" pipelines whose value or step is not a matching
Result, and Go assignments that violate the interop typing rules.
The structured data carries the expected and actual types.

```gom
let x = 1 + "a"
```

### E0402 — non-Unit expression in a block

**Severity:** error · **Phase:** check · **Spec:** §1.1

Every expression in a block except the last one must have type
Unit (spec section 1.1.2); a non-Unit value produced mid-block is a
compile error because silently dropping results hides bugs. To
discard a value deliberately, write "ignore e".

```gom
fn main () -> Unit = {
  let xs = [1, 2, 3]
  List.map double xs   // computed and dropped: write "ignore (...)"
  ()
}
```

### E0403 — missing else branch

**Severity:** error · **Phase:** check · **Spec:** §1.1

"else" may be omitted only when the "if" branch has type Unit
(spec section 1.1.3). An if expression that produces a value must
provide both branches.

```gom
let sign = if x > 0 { 1 }   // needs "else { ... }"
```

### E0404 — missing labeled argument

**Severity:** error · **Phase:** check · **Spec:** §2

The callee declares a labeled required argument ("~name: T")
that the call does not supply. Labeled arguments may be written in
any order but may not be omitted; partial application supplies some
labels now and leaves the rest in the resulting function type.

```gom
transfer alice ~to: bob          // ~amount: is missing
```

### E0405 — duplicate labeled argument

**Severity:** error · **Phase:** check · **Spec:** §2

The same label was supplied twice in one call.

```gom
transfer alice ~to: bob ~to: carol ~amount: 100
```

### E0406 — unknown label

**Severity:** error · **Phase:** check · **Spec:** §2

The call passes a "~name:" argument that the callee does not
declare. Check the spelling against the function's signature (or the
.gomi entry for imported functions).

```gom
transfer alice ~to: bob ~amont: 100   // ~amont is not declared
```

### E0407 — type annotation required

**Severity:** error · **Phase:** check · **Spec:** §3.3

Arithmetic operators are overloaded over the built-in numeric
types and are resolved to a concrete type at the end of type checking
(spec section 3.3). When nothing fixes the type, the compiler asks
for an annotation rather than guessing.

```gom
let x = read_number () + 1   // + stays ambiguous: annotate x
```

### E0408 — labeled call on an unresolved function type

**Severity:** error · **Phase:** check · **Spec:** §2

A call that uses labels requires the callee's type to be known
at the call site (spec section 2.1), because labels are part of the
function type. When the callee's type is still a type variable, add a
type annotation.

```gom
fn apply f = f ~to: 1   // the type of f is unknown here
```

### E0409 — == applied to a type without structural equality

**Severity:** error · **Phase:** check · **Spec:** §3.3

"==" is structural equality and can only be used on types that
contain no function (spec section 3.3); equality of type variables is
not allowed in v1. The same restriction applies to assert_eq in
tests (spec section 11.4).

```gom
(fn x -> x) == (fn x -> x)
```

### E0410 — try in a function that does not return Result

**Severity:** error · **Phase:** check · **Spec:** §6

"try e" escapes to the innermost enclosing function (fn
definition, lambda, or test block) by returning the error from it, so
that function's return type must be "Result _ e" (spec section 6.2).
"go { ... }" is not a function boundary — see E0702.

```gom
fn parse (s: String) -> Int =
  try strconv.Atoi(s)   // parse must return Result Int GoError
```

### E0411 — try error type does not match the function's error type

**Severity:** error · **Phase:** check · **Spec:** §6

The error type of the tried Result must equal the error type of
the enclosing function's return type (spec section 6.2): a function
returning "Result a GoError" cannot try a "Result b MyError" without
converting the error first.

```gom
fn f () -> Result Unit MyError = {
  try strconv.Atoi(s)   // GoError is not MyError
  Ok ()
}
```

## 05xx — Go interop

### E0501 — Go symbol cannot be represented in Gom

**Severity:** error · **Phase:** goimport · **Spec:** §8.5

The Go package is importable, but this symbol's signature uses
types Gom cannot represent (unsafe.Pointer, generic functions with
constraints other than any/comparable, and so on) and is therefore not
exposed (spec section 8.5). The structured data carries the reason.
An extern declaration can override the imported signature when a
representable view exists.

```gom
import go "unsafe"
let p = unsafe.Pointer(x)   // unsafe.Pointer is not a Gom type
```

### E0502 — method call on a Gom value

**Severity:** error · **Phase:** check · **Spec:** §8.1

Go-style method calls "x.M(args)" are only valid on values of Go
types (spec section 8.1.3); Gom types have no methods. Call the
corresponding Gom function instead.

```gom
shape.Area()   // write: area shape
```

### E0503 — implicit numeric conversion not allowed

**Severity:** error · **Phase:** check · **Spec:** §8.1

Between Gom and Go, numeric types convert implicitly only when
no information is lost: Int ↔ int/int64, Float ↔ float64 (spec
section 8.1.5). Other conversions (Int to int32 or float32, for
example) must be written explicitly with Go.conv.

```gom
let r: Int32 = Go.conv x : Int32   // x: Int needs the explicit form
```

### E0504 — misplaced _ placeholder

**Severity:** error · **Phase:** check · **Spec:** §8.2

"_" as a call argument is the partial-application placeholder
(spec section 8.2): it is allowed only inside a Go function call and
at most once per call. In patterns and let bindings "_" is the
wildcard, which is a different feature.

```gom
strings.Repeat(_, _, 3)   // only one _ is allowed
```

### E0505 — misplaced spread argument

**Severity:** error · **Phase:** check · **Spec:** §8.1

A spread argument "..xs" expands a list into a variadic Go call;
it may only appear as the last argument of a Go function call (spec
section 8.1.4). It is not a general list-splicing syntax.

```gom
fmt.Println(..xs, "end")   // the spread must be last
```

### E0506 — unsupported generic constraint

**Severity:** error · **Phase:** check · **Spec:** §8.5

Gom can call a generic Go function only when every type
parameter's constraint is any or comparable (spec section 8.5); other
constraints have no Gom-side meaning. Wrap the call behind an extern
binding or a Go helper.

```gom
maps.Clone(m)   // only if the constraint is any/comparable
```

### E0507 — invalid Go.conv conversion

**Severity:** error · **Phase:** check · **Spec:** §8.4

"Go.conv e : T" is checked against Go's conversion rules (spec
section 8.4): it works for converting between a named type and its
underlying type or between numeric types, but not for conversions Go
itself would reject.

```gom
Go.conv "abc" : Int   // String is not convertible to Int
```

## 06xx — Packages and interfaces

### E0601 — missing .gomi interface file

**Severity:** error · **Phase:** iface · **Spec:** §7

Every library package (anything but "package main") must have
exactly one .gomi interface file that declares its public API (spec
section 7.4). "gom gomi" generates a draft from the inferred
signatures.

### E0602 — disallowed declaration in .gomi

**Severity:** error · **Phase:** iface · **Spec:** §7

A .gomi file may contain only type declarations, function and
constant signatures, and /// doc comments (spec section 7.1). It
contains no expressions: signatures describe the interface, the .gom
files provide the implementation.

### E0603 — implementation does not match .gomi

**Severity:** error · **Phase:** iface · **Spec:** §7

The package's definitions must agree with the signatures in its
.gomi: same names, same argument shapes (including labels), same
types up to type-variable renaming (spec section 7.6). Either fix the
implementation or update the interface.

## 07xx — Concurrency

### E0701 — go block must produce Unit

**Severity:** error · **Phase:** check · **Spec:** §10

"go { e }" starts a goroutine and has type Unit, so e must have
type Unit (spec section 10.1) — a value produced by a goroutine would
be silently lost. Return results through a channel or Task.

```gom
go { compute x }   // if compute returns a value it is lost
```

### E0702 — try escapes across a go boundary

**Severity:** error · **Phase:** check · **Spec:** §6

A "go { ... }" block is not a function boundary (spec section
6.3): a "try" written directly inside it would escape to the enclosing
function — across a goroutine boundary — which is an error. Wrap the
body in a lambda or use Task.spawn so the try has a function to
escape to inside the goroutine.

```gom
go { let r = try fetch url }   // use: go { (fn () -> { ... }) () }
```

### E0703 — multiple default arms in select

**Severity:** error · **Phase:** check · **Spec:** §10

A select expression may contain at most one "default ->" arm,
matching Go's select.

```gom
select {
  r <- jobs -> handle r
  default -> 0
  default -> 1   // only one default is allowed
}
```

### E0704 — select receive arm must bind a variable or _

**Severity:** error · **Phase:** check · **Spec:** §10

The left side of a select receive arm "x <- ch" may only be a
variable or "_" (spec section 10.5) — every arm maps one-to-one onto a
Go select case, so a destructuring pattern cannot be decided there.
Match on the received "Option a" value in the arm's body instead.

```gom
select {
  Some job <- jobs -> work job   // write: r <- jobs -> match r { ... }
}
```

### W0701 — channel of mutable element type

**Severity:** warning · **Phase:** lint · **Spec:** §10

The channel's element type contains mutable values (Ref a,
Array a, Ptr T, Go.Slice a, Go.Map k v). Sharing mutable values
across goroutines is legal but race-prone, so the linter warns
(spec section 10). Prefer sending immutable Gom values, or make the
sharing explicit.

```gom
let ch = Chan.make 0   // inferred as Chan (Ref Int)
```

### W0702 — go expression captures a mutable variable

**Severity:** warning · **Phase:** lint · **Spec:** §10

The "go { ... }" block captures a variable whose type contains
mutable values (Ref a, Array a, Ptr T, Go.Slice a, Go.Map k v), so
the goroutine can mutate state shared with its parent (spec section
10). Pass copies or immutable values instead.

```gom
go { Ref.set counter 1 }   // counter: Ref Int is shared
```

## 08xx — Tests

### E0801 — invalid test body type

**Severity:** error · **Phase:** check · **Spec:** §11

A test declaration's block must have type Unit or
"Result Unit e" (spec section 11.1); returning Err fails the test and
any other type is rejected.

```gom
test "doubling" { 1 + 1 }   // Int is not Unit or Result Unit e
```

### E0802 — @test definition referenced from non-test code

**Severity:** error · **Phase:** resolve · **Spec:** §11

Definitions marked @test live in a test-only scope: they can be
referenced only from test declarations and other @test definitions
(spec section 11.3), and they never appear in the package's .gomi.

```gom
@test
fn helper () -> Int = 1

fn prod () -> Int = helper ()   // helper is test-only
```

## 90xx — Evaluation

### E9001 — construct not supported by the evaluator

**Severity:** error · **Phase:** eval

The typed-AST evaluator (internal/eval) — the tree-walking
interpreter the semantics-comparison harness checks generated Go
against (design.md section 7) — does not implement this construct in
its current scope. Reported for "go"/"select" expressions, "for"
over non-List iterables, "Go.conv" ascriptions, misplaced holes and
spreads, Ref/Array/Ptr/Chan/Task values, unknown builtins, module
members in expression position, and Go calls for which no host
function was bound (eval.NewHostFunc). This is a limitation of the
interpreter, not a rule of the language: the same program may compile
and run through the Go backend.

```gom
go { ping () }   // goroutines are outside the evaluator's scope
```

### E9002 — no match arm accepted the value

**Severity:** error · **Phase:** eval · **Spec:** §8.6

Every arm of a match rejected the scrutinee value at run time.
In a checked program the pattern checker guarantees coverage, so
this means a contract-violating value — a nil or malformed ADT
value produced by Go code — reached the match, which the spec
defines as a panic naming the type (spec section 8.6). On a
hand-built typed AST it can also mean the arms genuinely do not
cover the value.

### E9003 — panic during evaluation

**Severity:** error · **Phase:** eval

A Gom-level panic surfaced as a diagnostic instead of a Go
panic: "todo ()" was reached (spec section 12 item 5), "assert" or
"assert_eq" failed (spec section 11.4), an invalid variant value
was matched, a division or modulo by zero was evaluated, or a host
function raised a Go panic. The message carries the panic text.

```gom
let x = todo ()   // panic: todo reached
```

### E9004 — evaluator or compiler invariant violated

**Severity:** error · **Phase:** eval

A broken invariant inside the evaluator or the typed AST it
consumes: an unbound symbol, a non-function applied, a pattern
typed as irrefutable that failed anyway, a "try" escaping with no
enclosing function boundary, malformed literal text, and similar.
This indicates a bug in gom itself — or a malformed hand-built AST
in a test — not a mistake in the .gom program.

### E9005 — test returned Err

**Severity:** error · **Phase:** eval · **Spec:** §11

A "test" declaration's body evaluated to "Err e", which fails
the test (spec section 11). The structured data carries the test
name ("test") and the rendered result ("value").

```gom
test "round-trip" {
  try decode input   // an Err here fails the test
  ()
}
```
