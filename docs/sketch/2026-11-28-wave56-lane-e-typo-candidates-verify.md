# wave56 lane E: E0201 typo 候補第二段の調査

- 日付: 2026-11-28
- lane: E（調査のみ）
- 対応 decision: [../decisions/2026-11-28-e0201-bare-name-typo-candidates.md](../decisions/2026-11-28-e0201-bare-name-typo-candidates.md)

## 調査方法

`internal/resolve/scope.go`・`walk.go`・`expr.go`・`typex.go`・`diags.go` と `internal/gomimport/loader.go` の stdlib short-name index を読み、`/tmp/e0201probe` の fixture で現行 JSON を確認した。repo の実装は変更していない。

## 現行 fixture

| fixture | 入力の要点 | 現行 diagnostic | 観察 |
| --- | --- | --- | --- |
| `value.gom` | `area` 定義済み、`aera 1` を参照 | `undefined name `aera``, `namespace: "value"` | 候補 note/data はない。OSA 距離 1 |
| `shadow.gom` | parameter `count`、local `total`、未解決 `tota` | `undefined name `tota``, `namespace: "value"` | inner block の `total` が最有力。scope chain 走査で届く |
| `module.gom` | 未 import の `lst.map` | `undefined name `lst``, `namespace: "module"` | `list` stdlib の typo だが現行は候補なし |

## bare value name の設計確認

resolver の `scope` は `terms` / `types` を分け、lookup は innermost から outermost へ辿る。bare value typo はこの chain をそのまま使える。ただし `terms` には value だけでなく module も入るため、候補用には kind filter が必須である。

必要な filter は次のとおり。

- include: `KindVar`・`KindFunc`・`KindCtor`・`KindBuiltin`
- exclude: `KindModule`・`KindType`・`KindTypeVar`・`KindField`・`KindMethod`
- non-test code から使えない `TestOnly` と、使っても E0501 になる `Hidden` を除外
- 同名は shadowing に関係なく 1 回だけ候補化する
- 入力名が 3 文字未満なら候補を出さない

`aera` → `area` は transposition で距離 1、`tota` → `total` は deletion で距離 1。どちらも lane B の OSA metric と上限 3 件を再利用できる。

## 未 import stdlib module の設計確認

Gom stdlib short name は `gomimport` 内部の `stdlibPaths` に `list`・`option`・`result`・`cmp`・`str` としてある。一方 `resolve` は `gomimport` へ依存できないため、候補を resolver が直接この表へアクセスして出すことはできない。`resolve.Config` へ short-name index を注入するか、build/CLI 側で供給する設計が先に要る。

また `lst` を `list` と診断しても、実際の修正には import 文の追加が要る。note に canonical path、short name、利用者 typo をどう表示するかは lane B の単純な `did you mean` より設計量が大きい。`fixes` を付けない制約のため、候補だけではまだ編集手順が残る。

## 結論

次に実装する面は bare value name typo。未 import Gom stdlib module candidate は、stdlib index 注入と import alias 表示を決めてから扱う。goimports 的な未 import Go package 探索は対象外。

## 残課題

- bare value name typo の実装（TODO E0201-2）
- member typo の context-aware kind filter を bare-name 実装と共通化できるか
- 未 import stdlib module 候補の Config/index 所有と alias 表示
