# Gom package import syntax and .gomi contents are unspecified

- 状態: ほぼ解消（2026-11-06 — `import "path"`・`import alias "path"`・`.gomi` 内の import が spec §5/§7 で定義された。束縛名は別名があれば別名、なければパス末尾要素。参照: [decision](../decisions/2026-11-06-gom-import-alias-and-gomi-imports.md)、[検証](../sketch/2026-11-06-import-alias-and-gomi-imports-verification.md)。残件: 末尾要素と `package` 句の食い違いや末尾が識別子でないパス（`yaml.v3` 型）は明示の別名で救済 — 生成 import の無条件別名化はフォローアップ。`.gomi` 中の通常 `//` コメントは宣言ではなく字句 trivia として受け入れられるが spec は宣言の列挙のみで明言していない）
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
