# sketch: Emacs 用 gom-mode の下調べ

- 目的: `editor/emacs/gom-mode.el`（`.gom` / `.gomi` 用の major mode）を作るために必要な情報を集め、方針を決める
- 関連: 参考実装 [podhmo/moonbit-mode](https://github.com/podhmo/moonbit-mode)（2026-11-05 確認、`moonbit-mode.el` / `test/test-moonbit-mode.el`）。規範は [spec.md](../spec.md) と [grammar.ebnf](../grammar.ebnf)
- 状態: 規範ではない記録。言語仕様が変わったら mode 側を追従させる

## 1. 参考実装 moonbit-mode から借りるもの / 借りないもの

| moonbit-mode の機能 | gom-mode での扱い |
| --- | --- |
| tree-sitter (`treesit`) による font-lock / imenu | **借りない**。Gom には tree-sitter 文法が無い（`internal/gomgen` も EBNF インタプリタで tree-sitter ではない — [decisions/grammar-based-generator](../decisions/2026-11-02-grammar-based-generator.md)）。正規表現ベースの `font-lock-keywords` と `imenu-generic-expression` で書く。Emacs 27 以降で動き、文法のインストールが要らない |
| `moon check --output-json` を読む Flymake backend | 借りる。`gom check DIR` の JSON Lines（12 章）を読む |
| `moonfmt -` で buffer を整形する `moonbit-format-buffer` | 借りる。`gom fmt -` を使う |
| ERT + `ert-font-lock` のテスト | 借りる。`test/testdata/font-lock.gom(i)` に `//  ^^^ face` 形式の注釈を書く。`ert-font-lock` は Emacs 30 以降に同梱なので、それより古い Emacs ではこのテストだけ skip する |
| インデント | moonbit-mode は treesit 任せ。gom-mode は括弧の深さ（`syntax-ppss`）ベースで自前実装し、`testdata/fmt/*.out.gom` を「再インデントしても変わらない」ことで検証する |

## 2. `.gomi` に別 mode は要るか — 要らない

[grammar.ebnf](../grammar.ebnf) 1.1 節: `.gomi` は `.gom` と同じトークン規則で、本体なしの宣言（`type` / 本体なし `fn` / `let x : T` / `import`）だけを持つ。字句・キーワード・コメント（`///`）はすべて `.gom` の部分集合なので、`gom-mode` を `.gomi` にも割り当てれば足りる。`.gomi` 特有の書き方（`fn` の `=` が無い、`let` が `: T` で終わる、抽象型 `type Canvas`）は正規表現の font-lock に影響しない。

## 3. 字句（grammar.ebnf 2 章）

- コメント: `//`（行末まで）、`///`（doc comment、構文上は普通のコメント）、`/* */`（**入れ子にならない**、Go と同じ）
- 識別子: `ident` = 小文字か `_` で始まる ASCII、`UIdent` = 大文字で始まる。大文字は型・コンストラクタ・Go の公開名・モジュール（`List.map`）
- キーワード: `as default else extern false fn for go if ignore import in let match package select send test true try type with`
  - `assert` / `assert_eq` / `todo` / `not` / `show` / `compare` はキーワードではなく普通の関数（spec 1.1, 11.4）→ builtin face にする
  - `.` の後ではキーワードもメンバー名になる（`Chan.send`）→ `.send` はキーワードとして塗らない
- 属性: `@test` / `@tailrec`（`@` + member-name）
- リテラル: 10 進・`0x` / `0o` / `0b`・`_` 区切りの整数、`1.5` / `1e9` の浮動小数点（`.5` / `5.` は無い）、`'c'` の文字、`"..."` の文字列と補間 `${expr}`。raw 文字列（バッククォート）は無い
- 演算子: `|> <| |?> >> << || && == != < <= > >= + - * / % -> <- = | .. ~ : @`。`!` 単独・`&`・`^` は無い
- ラベル引数: `~name` / `~name:`（spec 2 章）

構文表（syntax table）上の注意:

- `'` は文字リテラルの区切りにしかならない（`x'` のような prime 付き識別子は無い）ので、文字列区切りとして扱ってよい
- 文字列補間の中に文字列を書ける（`"${f "x"}"`）。syntax table だけでは内側の `"` で文字列が閉じたと見なされる。v1 の gom-mode では補間内の入れ子文字列は扱わない（実際のコードでは稀）

## 4. インデント（spec 1.5, 1.7）

インデントは意味を持たない（[decisions/no-offside-rule-and-gom-fmt](../decisions/2026-11-01-no-offside-rule-and-gom-fmt.md)）。最終形は `gom fmt` が決めるので、editor のインデントは「打っている間に `gom fmt` の出力に近い位置へ置く」だけでよい。`gom fmt` の出力（`testdata/fmt/*.out.gom`）から読み取れる規則:

- 2 スペース。開いている `{` `(` `[` の数だけ下げる。閉じ括弧で始まる行は 1 段戻す
- `type X =` の次行の `| Ctor` は 1 段下げる（`decls.out.gom`）
- `=` / `->` で終わる行の次は 1 段下げる（`fn f ... =\n  body`）
- 二項演算子で始まる継続行（`+ bbb`、`|> f`）は 1 段下げる（`long.out.gom`）
- `match s {` の腕・`extern go "x" {` の項目・`select {` の腕は `{` の中なので括弧の規則だけで決まる

## 5. ツールチェーン連携（2026-11-05 に `go build ./cmd/gom` で確認）

- `gom fmt -`: stdin を読み、整形結果を **stdout**、JSON Lines の summary / 診断を **stderr** に出す。構文エラー時は exit 1 で、診断は `"file":"<stdin>"`
- `gom check FILE|DIR...`: stdout に JSON Lines。`-` (stdin) は受け付けない（`stat -: no such file or directory`, exit 2）。ディレクトリを渡すと `*.gom` / `*.gomi` を 1 パッケージとしてまとめて検査する。`span.file` は渡したパスに合わせた表記（絶対パスを渡せば絶対パス）
  - `span.start.line` / `col` は 1 始まり、`col` はコードポイント単位。Emacs の文字単位と一致するので、行に移動して `(1- col)` 文字進めればよい
  - stdin を受けないため、Flymake は**保存済みのファイル**を検査する（moonbit-mode と同じ制約）。未保存の編集は保存するまで反映されない
- 診断の `severity` は `error` / `warning` / `note`

## 6. 決めたこと

- ファイル: `editor/emacs/gom-mode.el`（単一ファイル）、`editor/emacs/test/gom-mode-test.el`、`editor/emacs/Makefile`（`make test`）
- 依存は Emacs 本体のみ（Emacs 27.1 以降。tree-sitter 不要）。2026-11-05 に Emacs 27.1（apt）と 31.1（snap）で `make test` が通ることを確認
- tree-sitter 化は Gom の tree-sitter 文法を書くところから必要なので、正規表現版を先に作り TODO.md に残す
- 機能: font-lock、インデント、imenu（`fn` / `type` / `let` / `test`）、コメント設定、`gom-format-buffer`、Flymake backend `gom-flymake`
- `.gomi` も `gom-mode`（2 節）

## 7. テストしながら分かったこと

- 2 スペースの括弧深さだけでは足りず、次の 2 つが要った
  - 二項演算子で始まる継続行（`long.out.gom` の `+ bbb`）は「文の先頭行」+2。文の先頭は、継続している行を同じ括弧深さの範囲で遡って求める（1 行前 +2 ではない。そうすると `+` の行が 1 行ごとに深くなる）
  - コメント行は直後のコード行と同じ深さにする（`decls.out.gom` の `/// docs for Rgb` は `| Rgb` に揃う）。直後が閉じ括弧ならブロック内の深さ
- `///` の判定で `syntax-ppss` を見る位置は `//` の 2 文字目より後（`+ 2`）でないとコメント内と判定されない
- `testdata/fmt/*.out.gom(i)` と `stdlib/**/*.gom(i)` は、行頭の空白をすべて消して再インデントすると元に戻る（`gom-mode-indent-formatted-files`）
- `internal/format` の `TestRepoFiles` はリポジトリ中のすべての `.gom` / `.gomi` を整形できるか検査するので、`editor/emacs/test/testdata/` の注釈付きファイルも構文として正しくなければならない
- Emacs 27 の `flymake-diagnostic-beg` は overlay を前提にするため、テストでは `flymake--diag-beg` を使う
