# Multi-file evaluation: the evaluator takes the package unit

- 状態: 採用（prototype で検証済み — [sketch](../sketch/2026-11-06-eval-package-unit-verification.md)）
- 日付: 2026-11-06

## Context

`eval.New` takes a single `*check.File`, so the evaluator cannot see
sibling package `let`s. Since package-unit compilation (V-6,
[2026-11-05-package-compilation-unit](2026-11-05-package-compilation-unit.md))
a directory's `.gom` files share one top-level scope, and the
eval-vs-compiled comparison harnesses cannot verify a program whose
behavior depends on declarations living in a sibling file — the gap is
recorded in docs/open-questions.md.

The init-order decision already anticipated the answer:
"a multi-file `eval` entry inherits the same rule whenever it exists"
([2026-11-05-package-initialization-order](2026-11-05-package-initialization-order.md)).
What remains is the API shape and how much of Go's package-initialization
semantics the evaluator reproduces.

## Decision

1. **API.** `eval.NewFiles(files []*check.File) *Evaluator` evaluates
   the typed files of one package unit — what `check.Files` returns and
   `build.CompileFiles` hands out through `Result.Typed`. `eval.New(f)`
   stays as the single-file convenience wrapper
   (`NewFiles([]*check.File{f})`), so every existing caller shares the
   multi-file implementation. `Program` gets the `ProgramFiles` twin.

2. **File order.** `NewFiles` sorts its input by the lexical file name
   — `filepath.Base(File.Sp.File)`, the name the generated `.go` file
   is written under — the cross-file declaration order spec 5.1
   defines, which is the order `go build` presents the generated
   package's files and the order `gom`'s directory expansion presents
   the sources. Caller order does not matter; a same-package unit
   shares one directory, so path order is file-name order and the
   base name is the whole key. Within a file, declarations stay in
   source order.

3. **One package scope.** All files' `TypeDecl`s register into the one
   constructor/record metadata set, and `interp.localT` unions across
   the unit: a package-local `type Result` declared in `a.gom` shadows
   the builtin for `b.gom`'s unqualified Cons too, matching check's
   shared scope and codegen's `Unit`-wide `localT`. All `FnDecl`s bind
   their closures into the shared global environment at construction;
   all top-level `LetDecl`s register lazy initializers into the shared
   `ctx.topLets`.

4. **Forward references: no new semantics.** The single-file evaluator
   already initializes a top-level `let` on first read rather than in
   source order (`evalVar` forces the registered initializer,
   transitively through called function bodies — Go's dependency rule).
   That mechanism is file-blind: a `Var` read forces the initializer
   whichever file declared it, so a sibling `let` behaves exactly like a
   same-file later `let`. `Run` keeps driving the unit's `LetDecl`s and
   `TestDecl`s in declaration order (file order × source order) before
   applying `main`, matching what the compiled package does.

5. **Harness migration.** The eval-vs-compiled helpers construct
   `eval.NewFiles` from the results' typed files; `bindGoStubs` walks
   every file's declarations; `Evaluator.SetLineIndexFor` registers a
   per-file line index so `|?>` step labels name each step's own
   file:line. The single-file callers (`New`, `SetLineIndex`) are
   untouched.

6. **Known limitation (pre-existing, now documented).** Lazy forcing
   reproduces Go's *dependency* ordering — a variable's initializer
   never runs before what it reads — but the tie-break among
   independent variables is the forcing context, not Go's
   earliest-declared-ready pick. `let x = z + 1; let y = f (); let z =
   40` (side-effecting `f`) initializes z-x-y under the evaluator where
   Go picks y first (y and z are both ready; y precedes z in
   declaration order). No Gom-visible value can observe the order —
   only host-call side effects inside top-level initializers can, and
   only when an earlier-declared variable depends on a later-declared
   one. That qualification holds because today's stub table has no
   state-reading host call: a stub whose *return* depends on call
   order would make a bound value itself differ, not merely the
   effect order. TestDecls share the gap — eval runs a `test` block
   at its declaration position in the unit order, while the compiled
   `go test` binary runs it after all package initialization, so a
   test observing init side effects can see a different order.
   Matching Go exactly needs the refset/vardep graph
   (`lower.CheckInitCycles`) recomputed over `check.Decl`s — deferred
   until a differential-suite finding needs it.

## Consequences

- `Evaluator` keeps `files []*check.File` (sorted) instead of one file;
  `Run` iterates every file's decls. `New(nil)` still yields an empty
  evaluator.
- The comparison harnesses can now cover sibling-`let` programs; a
  two-file eval-vs-compiled case pins the cross-file init order.
- The open-questions entry about multi-file eval is answered: eval
  accepts the unit's typed files. The residual item is the
  effect-order tie-break above.

## Alternatives considered

- **A unit object instead of `[]*check.File`.** There is no unit type
  in `check` — `check.Files` returns a slice and `build.CompileFiles`
  returns one `Result` per input. A wrapper type would add a step
  without information.
- **`NewFiles` takes files in caller-given order.** The language fixes
  cross-file declaration order (lexical file names); a caller-ordered
  API would let every caller express an order the compiled program
  cannot have. Sorting inside the evaluator keeps the ordering rule in
  one place.
- **Full dependency-graph initialization in eval.** Ports lower's
  refset/vardep machinery to `check.Decl`s to reproduce Go's
  ready-in-declaration-order pick — only observable through side-effect
  order in initializers. Bigger than the harness needs today; the
  deferred item stands.

## Prototype / verification

Verified:
[docs/sketch/2026-11-06-eval-package-unit-verification.md](../sketch/2026-11-06-eval-package-unit-verification.md).
`TestProgramFiles` (internal/eval) pins the shared scope, lexical
file order, cross-file forcing, the cross-file cycle diagnostic,
unit-wide `localT` shadowing, and per-file `|?>` step labels;
`TestEvalMatchesCompiledFiles` (internal/build) runs two two-file
units through `build.CompileFiles` and confirms evaluator output
equals the compiled binary's (`41`, `41:s42`).
