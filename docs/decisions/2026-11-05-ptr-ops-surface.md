# `Ptr` module: an explicit operation surface for `Ptr T`

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-p3-7-interop-ops-verification.md)。spec.md §8.3/§8.6 へ反映済み）
- 日付: 2026-11-05

## Context

The P3-7 audit
([sketch](../sketch/2026-11-05-p3-7-realworld-interop-audit.md),
[problems](../problems/2026-11-05-ptr-t-no-deref.md)) showed that
`Ptr T` — the mapping of Go's `*T` — is read-through only: methods
and fields resolve by peeling the pointer, but the pointer value
itself cannot be dereferenced, nil-tested, or constructed. Flag
values (`flag.Int64`) cannot be read back, optional `*T` parameters
(`tls.Dial`'s `*tls.Config`) cannot receive nil, and out-pointer
APIs (`errors.As`, `json.Unmarshal`, `flag.Var`) compile yet cannot
be given a usable target.

## Decision

Add a `Ptr` module in the builtin scope — like `Chan`/`Task`, a
compiler-owned member table backed by `rt` functions — with seven
members:

| member | Gom signature | Go meaning |
| --- | --- | --- |
| `Ptr.get` | `Ptr a -> a` | `*p` — panics on nil, like Go |
| `Ptr.set` | `Ptr a -> a -> Unit` | `*p = v` — panics on nil, like Go |
| `Ptr.is_nil` | `Ptr a -> Bool` | `p == nil` |
| `Ptr.new` | `() -> Ptr a` | `new(T)` — pointer to a zero value, for out-pointer APIs |
| `Ptr.make` | `a -> Ptr a` | `&v` — pointer to a fresh copy; mutation never reaches `v` |
| `Ptr.nil` | `() -> Ptr a` | nil — for optional `*T` parameters |
| `Ptr.to_option` | `Ptr a -> Option a` | nil → `None`, else `Some *p` — the safe read |

- `get`/`set` panic on nil deliberately — the same panic Go's own
  dereference produces; `is_nil`/`to_option` are the safe forms.
- `==`/`!=` on `Ptr` already work — spec section 4's equality
  rules and `internal/lower/equal.go` (`eqNative`) shipped
  pointer-identity equality on main; nil is checked with
  `is_nil`/`to_option`. (Correction at adoption: the draft text
  claimed identity equality was undecided — it was in fact
  already specified and implemented.)
- `errors.As` remains exposed as-is. `Ptr.new` plus a type
  annotation makes the intended shape expressible
  (`let t : Ptr (Ptr os.PathError) = Ptr.new ()` → `errors.As
  (e, t)` → `Ptr.get t`), and misuse still panics at runtime — the
  contract "an `any` that must be a non-nil pointer" cannot be
  stated in the type system; a dedicated binding or lint is future
  work, not this surface.
- The open-questions "*T default" entry is **not** forced: this
  surface is orthogonal — whatever `*T` results default to, the
  operations on a `Ptr T` in hand are the same.

## Alternatives

- *`Go.deref` builtin or dereference syntax*: a special form for one
  operation while the module table answers all seven; inconsistent
  with the `Chan`/`Task` shape already shipped.
- *`Option (Ptr T)`-only read path*: keeps nil safety but cannot
  write, construct, or feed `nil` to a Go parameter — the
  construction half of the surface is what out-pointer APIs need.
- *Defer construction until record literals exist*: `Ptr.new`'s
  zero-value is precisely what `errors.As`-shaped APIs need today;
  waiting leaves those APIs unusable for no benefit.
