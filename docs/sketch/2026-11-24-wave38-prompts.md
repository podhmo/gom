# wave38 prompts

- 状態: 草案（2026-11-24 インタビュー待ち）
- 日付: 2026-11-24
- 前提: wave37 スタック（#166→#171）全件マージ済みの新しい
  main から開始。引き続き単一エージェント直列運用、Devin
  Review が PR ごとの品質ゲート
- 開始条件: **wave37 マージ後に開始**（インタビュー＝prompts
  作成のみ。lane 実行はまだ行わない）
- 候補の出典: wave37 延期分（[prompts](2026-11-23-wave37-prompts.md)
  の繰越）+ `gom list`（podhmo 要望 2026-11-24）+ wave37 lane
  B/C/D の decision draft の採択確認

## レーン候補

### A. `gom list` サブコマンド（stdlib 列挙、実装）

- 出典: TODO [task]（podhmo 要望 2026-11-24）— import 可能な
  stdlib パッケージの短名を列挙するツールがない。
  `.gomi` は `gomstdlib.Gomis` に `go:embed` でバイナリ埋め込み
  済みなので単体で列挙可能
- 作業: 引数なしの `gom list` — 埋め込み FS を walk して
  `list`/`option`/`result` の短名を列挙するのみの一機能
- 規模: 小

### B. `Go.conv` List → named Go slice の check/emit ギャップ（bug）

- 出典: wave37 旧候補 A（延期）— `Go.conv xs : Go.Slice T` の
  named slice 宛てが check/emit で不整合。wave37 lane D の
  decision（`~` 制約の要素一致を Gom 側 `types.Equal` で見る案）
  が同じ境界面の材料 — lane D の採否で方針が変わる
- 作業: lane D decision の採択結果に応じて、拒否を明示するか
  変換を実装するか
- 規模: 小〜中

### C. `Go.Any` の検査面（調査→decision）

- 出典: wave37 旧候補 E（延期）— `Go.Any` を受け取る Go 関数
  （`fmt.Println` 等）への値渡しの検査面が未定義
- 作業: 何を許すか・どう検査するか調査 → decision
- 規模: 中

### D. record フィールドへの Go struct tag 宣言面（decision→実装）

- 出典: TODO [task]（wave37 旧候補 H、延期）— フィールド名は
  lowercase 強制・Go 側は PascalCase emit で `json:"name"` 等を
  付けられず `json.Marshal` が外部スキーマと合わない
- 作業: 属性引数かフィールド行 directive の形を decision → 実装
- 規模: 中

### E. `@method` emit 名の escape — initialism 対応（decision→実装）

- 出典: TODO [task]（wave37 旧候補 I、延期）— `serve_http`→
  `ServeHttp` しか出せず `ServeHTTP`/`URL`/`ID` を含むメソッド名を
  生成できず `http.Handler` 等を record に実装できない（E0401）
- 作業: 属性引数か verbatim 綴り escape の形を decision → 実装
- 規模: 小〜中

### F. 部分適用・`Unit` 残り関数値の lint（調査→decision）

- 出典: wave37 旧候補 K（延期）— `f x` の部分適用が残ると
  `f` の意図しない再利用になる恐れ（未呼出カリー）
- 作業: 警告対象の境界を調査 → decision
- 規模: 小〜中

### G. `//gom:mutable` directive（`.gomi`）の実装

- 出典: TODO [task]（wave36 lane G 採択済み・priority low、
  wave37 旧候補 L）— extern 裏付け・abstract export 型を
  `.gomi` directive で mutable 表明。name-only 形なので専用
  スキャン + ロード時 tyCons フラグ + `containsMutable` 照合 +
  `.gomi` 再生成時の directive 引き継ぎ
- 規模: 中

### H. wave37 lane B decision（bit 演算子）の実装 ※採択時のみ

- 出典: [decision draft](../decisions/2026-11-23-bit-operators.md)
  — 三重字 `&&&`/`|||`/`^^^`/`<<<`/`>>>`（+`&&^`）推薦。
  採択なら lexer+check+codegen の実装 lane
- 規模: 中〜大（採択時のみ lane 化）

### I. wave37 lane C decision（`Go.nil`）の実装 ※採択時のみ

- 出典: [decision draft](../decisions/2026-11-23-nil-spelling.md)
  （`Go.nil` ユニバーサル builtin 推薦）。採択なら Go 名前空間の
  拡張実装 lane
- 規模: 中（採択時のみ lane 化）

### J. wave37 lane D decision（`Go.Slice` 要素一致）の実装 ※採択時のみ

- 出典: [decision draft](../decisions/2026-11-23-goslice-gom-elem.md)
  （`~` 制約の要素一致を Gom 側 `types.Equal` で見る案推薦）。
  採択なら check の制約一致の実装 lane（候補 B の `Go.conv` gap
  方針とも連動）
- 規模: 中（採択時のみ lane 化）

## 候補から外したもの（引き続き繰越）

- List ハイブリッド表現・chan Send 制限・並列化ルール・
  `|?>` 文言カスタマイズ — wave サイズに収まらない
- interp-composite-display — wave37 でも繰越
- `iota` 流 Go 定数セット — Future work 区分
- `gom fix` で `.gomi` 適合検査 — 見送り決定済み
- D-3 README・D-4 go.mod 整理 — 終盤作業として後回し
