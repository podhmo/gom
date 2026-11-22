# Wave 31 orchestration prompt drafts

State as of 2026-11-22: wave30 の 5 lane（#112–#117、SC-2 脱出
検査 / SC-3 native defer / `@method` Go iface impl / `--strict`
設計 / defined-type 境界調査）は全て PR 化済み、Devin Review
反復・podhmo マージ待ち（stack #115）。締め作業下書きは #118。

このファイルは wave31 の prompts 草案。テーマは podhmo 指定で
**implementation gaps（仕様・実装・文書のずれ台帳）の解消**。
台帳の未解消は `Go.conv` の 1 件のみなので、残りは「ずれを
体系的に狩る監査 lane + 見つかった小粒の解消」の構成。
lane 順は下からチェーンする前提。採否は prompts 確定時の
インタビューで決める（`docs/ja/wave-process.md` の流儀）。

## Lanes

### A. `Go.conv` の defined-type 変換 — `devin/wave31-goconv-defined`

```
[impl] 台帳の未解消項目: `Go.conv` が単一フィールド record
（`type UserId = { value: Int }` → Go defined type）と要素型の
相互変換を E0507 で拒否している。Go では `int64(u)` /
`UserId(7)` が合法なので、convOK の変換規則に defined-type
unwrap/wrap の経路を足すか調査・実装。named Go 型の既存 conv
規則（litRange・underlying kind）との一貫性が論点 — 許すなら
spec §8.5 に追記、許さないなら decision で棄却理由を記録して
台帳を [x] にする。小粒 lane。
Ownership: internal/check, internal/gomimport, docs/spec.md,
docs/implementation-gaps.md, testdata/spec, decision.
依存: なし
```

### B. `docs/problems/` 未決・一部解消ファイルの棚卸し — `devin/wave31-problems-sweep`

```
[sweep] problems/ の open 状態ファイルを現行実装で再検証し、
解消済みは 状態: 解消 + 検証リンクに更新、残存は台帳へ
`- [ ]` エントリ化、小粒はその場で修正する。対象:
- 2026-11-02-imported-record-fields（未決）— `p.x`・ctor は
  解決済みだが未注釈リテラル `{x = ..}` が import record に
  対し "no record type has fields" のまま（2026-11-22 検証済）。
  lane C と分割: こちらは台帳化と問題文の正確化まで
- 2026-11-02-gom-missing-label-is-partial（未決）— E0404 が
  Gom 呼出しに発火しない件。spec 例の修正 or note 案の採否を
  decision draft まで
- 2026-11-02-variadic-arg-mapping（未決）— CallInfo.ArgToParam
  の nil 多重化（identity vs unrepresentable）。構造改善 or
  現状記述の明文化を判断
- 2026-11-03-overapply-labeled-params（一部解消）— 残件は
  E0401 wording。E0404 拡張の是非は missing-label と同根なので
  セットで扱う
- 2026-11-05-ptr-t-no-deref（一部解消）— `Ptr` メンバー表 /
  `Go.deref` / errors.As 契約の現状棚卸し
- 2026-11-05-slow-tests（対応中）— 状態コメントの更新のみ
Ownership: docs/problems, docs/implementation-gaps.md,
docs/decisions（必要時）, internal/check（検証用）。
依存: なし
```

### C. imported record の未注釈リテラル解決 — `devin/wave31-imported-record-literal`

```
[impl/investigate] lane B の検証で確認済みの live gap: 未注釈
record リテラル `{ x = 1.0, y = 2.0 }` が import 経由の record
型に解決されない（`recordByFields` がファイルローカルの
宣言しか見ない）。フィールド選択・ctor 呼出しは動くので残る
はリテラルとパターンの解決。設計論点は「注釈なしで import
先のどの record を候補にするか」— import した module の
record 全て？ 使用される import だけ？ 曖昧一致（同名
フィールドの record が複数）時の診断も要る。小さいなら impl、
設計が重いなら decision draft + 台帳で切り分け。
Ownership: internal/check (typenv, expr, pat),
internal/gomimport, docs/spec.md, testdata/spec, decision.
依存: lane B（problems 文書の正確化と連携）
```

### D. 診断コード ↔ spec fixture カバレッジ sweep — `devin/wave31-diag-coverage`

```
[sweep] diag/codes.go の全コードに対し testdata/spec の fixture
有無を機械的に突合（現状 81 コード中 30 件に fixture 無し —
2026-11-22 時点）。E9xxx（eval 内部 panic）・E0001/E0002
（字句）のような fixture を書けない/書かないコードは除外理由を
error-codes.md か台帳に記録し、書けるものは fixture を足す。
診断仕様（spec §の記述）と実際の発火条件のずれが出たら台帳へ。
Ownership: testdata/spec, docs/error-codes.md,
docs/implementation-gaps.md, internal/diag.
依存: なし
```

### E. eval ↔ codegen divergence registry の監査 — `devin/wave31-divergence-audit`

```
[sweep] internal/build の knownDivergences / knownBugs 登録を
全件再検証: 直っているのに登録が残るものは退役、依然ずれる
ものは台帳と内容が一致しているか照合（台帳未記載なら追記）。
registry の cap（24）と TODO.md 連動も点検。新規の divergence
を fuzz で掘る lane ではなく「登録済みの棚卸し」に限定。
Ownership: internal/build, docs/implementation-gaps.md, TODO.md.
依存: なし
```

## 採択待ちの問い

- lane C は impl（候補探索の規則まで決めて実装）か、
  investigate（decision draft まで）か — 曖昧性ルールの設計が
  重いなら後者
- 監査 lane で見つかった「小粒じゃないずれ」は台帳に残して
  wave32 送りでよいか（基本その方針）
- 従来案の `--strict` 実装・`|?>` 文言・scope 拡張・rune 境界は
  wave32 以降に繰り越しでよいか（`--strict` は採択済みなので
  優先度次第でこの wave に戻す選択もあり）
