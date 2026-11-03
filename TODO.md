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

- [x] **P2-2 多相関数・部分適用・クロージャ**: 関数値の表現は P0-2 の結論に従う。多相な定義の具体化とラッパー生成を Core IR で確定させる。依存: P2-1, P0-6, P1-13
  - [x] **P2-2c タプル等に格納した循環関数値の初期化循環を解消する**: `fn count (n: Int) -> Int = if n == 0 { 0 } else { again (n - 1) + 1 }; let (again, _) = (count, 0)` が check/build を通るが、生成 Go は hidden tuple → count → again → hidden tuple の initialization cycle になる。集約値の初期化の副作用・依存順を保つ表現を Core IR で決め、生成コードの実行回帰を追加する。[再現と範囲](docs/sketch/2026-11-03-static-function-alias-verification.md)。依存: P2-2b
- [-] **P2-4 `try` と `|?>`**: 脱出境界は最も内側の関数（[decisions/try-boundary](docs/decisions/2026-11-01-try-boundary.md)）、`|?>` の `%w` 自動ラップ（[spec 9 章](docs/spec.md)）。中間の `Result` を作らない生成（design.md 4.6）。依存: P2-1, P1-13
  - [ ] **P2-4c unguarded ステップの `EscapeCall` 融合（残る最適化）**: unguarded ステップの引数が Go `(T,error)` 呼び出しの場合、`ResultFromGo` で物質化してから次セグメントの `EscapeResult` が検査するため中間 Result が 1 個残る。引数評価（try を含むのでユーザー側の列）と呼び出し本体（セグメント内 `EscapeCall`）を分離すれば融合可能。併せて `pipeRest` の span 不統一（束縛は `lv.Y.Span()`・enter escape は `lv.Sp`）も直す（P2-4b レビュー F5/F6）。依存: P2-4b


### P3 Go 相互運用の本格実装

Go 相互運用を変えたら両方向（Gom → Go、Go → Gom）のテストを足す。`nil`、スライス、named type、interface を忘れない。

- [-] **P3-2 named type と interface**: 名前的な型として扱い、`Go.conv` で明示変換、interface は `go/types` の代入規則に従う。依存: P3-1
- [x] **P3-4 メソッド呼び出しと `_` プレースホルダ**: `_` 以外の引数を作成時に評価する（[spec 8.2](docs/spec.md)、[decisions/cute-placeholder](docs/decisions/2026-11-01-cute-placeholder.md)）。receiver 未解決時の遅延適用は [decisions/deferred-member-application](docs/decisions/2026-11-05-deferred-member-application.md)。連鎖 `x.M(1).N(2)` の parse は残件 → [problem](docs/problems/2026-11-05-method-chain-select-binds-args.md)。依存: P3-1, P2-2
- [x] **P3-5 `extern` と標準のバインディング**: 型と変換規則の上書き、comma-ok の `Option` 化（[spec 8.4](docs/spec.md)）。実装済み: extern は関数メンバーのみ上書き可、`Option` は `(T, bool)` / `*T` のみ許可、不一致は E0509（[decision](docs/decisions/2026-11-03-extern-override-binding.md)、[検証](docs/sketch/2026-11-03-extern-binding-verification.md)）
- [ ] **P3-6 Go から Gom を呼ぶ**: 生成 API を Go のテストから使うテスト、ADT の不正値（`nil`）に対する panic のテスト（[spec 8.6, 8.7](docs/spec.md)）。依存: P3-2, P3-3
- [ ] **P3-7 P3 の完了確認**: `os`、`io`、`strconv`、`encoding/json` などを使うプログラムが動く。依存: P3-2, P3-3, P3-4, P3-5, P3-6
- [ ] **[task] extern の「標準のバインディング」集の整備**: P3-5 で `extern` 機構は実装済みだが、それを使う標準バインディング集（`(T, bool)` → `Option` などの既定上書きの一覧）は未着手。管理場所は [open-questions](docs/open-questions.md) の未決事項。P3-7 の完了確認で `os`/`io`/`strconv`/`encoding/json` のプログラムが extern を必要とするかの判断材料になる。依存: P3-5

### P4 公開 API と開発体験

- [ ] **P4-6 LSP**: 型表示、網羅性エラーの即時表示、UTF-16 位置への変換。依存: P4-5
- [ ] **P4-7 インクリメンタルビルドのキャッシュ**: キャッシュキーは [design.md 5 章](docs/design.md)。依存: P4-1

### P5 並行処理と最適化

- [ ] **P5-1 `go` 式・チャネル型・`select` 式・`Task`**: [spec 10 章](docs/spec.md)、[decisions/concurrency-model](docs/decisions/2026-11-01-concurrency-model.md)、[select-receive-arms](docs/decisions/2026-11-01-select-receive-arms.md)。依存: P2-2
- [ ] **P5-2 可変な値の送信・捕捉に対するリンター警告**。依存: P5-1
- [ ] **P5-3 生成コードの最適化と性能検証**: `List.map` などの特殊化、ベンチマーク、エスケープ解析、`go test -race`。依存: P4-8

### 検証

- [-] **V-4 follow-up（レビュー軽微指摘）**: witness 0 件プログラムが ~40%（decl のみ/型宣言のみ — infer スイープに最小 decl 数を要求する選択肢）、`witnessedNode` の tie-break が map 反復で非決定的（infer_test.go:258）、`parseWant` が `->` を識別子として受理（latent）。ブロッカー無し・merge 済み（ab3f235）
- [x] **V-6 複数ファイル + `.gomi` の縦断検証**: 2 つの `.gom` と 1 つの `.gomi` からなる小さなライブラリを作り、別パッケージの Gom と Go の双方から呼ぶ（公開関数・ADT・関数値を1つずつ含める）。同一パッケージ複数ファイルのスコープ共有は open-questions の未決事項で、名前解決・型の同一性・初期化順・生成名衝突に影響するため `.gomi` 実装と併せて早めに決める（セカンドオピニオンの推奨到達点）。依存: P4-1（`.gomi`）、同一パッケージ複数ファイルは部分実装で先行可
  対応: パッケージをコンパイル単位とする決定（resolve.Files / check.Files / build.CompileFiles）を実装し、`internal/gomimport` で `.gomi` 経由の Gom パッケージ import を実装。`testdata/multifile/` fixture + `TestEndToEnd_MultifilePackage` で `gom check`→`gom build`→`go build`/`go test`/`go run` まで検証。残件は [problems](docs/problems/2026-11-05-gom-package-imports.md)。
  参照: [decision](docs/decisions/2026-11-05-package-compilation-unit.md)、[検証](docs/sketch/2026-11-05-multifile-package-verification.md)。
- [ ] **[task] V-6 残件（パッケージ単位コンパイルの境界）**: ① `pkg.Type.Ctor` 修飾綴りは未実装（resolve に per-type メンバー表が要る）② 相互 `.gom` import の循環は `gom check`/`build` を通り `go build` の import cycle としてしか検出できない（`.gomi` 内 import・循環は loader が機械処理、spec §7 の規定は未記述）③ `gom dump` は単一ファイルのみでマルチファイルパッケージのメンバーに誤解を招く E0201 を出す。④ 同名 `test` ブロックの兄弟ファイル衝突は `TestX` が Go 側重複になる（問題記録: [problems](docs/problems/2026-11-05-gom-package-imports.md)）。依存: V-6

## Bugs

（見つけたバグはここに `- [ ]` で足す）

- [x] **[bug] extern 内の `Option (Ptr Flag)` が意味不明な arity 診断になる**: 修復済み（P3-5）。原因は二重: ①extern 型位置で `flag.String`（func）が builtin `String` を覆い隠し ②その func シグネチャが型コンストラクタの arity と読み違えられた。resolve は extern の型表を KindType メンバーに限定し、check は型位置の項シンボルを E0201 "is a func, not a type" で報告する。`Option (Ptr Flag)` の変換規則上書きも実装（`option.FromGo(v, v != nil)`）。[監査](docs/sketch/2026-11-03-spec-examples-audit.md)、[検証](docs/sketch/2026-11-03-extern-binding-verification.md)
- [x] **[bug] 遅延束縛レシーバ上の Go メソッド呼び出しが誤分類される**: `fn apply3 (f: (time.Time -> time.Time)) (t: time.Time) -> time.Time = f t` + `apply3 (fn x -> x.AddDate 1 2 3) t` で `x.AddDate` のレシーバ型が適用チェック時点では未解決メタ → `MemberExpr.Sym=nil` → `calleeOf` が CallUnknown に倒し Gom 規約で引数を処理（`x.M ()` は Unit 引数化、複数引数はカリー化、`~l:` は E0408、spread は E0505）。`revisitSelects` は member だけを後付け解決し Apply 形状は戻せない → E0901×3 または誤 E0401。main では panic だった（改善だが残存）。修正は apply 分類の遅延化が要るため P2-1 の推論層の仕事（fix-lower-bugs レビューで発見。`x.f`/`x.M a`（位置1個）は動く。`{Go.conv}` も同族の E0901 "member builtin is not an expression" で未登録）。セカンドオピニオンからの構造論: 型が不明な段階で呼び出し方法を確定する設計が弱点 — 推論中は未解決の適用情報を保持し、制約解決後にメソッド選択・呼び出し規約・型引数・引数対応を一括確定する工程を設け、「lower に渡す時点で全件確定済み」の契約にすると個別修復を減らせる — P3-4 の deferred member application（pendingApplies + replay）でこの構造を実現し解消（[decision](docs/decisions/2026-11-05-deferred-member-application.md)）。残る `{Go.conv}` サブケースは別エントリへ分離
- [ ] **[bug] `{Go.conv}` のメンバー風記述が E0901 "member builtin is not an expression" になる**: 遅延束縛バグと同族として記録されていたが、原因はメソッド分類ではなく conv バリデーション経路の問題（P3-4 本筋とは別経路、修正未着手）
- [ ] **[divergence] Float リテラル演算が生成 Go の定数畳み込みで評価器と値がずれる**: `"${869.32 * 38.29}"` は eval がリテラルごとに float64 化してから乗算するため `33286.262800000004` を出すが、生成 Go は untyped 定数式のまま出力するため Go の任意精度定数畳み込み＋単一丸めで `33286.2628` になる。同じ `'g' -1` 表示を通るのに値自体がずれる。spec の Float 演算の意味を決める必要がある: IEEE754 ベースに寄せるなら codegen がオペランドを非定数化（変数経由か変換の挿入）する必要があり、Go の定数意味論に寄せるなら eval の各演算を定数畳み込み順に直す必要がある（V-3 の eval-vs-compiled 差分テストで発見、knownDivergences 登録済み。[docs/problems/2026-11-02-float-const-fold-arithmetic.md](docs/problems/2026-11-02-float-const-fold-arithmetic.md)）
- [ ] **[divergence] 異なるバリアント値への名前付きペイロードアクセス `.f` で eval とコンパイルがずれる**: `fn getr (s: Shape) -> Float = s.r`（`Shape = | Circle (r: Float) | Rect (w: Float) (h: Float) | Empty`）に `Rect 2.0 3.0` を渡すと、eval は index 0 のスロット `w` の値 `2.0` を返すのに対し、コンパイル側は `s.(Shape_Circle).R` の型アサーションで panic する（`Empty` では eval も E9004 "variant has no such field" で失敗するが、コンパイル panic と失敗の形が違う）。check は「そのフィールド名を持つバリアントがちょうど 1 つ」のときだけ `.f` を受理するので、所有者が静的に証明できないケースの意味を spec が定めていないのが根。アサーション意味（違約 = panic、match の DPanic 系と同系）を spec に明文化して eval 側も ctor 一致を検査するのが自然案。生成コード側は `core.FieldGet.Ctor` でアサート対象を IR 確定済み（[docs/problems/2026-11-02-variant-field-wrong-variant.md](docs/problems/2026-11-02-variant-field-wrong-variant.md)。fix-diff-bugs の `s.r` 回帰修正中に検証で発見）
- [ ] **[quality] E0415 の診断品質フォローアップ（fix-local-result レビューの nice-to-have）**: ① `try` が unbound-meta オペランドで E0415 を二重報告（`resultArgs` Meta ケースと `isShadowedCon` の両方）② `|?>` 経路でも `resultArgs` が `` `try` `` と誤った構文名を報告（construct 名を引数化する）③ `select` の channel オペランドが `type Chan` シャドウ時に E0415 ではなく E0401（`for` の `shadowedBuiltin` 分岐相当が recv/send アームにない）④ `for` の E0415 メッセージが "`for` iterates the builtin `List` or a channel type needs the builtin `Chan`" と連結が不自然 ⑤ E0415 後のカスケード（shadowed-bound meta の `markQuiet` 等で E0401 連鎖を減らせる余地）と `shadowedInType` が最初の1件しか報告しない

- [-] **[task] Gist CLIのGo境界**: byte I/Oと`Go.Slice String`→`List String`を実装して残りのexample-local adapterを縮小する。最小再現と必要操作は[Gist prototype](docs/sketch/2026-11-03-gist-cli-prototype.md)、関連: P3-2/P3-3。

- [ ] **[bug] Go.Any へ渡す数値リテラルが Gom の具体型を保たない**: `fmt.Sprintf ("%T", 1)` が `int64` ではなく `int` を返す。既存の `ConvBoxAny` 経路でも boxing 前に Gom の数値型を具体化し、直接リテラル・束縛・数値演算・Go 側の型 switch を実行検証する。[再現](docs/sketch/2026-11-03-parallel-implementation-round.md#goany-numeric-probe)。
- [ ] **[bug] Floatへ推論された整数リテラル束縛がGo intで生成される**: `fn f () -> Float = { let n = 1; n + 0.5 }` は型検査を通るが `n := 1` がGoのintとなり、生成Goが `0.5 truncated to int` で拒否される。lowerで確定した束縛の型を生成時に保持し、単体・interface引数の後にFloatへ解決される場合の実行回帰を追加する。[再現](docs/sketch/2026-11-03-go-http-boundary-verification.md)、[既存懸念](docs/problems/2026-11-02-let-int-literal-go-type.md)。
- [ ] **[task] SourceMap / GoPos 系のテスト網羅を広げる（P4-5 レビュー残件）**: test 宣言を持つ golden が `builtins.gom` 1 件のみで `TestSourceMap` の実パイプライン掃引が薄い。`GoToolDiagnostics` の multi-line chunk・複数 `.gom` 入力・別 dir 同名相対パス、`ParseGoErrorLine` の `file:line:`（末尾コロン・msg 空）・msg 内 `:` 含有も未検証。[レビュー記録](docs/sketch/2026-11-03-source-location-mapping.md)の Known limitations 参照。
- [ ] **[task] stdlib 生成成果物の鮮度ゲート**: `.gom` を編集して生成 `*.go`/`*_test.go` の再生成を忘れてもコミット済み成果物で CI が通ってしまう。`internal/stdlib` に「`.gom` を再コンパイルしてコミット物と一致するか」のテスト、または CI 相当の検査を足す（P4-8 レビュー残件）。
- [ ] **[task] stdlib 周辺のテスト穴を埋める（P4-8 レビュー残件）**: nil `Option` の `IsSome` が `InvalidVariant` panic になる pin、境界ヘルパ（`result.FromGo`/`FromErr`/`AssertOk`/`Unwrap`/`ToGo`、`option.FromGo`、`list` の FromSlice/ToSlice 独立性）の単体テスト、`E0505_list_spread_mid`・`stdlib_partial`（`let m = List.map f` 型の部分適用）fixture。併せて `stdlib/gomis.go` の embed 列挙と `internal/stdlib.packages` の同期ずれを検査する guard（第4パッケージ追加時の2箇所更新忘れ防止）。
