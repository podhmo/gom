# Generated per-type comparators for `==`/`!=`/`assert_eq` (core.Equal + File.EqDecls)

- 状態: 採用
- 日付: 2026-11-05
- Supersedes: [structural-equality-emission](2026-11-04-structural-equality-emission.md) のうち composite 型の `rt.Equal` 一律フォールバック（`rt.Equal` 自体は Go-view 型の fallback として残る）

## Context

The 2026-11-04 decision routed every non-trivially-comparable `==`
through `rt.Equal` — `reflect.DeepEqual` — as a documented interim.
Two defects remained:

- **Semantic**: `reflect.DeepEqual` short-circuits two references to
  the *same* backing store, so `let n = NaN` gives `n == n` false
  but `[n] == [n]` true — contradicting both eval's elementwise
  `eqValue` and spec 3.3's structural reading.
- **Static**: `hasFuncOrVar` only walked `types.Con` arguments, so
  `type Box = {f: Int -> Int}` hid the function from the eq
  judgment and `x == x` compiled (E0409 required by spec 3.3).

## Decision

### The eq judgment expands declarations (internal/check)

`==`/`!=`/`assert_eq` share one constraint. The walk that decides
"contains a function or a rigid variable" now expands *local* named
types the way `recordCycle` does: record fields, ADT payloads, and
aliases unfold with the type arguments substituted into the
declaration's parameters; abstract and foreign types stay opaque.
Visits are memoized by (declaration, instantiated arguments) with a
step budget, so recursive types terminate the branch instead of
looping. An eq constraint on a type that still contains an unbound
meta (`List ?n`) flags that meta, and binding it re-runs the
judgment — `xs == ys` cannot launder a function through inference.

### The comparison recipe is a Core IR decision (internal/core)

`core.Equal{Neg, X, Y, How}` replaces `Binary{BinEq}`/`CallBuiltin{eq}`.
`How` is an `EqPlan` — the semantic decision lowering freezes and
codegen merely renders. Leaf kinds (the only kinds an Equal carries):
`EqNative` (Go `==`), `EqRuntime` (`rt.Equal`), `EqError` (nil-aware
message-text comparison for `GoError`), `EqCall` (a generated
comparator). Composite kinds (`EqSlice`, `EqMap`, `EqTuple`,
`EqRecord`, `EqSum`, `EqResult`) appear only as an `EqDecl.Plan`;
nested operand pairs always route through `EqCall`, which is what
keeps recursive types finite.

`assert_eq` carries the same plan on `CallBuiltin.Eq`; codegen
emits `rt.AssertEq(t, <verdict>, actual, expected)` — the runtime
asserts a verdict it no longer computes, so `assert_eq` answers
exactly what `==` emits.

### Lowering picks the recipe from the same oracle information
(internal/lower)

`eqKindOf` classifies the operand type with the checker's
declaration data (`typeD`) plus the type oracle:

- scalars, `Ref`/`Ptr`/channels — `EqNative`;
- tuples, records, ADTs (incl. `Option`, `Result`) built entirely
  from native-comparable parts — `EqNative`: struct `==` compares
  fields natively (preserving IEEE NaN), and a sealed interface's
  `==` compares (variant, value) — the same reading — with panic-
  freedom guaranteed because no payload can hold a slice;
- `GoError` — `EqError` (message text, matching eval's `VGoErr`);
- everything else composite — a generated `EqDecl` per distinct
  operand type, minted on first use and deduplicated by type key;
  `File.EqDecls` emits them after the file's own declarations;
- named Go types (opaque layout), `Task`, `Go.Any`, and the
  defensive leaves the checker already rejects — `EqRuntime`.

### Comparator bodies are the eval reading (internal/codegen)

- slice/`List`/`Array`: same length (nil and empty both have
  length 0 — the spec's nil==empty rule at every nesting level),
  then elementwise — no same-backing-store shortcut;
- `Go.Map`: same key set, values per element plan;
- tuple/record: fieldwise `&&`;
- sum/`Option`: nil-aware, then a variant type switch comparing
  payloads; an unexpected dynamic type answers false, never panics;
- `Result`: same `Tag()`, then `Value` for Ok and `Error` for Err.

## Deferred / documented approximations

- `EqRuntime` keeps `reflect.DeepEqual` for named Go types, `Task`
  and `Go.Any` — the representations Gom does not own. DeepEqual's
  nil-vs-empty and same-backing-store quirks can still leak through
  those, as the code comment on `rt.Equal` records.
- `Ref` stays `EqNative` — pointer identity; the content-vs-identity
  question belongs to the decision that designs `Ref`.
- `Go.Int` compares natively; other `Go.*` views use the runtime
  fallback.

## Alternatives considered

- **Keep `rt.Equal` for everything composite** — rejected: the
  NaN/same-slice contradiction is user-visible and unfixable inside
  DeepEqual.
- **Emit inline loops/switches at each `==` site** — rejected:
  recursive types need recursive functions, and one comparator per
  type keeps generated code readable and debuggable.
- **Interface `==` for all-native ADTs vs always generating
  comparators** — kept native where sound (scalars only); any
  non-native payload takes the comparator, since interface `==`
  panics on slice-typed dynamic values.

## Verification

`docs/sketch/2026-11-05-equality-comparators.md` — generated code
for every plan kind, the NaN triple agreeing between eval and the
compiled binary, the E0409 fixture matrix, and a `go test` run of
the generated test file exercising the new `rt.AssertEq` signature.
