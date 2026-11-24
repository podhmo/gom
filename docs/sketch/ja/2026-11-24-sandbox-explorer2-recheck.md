> 転記元: [PR #196](https://github.com/podhmo/gom/pull/196) の `sandbox/recheck.md`（sandbox 探索 run 2）

# 前回探索（PR #163）の指摘の解消確認メモ — 2026-11-24 run

確認方法: `sandbox/recheck/<name>/main.gom` に各項目の最小 probe を置き、
`/tmp/gom check`/`gom run` で実検証。jsonl2csv・freq などのツール側での
実地利用も併記する。

## 解消済み（前回「不得意」「危険」「諦め」だったもの）

| 前回の指摘 | 現状 | 検証 |
| --- | --- | --- |
| ビット演算なし（`O_APPEND\|O_CREATE` が書けない） | **解消**。`&&& &&^ \|\|\| ^^^ <<< >>>` が実装済み。`os.OpenFile` のフラグ合成が `os.O_APPEND \|\|\| os.O_CREATE \|\|\| os.O_WRONLY` で素直に書けた | recheck/bitops — ファイル出力と `6&&&3=2` 等を確認 |
| stdlib List が薄い（take/drop/sort/sum Int なし） | **解消**。`take`/`drop`/`last`/`last_n`/`sort_by`/`sum_int`/`range` 等が揃い、実ツール内で普通に使えた | csvcut の `List.drop`、tabl の `List.range`、freq の `List.take`+`sort_by` |
| `@method` で `ServeHTTP` 等の initialism を出せない（`ServeHttp` 固定） | **解消**。`@method(ServeHTTP)` verbatim 引数で record が `http.Handler` を満たす（`let h : http.Handler = rec` が型付く） | recheck/method |
| `//go:embed` / blank import が原理的に不可 | **解消**。`//gom:embed hello.txt` + `@extern let greeting : String` で埋め込みが動いた。`import go _ "path"` もある | recheck/embed |
| json tag が付けられず `{"Name":...}` になる | **解消**。record フィールド末尾の raw string が struct tag に: `{name: String `+"`json:\"name\"`"+`}` → `{"name":"taro","age":30}` | recheck/jsontag |
| `h.Sum(nil)` が書けない（nil の綴りがない） | **概ね解消**。`Go.nil ()` がユニバーサル nil。注意点: `h.Sum` の param は `List Byte` 面なので `h.Sum (Go.nil ())` は E0513 — `let b : Go.Slice Byte = Go.nil ()` に束縛して渡す。`[]` の空リストでも通るので実用上の不自由は小さい | recheck/gonil — empty sha256 が正値 |
| `slices.SortFunc` に record のスライスを渡せない（E0507） | **部分的に解消**。`~[]E` 型引数の要素一致で `List Entry`（gom record）を `slices.MinFunc`/`BinarySearchFunc` 等に直接渡せるようになった。**ただし in-place 変更系（SortFunc 等）は List→[]T コピーなので効果なし** — W0501 が正しく警告する。実ソートは `List.sort_by` が正解（`Go.Slice` の gom 側コンストラクタは依然なく、`Go.conv List→Go.Slice` も gom 要素型では E0507、`Array` も `~[]E` を満たさず E0401） | recheck/sortfunc |
| `Go.Any` の中身を検査できない（型アサーション相当なし） | **解消**。`match v { s : String -> ...; n : Float -> ...; _ -> ... }` の型テスト腕（Go type-switch emit）が `Go.Any`/named interface に効く。jsonl2csv で実地利用 | sandbox/jsonl2csv |
| 部分適用の静かな罠（`ignore` で関数値を捨てても無警告） | **概ね解消**。`ignore (add 1)` で W0901。**残る罠**: `fmt.Println` 等の `any` 引数に関数値を渡す形は依然無警告で `0x4abde0` 表示になる | recheck/partapp |
| record フィールド初期化は `Ptr.new`+field 代入のみ | （変化なし）composite literal は依然非提供だが Ptr field 代入で十分だった、は前回の結論どおり | — |

## 新たに見つかった摩擦点・ずれ（tracker 登録候補）

- **`gom run` / `gom build` が gom の `import` を推移的に build しない**: `gom run sandbox/shop` は domain.go を emit せず `go build` 失敗（E0001）。`gom build dirA dirB` なら層順に全 emit するが、`run` は単一 DIR 限定（"inputs must belong to a single package directory"）。依存先のビルドは利用者の責任 — Makefile 等が要る。**ずれか未実装か**。
- **匿名 func 値の二重アダプタ**: `bufio.ScanWords` は import 面でカリー化 Gom fn に見えるが、`sc.Split` が要求する `bufio.SplitFunc` は `Go.Func2` 生ビュー → `Go.func2 bufio.ScanWords` と往復変換が必要。直感的でない。
- **`text/tabwriter` は uint 引数で全滅**: `NewWriter`/`Init` の `flags uint` が E0501。uint 系が非対応なので tabwriter 相当は自分でパディングするしかない。
- **`gom run` が `fmt.Println f`（func 値 → `any`）をすり抜けさせる**: 部分適用 lint（W0901）の対象外で、旧来の「静かな罠」がこの形だけ残っている。
- **`strings` の非対称**: `strings.Join`/`Split` はあるが文字列の `len`/部分列/添字がない。文字集合→ `strings.Split(s,"")`+`List.get` という遠回しになる。
- **1 ディレクトリ 1 パッケージ**（前回から継続の制約）: probes を並べるとき dir 単位が必須。
- **int64 → `Int` の写像**: `big.NewInt`（int64 引数）に `Go.conv x : Int64` を渡すと E0401 — Go の `int64` は gom では `Int`（`Int64` は `long` 相当ではない）。名前の直感に反する。
- **match 腕は `,` 区切り不可**: `{ Ok v -> v, Err _ -> 16 }` は E0101。JSON 風に書きたくなるが改行必須。
- **record literal の曖昧さ**: フィールド名が共通する record を複数立てる（DMMF の型遷移パターンだと自然に発生）と literal ごとに E0407 → `let v : T` 注釈が要る。
- **`$2_` の置換名は Go 仕様**: `${2}` braces が要る（gom 側の問題ではないがドキュメント価値あり）。

## 依然残る既知の制約（確認せず、tracker 記載のまま）

- 1 フィールド record の `~` 制約は依然拒否（implementation-gaps 未解消のまま）
- `Go.conv` で `List T → named slice`（`sort.IntSlice` 等）は check をすり抜けて go build 落ち、という前回指摘は今回未検証
- unsafe/reflect/cgo、E0506 の generics 制約、`iota` 連番 — 前回同様のはず
