# wave37 prompts

- 状態: 草案（インタビュー未実施 — lane 一覧は候補）
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

## レーン（候補 — 採否はインタビューで）

### A. `Go.conv` List → named Go slice の check/emit ギャップ（bug）

- 出典: TODO [bug] — `Go.conv xs : sort.IntSlice` が check を
  通り `sort.IntSlice(list.List[int64])` を emit → `go build`
  で落ちる（gaps 台帳登録済み）
- 判断点: **(a) 拒否化** — check の変換許可判定で List の Go
  表現を `[]T` と誤認している箇所を E0507 系の拒否に揃える。
  **(b) 変換 emit** — `list.FromSlice` 相当の helper 経由で
  named slice への変換コードを生成する
- 規模: 小〜中（check + codegen 境界）

### B. `gom run` のシグナル転送 + `rt.MakeChan[os.Signal]` vet 誤検知（bug 2 件）

- 出典: TODO [bug] ×2 — `gom run` が SIGINT を子プロセスに
  フォワードしない（`go run` と挙動差）。`rt.MakeChan` 経由の
  buffered chan 生成が vet の "misuse of unbuffered os.Signal
  channel" を踏む
- 作業: 親→子のシグナル転送（最低限親終了時の子 kill）と、
  生成コードの形（`make(chan T, n)` 直接 emit 等）または
  vet 対象からの除外。同一ランタイム/toolchain 面なので束ねる
- 規模: 小

### C. ビット演算子の提供形（調査→decision）

- 出典: open-questions — `|`/`&`/`^`/shift がなく
  `os.O_APPEND|os.O_CREATE` を `+` で代用するしかない。
  `<<`/`>>` は関数合成に使用済み（
  [problem](docs/problems/2026-11-01-operator-coverage.md)、
  sandbox 探索 jot で再観測）
- 作業: 提供形の調査（中置記号の追加 / builtin 関数 / 構文）→
  decision。採択されれば実装まで
- 規模: 中（調査 + 実装なら parser/codegen）

### D. nil の綴り（調査→decision）

- 出典: open-questions — `Ptr.nil` しか nil を書く面がなく
  `Go.Slice`/`Go.Map`/Go 関数型の nil を作れない
- 作業: nil 表現の提供形（`Go.nil` 的な面 / 型付き nil リテラル /
  各コンストラクタ）を調査 → decision
- 規模: 小〜中

### E. `Go.Any` の検査面（調査→decision）

- 出典: open-questions — `Go.conv` が具象型への取り出しを
  E0507 で拒否する設計のため `json.Unmarshal` で `any` に
  decode した値を触れない
- 判断点: panic 式 / `Option` 返り / `match` 拡張のどれで
  供給するか（回避は `json.RawMessage`/`Go.Map`/reflect）
- 規模: 中

### F. `Go.Slice` 要素に Gom 宣言型（E0507 緩和、調査→decision）

- 出典: open-questions — record が `[]MyRec` として
  `slices.SortFunc` 等に渡せない。`List T`→`[]T` の境界
  コピーの意味論と一体の設計
- 注意: lane A と同じ境界面 — A の採択方針（拒否/変換）が
  本 lane の材料になる。A と前後に置く
- 規模: 中

### G. `//go:embed` / directive comment / blank import の提供形（decision→実装）

- 出典: TODO [task] — `import _ "embed"` が E0101、directive
  コメントを書く面もなく `embed.FS` が原理的に使えない
- 作業: 属性・directive・blank import 構文のどれで供給するか
  decision → 実装。`//gom:` 系 directive の機構は既存
- 規模: 中

### H. record フィールドへの Go struct tag 宣言面（decision→実装）

- 出典: TODO [task] — フィールド名は lowercase 強制・Go 側は
  PascalCase emit で tag を付けられず `json.Marshal` の出力が
  外部スキーマと合わない
- 作業: 属性引数かフィールド行 directive の形を decision →
  実装
- 規模: 中

### I. `@method` emit 名の escape — initialism 対応（decision→実装）

- 出典: TODO [task] — `serve_http`→`ServeHttp` しか出せず
  `ServeHTTP`/`URL`/`ID` を生成できない → `http.Handler`・
  `sort.Interface` 等を record に実装できない（E0401）。
  [decision](docs/decisions/2026-11-21-go-iface-impl.md) が
  予告した分岐
- 作業: 属性引数か verbatim 綴り escape を decision → 実装
- 規模: 中

### J. stdlib `List` の補強

- 出典: TODO [task] — `take`/`drop`/`last`/`sort`/`sum`（Int）
  がなく実アプリで自前実装が要る。`List.sum` は Float 専用
- 作業: 不足関数の追加（stdlib + rt）。範囲は lane 内で
  sketch の観測セット（`last_n`・ソート等）を基準に確定
- 規模: 小〜中

### K. 部分適用・`Unit` 残り関数値の lint（調査→decision）

- 出典: open-questions — 残りパラメータを持つ関数値を
  `fmt.Println` 等に渡すと `0x...` が印字されるだけで
  エラーにならない
- 作業: 警告 lint を切るかの調査 → decision（採択なら実装）
- 規模: 小〜中

### L. `//gom:mutable` directive（`.gomi`）の実装

- 出典: TODO [task] — wave36 lane G で採択済み decision、
  priority low。実装がパッケージ境界の向こうに隠れた型の
  mutable 表明（`.gomi` directive、`containsMutable` 照合）
- 規模: 中

## 候補から外したもの

- `iota` 流の Go 定数セット宣言 — Future work 区分。
  sum type/`let` 定数で代替可能
- `gom fix` で `.gomi` 適合検査 — 2026-11-05 に見送り決定済み
  （E0601/E0603/E0604 に safe fix がなく観測差ゼロ）
- D-3 README・D-4 go.mod 整理 — 終盤作業として明示的に後回し
- List ハイブリッド表現・chan Send 制限・並列化ルール・
  `|?>` 文言カスタマイズ — wave サイズに収まらない
