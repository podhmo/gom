# gotchas — clone して中を覗かないと分からないもの

前提: skill 単体 + コンパイラのエラーメッセージ + `gom` のヘルプメッセージだけを頼りにコードを書く想定。README → `docs/spec.md` → `docs/design.md` で分かるもの、エラーメッセージが直接教えてくれるものは除く。**「それを知る術がリポジトリの中身を読むことしかない」ものだけを先に挙げる。**

## 1. 標準ライブラリ・組み込みモジュールの API 一覧を見る術が repo 内にしかない

`gom list` は importable な stdlib パッケージの**名前**だけを出す（`cmp` / `list` / `option` / `result`）。各パッケージに何があるかを列挙するコマンドはツールチェーンにない:

- 4 つの stdlib パッケージの中身の正は repo の `stdlib/<pkg>/<pkg>.gomi`（doc comment 付き）と `<pkg>.gom`。`Option.get_or_else`・`List.range`・`Result.to_option` などの存在はこれを読むか、名前を勘で書いて E0201 を踏むかの二択。
- `Go.Slice.to_list`・`Go.Map`・`Task.spawn`/`await`・`Ptr`・`Ref`・`Array`・`View`・`Chan` のような**組み込みモジュールの面は `.gomi` すら存在しない** — `internal/stdlib/builtin.go` の Go ソースにメンバ表として書いてあるだけ。spec はこれらのモジュールが存在することは書くが、関数一覧は載せていない。
- `gom info` の引数はディレクトリパス。repo を clone していれば `gom info -o - stdlib/list` で標準ライブラリのシグネチャも読めるが、clone なしでは組み込みモジュールを dump する口はない（`Go.Slice`/`Task`/`Ptr` 等は `.gom` ソースを持たないので repo があっても info では出ない）。

つまり clone なしで書くと、stdlib/builtin の API は「spec に名前が出たもの + 推測 + E0201」で探ることになる。skill を単体で ship するなら `stdlib/*/*.gomi` と builtin.go の面一覧を抜粋して同梱する価値がある。

## 2. `gom build` / `gom run` の emit 対象は引数のディレクトリだけ — gom 依存は推移しない

`gom build -h` / `gom run -h` にも書かれていない。`main` が gom パッケージ `domain` を `import` していても、`gom run shop` は `shop/main.go` しか emit しない。`gom check` は import 先の `.gomi` を読むだけなので通るのに、`run`/`build` 後の go 側で `no required module provides package .../domain`（E0001）になる。

エラーは出るが **gom の診断ではなく go 側のリンクエラーとして出る**ので「gom パッケージを自分で build に並べる」というモデルには、cmd/gom の実装か試行でしか辿り着かない。対処: `gom build shop shop/domain` と依存をすべて列挙してから `gom run shop`。

## 3. `gom info -o` が吐く `.gomi` は「全公開の素案」で、再生成は手編集を上書きする

`-h` にも "drafts" とあるが、実際に試して分かる点が 2 つ:

- 出力は全メンバの**具体型**そのまま。公開面はここから手で削る — sealed にしたい record は bare `type T` に書き換え、見せたくないメンバは消す。
- 既存 `.gomi` への再生成は手編集を問答無用で上書きする。実際 `gom info stdlib/list` で `stdlib/list/list.gomi` を regenerate したら `//gom:result-notaint` / `//gom:noescape` 行が落ちた（revert して復旧）。draft の土台として一度使って以後は手で保守する、という運用。

## 4. Go 側ライブラリの仕様を知らないと嵌る点（gom の資料には出ない）

- `encoding/csv`: `FieldsPerRecord = 0` は「先頭レコードと同じ列数を強制」であり「チェックしない」ではない。可変列数は負値（`0 - 1`）。
- 同じく csv: `w.Write [""]`（空文字列 1 フィールドの行）は空行として出力され読み側に届かない。空レコードを往復させる方法はない。
- `regexp` の置換参照は `${2}`（`$2` は `$name` 解釈）。
- `os.Rename` は既存の宛先を上書きする。拒否したいなら先に `os.Stat` で存在確認。
- `encoding/json`: 大整数は `dec.UseNumber ()` + `json.Number` にしないと `float64` 化で潰れる。`json.Valid` は `Go.Slice Byte` を取る。
- `time`: layout 文字列は参照日時 `2006-01-02` の魔法の値。ISO 用は定数 `time.DateOnly`。

---

## 参考: エラー/ヘルプで自己解決できるもの（劣後）

踏んだ時点でエラーメッセージが答えを教える、あるいは `gom` の `-h` に出るもの。ドキュメントには不要と判断したが、探索で実際に踏んだので痕跡として残す。

| 落とし穴 | エラー | 解決 |
| --- | --- | --- |
| `e : T`（`Go.conv e : T`）は式を閉じる | `Go.conv n : Float == qty` → E0101 | `(Go.conv n : Float) == qty` と括弧る |
| `{ }` 内の改行は式の区切り（spec §1.5） | 多行適用が E0407/E0401 に化ける（原因が見えにくい） | `(f\n a\n b)` と括弧る |
| `let` 束縛は再帰しない | `let f = fn xs -> f xs` → E0201 | トップレベル `fn` へ |
| ラムダ引数は識別子のみ（`fn` 宣言は `((a,b): T)` パターンを取れる非対称） | E0101 連発 | 本体で `match` |
| import した Go 関数値は gom 関数の面を持つ | `sc.Split (bufio.ScanWords)` → E0401 | `Go.func2 bufio.ScanWords` で再ラップ |
| `Char` → rune 系フィールド（`Int32`） | `r.Comma = '\t'` → E0401 | `Go.conv '\t' : Int32` |
| `Go.nil` を `List` 面のパラメータへ | E0513、エラーが nil-able 型の一覧を出す | `let b : Go.Slice Byte = Go.nil ()` |
| record リテラルの型が曖昧 | E0407 | `x : T` 注釈 |
| コマンド群 | — | `gom explain <code>`・`gom fix`・`gom list`・`gom info -o`・`gom fmt -w`・`gom dump -phase` が `-h` に全部出る |
