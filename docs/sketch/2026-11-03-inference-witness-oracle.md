# Sketch: inference witness oracle — bring-up log

Verification report for
[decisions/2026-11-03-inference-witness-oracle.md](../decisions/2026-11-03-inference-witness-oracle.md)
(TODO `V-4`): the typed-mode generator records a `Witness` per
emitted expression (byte span + wanted surface type) and a
`Guaranteed` flag; `internal/build/infer_test.go` replays each
witness against the `check` phase's inferred type as an instance
check.

- 日付: 2026-11-03, go1.27.x linux/amd64, worktree `wt/v4-hmprop`
- Sweep command: `GOM_INFER_SEEDS=<n> go test ./internal/build/
  -run 'TestGeneratedInfer|TestKnownInferBugs'`

## Sweep results

| seeds/mode | mode | accepted (witness-checked) | rejected | slipped | contract violations |
| --- | --- | --- | --- | --- | --- |
| 300 | typed | 268 (215) | 32 | 85 | 0 |
| 300 | typed-det | 242 (242) | 58 | 58 | 0 |
| 1000 | typed | 908 (731) | 92 | 269 | 0 |
| 1000 | typed-det | 840 (839) | 160 | 161 | 0 |

"Accepted" = zero diagnostics; "witness-checked" = accepted AND
guaranteed, so every witness was asserted against the typed tree.
A probe on clean samples confirmed every witness resolves to a
typed node (`missed=0/282`, `missed=0/99`), so the oracle is not
silently empty.

`gomgen`'s own floor test moved up too: `TestGeneratedParses`
reports `typed: 300/300 clean parses (100.0%)` after the `-(e)`
parenthesization.

## Failure classes found during bring-up — all generator-side

Every guaranteed-program diagnostic the oracle surfaced traced back
to the generator writing a program whose types did not mean what
the emitter intended — i.e. generator contract violations, not
inference bugs:

1. **`-(e)` glued into binary minus.** `-` is in the lexer's
   `line-start` set, so `prev\n-(e)` continues the previous line:
   `Ok 503.56\n-(40e3)` parsed as `Ok 503.56 - (40e3)` — producing
   E0401 "expected a floating-point type, got Result ?3 ?4" cascades
   and one E0101 (`}\n-(6e1)`, which cannot continue at all). Fixed
   by always emitting `(-(e))` — `(` is not in `line-start`.
2. **`Go.Any` oracle overreach.** `fmt.Println` arguments sit at
   `...any`; `checkBlock` stamps `want` on the node directly, so a
   `{563.33}` argument infers `Go.Any` — legitimate `any` boxing
   (verified `gom build` accepts and lowers it), but no single
   surface want matches both `Go.Any`-stamped blocks and
   natural-typed atoms. Fixed by suppressing witnesses inside
   `fmt.Println` arguments (`noWit`).
3. **ph-pattern metavariable leaks (E0407).** Bindings recorded
   from generalized top-level lets (`let seed = []` → `seed : List
   ph`) were picked where nothing pins the instantiation —
   `for z8 in seed` strands `?a`. Fixed three ways:
   `varsOf` drops `ph`-carrying bindings under `noTodo` (a
   metavariable expected-type position); `for` iterables and
   `match` scrutinees always require ph-free bindings
   (`concreteVarsOf` — their element type is *inferred*, not
   pinned); and `polyAtomOf` became `polyValueOf`, emitting only
   syntactic values — a non-value in a generalizing let
   (`Ok <var>`, `[<var>]`) fails `isValue` and strands the outer
   constructor's meta.
4. **Checker-vs-generator shadowing.** `varsOf` collected every
   same-named binding; the checker resolves to the latest. Fixed
   earlier by newest-first dedup.
5. **Labeled constructor fields.** `(w: T)` fields emitted as
   named positionals then called/matched with `~w:` — `fieldDef.
   labeled` now mirrors declaration and use.

No genuine `check` inference bug has been found so far —
`knownInferBugs` remains an empty capped registry. The asymmetry
under class 2 (`checkBlock` stamps `want` while if/match/select/
recordLit go through `bw`) is filed as an observation in
[implementation-gaps](../implementation-gaps.md).

## What the test does NOT claim

- A "rejected" program is not a bug — diagnostics are a correct
  answer for near-misses and the still-plausible fraction of
  ill-aimed emissions; only *guaranteed* programs with diagnostics
  fail.
- The oracle compares *types*, not *types at every node*: witnesses
  cover `exprOf`-level emissions and their structural parents, not
  every leaf inside (`emit`ed tokens between witnessed() calls are
  unchecked — spacing/identifiers are exercised by the sandbox
  contract instead).
- `ph` is a positional wildcard — it cannot express a shared
  quantified variable (`a -> a`), so lambda generalization is
  simply not generated (polyValueOf emits no `fn` literals).
