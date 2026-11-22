# `Char`↔`Int32`/rune 境界の調査 (wave31 lane F)

- 日付: 2026-11-22
- 目的: `Char`（Unicode コードポイント、Go 表現 `rune`）と `Int32`・Go の `rune` 引数の境界に実装ずれがないか調査する（investigate only、decision draft 不要の結論なら sketch のみ）。

## 検証結果 — ずれなし

| ケース | 結果 | 判定 |
| --- | --- | --- |
| `unicode.IsUpper c`（`rune` 引数に `Char`） | E0401 `expected Int32, got Char` | spec どおり — Go の `rune` は `Int32` と読むので `Go.conv` が必要 |
| `Go.conv c : Int32` / `Go.conv i : Char` | 両方向受理、`int32(c)` / `rune(i)` と emit、生成物コンパイル確認 | OK |
| `c < c3`（Char 同士の順序比較） | 受理 | Char は ordered scalar |
| `c3 - c`（Char 算術） | E0401 `expected a numeric type` | Char は非数値 — 距離を取るには `Go.conv` で Int32 へ |
| `'あ'` `'😀'` リテラル | 受理 | 直接 UTF-8 可 |
| `'\u0041'` / `'\u{1F600}'` エスケープ | 受理 | grammar.ebnf §2.3 の escape 規則どおり |
| `'\U0010FFFF'`（Go 式 `\U` 8 桁） | E0104 | `\u{...}` で代替 — grammar の PROVISIONAL 注記の設計どおり |
| `'\u{110000}'`（範囲外） | E0104 `outside the Unicode range` | lexer が utf8.MaxRune で検査済み |

## 観測（ずれではないが記録しておく点）

- `rune` 戻り値の Go 関数は `Int32` を返す（`Char` ではない）— `Char` に戻すには `Go.conv`。spec 8 章の記述どおりで、Char を rune 引数へ暗黙通過させない設計の帰結
- Char の算術は意図的に非数値 — `i + 1` のようなコードポイント演算は `Go.conv` 経由で Int32 で行う。検証コード: `let i : Int32 = (Go.conv c : Int32) + 1; Go.conv i : Char` は `tmp1 := int32(c); i := tmp1 + 1; return rune(i)` と emit される
- escape 集合は grammar.ebnf の `[PROVISIONAL]` 注記つきで暫定 — `\x` 2 桁や `\o` 8 進は未サポート（Go 式の `\U` は `\u{...}` が担う）

## 結論

実装ずれ・implementation gap は発見されず。Char/rune 境界は spec の記述どおり一貫している（`Char` は `Int32` ではない別の scalar、変換は `Go.conv` で明示）。
