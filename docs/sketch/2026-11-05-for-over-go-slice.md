# `for` over `Go.Slice` — prototype verification

Non-normative report for
[decisions/for-over-go-slice](../decisions/2026-11-05-for-over-go-slice.md):
the checker admits `Go.Slice a` as a `for` iterable, matching the
`ForList` lowering that already existed.

## What changed

- `checkFor` (`internal/check/expr.go`): a `*types.Con` arm for
  `Con{Qual: "Go", Name: "Slice", Args: [a]}` binds `a` — placed
  before the channel arm; E0401's expected-text reads
  "a List, a Go.Slice, or a channel" at both report sites.
- `evalFor` (`internal/eval/eval.go`): a `*VHost` iterable iterates
  `reflect.Slice`/`reflect.Array` values — the same reflect shape
  the call-spread uses — and `hostElem` boxes each element by the
  pattern's static element type: int/uint → `VInt` (`Char` →
  `VChar`), `Float` → `VFloat`, `Bool` → `VBool`, `String` → `VStr`,
  everything else opaque `VHost`. Without the unbox, `x + 1` on a
  `Go.Slice Int` element would `binMismatch` in eval while the
  compiled `x + 1` runs — an eval/compiled divergence in exactly the
  class the diff harness exists to catch. A `chan` VHost still
  fails `CodeUnsupported` (channel iteration is compiled-only per
  [eval-concurrency-compiled-only](../decisions/2026-11-04-eval-concurrency-compiled-only.md)).
- `lower`/`codegen`: untouched — `forExpr` already maps non-channel
  `for` to `ForList`, which emits `for _, x := range s`.
- spec §1.1 item 7 lists the iterable set; §8.3 `Go.Slice` notes
  `for`; SKILL.md's loops line updated.

## Verified

- `gom build` on a scratch `package main`:
  `for w in strings.Split "a,b,c" ","` lowers to
  `for _, w := range tmp2`, `go run` prints `a` `b` `c`.
- `internal/eval` unit tests: `for over a Go.Slice` iterates
  `[]int64{1,2,3}` and the bound var feeds `x + x` (an opaque
  `VHost` element would `binMismatch`); `of strings` iterates
  `[]string{"a","b"}` feeding `x < "c"`. The pre-existing "for over
  a host channel unsupported" test still reports CodeUnsupported —
  eval's contract for channels is unchanged.
- Spec fixtures: `ok_for_go_slice` (param `Go.Slice Int` +
  `strings.Split` result + wildcard form, zero diagnostics),
  `E0401_for_go_map` (`Go.Map String Int` still rejects with the new
  expected text — map order stays unspecified by design).
- `go test ./internal/check ./internal/eval ./internal/build` green.

## Boundaries observed

- `Go.Map` stays E0401 — Go map iteration order is deliberately
  unspecified; `Go.Map.to_list` remains the documented snapshot.
- `Array a` is inert — the type has no constructors or members, so
  no value reaches `for` (it lowers through `conSlice` anyway if
  one ever could).
- Named Go slice types (`type S []int`) behind `*types.Go` handles
  stay rejected — the underlying-kind oracle answers `*types.Basic`
  kinds only; widening it to `*types.Slice`/`Array` is a follow-up
  if the shape becomes reachable.
- Eval iterates scalar elements as their `Value` spelling and keeps
  non-scalar elements opaque `VHost` — a `Go.Slice`-of-named-Go
  element can only move through further Go calls under eval, the
  same limitation host values already carry.
- The generated `for _, x := range s` copies each element like Go —
  iterating a slice of structs binds copies, same as `Go.Slice.fold`.
