# `any`-hole cute calls lose to premature result binding in the deferred path

- 状態: 未決

## 状況

A `_` placeholder in a variadic-`any` Go call produces a partial
function whose parameter is `Go.Any`. When such a call is checked
through the deferred member application (receiver still a meta), a
consumer can bind the application's standing result meta before the
replay produces the real `any -> Unit` type:

```gom
let f = x.Print(_, ..["b"])   -- provisional: f : ?res
let _ = f "a"                 -- ?res := String -> ?ret   (premature)
-- replay then wants ?res = any -> Unit
```

The replay's `unify(e.Type(), res)` sees `String -> ?ret` vs
`any -> Unit` and reports `expected String, got Go.Any` (E0401).
The *eager* path compiles the same program: `x.Print(_, ..)`'s type
is `any -> Unit` immediately, `f "a"` unifies `any` (want) against
`String` — the `Go.Any` boxing acceptance in unify — and succeeds.

## なぜ問題か

- unify's `Go.Any` acceptance is want-side only
  (`unify.go` — an `any` parameter accepts a concrete argument),
  because `any` in the *got* position is an opaque Go named type,
  not the universal box. Once the consumer binds the standing meta
  to `String -> ?ret`, the replay cannot express "`any` accepts
  `String` here" through unification — the two directions of the
  same fact are not symmetric.
- The deferred mechanism itself is working as designed; this is the
  same premature-binding family as the interface-conformance case
  (`deferInterfaceSource` now covers `pa.ap.Type()` for
  *want-driven* conformance). But `f "a"` binds through the *callee*
  unification in the application check — there is no conformance
  hook to defer through.
- Widening the replay's unify to "result meta already bound →
  compatibility check" needs variance/subtyping (`any -> Unit` is a
  *supertype* of the bound `String -> ?ret` under Go-any boxing),
  which the unification machinery deliberately does not have — and
  whether the type written down by `f`'s consumers should be the
  narrower one is a spec question, not a local repair.

## 考えられる方向

- Spec answer first: does spec 8.2 intend `x.Print(_, ..)`'s
  partial-function type to be the `any`-parameter one? If yes, a
  consumer that pins `String -> Unit` is *also* valid — the
  semantics needs a bounded-width compatibility, which is a
  real (if small) extension to unify.
- Alternatively a nested deferral: an application whose *callee*
  type is an open pendingApply result could itself wait — the same
  treatment pendingSels/pendingApplies already compose — but that
  spreads the deferral machinery to every Gom apply site touching a
  deferred result, a much wider change than the win justifies at
  this stage.
- Status quo: the divergence is recorded; consumers that annotate or
  use the partial in a fully typed position are unaffected.
