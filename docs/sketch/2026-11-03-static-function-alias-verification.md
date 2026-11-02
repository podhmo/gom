# Static function alias verification (P2-2b)

- 日付: 2026-11-03
- Go: `go1.27.3 darwin/arm64`
- Base: `d4b278f788ec9dc8f1f8134be8b90b964751f20a`

## Prototype and decision

The [decision](../decisions/2026-11-03-static-function-alias-declarations.md)
was first committed as draft (`d18b5cd`), then implemented as prototype
(`5d35d50`). The prototype promotes only pure references rooted in local,
non-thunk function declarations, following alias chains regardless of source
order. Type ascriptions are transparent; explicit Go conversions are not.

Core `FuncDecl.ValueAlias` retains a KindVar symbol and an arity-one function.
Its callers still use `Var` and canonical `CallFunc`. Codegen uses its
existing function-declaration emitter; it does not infer alias purity or
calling conventions. The Core validator rejects wrong symbol kinds, entry
flags, type parameters, and zero/multiple parameters on this representation.
The Core dump spells these declarations `value-alias`.

## Executed evidence

- `TestEvalMatchesCompiled/static_function_alias`: direct recursion through
  an alias, an alias chain, higher-order application, a two-argument recursive
  declaration used through an alias, and partial application all produce
  `3:4:5:42` in both the typed evaluator and generated Go.
- `TestStaticAliasEagerInitialization`: an alias of a function returning a
  function prints `factory 40` once during package initialization; repeated
  calls of the stored partial application do not repeat it. Applying the
  alias to `1` runs the first declaration chunk even when the returned
  closure is unused. Exact output:

  ```text
  factory 40
  main
  42
  43
  factory 1
  after
  ```

- `TestStaticAliasRepresentation`: reverse-ordered aliases are wrappers
  without mutation of checked variable symbols.
- `TestComputedFunctionValueStaysEager`: a partial-application initializer
  remains a `VarDecl`.
- Existing `TestTailRecPkgVarAlias`, `TestTailRecPkgVarChain` and E0904
  fixtures still reject mutual recursion through aliases under `@tailrec`.
- Golden output was regenerated and inspected: `static_function_alias` has
  named canonical wrappers, while `add40` retains its eager initializer.
  `poly_functions` changed from a stored static adapter to a named wrapper.

`gofmt -l .` produced no output, `go vet ./...` and the complete
`go test ./...` passed. `git diff --check` also passed.
The first full test run passed compiler/runtime/tutorial
packages and exposed only the new decision's unsynchronized generated index;
the index was regenerated before the successful final full run.

## Independently reproduced remaining bug

This prototype deliberately does not promote function values projected from
aggregate initializers. The following accepted program still produces a Go
initialization cycle:

```gom
package main
import go "fmt"
fn count (n: Int) -> Int = if n == 0 { 0 } else { again (n - 1) + 1 }
let (again, _) = (count, 0)
fn main () -> Unit = { ignore (fmt.Println (again 3)); () }
```

`gom build` exits successfully; `go run .` reports hidden tuple → count →
again → hidden tuple. This is tracked separately as **P2-2c**, leaving the
P2-2 parent partial. Aggregate initialization may include other effects, so
replacing projections with late reads or moving all values to `init()`
without dependency analysis is not a safe extension of this fix.

## Fresh review and implementation response

The independent `review_alias_initialization` review found no actionable
defects. It confirmed the pure-reference restriction, eager computed
initialization, KindVar canonical arity-one wrappers, validator checks and
reuse of existing codegen. Additional probes covered reverse-ordered forward
alias chains, generic functions, lambdas and Unit aliases. Its formatting,
vet, full test suite and diff checks passed.

The implementer judged that no additional code changes were needed: the
review independently exercised the main representation and timing risks,
and the reproduced aggregate-cycle gap remains explicitly tracked as P2-2c.
This response changes documentation only; `git diff --check` was rerun.
