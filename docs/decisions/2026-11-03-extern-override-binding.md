# extern go binding: signature-level overrides with declared conversion rules

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-03-extern-binding-verification.md)）
- 日付: 2026-11-03

`extern go "pkg" { fn F ... }` binds one exported *function* member
of the named Go package. This file records what an extern may
override, how the binding is validated, and which Core IR shape the
override selects.

## Scope: function members only

An extern entry is a function signature and the member it overrides
must be a `*types.Func`. Overriding a type, var, const, or method is
rejected (E0509, "is a %s, not a function"). Var/const overrides
would need a value-position conversion channel that does not exist
(Core IR has no "ExprFromGo"); type members are already load-time
decisions (`member` maps them, Hidden marks the unrepresentable).
Method overrides were considered and dropped: method sets are
go/types facts the checker answers through MethodOf, not through
the package member table — there is no member symbol to replace.

## What the declaration must agree on

The declared signature is the *Gom-facing* view; the raw Go
signature stays authoritative for the emitted call. Binding
requires:

- the same parameter count, and each declared parameter type
  view-compatible with the raw one (`viewsCompatible` — the Equal
  views, the `Int`/`Int64` ↔ `int` numeric split, and the
  `List` ↔ `Go.Slice` boundary pair);
- a monomorphic declaration (no type variables — a Go member is
  not polymorphic; generic Go functions cannot be overridden yet);
- a result declaration the raw shape admits:

| Go results | admissible declared result | GoResults |
|---|---|---|
| `()` | `Unit` | `None` |
| `T` | `T` (compatible) | `One` |
| `*T` | `Option (Ptr T)` — `Option` requires the payload to keep the pointer | `Nil` |
| `error` | `Result Unit GoError` | `ErrOnly` |
| `(T, error)` | `Result T GoError` | `Err` |
| `(T, bool)` | `Option T` or `(T, Bool)` | `OK` / `Pair` |
| `(A, B)` | `(A, B)` | `Pair` |

A zero-parameter Go function is declared `fn F () -> T` — or
equivalently `fn F (u: Unit) -> T`: the sole `()`/`Unit` parameter
is the `f ()` call form's Unit literal, not a Go argument. The
bound signature drops it outright, matching the nullary `Fn` the
derived view gives `func() T`, so calls check and lower
identically (`pkg.F()`). `(u: Unit)` over a Go function with real
parameters is not a wildcard — it still fails view-compat.

`Option` over a non-pointer single result, or `Result` over
`(T, bool)`, is a lie the binding refuses — E0509 at the entry.

`Int64` joins the numeric compat pairs (`Int64` ↔ `int`/`int64`):
it renders the same `int64` as `Int`, and the spec's `io.Copy`
example declares `Result Int64 GoError` over `(int64, error)`.

## Pipeline placement

- `resolve` keeps extern type-scope lookups to `KindType` members —
  `String` inside `extern go "flag"` means the builtin String, not
  `flag.String` the func (the "Ptr takes 3 type argument(s)"
  misdiagnosis came from letting the func's signature stand in for
  the type name).
- `check` extends `SignatureSource` with an optional `ExternSource`
  (`BindExtern`): the declared sig is handed down during value
  registration; issues come back as E0509s at the entry, and a
  clean bind stores the override signature (the declared type plus
  the raw signature's variadicness) in `externBound`, which `sigOf`
  prefers.
- `goimport` implements `BindExtern` against the loaded package:
  member lookup, shape classification, and registration of the
  extern symbol's `member` record — `GoSigOf` then answers the
  override's raw facts (result shape `GoResultsNil` for `*T`+`Option`)
  exactly like a derived member.
- `lower` adds `core.GoResultsNil`: `OptionFromGo` accepts it beside
  `GoResultsOK`; `goCallTyp` reports the raw `*T`; the loaderless
  fallback `goResultsOf` reads `Option (Ptr T)` as `Nil`.
- `codegen` emits `v := f(...)` + `option.FromGo(v, v != nil)` for
  `Nil`; the comma-ok path is unchanged.

A failed bind leaves the *declared* signature standing — call sites
keep the declared view while E0509 explains the entry, instead of
silently reverting to the derived member shape (which would make
the call compile against a type the user did not write).

## Deliberate scope change: value symbols in type position

The `symTypeCtor` gate the misdiagnosis needed reaches beyond
extern signatures: any type-position name resolving to a
value-level symbol (`KindFunc`/`KindMethod`/var/builtin) now reports
E0201 `` `X` is a func, not a type `` instead of silently meaning
the symbol's own type (`y : os.Args` used to typecheck as
`List String`) or borrowing its arity for a bogus constructor
error. It also fixes `KindCtor` arity: `Ok` in type position spells
`Result` with the ADT's two arguments, not the payload's one.
Recording here since the behavior change is user-visible outside
the extern surface this decision covers.

## Open points deferred

- Extern entries that only rename labels (`~dst:`/`~src:` on
  `io.Copy`) already work — labels live on the declared signature.
- Non-function overrides, method overrides, and externs over
  generic callees stay rejected until a real need shows up.
