# 検証: 複数行カリー適用 — NL-2 維持の確認

wave44 lane G、[decision](../decisions/2026-11-24-multiline-application.md) の検証レポート。

## 手順

`testdata/gocall` モジュール配下に一時 fixture を置き `gom check` を実行
（検証後に fixture は除去）。

## 結果

| 形 | 結果 |
| --- | --- |
| `List.fold\n  (fn acc x -> acc + x)\n  0\n  [1,2,3]`（ブロック内） | E0407（`List.fold` 単体がジェネリック部分適用）+ 続く各行が別のブロック項目として E0402/E0407 |
| 同上（ファイルスコープの `let`） | 続く行が宣言でないため E0101 |
| `xs |> List.fold (fn acc x -> acc + x) 0` | ok |
| `(List.fold\n  (fn acc x -> acc + x)\n  0\n  [1,2,3])` | ok（NL-1: 括弧内の改行は空白） |

## 文法変更案の破壊量

`(` を line-start に加える案は `f ()\n()` — Unit ブロックの標準
終端 — を `f () ()` に読み替える。`testdata/` 配下で
`^\s*()$` を数えると 110 行あり、慣用形を大量に拒否することに
なるため却下（`[` もブロック値としてのリストリテラルを壊す）。
