# E0201 未 import Gom stdlib module 候補: 限定的に採用する

- 状態: 採用（実装は次 wave 以降）
- 日付: 2026-11-28

## 決定

`undefinedTerm` が unresolved module name について出す E0201 `namespace: "module"` に限定し、toolchain 同梱の Gom stdlib short name（`list` / `option` / `result` / `cmp` / `str`）だけを将来の候補 source として採用する。goimports 的な Go package 探索と import 追加の safe fix は対象外とする。`notModule` のように同じ `namespace: "module"` を持つが名前自体は解決済みの診断には候補を付けない。

## 表示境界

- exact match: 未解決名が `list` など short name そのものなら import 忘れとして扱い、`add import "list"` を note に出す
- typo match: `lst` のように short name と OSA 距離 1 なら typo candidate として `did you mean list?` に加えて同じ import 手順を note に出す
- canonical path: JSON `data.candidates` には short name と canonical path（例 `github.com/podhmo/gom/stdlib/list`）を入れる。text note は短名を主語にし、必要なら canonical path を 1 回だけ示す
- alias: 明示 alias は提案しない。default binding の short name を正とする。既に alias import 済みならその名前は resolve できるため、この診断は出ない
- fixes: 付けない。import 追加位置の選択は safe fix として別途設計する

## index の所有

`resolve` は `gomimport` を import できない。stdlib short-name index の正は `gomimport` に残し、将来 `build.Options` から `resolve.Config` へ `ImportableModules`（short name → canonical path）として注入する。CLI の `compileOptions` が `gomimport` の表から供給し、resolver は表の由来を知らない。

## typo と import 忘れの区別

`list.map` を import なしで参照した場合と `lst.map` を参照した場合は、現行 JSON がどちらも `undefined name ... namespace:"module"` で同形だった。前者は exact match で import 忘れ、後者は OSA 距離 1 の typo candidate と区別する。距離 2 以上は候補にしない。

調査記録: [sketch/2026-11-28-wave57-lane-e-stdlib-candidates-verify](../sketch/2026-11-28-wave57-lane-e-stdlib-candidates-verify.md)。
