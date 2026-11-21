# `type X = T` エイリアス構文 — 採否の推薦

- 状態: 採用（調査に基づく推薦 — defer。コード変更なし）
- 日付: 2026-11-21
- 問い: wave28 lane E — `AliasBody` が parser から到達不能

## 調査結果

`type X = T` の alias 構文（`AliasBody{Type}`）は内部 AST・resolve
（`resolveAlias` の lazy 展開）・check（`kindAlias`）・iface hash・
treedump に処理が揃っているが、`parseTypeBody` が `{` → RecordBody /
それ以外 → variant-list の 2 分岐しか持たず**一度も emit されていない**
（初版コミットから存在しない）。

実際の挙動（`/tmp` probe で検証）:

- `type A = Int`・`type C = Option Int`・`type L = List Int` — 型式に見える
  右辺が **単一バリアントの sum として静かに受理**される。`Int`/`Option`/
  `List` は UIdent なので `parseVariant` がコンストラクタ名として読み、
  後続の `Int` は位置フィールドになる。`A` は `Int` の別名ではなく
  コンストラクタ `Int` を持つ ADT になる
- `type B = any`、`type D = Int -> Int`、`(Int, Int)` — E0101
  （小文字・非 UIdent 先頭は variant にならずに落ちる）
- `type X = {f: Int}` — RecordBody として正しく動く（nominal record）

つまり「書けない」だけでなく「間違った意味で通る」方向に壊れている。

## 採否の検討

採用するには `type X = Foo` を「`Foo` 型への別名」か「`Foo` という
コンストラクタを持つ sum」か見分ける規則が要る。

- **semantic 曖昧性解消**（`Foo` が既存の型名なら alias、そうでなければ
  バリアント）: パーサは型名の解決を知らないので phase 境界を越える。
  `type Colour = Red` が後続宣言の有無で意味を変える非決定的な構文になる
- **別の綴りを足す**（`type X := T`、`type X alias T`、alias 側をカッコで
  囲う等）: 決定的だが、alias は nominal な `type X = {f: T}`（newtype）
  や素直な `T` 直書きで済む sugar に新しい構文の価値を払うことになる
- **棄却（AliasBody を消す）**: 下流の実装は稼働テスト済みで維持コストは
  ほぼない。消す積極的な理由もない

## 推薦: defer

alias の採用価値（Go の `type X = T` 対応の見た目、長い型式の略記）に
対し、決定的な採用経路には新構文が要る。今回の wave で構文を増やすほどの
需要は見えない。代わりに「variant へ静かに落ちる」現状の罠を
implementation-gaps に登録し、採用するときは `type X := T` 等の
識別可能な綴りから検討する。

将来採用する場合の着眼点:

- `parseTypeBody` で UIdent 先頭を `AmbiguousBody` 的に遅延させ、
  resolve で名前解決して sum/alias に分流させる案 — ただし variant-list
  `|` との組み合わせ（`type X = T | U`）をどう扱うかが残る
- alias の型変数パラメータ `type Wrap a = a` — resolve_test.go:1082 が
  既にこの形を扱う

なお spec §8.3 の「`type T = any` のエイリアスは `any` そのものに
展開されて `Go.Any` に写る」は書けない文法を参照している — gaps 台帳に
登録済み。
