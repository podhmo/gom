# Pattern-matrix checking: domains, guards, counterexamples (internal/pattern)

- 状態: draft
- 日付: 2026-11-02

## Background

P1-6 implements `internal/pattern` (design.md section 3, TODO.md P1-6):
the exhaustiveness and redundancy analysis of spec section 4 on the
typed AST. The algorithm is Maranget's usefulness test ("Warnings for
pattern matching", JFP 2007 — verified in
[reference-verification](../sketch/2026-11-02-reference-verification.md));
the same paper's specialization machinery yields the counterexample
witnesses `missing_patterns` reports. Decision-tree compilation for
code generation is Maranget 2008 and belongs to P1-7 (`internal/lower`),
not to this phase. The choices below are the implementation-internal
ones the spec and design.md leave open.

## Decisions

- **Pattern matrix.** Typed patterns (`check.Pat`) are lowered to
  *cells* — a constructor head plus sub-cells, or a wildcard — so a
  match is a matrix of one-cell rows. `useful(P, q)` is Maranget's U:
  constructor heads specialize `S(c, P)`, and at a wildcard column the
  recursion splits on Σ₁, the constructors heading the matrix's first
  column — it specializes per constructor when Σ₁ is a complete
  signature and takes the default matrix `D(P)` alone when Σ₁ is
  incomplete or the domain cannot be enumerated. Or-patterns expand into
  one row per alternative (nested or-patterns take the cartesian
  product per position); as-patterns reduce to their sub-pattern;
  variables and `_` are wildcards; list patterns become Nil/Cons cells.
- **Value domains.** A type's *signature* is the enumerable set of
  constructors a value of it can have, in declaration order:
  ADT variants, the record single constructor, tuple, list Nil/Cons,
  and the finite literal domains Bool `{true, false}` and Unit `{()}`.
  Everything else — Int, Float, Char, String, function types, `Go`
  types, unbound metas and abstract (`.gomi`-only) types — has a nil
  signature, meaning the domain cannot be enumerated: a wildcard or
  variable arm is required for exhaustiveness, matching spec section
  4's "無限型のリテラルは `_` が必須". An *empty* sum type is the
  opposite edge: an enumerable-but-empty signature, vacuously covered.
- **Literals.** Literal cells key on a normalized spelling (`0x1` ≡
  `1`, `"a"` ≡ the unquoted text) so differently-spelled equal
  literals collapse; spellings that fail to normalize keep their
  source text as a distinct key, which is conservative — it can only
  over-report coverage, never under-report it. Literal domains other
  than Bool/Unit are infinite, so literal arms alone can never
  complete a match.
- **Guards.** Guarded arms are excluded from the coverage matrix
  entirely: they neither prove exhaustiveness nor make later arms
  redundant (spec section 4). Symmetrically, a guarded arm is exempt
  from E0302 — the error-codes table states "Guarded arms are exempt"
  outright, though the underlying motivation only covers the case
  where earlier arms are guarded; see
  [problems/2026-11-02-redundant-guarded-arms](../problems/2026-11-02-redundant-guarded-arms.md).
- **Termination.** The Σ₁ case split is what terminates both
  recursions on recursive types: when Σ₁ is incomplete the default
  matrix consumes the column outright, whereas specializing every
  signature constructor at a wildcard column re-derives an identical
  matrix state forever under a recursive field (`Cons`'s tail is the
  same `List` type — the first prototype looped on
  `match (l, u) { (_, ()) -> … }` with tys cycling 2→3→2). The
  measure that decreases lexicographically is the constructor-node
  count of the matrix and pattern vector (wildcards not counted),
  then the column count — the same measure the Agda mechanization of
  this algorithm uses (wasabi315/coverage-checking). The
  all-wildcard-row cut in each function is only a shortcut for the
  common covered case — such a row covers the whole column space —
  not the termination guarantee.
- **Missing-witness split.** `missing` mirrors the same Σ₁
  distinction without a separate complete case: the signature
  iterates in declaration order, a constructor present in Σ₁ is
  refined by `S(c, P)`, and each absent constructor emits `c(w̄)·w`
  once per `w ∈ missing(D(P), tys[1:])` — the default-matrix result
  computed once and shared by every absent constructor.
- **Counterexamples.** Missing witnesses enumerate depth-first in
  declaration order (the spec's `Neg (Num _)` before `Neg (Add _ _)`
  before `Let _ _ _`), reconstructing each uncovered constructor with
  wildcard arguments, capped at five per design.md section 4.2
  (`Options.MaxCounterexamples`; `<= 0` means the default). The list
  shape renders cons-spines as `[a, ..rest]`, and nested patterns are
  parenthesized per spec section 4.1 (`Some ([_, .._])`).
- **Unknown constructors.** A constructor pattern whose type has no
  registered signature (an imported ADT whose `TypeInfo` was never
  added, a misspelled variant) receives a key outside every signature:
  it covers only itself, keeps arms reachable instead of flagging them,
  and still leaves the wildcard witness missing. False negatives are
  possible only when `Env` lacks a declaration — wiring `NewEnv` from
  the file's decls plus `.gomi`-imported `TypeInfo`s (`Env.AddType`)
  is P1-11's job.
- **Diagnostics.** E0301 attaches `scrutinee_type` and
  `missing_patterns` per spec section 12, gains the
  "arms with guards do not count toward exhaustiveness" note whenever
  the match contains a guarded arm, and carries the placeholder fix
  "add the missing arms": one edit inserting `p -> todo ()` lines at
  the closing brace's column, with the first arm filling the
  whitespace already on the brace's line (byte-for-byte the spec
  example when a `diag.Resolver` supplies the source; without one the
  edit inserts the arms on fresh lines at a fixed indent). E0302
  points at the arm's span and reports `arm_index`; E0303 reports the
  same data as E0301 plus `context` ∈ {let, for, parameter}; E0304
  reports `alternative` plus `missing`/`extra`/`mistyped` name lists.
- **API surface.** `CheckMatch`, `CheckPattern(p, Context)` and
  `CheckFile` share one `checker` over `Env`; `CheckFile` walks every
  pattern site of a typed file — match arms, let/for bindings, fn and
  lambda parameters — so P1-11 only needs `NewEnv(file.Decls)` plus
  `CheckFile(file, Options{Resolver: …})`.

## Consequences / limitations

- Or-pattern expansion is multiplicative: an or-pattern under every
  field of a wide constructor multiplies the row count. Fine for real
  code; a cap or lazy expansion is a TODO candidate if profiling ever
  shows it mattering.
- Redundancy is arm-level: an or-pattern arm is unreachable only when
  *all* its alternatives are covered. Per-alternative refinement
  (Maranget 2007 §6, "useless or-pattern alternatives") is not
  implemented; see
  [problems/2026-11-02-or-pattern-redundancy-granularity](../problems/2026-11-02-or-pattern-redundancy-granularity.md).
- `useful`/`missing` recompute per arm and per column: worst case is
  exponential in pattern depth like the algorithm everywhere else
  (OCaml, rustc share it). Nothing in the design suggests this needs
  mitigation now.
- The package does not make lowering decisions: cells and signatures
  are internal to `internal/pattern`, and P1-7 will either re-derive
  its own specialization or lift these helpers behind an explicit
  shared boundary — to be decided there.
