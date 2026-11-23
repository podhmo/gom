# wave38 prompts

- 状態: 採択済み（2026-11-24 インタビュー — 候補 10 件中
  A/B/C/E/H/I/J の 7 件採択。D/F/G は延期）
- 日付: 2026-11-24
- 前提: wave37 スタック（#166→#171）全件マージ済みの新しい
  main から開始。引き続き単一エージェント直列運用、Devin
  Review が PR ごとの品質ゲート
- 開始条件: **wave37 マージ後に開始**（インタビュー＝prompts
  作成のみ。lane 実行はまだ行わない）
- 候補の出典: wave37 延期分（[prompts](2026-11-23-wave37-prompts.md)
  の繰越）+ `gom list`（podhmo 要望 2026-11-24）+ wave37 lane
  B/C/D の decision draft 採択

## レーン（採択済み、旧候補レターを括弧に記す）

### A. `gom list` サブコマンド（stdlib 列挙、旧 A、実装）

- 出典: TODO [task]（podhmo 要望 2026-11-24）— import 可能な
  stdlib パッケージの短名を列挙するツールがない。
  `.gomi` は `gomstdlib.Gomis` に `go:embed` でバイナリ埋め込み
  済みなので単体で列挙可能
- 作業: 引数なしの `gom list` — 埋め込み FS を walk して
  `list`/`option`/`result` の短名を列挙するのみの一機能
- 規模: 小

### B. ビット演算子の実装（旧 H、wave37 lane B decision 採択）

- 出典: [decision](../decisions/2026-11-23-bit-operators.md) —
  三重字 `&&&`/`|||`/`^^^`/`<<<`/`>>>`（+`&&^`）を採択
- 作業: lexer + check + codegen + spec の実装。decision の
  状態を実装完了で更新
- 規模: 中〜大

### C. `Go.nil` ユニバーサル builtin の実装（旧 I、wave37 lane C decision 採択）

- 出典: [decision](../decisions/2026-11-23-nil-spelling.md) —
  `Go.nil` を採択（`Go.Slice`/`Go.Map`/Go 関数型等の nil を
  `Ptr.nil` 以外でも書ける面）
- 作業: Go 名前空間の拡張 + spec
- 規模: 中

### D. `Go.Slice` 要素の `~` 制約一致の実装（旧 J、wave37 lane D decision 採択）

- 出典: [decision](../decisions/2026-11-23-goslice-gom-elem.md) —
  `~` 制約の要素一致を Gom 側 `types.Equal` で見る案を採択
  （record を `[]MyRec` として `slices.SortFunc` 等に渡せる面）
- 作業: check の制約一致の実装。後続 lane E の `Go.conv` gap
  方針とも連動
- 規模: 中

### E. `Go.conv` List → named Go slice の check/emit ギャップ（旧 B、bug）

- 出典: wave37 旧候補 A（延期）— named slice 宛て
  `Go.conv xs : Go.Slice T` が check/emit で不整合
- 作業: lane D の `~` 制約一致機構に乗せて、変換を実装するか
  拒否を明示するか decision 結果に従って修正
- 規模: 小〜中

### F. `Go.Any` の検査面（旧 C、調査→decision）

- 出典: wave37 旧候補 E（延期）— `fmt.Println` 等 `any` を
  受け取る Go 関数への値渡しの検査面が未定義（`List` を渡すと
  内部表現が Go 側に見える等）
- 作業: 許容範囲と検査方法の調査 → decision
- 規模: 中

### G. `@method` emit 名の escape — initialism 対応（旧 E、decision→実装）

- 出典: TODO [task]（wave37 旧候補 I、延期）— `serve_http`→
  `ServeHttp` しか出せず `ServeHTTP`/`URL`/`ID` を含む
  メソッド名を生成できず `http.Handler` 等を record に実装
  できない（E0401）
- 作業: 属性引数か verbatim 綴り escape の形を decision → 実装
- 規模: 小〜中

## 延期した候補（次 wave 以降へ繰越）

- record フィールドへの Go struct tag 宣言面（旧 D）
- 部分適用・`Unit` 残り関数値の lint（旧 F、調査→decision）
- `//gom:mutable` directive（`.gomi`）の実装（旧 G、
  wave36 lane G 採択済み・priority low）

## 候補から外したもの（引き続き繰越）

- List ハイブリッド表現・chan Send 制限・並列化ルール・
  `|?>` 文言カスタマイズ — wave サイズに収まらない
- interp-composite-display — wave37 でも繰越
- `iota` 流 Go 定数セット — Future work 区分
- `gom fix` で `.gomi` 適合検査 — 見送り決定済み
- D-3 README・D-4 go.mod 整理 — 終盤作業として後回し
