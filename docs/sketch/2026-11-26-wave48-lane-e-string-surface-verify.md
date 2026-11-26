# wave48 lane E: string byte surface 検証

- `stdlib/str/str.gom` / `.gomi` に `length` / `get` / `slice` / `runes` を宣言し、`gom check stdlib/str` が通ることを確認した
- `gom build stdlib/str` で `str.go` を生成し、`go test ./stdlib/str` で package が compile されることを確認し、byte/rune 境界は spec fixture と外部 module compile で確認した
- 外部 module から `import "str"` した `str.length "abc"` が compile され、生成 Go が `github.com/podhmo/gom/stdlib/str` を import することを確認した
- spec fixture `ok_import_stdlib_string` を追加した

設計は [decision](../decisions/2026-11-26-string-byte-surface.md)。
