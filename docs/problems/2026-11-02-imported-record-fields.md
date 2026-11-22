# Record metadata is file-local: imported records cannot resolve fields

- 状態: 一部解消（2026-11-22 棚卸し — `.gomi` が record フィールドを
  運ぶようになり `registerForeignTypes` が import 先の decl を
  `tyCons` に登録するので、フィールド選択 `p.x`・ctor 呼出し
  `shapes.Point 1.0 2.0`・import 型への注釈付きリテラルは解決済み。
  残るは未注釈リテラル `{ x = .. }` と record パターン —
  `recordByFields` が候補をファイルローカル宣言に限定している。
  台帳 [docs/implementation-gaps.md](../implementation-gaps.md) の
  「未注釈 record リテラルが import 経由の record 型に解決されない」
  に移管済み、設計論点（import 先のどの record を候補にするか・
  曖昧一致時の診断）は wave31 lane C で decision draft にする）

## 状況

The checker keeps record/sum shape information in `c.tyCons` /
`c.records`. Imported declarations arrive via
`registerForeignTypes` (internal/check/files.go), so field selection
and ctor calls resolve — but `recordByFields` still enumerates only
the file's own declarations:

- An unannotated record literal `{ x = 1, y = 2 }` resolves its record
  type by field set (`recordByFields`) — but only over records
  declared in the same file. A literal of an *imported* record type
  cannot be resolved this way and reports "no record type has fields
  ..." even though the type exists (2026-11-22 検証済み).
- Record patterns share the same file-local enumeration.
- `SignatureSource` (`internal/check/env.go`) supplies value-level
  signatures (functions, methods, extern names, builtins) but has no
  channel for type *shape* — field names, indices, and payload
  layouts. (歴史的記述 — `.gomi` は今この shape を運ぶ)

## なぜ問題か

- Cross-package records are unusable in practice until this is filled:
  no field access, no literal, no pattern — the typed AST's `Sym` and
  `Index` fields that lowering and codegen need would be nil/-1.
- The `.gomi` interface-file design (decisions/2026-11-01-gomi-interface-files.md)
  will need to carry record field layout; this is the consumer-side
  gap that forces the question of what a `.gomi` records section
  looks like.
- A possible intermediate state — record types importable for
  *annotations only* (opaque, no field access) — is defensible for
  P1 but must be a conscious limitation, not a silent gap.

## 考えられる対応

- Extend `SignatureSource` (or add a `ShapeSource`) that can answer
  "the fields/variants of type `pkg.T`" so `tyCons` can be seeded for
  imports; `.gomi` gains a records section.
- Cheaper bridge for P1: treat imported nominal types as opaque and
  diagnose field access on them with a dedicated message until the
  interface format lands.
- Decide whether `recordByFields` should search imported records too
  (scope questions: which imported records are "candidates" without
  an annotation?).

## 関連

- internal/check/typenv.go (`tyCons`, `records`, `recordOf`),
  expr.go `checkRecordLit`/`checkSelect`, pat.go `checkRecordPat`
- internal/check/env.go `SignatureSource`
- decisions/2026-11-01-gomi-interface-files.md
