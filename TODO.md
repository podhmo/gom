# TODO

> **Note on updating this file:** upkeep rules live in
> [`.claude/skills/todo-md/SKILL.md`](.claude/skills/todo-md/SKILL.md) —
> follow them whenever you add or complete a task. Short version: this file
> lists actionable work only (`[ ]` / `[-]`); mark finished work `[x]` — a
> periodic GC sweep appends such entries to `TODO-archive.md`.

決まっていて手を動かすだけのものを書く。設計が決まっていないものは
[docs/open-questions.md](docs/open-questions.md)、懸念事項は
[docs/problems/](docs/problems/)、調査・検証の記録は [docs/sketch/](docs/sketch/)。
開発順序の理由は [decisions/development-order](docs/decisions/2026-11-01-development-order.md)。

## 読み方（オーケストレーター向け）

- 各タスクには ID（`P1-3` など）と `依存:` を付ける。依存がすべて `[x]` になったタスクは並列に着手してよい。依存の書かれていないタスクはいつでも着手できる
- 1 タスク = 1 worktree = 1 squash merge を目安にした粒度。大きすぎたら `[-]` の親にして子タスクへ分ける
- 完了の条件（全タスク共通）: `gofmt -l .` が空、`go vet ./...` と `go test ./...` が通る。仕様の規則を実装したら `testdata/spec/` に、生成コードを変えたら `testdata/golden/` にテストを足す（[CLAUDE.md](CLAUDE.md)）
- Go は 1.27 以上。モジュールパスは `github.com/podhmo/gom`
- 実装中に決めたことは `docs/decisions/` に `状態: draft` で追加し、prototype で検証してから状態を更新する（[decisions/README](docs/decisions/README.md)）
- 方針: 機能を積み上げる前に、Go との表現の境界を実験で確かめる。現実の Go パッケージを使う小さなプログラムを早く動かす

## To Be Implemented

### 基盤（最初に着手。P0 と並列に進められる）

- [x] **B-1 Go モジュールとディレクトリの初期化**: `go.mod`（`go 1.27`）、[design.md 3 章](docs/design.md) のディレクトリ構成の空パッケージ、`cmd/gom` の空の CLI。各パッケージに `doc.go` で役割を書く
- [ ] **B-2 CI とチェックスクリプト**: GitHub Actions で `gofmt -l`、`go vet`、`go test -race` を回す。ローカル用に同じ内容の `make check`（またはスクリプト）を用意する。依存: B-1
- [ ] **B-3 sealed interface の網羅性検査**: go-sumtype 相当のツール（現状を調査して [docs/sketch/](docs/sketch/) に記録）を CI に入れ、AST にバリアントを足したときに CI が失敗することを確かめるテストを置く。Go 1.27 とジェネリック型への対応も確認する（[design.md 2.4](docs/design.md)）。依存: B-2
- [ ] **B-4 テストハーネス**: `testdata/spec/`（規則ごとの `.gom` と期待する診断 JSON）と `testdata/golden/`（生成 Go コード）を読むテストランナーと `-update` フラグ。まだ通らないケースを skip として置ける仕組みを含む。依存: B-1, D-1

### ドキュメント

- [ ] **D-0 参考文献の確認**: [design.md 8 章](docs/design.md) のとおり、Maranget 2007 / 2008 の論文名・内容と go-sumtype の現状を確認し、[docs/sketch/](docs/sketch/) にレポートを書いて design.md を直す
- [ ] **D-1 形式文法 `docs/grammar.ebnf`**: 並置の適用、演算子の優先順位と結合性、改行の規則（[spec 1.5](docs/spec.md)）、波括弧の曖昧さ（1.6）、`select` の腕、ラベル付き引数、`_` プレースホルダを含める。パーサー（P1-3）の正になる
- [ ] **D-2 エラーコード表**: `internal/diag` に表を 1 か所で定義し、`docs/` へ生成または同期する。既存の `E0301`（非網羅の match）など spec に出ているコードから始める。依存: P1-1

### P0 Go 出力の実現可能性の検証

完成したコンパイラを作る工程ではない。Gom 構文からの自動変換はなくてよく、各案の Go 実装を手書きし、ビルド・ベンチマーク・Go からの使いやすさを比べる。実験コードは `experiments/p0/<topic>/` に置き、結果は [docs/sketch/](docs/sketch/) にレポートとして書いてから decisions を更新する。各タスクは独立で、すべて並列に進められる。

- [ ] **P0-1 再帰的 ADT の表現**: sealed interface＋ジェネリクスとタグ付き単一 struct を比較する。`nil` の扱い、割り当て、Go からの使いやすさを見る。結果を [decisions/value-representation](docs/decisions/2026-11-01-value-representation.md) に記録
- [x] **P0-2 多相関数の部分適用と関数値の表現**: カリー化形 `func(A) func(B) C` と arity 付きクロージャを比較する。型の具体化の時点、パッケージをまたぐ公開形、ラベル付き引数の部分適用を含む。結果を [decisions/function-representation](docs/decisions/2026-11-01-function-representation.md) に記録
- [ ] **P0-3 `List` と Go スライスの往復**: スライスベースと永続 cons リストを比較する。コピー・所有権・先頭追加の性能、`[x, ..rest]` のメモリ保持を見る。結果を [decisions/value-representation](docs/decisions/2026-11-01-value-representation.md) に記録
- [ ] **P0-4 `Result` と `(T, error)` の変換**: sealed interface と `struct { Value T; Error E; Ok bool }` を比較する。呼び出し規約、`|?>` の中間値の省略（[design.md 4.6](docs/design.md)）、大きな値型のコピー、ゼロ値を見る。結果を [decisions/value-representation](docs/decisions/2026-11-01-value-representation.md) に記録
- [ ] **P0-5 Go の interface と named type**: `go/types` から Gom の型への対応を小さなプログラムで確かめる（`io.Reader`、`time.Duration` など）。結果を [decisions/go-interop-boundary](docs/decisions/2026-11-01-go-interop-boundary.md) の追記と docs/sketch に記録
- [ ] **P0-6 P0 の結論の反映**: P0-1〜P0-5 の結果から [decisions/value-representation](docs/decisions/2026-11-01-value-representation.md)・[function-representation](docs/decisions/2026-11-01-function-representation.md) の状態を更新し、[design.md 4 章](docs/design.md) と [open-questions.md](docs/open-questions.md) の「P0 の実験で決めるもの」を整理する。依存: P0-1, P0-2, P0-3, P0-4, P0-5

### P1 最小の型付き言語

中間表現の型（P1-1, P1-2）を先に固めれば、以降の各フェーズは手書きの入力（AST や IR のリテラル）でテストしながら並列に実装できる。フェーズ間の依存は一方向（`syntax` → `resolve` → `check` → `pattern` → `lower` → `codegen`）。

- [ ] **P1-1 診断の内部表現 `internal/diag`**: 位置（UTF-8 バイトオフセット）、span、severity、code、message、構造化された `data`、notes、fixes の型。行・列への変換はここに置くが、出力形式はまだ作らない（[spec 12 章](docs/spec.md)、[decisions/json-diagnostics](docs/decisions/2026-11-01-json-diagnostics.md)）。依存: B-1
- [ ] **P1-2 中間表現の型定義**: 表層 AST（trivia を保持）、名前解決済み AST、型の表現（`internal/types`）、型付き AST（適用ノードの呼び出し情報を構造化）、Core IR（ANF、決定木、arity 分類、脱出ノード、相互運用の変換ノード）を sealed interface で定義する。Core IR には [design.md 2.1](docs/design.md) の不変条件を検査する validator を付ける。大きければ AST 側と IR 側で子タスクに分ける。依存: B-1, P1-1
- [ ] **P1-3 字句解析・構文解析 `internal/syntax`**: 再帰下降＋Pratt 法。並置の適用、パイプ類の優先順位、改行の規則、波括弧の曖昧さ、エラー回復。コメントと空行を trivia として保持する。依存: P1-2, D-1
- [ ] **P1-4 名前解決 `internal/resolve`**: スコープ、シャドーイング、モジュール参照、未定義名の診断。Go パッケージの読み込みは P1-9 で足す。依存: P1-2
- [ ] **P1-5 単相の型検査と ADT `internal/check`**: 型付き AST を作る。組み込み型、ADT、レコード、`if`/`match`/ブロックの型、ブロック途中の非 `Unit` 式のエラー（[spec 1.1](docs/spec.md)）、算術演算子の型の確定。依存: P1-2
- [ ] **P1-6 網羅性・冗長性の検査 `internal/pattern`**: Maranget の usefulness で判定し、反例を先頭 5 件まで生成する。ガード付きの腕の扱いを含む（[design.md 4.2](docs/design.md)）。`E0301` の診断と `fixes` を出す。依存: P1-2
- [ ] **P1-7 脱糖と決定木コンパイル `internal/lower`**: 型付き AST → Core IR。パイプの展開、ANF 化、match の決定木化、自己末尾再帰の検出、arity の分類。出力は P1-2 の validator を必ず通す。依存: P1-2
- [ ] **P1-8 Go コード生成 `internal/codegen`**: Core IR → go/ast。宛先渡し（`Return` / `Assign` / `Discard`）、type switch、ADT の不正値に対する `rt.InvalidVariant`、`//line` の付与、`go/printer` による出力。ADT の表現は P0-1 の結論に従う（それまでは design.md 4.1 の案で進めてよい）。依存: P1-2
- [ ] **P1-9 小規模な `import go`**: 引数・戻り値が基本型の Go 関数と `(T, error)` → `Result` を呼べるようにする（resolve・check・lower・codegen にまたがる）。依存: P1-4, P1-5, P1-7, P1-8
- [ ] **P1-10 診断の JSON Lines / text 出力**: `kind`、最後の行の `summary`、終了コード 0/1/2、行・列（列はコードポイント単位）の計算（[spec 12 章](docs/spec.md)）。依存: P1-1
- [ ] **P1-11 `cmd/gom` の `build` / `check`**: ファイル単位でフェーズをつなぎ、`.go` を出力する。`internal/build` は最小限（毎回すべて変換）。依存: P1-3, P1-4, P1-5, P1-6, P1-7, P1-8, P1-10
- [ ] **P1-12 意味論の比較テスト用の評価器**: 単相の純粋な式、ADT、`match`、関数適用を型付き AST（または Core IR）から直接評価し、生成 Go コードの実行結果と比べるテストを作る（[design.md 7 章](docs/design.md)）。依存: P1-2
- [ ] **P1-13 P1 の完了確認**: `Expr` 評価器のサンプルと、`strconv` を使う小さなプログラムがファイル単位でビルドでき、`go vet` を通り、評価器との比較テストに通る。依存: P1-9, P1-11, P1-12

### P2 型推論と関数

- [ ] **P2-1 HM 型推論**: 単一化、value restriction 付きでトップレベルのみ一般化（[spec 3.3](docs/spec.md)、[decisions/value-restriction](docs/decisions/2026-11-01-value-restriction.md)）。依存: P1-5
- [ ] **P2-2 多相関数・部分適用・クロージャ**: 関数値の表現は P0-2 の結論に従う。多相な定義の具体化とラッパー生成を Core IR で確定させる。依存: P2-1, P0-6, P1-13
- [ ] **P2-3 ラベル付き引数**: 評価順の保存（[design.md 2.1](docs/design.md) の例）、パンニング、v1 の制限（[spec 2 章](docs/spec.md)、[decisions/labeled-required-arguments](docs/decisions/2026-11-01-labeled-required-arguments.md)）。依存: P2-2
- [ ] **P2-4 `try` と `|?>`**: 脱出境界は最も内側の関数（[decisions/try-boundary](docs/decisions/2026-11-01-try-boundary.md)）、`|?>` の `%w` 自動ラップ（[spec 9 章](docs/spec.md)）。中間の `Result` を作らない生成（design.md 4.6）。依存: P2-1, P1-13
- [ ] **P2-5 Go ジェネリクスへの出力**: トップレベルの多相関数と多相 ADT を型パラメータ付きで出力する。依存: P2-2

### P3 Go 相互運用の本格実装

Go 相互運用を変えたら両方向（Gom → Go、Go → Gom）のテストを足す。`nil`、スライス、named type、interface を忘れない。

- [ ] **P3-1 `go/packages` による型の自動取り込み `internal/goimport`**: `go/types` の型の同一性を保って扱う。表現できないシンボルは参照時に理由付きの診断（[decisions/go-interop-boundary](docs/decisions/2026-11-01-go-interop-boundary.md)）。依存: P1-13
- [ ] **P3-2 named type と interface**: 名前的な型として扱い、`Go.conv` で明示変換、interface は `go/types` の代入規則に従う。依存: P3-1
- [ ] **P3-3 スライス・map・ジェネリック関数**: `List` との境界コピー、`Go.Slice`、`Go.Map`、制約が `any` / `comparable` のジェネリック関数。依存: P3-1, P2-5
- [ ] **P3-4 メソッド呼び出しと `_` プレースホルダ**: `_` 以外の引数を作成時に評価する（[spec 8.2](docs/spec.md)、[decisions/cute-placeholder](docs/decisions/2026-11-01-cute-placeholder.md)）。依存: P3-1, P2-2
- [ ] **P3-5 `extern` と標準のバインディング**: 型と変換規則の上書き、comma-ok の `Option` 化（[spec 8.4](docs/spec.md)）。依存: P3-1
- [ ] **P3-6 Go から Gom を呼ぶ**: 生成 API を Go のテストから使うテスト、ADT の不正値（`nil`）に対する panic のテスト（[spec 8.6, 8.7](docs/spec.md)）。依存: P3-2, P3-3
- [ ] **P3-7 P3 の完了確認**: `os`、`io`、`strconv`、`encoding/json` などを使うプログラムが動く。依存: P3-2, P3-3, P3-4, P3-5, P3-6

### P4 公開 API と開発体験

- [ ] **P4-1 `.gomi` の検査と `gom gomi`**: 実装とインターフェースの照合、下書き生成、正規化した構文木からのハッシュ（[spec 7 章](docs/spec.md)、[decisions/gomi-interface-files](docs/decisions/2026-11-01-gomi-interface-files.md)）。依存: P2-1
- [ ] **P4-2 `gom fmt`**: Wadler 流のプリティプリンタ。冪等性・往復一致のプロパティテスト、改行とコメントの境界ケース（[spec 1.7](docs/spec.md)、[design.md 6 章](docs/design.md)）。依存: P1-3
- [ ] **P4-3 ファイル内テストと `gom test`**: `_test.go` の出力、`go test -json` の変換、`assert_eq` 用の表示関数の自動生成（[spec 11 章](docs/spec.md)、[design.md 4.8](docs/design.md)）。依存: P1-13
- [ ] **P4-4 `gom fix` / `gom explain` / `gom schema`**: `safe` な fixes だけを適用、エラーコード表からの説明、メッセージの JSON Schema。依存: P1-10, D-2
- [ ] **P4-5 ソース位置の対応表**: Gom → Core IR → Go の対応表、`go build` / `go vet` のエラーと panic スタックを `.gom` の位置へ戻す（[design.md 2.2](docs/design.md)）。依存: P1-11
- [ ] **P4-6 LSP**: 型表示、網羅性エラーの即時表示、UTF-16 位置への変換。依存: P4-5
- [ ] **P4-7 インクリメンタルビルドのキャッシュ**: キャッシュキーは [design.md 5 章](docs/design.md)。依存: P4-1
- [ ] **P4-8 標準ライブラリ `stdlib/`**: `list`、`option`、`result` を Gom で書き、`.gomi` を付けてコンパイラ自身でビルドする。依存: P4-1, P2-5

### P5 並行処理と最適化

- [ ] **P5-1 `go` 式・チャネル型・`select` 式・`Task`**: [spec 10 章](docs/spec.md)、[decisions/concurrency-model](docs/decisions/2026-11-01-concurrency-model.md)、[select-receive-arms](docs/decisions/2026-11-01-select-receive-arms.md)。依存: P2-2
- [ ] **P5-2 可変な値の送信・捕捉に対するリンター警告**。依存: P5-1
- [ ] **P5-3 生成コードの最適化と性能検証**: `List.map` などの特殊化、ベンチマーク、エスケープ解析、`go test -race`。依存: P4-8

## Bugs

（見つけたバグはここに `- [ ]` で足す）
