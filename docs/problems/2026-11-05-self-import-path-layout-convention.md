# SelfImportPath keys off the reserved `stdlib/<pkg>/` directory layout

- 状態: 解決（2026-11-06 — [decision](../decisions/2026-11-06-self-import-path-module-derived.md): SelfImportPath は入力ディレクトリの module-relative パス。lexical な `stdlib/<pkg>/` スキャンは廃止）

## 状況

`gomstd.SelfPath` (internal/stdlib/stdlib.go) answers
`build.Options.SelfImportPath` by scanning the input file's path for
a `stdlib/<pkg>/` segment pair where `<pkg>` is `list`, `option`, or
`result`. A source under that layout compiles "into" the toolchain
package: `core.File.SelfImportPath` is set and codegen emits
references into that import path *unqualified* — a package never
imports itself, so `list.Map` inside `stdlib/list/` is a bare `Map`.

The scan is purely lexical — any `stdlib/<pkg>/` pair anywhere in
the path triggers it, including inside a user module. A user
project that keeps its own `stdlib/list/` directory (a monorepo
vendoring layout, a mirror checkout, a coincidence) gets the same
convention: generated code emits bare references where the real
intended package is the user's own, and the toolchain `list`
import is dropped from the generated file's import block. The
resulting Go either fails to compile (undefined `Map`) or, worse,
resolves a bare name to an unrelated declaration in the user's
package.

It is currently unreachable by accident, not by construction: the
toolchain's own stdlib sources live in the same repository layout,
and no user module in the test corpus uses `stdlib/<pkg>/` below
its root.

## なぜ問題か

- The convention is a *silent* miscompile path: the .gom compiles,
  the diagnostic stream is clean, and the generated Go is wrong.
  The usual "Gom diagnostics, not Go failures" boundary is crossed.
- The trigger condition is not visible to the user — there is no
  package-clause or module marker declaring "this is the stdlib";
  path shape alone decides.
- The .gomi member-surface work (V-6) makes more user modules
  importable, which widens the set of layouts the CLI actually
  meets.

## 考えられる方向

- Gate the convention on the module: `SelfPath` could require the
  input's enclosing module to be the toolchain module itself
  (`go.mod` module path `github.com/podhmo/gom`), not merely the
  directory names. `Loader.module()` already knows how to find the
  nearest go.mod.
- Alternatively resolve self-path by comparing the package's
  *computed* import path (module + relative dir) against the
  toolchain path instead of pattern-matching segments.
- Until then, a diagnostic ("source under reserved stdlib layout
  outside the toolchain module") would turn the silent case into a
  bounded one.

## 参照

- [implementation-gaps.md](../implementation-gaps.md) —
  `SelfImportPath` ledger entry
- [stdlib packaging decision](../decisions/2026-11-03-stdlib-packaging.md)
- internal/stdlib/stdlib.go `SelfPath`; internal/codegen/gen.go
  `selfPath`; cmd/gom/cli.go `build.Options.SelfImportPath`
