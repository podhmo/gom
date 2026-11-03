# Gom package import syntax and .gomi contents are unspecified

- 状態: 一部解消（2026-11-05 — `import "path"` は実装済み: 生成 Go パッケージの import path で Gom パッケージを読み、`.gomi` が公開面になる。名前参照はパッケージ句の名前（通常はパス末尾）。残る未決は `.gomi` 側の import・alias 形式の仕様化。参照: [decision](../decisions/2026-11-05-package-compilation-unit.md)、[検証](../sketch/2026-11-05-multifile-package-verification.md)）
- 日付: 2026-11-01

## Situation

spec.md only ever shows `import go "fmt"`. The syntax for importing a
Gom package is never shown:

- is it `import "example.com/mod/pkg"`, `import "pkg"`, or path-less
  module names?
- can imports be aliased or renamed (`import foo "path"`)?
- how are names referenced — last path segment, package clause?

Also, spec §7.1 says a `.gomi` may contain only "型定義・関数
シグネチャ・定数の型と `///` のドキュメントコメント" — no imports.
Yet a signature such as `fn read (r: io.Reader) -> ...` presumably needs
`io` in scope, so .gomi probably needs imports too. Whether ordinary
`//` comments are allowed is also unstated.

## Why it is a problem

`docs/grammar.ebnf` must define the grammar for both file kinds; it
currently guesses `import [ "go" ] [ ident ] STRING` and allows imports
in .gomi, both marked [PROVISIONAL gom-import-syntax].

## Related

- spec.md §5, §7 — grammar.ebnf `import-decl`, `iface-decl`
- TODO D-1, P1-4 (name resolution will need this decided)

## Possible responses

- `import "path/to/pkg"` for Gom packages, `import go "pkg"` for Go;
  Go-style alias `import alias "path"`
- add a spec example of a Gom-to-Gom import and a .gomi with imports
