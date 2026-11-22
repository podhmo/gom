# wave33 prompts

- 日付: 2026-11-22
- 状態: **採択**（2026-11-23 インタビュー）— 5 lane 全部入れる。B は調査結果によっては実装まで行く（ユーザー回答: "全部入れるが、Bは調査結果によっては実装"）

## テーマ

wave32 で残った scope 関連の端部と、棚卸し済みの調査・拡張候補。

## lanes（候補）

### A. `.gomi` の `let` 束縛ラムダへの `//gom:noescape` 書き出し

- 出典: wave32 lane C の残件（decision 非目標・制限の更新部）
- 作業: `noescapeLine` が `*check.FnDecl` しか見ないため、
  `let f = fn cl -> Scope.defer s cl` 型の公開 helper の flag が
  `.gomi` に出ず import 側では保守的 escaping のまま。LetDecl の
  Value が Lambda なら `let f : T` 行にも `//gom:noescape` を付け、
  reader/loader 側も let メンバ名を引けるようにする。`let` の
  メンバ形式と directive の結び付き（名前ベース）は既存の `fn` と
  同じ読みでよいか確認
- 規模: 小〜中

### B. `gom fmt` が `.gomi` の `//gom:` ディレクティブを失い得る問題

- 出典: decisions/2026-11-22-scope-escape-check.md 非目標・制限
- 作業: 調査 — fmt が `.gomi` のコメント（`//gom:` ディレクティブ
  行を含む）をどう扱うか実測し、消失するなら保持する修正か
  「fmt 非対象」注記のどちらにするか decision まで
- 規模: 小（調査）〜中（修正までなら）

### C. 結果経由の Scope リーク（non-escaping 引数の戻り値がクロージャを運ぶ）

- 出典: docs/implementation-gaps.md 唯一の未解消項目
- 作業: 調査 — `run_cb (f: Int -> (Unit -> Unit))` に
  `fn x -> fn () -> Scope.defer s ...` を渡すと `s` が結果経由で
  出ても E0419 が出ない。追跡に戻り値の taint 伝播が要る — 実現
  可能な近似（callback 結果に関数型を含む non-escaping 引数への
  scope 値の引き渡しを flag など）を検討し decision まで
- 規模: 中（調査）。実装までなら大

### D. `|?>` 文言カスタマイズ

- 出典: open-questions future work（残る未決は文言のカスタマイズのみ）
- 作業: 調査 → decision。パッケージ単位の書式指定か、ステップごとの
  文言構文か。`step: message` 規約と `rt.StepError` の枠は採用済み
- 規模: 小（調査）

### E. 実装が `.gomi` より多相の許容

- 出典: open-questions 未決事項（型システム）
- 作業: 調査 → decision。実装が多相・`.gomi` が単相の場合に
  許すか。既存の conformance 検査（E0603）への影響範囲を測る
- 規模: 小（調査）

## 繰り越し注記

- lane A は wave32 lane C の残件の直接の続き — B・C は同じ
  scope/escape 系なので A と同 wave にまとめると文脈が共有できて効率が
  よい。D・E は独立
