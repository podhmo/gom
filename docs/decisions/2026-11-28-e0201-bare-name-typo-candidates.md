# E0201 typo 候補の第二段: bare value name を優先する

- 状態: 採用（wave57 lane B として実施決定。実装は未実施）
- 日付: 2026-11-28

## 決定

次に実装する面は **bare value name typo** に限定する。E0201 `namespace: "value"` で、参照位置から見える value namespace の scope chain を走査し、optimal string alignment 距離 1 の候補を最大 3 件、note と `data.candidates` に出す。`fixes` は付けない。

未 import Gom stdlib module candidate は今回実装しない。

## bare value name の実装境界

対象は `undefinedTerm` が出す E0201 だけとする。候補集合は参照位置の lexical scope chain を innermost から outermost へ辿った `terms` とし、次を満たす。

- kind filter: `KindVar`・`KindFunc`・`KindCtor`・`KindBuiltin` だけを候補にする。`KindModule`・`KindType`・`KindTypeVar`・`KindField`・`KindMethod` は value position で使えないので出さない
- visibility filter: non-test code から見えるが使えない `TestOnly` symbol、test-only builtin の `assert` / `assert_eq` / `snapshot`、Go の `Hidden` symbol は出さない。これらの builtin は `TestOnly` flag ではなく checker 側の規則で制限されるため、名前で明示的に除外する
- shadowing dedup: 同じ名前は 1 回だけ数える。innermost の binding が優先だが、候補表示は名前だけなので shadowed binding を別候補にしない
- false positive control: 長さ 3 未満の入力名には候補を出さない。`x`/`y` のような 1 文字変数では距離 1 が頻繁すぎるため
- ordering/limit: 距離昇順（現状は 1 のみ）・名前昇順、最大 3 件。note/structured data は lane B と同じ形に揃える
- JSON: `data.candidates` は `{ "name": string, "edit_distance": 1 }` の列

fixture は少なくとも positive（typo から 1 件）、negative（距離 2 以上）、truncate（4 件以上から 3 件）、shadowing dedup、kind filter、short-name suppression、text/JSON を必要とする。

## 未 import Gom stdlib module を後送りする理由

`import "list"` 系の候補は有用だが、resolver は `gomimport` を import できない（依存方向が逆）ため、stdlib short-name index を `Config` へ注入する所有関係を先に決める必要がある。表示すべき import alias も未決定で、`lst.map` に対して canonical 名 `list`、利用者の typo `lst`、または既存プロジェクト慣習の alias のどれを勧めるかで UX が変わる。候補数は少なく false positive は抑えやすい一方、設計の中心は typo 距離ではなく toolchain index と import 文言になる。

対象外を確認する: goimports 的な未 import Go package 探索は行わない。Gom stdlib short name だけを将来の対象とする。

## 比較

| 面 | 主な価値 | 主な設計コスト | 判断 |
| --- | --- | --- | --- |
| bare value name typo | 変数・関数・constructor の typo を直接助ける。既存 scope chain で完結する | shadowing dedup、kind/visibility filter、短い名前の false positive 制御 | 次に実装 |
| 未 import Gom stdlib module | `lst` → `list` のような module 名 typo/import 忘れを助ける | stdlib index の注入、alias 表示、import path の canonical spelling | 後送り |

調査記録: [sketch/2026-11-28-wave56-lane-e-typo-candidates-verify](../sketch/2026-11-28-wave56-lane-e-typo-candidates-verify.md)。
