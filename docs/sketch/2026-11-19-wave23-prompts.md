# Wave 23 orchestration prompt drafts

State as of 2026-11-19: wave22 の4 lane（#61-#64）を PR 済み・
チェーンでマージ待ち。このファイルは wave23 の prompts 草案。
lane 構成と順位づけは podhmo 採択待ち。実行モデルは wave22 と
同じ（単独実行・Devin Review レビュー・podhmo がマージ）。

## Lanes

### 1. TypeArgs (b) 実装 — `devin/wave23-ctor-targs-b-impl`（要 lane4 判定）

wave22 lane4 の棚卸し（[sketch](2026-11-19-ctor-targs-b-inventory.md)）
で (b) 採用と判定された場合のみ成立する lane。不採用ならこの lane は
削除し open-questions の項を閉じる。

```
[impl] CtorApp/CtorRef の TypeArgs フィールドを廃止し Typ の
Con.Args を唯一の正にする（decision
2026-11-18-ctor-typeargs-authoritative の案 (b)、wave22 lane4
sketch 2026-11-19-ctor-targs-b-inventory 採用判定）。
core: CtorApp.TypeArgs / CtorRef.TypeArgs のフィールド除去。
lower: emit 8 経路（expr.go:203/208/348/351/1050/1445/1821/1833
付近）の TypeArgs 引数を落とす — Typ は既に同一具現化なので
追加の計算は不要。codegen: ctorTArgs を resultCon(Typ).Args
のみに単純化し variantParams/ctorLit から targs 引数を外す。
validator: ctorTArgs 関数と CtorApp/CtorRef の targs 呼出を撤去
（検査対象のフィールド自体が消える）。treedump の表示を落とす。
lane1 で足した TypeArgs 検査テストは対象が消えるため撤去・
書き換え。golden 出力は不変のはず — 差分が出たら emit 経路の
見落とし。decision (b) 採用の確定記録と open-questions 項の
クローズを同梱。
Ownership: internal/core, internal/lower, internal/codegen,
internal/treedump, internal/core/validate_test.go, docs/decisions,
docs/open-questions.
```

### 2. LSP shared importer — `devin/wave23-lsp-shared-importer`

wave22 の棚上げ案1。open-questions「LSP の Go import oracle の共有」
に設計済みの経路がある（session-scoped `goimport.Loader` 持ち回り +
exportcache fingerprint invalidation）。

```
[impl] gom lsp が compile のたびに goimport.Loader を作り直し、
export data の parse と member table 構築を毎回払っている
（per-change 数 ms オーダー）。LSP server が workspace dir ごとに
長寿命の Loader を1本持ち回り、*types.Named identity を安定
させて member table を compile 間で共有する。invalidation は
Loader 全体: compile 開始時に exportcache fingerprint（dep dir
単位の max ファイル mtime + 件数の srcStat）を照合し、miss なら
Loader ごと作り直す。go.mod/go.sum 変更も同一バージョンの dep
ソース編集も拾える。決定は decision (draft) → 検証 sketch の
順。計測は LSP hover/diagnostics の per-change レイテンシ差
（before/after）を sketch に載せる。
Ownership: internal/lsp (server/session), internal/goimport
(loader 保持), docs/decisions, docs/sketch.
```

### 3. cons 非適用形の蓄積 quadratic — `devin/wave23-noncons-accum`（調査）

`docs/problems/2026-11-05-stdlib-cons-built-combinators-quadratic.md`
残件: cons-recursion 特殊化で match-cons 蓄積は buffer loop 化済み
だが `concat` 系の非 cons 形は残ると記録。どのパターンが残っていて
救済の設計空間があるかの調査 lane。

```
[investigate] cons-recursion 特殊化（decision
2026-11-18-cons-recursion-specialization）が救えない蓄積形を
棚卸し: パターン別（連鎖 append、fold 内 concat、相互再帰の
蓄積）にどこまで残っているかを実測つきで整理し、救済案
（hybrid repr、より広い特殊化、Array 指向の idiom 誘導）の
費用対効果を sketch にまとめる。実装判断は podhmo。
Ownership: internal/lower (consrec), stdlib/list,
docs/sketch, docs/problems（該当項の更新）.
```

### 4. テスト速度の継続対策 — `devin/wave23-test-speed`（hygiene 寄り）

`docs/problems/2026-11-05-slow-tests.md`（状態: 対応中、10倍目標）。
wave22 lane1 のベンチ基盤（`scripts/bench-parallel.sh`）が使える。

```
[investigate] go test の遅いパッケージの内訳を取り直し、問題文書
の「テスト設計 → アーキテクチャ → キャッシュ」順で次に効く一手を
特定する。short mode 分離・golden/spec fixture の並列度・
ツール起動回数の削減候補を計測つきで sketch に。実装判断は
podhmo。
Ownership: internal/build・internal/gomtest（テスト構成）,
docs/problems/2026-11-05-slow-tests.md, docs/sketch.
```

## Post-lane work（lane 全完了後 — 締め作業）

```
[close-out] wave23 round 記録（docs/sketch/*-wave23-round.md）、
gom-agent-guide 同期、wave24 prompts 草案、台帳
（open-questions / implementation-gaps / TODO.md）の掃除を
最終 lane のブランチに載せる。
```
