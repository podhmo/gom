# Wave 26 orchestration prompt drafts

State as of 2026-11-19: wave25 の 4 lane（#75–#78）を PR 済み・
チェーンでマージ待ち。A は Ref メンバー実装、B–D は調査 wave。
#76 スレッドで podhmo が lane A/B（遅延 select 拡張・壁2 の
`fn` 本体 want 伝播）を採択 — ともに「次に入れたい」と明記。
lane C も採択（「辞書を先に定義して渡す部品として stdlib 提供は
便利」— 2026-11-19 チャット）。lane D はラベル引数の調査を
採択（同上）。deriving は「`Ord` だけのための導入は見送り」に
確定（decisions/2026-11-19-deriving-declined.md）。
このファイルは wave26 の prompts。実行モデルは wave25 と
同じ前提（単独実行・Devin Review・podhmo がマージ）。
lane 順は下からチェーンする。

## 候補（採否は podhmo が lane ごとに決める）

### A. `Ptr ?m` への遅延 select/assign（内側注釈不要化）— `devin/wave26-ptr-meta-defer` — **採択済み**（#76 スレッド 2026-11-19）

```
[impl] `let client : Ptr http.Client = { let c = Ptr.new ();
c.Timeout = ...; c }` が内側注釈なしで通るようにする。現状は
`c : Ptr ?1` が裸メタでないため select が遅延されず即
E0511/E0401。checkSelect の遅延条件を「受け手の中に未解決
メタを持つ Con（`Ptr ?m` 等）」まで広げ、finalize の
revisitSelects fixpoint で解く — 外側 want と最終式 `c` の
unify で `?1` が束縛されたあと member が引ける。pendingAssigns
経由で代入 RHS の want（`Go.FuncN`）も revisit 時に届くはず。
メタが最後まで解けないときの正直な E0407/E0401 と診断順序の
変化に注意。spec 振る舞い追加なら testdata/spec にケース。
Ownership: internal/check (expr.go checkSelect/revisitSelects/
pendingAssigns), testdata/spec.
```

### B. `fn` 本体の期待型伝播（`tap` イディオム解放・壁2）— `devin/wave26-fn-body-want` — **採択済み**（#76 スレッド 2026-11-19）

```
[impl] wave25 lane B の副産物: 注釈付き `fn` 本体の内部で
`want` が届かず、field-write 直下の `Go.funcN` で lambda
パラメータが E0407/E0510（implementation-gaps の open 項）。
`let` ブロック直下では動くので修復は `check` の伝播接続のみ —
これが直れば `Ptr.new () |> tap init` 形の初期化イディオムが
書ける。`fn` パラメータ注釈（E0101）は本 lane のスコープ外。
lane A と同じ領域（check の want 伝播）なので衝突に注意 —
A の遅延経路とどちらが先に効くか fixture で確認。
testdata/spec にケースを足し、implementation-gaps の項を閉じる。
Ownership: internal/check, testdata/spec,
docs/implementation-gaps.md.
```

### C. `cmp` 合成関数 — `devin/wave26-cmp-composers` — **採択済み**（2026-11-19 チャット）

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

### D. ラベル付き引数の制限緩和・省略可能引数の調査 — `devin/wave26-labeled-args` — **採択済み**（2026-11-19 チャット）

```
[investigate] open-questions 残件: 「ラベル付き引数の制限の緩和
（型が未知の関数へのラベル付き適用、ラベル付き／なし関数の暗黙
変換）」と「省略可能引数とデフォルト値」の設計空間を調査。
現行のラベル付き呼び出し規則（spec 該当節）と unify・arity
検査への影響範囲を測り、導入する場合の構文・推論コストと
`.gomi`/境界への影響を整理する。
Ownership: docs 調査 →（採用なら）internal/check/unify, syntax,
docs/spec.md.
```

（wave25 調査 lane の残り採否 — composite literal (a) と Go
interface 実装 — は未確定のまま。採択されたら別 lane として
追加する。deriving は「`Ord` だけのための導入は見送り」に確定
— decisions/2026-11-19-deriving-declined.md）

## 申し送り

- CI は push-to-main / PR-to-main synchronize でしか走らない —
  チェーン PR は Devin Review のみ。マージは podhmo が
  retarget → `gh pr merge --merge` を下から順に。
- wave25 ラウンド記録: `2026-11-19-wave25-round.md` — builtin
  member module は `resolve.stdlibModules` 登録必須（E0201 note
  の嘘）、`Ref` は境界変換なしの Gom-only 型、`Go.funcN` seed
  は field-write 直下では動くが `fn` 本体では `want` 切れ。
- lane A/B は同じ領域（`internal/check` の select 遅延と want
  伝播）に触れる — A→B のチェーン順で衝突を最小化。A が先に
  効くケース（内側注釈なし + フィールド代入の RHS lambda）は
  B 側で再テストが要る。
