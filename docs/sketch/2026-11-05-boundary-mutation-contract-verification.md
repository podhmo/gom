# Boundary mutability contract — verification (decision wording vs generated reality)

- 状態: 検証済み
- 日付: 2026-11-05 (`wt/boundary-contract`)
- 対象: [decision: boundary-mutation-contract](../decisions/2026-11-05-boundary-mutation-contract.md) — candidate 1 (OCaml posture) of [boundary-aliasing hypotheses](2026-11-04-boundary-aliasing-hypotheses.md)

## What this verification is

A wording decision has no prototype to build; what needs checking
is that the contract text *describes* the generated code rather
than aspiration, and that the rejected alternatives were weighed on
accurate grounds. Two mechanical checks:

1. `gom check` + `gom build` over `testdata/gocall/boundary` —
   inspect the generated `boundary.go` against each contract claim.
2. `go test ./cmd/gom -run TestEndToEnd_GoCallsGom` — the e2e
   driver (`gom check` → `gom build` → `go build` → `go test` over
   the fixture module) still passes after the spec edits. **PASS
   (1.09s)** — nothing the wording describes changed behavior; the
   fixture's Go-side writes are deliberate contract violations the
   surface cannot prevent, which is what the decision declares.

## Claim-by-claim against generated `boundary.go`

| contract claim | generated shape | verdict |
| --- | --- | --- |
| 8.5 copies live at Gom-originated view switches | `before := list.FromSlice(tmp6)` at every `p.Xs` field read (lines 83/91/111/121), `xxs := list.FromSlice(tmp20)` for `[][]int64` | confirmed — copies exist exactly at the positions the new 8.5 enumeration lists |
| generated API passes `List` verbatim | `func Echo(xs list.List[int64]) list.List[int64] { return xs }`, `func SharedList … { return Shared }` | confirmed — no `FromSlice`/`ToSlice` at the surface; caller backing enters and leaves |
| exported `let` is a writable package `var` | `var Shared list.List[int64] = list.List[int64]{1, 2, 3}` | confirmed — assignable, rebindable; `.gomi` `let` ↔ Go `var` |
| copy is one level deep | `xxs := list.FromSlice(tmp20)` is `slices.Clone` on the outer slice; element type stays `[]int64` (`Go.Slice Int`) | confirmed |
| records/variants share payload backing | `var pairVal Pair = func() Pair { … Pair{N: 7, Xs: tmp32} }()`; `GetPair` returns the struct value | confirmed — struct copies, `Xs` backing shared |
| mutable-representation values share by design | `Ref`/`Ptr`/`Go.Slice`/`Go.Map`/chan payloads — W0701's closed set | by construction; no copy path exists for them |

## Alternatives weighed (why wording suffices)

- **Defensive copies at the generated surface**: still shallow —
  `Ref`/`Go.Slice`/`Go.Map`/opaque payloads share through any
  element-level copy; deep copy hits cycles and observable pointer
  identity ([problems/go-boundary-copy-depth](../problems/2026-11-05-go-boundary-copy-depth.md)).
  Pays full per-call cost for a partial guarantee and leaves `let`
  rebinding untouched (vars are not copied).
- **Abstract `List` for Go**: forfeits `len`/index/`range` — the
  "Go 側からも最も自然な形" rationale of
  [value-representation](../decisions/2026-11-01-value-representation.md)
  — and cannot defend non-`List` payloads either. Rejected for v1.
- **Exported-`let` façade** (`var shared` + `func Shared() T`):
  feasible (a small `varDecl`/`symName` change, `.gomi`
  conformance unaffected since it is Gom-level), and it *does*
  remove rebinding — but `Shared()[0] = 9` still writes package
  state, and `Ref`/`Go.Map`-typed `let`s cannot be defended at
  all. The contract is needed regardless; the façade is
  generated-API hardening deferred as a follow-up.

## Spec coverage

- §8.5 bullet 1: scope ("Gom のコードの中で `[]T` と `List` の
  view が切り替わる位置 …"), one-level copy note, element
  caveat, pointer to 8.7 for the Go→Gom surface.
- §8.7: verbatim pass-through paragraph (List args/results,
  record/variant `List` payloads, exported `let` → package `var`)
  + the contract paragraph (no element writes, no rebinding;
  mutable-representation values shared; violations are spec-8.6
  contract violations — undiagnosed, guarantees void).
- §7 item 1: exported constants appear as Go package `var`s;
  writes forbidden by 8.7.
- §3.1 `List a` row: element-level caveat.
- `stdlib/list/types.go` + `stdlib/list/boundary_test.go`
  doc comments scoped to "where Gom code calls into Go";
  `testdata/gocall/boundary/boundary_test.go` header notes its
  writes are deliberate contract violations.

## Residuals (honest, recorded in the decision)

- The contract is *unenforced*: Go can physically violate it
  (`boundary.Shared = …` compiles). Same posture as OCaml's C FFI
  and Go's own exported `var`s (`http.DefaultClient`). No lint or
  vet check exists for it — a custom analysis would be the only
  enforcement, and is not planned.
- `docs/problems/2026-11-05-go-boundary-copy-depth.md` remains
  open: this decision answers its third question (generated-API
  surface is contract-governed, not copy-governed) but not the
  deeper "conversion depth" question for Gom-originated crossings.
