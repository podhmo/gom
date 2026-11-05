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

### ドキュメント

- [ ] **D-3 `README.md` を書く**: 言語の概要・ビルド/実行の quick start・ツール群（`gom build`/`check`/`fmt`/`info`）・ドキュメントへの導線（spec/design/tutorial）を載せる。**プロジェクト終盤に書く** — 機能が増えるたびに更新対象になるのを避けるため、内容が安定してから一括で書く（ユーザー指定）。PR #3 で英語 README の初版は入ったが、十分性の最終判定は終盤に行う
- [ ] **D-4 tutorial/examples の go.mod 配置の整理**: tutorial の各章にはほぼ同形の go.mod がある（実質 `tutorial_test.go` が scratch module へコピーする用途だけで使い、`run.sh` は自前生成）。章側で生成するか集約するかは、gom がパッケージとして取り込まれる形（examples/tutorial が利用者からどう見えるか）を決めるタイミングで整理する。外部依存を取り込んでいない現状では不要。**D-3 と同程度の後の作業**（ユーザー指定）

### P0 Go 出力の実現可能性の検証

完成したコンパイラを作る工程ではない。Gom 構文からの自動変換はなくてよく、各案の Go 実装を手書きし、ビルド・ベンチマーク・Go からの使いやすさを比べる。実験コードは `experiments/p0/<topic>/` に置き、結果は [docs/sketch/](docs/sketch/) にレポートとして書いてから decisions を更新する。各タスクは独立で、すべて並列に進められる。

### P1 最小の型付き言語

中間表現の型（P1-1, P1-2）を先に固めれば、以降の各フェーズは手書きの入力（AST や IR のリテラル）でテストしながら並列に実装できる。フェーズ間の依存は一方向（`syntax` → `resolve` → `check` → `pattern` → `lower` → `codegen`）。

### P2 型推論と関数

### P3 Go 相互運用の本格実装

Go 相互運用を変えたら両方向（Gom → Go、Go → Gom）のテストを足す。`nil`、スライス、named type、interface を忘れない。

- [ ] **[task] extern の「標準のバインディング」集の整備**: P3-5 で `extern` 機構は実装済みだが、それを使う標準バインディング集（`(T, bool)` → `Option` などの既定上書きの一覧）は未着手。管理場所は [open-questions](docs/open-questions.md) の未決事項。P3-7 の完了確認で `os`/`io`/`strconv`/`encoding/json` のプログラムが extern を必要とするかの判断材料になる。依存: P3-5
- [x] **[task] `Ptr` の操作面の設計**: コンパイラ組み込みの `Ptr` モジュール（rt バック）として採用・実装 — `get`/`set`（nil で Go と同じく panic）、`is_nil`、`new`（`errors.As` 等の out-pointer 用）、`make`、`nil`、`to_option`（安全な読み出し）。`*T` 既定の問いは open-questions に残る（[decision](docs/decisions/2026-11-05-ptr-ops-surface.md)、[検証](docs/sketch/2026-11-05-p3-7-interop-ops-verification.md)）
- [x] **[task] `Go.Map` の操作面（member table またはモジュール）**: ctor-table 経路の `Go.Map` メンバーテーブルとして採用・実装 — `make`/`get`（`Option` 返し）/`set`/`delete`/`length`/`to_list`（順序不定スナップショット、v1 の反復面）。nil map は Go のまま（set のみ panic）。非 comparable key は E0508（[decision](docs/decisions/2026-11-05-go-map-ops.md)、[検証](docs/sketch/2026-11-05-p3-7-interop-ops-verification.md)）
- [x] **[task] `[]T` を in-place 変更する Go 関数への警告**: W0501 lint として採用・実装 — Unit 結果の Go 呼び出しがコピーされる `List` 引数（`..xs` spread、`Go.Slice` 宣言 param への `List`、List 位置へ物質化された `Go.Slice`）を受け取ると警告。素の `Go.Slice`（`..gs` spread、`...[]T` 要素、`Go.Slice`→`Go.Slice`）は verbatim で沈黙。`signal.Notify` 系は文書化済みの偽陽性（[decision](docs/decisions/2026-11-05-go-mutation-warning.md)）

- [x] **[task] `Go.conv` の `Go.Slice T ↔ List T` をコピーにする**: `convKind` が `goConvKindOf` を先に照会し `ConvSliceToGo`/`ConvSliceFromGo` を返す — `list.FromSlice` / `.ToSlice()` に。`freeze`/`thaw`/`bump` で両方向の分離を実測、golden `go_conv` で emission を固定（[decision](docs/decisions/2026-11-04-goconv-list-slice-copies.md)）
- [x] **[task] `Go.Slice` の読み取り専用の操作面**: ctor-table 経路（`Config.Modules["Go.Slice"]`、rt バック）で `length`/`get`（`Option`）/`slice`（共有ビュー、不正境界は Go と同じく panic）/`fold`（`b -> a -> b` カリー形）/`to_list`（スナップショット）を実装。書き込みは非搭載（[decision](docs/decisions/2026-11-05-go-slice-ops.md)、[検証](docs/sketch/2026-11-05-go-slice-ops-verification.md)）

### P4 公開 API と開発体験

- [ ] **P4-6 LSP**: 型表示、網羅性エラーの即時表示、UTF-16 位置への変換。依存: P4-5
- [ ] **P4-7 インクリメンタルビルドのキャッシュ**: キャッシュキーは [design.md 5 章](docs/design.md)。依存: P4-1

- [x] **[task] `gom test` で `.gomi` 適合検査を走らせる**: `runTest` が `runBuild` と同じ位置で `checkInterfaces` を呼び、E0601/E0603 のあるパッケージグループは `.go`/`_test.go` 書き出しと `go test` 実行の双方から withhold（[decision](docs/decisions/2026-11-04-gomi-conformance-in-gom-test.md)。`TestTestWithholdsOnInterfaceFailure` / `TestTestInterfaceGatePass`）
- [x] **[task] `gom test` の panic スタックを `.gom` 位置付きで再描画する**: `GoPosIndex.RenderStack` がフレームの `file:line` を `.gom` 位置に書き換え、`renderTrace` が「再描画 + `--- raw go stack trace ---` + 生トレース」の 2 部構成を `TestResult.Output`・パッケージ級 E0001・no-events E0001 の 3 経路に適用。`-q`/`--quiet` は生トレース側のみ抑制（[decision](docs/decisions/2026-11-05-gom-test-panic-stack.md)）

### P5 並行処理と最適化

### 検証

- [ ] **[task] 境界での別名と可変性の検証**: [sketch](docs/sketch/2026-11-04-boundary-aliasing-hypotheses.md) の仮説 H1–H7（Go struct の field 読み取り、`Ptr` 越しの観測、`[]T` 型 field、Go に渡した `List`・レコード・バリアントの書き換え）を fixture で確かめ、結果を同じ sketch に追記する。MoonBit・OCaml の比較はドキュメントで裏を取る。結果次第で spec 8.7 の契約か境界の扱いを decision にする
- [ ] **gomtest spec ハーネスの複数ファイル対応**: testdata/spec の fixture は 1 case = 1 ファイルのため、パッケージ単位の規則（兄弟ファイルをまたぐ初期化サイクル、兄弟 test 名タグ付けなど）を spec fixture として書けない。現状は `internal/build` ドライバテストで代替済み（`TestCompileFilesInitCycleAcrossSiblings` 等）。複数ファイル case の fixture 形式を決めて対応する
- [x] **[task] `gom test` に `go test` フラグ透過がない**: `gom test -- FLAGS` で最初の `--` 以降を `go test -json` へ verbatim 透過（`-race`/`-run` が生成コードに届くことを実測。`-json` は常に先頭、`-json=false` 等の不正フラグは `source:"go"` の E0001 経路）[decision](docs/decisions/2026-11-05-gom-test-go-test-flags.md)、[検証](docs/sketch/2026-11-05-gom-test-go-test-flags.md)
- [ ] **[task] Emacs gom-mode を tree-sitter 化する**: `editor/emacs/gom-mode.el` は tree-sitter 文法が無いため正規表現ベースで書いた（[sketch](docs/sketch/2026-11-05-emacs-gom-mode.md)）。`docs/grammar.ebnf` から tree-sitter-gom（`grammar.js` + 改行 NL 挿入の外部 scanner）を書き、[podhmo/moonbit-mode](https://github.com/podhmo/moonbit-mode) と同じ `treesit` ベースの font-lock / imenu / インデントへ移す。`editor/emacs/test/` の ert-font-lock 注釈とインデントテストをそのまま回帰テストに使う

## Bugs

（見つけたバグはここに `- [ ]` で足す）

- [ ] **[bug] ラベル付き部分適用を let 束縛すると E0901**: `fn transfer (a:g) (a00:g) (~amount:a) -> Unit` に対し `let partial = transfer ""` の後 `partial ~amount` を呼ぶと、core の let 束縛で `binder type String -> Unit differs from bound expression type ~amount: a -> Unit` の invariant violation。lower が部分適用の残りシグネチャを束縛先の型として張る経路が怪しい（issue #11、corpus `internal/build/testdata/fuzz/FuzzCompile/a0e777a52c995c11`、knownCompileBugs 登録済み）
- [ ] **[bug] `gom fmt` の適用後セレクタが非冪等**: `{ 0 ().A }` が `{(0 ()).A}` にフォーマットされ、pass 2 で再び書き換わる（`0 ().A` は `0 (().A)` と読み直されるべきところ、出力が `(0 ()).A` 側に寄る）。issue #12、corpus `internal/format/testdata/fuzz/FuzzFormat/0f2182ae5078642c`、knownFormatBugs 登録済み
- [x] **[divergence] Float リテラル演算が生成 Go の定数畳み込みで評価器と値がずれる**: 数値演算は演算子ごとに機械型で 1 回評価する（eval 側の意味）と決定。lower がリテラル同士の Float 演算・比較と Int 系 `+ - * /` の左被演算子を変数に bind して生成 Go を非定数式にする（`litFoldDiverges`）。Int リテラルオーバーフローの go build 失敗も解消（[decision](docs/decisions/2026-11-05-numeric-arithmetic-per-operation.md)、[検証](docs/sketch/2026-11-05-numeric-arithmetic-per-operation.md)）
- [x] **[divergence] 異なるバリアント値への名前付きペイロードアクセス `.f` で eval とコンパイルがずれる**: assertion 意味を採用 — `.f` はフィールドを所有するバリアントを要求し、違約は panic（eval E9003、生成コードは型アサーション）。spec §3.2/§8.6 に明記（[decision](docs/decisions/2026-11-05-variant-field-assert-owning-ctor.md)、[検証](docs/sketch/2026-11-05-variant-field-assert-owning-ctor.md)）
- [ ] **[bug] 要素型の異なる `Go.conv` `Go.Slice T ↔ List U` が依然 pun で mutation が List に届く**: `Go.Slice Int64 → List Int`・`Go.Slice Int32 → List Char` など表現等値の異要素対は `goConvKindOf` の `types.Equal` ゲートを外れ `ConvNamed` → `list.List[T](xs)` のキャストに落ちる — コピー化で潰したクラスと同型の穴（`Go.conv` のみが到達経路）。`convOK` で要素不一致対を拒否するか、表現等値の要素対もコピー扱いにする（wave7 slice-ops レビューで発見、[台帳](docs/implementation-gaps.md)）
- [ ] **[bug] `checkInterfaces` が `filepath.Dir` を clean せずにグループ化する**: `gom test pkg/../pkg/a.gom pkg/b.gom` のような入力で同一物理ディレクトリが別グループに分裂し、E0601/E0603 の重複だけでなく両半分で `complete=false` となり missing-member 適合違反が免除されうる（check/build/test 全経路、pre-existing。グループ化キーを `filepath.Clean` すればよい。wave7 gomtest レビューで発見、[台帳](docs/implementation-gaps.md)）
- [x] **[divergence] eval は整数演算を型幅に関係なく int64 で計算する**: 演算位置の静的被演算子型で幅を引く方式を採用 — `VInt` は sign-extended 済み canonical 値の不変条件を持ち、`evalPrim`/`evalUnary`/`litValue` が幅に正規化して計算・wrap（`Int32` の `%`・`MinInt32 / -1`・単項 `-` も int64 で実行→wrap で Go と一致。ホスト注入 VInt は pre-normalized 前提。[decision](docs/decisions/2026-11-05-eval-int-width-aware-ops.md)、[検証](docs/sketch/2026-11-05-eval-int-width.md)）
- [ ] **[bug] check が整数リテラルの範囲を検査しない**: `let n : Int32 = 3000000000` は受理されるが生成される `var n int32 = 3000000000` は build 不能（litFoldDiverges 経由の `0 - 2147483648` も同じ穴）。解消方向（Go 型範囲要求で拒否 vs wrap 実体化）は spec が沈黙のため decision が要る（wave7 int32-eval 検証中に発見。[problems](docs/problems/2026-11-05-int-literal-range-check.md)、[台帳](docs/implementation-gaps.md)）
- [ ] **[task] E0904 の `through`/`path` に隠し `tmpN_xxxx` VarDecl 名が出る**: `tailrecCycle`/`tailrecPath` が生の `sym.Name` を使うため分解用の隠し VarDecl が診断に漏れる（`E0904_tailrec_var_destructure.expected.json` で実測）。E0907 の `nodeName`/`DiagName` 機構を R4 にも再利用すればよい（wave7 lower-residuals レビュー residual-1、[台帳](docs/implementation-gaps.md)）
- [ ] **[task] E0407/E0409/E0801 で raw meta ID がメッセージ・`data["type"]` に残る**: `types.Display`/`metaNamer` の `?n` 再採番は E0401 系のみ適用。同じ機構を各診断で新規 namer を作って適用するだけ（wave7 lower-residuals レビュー residual-2、[台帳](docs/implementation-gaps.md)）
- [-] **[task] Gist CLIのGo境界**: byte I/Oと`Go.Slice String`→`List String`を実装して残りのexample-local adapterを縮小する。最小再現と必要操作は[Gist prototype](docs/sketch/2026-11-03-gist-cli-prototype.md)、関連: P3-2/P3-3。

