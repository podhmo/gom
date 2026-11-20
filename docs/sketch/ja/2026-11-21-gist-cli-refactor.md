# examples/gist-cli のリファクタリングで得た知見

- 日付: 2026-11-21
- 対象: `examples/gist-cli`（ブランチ `devin/*-gist-cli-refactor` の一連のコミット）
- 関連: [prototype](../2026-11-03-gist-cli-prototype.md)、
  [wave23 struct 構築](../2026-11-19-wave23-struct-construction.md)、
  [struct-field-write decision](../../decisions/2026-11-19-struct-field-write.md)
- 状態: 調査記録（規範ではない）

## 目的

gist-cli は「host パッケージ（Go 側の adapter）に寄せる部分」と「Gom に残す部分」
という境界で切られて育った。host が消えた今、その切り口は意味を失い、逆に 1 本の
`run` に argument・ファイル・JSON・HTTP・Gist の判断が接着されていた。これを

1. 全体: すべての操作を 1 本の木として捉え直し、用途とサイズで切り分ける
2. 局所: 不要になった型注釈や手作りの補助を、今の言語機能でより小さく書く

の順に書き直し、その過程で分かった「今どう書くべきか」「今の限界」「将来ほしいもの」を残す。

## 手順

コミット単位（古い順）:

1. 例をディレクトリ単位（1 パッケージ）でコンパイルし、Gom の `test` ブロックも走らせる
   ハーネスに変更（`integration_test.go`、`build.sh`）
2. 引数を `Command`/`Options`/`Target` に解析する `args.gom` を分離
3. 操作の木を files / gist / transport / json の層に分割
4. 以降は局所: client の内側の型注釈削除と上限値の命名、`Go.Map` + `json.Marshal` による
   body 構築、regexp キャプチャによる Gist id 抽出、`Result.map_err`/`Option.to_result`
   による短縮

各コミットで `gom check`、`go test ./examples/gist-cli`（既存の httptest 結合テスト全件 +
新設の Gom `test` ブロック）を通した。

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
`cfg.gist != ""` という文字列フラグに、「Gist の知識」が `run` の中の `if creating`
の連鎖に散っていた。葉の性質で分けると次の 3 種類になる。

| 種類 | 例 | 置き場所 |
| --- | --- | --- |
| 純粋で Gist を知る | route, method, status, body, URL パース | `gist.gom` |
| 純粋で Gist を知らない | 引数文法, 重複検出, JSON の 1 フィールド取り出し | `args.gom` / `files.gom` / `json.gom` |
| 副作用 | ファイル読み取り, HTTP, 終了 | `files.gom` / `transport.gom` / `main.gom` |

`main.gom` の `run` は木の幹だけ（6 行）になり、各枝は単独で `test` できる。

### 判断は型にする

- `gist: String` + `if gist == ""` は「作成か更新か」という 2 値の判断を空文字という
  番兵で表していた。`type Target = | Create | Update (id: String)` にすると、
  method・route・期待 status・visibility がすべて `match target` に揃い、
  「更新なのに id がない」状態が作れなくなる。
- `help: Bool` と `literal: Bool` も同様。`help` は結果の種類（`Command = Help | Upload`）、
  `literal`（`--` 以降）は解析の状態であって設定ではないので、`Options` から外して
  解析関数の分岐（`parse_options` / `parse_files`）にした。
- ただし「解析しながら検証する」と優先順位が変わる。最初の分割では `--gist` の値を
  その場で `parse_gist_url` していたため、`--gist not-a-url --help` が help ではなく
  エラーになった（レビューで指摘）。書かれたままの値を `Raw` record に集め、`--help`
  がもう現れない `finish` で `Options` に変換する 2 段にした。「生の入力」と
  「検証済みの値」を別の型にすると、どこで検証するかが型の境界として見える。
- 「検証済みの値」を作る関数（`parse_gist_url : String -> Result String String`）は、
  bool を返す `valid_gist_url` + 後段の `path.Base` より安全。id の取り出しと検証が
  同じ正規表現 1 つで行われ、両者がずれない。

### 汎用部分と方針部分を分ける

`transport.gom` は `HttpRequest`/`HttpResponse` という自前の小さな record だけを
受け渡し、Gist を知らない。timeout・redirect 拒否・2 MiB 上限という「この CLI の
transport 方針」はここに閉じる。逆に `gist.gom` は `net/http` を import しない。
こうすると request の組み立てを HTTP なしでテストでき（`request_body` のテスト）、
transport を差し替える余地も生まれる。

### 失敗は早く、送信前に

重複 basename の検出を「読みながら」から「読む前に全体で」に移した
（`load_files` → `first_duplicate`）。挙動（送信前に失敗）は同じだが、
副作用の前に純粋な検査を済ませる形になり、テストも純粋関数に対して書ける。

## 2. 今の Gom でどう書くか

### Go ではできなかった/冗長だったが Gom で良くなった書き方

- **`match` + リストパターンで引数文法**: `os.Args` を `Go.Slice.to_list` で
  `List String` にし、`["--gist", url, ..rest]` のように「オプションとその値」を
  1 つのパターンで取る。Go の `for i := 0; i < len(args); i++ { ... args[i+1] ... }`
  にある添字の境界チェックが、`["--gist"] -> Err "--gist requires a URL"` という
  網羅的な腕になる。
- **regexp のキャプチャを直接パターンマッチ**: `FindStringSubmatch` の `[]string` は
  `List String` として受け取れるので、
  `match gist_url.FindStringSubmatch url { [_, id] -> Ok id  _ -> Err ... }`
  と書ける。Go の `if m == nil` + `m[1]` より境界が明確。
- **sum type で分岐を集約**: `Target` への `match` が method/route/status/visibility を
  網羅的に決める。Go の `if creating` の散在は網羅性検査が効かない。
- **`try` + `require`**: `try require (cond) "message"` で、Go の
  `if !cond { return ..., errors.New(...) }` の階段を平らにできる。
  `run` は 6 行の直線になった。
- **`Result.map_err` での誤差の型変換**: `GoError` → `String` の変換は
  `Result.map_err (fn e -> e.Error ()) r` の 1 行。
- **Gom の `test` ブロック**: 定義の隣に純粋部分のテストを置ける。生成 Go の
  `_test.go` に落ちるので `go test` からも走る。

### Go の API を直接使う（host 不要になった部分）

- `Ptr.new ()` + exported field 書き込みで `http.Client` を構築。外側の
  `let client : Ptr http.Client` があれば内側の `let c = Ptr.new ()` に注釈は要らない
  （型は外側の期待から決まる）。
- `Go.func2 (fn _req _via -> Err http.ErrUseLastResponse)` で `CheckRedirect`。
- JSON の生成は `Go.Map` + `json.Marshal`。文字列結合による JSON 組み立て
  （`quote` と `Sprintf`）が不要になり、エスケープの正しさを自前で担保する必要がなくなった。
  `Go.Map String (Go.Map String String)` と `Go.Map String Go.Any` を入れ子にでき、
  Go の map キー順ソートで出力は決定的（テストで完全一致を確認できる）。
- `for (name, value) in req.headers { request.Header.Set (name, value) }` で
  タプルのリストを Go のメソッドへ流す。
- `else` のない `if`（`if target == Create { Go.Map.set document "public" false }`）
  は Unit の文として書ける。

### 今の限界（このリファクタで踏んだもの）

- **一般の型注釈式がない**: `decoder.Decode (Ptr.new () : Ptr Go.Any)` は
  E0401（`e : T` は `Go.conv e : T` 専用）。型を決めるためだけに
  `let rest : Ptr Go.Any = Ptr.new ()` と名前を付ける必要がある。
- **入れ子のコンストラクタパターンは括弧必須**: `Ok Help ->` は構文エラー
  （E0101 "expected `->`… found identifier 'Help'"）、`Ok (Help) ->` と書く。
  仕様（4.1 節「ネストしたパターンは括弧で囲む」）どおりだが、引数なしの
  コンストラクタでも括弧が要るのは見落としやすく、エラーメッセージも理由を示さない。
- **`send` はキーワード**: `select` の送信腕のため予約されており、`fn send` は
  "a function declaration needs at least one (param: Type) group, found send" という
  分かりにくい診断になる。仕様に予約語の一覧がない
  （[implementation-gaps](../../implementation-gaps.md) に記録）。ここでは `perform` に改名。
- **`defer` がない**: `read_body` では `io.ReadAll` の結果を `try` する前に
  `response.Body.Close ()` を明示的に呼ぶ順序で「すべての経路で閉じる」を保証している。
  順序に意味があることをコメントで残すしかない。
- **struct リテラルがない**: `Ptr.new ()` + field 書き込みのブロックを書く
  （decision どおり。ただし 3 行以上の初期化ブロックが値の定義に混ざる）。
- **`&&`/`||` が正格**: `a != "" && f a` は `f` を常に評価する。短絡が意味を持つ
  検査は `if` に分ける必要がある（今回の `require` 連鎖はこの理由でも都合がよい）。
- **`Go.Map.get` は `Option`**、`json.Unmarshal` は `Ptr` 先: Go の
  `var v string; json.Unmarshal(raw, &v)` 相当に `let value : Ptr String = Ptr.new ()`
  と `Ptr.get value` が要る。
- **`gom fmt` の折り返し**: 長い `try require (...) "..."` が
  `try (require\n  (...)\n  "...")` という形に折られる。読みやすさのために
  中間の `let` を挟むかどうか迷う箇所。
- **テストからの `try`**: `test` ブロックは `try` の脱出先になるので
  `try request_body Create files` と書ける。今回はブロックの最後に `Ok ()` を置いた。
  `Result` を返さない純粋関数なら `assert_eq` だけで済む。

## 3. 将来の機能でどう書きたいか（要求と pitfalls）

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

- host が消えた後のファイル分割は「Go に渡すかどうか」ではなく「Gist を知るか」
  「副作用か」で切るのがよい。`run` は幹だけ、枝は純粋で `test` 可能になった。
- 文字列フラグ（`gist == ""`、`help: Bool`）を sum type にしたことが最大の改善で、
  局所的な短縮（注釈削除、`map_err` など）はその後でないと効果が小さい。
- 限界の多くは「型を決めるためだけの `let`」と「資源の後始末」に集中している。
  型注釈式と struct リテラル（または資源括りの慣用句）が次の候補。
