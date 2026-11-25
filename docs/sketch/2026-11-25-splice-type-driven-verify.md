# splice 表示の型駆動統一 — 検証スケッチ

- 日付: 2026-11-25
- decision: [2026-11-25-splice-display-type-driven.md](../decisions/2026-11-25-splice-display-type-driven.md)

## 検証マトリクス

`internal/build/compare_test.go` の compare case で oracle（eval）と
compiled（生成バイナリ）の stdout を両方検査:

| case | 検査すること |
| --- | --- |
| `splice_scalar_literal_record_shape` | `let f : Flag = true` が `{ on = true }` — literal materialize と record literal で同じ表示 |
| `splice_nested_peel_record_shape` | peel chain `type Masked = {u: UserId}` が `{ u = ({ v = 8 }) }` と入れ子で描かれる |
| `splice_field_record_shape` | record literal のフィールド内の scalar も宣言フィールド型で包まれる |
| `splice_list_tuple_record_shape` | List 要素・tuple スロットへの再帰 — `[({ on = true }), ...]` |

全て compiled と eval の stdout が一致（exec サブテストで生成バイナリを
実際にビルド・実行して確認）。

## 設計上の確認

- 値の表現は不変: scalar は scalar のまま（算術・等価・peel 意味論に影響なし）
  — 包むのは `displayTyped` の描画直前のみ
- generic な 1-field record は包まない — generic な宣言は peel されず struct
  emit なので scalar literal の代入がそもそも型付かず、包むべき scalar は
  値として現れない（compiled は concrete インスタンスに ShowRecord の
  renderer を通すが、そこに乗る値は必ず record 構築されている）
- float32 の幅表示（lane B）と統合: `displayTyped` が一本の入口
