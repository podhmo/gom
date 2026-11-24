# sandbox log — gom 探索記録（run 2）

ストリーミング形式。上から時系列。発生したエラー・不足機能・迂回・諦めを雑に残す。
うまくいったもの（特に sum types / match）も記録する。

前提:

- gom バイナリ: `go build -o /tmp/gom ./cmd/gom`（リポジトリルートで）
- 各ツールは `sandbox/<name>/main.gom` に置く。`gom run sandbox/<name>/main.gom -- args` で試す
- 前回 run: `docs/sketch/ja/2026-11-23-sandbox-explorer-log.md`（PR #163）

## 2026-11-24

### 事前読み込みメモ（再確認不要な罠 / 解消済みか検証したい項目）

前回ログ + tracker を読んだ要約:

**今回再確認しなくてよい罠**（前回確定済み、言語側は変わっていないはず）:

- tuple 適用 `f(1,2)` は Gom fn に使えない / Go 呼び出しはカンマ区切り必須
- `let in`・`else if`・`::`・`xs[i]`・`$`・`!` なし
- record literal `=` vs record type `:`
- `type X = T` は variant 宣言（alias は `type alias`）
- nested nullary pattern は括弧要（`Ok (Help)`）
- 改行は式末尾になりうる位置で文を区切る
- `&&`/`||` は短絡しない（評価順が必要なら `if`）
- `import "x"` vs `import go "x"`
- List の Go 表現は `list.List[T]`（`[]T` ではない）

**解消済みか検証したい項目**（前回の不得意/危険 → wave35–40 で実装が入ったもの）:

- [ ] ビット演算なし → `&&& ||| ^^^ <<< >>> &&^` 実装済みと TODO に記載。`os.OpenFile` の `O_APPEND|O_CREATE` 相当を検証
- [ ] stdlib List が薄い → `take`/`drop`/`last`/`last_n`/`sort_by`/`sum_int` 追加済み
- [ ] `@method` で `ServeHTTP` 出せない → `@method(ServeHTTP)` verbatim 名。record で `http.Handler` 実装を検証
- [ ] `//go:embed` / blank import 不可 → `//gom:embed` + `import go _ "x"` + `@extern let`
- [ ] json tag 付けられない → record field の raw-string tag 構文 `{name: String `json:"name"`}`
- [ ] `h.Sum(nil)` 問題 → `Go.nil` ユニバーサル綴り
- [ ] record を `[]MyRec` として `slices.SortFunc` に渡せない → `~` 制約の要素一致（wave38 lane D）
- [ ] `Go.Any` を検査できない → match 型テスト腕 `p : T`（wave40 lane B）
- [ ] 部分適用の静かな罠 → W0901 lint（wave40 lane A）

### ideas

`ideas.md` に 8 件メモ。テーマは前回と被らないよう
CSV・日時・ネットワーク・集計・リネーム・表出力・乱数・JSONL。
予想: csvcut/until/freq = 簡単、pscan/rmv/tabl/passgen = 中、jsonl2csv = 難しめ（`Go.Any` 検査が本題 — ちょうど型テスト腕の検証になる）。

### csvcut (encoding/csv 列選択) — 成功

- `let rec = fn ... -> Result ... = match` は lambda 本体が `=` を取らず型名が値位置に落ちて E0201 連発。再帰はトップレベル `fn` に分ける（`let` 束縛のラムダは自己参照できないので当たり前だが、型注釈付き多引数ラムダを書きたくなる衝動はある）。
- `csv.Reader.Read ()` は `Result (List String) GoError`。EOF は `errors.Is (e, io.EOF)` — package-level var `io.EOF` がそのまま GoError として読める。
- `List.drop`/`List.length`/`Option.get` — 新 stdlib が素直に効く。`List.get i row` の引数順は前回と同じく `(n, xs)`。
- Go の `r.FieldsPerRecord = -1` 相当の Ptr field 代入は今回使わなかった（デフォルトで同列数チェックが効く挙動のまま）。
- `w.Flush ()` + `w.Error ()` の2段も普通に通る。実出力は `cut` と一致（ヘッダ・stdin 両方確認）。

### until (日付カウントダウン) — ほぼ一発成功

- `time.Parse (time.DateOnly, d)` — package-level const `time.DateOnly` がそのまま読める。`Result time.Time GoError` を `try` で剥がして素直。
- `target.Sub (now).Hours ()` のメソッドチェーンも普通。named 型 `Duration` の変換は不要だった。
- Float→Int は `Go.conv (math.Ceil days) : Int` で。show の手間なし。
- ロジックの罠（gom とは関係ない）: 当日は target=0:00 < now で微妙に負 → `days >= 0.0` だと "0 days ago" になる。Ceil して整数化してから分岐で解決。
- 複数引数を `for d in args` で回して各々の失敗を握り潰さず表示 — 前回の s256 と同じ集計パターンは書きやすい。

### pscan (TCP ポートスキャナ) — 成功

- `net.DialTimeout` → `Result (Option net.Conn) GoError`。**interface 型は Option 包み**という前回の学びがそのまま当たった（最初 `conn.Close ()` で E0401、`Ok (Some conn)` に修正）。
- `Go.conv (200 * 1000000) : time.Duration` — named 数値型変換は前回通り。
- lambda の引数はパターン不可: `fn (_, t) ->` は書けないので `fn pt -> match pt { (_, t) -> ... }` で内部展開。やや回りくどいが慣れる。
- `List.range lo hi` でポート列、`List.map` で `(p, Task)` のタプルリスト、`Task.spawn`/`Task.await` で fan-out。localhost 1-1024 を 200ms timeout で ~0.1s（直列なら数分）。**並行 fan-out は前回に続き得意領域**。
- `net.JoinHostPort` + `strconv.Itoa` — Go の慣用句そのまま。

### freq (単語頻度) — 成功（1箇所学びあり）

- `bufio.ScanWords` は **匿名 func 値**なので import 面ではカリー化 Gom fn（`List Byte -> Bool -> Result (Int, List Byte) GoError`）として見える。`sc.Split` は named `bufio.SplitFunc`（`Go.Func2` 生ビュー）を要求するため **そのまま渡すと E0401、`Go.func2 bufio.ScanWords` で再度ラップ**して通した。「Go func 値 → Gom fn → Go func」の二重アダプタが必要な場面。
- `Go.Map.make`/`get`/`set`/`to_list` は素直。`to_list` のタプルを `List.sort_by` で降順ソート — 新 stdlib の sort_by が効いた（前回は自前マージソートだったのが不要に）。
- `let rec = fn step -> ... step ...` の自己呼び出し風は不可（再掲: 再帰はトップレベル `fn`）。

### rmv (regexp バッチリネーム) — 成功

- `regexp.Compile` → `Result (Ptr regexp.Regexp) GoError`、`re.ReplaceAllString (path, repl)` は素直。
- **`$2_$1` の罠（gom ではなく Go 仕様）**: 置換文字列の `$2_` はグループ名 `2_` と解釈され空に化ける。`${2}_${1}` と braces で区切る必要。dry-run 出力で即発見できたのは dry-run default の設計が生きた。
- `-x` flag で実行モード、`os.Rename` も素直。
- `match e.Error () { msg -> ... }` の無駄な match を書いてしまった（`msg` 変数を取りたいだけなら `let` でよい — でも `e.Error ()` は値なので `let msg = e.Error ()` でよかった）。小さいが改善余地。

### tabl (テーブル成形) — 成功だが tabwriter は断念

- **`text/tabwriter` は原理的に使えない**: `NewWriter`/`Init` の `flags uint` 引数が非対応で E0501（"parameter flags: uint is not supported yet"）。unsigned int は byte/uint8 以外 hidden。**回避**: 全行を読んで列ごとの最大幅を `utf8.RuneCountInString` で測り `strings.Repeat` で手動パディング。日本語も rune count でほぼ揃う（全角幅までは runewidth 相当が要るが外部 pkg なしではここまで）。
- **Go 関数はカリー化しない（再確認）**: `x |> strings.Join "  "` は `strings.Join " "` の単引数呼出＋残適用になって E0401。Go 呼び出しは常に `f(a, b)` のタプル1発。パイプで繋ぐなら `|> fn cells -> strings.Join (cells, " ")`。
- `Option.get_or_else`（`get_or` は存在しない — 名前の勘はエラーで即気づく）。
- `let` をラムダ本体に書くには `{ }` ブロック必須（`fn i -> { let x = ...; e }`）。裸 `fn i -> let` は E0101。

### passgen (crypto/rand パスワード) — 成功

- `crypto/rand.Int` + `math/big` — `rand.Reader` var、`big.NewInt`、`bi.Int64 ()` 全部素直。`int64` パラメータは gom の `Int` に対応（`Go.conv x : Int64` は逆に E0401 — **int64→Int の写像に注意**、`Int64` は別物）。
- 文字列の部分列/`len`/`s[i]` がないので `strings.Split (alphabet, "")` で `List String` にしてから `List.get` で拾う形に。文字列を文字の集合として扱う道筋が少し遠い。
- `math/rand` ではなく `crypto/rand` — Go の作法そのまま書けた。
- ネスト match を1行 `{ Ok v -> v, Err _ -> 16 }` と書いたら腕の区切りカンマで E0101 — match 腕は `,` でなく改行区切り。

### jsonl2csv (JSONL→CSV) — 成功。型テスト腕の実地検証にもなった

- `json.Unmarshal (sc.Bytes (), Ptr.new ())` — out-pointer decode は前回通り安定。
- **`Go.Any` の match 型テスト腕 `p : T` が実戦で効いた**: `match v { s : String -> ...; n : Float -> ...; b : Bool -> ...; _ -> ... }` — 前回「`Go.Any` は検査できない」が解消。JSON number は float64 として `Float` 腕に来る。
- JSON `null` は `any` の nil → `_` 腕で `%v` → `<nil>` と出る（Go 仕様）。
- キーの登場順序保持は `Go.Map.to_list`（順序不定）では取れないので seen-map + fold で自前に。`Go.Map` の反復面が「順序不定スナップショット」なのは仕様通りだが、CSV ヘッダの安定出力にはこの一手間が要った。

### recheck probes — 前回指摘の解消確認（詳細は recheck.md）

- bitops: `os.O_APPEND ||| os.O_CREATE ||| os.O_WRONLY` がそのまま書けた。`&&& ^^^ <<< >>>` も正常。
- method: `@method(ServeHTTP)` で `let h : http.Handler = rec` が通る — record の Go interface 実装が可能に。
- embed: `//gom:embed hello.txt` + `@extern let` で内容が読めた。
- jsontag: `{name: String `+"`json:\"name\"`"+`}` で `{"name":...}` が出る。
- gonil: `let b : Go.Slice Byte = Go.nil ()` で `h.Sum b` — sha256 of empty が正しい値。**ただし `h.Sum (Go.nil ())` は E0513**（Sum の param は `List Byte` 面で nil-able でない）— 一旦 `Go.Slice` の注釈付き変数が要る。
- sortfunc: `slices.MinFunc`/`BinarySearchFunc` に `List Entry`（gom record）+ `Go.func2` が通るようになった（`~[]E` の要素一致）。**ただし in-place 変更系 `slices.SortFunc` はコピーなので W0501 警告の通り無意味** — ソートは `List.sort_by` を使うのが正解。
- typetest: jsonl2csv で `match v { s : String -> ...; n : Float -> ...; b : Bool -> ...; _ -> ... }` が `Go.Any` に効いた（実地検証済み）。
- partapp: `ignore (add 1)` で W0901 が出る。**ただし `fmt.Println` の `any` 引数に関数値を渡す形は依然警告なしで `0x...` 表示** — 罠の面積は減ったが残っている。

### shop/ (パッケージ分割 + DMMF 風ドメイン) — 成功

- **パッケージ分割は動く**: 非 main パッケージは `package domain` + `.gomi` が必要。`gom info -o dir/domain.gomi dir` でドラフト生成 → 不要な helper を削ると公開面が絞れる（`.gomi` が visibility の正）。
- `import "github.com/podhmo/gom/sandbox/shop/domain"` で main から参照。`domain.Widget c` のように修飾コンストラクタが match で使える。
- **`gom run DIR` は単一パッケージ限定**（"inputs must belong to a single package directory"）。依存パッケージは `gom build dirA dirB` で層順に全 emit してから `go run ./dir` — `gom run` が gom の import を推移的に build しないのは小さな摩擦点。
- record literal の曖昧さ: 同じフィールド名を持つ record が複数あると E0407（"N record types own these fields"）。`let v : T = {...}` で注釈して解決。DMMF 的に「段階ごとの record」を並べる設計だと名前がかぶりやすいので頻出。
- **DMMF の型遷移は gom の直撃領域**: `UnvalidatedOrder -> ValidatedOrder -> PricedOrder` を別型で持ち、`place_order = validate |?> price` で合成。`PlaceOrderError` sum (Validation/Pricing) を利用側が match で網羅処理 — コンパイラが抜け漏れを教えてくれる。Widget=個数/Gizmo=kg の数量型分岐も `to_quantity` が `ProductCode` を match して決める形で素直に書けた。
- 整数判定は `Go.conv qty : Int` + 逆変換で往復一致を見た（`n == qty` だと型が違って E0401、`(Go.conv n : Float)` の括弧が要る — if 条件の中の ascription は括弧必須）。

## まとめ: gom の領域地図（run 2 の観測 — 前回からの差分付き）

### 得意領域（追認・拡大）

- **CLI ツール全般**（前回どおり。csvcut/until/freq/rmv はほぼ素直）
- **sum types + 再帰 match**（前回どおり。DMMF 的な「段階ごとの型」ワークフローは本領発揮 — Unvalidated→Validated→Priced + `|?>` 合成が素直）
- **並行 fan-out**（前回どおり。pscan で 1024 ポートを ~0.1s）
- **マルチパッケージ構成**（新規・良好）: `.gomi` ゲート付きの domain パッケージが成立し、`gom info` のドラフト生成が速い。`import "<module path>"` でそのまま解決
- **`Go.Map`/`Go.Slice` の Go 直結操作**（新規）: 頻度集計・`slices.*Func` 呼び出しが素直
- **JSON/CSV 系の変換**（新規）: `encoding/csv`・`json.Unmarshal` + out-pointer は安定。型テスト腕で `Go.Any` 中身を触れるようになったので RawMessage 戦略以外の道が増えた

### 不得意領域（今回の観測）

- **`uint` 系の非対応**が壁になる API: `text/tabwriter`（`flags uint` で全滅）。代替は手書きパディング
- **匿名 func 値の往復変換**: `Go.funcN`/`Go.fnN` の往復が必要な場面がたまに出る（bufio.ScanWords → Split）
- **文字列操作の薄さ**: len・部分列・添字がない → `strings.Split`+`List.get` の迂回
- **record literal の曖昧さ**: フィールド名がかぶる型を並べると毎回注釈が要る（DMMF 的設計で頻出しがち）
- **`gom run` の単一パッケージ限定**: 依存パッケージは `gom build` で先に emit が必要
- **`Go.Map.to_list` の順序不定**: 順序が意味を持つ集計（CSV ヘッダ）は seen-map + fold で自前に

### 危険領域（縮小 — 前回の項目が多数消えた）

- **残る**: unsigned int 非対応 API 全般（uint/uint16/uint32/uint64 は E0501）、`Go.conv` の check→build 抜け穴（named slice 等）、`fmt.Println` 等 `any` 位置への func 値（無警告のまま）、`Ptr` nil の自由 panic、unsafe/reflect/cgo
- **消えた**: ビット演算なし・stdlib List 薄さ・initialism メソッド不可・embed 不可・json tag 不可・nil 綴りなし・`Go.Any` 検査不可・`slices.*Func` に record 渡せない（in-place 系を除く）

## 所感

前回の「不得意・危険」は半分以上が wave35–40 の実装で解消していて、とくに `@method(GoName)`・`//gom:embed`・struct tag・型テスト腕は探索で普通に使えるレベル。新しく見つかった壁は「uint 非対応 API」「Go func 値の往復変換」「依存パッケージの非推移 build」あたり — どれもワークアラウンドがあるが、利用側が踏むポイントとして残す価値がある。

## 追記: Devin Review と記事監査（2026-11-24 後半）

- Devin Review の指摘を検証してツール群を修正（上書き防止・終了コード・scanner エラー伝播・
  json.Number 保持・TSV・range 上端・ローカル日付比較・MkdirTemp など）。各コミット参照。
- moonbit-sum-types 記事の指針で全コードを監査したメモは `sandbox/sumtypes.md`。
  大きな変更は出ず、言語側の穴として「`.gomi` がコンストラクタ非公開を表現できない
  （ValidatedOrder を place_order 経由に強制できない）」を確認。
