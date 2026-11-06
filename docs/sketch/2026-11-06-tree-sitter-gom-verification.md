# sketch: tree-sitter-gom / gom-mode treesit port verification

- 目的: `tree-sitter-gom/`（decisions [2026-11-06-tree-sitter-gom-grammar](../decisions/2026-11-06-tree-sitter-gom-grammar.md)）と `gom-mode` の treesit 移植が意図どおり動くことを prototype として確認した記録
- 状態: 規範ではない記録
- 環境: tree-sitter-cli 0.27.1（npm）、GNU Emacs 30.2（treesit あり）、cc で `libtree-sitter-gom.so` をビルド

## 1. `tree-sitter generate`

`grammar.js` + `src/scanner.c` から生成できる。残った conflict 宣言は 2 つだけ:

- `pat_arg`: `~x: pat` のラベル `:` と外側の型注釈の読み分け
- `arrow_type`: select arm の `x <- e : T -> body` で `->` が型を伸ばすか arm を開くか

ほかは `prec` / `prec.right`（ラベルの `:` の貪欲さ、lambda 本体と `|>` / `:` の右延長）と、`app_arg` の明示ラベル形を分けたことで解消した。

## 2. リポジトリ全体の parse カバレッジ

`tree-sitter parse` をリポジトリ内の全 367 個の `.gom` / `.gomi` に流した。

- **359 ファイルが ERROR / MISSING なしで parse できた**（stdlib、`testdata/fmt`、`testdata/spec/ok_*`、`testdata/golden`、`testdata/realworld`、`tutorial`、`examples`、editor の注釈付き fixture を含む）
- 残り 8 件はすべて「parse できないのが正しい」もの:
  - `internal/syntax/testdata/spec/E0101_*` 4 件、`E0102_unterminated_string`（字句エラー fixture）
  - `testdata/spec/E0101_go_field_write`（`b.N = 5` は代入構文が無いので parse エラーが正しい）
  - `testdata/spec/E0101_select_recv_pattern`（recv は ident / `_` のみで pattern はエラーが正しい）
  - `internal/gomtest/testdata/spans/f.gom`（`ab\ncd\nef` — gomtest のバイト位置 fixture で Gom ソースではない）

修正した食い違い:

- `ctor_pattern` は EBNF の `ctor-name {pat-arg}`（0 個以上）に合わせて `repeat`（`Empty` 裸コンストラクタが MISSING ident になっていた）
- `list_expr` は real parser（parse_expr.go parseList）と同じく `..expr` を任意位置の要素として受理（`[..xs]` が parse できなかった）
- string の `${` が `string_content` に飲まれていたのを、content から `$` を外して `interpolation` ノードが出るよう修正

## 3. `tree-sitter test`（corpus）

`tree-sitter-gom/test/corpus/basic.txt` に 17 件: 宣言、variant、`match`（guard 付き）、`if else`、pipe とラベル引数、`select`、record literal/update、list spread、文字列補間、`import go` / `extern`、`test`、演算子始まりの継続行、lambda、block、コンストラクタ/or/alias pattern、コメント、`.gomi` の宣言形 — **17/17 成功**。

## 4. gom-mode treesit port

`gom-mode` は `gom-use-tree-sitter`（default t）と `treesit-ready-p` で 2 系統を切り替える。`make test` = `test-regex` + `test-treesit`（`editor/emacs/.tree-sitter/` に grammar を build して `treesit-extra-load-path` に置く）。

結果（Emacs 30.2）:

- **treesit path: 13/13 pass** — 既存の ert-font-lock 注釈（`font-lock.gom` / `.gomi`）、`testdata/fmt/*.out.gom(i)` + stdlib の再インデント不変テスト、imenu、Flymake、fmt 連携がすべて treesit でも通る
- **regexp fallback: 12 pass + 1 skip**（treesit 専用テストが文法なしで skip — 正しい）
- 新規テスト `gom-mode-treesit-active` / `gom-mode-treesit-fallback` で使用中エンジンを pin

font-lock で対応した差分:

- `Chan.send` の `send` のように `.` 直後のキーワードは member_name 配下としてキーワード face から除外（`:pred` で member_name の子を判定）
- `fmt.Println` の `Println`（UIdent after `.`）は type face にしない
- `~name` は `~` と ident を別キャプチャして 1 トークンに見せる（app_arg / pat_arg / fn_param / arrow_type）
- `///` doc comment は `gom-doc-comment-face`（`:match "\\`///"`）

indent は `treesit-simple-indent-rules`（`gom--treesit-indent-rules`）で、括弧レベル +2、variant `|`、`else`、演算子継続行を node/parent の形で表現。再インデント不変テストがすべての `gom fmt` 出力に対して pass したので regexp 版 `gom-indent-line` と等価な結果になっている。

## 5. 未検証 / 残件

- `M-x treesit-install-language-grammar` 経由のインストール自体はネットワーク clone が要るので未検証。`treesit-language-source-alist` 用の `(gom URL nil "tree-sitter-gom")` 形式は src/ に parser.c + scanner.c がある前提で、コミット済みなので要件は満たす
- `.gomi` と `.gom` の start rule 共用や head `{` 前行の寛容さは editor grammar の意図的な leniency（compiler が別途受理判定する）
- gom-mode が Emacs 27（treesit なし）でも fallback で動くことは code path 上自明だが、この環境では Emacs 30.2 でしか実行していない
