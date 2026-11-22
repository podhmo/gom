# Record metadata is file-local: imported records cannot resolve fields

- 状態: 解消（2026-11-22 — wave31 lane C の調査で、本ファイルの残件
  記述は誤検証と判明。`registerForeignTypes`（internal/check/files.go）
  は import 先 record decl を `tyCons` と `c.records` の両方に登録する
  ため、フィールド選択・ctor・注釈/未注釈リテラル・record パターンは
  当初から全て解決していた。台帳項目も同時に [x] 訂正済み。
  残る設計論点「曖昧候補集合（import するだけで一意だったリテラルが
  E0407 になりうる）」は
  [decisions/2026-11-22-imported-record-literal](../decisions/2026-11-22-imported-record-literal.md)
  で現状維持（候補 = file-local ∪ import 先）を採用）

## 状況（解消済み・歴史的記述）

The checker keeps record/sum shape information in `c.tyCons` /
`c.records`. Imported declarations arrive via `registerForeignTypes`
(internal/check/files.go), which registers imported record decls in
**both** — so the whole pipeline resolves:

- Unannotated literals `{ x = 1, y = 2 }`: `recordByFields` enumerates
  `c.records`, which already includes imported records — a literal of an
  imported record type resolves uniquely (or E0407 on ambiguity).
- Record patterns resolve via `recordOf(want)` against `c.tyCons` —
  imported records included; there is no field-set enumeration fallback
  for patterns at all.
- Field selection `p.x`, ctor calls, and annotated literals resolve the
  same way.

（誤検証の記録: 本ファイルは長らく「recordByFields がファイルローカル
宣言しか見ない」「import 先 record のリテラルは E0401」と記述していたが、
2026-11-22 の two-package 検証で全部解決済みと判明。stale バイナリでの
確認が原因と思われる。）

## 残る設計論点

import 先 record が候補集合に入る副作用として、file-local record と
同名フィールドを持つ import が同居すると一意だったリテラルが E0407
ambiguous になる。現状維持（候補は union、曖昧は annotate 案内）を
採用 — 詳細は decision 参照。

## 関連

- internal/check/typenv.go (`tyCons`, `records`, `recordOf`),
  expr.go `checkRecordLit`/`recordByFields`/`checkSelect`,
  pat.go `checkRecordPat`/`recordOf`, files.go `registerForeignTypes`
- decisions/2026-11-22-imported-record-literal.md（残論点の決定）
- decisions/2026-11-01-gomi-interface-files.md
