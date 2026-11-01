# P1-8: codegen implementation and verification report

- 目的: implement `internal/codegen` — the mechanical translator
  from validated `internal/core` IR to Go source (TODO.md P1-8) —
  and verify the output builds, vets, and runs.
- 関連: [decisions/generated-go-conventions](../decisions/2026-11-02-generated-go-conventions.md),
  [decisions/generated-support-packages](../decisions/2026-11-02-generated-support-packages.md),
  [design.md](../design.md) section 4, P0 representation decisions
  (P0-1 ADT, P0-2 functions, P0-3 List, P0-4 Result).
- 実装: `internal/codegen/{codegen,gen,names,types,expr,emit,walk,decl}.go`,
  support `rt/{rt,tuple,task}.go`,
  `stdlib/{result,option,list}/`; date 2026-11-02, go1.27.1
  linux/amd64.

## Architecture recap

`File(f *core.File, src []byte) (*Output, error)` validates the IR
(`InvalidFileError` on violation), then runs one `gen` per output
file — main source plus, when `TestDecl`s exist, the `_test.go`
half. A `gen` holds the line index, output buffer, import/alias
tables, symbol→identifier map, used-name set, and declaration
indexes (local variants, local type names, file imports).

Emission is destination-passing (design 4.3): `emit(e, dest)` where
`dest` is Return / Assign(name) / Discard. Complex expressions emit
statements whose branch bodies inherit the destination — no
temporaries, no IIFEs (the `var x T = func() T { ... }()` of a
package-level `VarDecl` is the single sanctioned exception).

| file | role |
| --- | --- |
| `codegen.go` | `File`/`Output`/`render`, `//gomsrc:` → `//line` splice, `InvalidFileError` |
| `gen.go` | generator state, name/import resolution, output primitives (`raw`/`nl`/`open`/`close`/`caseLabel`), builtin con table |
| `names.go` | identifier mapping: camelCase, export, `safeIdent`, type-param and test names |
| `types.go` | `types.Type` → `go/ast` type expr (Unit→`struct{}`, List/Option/Result→stdlib, Tuple→`rt.TupleN`, channels, pointers, flat `func`) |
| `expr.go` | atoms and single-expression nodes → `go/ast` |
| `emit.go` | statement-level nodes under destinations: if/match/select/spawn/for/escapes/record-update/tail-call, `body`/`let`, unused-let elision |
| `walk.go` | binder collection (`coreWalkBinders`) + symbol-use analysis (`bindUsed`, `bodyUsesFrom`) |
| `decl.go` | top-level: FuncDecl/VarDecl/TestDecl, TSum/TRecord/TAlias/TAbstract |

## What the fixture covers

`internal/codegen/fixture_test.go` builds one validator-clean
`core.File` exercising every node family: TSum (mono `Shape` +
generic `Box[A]`), TRecord, TAlias, TAbstract, VarDecl, FuncDecl
(mono, generic `ident[A any]`, arity-split `mk_add`/`adders`),
closure values and partial application, `SwitchType`/`SwitchTag`/
`SwitchValue`/`SwitchList` + `DGuard`/`DLeaf`/`DPanic`, tail-loop
`countdown`, `ResultFromGo`/`EscapeResult`/`Conv`/`OptionFromGo`/
`TupleFromGo`/`EscapeCall`, `select` (recv/send/default), `spawn`,
`for`-range over list and chan, tuple/list/record construction +
record update, `todo`/`assert` builtins, CtorRef/FuncRef/GoFuncRef
values, and a `Result Unit e` test (`rt.AssertOk`).

Golden output: `internal/codegen/testdata/fixture.golden.go` +
`fixture_test.golden.go`, refreshed with `go test ./internal/codegen
-update`.

## Compile smoke (the load-bearing check)

`internal/codegen/compile_test.go` writes the generated files into a
temporary module `genout`, plus a stub `gostub` module standing in
for the fixture's Go callees (signatures matched to the Gom view:
`Int` is `int64`), with `replace github.com/podhmo/gom => <repo>`.
It then runs the real toolchain: `go build ./...`, `go vet ./...`,
`go test ./...` — the generated `_test.go` executes `TestSmoke` and
`TestResult` for real (the `rt.AssertOk` path).

Every bug the smoke caught was real:

1. `result.ResultTag_Ok` vs emitted `ResultTagOk` — constants renamed
   (stdlib decision doc).
2. Fixture `unwrap` bound the *payload* where SwitchType binds the
   *narrowed subject* — fixed to `v.V` field read.
3. Unused lets produced `declared and not used` — added
   symbol-use-based elision of named lets, and the test-decl path
   had to go through `body()` not a raw `g.let` loop.
4. `_ =` vs bare statement: a discarded `Conv` (`int64(n)`) is a
   *conversion* in Go, never a usable statement — now always
   `_ = T(x)`.
5. `adders` had a flat Sig but a curried body — Sigs are canonical
   flat arrows (`Func.ResidualType`), so the closure takes both
   params.
6. `escapeErr` fired `t.Errorf` for any escape when `to == curFn`
   inside a test — a closure nested in a test would have taken the
   test path; the test boundary is now tracked explicitly
   (`gen.testFn`).

## Current state

- `gofmt -l .` — clean; `go vet ./...` — clean; `go test -race
  ./...` — all packages pass; `make check` (incl.
  `check-sumtype` and the experiments modules) — green.
- Generated output is readable Go: flat signatures
  (`func add2(a, b int64) int64`), grouped same-type params, named
  struct fields, `n, err := f()` fused escapes, `for`-loop tail
  recursion with `n = m; continue`, and `//line t.gom:N` mappings on
  statements and declarations.

## Known output quirks (non-blocking)

- Unmapped generated constructs (marker methods, the TailLoop `for`)
  inherit the previous `//line` position — cosmetic.
- Fresh-name numbering is file-wide, so later functions get `v2`,
  `err1` — prevents collisions, mildly noisy.
- `switch` value/list arms emit `_ = lit` bodies when the
  destination is Discard — correct but chatty.
- `bindUsed`/`bodyUsesFrom` re-walk bodies per binder — O(binds ×
  body size); fine at current scale.

## Divergences / open items handed to later tasks

- Unused-`let` elision is required because Core IR permits binds the
  (future) checker may reject — filed as
  [problems/unused-let-bindings](../problems/2026-11-02-unused-let-bindings.md).
- `testdata/golden/` (the gomtest suite) still drives `.gom` source
  through `GenerateFunc` — end-to-end goldens arrive with P1-11's
  build wiring; codegen goldens live package-locally meanwhile.
- `B-6` (S-expression dumps) would make fixture construction less
  verbose than hand-built `core.File` literals.
