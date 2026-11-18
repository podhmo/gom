# experiments/w23/typeclass

wave23 lane3 の実験: 型クラス vs 明示的辞書渡し。同じ処理
（`min` / `sort` on `Ord`）を両案で書き切り、記述量・読みやすさ・
生成コードの差を比較する。調査は
`docs/sketch/2026-11-19-wave23-typeclass-vs-dict.md`。

- `dict/` — (1) 明示的辞書渡し。**今日コンパイルできる実物**:
  `a.gom` が `gom check`/`gom test`/`gom build` を通り、`a.go` と
  `a_test.go` は `gom build` の実出力
- `typeclass/` — (2) 型クラス風。`mock.gom.txt` が構文案の見た目、
  `gen.go` が elaboration 後に意味する手書き Go（辞書は (1) と同型、
  差は呼び出し側で暗黙に挿すことだけ）
