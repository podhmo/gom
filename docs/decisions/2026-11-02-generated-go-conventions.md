# Generated Go code conventions: names, unused binders, //line

- 状態: draft
- 日付: 2026-11-02

## Context

P1-8 (`internal/codegen`) had to pick concrete output conventions the
design notes leave open: how Gom names become Go identifiers, how
generated code deals with Go's unused-variable error, and how
`//line` source mappings survive gofmt. These are mechanical
conventions, not semantic decisions — Core IR has already fixed the
semantics.

## Naming

| Gom | Go | rule |
| --- | --- | --- |
| `add2`, `mk_add`, `use_answer` | `add2`, `mkAdd`, `useAnswer` | value/function names: snake_case → camelCase, kept lowercase |
| field `radius`, `x` | `Radius`, `X` | exported names capitalize |
| `type Shape` + ctor `Circle` | `Shape` / `Shape_Circle` | variant structs are `Type_Ctor` |
| ADT marker | `isShape()` | unexported `is` + type name seals the interface |
| tuple / positional payload | `V0`, `V1`, ... | positional fields are `V<index>` |
| sealed marker result | `(A, B)` | generic marker returns the type parameters (P0-1 finding: inference cannot bind a phantom parameter) |
| package-qualified | `pkg.Name` | imported packages are always referenced through a qualified identifier |

Name assignment is deterministic: every `resolve.Symbol` gets one
emitted identifier (`gen.names`), assigned up front by `indexDecls`
in file walk order. Distinct symbols that share a Gom name — a
shadowing `let x = x + 1`, a case bind repeating a parameter's name —
are disambiguated with a numeric suffix (`x`, `x1`, `x2`), because a
second `x :=` in one Go block is a redeclaration error. The dedup
set resets per declaration (`gen.fnames`): binder names only need to
differ inside their own emitted function. Package-scope names
(`gen.pused`) additionally block binder names — a same-named local
would shadow the decl for every reference inside its scope. Every
emitted identifier also lands in `gen.used`, so compiler-generated
temporaries (`v`, `err`, `ok`, `u`, `r`, `a`, `b` + counter) and
import bindings can never collide with user names or each other.
Keywords and predeclared identifiers are excluded by `safeIdent`.

Test functions are named `Test` + PascalCase of the declared test
name (`test "result"` → `TestResult`), deduplicated with a numeric
suffix on collision.

## Unused binders are elided

Go rejects declared-and-never-used variables. Core IR binders whose
symbol is never referenced inside their scope produce no Go
variable:

- a named `let x = e` whose `x` is never used emits `e` under the
  Discard destination (`_ = e`, or a bare call);
- a type-switch guard variable exists only when a case's bind is
  used (`switch v := s.(type)` vs `switch s.(type)`);
- `SwitchTag`/`SwitchList` case payload binds, `select` receive
  binds, and `for`-range element binds are emitted only when used.

The analysis (`walk.go`) is by symbol identity, not name, so
shadowing is handled exactly. This is defensive: if the checker ever
rejects unused `let`s the elision is dead code; if it does not, the
elision is what keeps generated code compilable (see
[problems/unused-let-bindings](../problems/2026-11-02-unused-let-bindings.md)).

## `//line` survives gofmt via a sentinel splice

A `//line file:line` directive only takes effect at column 1, and
gofmt does not re-indent the line following one — emitting
directives before `format.Source` leaves every mapped statement at
column 1. Instead `g.line` writes an ordinary comment
`//gomsrc:<line>:<file>`, the whole file is formatted normally, and
`render` rewrites each sentinel to `//line <file>:<line>` at column
1 afterwards (`spliceLines` in codegen.go). Indentation and source
mappings both survive.

Consequence: generated constructs with no mapped span (variant
marker methods, import block, the `for` of a TailLoop) inherit the
position of the preceding directive — harmless but visible in
`//line`-aware tooling.

## Self-declaring escapes

The fused `(v, err)` escape has two emission shapes, distinguished
by destination:

- Under the **Declare** destination — reached only when a `let`'s
  RHS *is* the escape (`let n = try f()`; `selfDeclares`) — the call
  result binds the let name directly: `n, err := f()`. No `var` is
  emitted; a `var n` would be a dead declaration.
- Under every other destination — Assign (`var n` declared in an
  enclosing block), Return, Discard — the call result binds a fresh
  variable which is then written to the destination:
  `v1, err := f(); if err != nil { ... }; n = v1`.

The split exists because destination-passing propagates `Assign`
into nested blocks (`let n = if c { try f() } else { 0 }` emits the
escape inside the `if` body): `n, err :=` there would declare a
*new* `n` shadowing the outer `var` — which then goes unused (a Go
compile error) and keeps its zero value. An earlier version claimed
`:=` "reuses the same-block `var`" — wrong; `:=` only reuses
variables *already declared in the same block*, and `var n` +
`n, err :=` in one block never co-occurs because `let` takes the
Declare path for that shape.

A `Conv` under Discard must keep `_ =` even though it prints as
`T(x)`: to Go it is a conversion, not a call, and conversions are
not valid expression statements.

## Verification

- 根拠: [sketch: P1-8 codegen verification](../sketch/2026-11-02-p1-8-codegen-verification.md)
  — the comprehensive fixture compiles under `go build`/`go vet` and
  its generated tests run under `go test`, in a real temporary
  module against `rt/` and `stdlib/`.
