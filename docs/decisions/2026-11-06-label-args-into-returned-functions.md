# 返り値関数のパラメータを指すラベル引数 — カリー鎖をまたいだ並べ替え・充填を採用

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-label-args-into-returned-functions.md)。spec.md §2 へ反映済み）
- 日付: 2026-11-06
- 関連: [reclassify sketch](../sketch/2026-11-05-reclassify-arg-to-param-guard.md)（`ArgToParam` ガードの不変条件）、[overapply-labeled-params](../problems/2026-11-03-overapply-labeled-params.md)

## Context

A declared callee whose return type is a labeled function —
`fn f (~a: Int) -> ~b: Int -> Int` — exposes two parameter lists:
the declared list `~a` and the flattened list `~a, ~b`. Today a
labeled call resolves against the flat list (`paramByLabel` returns
flat index 1 for `~b`), but `checkGomApply` then marks any index
`>=` the declared arity as `overDeclared` and nils `ArgToParam`.
The call records no diagnostic and lowering falls back to
positional application:

- `f ~b: "x"` emits `f("x")` — `int64` cannot take `"x"`, so the
  generated Go does not build.
- `same ~b: 5` on `fn same (~a: Int) -> ~b: Int -> Int` emits
  `same(5)`, binding `5` to `~a` — a silent misbinding.
- `f ~b: "x" ~a: 1` emits `f("x")(1)` — reordering is dropped.

The checker's own residual type already reads the fill correctly:
`applyResidual` over the flat `used` set types `f ~b: "x"` as
`~a: Int -> Int`. It is only the call convention that lies.

The same call through a `let`-bound value — `let g = same` —
already emits the correct eta-closure (`func(a){ g(a)(x) }`),
because CallUnknown treats the whole flat list as the arity. But
the evaluator panics on it: `applyArgs` indexes `f.Remaining()`
(the callee's own VFunc level) and a position into the returned
function's level is out of range — E9004. So today the two
observable semantics disagree on the same program depending on
*how the callee was bound*: `same ~b: 3 ~a: 1` misbinds in both,
`g ~b: 3 ~a: 1` compiles correctly but panics under eval.

The declared/flat split is a calling-convention detail the type
system does not see: `types.Equal` compares the flattened arrow
chain, `fn f (~a) -> ~b -> T` and `fn f (~a) (~b) -> T` spell the
same type, and a `let`-bound value of the same type accepts the
call the declared name cannot answer. Making label resolution
depend on the declaration boundary would be incoherent.

## Decision

### `~label:` binds by name across the callee's whole curried chain

A labeled argument may name any parameter in the callee's
flattened parameter list, including parameters of a returned
function level. `f ~b: "x"` on `fn f (~a: Int) -> ~b: Int -> Int`
is the cross-level fill `fn ~a -> f ~a ~b: "x"` — the same
eta-expansion `partialCall`/`unknownCall` already emits for a
non-prefix fill inside one level. `f ~b: "x" ~a: 1` applies as
`f(1)("x")`.

### `ArgToParam` entries may reach the flattened arity

`CallInfo` gains `FlatArity` — the callee's flattened parameter
count (0 means Arity, for hand-built records and Go calls).
`ArgToParam[i]` continues to map `Args[i]` to a parameter
position, but the position domain widens from `[0, Arity)` to
`[0, FlatArity)`. The old rule that a non-nil mapping implies
`Applied <= Arity` becomes `Applied <= FlatArity`; an over-applied
call keeps the nil mapping it always had (positional extras have
no parameter position to point at).

### Classification: prefix-fill is Direct, a gap is Partial

With deep fills representable, the Direct/Partial split is no
longer "did the call reach the declared arity". A call is:

- **CallDirect** when the filled flat positions form a prefix
  `{0..k}` covering the declared level (`k >= declArity-1`) — the
  callee applies directly and every further filled position chains
  onto the returned function value. `f ~a: 1 ~b: "x"` is Direct.
- **CallPartial** when an unfilled flat position sits below a
  filled one — `f ~b: "x"` (the `~a` slot is open). `Unapplied`
  becomes the count of unfilled flat positions, so
  `Applied + Unapplied == FlatArity` whenever the mapping is
  representable; the closure binds the remaining positions in
  declaration order and saturates the call inside.

Positional behavior is unchanged: a `pi < 0` argument (an extra
beyond the flat list, or a label already reported E0405/E0406)
nils the mapping and the call classifies positionally, as today.

### `reclassifyCalls` classifies the same way

When a `let`-bound lambda flips to `KindFunc`, calls recorded as
CallUnknown keep their flat-indexed `ArgToParam` — the guard now
drops the mapping only when an entry falls outside the flat list
(the recovery shape it was written for), and re-runs the same
prefix/gap classification against the newly declared arity. A
`g ~b:` that reclassifies to a gap becomes CallPartial, matching
what `checkGomApply` would have recorded had the kind been known.

### The evaluator mirrors the eta

`eval.applyArgs` treats a position beyond the VFunc's own
`Remaining()` as a deep fill: the argument is bound once the level
that owns it is reached, so a gap produces the same partial
function value the compiler emits. `f ~b:` evaluates to a function
awaiting `~a` and `f ~b: 3 ~a: 1` answers `f(1)(3)` — the same
value the compiled binary produces.

## Alternatives

- **Reject cross-level labels (E04xx)** — rejected: the rejected
  program is exactly the one the `let`-bound path already accepts
  and lowers correctly. The declared/flat split is invisible at
  the type level (`types.Equal` flattens), so the rule would make
  callability depend on how the callee was *bound*, not on its
  type — `let g = f; g ~b:` would stay legal while `f ~b:` errors.
  It also leaves the reclassified `CallUnknown` calls behind:
  `let g = fn ...` flipping to `KindFunc` after its call sites
  were recorded would still need a semantic for deep mappings.
- **Reject only when a gap exists** (`f ~a:1 ~b:"x"` allowed,
  `f ~b:"x"` rejected) — rejected: the two spellings of one type
  would accept different calls; the residual `~a -> Int` is a
  function the language already represents faithfully.
- **Rewrite the surface call into an explicit eta lambda in the
  checker** — rejected: synthesizing a `Lambda` with fresh symbols
  during `checkApply` duplicates what `partialCall` and the
  evaluator's eta already express in the typed/Core layers, and
  loses the labeled-argument diagnostics' precise spans.

## Consequences

- `f ~b: "x"` compiles to `func(a int64) int64 { return f(a)("x") }`
  and evaluates to the same partial function; `same ~b: 5` binds
  `~b`, not `~a`; `f ~b: "x" ~a: 1` applies `f(1)("x")`.
- `CallInfo.Validate`'s mapping bound moves to `FlatArity`; the
  `CallPartial` accounting becomes `Applied + Unapplied ==
  FlatArity`. `CallUnknown` is untouched — its arity was already
  the flat one.
- `docs/spec.md` section 2 records that a label names a parameter
  anywhere in the callee's flat type; a call can fill parameters
  of a returned-function level only by label (a positional
  argument still never crosses into a labeled parameter, and
  still applies level by level otherwise).
