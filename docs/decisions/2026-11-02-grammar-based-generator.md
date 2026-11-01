# Grammar-based random program generator (internal/gomgen)

- 状態: 採用
- 日付: 2026-11-02
- 検証: [v2-gen-measurements](../sketch/2026-11-02-v2-gen-measurements.md)

## Background

V-2 wants `.gom` programs generated grammar-aware so parser fuzzing,
the compile canary, and later the eval-vs-compiled differential get
seeds that reach deeper syntax and pipeline paths than corpus
mutation does. Two design questions: where the grammar comes from,
and how the generator is shaped.

## Decisions

- **The grammar source of truth is `docs/grammar.ebnf`, read by a
  small EBNF interpreter in `internal/gomgen`** — not tree-sitter,
  not a second hand-written grammar. The EBNF file is already the
  authoritative reference for the parser, so generating from it
  keeps grammar drift between "what we generate" and "what we parse"
  structurally impossible: a grammar edit changes the generator for
  free. Tree-sitter would buy incremental parsing we do not need and
  adds a grammar fork to maintain. Loading validates that every
  referenced rule is defined or provided by a built-in token
  producer, so an unsupported grammar edit fails at load, not
  silently mid-generation.
- **Two generation modes sharing one `Gen`.**
  - *Mechanical*: walks grammar alternatives with weighted picks,
    depth/token budgets, and a perturbation knob that occasionally
    drops or swaps a terminal for near-miss inputs.
  - *Typed-ish*: emits declarations and expressions driven by a
    small type palette plus scope tracking — declared ADTs, records,
    constructor arities, and in-scope bindings steer expression
    choice. It is deliberately shallow (no unification, no variance,
    no generics) — the goal is raising the share of programs that
    survive `check`, not proving generated programs correct.
- **Grammar quirks live in the generator, not the grammar.** The
  head-expr brace ban (a head may not begin with `{`), no
  interpolation inside import/test/extern strings, and the
  control-expr-as-operand parenthesization rule are tracked as
  generator state (`headDepth`, `noInterp`, operand wrappers).
  `docs/grammar.ebnf` is not edited to fit the implementation.
- **`assert`/`assert_eq` are emitted only inside `test` blocks.**
  Lowering reports them as E0901 outside tests — an open semantic
  question (docs/problems/2026-11-02-assert-outside-test.md), not a
  user diagnostic, so generation avoids the shape rather than
  letting the canary spam a known-questionable code.
- **Determinism contract**: `Generate(g, rng, opts)` is a pure
  function of the RNG stream — same seed, byte-identical output.
  Tests pin both determinism and seed-to-seed variation.
- **One consumer wired now**: `internal/build/gen_test.go` runs a
  deterministic seed sweep through `build.Compile` under the same
  `checkCompile` contract FuzzCompile uses. Corpus integration
  (fuzz seeds) and the eval-vs-compiled differential stay future
  work — the differential needs eval'd outputs as oracle inputs
  first.

## Consequences

- The generator may emit semantically invalid programs; user
  diagnostics are canary inputs, not failures. Contract violations
  (panic, internal error, E0901, malformed or out-of-bounds
  diagnostics, generated Go that fails to typecheck) remain test
  failures.
- `select`/`spawn`/`go`/`for` shapes are generated (grammar-covered
  and typed-mode-emitted) but typed mode makes no claims about
  channel/Task typing — best effort only.
- Perturbed inputs deliberately break the parse rate — the measured
  rates in the sketch report are for unperturbed streams.
