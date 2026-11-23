# sandbox 探索（PR #163）からの不足機能の整理

- 日付: 2026-11-23
- 関連: [PR #163](https://github.com/podhmo/gom/pull/163)（sandbox/ の探索者 run — 
  未マージのアーカイブとして残る）、
  [観測ログの転記](ja/2026-11-23-sandbox-explorer-log.md)、
  [TODO.md](../../TODO.md)、[open-questions.md](../open-questions.md)、
  [implementation-gaps.md](../implementation-gaps.md)
- 状態: 調査記録（規範ではない）

## 目的

PR #163 の「探索者」run — 普段使いの Go 風 CLI ツールを gom で実際に書き、
`sandbox/log.md` にエラー・不足・迂回・諦めを時系列で残したもの — から、
gom に不足している機能と不具合を抽出し、各項目の対応先（TODO / open-questions /
既存 tracker / 対応不要）を決める。

以下の「観測」はすべて `sandbox/log.md` の記録が根拠。log・ideas・プロンプトの
`.md` は [docs/sketch/ja/](ja/) に転記済み（`2026-11-23-sandbox-explorer-*.md`）。
`.gom` のコード（「諦めたもの」の実例など）は未マージの PR #163 側に残る。

## 抽出した不足（対応先別）

### TODO に載せたもの（機能追加・明らかな不具合）

1. **`//go:embed` / directive comment / blank import**
   - 観測: `import _ "embed"` が E0101（構文自体がない）。Go の directive
     コメントを書く面もなく `embed.FS` は原理的に使えない。
   - 迂回: `os.ReadFile` で実行時読み込み。
   - 材料: `//gom:allow`・`//gom:noescape`・`//gom:result-notaint` と
     ファイル内 directive の機構は既にある。属性・directive・blank import
     構文のどれで供給するかは decision 案件。
   - → TODO「Go 相互運用」

2. **record フィールドへの Go struct tag（`json:"name"` など）**
   - 観測: フィールド名は lowercase 強制・Go 側は PascalCase emit で tag を
     付けられず、`json.Marshal` が `{"Name":...}` を出す。外部スキーマの
     snake_case JSON と合わない。text/template の `{{.name}}` も同根。
   - 迂回: `Go.Map` / `json.RawMessage` 経由（jsoncfg・gist-cli と同じ戦略）。
   - → TODO「Go 相互運用」

3. **`@method` emit 名の escape（initialism 対応）**
   - 観測: `serve_http` → `ServeHttp` しか出せず `ServeHTTP`/`URL`/`ID` 等の
     initialism を含む Go メソッド名を生成できない → record に `http.Handler`
     や `sort.Interface` を実装できない（E0401）。
   - 迂回: `Go.conv (Go.func2 h) : http.HandlerFunc` または `mux.HandleFunc`。
   - 材料: [decision](../decisions/2026-11-21-go-iface-impl.md) が
     「規約で足りなければ別 decision」と予告していた分岐。属性引数は現行の
     属性構文にないので、属性引数の導入か verbatim 綴りの escape が選択肢。
   - → TODO「Go 相互運用」

4. **`Go.conv` の `List` → named Go slice が check を通るが `go build` で落ちる**
   - 観測: `Go.conv xs : sort.IntSlice`（`xs : List Int`）が check 通過、
     emit は `sort.IntSlice(list.List[int64])` でコンパイル不能。
   - → TODO「Bugs」（implementation-gaps に PR #163 側で登録済み — 修正作業として TODO 化）

5. **`gom run` がシグナルを子プロセスにフォワードしない**
   - 観測: `gom run FILE` の実バイナリは `gom-run-*/out` の子で、親の gom
     プロセスへのシグナルが届かない。`go run` はシグナルを転送するので
     挙動が違う（実害: Ctrl+C で止める対象がずれる）。
   - → TODO「Bugs」

6. **`rt.MakeChan[os.Signal](1)` が `go vet` の false positive を踏む**
   - 観測: buffered chan を helper 経由で作ると vet が透視できず
     "misuse of unbuffered os.Signal channel"。生成 `.go` を vet する
     利用形態で毎回出る。
   - → TODO「Bugs」

7. **stdlib `List` の補強**
   - 観測: `take`/`drop`/`last`/`sort`/`sum`（Int 版）がなく、`List.sum` は
     `List Float` 専用。jot の `last_n` や duu のソートで自前実装が要った。
   - → TODO「言語機能」

### open-questions に載せたもの（提供形が未決）

8. **ビット演算子**
   - 観測: `|`/`&`/`^`/shift がなく `os.O_APPEND|os.O_CREATE|os.O_WRONLY`
     を `+` で代用。`<<`/`>>` は関数合成に使用済みで名前の余地が狭い。
   - 既存 tracker: [problems/operator-coverage](../problems/2026-11-01-operator-coverage.md)
   - → open-questions「言語機能」

9. **`Go.Any` の検査面（型アサーション相当）**
   - 観測: `Go.conv` では取り出せない（E0507 — spec が明示的に拒否する
     設計）が、代替の面がなく `json.Unmarshal` で `any` に decode した値は
     触れない。
   - 迂回: `json.RawMessage` の段階デコード、`Go.Map`、reflect。
   - → open-questions「Go 相互運用」

10. **`Go.Slice` の要素に Gom 宣言型を置けるか（E0507 緩和）**
    - 観測: `Go.conv xs : Go.Slice Entry` が E0507 "no Go representation"。
      `slices.SortFunc` に record のスライスを渡せず、duu はマージソートを
      自前実装した。
    - → open-questions「Go 相互運用」

11. **nil の綴り（`Go.Slice`/`Go.Map`/Go 関数型）**
    - 観測: `h.Sum(nil)` が書けず `[]` で代用。`Ptr.nil` しか nil の面がない。
    - → open-questions「言語機能」

12. **部分適用・`Unit` 残り関数値の lint**
    - 観測: `string i`（`()` 残り）を `fmt.Println` に渡すとエラーにならず
      `0x...` と印字される — log 曰く「gom 全体で最大の静かな罠」。
    - → open-questions「言語機能」（W-code lint を切るか）

### 対応不要・既存 tracker 済み

13. **`Ptr` が Option に包まれない** — `http.Head` の `Ptr http.Response` は
    nil を包まず deref panic がありうるが、`Ptr.to_option` が回避口として
    仕様化済み（[decision](../decisions/2026-11-05-ptr-ops-surface.md)）。
    仕様済みの設計なので tracker には載せない。

14. **`iota` 流の Go 定数セット宣言** — Go API の定数群を gom 側から
    作る面がない。→ open-questions「Future work」に 1 行追加。

15. **unsafe/reflect/cgo・メソッド制約を持つ Go generics** — E0501/E0506
    で拒否されるのは仕様済み。前者は Future work 既存項目、後者は
    spec 8.3 の未対応リスト記載済み。新規 tracker は作らない。

16. **`syscall.SIGINT` → `os.Signal`** は named→interface の適合で Go と
    同じ作法（`os.Interrupt` 変数を使う）。不足ではない。

17. **文字列 `+` なし** — `${}` 補間が設計上の答え。`Chan.make` の型注釈
    必須・`List.get` の引数順も仕様済みで、スキルの「ありがちな間違い」で
    カバーされる範囲。

18. **Option 値の `fmt.Println` 表示が `{41}`** — Go struct の素の表現。
    表示系の要否は別問題として tracker には載せない。

## 所感

不足の主戦場は Go 境界の慣用句（directive/tag/initialism/named slice 変換）
で、言語の核（ADT/match/try/currying）には破綻の報告がない — log.md の
結論と一致。優先度は「外部とのデータ交換に直結するもの」順に
embed・field tag・`@method` escape が上位になりそう。
