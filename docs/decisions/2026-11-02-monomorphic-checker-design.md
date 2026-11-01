# Design of the P1 monomorphic checker

- 状態: draft
- 日付: 2026-11-02

Implementation findings from P1-5 (`internal/check`). These are the
choices the checker makes where spec.md leaves room; each is a draft
until exercised by P1-6/P1-7 and the spec test suite.

## Declared annotations are authoritative

The P1 checker is monomorphic: a declaration's written annotation is
the source of truth and is never generalized. Metas (unification
variables) exist only *locally*:

- numeric literal defaulting (`1` is a numeric-constrained meta that
  defaults to `Int`, `1.0` to `Float`),
- operator resolution (`+` forces `num`, `%` forces `intg`,
  comparisons force `ord`, `==`/`assert_eq` force `eq` and reject
  function-containing or type-variable types with E0409),
- instantiation of a declared type's rigid variables at each use site
  (a `List a` parameter unifies `a` with the argument's element).

A meta that survives to the end of a declaration is an error (E0407),
not a polymorphic type — generalization is P2-1's. To keep diagnostics
deterministic, `finalize` sorts the live metas by source span and then
by creation id before defaulting and reporting.

### Who may instantiate

A use of a symbol instantiates the rigid variables of its signature
only when the signature *quantifies* them — declared `fn`s,
constructors, builtins, Go methods, and everything the
`SignatureSource` supplies (`fn id (x: a) -> a` reads "for every a",
so each call site gets fresh metas).

A local value binding (`resolve.KindVar` — a let binding, a
parameter, a pattern variable) is different: the `a` in its declared
type is the enclosing declaration's rigid variable and is shared by
every use of the binding. `valOf` returns the stored type unchanged.
Without this, `fn f (x: a) -> Int = x + 1` checked `x` against a fresh
meta per use, so the `num` constraint never reached the declaration's
`a` and `f "s"` compiled — a soundness hole. With the rigid reading
the body itself is rejected (a numeric constraint cannot apply to
`a`), which is the correct verdict for a monomorphic signature.

The rule has one consequence worth noting: a bare rigid annotation on
a value binding, like `let f : a -> a = ...`, can never be applied —
`a` stays rigid and `f 1` is E0401. Under the "no generalization"
rule that is the intended reading: only signatures quantify.

## Every application carries a validated `CallInfo`

`check.Apply` records `Call CallInfo` on every application node;
`CallInfo.Validate` is run before the node is returned and a violation
is an internal bug (`panic`), not a diagnostic.

- `CallUnknown`: the callee is a function-typed value, not a named
  symbol. `Callee` is nil.
- `CallDirect`: a known Gom callee that is saturated or over-applied.
- `CallPartial`: a known Gom callee applied to fewer arguments than
  its declared arity; the result is a closure and `Unapplied > 0`.
- `CallGo`: a Go function or method — the tuple argument convention,
  no currying, the only kind that may carry a `HoleIndex` (`_`, the
  cute placeholder, at most one) or a `Variadic` tail.

`CallInfo.Arity` is the callee's **declared** arity, not the length of
its flattened curried parameter list: `fn mk () -> (Int -> Int)`
declares one parameter, so `mk ()` is `CallDirect` and a second
argument over-applies into the returned function. Signatures carry
`DeclArity` for the same reason — a `Signature` whose `Type` is a
function returning a function must not confuse the outer parameter
count with the flattened shape.

`ArgToParam` maps each applied argument (in source order) to its
declared parameter index. `nil` encodes both the identity mapping and
the unrepresentable ones — over-applied calls (extras have no
parameter slot), unresolved labels (already diagnosed), and repeated
variadic slots. Consumers needing only "which parameter did this arg
fill" read `ArgToParam[i]` when non-nil and `i` otherwise.

## The environment is a `SignatureSource`

Value types come from three places, unified behind `SignatureSource`:
file-local declarations (pre-registered `fn` signatures and let-bound
metas), imported Gom symbols and `extern` bindings, and builtins
(`Some`, `None`, `Ok`, `Err`, `assert`, `assert_eq`, `todo`). Builtin
type variables live in a disjoint id range (`1 << 20`) so they never
collide with declaration-scoped variables. `MapSource` is the
test-oriented implementation; `Chain` composes sources. A `Signature`
is `{Type, Variadic, DeclArity}`.

Type-level names resolve through `tyCons`, which registers every
`type` declaration (record, sum, abstract, alias) of the file plus the
builtin constructors (`Option`, `Result`, `List`, channels, `Ptr`).

## Function unification flattens, then absorbs the tail

`unifyFunc` compares function types on their flattened curried form,
mirroring `types.Equal` — but where Equal requires equal parameter
counts, unification lets the shorter side's result absorb the longer
side's tail: `A -> ?r` unifies with `A -> B -> R` by binding `?r` to
`B -> R`. That is the same type once flattened (`->` is
right-associative), and it is what makes local inference work at all:
`fn f -> fn x -> f (f x)` fixes `f` to `?x -> ?r`, so a call whose
argument is a two-parameter lambda needs `?r` to take the tail rather
than fail on arity (a real occurs-mismatch still fails, just with one
E0401 instead of a mismatch plus dangling-metas cascade).

Labels keep their slot discipline inside that walk. A labeled
parameter and a positional one never pair; a labeled run whose labels
the other run lacks leaves its unmatched members behind, and they may
only fold into the result when that side's parameter list ends there
(`A -> ~l:B -> ?r` unifies with `A -> ~l:B -> ~m:C -> R`, `?r` taking
`~m:C -> R`). Leftover labels on *both* sides — or a leftover that
still has parameters behind it — are a genuine mismatch, since runs
separated by a positional parameter do not merge (spec section 2.1).

## `try` boundaries

`try e` escapes to the innermost enclosing *function boundary*: a `fn`
declaration, a lambda, or a `test` block. The checker keeps a stack of
boundaries; `go { ... }` is not a boundary and blocks escape (E0702).
A function whose return type is not `Result _ e` gets E0410, a
mismatched error channel E0411. A `test` body accepts `Unit` or
`Result Unit e`; its error channel is free and defaults to `GoError`
when nothing else pins it.

## Error recovery: one diagnostic per root cause

Where the surface shape already explains the failure, the checker
prefers the semantic code over a cascade:

- A *constrained* meta that can never be `Unit` (a numeric literal,
  an ordering result) produces the domain diagnostic — E0402 mid
  block, E0403 missing else, E0701 `go` body, E0801 test body —
  instead of a generic E0401 mismatch against `Unit`.
- An expression checked under an expected type whose inner statement
  already reported the mismatch takes the expected type as its own
  (`if` arms, blocks), so the same error is not reported again at the
  outer span.
- Recovery paths bind rather than dangle: an unresolved record field
  binds its pattern variable to `Unit`, a non-iterable `for` binds the
  element to the iterable's own type, a defaulted meta whose default
  violates its constraints still binds after the single constraint
  diagnostic. The goal is that a genuine source error never leaves an
  unsolved meta behind to add a redundant E0407.

## Open edges

- `CallGo` `ArgToParam` cannot express a repeated variadic parameter
  index; see docs/problems/2026-11-02-variadic-arg-mapping.md.
- E0404 (missing labeled argument) only fires for Go calls; for Gom
  calls the same source shape is a legal `CallPartial`. See
  docs/problems/2026-11-02-gom-missing-label-is-partial.md.
- Record metadata is file-local: imported record types do not yet
  resolve fields by name. See
  docs/problems/2026-11-02-imported-record-fields.md.
- `e : T` ascription is only accepted as `Go.conv e : T`; the general
  form reports E0401 pending docs/problems/2026-11-01-type-ascription.md.
