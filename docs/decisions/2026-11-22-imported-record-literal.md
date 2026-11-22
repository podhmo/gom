# 未注釈 record リテラルと import 先 record — 既に解決済み、曖昧候補集合は現状維持

- 状態: draft
- 関連: [problems/2026-11-02-imported-record-fields](../problems/2026-11-02-imported-record-fields.md) / 台帳項目「未注釈 record リテラルが import 経由の record 型に解決されない」

## 背景

`docs/implementation-gaps.md` に「`{ x = .. }` のような未注釈 record
リテラルが import 先の record 型に解決されない（E0401 "no record type
has fields"）」という open 項目が残っており、wave31 lane C はその解消案を
調査する lane だった。

## 調査結果（2026-11-22、main `4c50488` で検証）

**ずれは存在しない — 既に解消済みだった。** `registerForeignTypes`
（`internal/check/files.go`、V-6 の multi-file 対応 `32650f7` 由来）は
import 先の record decl を `tyCons` と `c.records` の両方に登録しており、
未注釈リテラルの逆引き `recordByFields` は最初から import 先 record を
候補に含んでいた。two-package scratch で検証:

- `{ x = 1.0, y = 2.0 }` は `shapes.Point`（`import` しか参照がない
  場合でも）一意に解決する
- `match p { {x = n} -> .. }` の record パターンも期待型経由で
  import 先 record を解決する（パターン側はそもそも `recordOf(want)`
  で expected-type 駆動）
- 台帳・problems ファイルの「E0401 が出る」という記述は誤検証だった
  （stale バイナリで確認した疑いが強い — 同一セッションで別件の
  stale `/tmp/gom` 誤検証が発生している）

## 残る設計論点: 曖昧候補集合の構成

import 先 record は候補集合を広げる副作用がある — ファイルローカルの
`type Pt = {x, y}` と `import` した `shapes.Point` が同居すると、従来
一意だった `{x = .., y = ..}` が E0407 ambiguous になる。import する
だけで既存コードの解釈が変わりうる。選択肢:

- **A（採用案）: 現状維持** — 候補は file-local + import 先の union。
  曖昧一致は E0407 で「annotate the type」を案内する。優先順位の
  マジックがなく、解決規則が素直。
- **B: file-local 優先** — ローカル候補がちょうど 1 つならそれを採用、
  0 なら import 候補へ。OCaml 式の「unqualified が勝つ」慣習に近いが、
  import を足しただけでリテラルの型が黙って変わりうる別の驚きがある。
- **C: 未注釈リテラルは file-local のみ** — 一番保守的だが、import 先
  record を使う正当な未注釈リテラルまで E0401 に戻す（現状より後退）。

B/C は「import が解釈を変える」驚きを別形で抱える。A の E0407 は診断が
正直で、注釈を書けば一意に解決できる。現状維持を推奨し、曖昧一致が
実害として報告されたら B を再検討する。

## 決定（案）

候補集合は現状維持（案 A）。実装作業は発生しない — 台帳項目は誤検証の
訂正として `[x]` に、problems ファイルは解消に更新する。
