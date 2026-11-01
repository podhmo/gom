# go/types → Gom type mapping and where boundary conversions live

- 状態: 採用（P1-9 の `internal/goimport` が実装・検証した — `strconv.Atoi`/`Itoa`・`os.LookupEnv`・`bytes.Buffer` メソッドが E2E で動く）
- 日付: 2026-11-03

P1-9 turns `go/types` objects into the `check.Signature` values the
existing Go-call machinery consumes. The mapping must honour spec
8.3 (`(T, error)` → `Result T GoError`, `int`/`int64` → `Int`, named
types → `types.Go`, …) *and* produce generated Go that compiles —
which is where `int` becomes a design problem, because Gom `Int` is
`int64` while Go `int` is a distinct type.

## Two views of one signature

Every loaded parameter and result is mapped twice:

- **Gom view** — what `check.Signature.Type` carries and users see:
  `int` → `Int`, `int64` → `Int`, `int32` → `Int32`, `float64` →
  `Float`, `string` → `String`, `bool` → `Bool`, `error` → `GoError`,
  `*T` → `Ptr`, `[]T` → `Go.Slice`, `map[K]V` → `Go.Map`,
  `chan T`/`<-chan T`/`chan<- T` → `Chan`/`RecvChan`/`SendChan`,
  named types (incl. named interfaces) → `types.Go` NamedRefs,
  `any`/empty interface → `Go.Any`.
- **Go view** — the type the generated code actually needs: identical
  to the Gom view *except* `int` → `Go.Int` (a new `Con{Qual:"Go"}`
  rendering `int`). `int` is the only basic type whose Gom spelling
  (`int64`) differs from its Go spelling in the supported set.

`check.Signature` keeps the Gom view — `strconv.Atoi` is
`String -> Result Int GoError` exactly like an `extern` declaration —
so no checker rule changes for calls. The Go view rides in
`lower.Env.GoSig`'s `Params`/`ResTypes` (the same oracle slot that
already supplies arity and result shape).

## Conversions are lower's job, recorded in Core IR

Wherever a Go view differs from the Gom view the generated code
needs a real Go conversion (`int64(v)`, `int(x)`), and Core IR must
record that decision (design.md 2.1: no semantics rediscovered in
codegen):

- **Arguments**: `lower` wraps each `CallGo` argument whose atom type
  differs from the Go-side parameter in `core.Conv{ConvNum}`,
  bound by the ordinary ANF `Let` (`strconv.Itoa(int(n))`).
- **Single results**: the `CallGo`'s raw `Type()` is the Go view
  (`Go.Int`); the value is wrapped in `Conv` to the Gom view when
  they differ (`n := int64(f())`).
- **`(T, error)` payloads**: `ResultFromGo` gains a `ValConv`
  field — `rt.ResultFromGo(int64(v), err)`. `EscapeCall` needs no
  field: lowering wraps the atom it binds in a `Conv`
  (`v, err := f(); n = int64(v)`).
- **`(T, bool)`/`(A, B)` pairs**: `TupleFromGo` gains per-element
  `ElemConv`; `OptionFromGo` gains `ValConv` (extern comma-ok
  bindings may produce `int` payloads).

`check.Ascribe`/`convKind` already classify `Int`↔`Go.Int` as
`ConvNum`, so `Go.conv`-written conversions compose with the same
node.

## `any` and `Go.Any`

`any` maps to `Con{Qual:"Go", Name:"Any"}` rendering `any`. One
unify rule — *wanted* `Go.Any` accepts any argument — implements
spec 8.1.7's implicit boxing at call sites (Go auto-boxes the emitted
argument; no `Conv` is needed). A `Go.Any`-typed *result* stays
`Go.Any`: it unifies back into `any` parameters and nothing else.

## Result shapes (checked against spec 8.3)

| Go results | Gom-facing type | `core.GoResults` |
| --- | --- | --- |
| none | `Unit` | `GoResultsOne` |
| `T` | `T` | `GoResultsOne` |
| `error` | `Result Unit GoError` | `GoResultsErrOnly` |
| `(T, error)` | `Result T GoError` | `GoResultsErr` |
| `(T, bool)` | `(T, Bool)` tuple | `GoResultsPair` |
| `(A, B)` | `(A, B)` tuple | `GoResultsPair` |
| `>2` results | — | hidden (E0501) |

Comma-ok → `Option` stays extern-only per spec.

## Hidden members (E0501)

A member whose signature uses a type the mapping cannot express —
`uint*`, `float32`, `complex`, `unsafe.Pointer`, anonymous interfaces,
arrays, anonymous structs, generic functions or generic named types
(deferred), more than two results — is still entered in
`Package.Members`, but its `resolve.Symbol` carries `Hidden` (the
reason). References then bind and check reports E0501 with that
reason at the use site, instead of an E0201 "no such member".

## Methods

`x.M` selectors on `types.Go`/`Ptr`-of-`types.Go` receivers resolve
through an optional `check.MethodSource` the loader implements:
`types.NewMethodSet` of the named type (pointer receiver set when the
Gom value is `Ptr _`). Found methods mint `KindMethod` symbols whose
signatures enter the same loader table, so `CallGo`/`MemberExpr`/
`recv.M()` codegen work through the existing machinery. Fields of Go
structs stay unresolved (deferred — open question in spec 8.3).

## Deferred (filed under docs/problems/)

- `[]byte`/`uint8` and the other unsigned/float32/complex basics
- `List a` ↔ `[]T` boundary copies (spec 8.5) — `[]T` maps to
  `Go.Slice` today, so a `List` cannot feed a slice parameter and a
  `..spread` into a variadic of a different element type is unchecked
- interface *assignability* (`*os.File` into `io.Reader` args) —
  `types.AssignableTo` exists but is not wired; `Go.conv` covers it
- generics (constraint check exists in go/types; deferred wholesale)
- `func`-typed parameters needing calling-convention adapters
- package loading performance (batch `go list -deps`)
