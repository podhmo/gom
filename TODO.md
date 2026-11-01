# TODO

決まっていて手を動かすだけのもの。設計が決まっていないものは [docs/open-questions.md](docs/open-questions.md) に書く。開発順序の理由は [docs/decisions/0024-development-order.md](docs/decisions/0024-development-order.md)。

方針: 機能を積み上げる前に、Go との表現の境界を実験で確かめる。現実の Go パッケージを使う小さなプログラムを早く動かす。

## ドキュメント

- [ ] `docs/grammar.ebnf` を作る（並置の適用、改行の規則、`select` の腕、ラベル付き引数、`_` プレースホルダを含む形式文法）
- [ ] エラーコード表（E0210, E0301 …）を `internal/diag` と `docs/` で一元管理する

## P0 Go 出力の実現可能性の検証

完成したコンパイラを作る工程ではない。Gom 構文からの自動変換はなくてよく、それぞれ複数の Go 実装案を手書きし、ビルド・ベンチマーク・Go からの使いやすさを比べる。結果は decisions/0018 と 0020 に記録する。

- [ ] 再帰的 ADT: sealed interface とジェネリクスの実用性、`nil` の扱い、タグ付き struct との比較
- [ ] 多相関数の部分適用: クロージャの生成と型の具体化、関数値の 2 つの表現の比較
- [ ] `List` と Go スライスの往復: コピー・所有権・性能、`[x, ..rest]` のメモリ保持
- [ ] `Result` と `(T, error)` の変換: 呼び出し規約と中間値の省略、2 つの表現の比較
- [ ] Go の interface と named type: `go/types` による型の対応

## P1 最小の型付き言語

- [ ] リポジトリと Go モジュールの初期化（docs/design.md のディレクトリ構成）
- [ ] 字句解析・構文解析（並置の適用、パイプ類の優先順位、改行の規則、波括弧の曖昧さ）
- [ ] 表層 AST → 名前解決済み AST → 型付き AST → Core IR（不変条件つき）→ Go AST の骨格
- [ ] 単相の型検査、ADT
- [ ] `match` の網羅性・冗長性チェックと反例生成（`internal/pattern`）。ガード付きの腕の扱いを含む
- [ ] 決定木コンパイルと type switch の生成、宛先渡しのコード生成
- [ ] 診断の内部表現と JSON / text 出力
- [ ] 小規模な `import go`（引数・戻り値が基本型の関数、`(T, error)`）
- [ ] 意味論の比較テスト用の小さな評価器（単相の純粋な式、ADT、`match`、関数適用）
- [ ] 完了条件: `Expr` 評価器のサンプルと、`strconv` を使う小さなプログラムがファイル単位でビルドでき、`go vet` を通る

## P2 型推論と関数

- [ ] HM 型推論（value restriction 付きでトップレベルのみ一般化）
- [ ] 多相関数、部分適用、クロージャ（関数値の表現は P0 の結果に従う）
- [ ] ラベル付き引数（評価順の保存、パンニング）
- [ ] `try` と脱出境界、`|?>` と自動ラップ
- [ ] Go ジェネリクスへの出力

## P3 Go 相互運用の本格実装

- [ ] `go/packages` による型の自動取り込み（`internal/goimport`）。表現できないシンボルの診断
- [ ] named type、interface（代入規則）、ジェネリック関数（`any` / `comparable`）、スライスのコピー、`Go.Map`、`Go.Slice`、`Go.conv`
- [ ] メソッド呼び出し、`_` プレースホルダ
- [ ] `extern`（型と変換規則の上書き）、標準のバインディング
- [ ] ADT の不正値（`nil`）に対する panic
- [ ] Go 相互運用テスト（両方向。`nil`、スライス、named type、interface、ジェネリクス）
- [ ] 完了条件: 実際の Go パッケージ（`os`、`io`、`strconv`、`encoding/json` など）を使うプログラムが動く

## P4 公開 API と開発体験

- [ ] `.gomi` の検査・`gom gomi`
- [ ] `gom fmt`（冪等性・往復一致のプロパティテスト、改行とコメントの境界ケース）
- [ ] ファイル内テストと `gom test`（`go test -json` の変換）
- [ ] `gom fix`、`gom explain`、`gom schema`
- [ ] ソース位置の対応表（`//line` と、Go のエラー・panic スタックの変換）
- [ ] LSP（型表示・網羅性エラーの即時表示、UTF-16 位置への変換）
- [ ] インクリメンタルビルドのキャッシュ（キャッシュキーは design.md 5 章）

## P5 並行処理と最適化

- [ ] `go` 式・チャネル型・`select` 式、`Task`
- [ ] 可変な値を送る・捕捉する場合のリンター警告
- [ ] コード生成の最適化（`List.map` などの特殊化）と性能検証（ベンチマーク、エスケープ解析、`go test -race`）
