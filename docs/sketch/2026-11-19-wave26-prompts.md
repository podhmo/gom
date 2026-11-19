# Wave 26 orchestration prompt drafts

State as of 2026-11-19: wave25 の 4 lane（#75–#78）を PR 済み・
チェーンでマージ待ち。A は Ref メンバー実装、B–D は調査 wave
（採否は podhmo 判断待ち）。このファイルは wave26 の prompts
草案。実行モデルは wave25 と同じ前提（単独実行・Devin Review・
podhmo がマージ）。lane 順は下からチェーンする。

## 候補（採否は podhmo が lane ごとに決める）

### A. `fn` 本体の期待型伝播（`tap` イディオム解放）— `devin/wave26-fn-body-want`

```
[impl] wave25 lane B の副産物: 注釈付き `fn` 本体の内部で
`want` が届かず、field-write 直下の `Go.funcN` で lambda
パラメータが E0407/E0510（implementation-gaps の open 項）。
`let` ブロック直下では動くので修復は `check` の伝播接続のみ —
これが直れば `Ptr.new () |> tap init` 形の初期化イディオムが
書ける。`fn` パラメータ注釈（E0101）は本 lane のスコープ外。
spec 振る舞い追加なら testdata/spec にケースを足し、
implementation-gaps の項を閉じる。
Ownership: internal/check, testdata/spec,
docs/implementation-gaps.md.
```

### B. `cmp` 合成関数 — `devin/wave26-cmp-composers`

```
[impl] wave25 lane C 案 1（採否を待たずに進められる低コスト側）:
`stdlib/cmp` に「辞書→辞書」の合成関数を追加 —
`option_ord`/`option_eq`、`list_ord`/`list_eq`、
`pair_ord`/`pair_eq`（(a, b) タプル）、必要なら `result_ord`。
多相フィールドを持つ辞書の手作業を Option/List/tuple で消す。
`desc`/`eq_of` の配置と命名規約に揃える。`.gomi` 更新・
ガイド `ad-hoc-polymorphism.md` に合成節を追記。
Ownership: stdlib/cmp (cmp.gom, cmp.gomi, cmp_test.go か gom
test), skills/gom-agent-guide/references/ad-hoc-polymorphism.md.
```

### C. wave25 調査 lane の採否に応じた実装 — `devin/wave26-followups`

```
[decide→impl] podhmo の lane B/C/D 採否次第で 1 本選ぶ:
- (a) composite literal 採用なら `pkg.T { F = v }`（`&T{...}`
  専用形）の syntax→check→codegen
- `@derive` 採用なら `Eq`+`Ord` の構造 derive（多相フィールドは
  v2 以降に分離）
- interface impl 採用なら `fmt.Stringer`/`error` の最小スコープ
  （値レシーバ・引数なしから）
採否が出なければ本 lane はスキップして D だけ回す。
Ownership: 採択 lane の sketch に記載の経路。
```

### D. 省略可能引数・ラベル付き引数の制限緩和の調査 — `devin/wave26-labeled-args`

```
[investigate] open-questions 残件: 「ラベル付き引数の制限の緩和
（型が未知の関数へのラベル付き適用、ラベル付き／なし関数の暗黙
変換）」と「省略可能引数とデフォルト値」の設計空間。現行の
ラベル付き呼び出し規則（spec 該当節）と、緩和した場合の unify・
arity 検査への影響範囲を調査。
Ownership: docs 調査 →（採用なら）internal/check/unify, syntax,
docs/spec.md.
```

## 申し送り

- CI は push-to-main / PR-to-main synchronize でしか走らない —
  チェーン PR は Devin Review のみ。マージは podhmo が
  retarget → `gh pr merge --merge` を下から順に。
- wave25 ラウンド記録: `2026-11-19-wave25-round.md` — builtin
  member module は `resolve.stdlibModules` 登録必須（E0201 note
  の嘘）、`Ref` は境界変換なしの Gom-only 型、`Go.funcN` seed
  は field-write 直下では動くが `fn` 本体では `want` 切れ。
- lane A は wave25 lane B（E0407）の修復 lane — lane B スケッチ
  の「壁 2」節が実装の入口。
