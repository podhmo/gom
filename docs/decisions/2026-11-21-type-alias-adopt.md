# 型エイリアス構文 `type alias X = T` の採用

- 状態: 採用
- 日付: 2026-11-21
- Supersedes: [2026-11-21-type-alias-syntax](2026-11-21-type-alias-syntax.md)
- 問い: wave29 lane E — `type X = T` の silent-variant 罠をどう解くか

## 決定

`type alias X = T` を新しい構文として追加する。

`type X = …` は引き続き**新しい型の導入**で一貫させる — 単一 decl、
record（`{f: T}`）、sum（`A | B`）のどれも nominal な型を作る宣言。
`type alias X = T` だけが透明な別名（Go の `type X = T` に対応）を
導入する。

## 理由

wave28 lane E の調査（Supersedes 元）では「`type A = Int` が単一
バリアント sum として静かに受理される」罠が確認され、識別可能な
綴りが要ると結論していた。`type X := T` は Go の `=`（alias）と
記号の向きが逆に読めて紛らわしいため、`alias` キーワードを明示する
`type alias X = T` を採る。`type X = …` が常に new type を意味
するので意味の一本性が保てる。

## 実装の着眼点

- `AliasBody`・`resolveAlias`・`kindAlias`・codegen `talias`
  （`type Name[A] = T` emit）の下流経路は wave28 で確認済み。
  必要なのは `parseTypeBody` からの到達経路と `alias` の字句面
  （予約語化か `type` 直後の contextual keyword か — 実装時に
  decision 記録、既存識別子 `alias` への影響を測って選ぶ）。
- `type X = T`（alias 意図の誤記）が variant に落ちる罠自体は
  残る — ガード（型名一致 lint 等）の要否は別途評価。
- spec §8.3 の「`type T = any`」の記述は `type alias T = any`
  への書き換えが要る。

## 実装時の決定（wave29 lane E 完了時点）

- **`alias` は contextual keyword**（`type` の直後かつ UIdent が
  続くときだけ）— 予約語にはしない。`let alias = ...` や
  `x.alias` は従来どおり普通の識別子として書ける
  （golden `poly_functions.gom` の `let alias = choose` が既存の
  使用例）。予約語化するとそれらが壊れるため、lookahead 付きの
  文脈判定を採った。
- **silent-variant 罠への lint ガードは入れない**（現時点）。
  `type A = Int` のような単一バリアント sum を「alias の誤記」
  と決めつける lint は、正当な単一バリアント sum（`type A = Int`
  を意図した ADT）を区別できず、偽陽性が避けられない。別名が
  欲しい側は `type alias` という明示的な綴りを持ったので、
  書き手側の問題として残す。
- alias の body は無名 record を許さない（`type alias R = {f:Int}`
  は E0401 — 無名 record は宣言型になれない既存ルールと一致）。
- tree-sitter 側も `type_decl` に `alias` 分岐を追加（本体は
  `type_body` ではなく `type_expr`）。
