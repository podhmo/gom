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
- [x] **B-2 CI とチェックスクリプト**: GitHub Actions で `gofmt -l`、`go vet`、`go test -race` を回す。ローカル用に同じ内容の `make check`（またはスクリプト）を用意する。依存: B-1
- [x] **B-3 sealed interface の網羅性検査**: go-sumtype 相当のツール（現状を調査して [docs/sketch/](docs/sketch/) に記録）を CI に入れ、AST にバリアントを足したときに CI が失敗することを確かめるテストを置く。Go 1.27 とジェネリック型への対応も確認する（[design.md 2.4](docs/design.md)）。依存: B-2
- [x] **B-4 テストハーネス**: `testdata/spec/`（規則ごとの `.gom` と期待する診断 JSON）と `testdata/golden/`（生成 Go コード）を読むテストランナーと `-update` フラグ。まだ通らないケースを skip として置ける仕組みを含む。依存: B-1, D-1
- [x] **B-5 sealed interface への `//sumtype:decl` 適用**: `internal/{syntax,check,types}` の sealed interface に注釈を付けて網羅性検査を有効化し、`internal/syntax/expr_test.go` の部分プローブ switch 2 件（fork は `_test.go` も検査する）を型アサーションに書き換える（[sketch の findings](docs/sketch/2026-11-02-sumtype-check-selection.md)）。依存: P1-2a, B-3
- [x] **B-6 中間表現のテキストダンパー**: `syntax.File`/`check.File`/`types`/`core.File` に S式風の安定したテキスト `Dump` を用意し、フェーズ単体のゴールデンテストを可能にする（どのフェーズで壊れたかを分離できる）。`core` は P1-2b マージ後のフォローアップでよい。依存: P1-2a
- [x] **B-8 `DumpCore` と dump の gomtest 配線**: `internal/treedump` に `core.File` 用の `DumpCore` を追加し、フェーズ別 `.dump` ゴールデン fixture を `gomtest` で扱えるようにする（どのフェーズで壊れたか分離できる）。依存: B-6, P1-2b
- [x] **B-7 decisions README 同期ジェネレータ**: `internal/diag/codes_doc.go` と同じパターン（`go:generate` でレンダリング → テストで一致を強制）を `docs/decisions/` の frontmatter（状態/日付/タイトル）→ `docs/decisions/README.md` 一覧に適用し、手動同期を廃止する。依存: なし

### ドキュメント

- [x] **D-0 参考文献の確認**: [design.md 8 章](docs/design.md) のとおり、Maranget 2007 / 2008 の論文名・内容と go-sumtype の現状を確認し、[docs/sketch/](docs/sketch/) にレポートを書いて design.md を直す
- [x] **D-1 形式文法 `docs/grammar.ebnf`**: 並置の適用、演算子の優先順位と結合性、改行の規則（[spec 1.5](docs/spec.md)）、波括弧の曖昧さ（1.6）、`select` の腕、ラベル付き引数、`_` プレースホルダを含める。パーサー（P1-3）の正になる
- [x] **D-2 エラーコード表**: `internal/diag` に表を 1 か所で定義し、`docs/` へ生成または同期する。既存の `E0301`（非網羅の match）など spec に出ているコードから始める。依存: P1-1

### P0 Go 出力の実現可能性の検証

完成したコンパイラを作る工程ではない。Gom 構文からの自動変換はなくてよく、各案の Go 実装を手書きし、ビルド・ベンチマーク・Go からの使いやすさを比べる。実験コードは `experiments/p0/<topic>/` に置き、結果は [docs/sketch/](docs/sketch/) にレポートとして書いてから decisions を更新する。各タスクは独立で、すべて並列に進められる。

- [x] **P0-1 再帰的 ADT の表現**: sealed interface＋ジェネリクスとタグ付き単一 struct を比較する。`nil` の扱い、割り当て、Go からの使いやすさを見る。結果を [decisions/value-representation](docs/decisions/2026-11-01-value-representation.md) に記録
- [x] **P0-2 多相関数の部分適用と関数値の表現**: カリー化形 `func(A) func(B) C` と arity 付きクロージャを比較する。型の具体化の時点、パッケージをまたぐ公開形、ラベル付き引数の部分適用を含む。結果を [decisions/function-representation](docs/decisions/2026-11-01-function-representation.md) に記録
- [x] **P0-3 `List` と Go スライスの往復**: スライスベースと永続 cons リストを比較する。コピー・所有権・先頭追加の性能、`[x, ..rest]` のメモリ保持を見る。結果を [decisions/value-representation](docs/decisions/2026-11-01-value-representation.md) に記録
- [x] **P0-4 `Result` と `(T, error)` の変換**: sealed interface と `struct { Value T; Error E; Ok bool }` を比較する。呼び出し規約、`|?>` の中間値の省略（[design.md 4.6](docs/design.md)）、大きな値型のコピー、ゼロ値を見る。結果を [decisions/value-representation](docs/decisions/2026-11-01-value-representation.md) に記録
- [x] **P0-5 Go の interface と named type**: `go/types` から Gom の型への対応を小さなプログラムで確かめる（`io.Reader`、`time.Duration` など）。結果を [decisions/go-interop-boundary](docs/decisions/2026-11-01-go-interop-boundary.md) の追記と docs/sketch に記録
- [x] **P0-6 P0 の結論の反映**: P0-1〜P0-5 の結果から [decisions/value-representation](docs/decisions/2026-11-01-value-representation.md)・[function-representation](docs/decisions/2026-11-01-function-representation.md) の状態を更新し、[design.md 4 章](docs/design.md) と [open-questions.md](docs/open-questions.md) の「P0 の実験で決めるもの」を整理する。依存: P0-1, P0-2, P0-3, P0-4, P0-5

### P1 最小の型付き言語

中間表現の型（P1-1, P1-2）を先に固めれば、以降の各フェーズは手書きの入力（AST や IR のリテラル）でテストしながら並列に実装できる。フェーズ間の依存は一方向（`syntax` → `resolve` → `check` → `pattern` → `lower` → `codegen`）。

- [x] **P1-1 診断の内部表現 `internal/diag`**: 位置（UTF-8 バイトオフセット）、span、severity、code、message、構造化された `data`、notes、fixes の型。行・列への変換はここに置くが、出力形式はまだ作らない（[spec 12 章](docs/spec.md)、[decisions/json-diagnostics](docs/decisions/2026-11-01-json-diagnostics.md)）。依存: B-1
- [x] **P1-2 中間表現の型定義**: 表層 AST（trivia を保持）、名前解決済み AST、型の表現（`internal/types`）、型付き AST（適用ノードの呼び出し情報を構造化）、Core IR（ANF、決定木、arity 分類、脱出ノード、相互運用の変換ノード）を sealed interface で定義する。Core IR には [design.md 2.1](docs/design.md) の不変条件を検査する validator を付ける。大きければ AST 側と IR 側で子タスクに分ける。依存: B-1, P1-1
  - [x] **P1-2a** AST 側（表層・解決済み・types・型付き AST + CallInfo）。配置の決定は [decisions/2026-11-02-ast-representation](docs/decisions/2026-11-02-ast-representation.md)
  - [x] **P1-2b** IR 側（Core IR + 不変条件 validator）。`Func.Sig`=canonical flat/`Params`=宣言 arity、`ResidualType()`、呼び出し分類はノード型で固定
- [x] **P1-3 字句解析・構文解析 `internal/syntax`**: 再帰下降＋Pratt 法。並置の適用、パイプ類の優先順位、改行の規則、波括弧の曖昧さ、エラー回復。コメントと空行を trivia として保持する。依存: P1-2, D-1。実装時の解釈は [decisions/p1-3-parser-interpretations](docs/decisions/2026-11-02-p1-3-parser-interpretations.md)、未解決の文法ギャップは docs/problems/2026-11-02-* を参照
- [x] **P1-4 名前解決 `internal/resolve`**: スコープ、シャドーイング、モジュール参照、未定義名の診断。Go パッケージの読み込みは P1-9 で足す。依存: P1-2
- [x] **P1-5 単相の型検査と ADT `internal/check`**: 型付き AST を作る。組み込み型、ADT、レコード、`if`/`match`/ブロックの型、ブロック途中の非 `Unit` 式のエラー（[spec 1.1](docs/spec.md)）、算術演算子の型の確定。依存: P1-2
- [x] **P1-6 網羅性・冗長性の検査 `internal/pattern`**: Maranget の usefulness で判定し、反例を先頭 5 件まで生成する。ガード付きの腕の扱いを含む（[design.md 4.2](docs/design.md)）。`E0301` の診断と `fixes` を出す。依存: P1-2
- [x] **P1-7 脱糖と決定木コンパイル `internal/lower`**: 型付き AST → Core IR。パイプの展開、ANF 化、match の決定木化、自己末尾再帰の検出、arity の分類。出力は P1-2 の validator を必ず通す。依存: P1-2
- [x] **P1-8 Go コード生成 `internal/codegen`**: Core IR → go/ast。宛先渡し（`Return` / `Assign` / `Discard`）、type switch、ADT の不正値に対する `rt.InvalidVariant`、`//line` の付与、`go/printer` による出力。ADT の表現は P0-1 の結論に従う（それまでは design.md 4.1 の案で進めてよい）。依存: P1-2。実装の決定: [decisions/generated-go-conventions](docs/decisions/2026-11-02-generated-go-conventions.md), [decisions/generated-support-packages](docs/decisions/2026-11-02-generated-support-packages.md), 検証: [sketch](docs/sketch/2026-11-02-p1-8-codegen-verification.md)
- [x] **P1-9 小規模な `import go`**: 引数・戻り値が基本型の Go 関数と `(T, error)` → `Result` を呼べるようにする（resolve・check・lower・codegen にまたがる）。依存: P1-4, P1-5, P1-7, P1-8
- [x] **P1-10 診断の JSON Lines / text 出力**: `kind`、最後の行の `summary`、終了コード 0/1/2、行・列（列はコードポイント単位）の計算（[spec 12 章](docs/spec.md)）。依存: P1-1
- [x] **P1-11 `cmd/gom` の `build` / `check`**: ファイル単位でフェーズをつなぎ、`.go` を出力する。`internal/build` は最小限（毎回すべて変換）。依存: P1-3, P1-4, P1-5, P1-6, P1-7, P1-8, P1-10
- [x] **P1-12 意味論の比較テスト用の評価器**: 単相の純粋な式、ADT、`match`、関数適用を型付き AST（または Core IR）から直接評価し、生成 Go コードの実行結果と比べるテストを作る（[design.md 7 章](docs/design.md)）。依存: P1-2
- [x] **P1-13 P1 の完了確認**: `Expr` 評価器のサンプルと、`strconv` を使う小さなプログラムがファイル単位でビルドでき、`go vet` を通り、評価器との比較テストに通る。依存: P1-9, P1-11, P1-12
- [x] **P1-14 E90xx 診断コードの登録**: `internal/eval` が使う E9001–E9005 は `codes.go` の `categories` に `90` が無く登録不能。カテゴリ `90` と Phase `eval` を加えて登録し、`docs/error-codes.md` を再生成する。依存: P1-12, D-2

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
- [ ] **P4-9 LLM 向け言語ガイド skill `gom-agent-guide`**: MoonBit の `moonbit-agent-guide` 方式で `skills/gom-agent-guide/`（canonical、`npx skills` 互換の実ファイル）に SKILL.md(~500-800行, frontmatter は name+description,description を `.gom`/`.gomi`/`gom` コマンドで implicit 発火)+ `references/`(language-fundamentals, go-interop, concurrency, interface-files, diagnostics, project-layout)を作り、`.devin/skills/` へ vendor。AGENTS.md に「`.gom` を書くなら先に `$gom-agent-guide`」の routing 節を追加。skill 内の ```gom コード例は testdata 抽出テストで `gom check` 検証する（MoonBit の SKILL.mbt.md CI と同じ発想）。`gom new` 生成 AGENTS.md テンプレート（薄いルーター）は `gom new` 自体の実装時の別タスク。設計の正: [docs/sketch/2026-11-02-agent-skill-for-gom.md](docs/sketch/2026-11-02-agent-skill-for-gom.md)。依存: なし

### P5 並行処理と最適化

- [ ] **P5-1 `go` 式・チャネル型・`select` 式・`Task`**: [spec 10 章](docs/spec.md)、[decisions/concurrency-model](docs/decisions/2026-11-01-concurrency-model.md)、[select-receive-arms](docs/decisions/2026-11-01-select-receive-arms.md)。依存: P2-2
- [ ] **P5-2 可変な値の送信・捕捉に対するリンター警告**。依存: P5-1
- [ ] **P5-3 生成コードの最適化と性能検証**: `List.map` などの特殊化、ベンチマーク、エスケープ解析、`go test -race`。依存: P4-8

### 検証

- [x] **V-1 実行可能チュートリアル**: `tutorial/` に `.gom` のサンプル群と実行手順を作り、`gom build` / `gom check` / `gom test` で実際に動かす。エンドユーザー視点で不備・分かりにくい診断・壊れた UX を洗い出し、見つけたものは TODO.md の Bugs/タスクに追加してから実装に戻る。依存: P1-13
- [ ] **V-2 grammar ベースのランダムプログラム生成器**: `docs/grammar.ebnf`（または tree-sitter 文法を別途整備して二重実装相互検証）から妥当な `.gom` を生成し、パーサー fuzz・compile-canary・eval vs compiled 差分テストのシードとする。意味論の食い違い（評価順・`|?>`・構造的 `==`）を先回りで発見するためのもの。依存: なし（seed 消費側は parser/compile fuzz があると効果大）

## Bugs

（見つけたバグはここに `- [ ]` で足す）

- [x] **[bug] 組み込み関数の呼び出しが `gom build` で落ちる**: `todo ()`・`not x`・`assert`・`assert_eq` は `gom check` を通るが `gom build` が E0901 "builtin X used as a value" で失敗する（`internal/lower/expr.go` `applyCall` が CallInfo の振り分け前に `e.Fn` を `varExpr` へ下ろし、`KindBuiltin` が値位置でエラーになる）。結果として `test` ブロックが一切使えず、E0301 が提案する修正テキスト `-> todo ()` がビルドできないコードを挿入する。再現: `fn f () -> Int = todo ()`（V-1、`tutorial/10-errors` の suggested fix が挿す `todo ()` もこれに当たる）
- [x] **[bug] 構造的 `==` が Go の `==` にそのまま下り、List を含む型で生成コードが壊れる**: `[1, 2] == [1, 2]` は `gom build` 成功だが生成 Go が `slice can only be compared to nil` でコンパイル不能。ADT・レコードが List フィールドを持つ場合も同様（`Items [1] == Items [1]` → `struct containing list.List[int64] cannot be compared`）。スカラ・ADT・タプルの `==` は動く（spec 3.3 は構造的等価を要求。`tutorial/04-match` は ADT の `==` のみに留めて回避）
- [x] **[bug] 可変長引数を取る Go 関数への `f <| x` が引数リスト展開扱いになる**: `fmt.Println <| "hi"` → E0401 "expected Go.Slice Go.Any, got String" と付随する E0407。非可変長の Go 関数（`strconv.Itoa <| 42`）と Gom 関数では動く（V-1、`tutorial/08-pipes-labels` で `double <| 21` は動作）
- [x] **[bug] Go のパッケージ変数・定数が E0901 の内部不変条件違反になる**: `os.Args`・`math.Pi`・`strconv.ErrSyntax` などを参照すると "core invariant violation: symbol \"X\" is not in scope"。spec 8.1.5 は型なし定数を期待される Gom の型へ解決すると約束している。未実装なら内部エラーではなくユーザー向け診断にすべき（V-1、`os.Getenv`/`os.Setenv` は動くが `os.Args` で発見）
- [x] **[bug] ガード内でパターン変数同士の `==` が E0407 になる**: `P a b if a == b` → "the type of `a` could not be determined"。`a == 3`（変数 vs リテラル）や通常の変数の `x == y` は通る（V-1、`tutorial/04-match` のガード執筆中に発見）
- [x] **[bug] span を持たない診断が出力されることがある**: `(String, Bool)` タプルを返す `os.LookupEnv` の結果を `Some`/`None` で match すると、`"span"` を欠く E0407 が出る（spec 12 は位置を診断の一部として扱う。V-1、`os.LookupEnv` の match 中に発見）
- [x] **[bug] `GoError` のメソッドセットが露出していない**: `e.Error()` → E0401 "GoError has no field or method `Error`"。`${e}` や `fmt.Println` 経由の文字列化しかできない（V-1、`tutorial/07-go-interop`・`09-capstone` 執筆中に発見。作る側の設計上の問題は [docs/problems/2026-11-04-goerror-construction.md](docs/problems/2026-11-04-goerror-construction.md)）
- [x] **[task] 隠し・未解決シンボル後の診断カスケードを整理する**: `slices.Contains` 参照で正しい E0501 のあと誤解を招く E0401 "the Go callee takes 0 argument(s), got 2" と E0407 が続く。`type Meters = Int` の誤用では同一 E0401 が 3 連発する。二次診断の抑制と重複排除（V-1、`slices.Contains`・`type Meters = Int` で確認）
- [x] **[task] 空の `select {}` が E0901 "core invariant violation: select has no arms"**: ユーザー入力に対し内部不変条件エラーが出る。parse か check の段階でユーザー向け診断に（V-1、probe で発見）
- [x] **[task] `gom build` が `test` ブロックを lower している**: spec 11.6 はテストコードを `gom test` のときだけ出力するとしているが、build でも lower に掛かり `assert_eq` の builtin バグを踏む。lower/codegen の対象をコマンドで絞る（V-1、`test`/`assert_eq` を試して発見）
- [ ] **[bug] `checkGoApply` の arity メッセージが自己矛盾する**: 部分適用 `strconv.FormatInt 255` で "the Go callee takes 1 argument(s), got 1"（want に arity−missing を出しているため expected == actual に見える）。main で再現（bugfix-check レビューで発見）
- [ ] **[task] spec の例のうち現状コンパイルできないものを整理する**: spec.md 冒頭例（`List.map`/`List.sum`、P4-8 未実装）、8.1 `report` 例（`ignore` 未実装 + `strings.Join` への `List` 引数、P3-3 未実装）、10 章（`Option.get`/`Chan`/`Task`）。「将来の例」と注記するか実装タスクへ紐付ける（V-1 で確認）
- [ ] **[bug] 診断メッセージに生のソース断片が混入する**: `internal/syntax/parser.go` の `describe` がトークンのテキストをそのまま message に埋め込むため、改行（`\r`・`\n`）を含む文字列リテラルなどで message が複数行になり `diag.Diagnostic.Validate` に違反する（spec 12 の 1 行ルール）。再現: `"\r`（Parse と ParseInterface の両方。FuzzParse の knownBugs 登録済み、corpus: `internal/syntax/testdata/fuzz/FuzzParse/f83fcca0bb79a947`）
- [ ] **[bug] 負のオフセットを持つ span が診断に出る**: `"${}0` のような未終了の文字列補間で E0102 "unterminated string literal" が `Start:-1` の span を持つ（`internal/syntax/lexer.go` の `l.err(open, ...)` が `open` 未初期化 -1 のまま呼ばれる）。`diag.Span.Valid` 違反で JSON 出力も壊れうる（FuzzParse の knownBugs 登録済み、corpus: `internal/syntax/testdata/fuzz/FuzzParse/7fd0da99ac4242a0`）
- [ ] **[bug] match の腕パースが EOF で無限ループする**: `fn f () -> Int = match r {A -` のように、コンストラクタパターンの腕のあとに `-` が来て `}` なしで EOF に達すると、`internal/syntax/parse_expr.go` `parseMatch` の腕ループが `parseMatchArm` に EOF トークンからの hole 腕を無限に組み立てさせ、診断を毎秒 GB 級の速度で吐き続ける（fuzz ワーカーが ~3GB で OOM 殺されるのを確認。`0 -`・`x -`・`_ -` は再現せずコンストラクタパターンのみ）。`let A -` でも再現する（最小 7 バイト。let バインディングのパターンで同じループに入るので、ループの根は match 腕固有ではなくコンストラクタパターンの EOF 処理にあるはず）（FuzzParse・FuzzCompile の knownBugs 登録済み、corpus: `internal/syntax/testdata/fuzz/FuzzParse/25cd876c8882179d`、`internal/syntax/testdata/fuzz/FuzzParse/95e4f3c862ab915e`、`internal/build/testdata/fuzz/FuzzCompile/317c473d58e2e09a`、`internal/build/testdata/fuzz/FuzzCompile/95e4f3c862ab915e`）
- [ ] **[bug] 多相関数の型パラメータが生成 Go で宣言されない**: `fn len2 (xs: List a) -> Int = ...` が `func len2(xs list.List[A]) int64` を生成するが `[A any]` の型パラメータ宣言がなく `undefined: A` でコンパイル不能。型変数を含む `fn` は codegen まで通るが Go 側のジェネリクス宣言が出ない（FuzzCompile の seed で発見、knownBugs 登録済み）
- [ ] **[bug] 引数なしの Go 呼び出しが E0901 "missing atom" になる**: `let a = fmt.Print()` のような引数 0 個の Go 呼び出しが `CallGo.Args[0]` の欠落として E0901 "core invariant violation: missing atom"（span なし）を出す。ユーザー入力に対し内部不変条件違反を見せている（FuzzCompile が発見、knownBugs 登録済み、corpus: `internal/build/testdata/fuzz/FuzzCompile/491e9ef7ff2fcab4`）
- [ ] **[bug] 型変数を返す関数の本体との型不一致が E0901 になる**: `fn a()->a={}` が "core invariant violation: body type Unit differs from residual type a"、`fn f(n:Int)->a=if 0>0{}else{}` が "node type a differs from branch type Unit" を出す。型変数 `a` が束縛されないシグネチャを check が通し、lower の不変条件がユーザー入力由来で落ちる（FuzzCompile が発見、knownBugs 登録済み、corpus: `internal/build/testdata/fuzz/FuzzCompile/9b0854a6817c31f0`、`internal/build/testdata/fuzz/FuzzCompile/eb757a61313ec99f`）
- [ ] **[bug] Go モジュール名の式・呼び出し位置での誤用が E0901 になる**: `import go "fmt"` したあと `{fmt}` のようにモジュール名そのものを式として使うと E0901 "module fmt is not an expression"、`{fmt()}` のように呼ぶと E0901 "callee fmt has kind module, want func or method" を出す。stdlib モジュール名 `Result()` の呼び出しでは "module Result is not an expression" に加えて "applying a non-function value of type Unit" という第 2 の E0901 も漏れる（最小 repro: `fn f () -> Unit = Result()`）。resolve/check を素通りして lower が内部不変条件違反を報告している（FuzzCompile が発見、knownBugs 登録済み、corpus: `internal/build/testdata/fuzz/FuzzCompile/8766eb6f9d7dacac`、`internal/build/testdata/fuzz/FuzzCompile/7e29a555777c312f`、`internal/build/testdata/fuzz/FuzzCompile/4530d4217dc217ba`）
- [ ] **[bug] 非 UTF-8 バイトを含む文字列リテラルが生成 Go を壊す**: `fmt.Print("\xeb")` のように不正 UTF-8 バイトを含む文字列リテラルがそのまま生成 Go に出力され、codegen 内部の gofmt チェックが "illegal UTF-8 encoding" で落ちて Compile が内部エラーを返す。NUL バイトも同様に verbatim で出て "illegal character NUL" になる（最小 repro: `fn f () -> String = "\x00"`）。lexer がリテラルの UTF-8 正当性を検査していない（FuzzCompile が発見、knownBugs 登録済み、corpus: `internal/build/testdata/fuzz/FuzzCompile/21a1017aecd04729`、`internal/build/testdata/fuzz/FuzzCompile/44f4de9cded9f70c`）
- [ ] **[bug] 数値リテラルへの member アクセスで lower が panic する**: `fn a()->a=0.A00()` のように数値リテラルをレシーバにした member 式が `internal/lower/expr.go` `memberExpr` で panic する（check が素通りした型を lower が想定していない）。ユーザー入力に対しコンパイラが落ちる（FuzzCompile が発見、knownBugs 登録済み、corpus: `internal/build/testdata/fuzz/FuzzCompile/bfbbf06d58cec1a4`）
- [ ] **[bug] 整数リテラルの表記が正規化されず生成 Go が壊れる**: `let a=080` の `080` がそのまま `var a int64 = 080` に出力され、Go では不正な 8 進リテラルとして codegen 内部の gofmt チェックが "invalid digit '8' in octal literal" で落ちる。Gom のリテラル受理規則と Go の表記変換がずれている（FuzzCompile が発見、knownBugs 登録済み、corpus: `internal/build/testdata/fuzz/FuzzCompile/f7bdbe872126e546`）
- [ ] **[task] fuzz knownBugs の `Skip: true` エントリがバグ修正後に orphan 化する**: `CheckRegistry` の TODO 判定は `strings.Contains(todo, bug.TODO)` で `- [x]` の完了行にもマッチするが、Skip 済み（hang 系 4 件）は実行されないため staleness 検出が効かず、対象バグが直ってもエントリが永続的に残る。`- [ ]` 行内マッチに絞るとよい（fuzz レビューで発見、`internal/testutil/fuzzutil` / `internal/{syntax,build}/fuzz_test.go`）
