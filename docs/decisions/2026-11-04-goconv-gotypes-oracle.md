# `Go.conv` legality: delegate to `go/types`, recheck after metas solve

- 状態: draft
- 日付: 2026-11-04
- 検証: [sketch/2026-11-04-p3-2-p3-6-interop-verification.md](../sketch/2026-11-04-p3-2-p3-6-interop-verification.md)

## Background

Spec 8.4 says `Go.conv e : T` is legal when Go's own conversion
rules allow the pair. Until now the checker answered that from its
own tables: numerics, same-underlying named types, and interface
targets — but a Go named type's underlying representation was only
approximated inside the checker, so pairs like `Int` →
`net/http.Server` could slip through or be rejected for the wrong
reason. The `InterfaceAssignable` seam introduced for spec 8.1.8
already mapped Gom boundary types to real `go/types.Type` values;
the same machinery answers convertibility exactly.

A second hole was ordering: `Go.conv 5 : http.Server` passed when
the operand was a bare literal because its type was still an
unsolved meta at check time, and the conversion test never ran on
the defaulted type.

## Decision

`convOK` gains a last-resort oracle: `check.ConvertSource` with a
single `GoConvertible(from, to) (decided, ok bool)` method,
implemented by `goimport` (mapping each boundary view to its
`go/types.Type` — named refs as the named type, `List`/`Go.Slice`
as slices, `Go.Any` as `any`, numerics as `int64`/`float64`/etc.,
`Unit` as `struct{}`) and forwarded by `gomimport.Loader`. Two
refusals carry different weights: `decided=true, ok=false` is a
disproof — either both sides mapped and `ConvertibleTo` said no, or
a `gomOnly` shape (Gom ADTs, tuples, rigid vars) proves no Go
reading exists — while `decided=false` is mere ignorance (an
unmappable Go view, unsolved metas) and leaves the pre-existing
Gom-side rules their verdict.

`boundaryType` maps every unqualified `Con` by name alone, so a
file-local `type Int` nested inside `List`/`Ptr`/`Go.Slice`/
`Go.Map`/`Chan` arguments would be read as the builtin's Go
representation — accepting the pair and emitting uncompilable code.
`convOK` therefore walks both types for a shadowed-builtin `Con` at
any depth (`containsShadowed`) and keeps the oracle out when one is
found; the pair falls to the fallback's E0507 like the old
outermost check produced.

Conversions whose operand type is an unsolved meta are recorded as
`pendingConvs` and re-validated by `revisitConvs` during `finalize`,
after defaulting — the same deferral shape as `pendingSels` /
`pendingApplies`. A literal therefore converts (or fails) under the
type it will actually have, so `Go.conv 5 : http.Server` is E0507
while `Go.conv 5 : time.Duration` stays legal.

## Consequences

- Member-style misuse `{Go.conv}` / `Go.conv 1` keeps reporting E0507
  at check time — the diagnostic fires in validation, so the
  malformed node never reaches lowering's E0901.
- `Go.conv` between `Go.Slice Int` and `List Int` is a
  representation pun (identical `go/types` underlying type): it does
  NOT copy. The spec'd boundary copy (8.5) lives in implicit
  conformance positions, which materialize `list.FromSlice` /
  `.ToSlice()`.
