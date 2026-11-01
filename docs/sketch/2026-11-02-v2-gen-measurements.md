# V-2 prototype validation: gomgen measured rates

Prototype for `docs/decisions/2026-11-02-grammar-based-generator.md`.
All numbers come from the committed tests; seeds are `rand.NewPCG`
streams so runs are reproducible.

*Updated after the review round (back-pipe side fix, ctor-argument
parenthesization, divergent-rule load validation, forced-descent
depth accounting, and the mechanical-mode name tracking below).*

## Parser acceptance (unperturbed generation)

`internal/gomgen/gomgen_test.go` generates 300 programs per mode and
counts diagnostics-free `syntax.Parse` results:

| mode | clean parses | rate | test floor |
| --- | --- | --- | --- |
| mechanical | 296/300 | 98.7% | 92% |
| typed | 297/300 | 99.0% | 95% |

Remaining failures are deliberate near-misses: `attribute` emits a
random `@name` ~8% of the time while the parser accepts only
`@test` (E0101), plus occasional `~`-in-pattern positions the parser
narrows beyond the grammar. No panics, no malformed or out-of-bounds
spans across 800 programs (300 + 500 panic-guarded).

Fixes and restrictions the generator now encodes — each is a
grammar-vs-semantics gap a hand-written emitter would ship wrong:

1. Random attributes — the grammar's `attribute` production allows
   arbitrary member names but the parser semantically accepts only
   `@test`. The generator emits `@test` 92% of the time.
2. String interpolation is suppressed inside import paths, test
   names, and extern package strings (`noInterp`), and nowhere else —
   block strings interpolate again.
3. `head-expr` may not begin with `{`, and control expressions
   cannot be operands of binary/pipe operators — `headDepth` +
   `operandOf` enforce both, and `pickAlt`'s depth-starved fallback
   obeys the same brace ban.
4. `fn-param` emits only a plain name after `~` (the parser rejects
   `~<complex-pattern>`), and `let`/`for` patterns plus unlabeled
   fn params emit only the irrefutable fragment (spec 1.1.5/4).
5. Depth is spent on choice points only: aliases and
   sole-mandatory-member sequences (the `expr → … → atom` ladder)
   cost 0. Before this, `need(ascribed-expr)=11` exceeded the
   default budget of 7 and function bodies could only produce
   control expressions — 0/300 programs contained a binary
   operator. Now 286/300 mechanical programs do.
6. Back-pipe emits ``(fn v -> body) <| arg`` — the
   function on the left, parenthesized because lambdas cannot be
   bare operator operands (the forward pipe keeps value-on-the-left).
7. Field-carrying constructor applications are parenthesized in
   argument position — ``C a b`` there reads as extra args of the
   outer call.
8. Grammar loading rejects every REACHABLE rule that cannot expand
   finitely (``a = a , "x" ;`` and forced-descent cycles like
   ``a = b ; b = a ;``) instead of letting Generate stack-overflow;
   the fixpoint fallback marks only non-converged rules
   ``depthInfinite`` — see ``ebnf_internal_test.go``.
9. ``select {}`` with zero arms is biased to at least one arm
   (empty select is E0705), and ``_`` in expression position is
   down-weighted (spec 8.2 admits it only as a Go-call argument).

Mechanical-mode name tracking records which names declaration sites
emitted (``type-decl`` heads, variant ctors, fn names, import
aliases, pattern bindings) and prefers them at the matching use
sites: ctor names inside ``ctor-name``/``ctor-pattern``, type names
inside type rules, bound names at identifier positions, imported
modules as qualifiers. Use sites still fall back to the pools, so
unbound-name diagnostics remain — they are now the exception rather
than the default.

## Compile canary yield

``internal/build/gen_test.go`` compiles 80 programs per mode inside
the same ``fuzzutil.Sandbox`` subprocess as ``FuzzCompile`` (real
``goimport`` oracles when ``go`` is on PATH; parse 1.5 s, compile
8 s budgets; ``LooksHangSuspect`` inputs get a 0.5 s parse cap).
Failures matching the ``knownCompileBugs`` registry are logged, not
counted:

| mode | fully clean | rejected with user diagnostics | known bugs | contract violations |
| --- | --- | --- | --- | --- |
| mechanical | 31/80 | 48 | 1 | 0 |
| typed | 43/80 | 37 | 0 | 0 |

The generated-Go typecheck runs under the same ``opts`` the sandbox
uses — an earlier version measured the rejection count with nil
options, which made every ``import go`` fail with E0203 and
understated reach by roughly 20 points.

A 200-seed probe (same streams, driver only — no generated-Go
typecheck) shows where programs stop:

| mode | no diags | resolved | typed | codegen |
| --- | --- | --- | --- | --- |
| mechanical | 72 | 194 | 92 | 72 |
| typed | 117 | 196 | 172 | 117 |

Diagnostic counts over those 200 programs:

| code | mechanical | typed |
| --- | --- | --- |
| E0101 | 6 | 4 |
| E0201 | 288 | — |
| E0202 | 20 | 12 |
| E0203 | 38 | 11 |
| E0401 | 57 | 13 |
| E0402 | 2 | 2 |
| E0403 | — | 8 |
| E0406 | — | 4 |
| E0407 | 8 | 242 |
| E0410 | 2 | — |
| E0411 | — | 2 |
| E0504 | 59 | — |
| E0705 | 26 | — |
| E0802 | 2 | 12 |

Mechanical programs now survive wider shapes (binary operators,
pipes, blocks), so the funnel moved downstream: ~half still die at
resolve on unbound names (E0201 — value refs that never met a
binding), and the placeholder ``_`` / empty-``select`` / import
classes are the deliberate near-miss remainder. Typed mode keeps its
shape: most rejections are E0407 (type annotation required —
arithmetic over metavar-typed operands).

## Two real bugs surfaced

- ``E0901 "builtin assert may only be called inside a test block"``
  surfacing from lowering for a user-level mistake — generated code
  calling ``assert`` in a plain block. Tracked by
  ``docs/problems/2026-11-02-assert-outside-test.md``; the generator
  emits assert forms only inside ``test`` blocks.
- ``type Point = {x: Point}`` — a record containing itself by value
  passes check and lowers to ``type Point struct { X Point }``,
  which Go rejects ("invalid recursive type"). Variants are safe
  (they lower through an interface). Registered in
  ``knownCompileBugs`` + TODO.md.

## Reproducing

    go test ./internal/gomgen/ -run TestGeneratedParses -v   # parse rates
    go test ./internal/build/ -run TestGeneratedCompileCanary -v  # canary yield

## Deferrals

- Fuzz-corpus integration and the eval-vs-compiled differential
  consumer (needs an eval oracle to diff against).
- ``Options.Perturb`` is wired through the walker but no consumer
  sweeps perturbed streams yet.
- Generics/params annotation (``(x: T)`` lambda params are
  PROVISIONAL in the grammar) — typed mode does not emit them.
- Type variables (``a``, ``List a``): annotations are rendered from
  ``gty.text()`` and the palette stays monomorphic for now — no
  type-param declarations are generated.
- Mechanical scope tracking stops at "prefer a recorded name": it
  does not model lexical extent, so old bindings still leak into
  references (residual E0201s) and top-level ``let`` names can
  collide.