# gotchas — skill とエラーメッセージだけで書いたときのはまりポイント

前提: トップレベルの README から辿れる `docs/spec.md`・`docs/design.md` に書いてある内容は省く。ここに挙げるのは、仕様を読んでも気付けず、コンパイルエラーや生成物を見て初めて分かった点（あるいは仕様にあっても「エラーの出方」が原因を示さない点）だけ。

## ツールチェーン

### `gom build` / `gom run` は gom 依存を推移的に emit しない

生成対象は引数に並べたディレクトリだけ。`main` が gom パッケージ `domain` を `import` していても、`gom run shop` は `shop/main.go` だけを出す。`gom check` は import 先の `.gomi` を読むので通るのに、`gom run`/`gom build` 後の Go のビルドで `no required module provides package .../domain` 系のリンクエラー（E0001）になる。

対処: `gom build shop shop/domain` と依存先をすべて並べて emit してから `gom run shop`。依存の列挙は自動化されないので、パッケージ構成が増えたら Makefile 等に書いておくとよい。

### `.gomi` は `gom info` で生成できる

非 `main` パッケージの `.gomi` をゼロから手書きする必要はない。`gom info -o domain/domain.gomi shop/domain` が全メンバのシグネチャを書き出す。吐き出されるのは**全公開の具体型**なので、ここから「外に見せたい形」へ削る作業になる — record を sealed にしたい型は `type T` の bare 宣言に書き換える、公開しないメンバは消す、等。

## 構文・式の組み立て

### 型明示 `e : T` は式を閉じる

`Go.conv e : T`（内部表現は ascription）を二項演算・比較・`if` 条件の中に裸で置くと、その直後の演算子が文法エラーになる:

```gom
let ok = Go.conv n : Float == qty   -- E0101: expected a newline, ... found "=="
let ok = (Go.conv n : Float) == qty -- OK
```

`:` の右側は型だけを食って式がそこで完結するので、式の中で使うときは毎回括弧で囲む。`r.Comma = Go.conv '\t' : Int32` のように文全体が右辺になる位置なら裸でよい。

### 関数適用を次の行へ続けるには括弧が要る

`{ }` ブロックの中で改行は式の区切り（spec §1.5）。ここで落ちるのは規則そのものではなく**エラーの出方**:

```gom
let ok = List.fold
  (fn acc path -> acc)   -- List.fold 単体が let の右辺として読まれる
  true
  paths
```

これは「`(` が閉じていない」系ではなく、`List.fold` への裸の参照が型引数を決められない **E0407 (type arguments could not be determined) や E0401** として出る。改行のせいだとは見えない。続けたいときは `(List.fold\n  f\n  acc\n  xs)` と括弧る。

### `let` 束縛は再帰しない

```gom
let rec_fn = fn xs -> rec_fn xs   -- E0201: undefined name `rec_fn`
```

`let` の右辺から束縛名は見えない（`let rec` は存在しない）。再帰したい関数はトップレベルの `fn` 宣言に出す。spec は `fn`/`@tailrec` の再帰ルールを書くが、「`let` では自己参照できない」とは明記されていない。

### ラムダのパラメータは識別子だけ、`fn` 宣言はパターンを取れる

非対称なので間違えやすい:

```gom
let g = fn (a, b) -> a                        -- E0101 が 6 連発。原因は見えない
let g = fn pt -> match pt { (a, b) -> a }     -- 分解は本体で

fn f ((a, b): (Int, Int)) -> Int = a + b      -- 宣言側はこれで通る
```

ラムダのパラメータに書けるのは `x`・`(x: T)`・`~x`・`(~x: T)` だけ（spec の文法そのものは合っているが、宣言側がパターンを取れるのでラムダでも取れると思い込む）。

## Go 境界の細部

### import した Go 関数の値は gom 関数の面を持つ

`bufio.ScanWords` のような Go 関数を値として使うと、gom 側ではカリー化された関数値として見える。`Go.Func2` を要求するパラメータ（`sc.Split` など）にそのまま渡すと E0401:

```gom
sc.Split (bufio.ScanWords)         -- E0401: 型が合わない（なぜかはエラーから分からない）
sc.Split (Go.func2 bufio.ScanWords) -- OK: gom 関数値 → Go.Func2 に再ラップ
```

`Go.funcN` が「gom の関数値」を取る変換器であることは spec にあるが、「import 直後の Go 関数の素の面が gom 側にある」ことは実際に E0401 を踏むまで分かりにくい。

### Go struct のフィールド代入は「面を変えてから」

`r.Comma = '\t'` は E0401（expected Int32, found Char）なので `r.Comma = Go.conv '\t' : Int32`。代入自体は `ptr.Field = v`（spec にある）だが、Char と Int32 が別物なので rune 系フィールドは毎回 `Go.conv` が要る。

## gom ではなく Go 側の仕様が原因ではまる点

skill・spec に書かれていて当然なものは除くが、実際にコードを書くと Go のライブラリ仕様で嵌る:

- `encoding/csv`: `FieldsPerRecord = 0` は「先頭レコードと同じ列数を強制」であり「チェックしない」ではない。可変列数を許すには負値（`0 - 1`）。
- 同じく csv: `w.Write [""]`（空文字列 1 フィールドの行）は空行として出力され、読み側に届かない。空レコードを往復させる方法はない。
- `regexp` の置換参照は `${2}`。`$2` は `$name` 解釈で別物になる。
- `os.Rename` は Unix では既存の宛先を黙って上書きする。拒否したいなら先に `os.Stat` で存在確認。
- `encoding/json`: 大きな整数は `dec.UseNumber ()` + `json.Number` にしないと `float64` 化して精度が潰れる。`json.Valid` は `Go.Slice Byte` を取る。
- `time`: layout 文字列は参照日時 `2006-01-02 ...` の魔法の値。ISO 用は定数 `time.DateOnly` がある。

## 診断の読み方のクセ

- **E0101 の連発**はたいてい「その場に書けない形を書いた」（ラムダのパターン、ascription の直後に演算子）。最初の 1 個だけ見て位置を絞る。残りはパースがずれた派生エラー。
- **E0407** は「どこかに `x : T` を付けろ」の合図。エラーの指す式そのものではなく、その値の出処（リテラル・`Go.nil`・型引数のある関数呼び出し）に注釈を足すと治る。
- **`gom check` は型・パターン検査まで**で、`@tailrec` 違反や Go リンク段階の不整合は `gom build`/`gom run` で初めて出る。`check` が緑でも run が失敗し得る。
