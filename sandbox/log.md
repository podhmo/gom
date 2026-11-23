# sandbox log — gom 探索記録

ストリーミング形式。上から時系列。発生したエラー・不足機能・迂回・諦めを雑に残す。
うまくいったもの（特に sum types / match）も記録する。

前提:

- gom バイナリ: `go build -o /tmp/gom ./cmd/gom`（リポジトリルートで）
- 各ツールは `sandbox/<name>/main.gom` に置く。`gom run sandbox/<name>/main.gom -- args` か `gom build` + `go run ./sandbox/<name>` で試す
- 参照: `examples/rgrep` が行指向ツールの雛形として最も近い

## 2026-11-23

### ideas

`ideas.md` に Go の普段使いツール 8 件をメモ。gom のことは考えずに列列挙した。
難易度予想: wcl/b64/pick = 簡単、jqp/jot/s256 = 中、duu/hs = 難しい。

### wcl (wc クローン) — 成功

試行 1: E0101 — `Ok HelpA ->` は nested nullary pattern。`Ok (HelpA)` に修正（memory の syntax trap 通り）。
試行 2: `let words = List.sum` の次行で `(...)` — 式が改行で切れて `List.sum` 単体が束縛され、次行が mid-block `List Int` で E0402。**改行ルール: 式末尾になりうる行で改行すると文が区切られる**。関数適用を複数行に割るなら最後の引数行で終わるように書くかパイプを使う。
試行 3: `List.sum` は `List Float -> Float` しかない（Int 版なし）。`List.fold` で代替。
結果: stdin `-l file` ともに `wc` と一致。args は `Go.Slice.to_list os.Args` でリスト化、flag 処理は match + 蓄積再帰で素直に書けた。

### b64 (base64) — 一発成功

- `base64.StdEncoding.DecodeString` は `Result (List Byte) GoError` として見える。`try` でそのまま剥がせた。
- `Go.conv` は `List Byte` ↔ `String` 双方向に効いた（encode 出力の `String` → `os.Stdout.Write` 用に `List Byte` へ）。
- `os.Stdout.Write out` — Go メソッド呼び出し、`List Byte` が `[]byte` に境界コピー。違和感なし。

### pick (簡易 grep) — 成功

- 自分で `where { fn loop ... }` 構文を捏造してエラー。gom に関数内ネスト `fn` はない（調べずに書いた）。トップレベル `parse_loop` で解決。
- それ以外は素直。`if hit != o.invert` で -v 反転、`for` ではなく蓄積+再帰の代わりに `match` 再帰で行番号付き走査。
- `fmt.Printf "%d:%s\n" n line` — variadic は素直に通る。
- パターン: `match` + `Result Opts String` による自前引数パースは rgrep 例と同じで書きやすい。**CLI引数パースは sum types + match が得意領域**。

### jqp (jq もどき) — 成功

- ハマり: `Go.Any` → 具象型の取り出しは `Go.conv` 不可（spec 8.x、Go の型アサーション相当）。`json.Unmarshal` を `Ptr (Go.Map String json.RawMessage)` / `Ptr (List json.RawMessage)` に毎段かける方式で迂回。RawMessage なら出力も生 JSON そのままで自然。
- `List.get` の引数順は `(n: Int) (xs: List a)` — 値が最後（パイプ設計）。Go の `xs[i]` 感覚で書いて E0401。エラーメッセージで即修正。
- `Go.conv raw : Go.Slice Byte`（named → slice）と `Go.conv v : String`（RawMessage → string）どちらも通った。
- `Ptr.new ()` + 型注釈での out-pointer decode は gist-cli 例と同じで安定。
- **sum types 快適ポイント**: `type Step = | Field String | Index Int` を `match` で walk。網羅チェックが走るので arm 漏れで死なない。
- 引数は `[path]` / `[path, file]` のリストパターンで直接ディスパッチ — これも sum/match が効いている。

### jot (タイムスタンプ付きメモ) — 成功

- **ビット演算子がない**: `os.O_APPEND | os.O_CREATE | os.O_WRONLY` が書けない。フラグは互いに素なビットなので `+` で合成して迂回。動くが気持ち悪い — **不得意領域候補（ビットフラグ合成）**。Go の `os.OpenFile` 慣用句と表面がずれる。
- `List.drop`/`take` は stdlib にない → 自前の `take` + `rev` で `last_n`。
- `fmt.Fprintf` の `(int, error)` → `Result Int GoError`。`ignore (try ...)` で捨てられる — `try` と `ignore` の合成は素直。
- `os.Getenv` は `String`（Go では `string`）— `""` との match で分岐。Option 化はしない設計らしい（Go のまま）。
- `time.Now ()` → `now.Format time.RFC3339`、Go メソッド呼び出しは素直。
- ファイル一覧表示に `for line in shown { ... }` — Unit ループは楽。

### s256 (sha256sum) — ほぼ一発成功

- `h.Sum(nil)` が書けないので `Go.Slice.nil ()` を捏造 → 存在しない。`h.Sum []` で空リストが `[]byte` へ境界コピーされて解決。
- `io.Copy (h, f)` — hash.Hash (io.Writer) と *os.File (io.Reader) の interface 適合が普通に通る。
- `%x` に `List Byte` をそのまま渡せた。
- 複数ファイルで1つ失敗しても続行する形にした（結果コードを集計して最後に exit）— `try` で抜けない設計も match で書きやすい。

### duu 試行1 — 複数エラーから学び

- `os.DirEntry.Info()` は `Result (Option fs.FileInfo) GoError`。**interface 型の Go 値は nil-able として Option に包まれる**（os.Stat も同じ形、rgrep 例がそうだった）。中身の取り出しは `Ok i` のさらに内側で `Some i` を match。
- `Go.conv xs : Go.Slice Entry` → E0507。**宣言した record/sum 型は Go の要素型になれない**（"no Go representation"）。`slices.SortFunc` に Gom record のスライスは渡せない。E0510/E0508 も同根。
- → ソートは gom 側でマージソートを書く方針に変更。

### duu (du もどき) — 成功（ソートは迂回）

- `e.Info ()` は `Ok (Some i)` / `Ok (None)` の二段 match。
- ソート: Go の `slices.SortFunc` は Gom record を要素にできず断念。マージソートを再帰で実装 — `[x, y, ..rest]` の複数要素パターンがそのまま書けるのは快適。
- `Go.conv n : Float` で Int→Float 変換して %.1f フォーマット。
- 再帰のディレクトリ走査（`os.ReadDir` + `filepath.Join` + `e.IsDir ()`）は素直。
- 警告の蓄積を `{ size, warns }` レコードで持ち回る形も素直。

### hs (HTTP ステータスチェッカ) — 成功

- `http.Head` は `Result (Ptr http.Response) GoError` — **Ptr は Option に包まれない**（interface 型だけが Option 化のようだ）。最初 `Ok (Some res)` と書いて E0401。Go の nil は来るのに包まれないのは少し怖い（nil deref は panic）。
- `Task.spawn (fn () -> probe u)` + `Task.await` で並行フェッチがそのまま書けた。**並行の単純な fan-out は得意領域**。
- `time.Since`/`Milliseconds` など named 型メソッドも素直。
- ローカル http.server で検証。エラー経路（DNS 失敗）も `err: Option String` に載せて FAIL 表示できた。

### calc (式評価器) — 成功。sum types の見せ場

- 再帰 ADT `type Expr = | Num Float | Add Expr Expr | ...` が普通に宣言できる。
- **網羅チェックの体験が良い**: `Div` をわざと落として `gom check` → E0301、JSON だと `missing_patterns: ["Div _ _"]` と placeholder fix (`Div _ _ -> todo ()`) が出る。`gom fix` は placeholder 適用をスキップした（safe のみ適用のよう）→ 手で追記。
- パーサは `(Expr, List String)` を返す再帰下降 + `try`/`Result _ String`。`try` は GoError 専用でなく任意のエラー型で効く。
- 文字列補間 `"${show e} = ${eval e}"` に関数適用が書けた。
- **得意領域の確定: AST + match + try による小さな言語系処理は最も書きやすい領域**。

### srv (HTTP サーバ) — 意外なくらい成功

- `mux.HandleFunc ("/", Go.func2 handler)` — Gom のカリー化 fn `http.ResponseWriter -> Ptr http.Request -> Unit` が `Go.func2` で `func(w, r)` に変換されてそのまま渡った。
- `http.NewServeMux ()` の `*ServeMux` は `http.Handler` を満たすので `ListenAndServe (addr, mux)` にそのまま渡せる — interface 適合の判定が効いている。
- variadic 呼び出し `fmt.Fprintf (w, "fmt", a, b)` は**カンマ区切り必須**。空白区切りだと全体が1つの式としてパースされて `String -> ?1` みたいな変な型エラーになる（習慣で `f(a, b)` が Go 呼び出しの形というスキル記載通り）。
- `r.Method`/`r.URL.Path` のフィールド参照、`(r.URL.Query ()).Get "q"` のメソッドチェーンも普通。
- curl で method/path/query が正しく返るのを確認。**HTTP サーバ系は「書ける」領域**（危険と予想していたが）。

### runner (タイムアウト付き実行) — 成功

- `context.WithTimeout` が `(Context, context.CancelFunc)` を返し、CancelFunc は `Go.Func0` ビュー → `(Go.fn0 cancel) ()` で呼ぶ（`Go.fn0 cancel ()` は「引数を2つ取る」で E0510 — `(Go.fn0 f) ()` の形）。
- `exec.CommandContext (ctx, name, ..rest)` — variadic 展開 `..rest` がそのまま効いた。
- **バグりやすい点**: `ctx.Err()` を `cancel` 前に取らないと Canceled が原因を上書きする（gom ではなく自分のロジックミス）。
- `Go.conv sec : time.Duration` で Int → named 数値型の変換が通る。

### 小規模 probe（json.Marshal / flag / regexp）— 全部通る

- `json.Marshal u`（gom record）→ `{"Name":"taro","Age":30}`。**フィールド名は PascalCase で、json tag を付けられないので小文字キーにできない** — 外部APIとやり取りする JSON は `Go.Map` か RawMessage 経由にせざるを得ない。これは実用上の注意点（jsoncfg/gist-cli が RawMessage+Map を使っているのと一致）。
- `flag.Bool ("v", false, "verbose")` + `Ptr.get` — flag パッケージも普通に動く。
- `regexp.Compile` → `Result (Ptr regexp.Regexp) GoError`、`re.MatchString s` も素直。
- 文字列の `+` 連結はない（`+` は数値のみ）。`"..." + s` で E0401 → `"${s}"` 補間が正しい。

### probe: select + signal + ticker (probes/sigw) — 成功

- `Chan.make 1` に `let sig : Chan os.Signal` の注釈が必要（`Chan.make os.Signal` は型を値にした E0201）。
- `signal.Notify` の variadic は `..[os.Interrupt]`。`syscall.SIGINT` は named `syscall.Signal` で `os.Signal`(interface) にそのままは渡せない → `os.Interrupt` 変数を使う（Go でも同じ作法）。W0501 で in-place 変更の警告が出るが Notify は変更しないので無害。
- `select { _ <- sig -> ...; _ <- ticker -> ... }` で tick ループ、SIGINT で抜けられた。
- **1ディレクトリ1パッケージ**: probes/ に main を2つ置くと `go build` が `main redeclared`。tool ごとにサブディレクトリ必須（当たり前だが `gom run FILE` でも兄弟の .go を巻き込むので注意）。
- 補足: `gom run` の temp binary に kill する時は親の gom プロセスではなく `gom-run-*/out` 子を殺す（シグナルはフォワードされない）。

### tpl (text/template) — 成功

- `template.ParseFiles` → `Result (Ptr template.Template) GoError`、`t.Execute (os.Stdout, data)` もそのまま。
- データは `json.Unmarshal` を `Ptr (Go.Map String Go.Any)` に → `{{.name}}`/`{{range .items}}` が普通に動く。**テンプレ描画は書ける領域**。
- ただし gom record 直渡しは json tag 問題と同じで PascalCase フィールド参照（`{{.Name}}`）になるはず — map 経由が実用的。

### probe: @method + fmt.Stringer / sort.IntSlice（probes/method）

- `@method fn (i: Id) string () -> String` — `let st : fmt.Stringer = i` で interface 適合し、`fmt.Println st` が `id:42`（String() 経由）を出す。**record → Go interface 適合は動く**。
- 罠: gom 側の呼び出しは `string i` では部分適用（Unit パラメータが残る）で `func` 値が `any` にボックスされて `0x4abf80` と印字された。正しくは `string i ()`。**部分適用が静かに通るのは gom 全体で最大の静かな罠**。
- `Go.conv xs : sort.IntSlice` は gom check を通るが生成 Go が `list.List[int64]` への変換で go build 失敗（E0001 で表面化はする）。**List の Go 表現は `[]T` ではなく `list.List[T]` — 名前付き Go slice 型への Go.conv は check をすり抜けて壊れる**可能性あり（implementation gap 候補）。

### probes: 共有 Ref / ジェネリクス / Unmarshal / @method

- `go {}` で `Ref` を捕捉 → W0702 警告だがコンパイル・実行とも通る（lint は警告止まり — 共有可変性はユーザーの責任）。
- ジェネリック関数 `fn id (x: a) -> a` はそのまま。tuple パターン引数は `fn swap ((a, b): (a, b))` と `pattern : type` 形で書く（`((a: a, b: b))` は E0101）。
- `json.Unmarshal` → `Ptr User`（gom record）も通る。**ただしフィールド名は lowercase 強制**で Go 側は PascalCase に emit — JSON キーも PascalCase 必須。スネークケースの JSON とは相性が悪い（map 経由で逃げる）。
- Option の Go 出力は `{41}` っぽく見える（`fmt.Println` 時）— Sum/Option の表示は素の構造体表現。

### probes: slices.Sort / @method ServeHTTP / embed — 境界の輪郭が見えた

- `slices.Sort xs`（List 引数）→ **W0501 警告**: List は `[]T` にコピーされて渡るので in-place sort が効かない、と静的に教えてくれる。誤ったコードを書いてもコンパイラが理由付きで指摘 — 良い体験。
- `@method fn (h: H) serve_http ...` → `H` は `http.Handler` を満たさず E0401。原因: メソッド名の emit 規則は `serve_http` → `ServeHttp`。**`ServeHTTP`/`URL`/`ID` など Go の initialism を含むメソッド名は @method では生成できない** → record で `http.Handler`・`sort.Interface`(Sort) 等の interface 実装は不可能。**回避**: `Go.conv (Go.func2 h) : http.HandlerFunc` で named func 型にキャストするか、素直に `mux.HandleFunc`。
- `import _ "embed"` は E0101 — **blank import 構文自体がない**。`//go:embed` ディレクティブも書けないので embed.FS の利用は原理的に不可能（回避: `os.ReadFile` で実行時読み込み）。

### probe: gom test

- `test "name" { ... }` + `assert_eq` が同じファイルに書けて `gom test FILE` で go test 経由実行。JSON レポート付き。

### 諦めたもの（第二の財産: 状況とコード）

**A. `sort.IntSlice` / `slices` 系で record をソート**
- 目的: 外部データを Go の高速ソートに渡したかった。
- 壁: (1) `Go.conv xs : sort.IntSlice` は gom check を通るが、生成コードが `list.List[int64]` → `sort.IntSlice` の変換で go build 失敗（check vs emit のずれ、implementation gap 候補）。(2) Gom 宣言型は `Go.Slice` の要素にできないので `slices.SortFunc` の `~[]E` を満たせない。(3) `slices.Sort` は List 境界コピーで無意味（W0501 が教えてくれる）。
- 結論: **gom 側でマージソートを書くのが現実解**（duu で採用）。コードは sandbox/duu/main.gom の split/merge_desc/sort_desc。

**B. record に `http.Handler` を実装させる**
- 目的: `type H = { ... }` に `ServeHTTP` を生やして `ListenAndServe` に渡す。
- 壁: `@method` の emit 名規則は `serve_http` → `ServeHttp` で、`ServeHTTP` の initialism は生成できない → interface 適合しない (E0401)。
- 回避: `Go.conv (Go.func2 handler) : http.HandlerFunc` または `mux.HandleFunc`（実動作確認済み）。**@method が実装できない interface: initialism を含む名前を要求するもの全般**（sort.Interface 等）。

**C. `//go:embed` / blank import**
- `import _ "embed"` は構文エラー（E0101）。directive comment も書けない。embed.FS は原理的に不可。回避は `os.ReadFile`。

## まとめ: gom の領域地図（今回の観測）

### 得意領域（すらすら書ける・成功率が高い）

- **CLI ツール全般**: 引数パース（List pattern + match + Result _ String）、stdin/ファイル読み、行処理、出力フォーマット。wcl/b64/pick/jot/s256 はほぼ一発で書けた。
- **sum types + 再帰 match**: パーサ・AST・状態機械（calc の Expr）。E0301 の missing_patterns が具体的。
- **逐次 I/O とエラー処理**: `try`/`|?>`/`Result`、interface 適合（io.Reader/Writer へ *os.File 等）、out-pointer decode (`Ptr.new`)。
- **単純な並行 fan-out**: `Task.spawn`/`Task.await`、chan+select、signal.Notify、timer。
- **HTTP**: クライアント (Head/Get) とサーバ (ServeMux+HandleFunc) 両方。

### 不得意領域（書けるが迂回路が要る・つまずきやすい）

- **ビット演算なし**: `|`/`&`/`<<`/`>>` が存在しない（`<<`/`>>` は合成演算子）。フラグ合成は `+` で代用するしかない。
- **List ↔ Go slice の境界**: List は `list.List[T]` で `[]T` ではない。named slice 型への `Go.conv` が check をすり抜けて build 落ちすることがある。`slices.Sort` 等の in-place API は根本的に向かない。
- **JSON フィールド名**: gom record フィールドは lowercase 強制 → Go 側 PascalCase → json tag が付けられず `{"Name":...}`。外部 JSON スキーマとは RawMessage/Go.Map 経由で。
- **`Go.Any` の検査**: 型アサーション相当がないので `any` をデコードしたものは触れない（reflect か RawMessage 戦略で逃げる）。
- **部分適用の静かな罠**: labeled 引数の省略、`Unit` 残りの関数値を `fmt.Println` に渡す等はエラーにならず `0x...` 表示になる。
- **stdlib List が薄い**: `take`/`drop`/`sort`/`sum(Int)` がない。自前で書く。
- **Go メソッド値/初期化慣用句**: `ctx.Err()` 評価タイミングなど Go 脳の手続き的な順序付けは自分で考える必要（言語のせいではないが慣れが要る）。

### 危険領域（明らかに無理 or 本質的に厳しい）

- **`//go:embed` / directive comment 全般**: 書けない。
- **initialism を含む Go interface の @method 実装**: `ServeHTTP` 等 → `ServeHttp` になる。
- **check 通るが go build で落ちる抜け穴**: `Go.conv` List→named slice 等。E0001 で表面化はするが gom 単体では検知しきれない。
- **unsafe/reflect/cgo/unsafe.Pointer**: 非表現型は hidden（E0501）。
- **generics の種類制約にメソッドを持つ Go generic fn**: E0506。
- **`iota` 連番や定数宣言の Go 慣用**: sum type/定数 let で代替になるが Go API の定数セットを gom 側から作る話は不可。

## 所感

「日曜プログラマの CLI ツール」領域はかなり広くカバーされている。詰まるのはほぼ Go 境界の慣用句（named slice への変換、initialism メソッド名、embed/ディレクティブ）で、言語の核（ADT/match/try/currying）は破綻がない。エラーメッセージは具体的で、スキルの「ありがちな間違い」に載っている罠（tuple 適用、nullary pattern の括弧、改行区切り、record literal の `=`）を踏むと即座に E0101/E0301/E0401 が返り、学習コストは低い。

### 補足: 生成 .go と repo lint

- `go vet ./...` が生成コードを vet 対象にする。sigw の `rt.MakeChan[os.Signal](1)` は buffered なのに helper 越しに見えず「misuse of unbuffered os.Signal channel」→ vet false positive（ヘルパ関数経由は vet が透視できない典型例）。生成 .go は成果物としては見せたいが repo の `go vet` を汚すので `.gitignore` で `*.go` を除外してコミットしないことにした。
- `gom fmt` で全ファイル成形済み（emb は構文エラーのまま放置 = 諦め証拠）。
