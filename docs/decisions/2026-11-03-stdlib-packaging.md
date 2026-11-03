# stdlib packaging: .gomi-driven exports, builtin module tables, leaf layering

- 状態: 採用
- 日付: 2026-11-03

## Context

P4-8 asks for `stdlib/list`, `stdlib/option`, `stdlib/result`
written in Gom, each with a `.gomi`, built by the compiler itself —
the first real consumer of `.gomi` conformance and generic Go
emission. Three representation facts shaped the implementation:

1. The builtin types (`List a`, `Option a`, `Result a e`) are
   compiler-known: codegen hardcodes their Go paths
   (`stdlib/{list,option,result}`) for every file it emits —
   including the stdlib sources' own outputs.
2. A file compiled into `package list` cannot import
   `github.com/podhmo/gom/stdlib/list` — a package never imports
   itself.
3. `rt` carried the boundary helpers (`ResultFromGo`,
   `OptionFromGo`, `AssertOk`), which made `rt` import
   `stdlib/{option,result}` — a cycle for any generated stdlib
   code that also needs `rt` (`InvalidVariant` for match defaults,
   `Todo` for `todo`, `Assert`/`AssertEq` for tests).

## Decision

**The `.gomi` is the single source of truth for the public
surface.** `iface.ExportNames(dir, pkg)` reads
`<dir>/<pkg>.gomi` and lowers its member names through
`build.Options.Exports` → `lower.Env.Exported` →
`core.Decl.Exported`, so interface-listed declarations emit
exported Go identifiers (`fn get_or_else` → `GetOrElse`) while
implementation details stay unexported (spec section 5). The same
`.gomi` sources are compiled once by `internal/stdlib` into the
resolver's builtin-module member tables (`Config.Modules`), the
checker's signatures and lowering's `FuncArity` — user code sees
`List.map` with no import clause, and `List.map f` partially
applies at the declared arity.

**Packages do not import themselves.** `build.Options.SelfImportPath`
carries the file's own Go import path to `core.File`; codegen
renders references into it unqualified (`list.List` → `List`,
`option.Option_Some` → `Option_Some`, member refs `list.Map` →
`Map`). `internal/stdlib.SelfPath` answers it for sources under
`stdlib/{list,option,result}/` — a reserved directory convention.

**`rt` is a leaf.** The boundary helpers moved into the packages
they construct: `result.FromGo`, `result.FromErr`, `option.FromGo`,
`result.AssertOk` — discovered next to `Ok`/`Err`, and emitted
through the existing `resultPath`/`optionPath` qualifiers. `rt`
keeps only representation-free helpers (`InvalidVariant`, `Todo`,
`Equal`, `Assert`, `AssertEq`, `ShowError`, `Task`, `TupleN`), so
generated stdlib code may import it freely. `rt`'s tests moved to
`package rt_test` because `stdlib/list` now imports `rt`.

**Stdlib layering is a DAG, not a cycle.** `result` imports only
`rt`; `option` adds `result` (`to_result`); `list` adds `option`
(`get`, `head`, … returning `Option`). A conversion that would
close a cycle simply does not exist: there is no `Result.to_option`
(it would import `option` while `option.to_result` imports
`result`) and no `Option.to_list` (`List.get` already imports
`option`). The `.gom` and `.gomi` files record this at the point a
reader would look for the function.

Generated `*.go` and `*_test.go` are committed beside the `.gom`
sources: they are checked-in build artifacts so `go build` of the
repo does not bootstrap.

## Consequences

- `gom check`/`build`/`test` on the stdlib directories exercises
  the real driver: parse → `.gomi` conformance → generic emission
  → `go test`.
- The spec preamble (`shapes |> List.map area |> List.sum`)
  compiles and runs; pinned by
  `testdata/spec/ok_stdlib_preamble.gom` and
  `testdata/golden/stdlib_modules.gom`.
- Every compile entry point (`cmd/gom`, the spec/golden suites,
  tutorial, gist-cli, skill snippets, the fuzz harness) wires the
  stdlib through `gomstd.Apply`.
- Emitted boundary calls changed name — `rt.ResultFromGo` →
  `result.FromGo` — regenerating the codegen and golden fixtures.
- Verified in
  [sketch: stdlib built by the compiler](../sketch/2026-11-03-stdlib-gom-built.md).
