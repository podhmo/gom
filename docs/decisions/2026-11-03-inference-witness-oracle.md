# Inference witness oracle: generator-side wanted types vs `check`'s inference

- 状態: 採用
- 日付: 2026-11-03

## Background

V-4 asks for a property test of Hindley–Milner inference: gomgen's
typed mode already aims every emitted expression at a generator-side
type, so in principle the checker should infer a compatible type
everywhere — and an E0401 on a well-aimed program is an inference
bug candidate. The existing compile canary only asks "did the
compiler keep its contract"; it never asks whether the *types* agree.

The catch is that "typed-ish" generation is not a proof: emitters
share grammar machinery that can silently write a program whose
types mean something else. Treating every emission as oracle input
produces failures that are generator contract violations, not
inference findings — which is still signal, just the wrong inbox.

## Decision

`gomgen.GenerateResult` returns `Source`, `Witnesses`, and a
`Guaranteed` flag, and the property test
(`internal/build/infer_test.go`) consumes all three:

- A **Witness** records one emitted expression's byte span
  `[Start, End)` plus the surface type the emitter aimed at. The
  test indexes the typed AST by span and takes the *largest* node
  wholly contained in the span — grouping parens produce no typed
  node, so the root is always a contained argmax, not an exact hit.
- **Matching is an instance check**: the inferred type must be a
  shape-instance of the wanted one. A rigid/generalized variable
  matches anything — `let xs = []` binds `xs : List a`, and a use
  witnessed at `List Int` is a legal instantiation. An unbound meta
  (`?n` still free) or a structural mismatch is a failure.
- **Guarantee vs slip**: emitters call `slip()` on deliberate
  near-misses (an else-less `if` on a non-Unit want, a bare
  `fmt.Println` in a Unit slot, an invented import path). Slipped
  programs stay canary input but drop out of the oracle contract.
  A *guaranteed* program carrying any diagnostic is itself a
  finding — either an inference bug or a generator contract
  violation the slip flags failed to mark.
- The known-bug machinery is reused (`knownInferBugs`,
  `fuzzutil.CheckRegistry`): failures with a registered signature
  are logged; new ones fail with mode+seed+source for replay. The
  registry is capped at 16 like the compile-bug list.
- `GOM_INFER_SEEDS` widens the default 60-seed-per-mode budget for
  longer opt-in sweeps, matching `GOM_DIFF_SEEDS`.

## Generator-side machinery the oracle needs

- **`noTodo`** marks positions whose expected type reaches the
  checker as nil or a metavariable (unannotated let values, operator
  operands, scrutinees, Go-call and `ignore`/`assert_eq` args,
  interpolation splices). Leaves that cannot pin their own metas
  (`[]`, `None`, `Ok`, `Err`, `todo ()`) either self-pin (`[e]`,
  `Some e`) or take the annotated-let pin (`({ let v : T = e; v })`);
  `atConcrete` clears the flag inside declared-type positions.
- **`ph`** is a wildcard inside a binding's recorded type PATTERN —
  a top-level `let x = []` generalizes to `List a`, recorded as
  `List ph`. `matchT` treats `ph` as matching anything, and the
  let-body emission picks the instantiation. Under `noTodo` a
  `ph`-carrying binding is unusable — nothing pins the fresh meta —
  and `for` iterables / `match` scrutinees, whose element type is
  *inferred* rather than pinned by an expected type, always require
  ph-free bindings (`concreteVarsOf`). `polyValueOf` emits only
  syntactic values: a non-value in a generalizing let strands every
  ph as an unsolved meta (E0407).
- **`noWit`** suppresses witnesses where no single wanted type is
  right — a Go `...any` argument (fmt.Println) stamps `Go.Any` on
  block nodes but keeps natural types elsewhere (the `bw` dance in
  `checkExpr`). The emissions still typecheck; they just have no
  usable oracle type.
- **Unary `-` is emitted parenthesized** (`(-(e))`): `-` is in the
  lexer's `line-start` set, so a bare `-(e)` at a fresh item start
  continues the previous line as binary minus — `Ok 503.56\n-(40e3)`
  parses as `Ok 503.56 - (40e3)`. `(` is not in `line-start`, so the
  paren breaks the continuation.

## Results and what it caught

Verification lives in
[sketch: inference witness oracle](../sketch/2026-11-03-inference-witness-oracle.md).
Headline: 1000-seed-per-mode sweeps run clean — 908/1000 (typed) and
840/1000 (typed-deterministic) fully accepted with zero witness
mismatches and zero guaranteed-with-diagnostics programs. Every
failure the oracle surfaced during bring-up was a **generator**
contract violation (the three classes above: `-(e)` gluing,
`...any` stamping, ph-meta leaks) — no inference bug has been
registered in `knownInferBugs` yet, which is itself evidence that
`check`'s inference is sound on generated coverage.
