# examples/gist-cliのリファクタリングで得た知見

- 日付: 2026-11-21
- 対象: ブランチ `devin/*-gist-cli-refactor` の一連のコミットによる `examples/gist-cli` の変更
- 関連: [prototype](../2026-11-03-gist-cli-prototype.md)、
  [wave23 struct 構築](../2026-11-19-wave23-struct-construction.md)、
  [struct-field-write decision](../../decisions/2026-11-19-struct-field-write.md)
- 状態: 調査記録。規範ではない

## 目的

gist-cliは「hostパッケージに寄せる部分」と「Gomに残す部分」という境界で切られて育った。
hostパッケージはGo側のadapterである。hostが消えた今、その切り口には意味がない。
一方で、引数・ファイル・JSON・HTTP・Gistの判断は1本の `run` に接着されていた。これを

1. 全体: すべての操作を1本の木として捉え直し、用途とサイズで切り分ける
2. 局所: 不要になった型注釈や手作りの補助を、今の言語機能でより小さく書く

の順に書き直し、その過程で分かった「今どう書くべきか」「今の限界」「将来ほしいもの」を残す。

## 手順

コミットの単位を古い順に並べる:

1. `integration_test.go` と `build.sh` を、例をディレクトリ単位の1パッケージとしてコンパイルし、
   Gomの `test` ブロックも走らせるハーネスに変更
2. 引数を `Command`/`Options`/`Target` に解析する `args.gom` を分離
3. 操作の木をfiles / gist / transport / jsonの層に分割
4. 以降は局所: clientの内側の型注釈削除と上限値の命名、`Go.Map` + `json.Marshal` による
   body構築、regexpキャプチャによるGist id抽出、`Result.map_err`/`Option.to_result`
   による短縮

各コミットで `gom check` と `go test ./examples/gist-cli` を通した。後者は既存のhttptest
結合テスト全件と、新設したGomの `test` ブロックを走らせる。

## 1. 言語非依存の観点

### 操作の木

元の `run` を木として書き出すとこうなる:

```text
main
├─ 引数を読む            (os.Args → オプション/ファイル列)
├─ 検証する              (Gist URL, API URL, ファイルが 1 つ以上)
├─ トークンを得る         (環境変数)
├─ ファイルを読む         (basename, 重複, UTF-8)
├─ リクエストを組む       (method/route/header/body … Gist の知識)
├─ 送る                  (client policy: timeout, redirect, 上限付き読み取り)
├─ 応答を解釈する         (status, JSON 1 オブジェクト, html_url)
└─ 出力 / 終了コード
```

旧コードでは「検証」が `run` の先頭に、「引数の意味づけ」が `parse_args` の
`cfg.gist != ""` という文字列フラグに、「Gistの知識」が `run` の中の `if creating`
の連鎖に散っていた。葉の性質で分けると次の3種類になる。

| 種類 | 例 | 置き場所 |
| --- | --- | --- |
| 純粋で Gist を知る | route, method, status, body, URL パース | `gist.gom` |
| 純粋で Gist を知らない | 引数文法, 重複検出, JSON の 1 フィールド取り出し | `args.gom` / `files.gom` / `json.gom` |
| 副作用 | ファイル読み取り, HTTP, 終了 | `files.gom` / `transport.gom` / `main.gom` |

`main.gom` の `run` は木の幹だけを持つ6行になり、各枝は単独で `test` できる。

### 判断は型にする

- `gist: String` + `if gist == ""` は「作成か更新か」という2値の判断を、空文字という
  番兵で表していた。`type Target = | Create | Update (id: String)` にすると、
  method・route・期待status・visibilityがすべて `match target` に揃い、
  「更新なのにidがない」状態が作れなくなる。
- `help: Bool` と `literal: Bool` も同様で、どちらも設定ではないので `Options` から外した。
  - `help` は結果の種類なので、`Command = Help | Upload` にした
  - `--` 以降を表す `literal` は解析の状態なので、`parse_options` の分岐にした。
    `["--", ..rest]` の腕が残りをすべてファイルとして `finish` へ渡すので、
    フラグを持ち回る必要がない
- ただし、解析しながら検証すると `--help` の優先順位が変わる。最初の分割では `--gist` の値を
  その場で `parse_gist_url` していたため、`--gist not-a-url --help` がhelpではなく
  エラーになった。これはレビューで指摘された。書かれたままの値を `Raw` record に集め、
  `--help` がもう現れない `finish` で `Options` に変換する2段にした。「生の入力」と
  「検証済みの値」を別の型にすると、どこで検証するかが型の境界として見える。
- `parse_gist_url : String -> Result String String` のように「検証済みの値」を作る関数は、
  boolを返す `valid_gist_url` と後段の `path.Base` の組より安全である。idの取り出しと
  検証を1つの正規表現で行うので、両者がずれない。

### 汎用部分と方針部分を分ける

`transport.gom` は `HttpRequest`/`HttpResponse` という自前の小さなrecordだけを
受け渡し、Gistを知らない。timeout・redirect拒否・2 MiB上限という「このCLIの
transport方針」はここに閉じる。逆に `gist.gom` は `net/http` をimportしない。
こうするとrequestの組み立てをHTTPなしでテストできる。`request_body` のテストがその例である。
transportを差し替える余地も生まれる。

### 失敗は早く、送信前に

重複basenameの検出を「読みながら」から「読む前に全体で」に移した。`load_files` は
ファイルを読む前に `first_duplicate` で検査する。送信前に失敗するという挙動は同じである。
副作用の前に純粋な検査を済ませる形になったので、テストを純粋関数に対して書ける。

## 2. 今のGomでどう書くか

### Goではできなかった/冗長だったがGomで良くなった書き方

- 引数文法を `match` とリストパターンで書く。`os.Args` を `Go.Slice.to_list` で
  `List String` にし、`["--gist", url, ..rest]` のように「オプションとその値」を
  1つのパターンで取る。Goの `for i := 0; i < len(args); i++ { ... args[i+1] ... }`
  にある添字の境界チェックが、`["--api-url"] -> Err "--api-url requires a URL"` という
  網羅的な腕になる。
- regexpのキャプチャを直接パターンマッチする。`FindStringSubmatch` の `[]string` は
  `List String` として受け取れるので、
  `match gist_url.FindStringSubmatch url { [_, id] -> Ok id  _ -> Err ... }`
  と書ける。Goの `if m == nil` と `m[1]` より境界が明確である。
- sum typeで分岐を集約する。`Target` への `match` がmethod/route/status/visibilityを
  網羅的に決める。Goで `if creating` を散在させると、網羅性検査が効かない。
- `try` と `require` で検査を平らにする。`try require (cond) "message"` で、Goの
  `if !cond { return ..., errors.New(...) }` の階段を平らにできる。
  `run` は6行の直線になった。
- `Result.map_err` でエラーの型を変換する。`GoError` から `String` への変換は
  `Result.map_err (fn e -> e.Error ()) r` の1行で済む。
- Gomの `test` ブロックで、純粋部分のテストを定義の隣に置ける。生成されたGoの
  `_test.go` に出力されるので、`go test` からも走る。

### hostが不要になった部分: GoのAPIを直接使う

- `Ptr.new ()` とexported fieldへの書き込みで `http.Client` を構築する。外側の
  `let client : Ptr http.Client` があれば、内側の `let c = Ptr.new ()` に注釈は要らない。
  型が外側の期待から決まるためである。
- `Go.func2 (fn _req _via -> Err http.ErrUseLastResponse)` で `CheckRedirect` を設定する。
- JSONの生成は `Go.Map` と `json.Marshal` で行う。`quote` と `Sprintf` の文字列結合で
  JSONを組み立てる必要がなくなり、エスケープの正しさを自前で担保しなくてよくなった。
  `Go.Map String (Go.Map String String)` と `Go.Map String Go.Any` は入れ子にできる。
  Goはmapをキー順にソートして出力するので、出力は決定的である。そのため、テストで
  完全一致を確認できる。
- `for (name, value) in req.headers { request.Header.Set (name, value) }` で、
  タプルのリストをGoのメソッドへ流す。
- `else` のない `if` は、`if target == Create { Go.Map.set document "public" false }`
  のようにUnitの文として書ける。

### このリファクタで踏んだ今の限界

- 一般の型注釈式がない。`decoder.Decode (Ptr.new () : Ptr Go.Any)` はE0401になる。
  `e : T` は `Go.conv e : T` 専用だからである。型を決めるためだけに
  `let rest : Ptr Go.Any = Ptr.new ()` と名前を付けなければならない。
- 入れ子のコンストラクタパターンには括弧が要る。`Ok Help ->` は構文エラー
  E0101 "expected `->`… found identifier 'Help'" になり、`Ok (Help) ->` と書く。
  これは仕様4.1節の「ネストしたパターンは括弧で囲む」どおりである。ただし、引数なしの
  コンストラクタでも括弧が要る点は見落としやすく、エラーメッセージも理由を示さない。
- `send` はキーワードである。`select` の送信腕のために予約されており、`fn send` は
  "a function declaration needs at least one (param: Type) group, found send" という
  分かりにくい診断になる。仕様に予約語の一覧がないことは
  [implementation-gaps](../../implementation-gaps.md) に記録した。ここでは `perform` に改名した。
- `defer` がない。`read_body` は、`io.ReadAll` の結果を `try` する前に
  `response.Body.Close ()` を明示的に呼ぶ。この順序によって、すべての経路で閉じることを
  保証している。順序に意味があることは、コメントで残すしかない。
- structリテラルがない。decisionどおり、`Ptr.new ()` とfield書き込みのブロックを書く。
  そのため、3行以上の初期化ブロックが値の定義に混ざる。
- `&&`/`||` は正格に評価する。`a != "" && f a` は `f` を常に評価する。短絡が意味を持つ
  検査は `if` に分ける必要がある。今回の `require` 連鎖が都合がよいのは、この理由もある。
- `Go.Map.get` は `Option` を返し、`json.Unmarshal` は `Ptr` の先へ書き込む。そのため、
  Goの `var v string; json.Unmarshal(raw, &v)` に相当する処理に
  `let value : Ptr String = Ptr.new ()` と `Ptr.get value` が要る。
- `gom fmt` は長い `try require (...) "..."` を
  `try (require\n  (...)\n  "...")` という形に折り返す。読みやすさのために
  中間の `let` を挟むかどうか迷う箇所である。
- `test` ブロックは `try` の脱出先になるので、テストの中で
  `try request_body Create files` と書ける。今回はブロックの最後に `Ok ()` を置いた。
  `Result` を返さない純粋関数なら `assert_eq` だけで済む。

## 3. 将来の機能でどう書きたいか: 要求とpitfalls

| 欲しいもの | 今の書き方 | 書きたい形 | pitfalls |
| --- | --- | --- | --- |
| 型注釈式 `(e : T)` | `let rest : Ptr Go.Any = Ptr.new ()` | `decoder.Decode (Ptr.new () : Ptr Go.Any)` | `Go.conv e : T` と見た目が衝突する。変換と注釈を区別できる構文が要る |
| struct リテラル | `Ptr.new ()` + field 書き込み | `http.Client { Timeout = 30 * time.Second, CheckRedirect = ... }` | 値型/ポインタの区別、未指定 field のゼロ値をどう見せるか |
| `defer`/資源の括り | 明示的な `Close` の順序 | `with_body response (fn body -> io.ReadAll ...)` のような高階関数、または `defer` | `try` の早期脱出との順序。Go の `defer` に落とすなら Gom の式の値との関係 |
| record から JSON | `Go.Map` を手で組む | `json.Marshal { files = ..., public = false }`（タグ付き struct 生成） | field 名の大小文字・省略（`omitempty` 相当）・Option の表現 |
| JSON から record | `Go.Map String json.RawMessage` + `Ptr` | `json.Unmarshal` で record へ | 欠落・null・型違いの区別（今は `""` と null が同一視） |
| 引数なしコンストラクタの入れ子パターン | `Ok (Help)` | `Ok Help` | 変数パターン（小文字）との文法的曖昧さはない。大文字始まりなら許してよいか |
| `List` の重複検出・集合 | `List.find` の 2 重ループ | `Set`/`Go.Map` の Gom 側 API | 大きい入力では O(n²)。`Go.Map` で代替できるが記述が重い |
| 予約語の明示 | 改名で回避 | 仕様に一覧、診断で「`send` は予約語」 | `select`/`send`/`default` のような文脈キーワードを文脈キーワードにする案もある |

## 結論

- hostが消えた後のファイル分割は、「Goに渡すかどうか」ではなく「Gistを知るか」
  「副作用か」で切るのがよい。`run` は幹だけになり、枝は純粋で `test` できるようになった。
- `gist == ""` や `help: Bool` といった文字列フラグをsum typeにしたことが最大の改善である。
  注釈削除や `map_err` などの局所的な短縮は、その後でないと効果が小さい。
- 限界の多くは「型を決めるためだけの `let`」と「資源の後始末」に集中している。
  型注釈式と struct リテラル（または資源括りの慣用句）が次の候補。
