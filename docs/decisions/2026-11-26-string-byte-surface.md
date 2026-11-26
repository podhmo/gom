# 文字列操作は stdlib package の byte 面に置く

- 状態: 採用（prototype 検証済み）
- 日付: 2026-11-26

## 決定

`len(s)` / `s[i]` / `s[lo:hi]` 相当は builtin `String` のメンバー面や専用構文にせず、`import "str"` で読み込む toolchain stdlib package `str` に置く。

最小面は:

- `str.length : String -> Int` — byte 長
- `str.get : Int -> String -> Option Byte` — byte index の読み出し。範囲外は `None`
- `str.slice : Int -> Int -> String -> String` — byte range。範囲外は Go と同じく panic
- `str.runes : String -> List Char` — UTF-8 decode による明示的な rune 走査。不正 byte 列は Go の変換と同じく U+FFFD

データは最後の引数に取り、既存 stdlib と同じく `s |> str.slice 0 1` の pipeline 形を保つ。Unicode 文字列処理全般や mutable string builder は対象外。

## 理由

これらの操作は `String` 型の表現へ踏み込むだけで、型制約・特殊な型推論・専用 lowering を必要としない。compiler-owned builtin member にすると resolve/check/lower/codegen の手組み oracle が増えるが、stdlib package なら通常の `.gom`/`.gomi` の公開面として実装できる。文字列補間や比較のような言語コアの操作とは層が違う。

byte index を正にするのは Go 文字列の表現に一致し、部分列のコストと失敗条件を隠さないため。rune 走査は暗黙の index 規則を足さず、`runes` への変換として明示する。

## 実装メモ

`stdlib/str` は `@extern fn` の薄い Go 実装。短名は `str` にする。`string` は生成 Go で predeclared type `string` と衝突し、利用側の import alias が生成コード内の型名を覆すため。
