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

- [ ] **D-3 `README.md` を書く**: 現在 `# gom` のみの空状態。言語の概要・ビルド/実行の quick start・ツール群（`gom build`/`check`/`fmt`/`info`）・ドキュメントへの導線（spec/design/tutorial）を載せる。**プロジェクト終盤に書く** — 機能が増えるたびに更新対象になるのを避けるため、内容が安定してから一括で書く（ユーザー指定）
- [ ] **D-4 tutorial/examples の go.mod 配置の整理**: tutorial の各章にはほぼ同形の go.mod がある（実質 `tutorial_test.go` が scratch module へコピーする用途だけで使い、`run.sh` は自前生成）。章側で生成するか集約するかは、gom がパッケージとして取り込まれる形（examples/tutorial が利用者からどう見えるか）を決めるタイミングで整理する。外部依存を取り込んでいない現状では不要。**D-3 と同程度の後の作業**（ユーザー指定）

### P0 Go 出力の実現可能性の検証

完成したコンパイラを作る工程ではない。Gom 構文からの自動変換はなくてよく、各案の Go 実装を手書きし、ビルド・ベンチマーク・Go からの使いやすさを比べる。実験コードは `experiments/p0/<topic>/` に置き、結果は [docs/sketch/](docs/sketch/) にレポートとして書いてから decisions を更新する。各タスクは独立で、すべて並列に進められる。

### P1 最小の型付き言語

中間表現の型（P1-1, P1-2）を先に固めれば、以降の各フェーズは手書きの入力（AST や IR のリテラル）でテストしながら並列に実装できる。フェーズ間の依存は一方向（`syntax` → `resolve` → `check` → `pattern` → `lower` → `codegen`）。

### P2 型推論と関数

- [-] **P2-2 多相関数・部分適用・クロージャ**: 関数値の表現は P0-2 の結論に従う。多相な定義の具体化とラッパー生成を Core IR で確定させる。依存: P2-1, P0-6, P1-13
  - [ ] **P2-2c タプル等に格納した循環関数値の初期化循環を解消する**: `fn count (n: Int) -> Int = if n == 0 { 0 } else { again (n - 1) + 1 }; let (again, _) = (count, 0)` が check/build を通るが、生成 Go は hidden tuple → count → again → hidden tuple の initialization cycle になる。集約値の初期化の副作用・依存順を保つ表現を Core IR で決め、生成コードの実行回帰を追加する。[再現と範囲](docs/sketch/2026-11-03-static-function-alias-verification.md)。依存: P2-2b
- [x] **P2-3 ラベル付き引数**: 評価順の保存（[design.md 2.1](docs/design.md) の例）、パンニング、v1 の制限（[spec 2 章](docs/spec.md)、[decisions/labeled-required-arguments](docs/decisions/2026-11-01-labeled-required-arguments.md)）。P2-2 の呼び出し情報構造化で実装は揃っており、規則ごとの spec fixture・評価順を固定する golden・eval/compiled 比較を追加した（[検証](docs/sketch/2026-11-05-p2-3-labeled-args.md)）。依存: P2-2
- [-] **P2-4 `try` と `|?>`**: 脱出境界は最も内側の関数（[decisions/try-boundary](docs/decisions/2026-11-01-try-boundary.md)）、`|?>` の `%w` 自動ラップ（[spec 9 章](docs/spec.md)）。中間の `Result` を作らない生成（design.md 4.6）。依存: P2-1, P1-13
  - [x] **P2-4b 明示的 `try` を含むパイプの融合**: unguarded try（最内ユーザー関数を脱出する `try`）を `hasUnguardedTry` で検出し、チェーンをセグメント化して融合。セグメント内は `EscapeCall`/`EscapeResult` で payload のみ運び末尾で一度だけ `Ok` 構築、セグメント間は feed 決定木、脱出先はユーザー関数のまま保存。ラムダ内だけに `try` があるケースは従来の単一境界融合。eval/compiled 一致は 16+21 ケース、GoError wrap 同一性は `TestPipeGoErrorChain` で実行検証（[decision](docs/decisions/2026-11-03-pipe-try-segmented-fusion.md)、[検証](docs/sketch/2026-11-03-pipe-try-segmented-fusion.md)）。依存: P2-4a
  - [ ] **P2-4c unguarded ステップの `EscapeCall` 融合（残る最適化）**: unguarded ステップの引数が Go `(T,error)` 呼び出しの場合、`ResultFromGo` で物質化してから次セグメントの `EscapeResult` が検査するため中間 Result が 1 個残る。引数評価（try を含むのでユーザー側の列）と呼び出し本体（セグメント内 `EscapeCall`）を分離すれば融合可能。併せて `pipeRest` の span 不統一（束縛は `lv.Y.Span()`・enter escape は `lv.Sp`）も直す（P2-4b レビュー F5/F6）。依存: P2-4b

- [x] **P2-5 Go ジェネリクスへの出力**: トップレベルの多相関数と多相 ADT を型パラメータ付きで出力する。本体は既存実装（`poly_functions` / `variant_named_field` golden）を `poly_adts` golden・評価一致テストで確定。監査で見つかった成長する多相再帰（生成 Go が instantiation cycle で拒否される）は E0416 で拒否（[decision](docs/decisions/2026-11-03-generic-instantiation-cycles.md)、[検証](docs/sketch/2026-11-03-p2-5-generic-emission-verification.md)）。依存: P2-2

### P3 Go 相互運用の本格実装

Go 相互運用を変えたら両方向（Gom → Go、Go → Gom）のテストを足す。`nil`、スライス、named type、interface を忘れない。

- [-] **P3-2 named type と interface**: 名前的な型として扱い、`Go.conv` で明示変換、interface は `go/types` の代入規則に従う。依存: P3-1
- [ ] **P3-3 スライス・map・ジェネリック関数**: `List` との境界コピー、`Go.Slice`、`Go.Map`、制約が `any` / `comparable` のジェネリック関数。依存: P3-1, P2-5
- [ ] **P3-4 メソッド呼び出しと `_` プレースホルダ**: `_` 以外の引数を作成時に評価する（[spec 8.2](docs/spec.md)、[decisions/cute-placeholder](docs/decisions/2026-11-01-cute-placeholder.md)）。依存: P3-1, P2-2
- [ ] **P3-5 `extern` と標準のバインディング**: 型と変換規則の上書き、comma-ok の `Option` 化（[spec 8.4](docs/spec.md)）。依存: P3-1
- [ ] **P3-6 Go から Gom を呼ぶ**: 生成 API を Go のテストから使うテスト、ADT の不正値（`nil`）に対する panic のテスト（[spec 8.6, 8.7](docs/spec.md)）。依存: P3-2, P3-3
- [ ] **P3-7 P3 の完了確認**: `os`、`io`、`strconv`、`encoding/json` などを使うプログラムが動く。依存: P3-2, P3-3, P3-4, P3-5, P3-6

### P4 公開 API と開発体験

- [x] **P4-1 `.gomi` の検査と `gom info`**: 実装とインターフェースの照合、下書き生成、正規化した構文木からのハッシュ（[spec 7 章](docs/spec.md)、[decisions/gomi-interface-files](docs/decisions/2026-11-01-gomi-interface-files.md)）。moon の慣習に合わせサブコマンド名は `gomi` ではなく `info`（`moon info` が .mbti を生成するのと同じく成果物の役割で命名）。CLI 体系の整理方針は [sketch/moon-cli-command-structure](docs/sketch/2026-11-05-moon-cli-command-structure.md)（`build check test fmt fix explain schema dump` + `new`/`version`、registry 系は Go に任せる、write-if-different、`gom-*` 外部 dispatch）。依存: P2-1 ✅
- [x] **P4-3 ファイル内テストと `gom test`**: `_test.go` の出力、`go test -json` の変換、`assert_eq` 用の表示関数の自動生成（[spec 11 章](docs/spec.md)、[design.md 4.8](docs/design.md)）。依存: P1-13
- [x] **P4-4 `gom fix` / `gom explain` / `gom schema`**: `safe` な fixes だけを適用、エラーコード表からの説明、メッセージの JSON Schema。依存: P1-10, D-2 — `internal/diag` に `applied`/`explanation`/`ApplyEdits`/`MessageSchema`、E0402 に初の safe fix（`ignore (...)` で囲む）。fix は診断ごとに最初の safe fix をアトミックに適用（編集は入力ファイル内・非重複のみ）。wire 形状と適用ポリシーは [decisions/gom-fix-explain-schema](docs/decisions/2026-11-03-gom-fix-explain-schema.md)、検証は [sketch](docs/sketch/2026-11-03-gom-fix-explain-schema-verification.md)。残課題: スキップされた fix の報告、クロスファイル編集、fixpoint、`-dry-run`、`test` kind のスキーマ厳密化
- [x] **P4-5 ソース位置の対応表**: `//gomsrc` センチネルのアンカー表を `//line` 書き換えと同一操作で構築し `build.Result.Output.{SourceMap,TestSourceMap}` で公開（P4-6 の内部 API 境界）。`GoPosIndex`/`GoToolDiagnostics`/`ResolveStack` が `go build`/`go vet`/`go test` の `file:line:col` 出力と panic スタックを `.gom` 位置へ戻す。`Input.Path` の制御文字は E0002 で拒否（ファイル名インジェクション対策）。全 23 golden のアンカーを go/token の `//line` 解釈と照合、実 `go run` panic の E2E 済み（[decision](docs/decisions/2026-11-03-source-location-correspondence.md)、[検証](docs/sketch/2026-11-03-source-location-mapping.md)）。依存: P1-11
- [ ] **P4-6 LSP**: 型表示、網羅性エラーの即時表示、UTF-16 位置への変換。依存: P4-5
- [ ] **P4-7 インクリメンタルビルドのキャッシュ**: キャッシュキーは [design.md 5 章](docs/design.md)。依存: P4-1
- [x] **P4-8 標準ライブラリ `stdlib/`**: `list`、`option`、`result` を Gom で書き、`.gomi` を付けてコンパイラ自身でビルドする。依存: P4-1, P2-5 — `.gomi` の公開名は `Exports`/`Exported` フックでエクスポート名に落とし、`internal/stdlib` が埋め込み `.gomi` を一度だけコンパイルして組み込みモジュール表・シグネチャ・arity を供給。`rt` の境界ヘルパは `stdlib/{result,option}` へ移して葉化。`gom check/build/test` と spec 冒頭例の実行で検証。[decision](docs/decisions/2026-11-03-stdlib-packaging.md)、[検証](docs/sketch/2026-11-03-stdlib-gom-built.md)
- [x] **P4-9 LLM 向け言語ガイド skill `gom-agent-guide`**: MoonBit の `moonbit-agent-guide` 方式で `skills/gom-agent-guide/`（canonical、`npx skills` 互換の実ファイル）に SKILL.md(~500-800行, frontmatter は name+description,description を `.gom`/`.gomi`/`gom` コマンドで implicit 発火)+ `references/`(language-fundamentals, go-interop, concurrency, interface-files, diagnostics, project-layout)を作り、`.devin/skills/` へ vendor。AGENTS.md に「`.gom` を書くなら先に `$gom-agent-guide`」の routing 節を追加。skill 内の ```gom コード例は testdata 抽出テストで `gom check` 検証する（MoonBit の SKILL.mbt.md CI と同じ発想）。`gom new` 生成 AGENTS.md テンプレート（薄いルーター）は `gom new` 自体の実装時の別タスク。設計の正: [docs/sketch/2026-11-02-agent-skill-for-gom.md](docs/sketch/2026-11-02-agent-skill-for-gom.md)。依存: なし — `skills/gom-agent-guide/` + `make vendor-skills` + `skills/snippets_test.go`（[decision](docs/decisions/2026-11-03-agent-skill-example-verification.md)、[検証](docs/sketch/2026-11-03-agent-skill-verification.md)）

### P5 並行処理と最適化

- [ ] **P5-1 `go` 式・チャネル型・`select` 式・`Task`**: [spec 10 章](docs/spec.md)、[decisions/concurrency-model](docs/decisions/2026-11-01-concurrency-model.md)、[select-receive-arms](docs/decisions/2026-11-01-select-receive-arms.md)。依存: P2-2
- [ ] **P5-2 可変な値の送信・捕捉に対するリンター警告**。依存: P5-1
- [ ] **P5-3 生成コードの最適化と性能検証**: `List.map` などの特殊化、ベンチマーク、エスケープ解析、`go test -race`。依存: P4-8

### 検証

- [-] **V-4 follow-up（レビュー軽微指摘）**: witness 0 件プログラムが ~40%（decl のみ/型宣言のみ — infer スイープに最小 decl 数を要求する選択肢）、`witnessedNode` の tie-break が map 反復で非決定的（infer_test.go:258）、`parseWant` が `->` を識別子として受理（latent）。ブロッカー無し・merge 済み（ab3f235）
- [x] **V-1 実行可能チュートリアル**: `tutorial/` に `.gom` のサンプル群と実行手順を作り、`gom build` / `gom check` / `gom test` で実際に動かす。エンドユーザー視点で不備・分かりにくい診断・壊れた UX を洗い出し、見つけたものは TODO.md の Bugs/タスクに追加してから実装に戻る。依存: P1-13
- [x] **V-2 grammar ベースのランダムプログラム生成器**: `docs/grammar.ebnf`（または tree-sitter 文法を別途整備して二重実装相互検証）から妥当な `.gom` を生成し、パーサー fuzz・compile-canary・eval vs compiled 差分テストのシードとする。意味論の食い違い（評価順・`|?>`・構造的 `==`）を先回りで発見するためのもの。依存: なし（seed 消費側は parser/compile fuzz があると効果大） — `internal/gomgen` + `internal/build/gen_test.go`。差分テスト seed 化は V-3 で配線済み
- [x] **V-3 gomgen → eval-vs-compiled 差分 fuzz の配線**: 文法的に妥当な生成プログラムを eval とコンパイルの両方で実行し、stdout/診断を差分比較する（V-2 の意図的繰越分）。評価順・意味論乖離の検出器。決定性が必要なので `go{}`・`select`・非決定的組み込みを生成から除く deterministic サブセットモードを gomgen に追加する形が自然。差分は既存の knownBugs ではなく別レジストリ（divergence はバグ確定ではなく両側の仕様確認が要るため）か、比較結果を分類して既知のものだけ抑制する。依存: V-2 ✅（`gomgen.Options.Deterministic` + `internal/build/diff_test.go` で実装。200 seed/mode 実測: typed 41・mechanical 65 が clean 比較に到達。既知 divergence は knownDivergences レジストリで抑制）
- [x] **V-4 HM 推論のプロパティテスト**: gomgen typed モードが生成する型注釈をオラクルに `check` の推論結果と照合する（typed 生成が「この式は T を持つ」と保証しているのに check が別の型/E0401 を出せば推論バグ）。P2-1 着地で実行可能になった。依存: V-2 ✅, P2-1 ✅ — `gomgen.GenerateResult` が `Witness`（バイト span + 狙った表面型）と `Guaranteed`（deliberate near-miss "slip" が無ければ真）を返し、`internal/build/infer_test.go` が witness span を包含する最外型付きノードの推論型と instance 照合（一般化変数は任意インスタンスとして受理、未解決メタ/構造不一致は失敗）。`GOM_INFER_SEEDS` で長い opt-in sweep（既定 60/mode）。1000 seed/mode 実測: typed 908 accepted・typed-det 840 accepted、witness mismatch・guaranteed 診断ともに 0 件。持ち上げた失敗は全て generator 側の contract 違反（`-(e)` の binary minus 化、`...any` 位置の `Go.Any` スタンプ、ph メタ漏れ）で、check 側の推論バグは未登録（knownInferBugs は空の capped registry）。[decision](docs/decisions/2026-11-03-inference-witness-oracle.md)、[検証ログ](docs/sketch/2026-11-03-inference-witness-oracle.md)
- [x] **V-5 `gom fmt` の継続 fuzz を gomgen seed で拡張**: formatter fuzz の入力を grammar 生成器由来に広げてカバレッジを上げる（手書き corpus では到達しにくい構文の組み合わせを網羅）。冪等性・往復一致のプロパティを生成入力で回し続ける。依存: V-2 ✅, P4-2 — `internal/format/gen_test.go`: `FuzzFormatGen`（生成プログラムをシードにした byte 変異 fuzz）+ `TestGeneratedFormat`（`GOM_FMTGEN_SEEDS` で深さ調整、既定 60 seed/mode の bounded sweep）。契約は FuzzFormat + spec 1.7 の構文木一致（`normSyntax` を per-decl dump に作り替えてコメント/blank trivia と import 巻き上げによる false positive を排除）に拡張。200 seed/mode 実測で見つかった新規バグは無し、既知の深ネスト指数遅延 bug は typed seed 62/162/168/172/176 の repro を knownGenFormatBugs に Skip 登録（TODO の当該 bug 項目が継続して追跡）。`check-fuzz` にも format ターゲットを追加
- [x] **[task] compile-canary の生成 Go に vet 相当の解析を追加**: 現在の canary は in-process go/types で型検査のみ。shadow・到達不能コード等を先回り検出したい。方式の決定が要る: (a) `golang.org/x/tools/go/analysis` — stdlib-only の repo 初の外部依存になる、(b) unreachable/shadow の小さな AST 歩行を自作（scope 追跡が正確に要る）、(c) sandbox 内で実 `go vet` を temp module に実行（重い）。`(a)` の依存追加は decisions 経由で → (b) を採用: `internal/fuzzutil` `VetGenerated` が go/ast＋既存の型検査で得る go/types 情報だけで unreachable・assign（自己代入）・unusedresult・stringintconv・shadow の 5 パスを移植（外部依存・subprocess なし）。`unreachable`/`shadow` は AST のみで toolchain 不在時も動く。初回スイープで TailCall の `p = p` 自己代入を検出し codegen 側で修正（`internal/codegen/emit.go` `tailCall`、`testdata/golden/tailcall_unchanged_args`）、副産物で定数除算ゼロの既存バグを発見・登録（[decisions/canary-vet-analysis](docs/decisions/2026-11-02-canary-vet-analysis.md)）
- [ ] **V-6 複数ファイル + `.gomi` の縦断検証**: 2 つの `.gom` と 1 つの `.gomi` からなる小さなライブラリを作り、別パッケージの Gom と Go の双方から呼ぶ（公開関数・ADT・関数値を1つずつ含める）。同一パッケージ複数ファイルのスコープ共有は open-questions の未決事項で、名前解決・型の同一性・初期化順・生成名衝突に影響するため `.gomi` 実装と併せて早めに決める（セカンドオピニオンの推奨到達点）。依存: P4-1（`.gomi`）、同一パッケージ複数ファイルは部分実装で先行可

## Bugs

（見つけたバグはここに `- [ ]` で足す）

- [x] **[task] spec の例のうち現状コンパイルできないものを整理する**: spec.md 冒頭例（`List.map`/`List.sum`、P4-8 未実装）、8.1 `report` 例（`ignore` 未実装 + `strings.Join` への `List` 引数、P3-3 未実装）、10 章（`Option.get`/`Chan`/`Task`）。「将来の例」と注記するか実装タスクへ紐付ける（V-1 で確認） — 全例を実測し失敗する 7 ブロックに注記を追加。`ignore`・`_`・`|?>`・`select` は実装済み。新たに `ctx.Done()` の `struct{}` 非対応と `Result Int64` 例の表記ずれを gaps へ登録。[監査](docs/sketch/2026-11-03-spec-examples-audit.md)
- [ ] **[bug] extern 内の `Option (Ptr Flag)` が意味不明な arity 診断になる**: `extern go "flag" { fn Lookup(name: String) -> Option (Ptr Flag) }` が `` `Ptr` takes 3 type argument(s), got 0 `` とだけ報告する（3 引数の型コンストラクタは存在しない）。実際は `*T` → `Option (Ptr T)` の変換規則上書きが未実装（P3-5）の範囲だが、診断自体が壊れているので P3-5 着手時に経路と文言を直す。[監査](docs/sketch/2026-11-03-spec-examples-audit.md)
- [ ] **[bug] 遅延束縛レシーバ上の Go メソッド呼び出しが誤分類される**: `fn apply3 (f: (time.Time -> time.Time)) (t: time.Time) -> time.Time = f t` + `apply3 (fn x -> x.AddDate 1 2 3) t` で `x.AddDate` のレシーバ型が適用チェック時点では未解決メタ → `MemberExpr.Sym=nil` → `calleeOf` が CallUnknown に倒し Gom 規約で引数を処理（`x.M ()` は Unit 引数化、複数引数はカリー化、`~l:` は E0408、spread は E0505）。`revisitSelects` は member だけを後付け解決し Apply 形状は戻せない → E0901×3 または誤 E0401。main では panic だった（改善だが残存）。修正は apply 分類の遅延化が要るため P2-1 の推論層の仕事（fix-lower-bugs レビューで発見。`x.f`/`x.M a`（位置1個）は動く。`{Go.conv}` も同族の E0901 "member builtin is not an expression" で未登録）。セカンドオピニオンからの構造論: 型が不明な段階で呼び出し方法を確定する設計が弱点 — 推論中は未解決の適用情報を保持し、制約解決後にメソッド選択・呼び出し規約・型引数・引数対応を一括確定する工程を設け、「lower に渡す時点で全件確定済み」の契約にすると個別修復を減らせる
- [ ] **[divergence] Float リテラル演算が生成 Go の定数畳み込みで評価器と値がずれる**: `"${869.32 * 38.29}"` は eval がリテラルごとに float64 化してから乗算するため `33286.262800000004` を出すが、生成 Go は untyped 定数式のまま出力するため Go の任意精度定数畳み込み＋単一丸めで `33286.2628` になる。同じ `'g' -1` 表示を通るのに値自体がずれる。spec の Float 演算の意味を決める必要がある: IEEE754 ベースに寄せるなら codegen がオペランドを非定数化（変数経由か変換の挿入）する必要があり、Go の定数意味論に寄せるなら eval の各演算を定数畳み込み順に直す必要がある（V-3 の eval-vs-compiled 差分テストで発見、knownDivergences 登録済み。[docs/problems/2026-11-02-float-const-fold-arithmetic.md](docs/problems/2026-11-02-float-const-fold-arithmetic.md)）
- [ ] **[divergence] 異なるバリアント値への名前付きペイロードアクセス `.f` で eval とコンパイルがずれる**: `fn getr (s: Shape) -> Float = s.r`（`Shape = | Circle (r: Float) | Rect (w: Float) (h: Float) | Empty`）に `Rect 2.0 3.0` を渡すと、eval は index 0 のスロット `w` の値 `2.0` を返すのに対し、コンパイル側は `s.(Shape_Circle).R` の型アサーションで panic する（`Empty` では eval も E9004 "variant has no such field" で失敗するが、コンパイル panic と失敗の形が違う）。check は「そのフィールド名を持つバリアントがちょうど 1 つ」のときだけ `.f` を受理するので、所有者が静的に証明できないケースの意味を spec が定めていないのが根。アサーション意味（違約 = panic、match の DPanic 系と同系）を spec に明文化して eval 側も ctor 一致を検査するのが自然案。生成コード側は `core.FieldGet.Ctor` でアサート対象を IR 確定済み（[docs/problems/2026-11-02-variant-field-wrong-variant.md](docs/problems/2026-11-02-variant-field-wrong-variant.md)。fix-diff-bugs の `s.r` 回帰修正中に検証で発見）
- [ ] **[quality] E0415 の診断品質フォローアップ（fix-local-result レビューの nice-to-have）**: ① `try` が unbound-meta オペランドで E0415 を二重報告（`resultArgs` Meta ケースと `isShadowedCon` の両方）② `|?>` 経路でも `resultArgs` が `` `try` `` と誤った構文名を報告（construct 名を引数化する）③ `select` の channel オペランドが `type Chan` シャドウ時に E0415 ではなく E0401（`for` の `shadowedBuiltin` 分岐相当が recv/send アームにない）④ `for` の E0415 メッセージが "`for` iterates the builtin `List` or a channel type needs the builtin `Chan`" と連結が不自然 ⑤ E0415 後のカスケード（shadowed-bound meta の `markQuiet` 等で E0401 連鎖を減らせる余地）と `shadowedInType` が最初の1件しか報告しない

- [-] **[task] Gist CLIのGo境界**: byte I/Oと`Go.Slice String`→`List String`を実装して残りのexample-local adapterを縮小する。最小再現と必要操作は[Gist prototype](docs/sketch/2026-11-03-gist-cli-prototype.md)、関連: P3-2/P3-3。

- [ ] **[bug] Go.Any へ渡す数値リテラルが Gom の具体型を保たない**: `fmt.Sprintf ("%T", 1)` が `int64` ではなく `int` を返す。既存の `ConvBoxAny` 経路でも boxing 前に Gom の数値型を具体化し、直接リテラル・束縛・数値演算・Go 側の型 switch を実行検証する。[再現](docs/sketch/2026-11-03-parallel-implementation-round.md#goany-numeric-probe)。
- [ ] **[bug] Floatへ推論された整数リテラル束縛がGo intで生成される**: `fn f () -> Float = { let n = 1; n + 0.5 }` は型検査を通るが `n := 1` がGoのintとなり、生成Goが `0.5 truncated to int` で拒否される。lowerで確定した束縛の型を生成時に保持し、単体・interface引数の後にFloatへ解決される場合の実行回帰を追加する。[再現](docs/sketch/2026-11-03-go-http-boundary-verification.md)、[既存懸念](docs/problems/2026-11-02-let-int-literal-go-type.md)。
- [ ] **[task] SourceMap / GoPos 系のテスト網羅を広げる（P4-5 レビュー残件）**: test 宣言を持つ golden が `builtins.gom` 1 件のみで `TestSourceMap` の実パイプライン掃引が薄い。`GoToolDiagnostics` の multi-line chunk・複数 `.gom` 入力・別 dir 同名相対パス、`ParseGoErrorLine` の `file:line:`（末尾コロン・msg 空）・msg 内 `:` 含有も未検証。[レビュー記録](docs/sketch/2026-11-03-source-location-mapping.md)の Known limitations 参照。
- [ ] **[task] stdlib 生成成果物の鮮度ゲート**: `.gom` を編集して生成 `*.go`/`*_test.go` の再生成を忘れてもコミット済み成果物で CI が通ってしまう。`internal/stdlib` に「`.gom` を再コンパイルしてコミット物と一致するか」のテスト、または CI 相当の検査を足す（P4-8 レビュー残件）。
- [ ] **[task] stdlib 周辺のテスト穴を埋める（P4-8 レビュー残件）**: nil `Option` の `IsSome` が `InvalidVariant` panic になる pin、境界ヘルパ（`result.FromGo`/`FromErr`/`AssertOk`/`Unwrap`/`ToGo`、`option.FromGo`、`list` の FromSlice/ToSlice 独立性）の単体テスト、`E0505_list_spread_mid`・`stdlib_partial`（`let m = List.map f` 型の部分適用）fixture。併せて `stdlib/gomis.go` の embed 列挙と `internal/stdlib.packages` の同期ずれを検査する guard（第4パッケージ追加時の2箇所更新忘れ防止）。
