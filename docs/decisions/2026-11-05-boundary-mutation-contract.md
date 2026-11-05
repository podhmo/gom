# Boundary mutability contract: Gom guarantees hold inside Gom; the generated Go surface does not defend

- 状態: 採用（文言契約 — 生成コードは変えない。実測との照合は [検証](../sketch/2026-11-05-boundary-mutation-contract-verification.md)。spec.md §3.1/§7/§8.5/§8.7 へ反映済み）
- 日付: 2026-11-05
- 関連: [go-interop-boundary](2026-11-01-go-interop-boundary.md)、[value-representation](2026-11-01-value-representation.md)、[goconv-list-slice-copies](2026-11-04-goconv-list-slice-copies.md)、[generated-code-goals](2026-11-01-generated-code-goals.md)、[problems: go-boundary-copy-depth](../problems/2026-11-05-go-boundary-copy-depth.md)

## Context

The wave8 boundary-aliasing verification
([sketch](../sketch/2026-11-04-boundary-aliasing-hypotheses.md),
fixtures `testdata/gocall/boundary` + `testdata/gocall/box`)
measured what the generated Go surface actually guarantees. Three
observed behaviors do not match a plain reading of the spec:

1. **The spec 8.5 copy rule has no effect at the generated API
   surface.** `fn echo (xs: List Int) -> List Int` emits
   `func(xs list.List[int64]) list.List[int64] { return xs }` — the
   caller's own `[]int64` backing enters and leaves verbatim. The
   copies spec 8.5 promises exist only at the boundary-conversion
   points the compiler emits *inside* Gom code (call arguments and
   results, exported-field reads, `Go.conv`, channel sends into
   `[]T` parameters). Go code calling the generated API directly
   (`boundary.Echo(in)`, then `in[0] = 9`) mutates what a later Gom
   call observes.
2. **An exported `let` is a writable Go package var.** `.gomi`'s
   `let shared : List Int` emits `var Shared list.List[int64]`, so
   Go can both write elements (`Shared[1] = 42`) and rebind the
   binding itself (`boundary.Shared = []int64{7}`) — while spec 7
   describes `.gomi` `let` entries as 定数.
3. **`List (Go.Slice Int)` is an immutable-spelled container holding
   mutable elements.** A `[][]int64` field read materializes the
   `List` copy one level deep; every inner slice stays a shared
   `Go.Slice` over Go's backing. "`List` is immutable" needs an
   element-level caveat wherever it is claimed.

The question the verification left open: close these gaps by
contract wording (candidate 1 — the OCaml posture), by defensive
copies at the generated surface (candidate 2), or by making `List`
abstract from Go (candidate 3).

## Decision

Adopt candidate 1. **Immutability is a guarantee Gom's own
operations uphold; the generated Go surface does not defend against
Go writes.** Concretely:

- **spec 8.5 scopes itself to Gom-originated boundary crossings.**
  The `List` ↔ `[]T` copies happen where the compiler emits
  boundary conversions inside Gom code — Go-call arguments and
  results, exported-field reads, `Go.conv`, channel sends. The
  copy is one level deep (`slices.Clone`): a nested `[]T` reads as
  `List (Go.Slice Int)` whose elements stay Go-owned and shared.
- **spec 8.7 gains the Go-side contract.** Values the generated API
  hands to Go — `list.List[A]` arguments and results, `List`
  payloads inside records and variant structs, and the package var
  of an exported `let` — pass without copying. Go code must not
  write through them: no element mutation, no rebinding an exported
  `let`'s var. Violations are contract violations in the spec 8.6
  sense: Gom-side guarantees are void for that program, not
  diagnosed. This is the OCaml FFI posture — a C stub *can*
  `Store_field` an immutable block; the discipline is on the
  foreign side — and it matches Go's own exported-`var`
  conventions (`http.DefaultClient`, `os.Stdout`, `time.Local` are
  assignable; docs own the contract).
- **Exported `let` keeps the `var` shape.** Go has no read-only
  exported variable (`const` cannot express a slice or a computed
  initializer), so `.gomi`'s 定数 becomes a package `var` whose
  contract forbids assignment. Spec 7 keeps the Gom-side word
  "定数" and points at 8.7 for the generated shape.
- **The element-level caveat is stated once, in 8.5.** "`List` is
  immutable" means Gom provides no `List`-mutating operation; the
  mutability of the *elements* is their own type's contract — the
  W0701 set (`Ref`/`Array`/`Ptr`/`Go.Slice`/`Go.Map`) and values
  Gom does not represent (opaque Go named types, functions over
  captured mutable cells) stay mutable inside a `List`. This was
  always true (`List (Ref Int)` never froze its refs); the
  `[][]int64` field read is only the first place it becomes
  visible without opting in.

## Alternatives

- **Defensive copies at the generated API surface (candidate 2) —
  rejected.** Every exported call would copy every `List` argument
  and result, and the guarantee would *still* be partial: a shallow
  copy cannot protect `Ref`/`Ptr`/`Go.Slice`/`Go.Map` payloads,
  opaque Go values, or function closures, and a deep copy hits
  cycles and observable pointer identity
  ([problems/go-boundary-copy-depth](../problems/2026-11-05-go-boundary-copy-depth.md)).
  The outcome is full cost for a guarantee that remains leaky —
  strictly worse than an honest contract. It also leaves `let`
  rebinding untouched (vars are not copied).
- **Abstract `List` on the Go side (candidate 3) — rejected for
  v1.** Replacing `type List[A] []A` with an opaque wrapper
  forfeits `len`, indexing, and `range` on the Go side — the
  "Go 側からも最も自然な形" rationale of
  [value-representation](2026-11-01-value-representation.md) — and
  still cannot defend non-`List` payloads or `let` rebinding. If a
  read-only Go surface is ever wanted it is a representation-level
  follow-up, not a contract change.
- **Read-only façade for exported `let` — deferred.** Emitting
  `var shared` + `func Shared() T` removes rebinding but not
  element mutation through the returned `List`, and cannot help
  `Ref`-/`Go.Map`-typed `let`s at all — the contract is needed
  regardless. The façade is generated-API hardening (a codegen
  change, no language change) and can be added later without
  touching this decision's contract.
- **Deep copy on conversion** — rejected: same cycle/identity
  argument; the element types are not all Gom-representable, so a
  uniform deep copy does not exist.

## Consequences

- spec 8.5's first bullet gains the scope sentence and the
  one-level note; 8.7 gains the contract paragraph; spec 7's
  `.gomi` item gains a pointer to the generated shape; spec 3.1's
  `List a` row gains the element caveat.
- No compiler change: the observed generated code *is* the
  contract. The `testdata/gocall/boundary` fixtures pin the shapes
  this decision blesses (verbatim `List` pass-through, package
  `var`, shared `Go.Slice` elements).
- `docs/problems/2026-11-05-go-boundary-copy-depth.md`'s third
  question ("do the same boundary conversions apply when Go calls
  generated APIs?") is answered: **no — that surface is
  contract-governed, not copy-governed.**
- The exported-`let` residual is honest: Go *can* physically
  violate the contract (the var is assignable); nothing in the
  toolchain stops it — same posture as a C stub writing an OCaml
  block. If generated APIs grow a hardening mode later, the façade
  above is the increment.
