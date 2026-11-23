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
