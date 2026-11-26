# wave51 prompts

- 日付: 2026-11-27
- 状態: 草案（未採否）
- 入力: [wave50 round](2026-11-27-wave50-round.md)、[TODO](../../TODO.md)、[open-questions](../open-questions.md)、[implementation-gaps](../implementation-gaps.md)

wave50 は診断 coverage の土台、diagnostic `labels` / `details` wire、match narrowing decision、record literal 文脈推論まで進んだ。wave51 は wave50 で実装まで確定した残件を先に処理しつつ、延期した module 外実行/初期化と、型推論の残りの設計論点を扱う候補にする。

## lane 候補

### A. module 外 `gom run` / `gom init`（中）

wave50 から延期。go.mod のない directory では scratch module を合成して `gom run` し、`gom init` は `go.mod` / `main.gom` 雛形を生成する。scratch module の寿命、cache、既存ファイル非上書き、module-aware な import 解決を decision/fixture で固定する。run/init の両方を入れるかは採否時に確認する。

### B. same-scrutinee root constructor narrowing 実装（中）

wave50 lane D の decision を実装する。`internal/pattern` に same-scrutinee root constructor narrowing を入れ、selfish/DMMF/positive fixture を red-first で green にする。guard boundary fixture は E0301 のまま残す。contextual E0302 移行や guard 条件解析は入れない。

### C. unit-only 診断 coverage 再分類と meta-test 強化（小〜中）

wave50 lane A の残件。unit-only / no explicit coverage を意味確認し、spec/CLI fixture 追加、意図的 reserved、または meta-test 対象外の明示へ落とす。E0204/E0503/E0704 の方向が未決なら、この lane では分類可能な範囲に限定する。

### D. record pipe の二重 mismatch 診断抑制（小）

pipe operand の record 適合失敗と `pipeCall` の同じ mismatch が二度出るのを抑制する。失敗済み情報の受け渡し方を決め、二重診断 fixture を red-first で直す。正しいコードの推論規則は変えない。

### E. labeled lambda seeding / structural record の設計（調査 + decision、中）

wave50 lane E の残り。labeled expected signature の positional lambda parameter に record 型を seed するか、匿名 record 型または structural record を導入するかを調査し decision まで行う。実装は wave52 以降。label introduction 全体への影響と、ad hoc accumulator の需要を selfish/DMMF probe で確認する。

推薦順: B → D → C → A → E。B/D/C は wave50 の直接残件で小さく、A はユーザー向け、E は設計論点なので最後に置く。A/E の同時採用は重い場合があり、採否時にどちらかを落とす選択肢を確認する。

## 採否を確認したい点

1. 推薦 5 lane を採用するか。落とす lane があれば指定する
2. A は run/init の両方か、run だけに絞るか
3. B/C/D は wave51 で実装してよいか。C は E0204/E0503/E0704 の方向が未決のまま進めてよいか
4. E は調査 lane に入れるか、需要が集まるまで保留するか
5. 診断系の open questions（expected type provenance、details 昇格基準、snippet 統合）を独立 lane にする必要があるか

## 見送り候補（少なくとも現時点では lane にしない）

- checker の状態分割、rt 表示一元化、benchmark 基盤 — 有効な内部改善だが、wave51 は wave50 残件とユーザー向け A を優先
- E0201 typo 候補、error recovery 拡大、継続行誤パース診断 — wave50 lane C の wire 結果を使う設計として別途絞る
- stdlib `str` 拡張、Option traverse、panic capture adapter — 需要確認が弱い
- `gom eval` / REPL、`gom search`、goimport export data 共有、`gom fix` の `.gomi` 検査 — 設計または需要確認が大きい
- `gom install`、Go 定数セット、cgo/unsafe/reflect、セルフホスト — future work 維持
