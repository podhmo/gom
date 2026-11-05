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

- [x] **[task] extern の「標準のバインディング」集の整備**: 管理場所はコンパイラ内蔵テーブル `internal/goimport/stdbind.go`（drift は Hidden 報告、明示 extern が優先）と決定 — 初期エントリとして `errors.New`/`fmt.Errorf` → `GoError`、`errors.Join`/`errors.Unwrap` → `Option GoError`（lone `error` の宣言ビュー機構と共用）。`(T, bool)` → `Option` 系エントリは用途ごとの extern を維持する判断（[decision](docs/decisions/2026-11-05-error-result-bindings.md)、[検証](docs/sketch/2026-11-05-error-result-bindings-verification.md)）
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

- [x] **[task] 境界での別名と可変性の検証**: H1–H7 を `testdata/gocall/boundary`+`box` fixture で実測 — `[]T` field は read ごとの boundary copy（`[][]int64` は `List (Go.Slice Int)` で内側は共有）、`let` は Go の書き換え可能な package var、生成 API 面の `List` は verbatim 受け渡し、バリアントは value struct。MoonBit（可視性は 4 種）/OCaml（FFI はブロック共有・規律は C 側）もドキュメントで確認（[sketch 検証結果](docs/sketch/2026-11-04-boundary-aliasing-hypotheses.md)）
- [ ] **[task] 境界の別名・可変性の契約を decision にする**: H1–H7 検証で確定した契約ギャップ（exported `let` が Go 側から再束縛可能、生成 API 面は `List` verbatim で spec 8.5 のコピー規則は Gom 自身の呼び出し側のみ、`List (Go.Slice Int)` の内側要素は可変）を、spec 8.7 の契約明記（OCaml 型: 不変は Gom 内側だけの保証）・境界コピー・型で防ぐのどれで閉じるか。sketch の方向候補 1–3 が入力（[台帳](docs/implementation-gaps.md) にずれ 2 件を登録済み）
- [x] **gomtest spec ハーネスの複数ファイル対応**: ディレクトリ case を採用 — 裸の side file（`expected.json`/`golden.go`/`case.json` のいずれか）を直接持つサブディレクトリが 1 case になり、メンバ `.gom` は 1 パッケージユニット、`.gomi` は自身のスコープで interface file としてコンパイル。`DiagnosticsFunc` は `func([]File)` に拡張（Supersedes: test-harness decision の一部）。fixture: `E0907_init_cycle_siblings`/`E0904`/`E0202`/`E0605` siblings + `ok_sibling_let`/`ok_sibling_iface`（[decision](docs/decisions/2026-11-05-gomtest-multifile-cases.md)、[検証](docs/sketch/2026-11-05-gomtest-multifile-cases-verification.md)）
- [ ] **[task] golden スイートのディレクトリ case 対応**: spec スイートは複数ファイル case をサポートしたが、golden スイートはグループ出力がメンバーごとに 1 ファイルになるため golden 形式自体の設計が必要。`RunGoldenTests` はディレクトリ case を `t.Fatalf` で明示拒否する状態（[decision](docs/decisions/2026-11-05-gomtest-multifile-cases.md) Consequences 参照）
- [ ] **[task] 兄弟 test 名タグ付けを spec fixture で pin できるようにする**: 出力される `TestX_<tag>` func 名は診断ではないため expected.json では表現不能。現状 `internal/build` の `TestCompileFilesTestNameCollision` が唯一のカバレッジ。suite option で `lower.Env{Tests:true}` を立てる経路か、func 名を期待する fixture 面の設計が要る（複数ファイル対応の残余、wave9 レビュー）
- [ ] **[task] spec スイート root 直下の裸 side file が無言で無視される**: `dirCase` は suiteDir 自身には走らず、root に置かれた `expected.json`/`case.json`/`golden.go` は marker にも orphan にもならずゼロシグナル。orphan 報告を足すか無視を明記するか（wave9 gomtest レビュー N3、nit）
- [x] **[task] `gom test` に `go test` フラグ透過がない**: `gom test -- FLAGS` で最初の `--` 以降を `go test -json` へ verbatim 透過（`-race`/`-run` が生成コードに届くことを実測。`-json` は常に先頭、`-json=false` 等の不正フラグは `source:"go"` の E0001 経路）[decision](docs/decisions/2026-11-05-gom-test-go-test-flags.md)、[検証](docs/sketch/2026-11-05-gom-test-go-test-flags.md)
- [ ] **[task] Emacs gom-mode を tree-sitter 化する**: `editor/emacs/gom-mode.el` は tree-sitter 文法が無いため正規表現ベースで書いた（[sketch](docs/sketch/2026-11-05-emacs-gom-mode.md)）。`docs/grammar.ebnf` から tree-sitter-gom（`grammar.js` + 改行 NL 挿入の外部 scanner）を書き、[podhmo/moonbit-mode](https://github.com/podhmo/moonbit-mode) と同じ `treesit` ベースの font-lock / imenu / インデントへ移す。`editor/emacs/test/` の ert-font-lock 注釈とインデントテストをそのまま回帰テストに使う

## Bugs

（見つけたバグはここに `- [ ]` で足す）

- [x] **[bug] ラベル付き部分適用を let 束縛すると E0901**: 原因は `CallUnknown` が `CallInfo.ArgToParam` を無視して常に位置順 `CallFunc` チェーンに落としていたこと — 非前置のラベル充填はカリー化チェーンで表現不能だった。`unknownCall` が ArgToParam を消費し、飽和は宣言順チェーン・非前置充填は `closureChain` で η 展開（eval と同じ意味論）、不安定 callee は `tmpBind` で 1 回評価に固定（issue #11、knownCompileBugs 解除、golden `labeled_args` に η/リオーダー形追加）
- [x] **[bug] `gom fmt` の適用後セレクタが非冪等**: `argRaw` が `Select` 引数を `pPost` 裸出力し、レシーバ連の最左が `(` 先頭（`()`/タプル/低優先式、nb 下の `{`-led RecordLit/Block 含む）の場合 `.f` が適用側へエスケープして再パースでツリーが変わっていた。`selectEscape`/`parenLed` で外側を `(...)` に包む（`nb` 伝播込み、issue #12、knownFormatBugs 解除、`TestSelectArgParens` 17 ケース）
- [x] **[divergence] Float リテラル演算が生成 Go の定数畳み込みで評価器と値がずれる**: 数値演算は演算子ごとに機械型で 1 回評価する（eval 側の意味）と決定。lower がリテラル同士の Float 演算・比較と Int 系 `+ - * /` の左被演算子を変数に bind して生成 Go を非定数式にする（`litFoldDiverges`）。Int リテラルオーバーフローの go build 失敗も解消（[decision](docs/decisions/2026-11-05-numeric-arithmetic-per-operation.md)、[検証](docs/sketch/2026-11-05-numeric-arithmetic-per-operation.md)）
- [x] **[divergence] 異なるバリアント値への名前付きペイロードアクセス `.f` で eval とコンパイルがずれる**: assertion 意味を採用 — `.f` はフィールドを所有するバリアントを要求し、違約は panic（eval E9003、生成コードは型アサーション）。spec §3.2/§8.6 に明記（[decision](docs/decisions/2026-11-05-variant-field-assert-owning-ctor.md)、[検証](docs/sketch/2026-11-05-variant-field-assert-owning-ctor.md)）
- [x] **[bug] 要素型の異なる `Go.conv` `Go.Slice T ↔ List U` が依然 pun で mutation が List に届く**: `convOK` の `sliceListPair` で不一致要素対を oracle 手前で E0507 拒否（要素コピー案より拒否を採用）。`Go.Slice↔Go.Slice` verbatim pun と同要素 copy は不変（[decision](docs/decisions/2026-11-05-goconv-slice-list-element-mismatch.md)、[検証](docs/sketch/2026-11-05-goconv-slice-list-element-mismatch.md)、[fixture](testdata/spec/E0507_go_conv_slice_elem_mismatch.gom)）
- [x] **[bug] 未 clean な `.gomi` 入力パスが `gomiInputs[want]` を外れ `.gomi` が二重コンパイルされ診断が重複する**: `gom check pkg/../pkg/ex.gomi pkg/a.gom` のような入力で、Join 済みの clean な `want` キーに一致せず `os.ReadFile`+`compileInput` にフォールバック → 同一 `.gomi` の診断が入力スペリングと clean スペリングの 2 件で出る。`gomiInputs`/`dirCensus` `byPath` のキーを `filepath.Clean` して修正（初版の登録は「`filepath.Dir` 未 clean で `.gom` グループ分裂」と誤診 — `Dir` は内部で Clean するため `.gom` 側は常に正しかった。実際の穴は `.gomi` 入力キー。wave7 gomtest レビューで発見、wave8 レビューで機序訂正。[台帳](docs/implementation-gaps.md)、`TestCheckUncleanGomiInput`）
- [x] **[divergence] eval は整数演算を型幅に関係なく int64 で計算する**: 演算位置の静的被演算子型で幅を引く方式を採用 — `VInt` は sign-extended 済み canonical 値の不変条件を持ち、`evalPrim`/`evalUnary`/`litValue` が幅に正規化して計算・wrap（`Int32` の `%`・`MinInt32 / -1`・単項 `-` も int64 で実行→wrap で Go と一致。ホスト注入 VInt は pre-normalized 前提。[decision](docs/decisions/2026-11-05-eval-int-width-aware-ops.md)、[検証](docs/sketch/2026-11-05-eval-int-width.md)）
- [x] **[bug] check が整数リテラルの範囲を検査しない**: Go の定数モデルに揃えて拒否を採用 — `pendingLit` が式・パターンの整数・float リテラルを記録し `finalize` の `revisitLitRanges` が既定後の静的型で判定（単項 `-` 直接被演算子は符号込み、`Go.conv` 直接被演算子は変換先で float→整数の `constant.ToInt` 規則込み）、範囲外は E0417。eval の wrap 実体化は防御分岐化（[decision](docs/decisions/2026-11-05-int-literal-range-check.md)、[検証](docs/sketch/2026-11-05-int-literal-range-check.md)、[fixture](testdata/spec/E0417_int_lit_range.gom)）
- [x] **[task] E0904 の `through`/`path` に隠し `tmpN_xxxx` VarDecl 名が出る**: `CheckTailRecMutual` が `vars` を `tailrecCycle`/`tailrecPath` に渡し E0907 の `nodeName`/`DiagName` で描画（`tailrecLess` は表示名→生名→def offset で全順序）。fixture は `tmp1_b96eea0a` → `let (g, _)` に更新（wave7 lower-residuals residual-1）
- [x] **[task] E0407/E0409/E0801 で raw meta ID がメッセージ・`data["type"]` に残る**: 各診断が fresh `metaNamer()` で `types.Display` — E0409（satisfies・finalize）、E0407、E0801 に適用。fixture `E0409_eq_open_meta`/`E0801_test_body_open_meta` 追加（wave7 lower-residuals residual-2）
- [x] **[task] E0701 の Meta arm も raw `?n` を出しうる**: `check/expr.go` の E0701 報告が `shown.String()` を message/`data["type"]` に使い、`dflt` を持たない制約付きメタで内部 mint ID が漏れる — E0407/E0409/E0801 と同じリーククラス（fresh `metaNamer` で揃う。wave8 diag-fuzz レビュー F-4、pre-existing）— 対応: 3 報告箇所すべて `types.Display(t, metaNamer())` に統一（fixture [E0701_go_body_open_meta](testdata/spec/E0701_go_body_open_meta.gom) で `?1` を pin）。同クラスの残件は下の E0402/E0403 項目を参照
- [x] **[bug?] `reclassifyCalls` の `ArgToParam` nil 化で eval と compiled が乖離しうる（未確認）**: 結論は「乖離しない」と確認 — ただし機序は当初の記録と異なる。`ArgToParam` 非 nil は**到達可能**（`fn take () -> (Int -> ~a: Int -> ~b: Int -> Int) = f` のような非呼出使用が pre-registered meta を束縛した後の前方呼出で `ArgToParam=[0,2,1]` が乗る）で、ガードは `CallInfo.Validate` の panic を防ぐ**稼働中の防御**。乖離がないのは eval が再分類後の同一 `CallInfo` を読む（`eval.go:1088`）ことと、nil 化後の位置適用が declared-fn の `overDeclared` 規約と同じだから（[sketch](docs/sketch/2026-11-05-reclassify-arg-to-param-guard.md)、wave9 small-bugs。TODO 自体はクローズ）
- [-] **[task] Gist CLIのGo境界**: byte I/Oと`Go.Slice String`→`List String`を実装して残りのexample-local adapterを縮小する。最小再現と必要操作は[Gist prototype](docs/sketch/2026-11-03-gist-cli-prototype.md)、関連: P3-2/P3-3。
- [x] **[bug] `(error, error)` 系の結果で先頭の nil-able `error` が `Ok` ペイロードに検査なしで入る**: `func F() (error, error)` → `Result GoError GoError` の派生写像で、`resultFromGo` は Ok 側に nil 検査を置かないため第 1 結果が nil error のとき nil `GoError` が `Ok` に紛れ込み `e.Error()` が Go 側で nil-interface panic する。lone `error` の宣言ビュー（`Option GoError` 相当を Ok 側に与える）で閉じられる同族の穴（pre-existing、wave8 error-boundary レビュー F-4。[台帳](docs/implementation-gaps.md)）— 解消: 派生ビューは `Result (Option GoError) GoError`、宣言の `Result GoError GoError` は `NilGuard` で境界検査、nil 許容な第 1 結果への `Result (Option T) GoError` 宣言を許容（[decision](docs/decisions/2026-11-05-error-pair-ok-payload.md)、[検証](docs/sketch/2026-11-05-error-pair-ok-payload-verification.md)）
- [x] **[bug] `Go.conv` のリテラル被演算子が named Go 型の変換先で範囲判定を skip する**: 対応: `check.UnderlyingSource` プロトコル + `goimport.Loader.GoUnderlyingKind`（underlying BasicKind の oracle）を追加し `litRange` が named 型の幅・符号で判定 — unsigned `[0,2^bits)`、float32、`LitChar` 監視を含む。非 Basic underlying は従来どおりリテラル自身の型で判定（[sketch](docs/sketch/2026-11-05-goconv-named-target-lit-range.md)、fixture [E0417_go_conv_named_target](testdata/spec/E0417_go_conv_named_target.gom)・[ok_go_conv_named_boundaries](testdata/spec/ok_go_conv_named_boundaries.gom)）
- [ ] **[task] E0402/E0403（および E0410/E0411 の可能性）も同クラスの `?n` 漏れを持つ**: `expr.go` の E0403（`if` without else）が `then.Type()/.String()`、E0402（mid-block Unit 警告）が `ty.String()` を使い、`if true { todo () % todo () }` や mid-block の制約付きメタで内部 mint ID が message/`data["type"]` に漏れる。E0410/E0411 の `b.ret.String()`（lambda 境界の未解決 ret メタ）も同リークの可能性。E0407/9・E0701・E0801 と同じく fresh `metaNamer` で揃う（wave9 small-bugs レビュー F、pre-existing）
- [ ] **[bug] 返り値関数のパラメータを指す labeled 引数が無診断で位置適用に潰れる**: `fn f (~a: Int) -> (~b: Str) -> Int` への `f ~b: "x"` は `paramByLabel` が flat index 1 を返すが `pi >= declArity` で `overDeclared` が**無診断**で立ち、`ArgToParam` nil → `f("x")` を出力（`lower/expr.go` ~858-900）→ Go が build できない。同型なら `f ~b: 5` → `f(5)` で `~a=5` の誤束縛が静かに走る。named-target `Go.conv` が直した accept→unbuildable と同クラスで、`reclassifyCalls` の ArgToParam ガードが依存している不変条件でもある（wave9 small-bugs レビュー F、pre-existing。[sketch](docs/sketch/2026-11-05-reclassify-arg-to-param-guard.md) 末尾にも記録）
- [ ] **[task] `(NamedIface, error)` / `(any, error)` の第 1 結果は nil-able だが `Result (Option T) GoError` を導出・宣言ともにできない**: `nilablePresence`/`isError` が宇宙 `error` と `*T` に限定されるため `(net.Error, error)` は `Result <named> GoError`（unwrap なし）に導出され、Option ビュー宣言も拒否される — `(error,error)` で閉じた穴の同族残余。宣言許容を広げるか現状維持かの判断が要る（wave9 error-tail レビュー F、[台帳](docs/implementation-gaps.md)）
- [ ] **[task] `payloadNilGuard`/`ConvOptNil` の lower ユニットテスト追加**: golden + e2e + extern test で形は pin 済みだが、core ノード生成条件（`GoResultsErr` + payload が raw `error` + 宣言 payload が `error`、の3条件）を直接 pin する lower 側テストがない（wave9 error-tail レビュー nit）

