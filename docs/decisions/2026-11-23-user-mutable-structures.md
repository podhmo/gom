# ユーザー宣言の可変構造

- 状態: 採用（調査の結果、宣言型の推移展開は実装済みと判明。残る隙間は extern 裏付け型のみ — `.gomi` の `//gom:mutable` directive を採択）
- 日付: 2026-11-23

## 背景

open-questions の未決項「`Ref a` と可変配列 `Array a` 以外に可変構造を許すか」。W0701/W0702（mutable-sharing lint、spec 10 章、[mutable-sharing-lint](2026-11-04-mutable-sharing-lint.md)）の可変判定は builtin の閉じた集合 — `Ref`/`Array`/`Ptr`/`Go.Slice`/`Go.Map` — に基づき、ユーザー宣言型をどう扱うかが懸案だった。

## 調査結果

`containsMutable`（internal/check/lint.go）は `Con` の既定経路で tyCons を展開する — 宣言された record/sum のフィールドを Subst で具体化して再帰し、`seen` で循環を防ぐ。つまり **`Ref`/`Array`/`Ptr`/`Go.Slice`/`Go.Map` を含む宣言型は既に「可変を含む」と判定される**:

- `type Box = { cell: Ref Int }` — `Box` 型のキャプチャは W0702、`Chan.make Box` は W0701 が出る（推移律どおり）
- `type Wrap = | W { b: Box }` — sum variant のフィールド経由でも伝播する
- `shadowedBuiltin` で同名列に影を落とされた builtin は可変扱いしない（偽陽性抑止、既存）

見えないのは **extern 裏付けで可変状態を持つ宣言型**だけ:

- extern パッケージが `type Queue a` を宣言し実装を Go の `[]T`/`map`/mutex で書いた場合、`.gomi` には `type Queue a`（kindAbstract）と操作関数群だけが見える。lint は `Queue` が可変状態を運ぶことを推認する手立てがない — `Chan.make (Queue Int)` や `go {}` への `Queue` キャプチャは警告されない
- これは `//gom:noescape`・`//gom:result-notaint` と同じ形の問題 — パッケージ境界を跨いだ性質は `.gomi` の directive でしか伝えられない

## 決定

**案 A 採択**: 可変構造の入口は閉じた builtin 集合のまま据え置き、宣言型の可変性はフィールドの推移律で導出する（実装済み・変更不要）。extern 裏付け型だけを `.gomi` の **`//gom:mutable <name>`** directive で覆う:

- extern パッケージの著者が `.gomi` に手で書く（`//gom:noescape` と同じ directive 系統。toolchain は生成しない — 著者の意図表明のみ）
- `.gomi` ロード時に該当 `tyCons` エントリへ「mutable」フラグとして適用し、`containsMutable` が `Qual != ""` の `Con` でも照合する
- ローカルの `.gom` 宣言には効果を持たせない — ユーザー宣言型の可変性はフィールドから常に導出可能で、矛盾する宣言（可変フィールドを持たない型への `//gom:mutable` 付与）は `.gom` 側では受理しない（extern の `.gomi` は実装が見えないので信用する）

棄却した選択肢:

- **何もしない**: extern パッケージは上級用法で偽陰性を許容する。今の extern パッケージ群（stdlib の `Ref`/`Array` 等）は builtin 集合で既に覆われており、実害は将来のユーザー extern に限られる — しかし directive 追加は noescape/notaint の既存機構の再利用で安価
- **第一級の `mutable` 型修飾**: 言語面に新しい修飾を導入して全ての宣言型が可変性を自己表明する。宣言型の可変性は既にフィールドから導出できており冗長 — 効果は extern 型への伝播だけで、型システムを広げる程の対象ではない

## 実施

TODO: `//gom:mutable` directive の `.gomi` scan/apply/emit（`scanDirective` 系統に追加、同じ lexer 共通路）、`containsMutable` の照合、spec 10 章への記述。priority low — ユーザー extern パッケージが実在するまでの隙間。

## 関連

- [mutable-sharing-lint](2026-11-04-mutable-sharing-lint.md) — W0701/W0702 の規則本体
- [ref-members](2026-11-19-ref-members.md) — `Ref` モジュール面
- spec 10 章 — 可変共有 lint の仕様
