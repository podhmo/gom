# wave57 lane E: 未 import Gom stdlib module candidate 調査

- 日付: 2026-11-28
- lane: E（調査のみ）
- decision: [../decisions/2026-11-28-e0201-unimported-stdlib-candidates.md](../decisions/2026-11-28-e0201-unimported-stdlib-candidates.md)

## fixture 比較

`/tmp/w57eprobe` で `go run ./cmd/gom` 相当の binary を使い、実装は変えず現行 JSON を比較した。

| fixture | 入力 | 現行 diagnostic | 解釈 |
| --- | --- | --- | --- |
| typo | `lst.map`（import なし） | E0201 `undefined name `lst``, `namespace: "module"` | `list` の typo 候補。OSA 距離 1 |
| forgotten | `list.map`（import なし） | E0201 `undefined name `list``, `namespace: "module"` | short name exact match。import 忘れ |
| imported | `import "list"` + `list.map` | diagnostics なし | short name default binding は正しく使える |

typo と import 忘れは現行出力上は同じ `namespace: "module"` であり、名前が short name と exact か OSA 距離 1 かで分ける必要がある。

## 所有関係

stdlib short-name table は `gomimport` の `stdlibPaths` にあり、`resolve` からは依存方向の都合で参照できない。将来の実装は table を移さず、`build.Options` → `resolve.Config` へ short name → canonical path の read-only map を注入する形が最小。`resolve` は候補 source の正を持たず、driver 注入の index だけを見る。

## alias/path の結論

Gom import は `import alias "path"` を持つが、stdlib short name は default binding が短く自明なので、候補は alias を提案せず `import "list"` を案内する。canonical path は JSON data に残し、text note では短名を優先する。既に `import lst "list"` 済みなら `lst` は resolve できるため、typo 診断とは衝突しない。

## 残件

- 実装: exact/typo の structured data shape、最大件数、text note 文言
- safe fix: import 追加位置（既存 import block の有無、format 規則）を別 decision で決める
