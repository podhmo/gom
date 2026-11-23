# wave39 lane 候補（採否インタビュー用・草案）

- 日付: 2026-11-24
- 状態: 草案 — 採否インタビュー後に確定
- 前提: wave38 スタック（#173–#179）マージ後に lane 実行を開始する
- 候補の出典: wave38 延期分（[prompts](2026-11-24-wave38-prompts.md)
  の繰越）+ wave38 lane F decision（採択時の実装）+ TODO/open-questions

## 候補

### A. `Go.try_as` — `Go.Any` 取り出し面の実装（decision→実装）

- 出典: wave38 lane F の decision（draft —
  [decision](../decisions/2026-11-24-go-any-extraction.md)）—
  comma-ok の `Option` 返りを唯一の面とする案
- 作業: 採択なら `Go.try_as x : Option T` を実装（check で
  `x : Go.Any` + 目標 `T` 検査、rt generic helper 経由の emit、
  eval 側の内部断言、spec 8.3 追記 + fixture）
- 規模: 中

### B. record フィールドへの Go struct tag 宣言面（decision→実装）

- 出典: TODO [task]（wave37 旧候補 D、wave38 延期）—
  `json:"name"` 等の tag を宣言できず `json.Marshal` 出力が外部
  スキーマと合わない（`{{.name}}` テンプレ参照も同根）
- 作業: 属性引数かフィールド行 directive の形を decision → 実装
- 規模: 中

### C. 部分適用・`Unit` 残り関数値の lint（調査→decision）

- 出典: TODO/open-questions（wave37 旧候補 F、wave38 延期）—
  `f x` の部分適用で残った関数値が意図せず再利用される懸念
- 作業: 警告対象の境界（エラー/警告どちらにするか）の調査 → decision
- 規模: 小〜中

### D. `//gom:mutable` directive（`.gomi`）実装

- 出典: TODO [task] — wave36 lane G 採択済み・priority low
- 作業: extern 裏付け・abstract export の宣言型を `.gomi` の
  directive で mutable 表明（専用スキャン + tyCons フラグ +
  `.gomi` 再生成時の引き継ぎ）
- 規模: 中

### E. `Go.Any` への `match` 型テスト腕（調査→decision）

- 出典: wave38 lane F decision の棄却案 — 多型ディスパッチ需要が
  あれば開いた型への網羅性設計を調査
- 作業: default 腕必須化の意味論設計 → decision（実装は wave40 以降）
- 規模: 中〜大

## 延期した候補（次 wave 以降へ繰越）

- List ハイブリッド表現・chan Send 制限・並列化ルール・
  `|?>` 文言カスタマイズ — wave サイズに収まらない
- interp-composite-display — 繰越
- `iota` 流 Go 定数セット — Future work 区分
- `gom fix` で `.gomi` 適合検査 — 見送り決定済み
- D-3 README・D-4 go.mod 整理 — 終盤作業として後回し
