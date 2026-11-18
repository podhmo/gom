# Wave 22 orchestration prompt drafts

State as of 2026-11-19: wave21 の4 lane（#57-#60）を PR 済み・
チェーンでマージ待ち。このファイルは wave22 の prompts。
採択済み（podhmo, 2026-11-19）: lane 構成 = **実測ベンチ →
W0502 `Ptr (Ptr E)` → flat 部分適用 timing → TypeArgs (b)
判定** の順。案1（LSP shared importer）は W0502 に差し替え
（LSP 系は shelf に戻す）。実行モデルは wave21 と同じ（単独
実行・Devin Review レビュー・podhmo がマージ）。

## Lanes

### 1. 並列ビルド実測ベンチ — `devin/wave22-parallel-bench`

```
[measure] wave21 lane3 で dir グループを dep 層 + GOMAXPROCS
ワーカーに並列化したが wall-clock の効果は未計測
（open-questions 未決項）。複数 dir・dep 連鎖の fixture を
作り、serial（単一 dir ごとの順次）相当との比較で効果を
計測する。fixture は独立 dir 群（dep なし）と連鎖 dep
（a→b→c）の2構成が最低限 — 層内の並列性と層をまたぐ直列性
の両方が見える形にする。計測は gom check / gom build の
wall-clock・繰り返し回数・並列度（GOMAXPROCS 変化）で
スケーリングを見る。結果と fixture・計測スクリプトは
docs/sketch/ に記録 — 並列化が効かないなら層構造のオーバー
ヘッドを切り分ける。open-questions の該当項を結果で閉じる
（効果が見えなければ「計測済み・効果なし」で閉じる）。
コード変更は原則なし — 計測系は scripts/ か testdata 下の
fixture としてコミットしてよい。
Ownership: scripts/, testdata/ or docs/sketch (fixture),
docs/sketch (結果), docs/open-questions (閉じ).
```

### 2. W0502 `Ptr (Ptr E)` の証明可能違反 — `devin/wave22-w0502-nested-ptr`

```
[impl] open-questions 未決項（wave9 error-tail レビュー nit
由来）: `errors.As` の target が `Ptr (Ptr E)` のとき、E が
named Go 型でなければ `*E` は error を実装できず interface
でもないため panic が証明可能 — 現状は nested-Ptr 除外で
沈黙している（lint.go notAsTargetElem の `case "Ptr"` が
無条件 false）。**確定済み（podhmo, wave22 prompts 作成時）:
証明可能な分だけ warn を広げる** — `Ptr (Ptr E)` の E が
builtin 型・ADT など repr が unnamed/インターフェース確定の
型なら W0502 を出す。沈黙を維持するのは E が `types.Go`
（named 型の `*E` メソッド集合は見えない）・`Meta`/`Var`・
宣言 record（named struct — `*R` の Error() は否定しきれない）
の場合。対象メッセージは既存 W0502 と同系で「element type
`*E` implements no method set and is not an interface」系。
decision（採択済み・実装の境界を draft で記録）→ spec の
lint 節・test fixture を更新。open-questions の該当項を閉じる。
Ownership: internal/check/lint.go (notAsTargetElem),
testdata/spec (W0502 fixture), docs/decisions,
docs/spec.md, docs/open-questions (閉じ).
```

### 3. flat パラメータの部分適用 timing 差 — `devin/wave22-flat-partial`

```
[impl] wave20 lane2 の残存差（decision
2026-11-18-flat-fn-params・spec §1.3 に記録済み）: flat 宣言
パラメータを持つ関数の部分適用 `f a` は、eval（canonical
closure）では直ちに発火するが生成コードでは residual 到着
まで発火を遅延する。**確定済み（podhmo, wave22 prompts
作成時）: eval 側に residual partial を移植して divergence
を消す** — 生成コードの発火タイミングを正として eval を
合わせる方向。eval 側の residual 適用を遅延させるか、
canonical 表現側を揃えるかの機構は実装時の設計（decision
draft → sketch 記録手順どおり）。移植が不成立なら発火
タイミング差が観測可能なユーザー影響かを sketch に整理して
podhmo に持ち帰る（ divergence を残す判断はここではしない）。
`TestEagerFnChunkTiming` が現行挙動をピン留めしている —
揃えた方向に更新する。spec §1.3 の注記を解消する。
Ownership: internal/eval (canonical eval), internal/lower
(curryEta/Uncurry), testdata or eval tests, docs/spec.md,
docs/decisions, docs/sketch (検証).
```

### 4. `TypeArgs` の (b) 移行判定 — `devin/wave22-ctor-targs-b`

```
[investigate] wave21 lane1 は (a) optional-but-authoritative
を採択し、(b) `TypeArgs` を廃止して `Typ` の `Con.Args` を
唯一の正にする案は future work（open-questions 記録済み）。
この lane は移行の判定: lower が `TypeArgs` を emit する全
経路と codegen の `ctorTArgs` 参照を棚卸しし、撤去の規模
（フィールド除去・呼び出し規約への影響・flat params との
相互作用）と利得を sketch にまとめる。結果「移行する/しない」
の最終判断は sketch を受けて podhmo が決める（この lane で
実装まで進めない — prompts ルールの実行時分岐を避けるため
調査だけに切る）。調査結果次第で open-questions の該当項は
「判定済み（採用/不採用）」に更新するか、不採用なら閉じる。
Ownership: internal/lower (TypeArgs emit 経路),
internal/codegen (ctorTArgs), internal/core (TypeArgs
フィールド), docs/sketch (調査結果),
docs/open-questions (更新).
```

## Post-lane work（lane 全完了後 — 締め作業）

- `docs/sketch/2026-11-*-wave22-round.md` の round 記録
- `skills/gom-agent-guide` の同期（ユーザー可視分があれば）
- wave23 prompts 草案（採択が要る点は作成時に podhmo に聞く）
- 台帳掃除（直列サブタスクとして必要分だけ）

## Lane candidates left on the shelf

- **LSP shared importer + member table 共有** — wave19 経路
  設計済み。per-change 数ms削減の価値に対し LSP server 配線の
  変更規模が見合わない判断（podhmo, wave22 prompts 作成時で
  shelf 継続）。
- **LSP completion/definition/references** — 大規模・専用
  wave 向き。
