# How deep is the immutability guarantee at the Go boundary?

Raised by a second-opinion review (2026-11-05). The current `List`
boundary conversion in `stdlib/list/list.go` copies with
`slices.Clone` — a *shallow* copy. If the element type contains
slices, pointers, maps, or interfaces, the referenced objects stay
shared between the Gom side and the Go side, so a mutation on either
side is visible to the other. That silently breaks the language's
immutable-by-default contract at exactly the place it is least
visible.

This is not a claim that today's paths are all wrong — it is a call
to decide the guaranteed depth *before* P3 widens the interop
surface, because the answer changes both the conversions we generate
and what we promise users.

## Questions to settle

- `[][]T`, records containing lists, ADTs containing slices: to how
  many levels does a boundary conversion copy?
- A Go pointer / map / `interface{}` reaching Gom: do we expose it
  as an immutable-looking Gom value (dishonest), as an opaque
  mutable handle (a distinct "Go ref" view type), or reject it?
- Calling generated APIs directly from Go (no Gom caller): do the
  same boundary conversions apply, or is that contract void?
- Cyclic / shared structures make a blanket deep copy both hard
  (needs visited-maps) and semantically wrong (pointer identity is
  observable on the Go side).

## Directions worth a small prototype

- Split the boundary into two contracts: values *converted into*
  Gom value types (deep enough to be honest — recursively for
  Gom-representable shapes, rejecting or copying-through cycles)
  versus values kept as mutable Go references (a separate opaque
  type that only `import go` calls can consume).
- `slices.Clone` stays correct only for element types that are
  fully scalar; generated conversions should pick the copy depth
  from the static type rather than a runtime generic.

## Status

Open. Partially answered (2026-11-05): the third question —
"do the same boundary conversions apply when Go calls generated
APIs?" — is settled **no**: that surface is contract-governed, not
copy-governed, and spec 8.7 now says so
([boundary-mutation-contract](../decisions/2026-11-05-boundary-mutation-contract.md),
spec 8.5/8.7). The depth question for Gom-originated crossings
stays open — spec 8.5 records only that the copy is one level
(`slices.Clone`), not a decided depth policy.

Related: `docs/decisions/2026-11-01-go-interop-boundary.md`,
P3-3/P3-4 (Go collections, named types) in TODO.md.
