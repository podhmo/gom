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
