# gotchas — clone して中を覗かないと分からないもの

前提: skill 単体 + コンパイラのエラーメッセージ + `gom` のヘルプメッセージだけを頼りにコードを書く想定。README → `docs/spec.md` → `docs/design.md` で分かるもの、エラーメッセージが直接教えてくれるものは除く。**「それを知る術がリポジトリの中身を読むことしかない」ものだけを先に挙げる。**

## 1. stdlib パッケージの関数一覧を引く術がない（機能不足）

想定の流れは `gom list` → `gom info <短名>` だったが、`gom info` はディレクトリパスしか解決しないので `gom info list` は `stat list: no such file or directory` になる。**stdlib のシグネチャを名前で dump する口がない**のはドキュメントではなく機能の不足（open-questions.md に登録済み）。

- `gom list` が出すのはパッケージ名だけ（`cmp` / `list` / `option` / `result`、spec §873）。各パッケージの全メンバの正は repo の `stdlib/<pkg>/<pkg>.gomi`（doc comment 付き）。spec は `List.map`/`sum`/`filter`/`length`/`range` や `Result.to_option` 等を断片的に使うだけで、`take`/`drop`/`sort_by`/`zip_with_index` など全一覧は載せていない。clone していれば `gom info -o - stdlib/list` で stdout にも出せるが、それはたまたま repo のディレクトリを指しただけ — clone なしでは「spec に名前が出たもの + 推測 + E0201」で探ることになる。skill を単体で ship するなら `stdlib/*/*.gomi` の抜粋同梱が価値がある。
- 一方で**組み込みモジュールは spec に全部書かれている**: `Go.Slice`/`Go.Map`/`Array`/`View`/`Ref`（8.x 節）、`Task`/`Chan`（10 章）、`Result`/`Option`/`List` の層変換（§340）。こちらは repo を覗く必要はない — 混同しやすいので分けておく。

## 2. `gom build` / `gom run` の emit 対象は引数のディレクトリだけ — gom 依存は推移しない

`gom build -h` / `gom run -h` にも書かれていない。`main` が gom パッケージ `domain` を `import` していても、`gom run shop` は `shop/main.go` しか emit しない。`gom check` は import 先の `.gomi` を読むだけなので通るのに、`run`/`build` 後の go 側で `no required module provides package .../domain`（E0001）になる。

エラーは出るが **gom の診断ではなく go 側のリンクエラーとして出る**ので「gom パッケージを自分で build に並べる」というモデルには、cmd/gom の実装か試行でしか辿り着かない。対処: `gom build shop shop/domain` と依存をすべて列挙してから `gom run shop`。

## 3. `.gomi` を手で管理せず生成物にしたいなら、機能不足がいくつかある

「`.gomi` は `gom info` で生成して手で管理しない」方向にするなら、現状で欠けているもの:

- **再生成が `//gom:` ディレクティブを落とす** — spec は `//gom:noescape` / `//gom:result-notaint` を「`gom info` が書き出し」とするが、`gom info stdlib/list` で `stdlib/list/list.gomi` を再生成するとこれらの行が差分で消えた（implementation-gaps.md に登録済み）。生成物ならこれらの注釈は毎回 emit されないといけない。
- **abstract 化・非公開化の意図を生成に残す経路がない** — 今の出力は全メンバの具体型そのままで、sealed にしたい record を bare `type T` にする・見せたくないメンバを消す、という削りは手編集を前提にしていて再生成で全部消える。生成管理にするなら、その意図を `.gom` 側の属性か `.gomi` 内のディレクティブとして表して `gom info` が引き継ぐ仕組みが要る（`//gom:mutable` は既にこの形 — spec 上「著者が記述、`gom info` の再生成が引き継ぐ」となっているので、同じモデルを abstract/非公開にも広げる設計がありうる）。
- つまり現状の運用は「draft の土台として一度 `gom info -o` を使い、以後は手で保守」。生成管理に倒すなら上の 2 点が前提条件（open-questions.md に登録済み）。

## 4. Go 側ライブラリの仕様を知らないと嵌る点（gom の資料には出ない）

- `encoding/csv`: `FieldsPerRecord = 0` は「先頭レコードと同じ列数を強制」であり「チェックしない」ではない。可変列数は負値（`0 - 1`）。
- 同じく csv: `w.Write [""]`（空文字列 1 フィールドの行）は空行として出力され読み側に届かない。空レコードを往復させる方法はない。
- `regexp` の置換参照は `$1`/`$2` 単独でも動くが、直後に名前文字（英数字・`_`）が続くとグループ名として吸われる（`$2_x` → 名前 `2_x` の検索）。ブレース形 `${1}`/`${2}` にしておくと確実。
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
| `Go.nil` を `List` 面のパラメータへ | E0513 は拒否された型を名指しするだけ。nil-able の一覧は `gom explain E0513` が出す（ただし `Go.FuncN` が記載漏れ — implementation-gaps.md 登録） | `let b : Go.Slice Byte = Go.nil ()` |
| record リテラルの型が曖昧 | E0407 | `x : T` 注釈 |
| コマンド群 | — | `gom explain <code>`・`gom fix`・`gom list`・`gom info -o`・`gom fmt -w`・`gom dump -phase` が `-h` に全部出る |
