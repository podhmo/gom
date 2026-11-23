# wave37 prompts

- 状態: 採択済み（2026-11-23 インタビュー — 候補 12 件中
  B/C/D/F/G/J の 6 件採択。A/E/H/I/K/L は延期）
- 日付: 2026-11-23
- 前提: wave36 スタック（#155→#162）全件マージ済みの新しい
  main から開始。引き続き単一エージェント直列運用、Devin
  Review が PR ごとの品質ゲート
- 開始条件: **wave36 マージ後に開始**（インタビュー＝prompts
  作成のみ。lane 実行はまだ行わない）
- 候補の出典: PR #164（sandbox 探索の整理 — TODO +7、
  open-questions +6、
  [sketch](docs/sketch/2026-11-23-sandbox-missing-features.md)）
  と wave36 lane G の decision 残件

## レーン（採択済み、旧候補レターを括弧に記す）

### A. `gom run` のシグナル転送 + `rt.MakeChan[os.Signal]` vet 誤検知（旧 B、bug 2 件）

- 出典: TODO [bug] ×2 — `gom run` が SIGINT を子プロセスに
  フォワードしない（`go run` と挙動差）。`rt.MakeChan` 経由の
  buffered chan 生成が vet の "misuse of unbuffered os.Signal
  channel" を踏む
- 作業: 親→子のシグナル転送（最低限親終了時の子 kill）と、
  生成コードの形（`make(chan T, n)` 直接 emit 等）または
  vet 対象からの除外。同一ランタイム/toolchain 面なので束ねる
- 規模: 小

### B. ビット演算子の提供形（旧 C、調査→decision）

- 出典: open-questions — `|`/`&`/`^`/shift がなく
  `os.O_APPEND|os.O_CREATE` を `+` で代用するしかない。
  `<<`/`>>` は関数合成に使用済み（
  [problem](docs/problems/2026-11-01-operator-coverage.md)、
  sandbox 探索 jot で再観測）
- 作業: 提供形の調査（中置記号の追加 / builtin 関数 / 構文）→
  decision。採択されれば実装まで
- 規模: 中（調査 + 実装なら parser/codegen）

### C. nil の綴り（旧 D、調査→decision）

- 出典: open-questions — `Ptr.nil` しか nil を書く面がなく
  `Go.Slice`/`Go.Map`/Go 関数型の nil を作れない
- 作業: nil 表現の提供形（`Go.nil` 的な面 / 型付き nil リテラル /
  各コンストラクタ）を調査 → decision
- 規模: 小〜中

### D. `Go.Slice` 要素に Gom 宣言型（旧 F、E0507 緩和、調査→decision）

- 出典: open-questions — record が `[]MyRec` として
  `slices.SortFunc` 等に渡せない。`List T`→`[]T` の境界
  コピーの意味論と一体の設計
- 注意: 延期した `Go.conv` List→named slice バグ（旧候補 A）
  と同じ境界面。本 lane の decision はそのバグの拒否/変換
  方針にも材料になるので、decision に触れておく
- 規模: 中

### E. `//go:embed` / directive comment / blank import の提供形（旧 G、decision→実装）

- 出典: TODO [task] — `import _ "embed"` が E0101、directive
  コメントを書く面もなく `embed.FS` が原理的に使えない
- 作業: 属性・directive・blank import 構文のどれで供給するか
  decision → 実装。`//gom:` 系 directive の機構は既存
- 規模: 中

### F. stdlib `List` の補強（旧 J）

- 出典: TODO [task] — `take`/`drop`/`last`/`sort`/`sum`（Int）
  がなく実アプリで自前実装が要る。`List.sum` は Float 専用
- 作業: 不足関数の追加（stdlib + rt）。範囲は lane 内で
  sketch の観測セット（`last_n`・ソート等）を基準に確定
- 規模: 小〜中

## 延期した候補（次 wave 以降へ繰越）

- `Go.conv` List → named Go slice の check/emit ギャップ
  （旧 A、bug）— lane D の decision と同じ境界面なので、その
  decision を材料に次 wave で取り上げる
- `Go.Any` の検査面（旧 E、調査→decision）
- record フィールドへの Go struct tag 宣言面（旧 H、
  decision→実装）
- `@method` emit 名の escape — initialism 対応（旧 I、
  decision→実装）
- 部分適用・`Unit` 残り関数値の lint（旧 K、調査→decision）
- `//gom:mutable` directive（`.gomi`）の実装（旧 L、
  wave36 lane G 採択済み・priority low）

## 候補から外したもの

- `iota` 流の Go 定数セット宣言 — Future work 区分。
  sum type/`let` 定数で代替可能
- `gom fix` で `.gomi` 適合検査 — 2026-11-05 に見送り決定済み
  （E0601/E0603/E0604 に safe fix がなく観測差ゼロ）
- D-3 README・D-4 go.mod 整理 — 終盤作業として明示的に後回し
- List ハイブリッド表現・chan Send 制限・並列化ルール・
  `|?>` 文言カスタマイズ — wave サイズに収まらない
