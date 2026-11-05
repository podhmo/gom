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
| `09xx` | Lowering | User-facing diagnostics of the desugaring phase (internal/lower) — the @tailrec requirement rules of spec section 1 and limits of the desugaring step such as equality comparator expansion. |
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

### E0002 — invalid input path

**Severity:** error · **Phase:** driver · **Spec:** §12

The path of a .gom input contains a control character or invalid
UTF-8 and was refused before compilation. The path is written verbatim
into "//line" operands in generated Go (docs/design.md section 2.2),
into diagnostic spans, and into JSON output — a newline or carriage
return in it would split an emitted line and inject a spurious
directive, and undecodable bytes would leak into the generated file.
Rename the file; the check covers every Unicode control character (Cc)
and rejects strings that are not valid UTF-8 at all.

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

### E0105 — invalid byte in literal

**Severity:** error · **Phase:** syntax

A string or character literal contains a byte that cannot appear
in generated Go source: an invalid UTF-8 byte, or the NUL byte. Both
would be emitted verbatim and rejected by the Go toolchain, so the
literal is refused at lex time. Write an escape instead: \0 for NUL
and \uXXXX or \u{X...} for code points — source text must be valid
UTF-8, so arbitrary bytes cannot be spelled in a literal.

```gom
let s = "<0xEB>"
```

## 02xx — Name resolution

### E0201 — undefined name

**Severity:** error · **Phase:** resolve · **Spec:** §2

The name does not resolve to any variable, constructor, type, or
module in scope. This also covers the label punning form "~name",
which is defined to reference a variable of the same name (spec
section 2.7): "~to" without a variable named "to" reports "undefined
variable `to`". The same code covers wrong-kind uses that
have no legitimate reading, in either direction of the value/type
divide — a module name in expression or callee position reports
"`fmt` is a module, not a value", and a value-level name
used where a type was written (a parameter annotation spelling a
variable or a Go member function) reports "`Ptr` is a func, not a type"
— the checker emits that one, resolution the rest — rather than an
arity diagnostic derived from the func's signature.

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
discard a value deliberately, write "ignore e". The diagnostic
offers a safe fix that wraps the statement in "ignore (...)" —
the discard the code already performs, made explicit — which
"gom fix" applies (spec section 12 item 5).

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
Because a partially applied Gom call is a legal function value,
E0404 currently fires only for Go callees — a Go call never curries,
so every declared labeled parameter must be supplied (see
docs/problems/2026-11-02-gom-missing-label-is-partial.md for the
Gom-side wording question).

```gom
extern go "io" {
  fn Copy (~dst: Writer) (~src: Reader) -> Result Int64 GoError
}

io.Copy ~src: f   // ~dst: is missing — a Go call cannot be partial
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

### E0412 — unbound type variable

**Severity:** error · **Phase:** check · **Spec:** §3.3

A type variable in a "let" annotation has nothing to bind it:
type variables quantify over a declaration — a "fn" signature, a type
declaration, or a generalized top-level "let" — and a "let" whose
pattern is not a bare name, whose right-hand side is not a syntactic
value, or that sits inside a function body cannot introduce them
(spec section 3.3, value restriction). Declare the variable on the
enclosing signature or remove it from the annotation.

```gom
fn f () -> Unit = {
  let x : a = ()   // a is not bound by f's signature
  ()
}
```

### E0413 — record type contains itself by value

**Severity:** error · **Phase:** check

A record's Go representation is a struct whose
fields are stored inline, so a field whose type expands back to the
same record — directly or through a cycle of records — describes an
infinitely large value, which Go rejects as an invalid recursive
type. Recursion is still allowed across representations that box
their payload: sum-type variants (sealed interfaces), function
types, and the reference-shaped builtins List, Ref, Ptr, Array,
Go.Slice, Go.Map, and the channel types.

```gom
type Point = {x: Point}   // use a sum type or {f: () -> Point}
```

### E0414 — division by a constant zero

**Severity:** error · **Phase:** check · **Spec:** §3.3

The right operand of "/" or "%" is a constant
expression that evaluates to zero, so the operation can only fail —
and the emitted Go would not even compile, because Go rejects a
division whose divisor is a constant zero. For the integer types the
diagnostic fires whenever the divisor is constant, dividend or not:
Go rejects "x / 0" even though x is a variable. For Float, division
by zero yields the IEEE values +Inf, -Inf and NaN at run time, so
"x / 0.0" stays legal and only a constant dividend makes it an error
("10.0 / 0.0"). Constant expressions are the forms that emit Go
constants: numeric literals, unary "-", the arithmetic operators
over them, and type ascriptions — named constants and calls are not
folded.

```gom
fn f () -> Int = 10 / 0   // the divisor is a constant zero
```

### E0415 — file-local type shadows a builtin the construct needs

**Severity:** error · **Phase:** check · **Spec:** §3.1

A file-local "type" declaration may reuse a builtin type's
name — "type Result", "type Option", "type List", "type Int" — and
then shadows the builtin completely (spec section 3.1): every
unqualified use of the name in the file denotes the local
declaration. Constructs that specifically need the builtin — "try"
and "|?>" (Result), the builtin constructors Some/None/Ok/Err, list
literals and list patterns (List), a select receive arm's bind
(Option), comparisons and "if" conditions (Bool), literals and
numeric constraints (the scalar builtins), and signatures supplied
from outside the file that mention the name — are reported here,
because the builtin is unreachable inside the file.

```gom
type Result = | Ok Int | Err String

fn f () -> Int = try compute ()   // try unwraps the builtin Result
```

### E0416 — recursive call instantiates a type parameter at a growing type

**Severity:** error · **Phase:** check · **Spec:** §3.3

A recursive call — directly or through a cycle of
generic functions — instantiates a type parameter at a strictly
larger type: the call inside "fn wdepth (w: W a)" passes "W (List a)"
to wdepth's "a". The emitted Go generics would need an unbounded
set of instantiations (A, List[A], List[List[A]], …), which the Go
compiler rejects as an instantiation cycle, so the program is
diagnosed here. Renaming the parameters — "f (x: a) (y: b)" calling
"f y x" — stays legal: the instantiation set is still finite.

```gom
type W a = | WLeaf a | WNode (W (List a))

fn wdepth (w: W a) -> Int =
  match w { WLeaf x -> 0; WNode inner -> wdepth inner + 1 }   // wdepth at W (List a)
```

### E0417 — literal overflows its type

**Severity:** error · **Phase:** check · **Spec:** §3.3

An integer or float literal must be representable by the
static type at its own position — the same rule Go applies to the
constant the generated code emits. A literal typed Int or Int64
must fit int64; Int32 or Char must fit int32; Float must be a
representable float64. A literal that is the direct operand of a
unary "-" is judged with the sign, so -9223372036854775808 is a
legal Int64 while 9223372036854775808 alone is not. A literal that
is the direct operand of "Go.conv" is judged at the conversion's
target type, since the emitted "T(lit)" is a constant conversion
to T — a float literal operand at an integer target must be
integer-valued and in range ("int32(1.5)" is rejected while
"int32(2.0)" is legal, matching Go's constant-conversion rule).

```gom
let n : Int32 = 3000000000   // does not fit int32
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

### E0505 — misplaced spread

**Severity:** error · **Phase:** check · **Spec:** §8.1

A spread "..xs" expands a list in place. It appears in three
positions and is always the last element: a list literal ([x, ..xs]
is cons), a list pattern ([x, ..rest] binds the tail), and the last
argument of a variadic Go call (fmt.Println(..xs), spec section
8.1.4). Anywhere else — mid-argument, mid-literal, or a non-list
operand — is this diagnostic. In a Go call the spread must also
supply the variadic parameter alone: f("tag:", ..xs) shares the
"...any" parameter with the written "tag:" and has no Go spelling.

```gom
fmt.Println(..xs, "end")   // the spread must be last
fmt.Println("tag:", ..xs)  // and must own the variadic tail alone
[1, ..xs, 2]               // the literal tail must be last too
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

### E0508 — type argument does not satisfy a Go constraint

**Severity:** error · **Phase:** check · **Spec:** §8.5

A generic Go function whose type parameter carries the
"comparable" constraint needs a type argument whose generated Go
representation is comparable (spec section 8.5). Lists, arrays, maps,
slices, functions, and Gom type variables (which instantiate as "any")
are not comparable; records and tuples are comparable only when every
field is. Wrap the call behind an extern binding or a Go helper, or
choose a type argument that is comparable.

```gom
cmp.Or [1, 2] [3]   // List Int is not comparable
```

### E0509 — extern signature does not match the Go member

**Severity:** error · **Phase:** check · **Spec:** §8.4

An "extern" entry must name an exported function of the Go
package the block overrides, and its declared signature must be a
redeclaration the Go signature can support: same parameter count and
view-compatible parameter types, and a result declaration the result
shape admits — Option over a (T, bool) comma-ok pair, a *T result, or
a lone error (Option GoError reads nil as None); Result over
(T, error) — Result (Option T) GoError admitted when the first
result is nil-able (*T or error); a lone error also takes
Result Unit GoError (the default view) or GoError itself — the
never-nil claim, enforced by a panic check at the call boundary;
a tuple over any other two-result shape
(spec section 8.4). The compiler's standard bindings — errors.New and
fmt.Errorf as GoError, errors.Join and errors.Unwrap as Option
GoError — apply through this same admission, so an extern can also
redeclare them back to their type-derived default.

```gom
extern go "flag" {
  fn Lookup (name: Int) -> Option (Ptr Flag)   // error: name is String
}
```

### W0501 — Go call receives a copied List where mutation may be expected

**Severity:** warning · **Phase:** lint · **Spec:** §8.5

A Go function whose signature returns Unit can only matter
through a side effect — and when one of its parameters is a Gom List
(Go's []T), the boundary hands it a copy (spec section 8.5), so a
callee whose contract is to mutate the slice in place — sort.Strings
is the canonical example — has no observable effect. The lint fires
on that combination: a Unit-result Go call whose argument crosses
the boundary by copy — a List argument (including ..xs spreads into
variadic parameters and a List into an extern-declared Go.Slice
parameter) or a Go.Slice argument the checker materialized into a
List first. A bare Go.Slice reaches its []T slot verbatim — the
extern Go.Slice parameter, a ..gs spread, a ...[]T element — so
those stay silent. Read-only []T consumers never return Unit, so
the shape keeps false positives rare (signal.Notify is the known
one — the warning is advisory either way). To let Go mutate the
data, pass a Go.Slice binding declared through extern instead.

```gom
import go "sort"
let xs = ["b", "a"]
sort.Strings xs   // the copy is sorted; xs is unchanged
```

### W0502 — errors.As target cannot satisfy Go's contract

**Severity:** warning · **Phase:** lint · **Spec:** §8.4

errors.As requires its target argument to be a non-nil
pointer whose element type either implements error or is an
interface type — anything else panics at run time, a contract Go's
type system does not express. The derived signature
(GoError, Go.Any) -> Bool accepts every argument, so the lint
warns on the provable violations: a non-Ptr argument, a literal
Ptr.nil, or a Ptr over a concrete shape that is neither an
interface nor able to implement error (builtin concrete types,
function and tuple types, declared records). Named Go types stay
silent — a *T over a Go error type is the intended spelling and
the checker cannot disprove it cheaply.

```gom
import go "errors"
let target = Ptr.new () : Ptr Int   // *int implements no error
errors.As e target                  // W0502 — panics at run time
```

## 06xx — Packages and interfaces

### E0601 — missing .gomi interface file

**Severity:** error · **Phase:** iface · **Spec:** §7

Every library package (anything but "package main") must have
exactly one .gomi interface file that declares its public API (spec
section 7.4). "gom info" generates a draft from the inferred
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

### E0604 — stray .gomi interface file

**Severity:** error · **Phase:** iface · **Spec:** §7

A package has exactly one .gomi, named "<package>.gomi"
(spec section 7.4): a file whose name does not match the package it
sits with is stray. Rename or remove it. Several .gomi files for one
package name are stray too — merge their declarations into one.

### E0605 — conflicting package declaration

**Severity:** error · **Phase:** resolve

The .gom files of one directory compile into one Go package
(docs/decisions/2026-11-05-package-compilation-unit.md): the files
compiled together as a unit must declare the same "package" name,
like Go's own "found packages a and b" rule. Rename the clause or
move the file to its own directory.

```gom
// a.gom declares "package shapes"
// b.gom:
package ops    // conflicting package declaration
```

### E0606 — package import cycle

**Severity:** error · **Phase:** resolve

Gom packages may not import each other in a cycle — through
their .gom files, transitively, or back to the importing package —
because the generated Go packages would not build ("import cycle not
allowed"). Break the loop by moving the shared declarations into a
third package both sides import.

```gom
// a/a.gom: import "example.com/m/b"
// b/b.gom: import "example.com/m/a"
// import cycle: example.com/m/a -> example.com/m/b -> example.com/m/a
```

### E0607 — member emits a colliding Go name

**Severity:** error · **Phase:** iface

The .gomi surface must name members whose generated Go
identifiers do not collide. Generated code claims package-scope
names deterministically — type names verbatim, then values by
emitted base — and a member that loses emits a suffixed name
(Foo1) an importing package cannot spell: "lib.foo" compiles to
"lib.Foo", the other claimant's name. Rename either member so the
emitted names differ (docs/decisions/
2026-11-05-exported-value-vs-type-name.md).

```gom
// lib.gomi:  let foo : Foo
// lib.gom:   type Foo and let foo — "foo" would emit Foo1
```

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

### E0705 — empty select

**Severity:** error · **Phase:** check · **Spec:** §10

A select expression requires at least one arm. The empty form
"select {}" would lower to Go's "select {}", which blocks forever —
hardly ever the intent, and never a value-producing expression.

```gom
fn poll () -> Int = select {}   // write at least one arm
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

### E0803 — assert outside a test block

**Severity:** error · **Phase:** check · **Spec:** §11

assert and assert_eq compile to test-assertion calls that need the
*testing.T a generated test function binds, so they may appear only
inside a test block (spec section 11.2). Whether assertions will gain
a meaning in ordinary code is an open question; until then this
diagnostic reports the restriction.

```gom
test "ok" {
  assert (1 + 1 == 2)
}

fn prod () -> Unit = assert true   // no test function binds t
```

## 09xx — Lowering

### E0901 — lowering invariant violated

**Severity:** error · **Phase:** lower

A contract between the type checker and the lowering phase —
or inside the Core IR lowering produced — was violated: malformed
call information, an unbound or twice-bound symbol, a decision tree
that cannot enumerate its subject's type, or a finding of the Core
validator on generated IR. This is compiler-bug evidence (the E9004
of the desugaring side), not a mistake in the .gom program — see
docs/problems/2026-11-02-lower-input-contract-gaps.md.

### E0902 — recursive call is not in tail position

**Severity:** error · **Phase:** lower · **Spec:** §1.1

A function marked "@tailrec" requires every self-recursive call
to sit in tail position — the position whose value is the function's
result, reached only through block tails, if/match/select arm
results, and the outermost application of a "|>" or "<|" pipeline.
Anything that consumes the call's result — an operand position, a
"let" binding, a "for" body, "try", "|?>", or a nested lambda —
takes it out of tail position, as does a call inside a nested fn.
Calls inside "go { ... }" are exempt: the goroutine runs on a fresh
stack. Move the call to the result position or drop the attribute.

```gom
@tailrec
fn f (n: Int) -> Int =
  if n <= 0 { 0 } else { f (n - 1) + 1 }   // the call feeds "+"
```

### E0903 — function marked @tailrec is used as a value

**Severity:** error · **Phase:** lower · **Spec:** §1.1

Inside its own body, a function marked "@tailrec" may not be
referenced as a value: an alias such as "let g = f" turns "g x" into
a call through a variable, which the tail-call conversion cannot
recognize, so constant stack can no longer be proven. Passing the
function to other code is still allowed *outside* its own body.
Call the function directly instead of aliasing it, or drop the
attribute.

```gom
@tailrec
fn f (n: Int) -> Int = {
  let g = f                       // g is an alias, not a direct call
  if n <= 0 { 0 } else { g (n - 1) }
}
```

### E0904 — @tailrec function is mutually recursive

**Severity:** error · **Phase:** lower · **Spec:** §1.1

Go has no general tail-call optimization, so a cycle of
direct references "f -> g -> f" — calls, function values, or
package-level "let" bindings holding one, all of which can be
applied — grows the stack even when every call is in tail position;
only a self-recursive function compiles to a loop. The check follows
only references the compiler can resolve statically: recursion
routed through a function value it cannot see into — a parameter
fed back into the recursion, a returned function — is outside the
"@tailrec" guarantee. The structured data names one other
declaration on the cycle ("through") and the reference path
("path"). Break the cycle — inline one side or restructure the
recursion — or drop the attribute.

```gom
@tailrec
fn even (n: Int) -> Bool = if n == 0 { true } else { odd (n - 1) }

fn odd (n: Int) -> Bool = if n == 0 { false } else { even (n - 1) }
```

### E0905 — @tailrec function has no self-call in tail position

**Severity:** error · **Phase:** lower · **Spec:** §1.1

"@tailrec" asserts that the function is tail-recursive, so a
marked function must contain at least one self-recursive call the
compiler can convert to a loop iteration — a call in tail position.
The report covers the shapes where nothing converts: no self-call
at all, every self-call hidden behind a value or an alias (E0903 or
E0904 names the escape route), or every self-call inside a "go"
body — spawned calls are exempt from the position rule but run on
another stack, so no loop is emitted for them. A misplaced call
that simply sits outside tail position is reported as E0902
instead. Remove the attribute or add a convertible recursive
call.

```gom
@tailrec
fn answer () -> Int = 42   // no self-call anywhere
```

### E0906 — equality comparator expansion exceeds budget

**Severity:** error · **Phase:** lower

"==" on a composite type emits one generated comparator
function per distinct operand type. A generic type whose expansion
keeps growing — a self-application like "type W a = {next: List (W (W a))}",
where each level instantiates a *larger* type — needs an unbounded
set, so lowering stops at the budget and reports here instead of
hanging. Degrading the comparison to the reflect.DeepEqual fallback
is not done silently: that reading diverges from "==" on shared
backing stores (NaN, alias cycles).

```gom
type W a = {next: List (W (W a))}
fn eq (x: W Int) (y: W Int) -> Bool = x == y   // no finite comparator set
```

### E0907 — package-level initialization cycle

**Severity:** error · **Phase:** lower · **Spec:** §5.1

Package-level "let" bindings initialize eagerly in dependency
order, following the same rule Go applies to "var" declarations:
a binding's initializer must wait for every variable the
initializer — or a function body the initializer references —
mentions. When the bindings form a cycle no order exists, and the
generated Go could not compile.

A function value stored inside an aggregate is the usual culprit.
When the bound function is a plain reference to a local "fn" —
"let (f, _) = (g, 0)" — the compiler emits a named wrapper calling
"g" directly, which removes the dependency. The diagnostic
therefore means one of the remaining shapes: a cycle through a
non-function binding, through an element the compiler cannot trace
to a function (a call, a conditional, a variable holding the
aggregate), or through a lambda that mentions its own binding.
Restructure by referencing the function inside a function body
rather than binding the value.

```gom
fn g (n: Int) -> Int = x + n
let (f, x) = (g, 41)   // tuple -> g -> x -> tuple: no valid init order
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

### E9006 — evaluation resource budget exhausted

**Severity:** error · **Phase:** eval

The evaluator stopped because it crossed a resource budget:
it reduced more expression nodes than the step budget set with
eval.SetMaxSteps, or it nested more function calls than the
call-depth budget (eval.SetMaxDepth, default 50000). The budgets
are a harness device (the eval-vs-compiled differential test): a
tree-walking interpreter cannot be timed out from outside, so a
diverging program — unbounded recursion, a very long loop — is
cut off structurally instead of growing the Go stack without
limit; the depth bound additionally keeps the fatal goroutine
stack ceiling unreachable. It does not mean the program is wrong,
only that it did not finish within the bound.
