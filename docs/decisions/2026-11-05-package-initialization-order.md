# Package initialization order: Go's dependency analysis over the whole unit

- 状態: 採用（prototype で検証済み — [sketch](../sketch/2026-11-05-package-initialization-order-verification.md)。spec.md §5.1 へ反映済み）
- 日付: 2026-11-05

## Context

V-6 made a package a multi-file unit: sibling `.gom` files share one
top-level scope, and their top-level `let`s emit package-scope `var`
declarations into one generated Go package. The package-unit decision
already states the intent in one line — "Initialization order: Go's own
package-level dependency order — a cross-file `let` reference
initializes like a same-file one"
([2026-11-05-package-compilation-unit](2026-11-05-package-compilation-unit.md))
— but docs/spec.md has no section defining what that order *is*:

- E0907 ("package-level initialization cycle") is defined without
  any `Spec:` reference — the diagnostic is ungrounded by omission
  (docs/implementation-gaps.md).
- `checkInitCycles` built its dependency graph over one lowered file,
  so `a.gom: let x = y + 1` / `b.gom: let y = x + 1` produced `var x =
  y + 1` / `var y = x + 1` and left `go build` to report the cycle —
  verified in
  [docs/sketch/2026-11-05-v6-residual-verification.md](../sketch/2026-11-05-v6-residual-verification.md).

What the rule must be is not really open: generated Go must compile,
so the language rule has to be a *lower bound* of Go's package
initialization — any ordering Go accepts must be the ordering Gom
specifies, and any program Go rejects must be a Gom diagnostic. The
question this file answers is how to state it (and how far to go in
matching Go exactly).

## Decision

Adopt Go's package-initialization rule verbatim, as a property of the
package unit rather than of any one file.

1. **What counts.** A top-level `let` binding is a package-level
   variable; `fn` declarations — and `let` bindings the
   aggregate-binding rule emits as named wrapper functions
   ([2026-11-05-aggregate-function-bindings](2026-11-05-aggregate-function-bindings.md)),
   including generalized `let`s that emit thunks — are functions.
   Hidden temporaries lowering mints for destructuring / tested
   patterns and for eager alias initializers are package variables
   like any other.

2. **Dependency.** Variable x depends on variable y iff x's
   initialization expression refers to y, or refers to a function
   whose body (transitively, through function bodies and function
   literals) refers to y. A function-to-function reference is not a
   dependency. References to *imported* package variables carry no
   dependency — an imported package is always initialized first.

3. **Order.** Initialization proceeds in dependency order: a variable
   is ready once nothing it depends on is uninitialized. Ready
   variables are picked in declaration order — source order within a
   file, lexical file-name order across files (the order the
   generated `.go` files are presented to `go build`, which is also
   the order `gom`'s directory expansion presents the sources).

4. **Residual cycles.** A variable-to-variable cycle that remains is
   rejected as E0907 — one diagnostic per variable on the cycle, at
   the binding's own span, naming `.gom` bindings (`let (a, _)`)
   rather than generated `tmpN`s.

### Spec wording (inserted as docs/spec.md §5.1)

```markdown
### 5.1 パッケージの初期化順

トップレベルの `let` 束縛はパッケージ変数となり、そのパッケージが
使われる前に 1 度だけ評価される。順序は Go のパッケージ初期化と同じ
依存解析で決まる。

- 変数 x が変数 y に依存するのは、x の初期化式が y を参照するとき、
  または初期化式が参照する関数（`fn` 宣言・関数リテラル）の本体が
  推移的に y を参照するとき。関数から関数への参照だけでは依存を
  生じない。
- 初期化は依存の残っていない変数から進む。初期化できる変数が複数
  あるときは宣言順に選ぶ — 宣言順はファイル内では出現順、ファイルを
  またぐときはファイル名の辞書順である。import したパッケージは
  それを参照する側の初期化より先に初期化される。
- 依存関係が循環して初期化順を定められない変数が残ったら、その
  プログラムは不適格で、変数ごとに E0907 として報告される。
```

(English gloss: top-level `let`s are package variables initialized once
before the package is used, in Go's dependency order — a variable depends
on variables its initializer references, directly or transitively
through referenced function bodies; ready variables initialize in
declaration order, which across files is lexical file-name order;
imported packages initialize first; a residual variable cycle is an
E0907 error per variable.)

## Consequences

- `checkInitCycles` becomes a package-unit pass:
  `lower.CheckInitCycles([]*core.File)` builds the refset/vardep
  graphs over every member's `Decls` and runs after all files of the
  unit lower (`build.CompileFiles`, and `build.Compile` for the
  one-file unit). `lower.File` no longer runs it — the per-file call
  could not see sibling declarations.
- Hidden temporary VarDecls carry `core.VarDecl.DiagName` (the
  `let <pat>` spelling the tmpLets map used to supply) so the unit
  pass names `.gom` bindings without the lowering context.
- `@tailrec` requirement R4 has the same file-local limitation: its
  direct-reference graph sees one file, so mutual recursion routed
  through a *sibling* function or let escapes E0904 the same way
  sibling init cycles escaped E0907. Not fixed here — proposed for
  the gaps ledger at merge.
- The evaluator is single-file today; a multi-file `eval` entry
  inherits the same rule whenever it exists.
- testdata/spec fixtures are one file per case, so the cross-file
  rule is covered by `internal/build` driver tests, not spec
  fixtures.

## Alternatives considered

- **Forbid cross-file initialization dependencies.** Rejects programs
  the generated Go would run correctly, contradicts the shared-scope
  reading already adopted for the package unit, and buys nothing —
  the analysis is already written.
- **Declaration order only (no dependency analysis).** Diverges from
  the generated Go program: a cross-file `let` could observe a zero
  value `var` where `go build` would have ordered the dependency
  first — an eval/compiled divergence of exactly the kind the
  semantics-comparison suite exists to catch.
- **Keep the per-file check (status quo).** Files whose cycles route
  through siblings emit `var` declarations `go build` then rejects —
  the compiler's own output fails to compile, which the E0907
  machinery exists to prevent.

## Prototype / verification

Implemented on branch `wt/initorder-small` and verified in
[docs/sketch/2026-11-05-package-initialization-order-verification.md](../sketch/2026-11-05-package-initialization-order-verification.md):
the sibling-cycle repro now reports E0907 at `gom build` (and nothing
reaches `go build`), a benign cross-file forward reference initializes
in dependency order, and the existing single-file E0907 suite
(spec fixtures + lower tests) is unchanged.
