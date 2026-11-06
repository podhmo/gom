# 仕様・実装・文書のずれ

このリポジトリで見つけた「こことここがずれている」を残す台帳。
セッションをまたいで使う。仕様と実装だけでなく、文書同士、評価器と
生成コード、説明とテストのずれも含む。この台帳自体は規範ではない。

## 使い方

- 見つけたら末尾に `- [ ] **短い説明**` と該当箇所へのリンクを足すだけでよい。
  原因・優先度・修正方針・担当・番号付けは登録の条件にしない。
  未確認なら「未確認」と書いて登録してよい。既存項目があれば追記する。
- 解消したら `[x]` にし、「何を合わせたか」と検証・変更へのリンクを1行足す。
  消さずに残す。作業中という理由だけでは解消扱いにしない。
- ここで正しい側を決めない。規範の所在は [AGENTS.md](../AGENTS.md) に従う。
  決定が必要なら decisions の draft → prototype → 検証の手順を使う。
- 実装する作業は [TODO.md](../TODO.md)、詳しい懸念・検証は problems / sketch に置き、
  台帳からリンクする。同じ説明を長く複製しない。台帳だけのメモでもよい。
- 未解消だけを探すときは `rg '^- \[ \]' docs/implementation-gaps.md`。

追記例: `- [ ] **短い説明**: A にはこう書かれているが、B ではこうなっている。`
次の行に `[仕様](spec.md)`、`[実装](../internal/goimport/mapping.go)` などの参照を足す。

## 記録

以下は 2026-11-03 の実装・実行検証で発見、または関連資料から確認したもの。
未解消項目は網羅的な未実装機能一覧ではない。

- [x] **Go パッケージ読み込み方式の説明が古い**: TODO/design は `go/packages`、
  採用済み decision と実装は `go list -export` + 共有 `go/importer` だった。
  対応: 既存 decision に説明を合わせ、型同一性と別モジュール取り込みを実行検証。
  参照: [decision](decisions/2026-11-03-import-go-loading-mechanics.md)、[検証](sketch/2026-11-03-go-import-session-audit.md)。

- [x] **`error` の Go alias だけ Result 対応から外れる**: spec 8.3 の `error` 戻り値の規則に対し、
  `type Failure = error` の戻り値はタプル等に分類されていた。
  対応: alias を正規化して分類し、単一・末尾戻り値と Go → Gom → Go を検証。
  参照: [spec 8.3](spec.md#83-型の対応)、[検証](sketch/2026-11-03-go-import-session-audit.md)。

- [x] **トップレベルの関数別名から型変数が漏れる**: 一般化しない `let` が他の宣言の型変数を保持し、
  check/build が成功しても生成 Go は未定義の型パラメータで失敗していた。
  対応: 仕様の一般化範囲を変えず E0412 で拒否し、正常な別名・多相関数も検証。
  参照: [spec 3.3](spec.md#33-型推論の範囲)、[検証](sketch/2026-11-03-poly-functions-verification.md)。

- [x] **単純な循環関数別名が生成 Go の初期化循環になる**: check/build は成功するが、
  `fn f (x: Int) -> Int = g x; let g = f` の生成 Go はコンパイルできなかった。
  対応: Core IR で純粋な関数別名を wrapper にし、評価時機も実行検証。集約値経由は別項目。
  参照: [検証](sketch/2026-11-03-static-function-alias-verification.md)。

- [x] **集約値経由の循環関数値は初期化循環が残る**: 単純別名は直ったが、
  `let (again, _) = (count, 0)` を関数本体から参照すると check/build 成功後に Go が拒否する。
  対応: 集約内の関数型束縛を named `ValueAlias` ラッパーに下ろして初期化依存の変数辺を外し、
  残る変数→変数サイクルは lowered Core IR 上の解析で E0907 として拒否。レビューで発見した
  `let g = { …; f }` の初期化式喪失（評価順回帰）は hidden eager var で修正。合法・拒否の
  両形状を実行回帰で検証。
  参照: [decision](decisions/2026-11-05-aggregate-function-bindings.md)、
  [検証](sketch/2026-11-05-aggregate-function-bindings-verification.md)、[TODO P2-2c](../TODO.md)。

- [x] **`|?>` の失敗が外側の関数を脱出する**: spec 9.2 と評価器は Result 値を返すが、
  lower は外側から早期 return し、パイプ外の処理が続かないことがあった。
  対応: 局所境界と明示的 `try` の決定木経路で仕様に合わせ、短絡・継続・error chain を検証。
  参照: [spec 9.2](spec.md#92-意味)、[検証](sketch/2026-11-03-result-pipe-local-boundary.md)。

- [x] **Result パイプの Go 流入だけ数値・Unit の扱いが異なる**: 通常呼び出しでは行う
  `int` → `Int` 変換が抜け、戻り値なし呼び出しを値として代入する生成 Go もあった。
  対応: 通常の境界変換を適用し、数値・多値・Unit の生成 Go を実行検証。
  参照: [検証](sketch/2026-11-03-result-pipe-local-boundary.md)。

- [x] **明示的 `try` を含むパイプは中間 Result が残る**: design 4.6 の中間 Result を省く生成に対し、
  脱出先を保存する保守的な経路には未融合の Result が残る。値の意味は検証済み。
  対応: セグメント融合でステップ数比例の物質化を解消（unguarded try を挟むチェーンは
  セグメント境界の `Ok` 構築のみ残存、意味は不変）。残る最適化余地は TODO へ。
  参照: [design](design.md)、[検証](sketch/2026-11-03-result-pipe-local-boundary.md)、
  [decision](decisions/2026-11-03-pipe-try-segmented-fusion.md)、
  [検証](sketch/2026-11-03-pipe-try-segmented-fusion.md)、[TODO P2-4b](../TODO.md)。

- [x] **Go slice の既定対応が spec と異なる**: spec 8.3/8.5 は `[]T` → `List T` と両方向コピー、
  loader は `Go.Slice T` を返す。`Go.conv os.Args : List String` も E0507。
  対応: シグネチャ境界の `[]T` を `List T` に写し、両方向のコピーを Core Conv
  （`.ToSlice()` / `list.FromSlice`）として生成。`Go.Slice` は明示の no-copy 経路。
  `Go.conv` に `List` との相互変換は足していない（境界では暗黙変換のため不要 — 要 spec 確認）。
  参照: [decision](decisions/2026-11-04-go-collections-and-generics.md)、
  [検証](sketch/2026-11-04-p3-3-go-collections-verification.md)、
  [golden](../testdata/golden/go_slice_boundary.golden.go)。
  追記（P3-2/P3-6 worktree）: `Go.conv` は go/types oracle 導入後 `Go.Slice T ↔ List T` を受理する
  （同一 underlying の raw pun で**コピーしない** — `list.List[T](xs)` キャストを生成）。
  spec 8.5 のコピーは暗黙の境界適合位置のみが `list.FromSlice`/`.ToSlice()` を生成する
  （検証: `testdata/gocall` の `freeze`/`thaw`、[sketch](sketch/2026-11-04-p3-2-p3-6-interop-verification.md)）。
  conv の pun 挙動を spec 明文化するかは open-questions の未決事項。
  追記（2026-11-05）: pun は解消 — `Go.conv` も両方向コピーに。`convKind` が
  `goConvKindOf` を経由して `ConvSliceToGo`/`ConvSliceFromGo` を返す実装
  （golden `go_conv`、gocall `freeze`/`thaw`/`bump`）。spec 8.4 は先行して
  コピーを明記していた。残る pun は要素型不一致対のみ（下の別項目）。

- [x] **Go interface の代入規則が未実装**: spec 8.3 は Go 由来の値を代入規則に従って渡せるとするが、
  `*strings.Reader` → `io.Reader` や `*os.File` → `io.Writer` が E0401 になる。
  対応: Go 由来の引数・注釈境界で代入規則を照会し、明示的 Core 変換を生成。Gist の接続関数も削減。
  参照: [spec](spec.md#83-型の対応)、[検証](sketch/2026-11-03-go-http-boundary-verification.md)。

- [x] **interface へ渡す整数リテラルの動的型が Gom の型と異なる**: HTTP 境界の修正試作を
  独立レビューしたところ、`Int` に解決した `1` が named empty interface へ渡る時に Go の
  `int` となり、想定する `int64` と異なることを `%T` で再現した。
  対応: named interface へ包む前に `ConvNum` で数値型を具体化し、動的型を実行検証。Go.Any は別項目。
  参照: [型の対応](spec.md#83-型の対応)、[レビュー対応と再レビュー](sketch/2026-11-03-go-http-boundary-verification.md)。

- [x] **既存の Go.Any 経路でも整数リテラルの動的型が int になる**:
  `fmt.Sprintf ("%T", 1)` を main `5e792f4` で生成・実行すると `int` を返した。
  Gom `Int` の `int64` 表現と異なり、named interface の修正だけではこの既存経路は直らない。
  対応: `ConvBoxAny` の全 emit サイトで `convBoxSrc` が `ConvNum` で数値型を具体化
  （`any(int64(42))`、`any(float64(x))`）。実行検証は `testdata/gocall` の
  `TestAnyBoxingFromGo`（literal/binding/arithmetic + Go 側型 switch）。
  参照: [検証](sketch/2026-11-04-p3-2-p3-6-interop-verification.md)、
  [lower/expr.go](../internal/lower/expr.go)。

- [x] **Go チャネルを interface に渡せない**: 同じ修正試作で、Go から返る `chan string` を
  named empty interface の引数へ渡すと E0401 になった。Go の代入規則では可能。
  対応: 全方向のチャネルと `chan any` の型復元を追加し、同一性と typed nil を検証。
  参照: [レビュー対応と再レビュー](sketch/2026-11-03-go-http-boundary-verification.md)。

- [x] **型が後で分かる Go field を interface に渡せない**: 同じ試作で、高階関数のラムダ内の
  `model.Accept x.Count` はレシーバの型が後で決まると E0401 になる。
  数値リテラル以外の未解決型についても、field 解決後まで代入判定を待つ必要がある。
  対応: field とその別名は型確定後に代入を検証し、裸のラムダ引数の推論は維持。
  参照: [レビュー対応と再レビュー](sketch/2026-11-03-go-http-boundary-verification.md)。

- [x] **Go struct フィールドの参照が未決・未実装**: HTTP の Header/StatusCode/Body を使いたいが、
  spec には対応表がなく、open-questions に未決として残り、実装は参照を拒否する。
  対応: exported field の読み取りを決定・実装し、Go の selector・nil・評価順・raw 型変換を検証。
  参照: [decision](decisions/2026-11-03-go-interface-assignment-and-fields.md)、[検証](sketch/2026-11-03-go-http-boundary-verification.md)。

- [ ] **Go ジェネリック関数の扱いが文書内でも異なる**: spec 8.3 は未対応、8.5 は
  `any` / `comparable` 制約なら呼べるとするが、loader はすべて非公開にする。
  対応（一部）: `any` / `comparable` 制約のジェネリック関数は宣言順の型引数で呼べる
  （E0508 は comparable 違反）。残るずれ: spec 8.1.6 の例 `slices.Contains` は構造制約
  `~[]E` のため非公開のまま（E0506）— 8.3 の「ジェネリック関数は未対応」という括弧書きも
  `any`/`comparable` が呼べる現状と齟齬。spec の文言修正か制約の拡張が必要。
  参照: [decision](decisions/2026-11-04-go-collections-and-generics.md)、
  [詳細](problems/2026-11-04-generic-go-fns-hidden.md)、
  [検証](sketch/2026-11-04-p3-3-go-collections-verification.md)。

- [x] **戻り値なしの Go 呼び出しを関数末尾に置けない**: Unit として型検査を通る
  `fn stop () -> Unit = os.Exit 1` が `return os.Exit(...)` を生成し Go でコンパイルできない。
  対応: Core の `GoResultsNone` と discard → Unit 正規化で区別し、各値位置・副作用順を検証。
  参照: [Gist probe](sketch/2026-11-03-gist-cli-prototype.md)、[検証と独立レビュー](sketch/2026-11-03-void-go-call-verification.md)。

- [ ] **Go の引数なし関数は値にすると呼べない**: spec は Go 関数を値として公開するが、
  `let f = e.Error; f ()` は直接の `e.Error ()` と異なり E0401 になる。
  参照: [spec 8.1](spec.md#81-go-を呼ぶ)、[再現](problems/2026-11-02-go-nullary-fn-value.md)。

- [x] **Float の評価器と生成 Go で丸めが異なる**: 評価器は各リテラルを float64 にして演算し、
  生成 Go は任意精度定数式を畳み込むため、同じ式の数値・文字列表示が異なる。
  対応: 数値演算は演算子ごとに機械型で 1 回評価する（eval 側の意味）と決定。
  lower がリテラル同士の Float 演算・比較と Int 系 `+ - * /` の左被演算子を
  変数に bind し、生成 Go が定数式にならないようにする（`litFoldDiverges`）。
  Int リテラルオーバーフロー（`9223372036854775807 + 1`）の go build 失敗も解消。
  参照: [decision](decisions/2026-11-05-numeric-arithmetic-per-operation.md)、
  [検証](sketch/2026-11-05-numeric-arithmetic-per-operation.md)、
  [詳細（解消済み）](problems/2026-11-02-float-const-fold-arithmetic.md)。

- [x] **Float に推論された整数リテラル束縛が Go では int のままになる**:
  `fn f () -> Float = { let n = 1; n + 0.5 }` は Float に型解決されるが、
  生成 Go の `n := 1` が int を推論し `0.5 truncated to int` でコンパイルできない。
  対応: codegen/eval/パターンで整数リテラルを型ごとに物質化（`n := float64(1)`）。
  `1 / 2` in Float context は `float64(1) / float64(2)` を出し float 除算を維持。
  参照: [golden](../testdata/golden/float_int_literal.golden.go)、
  [problems（解消済み）](problems/2026-11-02-let-int-literal-go-type.md)。

- [x] **別バリアントのペイロード参照の結果が異なる**: `s.r` を `Rect` に適用すると、
  eval は別名の同じ位置の値を返し、生成コードは型アサーションで panic する。spec の失敗時の意味も未定義。
  対応: assertion 意味を採用 — `.f` はそのフィールドを宣言する唯一のバリアントを
  値が保持することを要求し、違うバリアントは契約違反として panic する
  （eval は E9003、生成コードは既存の型アサーション panic。match の invalid-value
  腕と同じ系統）。spec §3.2/§8.6 に明記。
  参照: [decision](decisions/2026-11-05-variant-field-assert-owning-ctor.md)、
  [検証](sketch/2026-11-05-variant-field-assert-owning-ctor.md)、
  [詳細（解消済み）](problems/2026-11-02-variant-field-wrong-variant.md)。

- [x] **チュートリアルが `ignore` を未実装と説明する**: chapter 1 はすでに `ignore` を使う一方、
  README は未実装とし、他章は `let _ =` を使っている。
  対応: 全章の discard と README を更新し、追加2章を含む12章を実行検証。
  参照: [tutorial README](../tutorial/README.md)、[検証](sketch/2026-11-03-tutorial-iteration.md)。

- [x] **チュートリアルの成功キャッシュがコンパイラ変更を追跡しない**: 外部 CLI 経由だけのテストでは、
  コンパイラを壊しても `go test ./tutorial` が過去の成功を返すことを専用 worktree で再現した。
  API 依存を追加した試作の独立レビューでも、生成コードが使う `stdlib/result` を壊すと
  cached 成功することを再現。コンパイラだけでなく生成コードの実行依存も追跡が必要。
  対応: API/CLI 生成比較と実際の Go モジュール入力の追跡を追加し、コンパイラ・CLI・fixture・
  runtime の破損と runtime ソース追加を検出することを実測。
  参照: [テスト本体](../tutorial/tutorial_test.go)、[独立レビューと追加検証](sketch/2026-11-03-tutorial-iteration.md#fresh-review-and-runtime-input-follow-up)。

- [x] **ignore の problem file が実装済みなのに未解決のまま**:
  problems/2026-11-04-ignore-discard-syntax-unparsed.md は「パーサーに ignore がない（E0201）」
  と主張するが、現在は `ignore` がキーワードとして実装済みで tutorial 全章も使用している。
  ファイル自体の状態が更新されていない。
  対応: problem file を「解消」に更新し、解決の経緯（ignore キーワード実装済み、
  `let _ =` は別形として残存）を追記。
  参照: [problem](problems/2026-11-04-ignore-discard-syntax-unparsed.md)、[lexer](../internal/syntax/token.go)、
  [decision](decisions/2026-11-02-ignore-keyword.md)、[tutorial](../tutorial/01-hello/main.gom)。

- [x] **overapply-labeled-params の problems doc の記述が陳腐化**:
  `mk () 5` が位置引数で `~x` を埋めると説明するが、`flattenFunc` が全
  arrow を畳むため `overApply` の `rparams` 分岐は到達不能になり、
  現在は常に meta 拡張で E0401 になる (spec 2 規則3と結果は整合)。
  到達不能分岐の削除と E0401 文言の是非が残る未決。
  対応: 「到達不能」は誤りだった — `ret` が未束縛 meta のとき引数チェック中に
  Func 型へ束縛され、`id mk 3` が位置引数で `~x` を埋めて通ってしまう
  （`gom check` で実測: `id mk 3` は受理、`mk 3` は E0401）。`rparams` 分岐を
  削除し、extras は戻り値型に対する curried 拡張の unify に一本化 —
  `unifyFunc` の labeled-run 規則が位置引数→labeled を裁くので `id mk 3` も
  E0401 になった。正当な over-application（`id id 3`、`id add3 1 2`）は維持。
  E0401 文言の是非は problem doc が引き続き追跡。
  参照: [problem](problems/2026-11-03-overapply-labeled-params.md)、
  [overApply](../internal/check/apply.go)、
  [fixture](../testdata/spec/E0401_positional_for_labeled_via_meta.gom)。

- [x] **E0404 のコード表の例が発火しない形を示していた**:
  例の `transfer alice ~to: bob` は Gom 呼び出しでは合法な部分適用で、
  E0404 は Go callee でしか発火しない。対応: 例を `extern` 経由の
  Go 呼び出しに差し替えた。Gom 側の文言の未決は problems doc が追跡。
  参照: [error-codes](error-codes.md#e0404--missing-labeled-argument)、
  [problem](problems/2026-11-02-gom-missing-label-is-partial.md)、
  [fixture](../testdata/spec/E0404_go_missing_label.gom)。

- [x] **CLI usage の計画中コマンド名が P4-1 の決定と食い違う**:
  `cmd/gom/main.go` の usage は計画コマンドとして `gomi` を列挙するが、
  TODO P4-1 は `moon info` 流の `info` という名前を決めている（spec 7.5 も `gom gomi` 表記）。
  実装はまだ無いが、名前の正が文書間で不一致。
  対応: 下の [x] 項目と同一内容の重複登録 — `gom info` 実装済みで
  `gomi` はリネーム通知（exit 2）。usage・spec 7.5 とも `gom info` 表記に統一済み。
  参照: [usage](../cmd/gom/main.go)、[iface](../cmd/gom/iface.go)、[spec 7](spec.md#7-インターフェースファイル-gomi)、[sketch](sketch/2026-11-05-moon-cli-command-structure.md)。

- [x] **`struct{}` の Gom 側の対応が定められていない**: spec 8.3 の対応表にも v1 未対応リストにも
  `struct{}` が現れないが、spec 10 章の例は `ctx.Done()` の `<-chan struct{}` を `select` の腕に書く
  （`Unit` → `struct{}` なら `RecvChan Unit` が自然）。実装は "unsupported Go type struct{}" で
  E0501 にするため、10 章の例はメンバー未実装とは別の理由でもコンパイルできない。
  対応: `struct{}` → `Unit` のマッピングを採用（`goimport/mapping.go`）。実際の
  `context.Context` + `ctx.Done()` + `time.After(time.Second)` を select 腕に書く
  spec 例がコンパイル＆実行まで確認済み。
  参照: [decision](decisions/2026-11-04-chan-direction-go-boundary.md)、
  [検証](sketch/2026-11-04-p5-1-concurrency-verification.md)。

- [x] **spec の例の `Result Int64 GoError` が 8.3 節の対応表と食い違う**: 9.2 節の
  `parse_port`（`strconv.ParseInt` の結果）と 8.4 節の `extern io.Copy` は `Int64` を
  宣言するが、対応表は Go の `int64` → `Int` と定める（decisions/go-types-mapping も同じ）。
  `check` は "expected Int64, got Int" を出す。extern の `Int64` は変換の上書きとして
  意図的かもしれないが、9.2 の `parse_port` は少なくとも表とずれている。
  参照: [spec 8.3](spec.md#83-型の対応)、[spec 8.4](spec.md#84-extern上書き専用)、
  [spec 9.2](spec.md#92-意味)、[decision](decisions/2026-11-03-go-types-mapping.md)、
  [監査](sketch/2026-11-03-spec-examples-audit.md)。
  追記（P3-5）: extern 側は解消 — `BindExtern` の view 照合が `Int64` ↔
  `int`/`int64` の数値ペアを許すので 8.4 の `extern io.Copy` は実装通りコンパイルできる
  （[extern.go](../internal/goimport/extern.go)、[検証](sketch/2026-11-03-extern-binding-verification.md)）。
  残るのは 9.2 `parse_port` 側 — `strconv.ParseInt` を自動マッピングで読むと `Int` であり
  `Int64` を宣言したいなら extern か `Go.conv` が要る。例を直すか対応表に `int64` の扱いを
  明記するかは未決のまま。
  対応（wave8）: 9.2 の例を `Result Int GoError` に修正（ParseInt は自動マッピングで
  `int64 → Int`）し、Int64 を得たい場合は extern 宣言か `Go.conv` を使う旨の注記を
  同一節に追加 — 「例を直す」側を採用。
- [x] **成長する多相再帰が check を通るが生成 Go が拒否される**: `fn wdepth (w: W a)` が
  `wdepth inner`（`W (List a)` で自己呼出）すると check/build は成功するが、
  生成 Go は `instantiation cycle`（`@tailrec` 版は型不整合ループ）でコンパイルできなかった。
  対応: 型パラメータ流グラフで成長エッジの循環を検出し E0416 で拒否。型変数の入れ替えだけの
  再帰と ADT の型レベル再帰（`W (List a)` フィールド）は合法のまま。
  参照: [spec 3.3](spec.md#33-型推論の範囲)、[decision](decisions/2026-11-03-generic-instantiation-cycles.md)、
  [検証](sketch/2026-11-03-p2-5-generic-emission-verification.md)。
- [x] **非恒等の自己インスタンシエーションが末尾呼出変換で型不整合ループになる**: `fn f (x: a) (y: b)` の
  `f y x`（`f[B,A]` での自己呼出）は合法（E0416・Go とも受理）だが、`markTailCalls` が
  `x, y = y, x` の `for` ループへ書き換え、生成 Go が型エラーになった（収縮 `f x x`・回転
  `f z x y` も同じ）。main 上に存在したずれ。
  対応: 恒等インスタンシエーション（`TypeArgs` が宣言 `TypeParams` と同一順）のみ `TailCall`
  へ変換し、非恒等の自己呼出は `CallDirect` のまま Go の通常再帰として出力。`@tailrec` 下は
  従来どおり E0902。
  参照: [検証](sketch/2026-11-03-p2-5-generic-emission-verification.md)、
  [golden](../testdata/golden/tailcall_instantiation.golden.go)、
  [decision](decisions/2026-11-03-generic-instantiation-cycles.md)。
- [x] **CLI usage の計画中コマンド名が P4-1 の決定と食い違う**:
  `cmd/gom/main.go` の usage は計画コマンドとして `gomi` を列挙するが、
  TODO P4-1 は `moon info` 流の `info` という名前を決めている（spec 7.5 も `gom gomi` 表記）。
  実装はまだ無いが、名前の正が文書間で不一致。
  対応: `gom info` を実装し `gomi` はリネーム通知に変更、spec 7.5 も `gom info` 表記に統一。
  参照: [usage](../cmd/gom/main.go)、[iface](../cmd/gom/iface.go)、[spec 7](spec.md#7-インターフェースファイル-gomi)、[sketch](sketch/2026-11-05-moon-cli-command-structure.md)。
- [ ] **`...any` 位置で `checkBlock` だけが `want` をそのままノードに刻む非対称**:
  if/match/select/recordLit は `bw`（`Go.Any` を nil に倒す）で推論するのに対し、
  `checkExpr` は `checkBlock` に生の `want` を渡し、`checkBlock` はそれを Block ノードの型にする。
  結果として `fmt.Println ({563.33})` の Block ノードは `Go.Any` を持ち、最終式は `Float` のまま —
  `bw` のコメントが lower の不変条件 (E0901) を心配する形と同じだが、`gom build`・lower は通る
  （実測 `ignore (fmt.Println ({563.33}))` → `ok:true`）。意図的な設計か未整合かは未確認。
  参照: [checkExpr の bw 分岐](../internal/check/expr.go)（30-41 行目と 69-70 行目）、
  [checkBlock](../internal/check/expr.go)（`out.Base = baseOf(e.Sp, want)`）、
  [発見経緯](sketch/2026-11-03-inference-witness-oracle.md)。

- [ ] **`go build` / `go vet` のエラーを `.gom` 位置へ戻す経路に実起動がない**:
  design.md 2.2 は「`go build` / `go vet` のエラーと panic スタックを `.gom` の位置へ戻す」と定めるが、
  `GoToolDiagnostics` は内部 API として用意されただけで、CLI が実際に `go build`/`go vet` を
  起動して出力を変換する経路はない（検証は `go test` / `go run` 経路のみ）。
  参照: [design 2.2](design.md#22-ソース位置の対応)、[GoToolDiagnostics](../internal/build/gopos.go)、
  [decision](decisions/2026-11-03-source-location-correspondence.md)。

- [ ] **`//line` ドリフトによる位置エイリアシング（近似のまま）**:
  1 つの `.gom` 行が k 生成行に展開されると、`//line` は後続生成行を連番でドリフトさせるため
  `GoPosIndex.Resolve` の報告行が実在する別の `.gom` 行に alias されうる。旧実装と同等の近似で
  回帰ではないが、「対応表を使う」design.md 2.2 の意図とのずれ。抜本対応（アンカー表を使う
  逆引き、曖昧時フォールバック）は P4-6 (LSP) の設計入力。
  参照: [design 2.2](design.md#22-ソース位置の対応)、[GoPosIndex.Resolve](../internal/build/gopos.go)、
  [decision Consequences](decisions/2026-11-03-source-location-correspondence.md)。

- [ ] **`SpanAt` が「生成行」ではなく「内側構文の span」を返す粒度の差異**:
  対応表の逆引きは Go の継続行番号より粗く、エディタが期待する行・列の対応とは意味論が違う。
  LSP で列・範囲単位の対応が要るかは open-questions 参照。
  参照: [SourceMap/SpanAt](../internal/diag/srcmap.go)、
  [decision](decisions/2026-11-03-source-location-correspondence.md)。

- [x] **spec が規定する `[x, ..rest]` リテラルを parser だけが拒否していた**:
  spec 6 章と grammar.ebnf はリストリテラルの `..rest` 末尾を書いていたが、
  parse_expr.go は E0101「spread ".." is not allowed in a list literal」で落としていた
  （checker/lower/codegen/eval は対応済み）。
  対応: パターン同様に `..` 要素を受理し `syntax.Spread` を生成。
  `stdlib/list` の `cons`/`append`/`rev_append` がこの構文を使う。
  参照: [decision](decisions/2026-11-03-list-literal-spread.md)、
  [spec fixture](../testdata/spec/ok_list_spread_lit.gom)。
- [x] **`.gomi` の `let` メンバーは表面に出るが外部参照との形が一致しない**:
  generalized let は thunk 実装になるが、member table は `MemberLet` を
  KindVar シンボルとして登録し var 参照を生成する — 単相 let なら動くが
  一般化 let は不整合（gomimport 側も同じ形。conformance は alphaEqual で
  通るため「適合するが実装不可能」な面だった）。
  対応: `.gomi` の `let` シグネチャ内の型変数を parser が E0602 で拒否する
  bounded fix — member table の let は常に単相になり var 参照と一致する。
  多相 let メンバーの thunk 表現（check/lower の thunk oracle を import 越しに
  通す方針）は future work として残る。
  参照: [parser](../internal/syntax/parser.go) `mentionsTypeVar`、
  [検証](sketch/2026-11-05-v6-residual-verification.md)。
- [ ] **`.gomi` の type メンバーはモジュールメンバ表に乗らない**:
  `internal/stdlib` は `fn`/`let` メンバーだけを `Modules` 表に登録し、
  `MemberType` はスキップする（モジュール内型 `List.Foo` の参照経路は未実装）。
  現行の `.gomi` は型を宣言しないので不活性。V-6 監査で再確認: gomimport 側は
  type メンバーが `p.Members` に乗るため `pkg.Type` / `pkg.Type.Ctor` は解決
  する — ずれは stdlib builtin-module 側のみ。
  参照: [internal/stdlib](../internal/stdlib/stdlib.go)、
  [検証](sketch/2026-11-05-v6-residual-verification.md)。
- [ ] **`SelfImportPath` は `stdlib/<pkg>/` の予約ディレクトリ規約**:
  `gomstd.SelfPath` は入力パスに `stdlib/list|option|result/` を含むと
  そのパッケージの canonical import path を返す。ユーザーのモジュールが
  同名レイアウトを使うと同じ規約が適用される（builtin 型の参照が
  無修飾でそのローカルパッケージを指す）。V-6 監査で懸念を problems file に記録。
  参照: [internal/stdlib](../internal/stdlib/stdlib.go)、
  [problem](problems/2026-11-05-self-import-path-layout-convention.md)、
  [decision](decisions/2026-11-03-stdlib-packaging.md)。
  追記（2026-11-06）: 実害を観測 — `stdlib/list/` 内で `gom build`
  （basename 入力 `list.gom`）を実行すると `SelfPath` が `""` を返し、
  `List` 参照が stdlib `list` モジュール経由で修飾されるため、生成物
  が `package list` 内で `import "...stdlib/list"` する **self-import**
  になる（go build 不能）。正規の再生成は repo root から
  `gom test stdlib/<pkg>`（`//line stdlib/list/list.gom:N` を持つ
  コミット済み成果物がそれ）。ディレクトリ内ビルドを防ぐか、
  SelfPath の判定をパッケージ宣言・go.mod 配置に拡げる余地あり。
- [ ] **`Result.to_option` / `Option.to_list` は存在しない（Go の循環参照禁止によるもの）**:
  option→result（`to_result`）・list→option（`get`/`head` 等）が既にあるため、
  逆向き変換はパッケージ間で import cycle になる。layering は
  `result < option < list` に固定。各 `.gomi` の末尾に理由を記載済み。
  参照: [decision](decisions/2026-11-03-stdlib-packaging.md)、
  [stdlib/option/option.gomi](../stdlib/option/option.gomi)。

- [x] **`docs/grammar.ebnf` の `list-expr` が `[x, ..xs]` を記していなかった**:
  spec 6 章と実装はリストリテラルの末尾 `..` spread を受理するが、規範文法ファイルだけ
  未更新だった（decision の「grammar.ebnf は既に書いていた」という記述は `list-pat` 側との
  取り違え）。対応: `list-expr` の産出規則に末尾 `".." , expr` を追加しコメント修正、
  decision に訂正を追記。
  参照: [decision](decisions/2026-11-03-list-literal-spread.md)、
  [grammar.ebnf](grammar.ebnf)。

- [x] **制約の分類が型集合の部分集合を comparable と誤認**: `~int`・`int|string`・
  `interface{comparable; ~int}` は `IsComparable` が真でも生成 `pkg.F[int64]` が Go に
  拒否される。`interface{comparable}` は受理する必要があるため IsMethodSet でも判別不能。
  対応: universe `comparable` との `types.Identical` で型集合の同一性を照合し、
  部分集合は E0506 で非公開。`interface{comparable}` 同値表記は受理を維持。
  参照: [decision](decisions/2026-11-04-go-collections-and-generics.md)、
  [fixture](../testdata/spec/E0506_approx_constraint.gom)。
- [x] **コンテナ要素の Int/Go.Int 分裂が lower の E0901 になる**: `[]int`・
  `map[string]int`・`[][]int`・`chan int`・`Ptr int`・`...int` は要素ごとの
  int/int64 変換を Core IR で表せず、check を通って lowering で内部エラーになった
  （P3-3 以前からの潜在バグでもある）。
  対応: `viewsCompatible` で両ビューの整合を境界型ごとに検査し、
  分裂するメンバーは理由付きで非公開（E0501）。要素変換コピー（rt.MapSlice 案）は
  コストが読めないため見送り — `Go.Slice` 経路と spec 修正の余地として記録。
  参照: [decision](decisions/2026-11-04-go-collections-and-generics.md)、
  [fixture](../testdata/spec/E0501_go_container_view_split.gom)。

- [x] **`checkInitCycles` は file-local で Go のパッケージ単位の分析とずれる**:
  E0907 の初期化依存グラフは 1 ファイルの Core IR 上に構築するが、Go は
  パッケージ単位で初期化順を決める。**複数 `.gom` パッケージが実現した
  （V-6）ため発生可能になった**: `a.gom: let x = y + 1` / `b.gom: let y = x + 1`
  は Gom 診断なしに `var x = y + 1` / `var y = x + 1` を生成し、
  `go build` が "initialization cycle" を報告する（2026-11-05 実測）。
  対応: `lower.CheckInitCycles([]*core.File)` が全メンバーの `Decls` 上に
  グラフを構築し、`build.CompileFiles` がユニット検査してファイルごとに
  診断を振り分け・全出力を抑止。隠し tmp は `core.VarDecl.DiagName` で
  `let <pat>` 綴りに報告される。検証: `internal/build` の 3+1 回帰テスト
  （兄弟 var/fn/tmp 経由サイクル、前方参照 OK）。
  参照: [decision](decisions/2026-11-05-package-initialization-order.md)、
  [検証](sketch/2026-11-05-package-initialization-order-verification.md)。

- [x] **spec にパッケージ初期化順序の節がない**: E0907（初期化循環の拒否）の
  `Spec:` 参照先が未定義 — `@tailrec` 系の E09xx は spec 1.1 を参照するが、
  初期化順序の言語仕様自体は spec にない。spec 節を新設するかは未決。
  対応: decision 採用（Go の依存解析をユニット単位で逐語採用）のうえ
  spec.md §5.1 を新設し、E0907 のエントリに `Spec: "5.1"` を付与。
  参照: [decision](decisions/2026-11-05-package-initialization-order.md)、
  [spec](spec.md#51-パッケージの初期化順)、[codes.go](../internal/diag/codes.go)。

- [x] **遅延メンバー適用の引数タプル内で `_` / `..e` が provisional 通過をすり抜ける**:
  `x.M((_, 1), 2)` のように arg タプルの *要素* に置かれた `_` は provisionalArg が
  TupleLit を再帰的に保持するため E0504 を報告されず、メソッド確定後も
  checkGoApply の top-level 引数処理に届かなかった（spec 8.2 の「`_` は Go 呼び出しの
  引数にだけ使える」とのずれ）。対応: pendingApply に minted marker を記録し、
  replay の使用済み marker との差分を flagStrayMarkers で E0504/E0505 として報告、
  stray hole は markQuiet で eager と同じ回復形に揃えた。
  参照: [decision](decisions/2026-11-05-deferred-member-application.md)、
  [検証](sketch/2026-11-05-deferred-member-application.md)、
  [fixture](../testdata/spec/E0504_hole_in_go_arg_tuple.gom)。
- [x] **メソッド呼び出しの連鎖 `x.M(1).N(2)` が arg タプルへの select として parse される**:
  postfix の selector が適用の引数タプルより強く結合するため、`.N` は `(1)` の
  後続ではなくタプル要素とみなされる。`x.AddDate(1,0,0).AddDate(0,1,0)` は
  E0401（arity / "tuple type has no AddDate"）になるが、明示的な
  `(x.AddDate(1,0,0)).AddDate(0,1,0)` は動く。解決済み receiver でも同じ
  失敗 — 推論の問題ではなく grammar の優先順位の問題（spec 8.1.3 は連鎖を定めていない）。
  対応: post-argument selector を適用に結合させ左結合で parse（`f (e).m` は
  `(f e).m` に変化 — 意味変更は decision + spec 8.1.3 に明記）。spec fixture
  `ok_method_chain`。
  参照: [decision](decisions/2026-11-05-post-arg-selector-binding.md)、
  [検証](sketch/2026-11-05-post-arg-selector-binding-verification.md)。

- [x] **eval が Go 呼び出し内の `..e` spread を評価できない**: `*check.Spread` は
  list literal 内でのみ処理され、Go call の引数位置では `CodeUnsupported`。
  CallGo/variadic を eval 側が完全にカバーするには spread 展開の実装が要る
  （P3-4 レビューで観測、pre-existing）。
  対応: eval が `VList`/`VHost` スライスを splice（空リスト→引数 0 個）。
  checker は spread-sole-variadic-tail（E0505）を eager/遅延 replay 両経路で強制。
  参照: [eval.go](../internal/eval/eval.go) `spliceGoSpread`、
  [fixture](../testdata/spec/E0505_go_spread_shared_tail.gom)。

- [ ] **any-hole cute の結果型が callee unify で premature bind されると eager と診断がずれる**:
  deferred 経路だけ `expected String, got Go.Any`。`Go.Any` の受け入れが want 側
  限定であることに起因し、仕様判断が必要。
  参照: [problem](problems/2026-11-05-any-hole-deferred-result.md)。

- [ ] **`import alias "path"` 形式と `.gomi` 内 import は文法だけが許す**: spec §5 が
  `import "path"`（Gom パッケージ）を規定したのに対し、grammar.ebnf の
  `import-decl` の `[ ident ]` alias 部と `iface-decl` での import 受理は実装で
  未検証のまま（[PROVISIONAL gom-import-syntax] タグ付き）。`.gomi` 内 import は
  gomimport ローダ経由で機械的には読める（ネスト解決・サイクル検出、
  `TestGomiSideImport`）が spec §7 の規定としては未記述 — 残るずれは仕様定義側。
  参照: [spec 5](spec.md#5-モジュールと可視性)、[grammar.ebnf](grammar.ebnf)、
  [problems](problems/2026-11-01-gom-import-syntax.md)、
  [problems](problems/2026-11-05-gom-package-imports.md)。
- [x] **兄弟ファイルで同名 `test` ブロックは Go 側で重複シンボルになる**:
  パッケージ単位コンパイルで `gomEq*`/`tmp*` 系はファイルタグで回避するが、
  `test "name"` 由来の `func TestX` は無修飾のため、2 ファイルに同名 test が
  あると生成コードの `go build` が重複定義で失敗する（Gom 診断ではない）。
  対応: `core.UnitScope.Tests` が兄弟ファイルの test base name を記録し、
  衝突する名前だけファイルタグ（fnv32a of path、`pkgTmp` と同じ規約）付きの
  `TestX_<tag>` になる。衝突しない名前は無修飾のまま。
  参照: [decision](decisions/2026-11-05-test-name-file-tag.md)、
  [core/names.go](../internal/core/names.go)、
  [検証](sketch/2026-11-05-v6-residual-verification.md)。

- [x] **兄弟ファイルのトップレベル `let` 分解・ワイルドカードが生成 `var tmpN` で衝突する**:
  lower の `tmp()` はファイルタグを持たず、`let _ =` / `let (a,b) =` が 2 ファイルあると
  `gom build` は成功するが生成 Go パッケージが `tmp1 redeclared` でビルド失敗した
  （V-6 最終レビューで検出）。
  対応: package-scope の一時変数は `pkgTmp()`（`tmp<N>_<fnv32a(path)>`、`eqMark` と同型）で
  発行し、関数本体の一時変数は従来の `tmp()` のまま。decision の「`tmp*` はファイルタグを
  持つ」記述も併せて実態通りに修正済み。
  参照: [lower.go](../internal/lower/lower.go) (`pkgTmp`)、
  [テスト](../internal/build/files_test.go) `TestCompileFilesTmpVarsDontCollide`、
  [decision](decisions/2026-11-05-package-compilation-unit.md)。

- [x] **`gom build` が E0601（`.gomi` 不整合）でも `.go` を書き出す**: `checkInterfaces` は
  ファイルごとの codegen の後に走るため `r.Output` は既に設定済みで、exit 1 ながら出力は
  書かれる（サマリには正直に列挙される）。F3 のユニット原子性（失敗したユニットは出力を
  出さない）との整合が取れていない。
  対応: `checkInterfaces` が失敗した package group の全 result を返し、
  `runBuild` がその出力を書き出さない（E0601/E0603 いずれも。パッケージ単位で
  all-or-nothing、他ディレクトリのクリーンなパッケージは書き出す）。
  参照: [decision](decisions/2026-11-05-interface-failure-withholds-output.md)、
  [iface.go](../cmd/gom/iface.go)、[cli.go](../cmd/gom/cli.go)。

- [x] **exported `let foo` と `type Foo` が生成 Go で同名 `Foo` になる**: 単一ファイルでも
  再現（pre-existing）。export 値名と型名が大文字名前空間を共有し dedup されない。
  対応: codegen が型名を先に `Foo` として確保し、値シンボルは unit 全体を
  (emitted base, Gom name) の canonical 順で確保する — `let foo` + `type Foo` は
  `var Foo1 Foo = Foo{...}` になる（単一・複数ファイルとも）。
  さらに `.gomi` member が dedup される衝突は E0607 で拒否する: dedup 済み名は
  importer が綴れない（`lib.foo` は `lib.Foo` に emit され、型や先勝ちの値を指す）。
  `iface.Check` が package surface 上で codegen の確保順を replay し、先勝ちの
  claimant（impl 型・private を含む先の値）がいる interface member を報告する。
  importer 側にも網を置いた: `.gomi` loader が member 名と `core.ExportName` が
  衝突する member symbol に `EmitClash` を付け、resolve が参照位置で E0607 を
  出す — member テーブルに見える衝突（type Foo + let foo）は宣言側未検査でも
  誤参照しない。
  参照: [decision](decisions/2026-11-05-exported-value-vs-type-name.md)、
  [codegen/gen.go](../internal/codegen/gen.go) `indexDecls`、
  [iface/compare.go](../internal/iface/compare.go) `checkEmittedNames`、
  [gomimport/loader.go](../internal/gomimport/loader.go)、
  [resolve/diags.go](../internal/resolve/diags.go) `emitClash`。

- [x] **`.gom` 実装 import の循環が `go build` まで持ち越される**: `.gomi` 同士の
  循環はローダが検出していたが、`.gom` ファイルの `import "path"` 相互・推移的・
  自己循環はコンパイルを通り、生成 Go パッケージが "import cycle not allowed" で
  落ちるだけだった（package-compilation-unit decision の deferred boundary）。
  対応: `gomimport.Loader` が entry パッケージ起点で `.gom` import 辺を
  deterministic DFS し、`resolve.ImportCycleError` を E0606 として import パス
  span に報告（`data.cycle` に連鎖）。`.gomi`→`.gom` の逆向き参照は辺に含めない
  （interface の参照はコード依存を作らない）。cycle の verdict は発見した
  循環の構成員（`implCycleSet`）にのみ適用する — 循環外のパッケージへの
  clean import は通常どおり load され、誤って E0606 を報告しない。
  参照: [decision](decisions/2026-11-05-gom-implementation-import-cycles.md)、
  [loader](../internal/gomimport/loader.go) `visitImpl`、
  [検証](sketch/2026-11-05-v6-residual-verification.md)。

- [x] **Float 型の巨大整数リテラルが eval で失敗し生成 Go では動く**:
  `let m: Float = 99999999999999999999`（int64 超）は eval の `litValue` が
  `strconv.ParseInt` を Float 判定より先に呼ぶため CodeInternal "invalid int
  literal" で失敗するが、生成 Go は `float64(...)` を出してコンパイル・実行
  できる（exotic、Wave 5 hardening レビュー指摘）。
  対応: `isFloatCon` 時は `ParseFloat` → `big.Int.SetString` で評価
  （codegen の `float64(text)` と一致）。検証: `eval_test.go` 回帰 +
  `compare_test.go` の `float_annotated_huge_int_literal`。
  参照: [lit.go](../internal/eval/lit.go) `litValue`。

- [ ] **`Go.conv` の `gomOnly` 判定が修飾名 `Qual` を見ない残余の不健全性**:
  `Con{Qual:"geom",Name:"Int"}`（foreign ADT が builtin と同名）は名前検査で
  「未証明」に倒れ、`*types.Go` fallback が `Go.conv <Go handle> : geom.Int`
  を受理しうる（pre-existing、regression ではない — 旧 shape 規則でも受理）。
  `t.Qual == ""` で絞ると `list.List[T]` 表記（qualified stdlib List は
  真に変換可能）を誤拒否するため、真の「Gom 側型か」の答えが要る。
  参照: [boundary.go](../internal/goimport/boundary.go) `gomOnly`、
  [expr.go](../internal/check/expr.go) `convOK` の fallback、
  [検証](sketch/2026-11-04-p3-2-p3-6-interop-verification.md)。

- [x] **`for` の shadowed-builtin E0415 一覧に `SendChan` がない**:
  `checkFor` はファイルローカルの `type List`/`Chan`/`RecvChan` を E0415 で
  報告するが `type SendChan` は一覧に含まれず、shadowed `SendChan` 上の
  `for` は素の mismatch になる（pre-existing、Wave 5 hardening レビュー指摘）。
  対応: `checkFor` の一覧に `SendChan` を追加（fixture
  `E0415_for_local_sendchan`）。併せて select recv/send arm にも
  shadowed-builtin 分岐を追加し E0415 ③ を解消（P5-2）。
  参照: [expr.go](../internal/check/expr.go) `checkFor`。

- [ ] **`metasIn(want)` の quiet 化は共有 want の後続 mismatch を抑制しうる**:
  `Chan.close (Chan.recvOnly ch)` の E0407 カスケード削減のため、unify 失敗時に
  want 内の全未束縛メタを quiet にする（旧 `markQuiet(want)` は生のメタにのみ
  効いた）。コンパウンド want（`SendChan ?a` 等）への拡大で、同じ未解決 want
  オブジェクトを複数の conform 位置（if/match/select の兄弟腕）が共有すると、
  最初の失敗で quiet 化されたメタ経由で 2 腕目以降の正当な E0401 が
  `mentionsQuiet` に握り潰されうる — 例: `g (if c { 1 } else { "s" })` の
  else 腕。wave-5 の ⑤ 残件と隣接する過剰抑制領域。狭い到達条件を理由に
  現状容認（コードコメントにトレードオフ明記済み、P5-2 レビュー nit）。
  参照: [expr.go](../internal/check/expr.go) `conformExpr`/`chanDirConv`、
  [fixture](../testdata/spec/E0401_close_of_recvonly.expected.json)。

- [x] **E0401 の mismatch メッセージが未解決メタを `?n` で綴る**: quiet 化された
  メタを含む型が「expected SendChan ?3, got RecvChan ?4」のように内部名の
  まま表示される（P5-2 `close`/`recvOnly` 例で観測）。診断品質の残差。
  参照: [fixture](../testdata/spec/E0401_close_of_recvonly.expected.json)。
  対応: `types.Display` + `metaNamer` で診断ごとに未解決メタを `?1, ?2, ...`
  へ再採番（occurs bind・mismatch・missing-member の E0401 系に適用。
  [type.go](../internal/types/type.go) `Display`、[unify.go](../internal/check/unify.go)
  `metaNamer`）。残件: E0407/E0409/E0801 の message/`data["type"]` は
  依然 `String()` 由来の raw mint ID を出しうる（下記「`?n` 以外の残存 raw meta」）。

- [ ] **W0701/W0702 lint の捕捉範囲外**: `let mk = Chan.make` のエイリアス経由は
  callee シンボル一致を外れ W0701 を逃げる。`Task.spawn` の thunk 捕捉・
  クロージャ経由の捕捉（`let f = fn () -> r; go { f () }`）は値の可変性が
  型に現れず W0702 を逃げる。decision の scope-out として記録済み。
  参照: [decision](decisions/2026-11-04-mutable-sharing-lint.md)、
  [lint.go](../internal/check/lint.go)。

- [x] **ネスト `go` で同一位置に W0702 が 2 件重複出力される**: 外側・内側の
  `go` 境界がそれぞれ同一の捕捉を報告し、同一スパン・同一メッセージの
  警告が 2 件になる。各 boundary が別の共有イベントという解釈で採用済みだが、
  ユーザーからは判別不能 — boundary を特定する `data` を持たせる余地がある
  （P5-2 レビュー nit）。
  参照: [fixture](../testdata/spec/W0702_go_capture_mutable.expected.json)。
  対応: `goWarned` の `reported` set でスパン単位に dedupe（[lint.go](../internal/check/lint.go)）。
  異なる使用箇所は引き続き個別に警告される。

- [ ] **`rune` → `Char` 表記と実装（`Int32`）のずれ**: spec の型対応は
  `rune`/`Char` を書くが、goimport の basicType は `types.Int32` → `Int32`
  に写し、 untyped rune リテラルのみ `Char` になる（`unicode.IsUpper` で実測、
  `Char` とは `Go.conv` が要る）。
  参照: [mapping.go](../internal/goimport/mapping.go) `basicType`、
  [監査](sketch/2026-11-05-p3-7-realworld-interop-audit.md)。

- [x] **`Go.Map` に操作がない**: `builtinGoMembers` は `conv`/`Slice`/`Map` の
  型名のみで、`Go.Map.make` 等の操作は E0201。`Go.Map` を持つ Go API は
  実質使えない（P3-7 監査で判明、TODO に task 登録済み）。
  対応: ctor-table 経路の `Go.Map` メンバーテーブルで `make`/`get`/`set`/
  `delete`/`length`/`to_list` を追加（rt バック）。nil map は Go のまま、
  非 comparable key は E0508。
  参照: [decision](decisions/2026-11-05-go-map-ops.md)、
  [検証](sketch/2026-11-05-p3-7-interop-ops-verification.md)、
  [builtin.go](../internal/stdlib/builtin.go)。

- [ ] **3+ 戻り値の Go 関数は非公開だが spec は arity 上限を書いていない**:
  spec 8.3 は tuple 戻り値の行だけで、実装は 3 つ以上の結果を持つ関数を
  E0501 で隠す（`strings.Cut` 等。P3-7 監査で判明）。spec 文言を足すか
  N-ary 対応かの判断が残る。
  参照: [spec 8.3](spec.md#83-型の対応)、
  [監査](sketch/2026-11-05-p3-7-realworld-interop-audit.md)。

- [x] **`[]T` を in-place 変更する Go 関数はコンパイルして無音 no-op になる**:
  `sort.Strings` 等は境界でコピーされる List/Slice 規則の帰結として呼べて
  しまうが呼び出し側の値は変わらない。spec としては正しいが無意味 — lint
  警告か `Go.Slice` pun 規則の明文化が要る（TODO に task 登録済み）。
  対応: W0501 lint で警告化 — Unit 結果 + コピーされる `List` 系引数で発火。
  素の `Go.Slice`（verbatim 経路）は沈黙、`Go.Slice`→`Go.Slice` は逃げ道。
  `signal.Notify` 系は文書化済みの偽陽性。
  参照: [decision](decisions/2026-11-05-go-mutation-warning.md)、
  [lint.go](../internal/check/lint.go) `lintGoSliceCopy`、
  [fixture](../testdata/spec/W0501_go_slice_copy.gom)、
  [problems（解消済み）](problems/2026-11-05-go-param-mutation-noop.md)。

- [x] **`gom test` に `go test` フラグ透過がない**: `gom test` は内部で
  素の `go test` を呼ぶため生成コードを `-race` 等で検証できない —
  P5-3 の race 検証は生成物を手動で `go test -race` に掛けて代替した。
  検証基盤の穴（TODO に task 登録済み）。
  対応: `gom test -- FLAGS` で最初の `--` 以降を `go test -json` へ
  verbatim 透過（decision 採用、`TestTestGoFlagPassthrough` でピン留め）。
  参照: [decision](decisions/2026-11-05-gom-test-go-test-flags.md)、
  [検証](sketch/2026-11-05-gom-test-go-test-flags.md)、
  [test.go](../cmd/gom/test.go)、[gotest.go](../internal/build/gotest.go)。

- [ ] **spec 12 節は panic 出力を「2 部構成」と限定なく書くが、実装は
  解決フレーム 0 件なら単一部のまま返す**: `renderTrace` は `.gom` 位置へ
  解決できるフレームが 1 つも無い出力（ツールチェーン・手書き Go だけの
  スタック）では rendered==raw となるため raw をそのまま返す。fail-soft で
  妥当な挙動（design.md §4.8 に記載済み）だが、spec の文言とはずれている。
  `-q` でも同じ（全未解決なら raw が返る）。
  参照: [spec 12](spec.md)、[test.go](../cmd/gom/test.go) `renderTrace`、
  [design](design.md)。

- [x] **未 clean な `.gomi` 入力パスが `gomiInputs[want]` を外れて
  `.gomi` が二重にコンパイル・診断が二重に出る**: `iface.go` の
  `gomiInputs` が入力パスをそのままキーにするため、
  `gom check pkg/../pkg/ex.gomi pkg/a.gom` のような入力で
  `filepath.Join` が組む clean な `want` キーに一致せず、
  `os.ReadFile`+`compileInput` のフォールバックに落ちて同じ
  `.gomi` が二度コンパイルされ、その診断（例: 未定義型の E0201）
  が入力スペリングと clean スペリングの 2 件として出る
  （pre-existing。check/build/test 全経路に影響。
  初版の登録は「`filepath.Dir` 未 clean で `.gom` グループが
  分裂する」と書いていたが、`filepath.Dir` は内部で Clean する
  ため `.gom` 側のグループ化は常に正しかった — 実際の穴は
  `.gomi` 入力キーの方。wave7 gomtest レビューで発見、
  wave8 レビューで機構を訂正）。
  参照: [iface.go](../cmd/gom/iface.go) `checkInterfaces`、
  [cli.go](../cmd/gom/cli.go) `compileInputs`。
  対応: `gomiInputs` のキーと `dirCensus` の `byPath`（glob 結果の
  clean スペリングと照合する側）を `filepath.Clean` して正規化。
  `cmd/gom` の `TestCheckUncleanGomiInput` が未 clean `.gomi`
  入力で診断が 1 件だけ出ることを pin（main では 2 件に重複）。
  `TestCheckInterfaceEquivalentPaths` /
  `TestTestInterfaceEquivalentPaths` は従来通り正しかった
  `.gom` グループ化を pin する。

- [x] **eval は整数演算を型幅に関係なく int64 で計算する**: spec 3.3 は
  `Int32` が 32 ビットで wrap すると定めるが、eval の `VInt` は常に int64
  （`eval.go` の算術は `l.N + r.N` をそのまま返す）。`fn f (x: Int32) -> Int32 =
  x + 1` を `2147483647` に適用すると eval は `2147483648`、生成 Go（`int32`）
  は `-2147483648` を返す。生成コード側は spec 通りで、eval を型幅 aware に
  する必要がある（pre-existing — per-operation 数値意味の spec 文言追加で
  顕在化。Int32 の `%`・`MinInt32 / -1`・単項 `-` も同じ制限）。
  参照: [spec 3.3](spec.md#33-型推論の範囲)、[eval](../internal/eval/eval.go)、
  [decision](decisions/2026-11-05-numeric-arithmetic-per-operation.md)。
  対応: 演算位置の静的被演算子型で幅を引く方式を採用 — `VInt` は
  sign-extended 済み canonical 値の不変条件を持ち、`evalPrim`/`evalUnary`/
  `litValue` が幅に正規化して int64 で計算・wrap する
  （[decision](decisions/2026-11-05-eval-int-width-aware-ops.md)、
  [検証](docs/sketch/2026-11-05-eval-int-width.md)、
  差分テスト `int32_width_arith`/`int64_min_literal`）。ホスト注入 VInt は
  pre-normalized 前提（eval doc.go 契約参照）。

- [x] **`EscapeCall`/`EscapeResult` のスパン規約が unguarded/guarded で揃っていない**:
  P2-4c で unguarded 融合側は `lv.Y.Span()`（ステップ operand）に統一したが、
  guarded 側 `escapeStepPayload` は `e.Sp` を使う。`//line` の指す位置にのみ
  影響、動作は無害（P2-4c+P5-3 レビュー nit）。
  参照: [expr.go](../internal/lower/expr.go) `pipeRest`/`escapeStepPayload`。
  対応: `escapeStepPayload` 全 5 箇所を `e.Y.Span()`（ステップ operand）に統一
  （`try` 自体の `e.Sp` は別規約として維持）。`testdata/golden/result_pipe.gom`
  の `parseWrapped` が行 13/14 の `//line` を pin。

- [x] **`@tailrec` R4 の直接参照グラフは file-local で兄弟経由の相互再帰を見逃す**:
  `checkInitCycles`（E0907）と同型の file-local 制限 — 相互再帰が *兄弟*
  ファイルの関数・let 経由で閉じると E0904 をすり抜ける。修正方向は同じ:
  R4 のグラフをユニット `Decls` 上に構築する（wt/initorder-small の
  package-wide 化と同形）。
  参照: [decision Consequences](decisions/2026-11-05-package-initialization-order.md)、
  [initcycle.go](../internal/lower/initcycle.go) `checkTailRec`/`tailrecNeighbors`。
  対応: `CheckTailRecMutual` がユニット全ファイルの `Decls` 上にグラフを構築
  （[tailrec.go](../internal/lower/tailrec.go)）。`Compile`/`CompileFiles`/`lower1`
  全経路で配線済み。`a.f → b.g → a.f` と兄弟 `let` 経由を driver test が pin
  （[files_test.go](../internal/build/files_test.go)）。

- [x] **`Go.conv` の `Go.Slice T ↔ List T` がコピーしない**: spec 8.4 は（[decision](decisions/2026-11-04-goconv-list-slice-copies.md) で）コピーすると定めたが、実装は `list.List[T](xs)` へのキャスト（pun）のまま。上の Go slice 項目の追記を参照。実装は [TODO](../TODO.md)。
  対応: `convKind` が `goConvKindOf` を先に照会し、同要素のスライス対は
  `ConvSliceToGo`/`ConvSliceFromGo`（`list.FromSlice`/`.ToSlice()`）を返す。
  数値・named・チャネル方向の変換は従来のフォールバックで不変。
  参照: [expr.go](../internal/lower/expr.go) `convKind`/`goConvKindOf`、
  [golden](../testdata/golden/go_conv.golden.go)、
  [検証](sketch/2026-11-05-go-slice-ops-verification.md)。

- [x] **要素型の異なる `Go.conv` `Go.Slice T ↔ List U` は依然 pun のまま**:
  `convOK` の oracle は Go-convertible な対を受理するが、`goConvKindOf` の
  スライス対は要素の `types.Equal` を要求するため、`Go.Slice Int64 →
  List Int`・`Go.Slice Int32 → List Char` のような表現等値の異要素対は
  `ConvNamed` → `list.List[T](xs)` の pun に落ちる — Go.Slice 経由の
  mutation が List に届く、コピー化で潰したクラスと同型の穴。暗黙の境界
  （`sliceConv`）は要素の等値を要求するため `Go.conv` のみが到達経路
  （main 由来、コピー化ブランチの適用範囲外。wave7 slice-ops レビューで発見）。
  対処案: `convOK` で要素不一致対を拒否するか、`goConvKindOf` で
  表現等値の要素対もコピー扱いにする。
  参照: [check/expr.go](../internal/check/expr.go) `convOK`、
  [lower/expr.go](../internal/lower/expr.go) `goConvKindOf`、
  [boundary.go](../internal/goimport/boundary.go)。
  対応: 前者を採用 — `convOK` の `sliceListPair` が一方の `List` と他方の
  `Go.Slice` の要素を `types.Equal` で比較し、不一致は oracle に回す前に
  E0507 とする（同要素ペアのコピー変換と `Go.Slice↔Go.Slice` の verbatim
  pun は不変。ネストした要素の不一致も同じ判定）。
  参照: [decision](decisions/2026-11-05-goconv-slice-list-element-mismatch.md)、
  [検証](sketch/2026-11-05-goconv-slice-list-element-mismatch.md)、
  [fixture](../testdata/spec/E0507_go_conv_slice_elem_mismatch.gom)。

- [x] **W0501 がインライン `Go.conv e : Go.Slice T` 引数を見逃す**:
  `helper.Mutate (Go.conv xs : Go.Slice Int)` のような「コピーを作って
  そのまま渡す」引数は `Ascribe{Conv:true, Slice:false}` のため lint の
  剥がし条件（`as.Slice`）に合わず、`Go.Slice` verbatim として沈黙する。
  `Go.conv` がコピー化した今、この形は真の no-op — 到達条件は狭い
  （extern `Go.Slice` param + インライン conv 引数）が記録に値する
  （wave7 slice-ops レビュー。let 束縛版は値が生きるので沈黙が正しい）。
  参照: [lint.go](../internal/check/lint.go) `lintGoSliceCopy`、
  [check/expr.go](../internal/check/expr.go) `convOK`。
  対応: `lintGoSliceCopy` が Conv な Ascribe を剥がし、剥いだソースが
  builtin `List` ならコピー引数として警告（`Go.Slice` ソースの verbatim
  pun・let 束縛の `Go.Slice`・読み取り専用呼び出しは従来通り沈黙）。
  [fixture](../testdata/spec/W0501_go_slice_copy.gom) にインライン変換の
  warn ケースと verbatim pun の沈黙ケースを追加。

- [x] **CLI では extern の照合（E0509）が走らない**: spec 8.4 は extern の宣言を生の Go シグネチャと照合し、合わなければ E0509 とする。
  `skills/` のスニペット検査（`goimport.Loader` を直接 `Sigs` に渡す）では `fn New (a: Int) (b: Int) -> GoError` のような arity 不一致が E0509 になるが、
  `gom check`/`gom build` は `gomimport.Loader` を `Sigs` に渡し、これが `check.ExternSource`（`BindExtern`）を実装していないため素通りし、生成・実行まで通る。
  参照: [spec 8.4](spec.md#84-extern上書き専用)、[実装](../cmd/gom/cli.go)、[extern-override-binding](decisions/2026-11-03-extern-override-binding.md)。
  追記（wave8）: 従来の記録例 `fn New(text: String) -> GoError` は lone `error` の
  宣言ビュー実装で受理されるようになったため例を arity 不一致に差し替え。
  安全性は `lower` の `goResultsOf` フォールバックが宣言型から同じ shape を分類する
  ため保たれるが、**宣言 arity のずれは救えない**（`fn Remove (n: Int) -> GoError`
  は `os.Remove(int64)` を生成 → go build 失敗に回帰）。宣言ビューが codegen を
  形作るようになった分、この gap の影響面は広がった（wave8 error-boundary
  レビュー F-5）。
  対応: `gomimport.Loader` に `BindExtern` を追加 — 既存の `MethodOf` /
  `InterfaceAssignable` と同じく `Config.Sigs` の `check.ExternSource` への転送で、
  未実装・未設定の場合は空応答で checker の宣言どおりフォールバックを維持。
  `go list` を走らせる面が Go 側のみで留まるため loader fork は不要。
  CLI ドライバ回帰 [TestCheckExternBinding](../cmd/gom/cli_test.go) は
  `gom check` が arity 不一致の extern を `error[E0509]` で落とし、修正前は
  `success` を返していたことを検証（実測: 転送を外すと exit 0）。

- [x] **E0407/E0409/E0801 の message/`data["type"]` が raw meta ID を出しうる**:
  `types.Display`/`metaNamer` による `?n` 再採番は E0401 系（occurs bind・
  mismatch・missing-member）にのみ適用。`unify.go` の E0409/E0407 報告と
  `check.go` の E0801 は依然 `String()` 由来の内部 mint ID を綴りうる。
  同じ `metaNamer()` を診断ごとに新規に作って適用するだけで揃う
  （wave7 lower-residuals レビュー residual-2）。
  参照: [unify.go](../internal/check/unify.go)（E0407/E0409 報告箇所）、
  [check.go](../internal/check/check.go)（E0801 報告箇所）。
  対応: E0409（satisfies・finalize 両箇所）・E0407・E0801 が診断ごとの fresh
  `metaNamer()` で `types.Display` するよう変更。fixture
  [E0409_eq_open_meta](../testdata/spec/E0409_eq_open_meta.gom)・
  [E0801_test_body_open_meta](../testdata/spec/E0801_test_body_open_meta.gom) 追加。
  残件解消: E0701 の Meta arm（ほか 2 報告箇所）も `types.Display` + fresh
  `metaNamer()` に統一（fixture
  [E0701_go_body_open_meta](../testdata/spec/E0701_go_body_open_meta.gom) で
  `?1` を pin）。さらなる残件: E0402/E0403（E0410/E0411 の可能性も）が
  同クラス — wave9 small-bugs レビューで発見、TODO 参照。

- [x] **E0904 の `through`/`path` が隠し `tmpN_xxxx` VarDecl 名を出しうる**:
  `tailrecCycle`/`tailrecPath` は生の `sym.Name` を使うため、`let (g, _) = ...`
  の分解で mint される隠し VarDecl が `data["path"]`・最小名選択の `through`
  に漏れる（`E0904_tailrec_var_destructure.expected.json` で実測）。隣の
  E0907 は `nodeName`/`DiagName` でこれを防いでいる — R4 側も同じ機構を
  再利用すればよい（wave7 lower-residuals レビュー residual-1。pre-existing
  だが今回書き換えた領域なので記録）。
  参照: [tailrec.go](../internal/lower/tailrec.go) `tailrecCycle`/`tailrecPath`、
  [initcycle.go](../internal/lower/initcycle.go) `nodeName`、
  [fixture](../testdata/spec/E0904_tailrec_var_destructure.expected.json)。
  対応: `CheckTailRecMutual` が `vars` を渡し、`nodeName`/`DiagName` で描画。
  上記 fixture は `tmp1_b96eea0a` → `let (g, _)` に更新済み。

- [x] **check が整数リテラルの範囲を検査しない**: `let n : Int32 = 3000000000`
  は型検査を通るが、生成される `var n int32 = 3000000000` は Go が build
  できない。定数畳み込み回避（litFoldDiverges）で `0 - 2147483648` も
  `tmp - 2147483648` に落ちるため裸の `2147483648` リテラルでも同様。
  受理したプログラムが build 不能になる確定済みのずれ。解消方向（Go 型範囲
  要求で拒否するか、wrap 実体化するか）は spec が沈黙しており決定が要る
  （wave7 int32-eval 検証中に発見）。
  参照: [problems](problems/2026-11-05-int-literal-range-check.md)、
  [check/pat.go](../internal/check/pat.go) `litType`、
  [codegen/decl.go](../internal/codegen/decl.go) `varDecl`。
  対応: Go の定数モデルに揃えて拒否する方を採用 — `pendingLit` が式・
  パターンの整数・float リテラルを記録し、`finalize` の
  `revisitLitRanges` が既定後の静的型（単項 `-` の直接の被演算子は符号込み、
  `Go.conv` の直接の被演算子は変換先）で範囲判定、範囲外は E0417。
  eval の wrap 実体化は到達不能の防御分岐になった。
  参照: [decision](decisions/2026-11-05-int-literal-range-check.md)、
  [検証](sketch/2026-11-05-int-literal-range-check.md)、
  [fixture](../testdata/spec/E0417_int_lit_range.gom)、
  [ok fixture](../testdata/spec/ok_int_lit_boundaries.gom)。

- [x] **`Go.conv` のリテラル被演算子は named Go 型の変換先で範囲判定を skip する**:
  `revisitLitRanges` は `Go.conv` の直接のリテラル被演算子を変換先で判定するが、
  変換先が named Go 型（`os.FileMode` のような `Qual != ""` の整数 named 型）だと
  `litRange` が非対応でリテラル自身の型にフォールバックする。そのため
  `Go.conv 5000000000 : os.FileMode` は受理されるが、生成される
  `fs.FileMode(5000000000)` は定数変換の範囲外で Go が build できない
  （E0417 レーン実装中に実測）。float リテラルでも同じ抜けがある:
  `Go.conv 1.5 : time.Duration` はリテラル自身の `Float` 判定に
  フォールバックして `1.5` が表現可能なため受理されるが、生成される
  `time.Duration(1.5)` は非整数の定数変換で Go が拒否する
  （`Go.conv 1e100 : time.Duration` も同様 — wave8 レビュー後の
  float→整数変換対応でも builtin 変換先のみが届く範囲）。
  named 型の基礎幅は oracle（`goimport.Loader`）
  側の知識なので、oracle に underlying-kind を引く照会を足すか、
  lowering で定数被演算子を型付き変換に逃がす必要がある。
  参照: [expr.go](../internal/check/expr.go) `litRange`/`markLitConv`、
  [decision](decisions/2026-11-05-int-literal-range-check.md)（残余として記録済み）。
  対応: oracle 経路を採用 — `check.UnderlyingSource` プロトコルを追加し
  `goimport.Loader.GoUnderlyingKind` が named 型の underlying `BasicKind`
  を返す。`litRange` はその幅・符号で判定（unsigned は `[0,2^bits)`、
  float32 は 32bit 判定、`LitChar` も監視）。非 Basic underlying
  （struct/interface/func/complex/string/bool）は従来どおりリテラル自身の
  型で判定にフォールバック。fixture
  [E0417_go_conv_named_target](../testdata/spec/E0417_go_conv_named_target.gom)・
  [ok_go_conv_named_boundaries](../testdata/spec/ok_go_conv_named_boundaries.gom)、
  [検証](sketch/2026-11-05-goconv-named-target-lit-range.md)。
  なお `let a : os.FileMode = 5000000000`（注釈位置）は従来どおり E0401 —
  リテラル自身の型は num 制約メタで `*types.Go` になれない既存仕様。

- [x] **`(error, error)` 系の結果で先頭の nil-able `error` が `Ok` ペイロードに
  検査なしで入る**: `func F() (error, error)` の派生写像は
  `Result GoError GoError` / `GoResultsErr` になり、`resultFromGo`（emit.go）
  は Ok 側 payload に `convOf` のみで nil 検査を置かない。第 1 結果が nil
  error のとき nil `GoError` が `Ok` に紛れ込み、`e.Error()` で Go 側
  nil-interface panic する。lone `error` の宣言ビュー（`Option GoError` 相当を
  Ok 側 payload に与える余地）で閉じられる同族の穴だが、
  [error-result-bindings](decisions/2026-11-05-error-result-bindings.md) は
  `(T, error)` 形状を明示的に対象外としている（pre-existing、wave8
  error-boundary レビュー F-4 で顕在化）。
  参照: [goimport/mapping.go](../internal/goimport/mapping.go) 派生写像、
  [codegen/emit.go](../internal/codegen/emit.go) `resultFromGo`。
  対応: [decision](decisions/2026-11-05-error-pair-ok-payload.md) を採用 — 派生
  ビューは `Result (Option GoError) GoError`（`nil` は `Ok None`）、宣言による
  `Result GoError GoError` は never-nil の契約として `NilGuard` で境界検査、
  `(T, error)` 上の `Result (Option T) GoError` 宣言を nil 許容な第 1 結果
  （`*T`/`error`）に限り許容（`ConvOptNil` で物質化）。spec 8.3/8.4 を更新。
  [検証](sketch/2026-11-05-error-pair-ok-payload-verification.md)。

- [x] **spec 8.5 の「両方向ともコピーする」は Gom 自身の呼び出し側にしか
  適用されず、生成 API 面（8.7）では `list.List[A]` が verbatim に渡る**:
  boundary-aliasing 検証で実測 — `fn echo (xs: List Int) -> List Int` の
  生成形は `func(xs list.List[int64]) list.List[int64] { return xs }` で
  呼び出し側のバッキングがそのまま出入りし、`List` を返す生成 API も
  コピーしない（Go 側の `out[0]=9` が次の Gom 呼び出しに届く）。spec は
  コピー規則のスコープを「Gom が Go を呼ぶ位置」に限定するか、生成 API
  の入口・出口にも defensive copy を置くかのどちらかを書く必要がある
  （コストのトレードオフ。wave8 boundary-aliasing 検証で確認）。
  参照: [spec 8.5](spec.md)、[spec 8.7](spec.md)、
  [検証](sketch/2026-11-04-boundary-aliasing-hypotheses.md)、
  [fixture](../testdata/gocall/boundary/boundary_test.go)。
  併記: 同じ面で `List (Go.Slice Int)`（`[][]int64` field の読み取り結果）
  は「不変の綴りの List が可変 Go 要素を保持する」形になり、`List` は
  不変という記述は要素単位の留保が要る。
  対応: 文言契約で閉じた — 8.5 のコピー規則は Gom のコード内で `[]T`↔`List`
  の view が切り替わる位置に限定すると明記し、生成 API 面はコピーせず
  8.7 の Go 側契約（要素書き換え・`let` 再束縛は契約違反、診断なし）と
  した。`List (Go.Slice Int)` の要素留保は 8.5（コピーは 1 段目）と
  3.1 の `List a` 行に記載。
  [decision](decisions/2026-11-05-boundary-mutation-contract.md)、
  [検証](sketch/2026-11-05-boundary-mutation-contract-verification.md)。

- [x] **`.gomi` で export した `let` は Go 側から再束縛できる書き換え可能な
  package var になる**: spec 7 はトップレベルの `let` を「定数」として
  説明するが、生成コードは `var Shared list.List[int64]` であり、Go の
  テストから `boundary.Shared[1] = 42`（要素書き換え）と
  `boundary.Shared = []int64{7}`（束縛ごとの再代入）の両方が効く —
  Gom 側の不変性として最も強い違反形。export を読み取り専用にする意図
  なら accessor/façade が要る; 意図どおりなら spec 7/8.7 の契約文言が
  要る（wave8 boundary-aliasing 検証で実測、契約ギャップ）。
  参照: [spec 7](spec.md)、[spec 8.7](spec.md)、
  [検証](sketch/2026-11-04-boundary-aliasing-hypotheses.md)、
  [codegen/decl.go](../internal/codegen/decl.go) `varDecl`。
  対応: 文言契約で閉じた — `var` 形状を採用として明記（Go には読み取り
  専用の exported 変数がない）。8.7 の契約が要素書き換え・再束縛を禁じる。
  accessor/façade（`var shared` + `func Shared() T`）は再束縛は防げるが
  要素書き換えは防げず `Ref`/`Go.Map` 系 `let` には効かないため、生成
  API の硬化策として decision に記録し deferred。
  [decision](decisions/2026-11-05-boundary-mutation-contract.md)。

- [x] **`(NamedIface, error)` / `(any, error)` の第 1 結果は nil-able だが
  `Result (Option T) GoError` を導出・宣言ともにできない**:
  [error-pair-ok-payload](decisions/2026-11-05-error-pair-ok-payload.md) は
  `Result (Option T) GoError` の宣言を第 1 結果が `*T`/`error` の場合に限る
  （`nilablePresence`）ため、`(net.Error, error)`・`(any, error)` は
  `Result <named> GoError`（unwrap なし）に導出され Option ビューの宣言も
  拒否される。named error interface や `any` の nil を `Ok` に紛れ込ませる
  同族の小さな穴 — spec 8.4 の admission 表はこの制限を明記していない
  （wave9 error-tail レビュー F）。広げるか文言を足すかの判断が要る。
  参照: [goimport/extern.go](../internal/goimport/extern.go) `nilablePresence`、
  [goimport/mapping.go](../internal/goimport/mapping.go) 派生写像、
  [spec 8.4](spec.md)。
  対応: メソッド持ち interface を nil-able クラスとして採用 —
  `(I,error)` の派生写像は `Result (Option T) GoError`、`ResNilable`
  （`CallGo`/`GoSig`）で伝搬し verbatim 宣言は一般化 `payloadNilGuard`
  で境界検査。`any` は methodIface でないので verbatim 維持。
  spec 8.4 の admission 表も更新済み（wave10 call-honesty、
  [decision](decisions/2026-11-06-nilable-iface-pair-ok-payload.md)、
  [検証](sketch/2026-11-06-nilable-iface-pair-ok-payload.md)）。

- [x] **返り値関数のパラメータを指す labeled 引数が無診断で位置適用に
  潰れる**: `fn f (~a: Int) -> (~b: Str) -> Int` への `f ~b: "x"` は
  `paramByLabel` が flat index 1 を返すが `pi >= declArity` で
  `overDeclared` が無診断で立ち、`ArgToParam` が nil 化して位置適用
  `f("x")` が出力される → 生成 Go が build 不能。同型なら
  `f ~b: 5` → `f(5)` で `~a=5` の誤束縛が静かに走る。spec は labeled
  引数を「名前で束縛」と規定するが、この経路だけ受理→誤訳に落ちる
  （accept→unbuildable/misbind、wave9 small-bugs レビュー F、pre-existing。
  `reclassifyCalls` の ArgToParam ガードが依存する不変条件でもある）。
  参照: [check/apply.go](../internal/check/apply.go) `overDeclared`、
  [check/reclassify.go](../internal/check/reclassify.go) ガード、
  [sketch](sketch/2026-11-05-reclassify-arg-to-param-guard.md)。
  対応: 意味拡張を採用 — カリー鎖をまたいだラベル引数は η 展開
  （`f ~b: "x"` = `fn a -> f a ~b: "x"`）と決定し、`ArgToParam` を
  flat ドメインに拡張、prefix 規則 `ClassMappedCall` で分類を共有。
  spec §2 規則 6 として明記（wave10 call-honesty、
  [decision](decisions/2026-11-06-label-args-into-returned-functions.md)、
  [検証](sketch/2026-11-06-label-args-into-returned-functions.md)）。

- [ ] **`expr.go` に raw `.String()` 描画が残り `?n` リークの可能性
  （到達性は未確認）**: E0402/E0403/E0410/E0411 は wave10 hygiene で
  `types.Display`+`metaNamer` に修正済みだが、E0502 `data["type"]`
  （expr.go ~363、`recv.Type()` raw）、E0401 `data["type"]`（~728）、
  E0507 `data["from"/"to"]`（~773）が同クラス候補として残る。E0507 は
  `convOK` がトップレベル meta のみ defer するため入れ子 meta が出うる
  （例 `Go.conv ([todo ()]) : Go.Slice Int`）。E0417 `judgeT` は常に
  具象で安全。メッセージは spec の診断形式と齟齬しうる。
  参照: [check/expr.go](../internal/check/expr.go)、
  [decision](decisions/2026-11-06-spec-suite-root-bare-side-files.md)
  同 wave の修正対象外。wave10 hygiene レビュー・worker が独立に指摘。

- [ ] **eval の初期化順は Go の ready-in-declaration-order とずれうる
  （pre-existing、multi-file eval で記録）**: eval は依存を forcing 順に
  遅延駆動するが、Go は「準備完了の中で宣言が最も早い変数」を選ぶ。
  先に宣言された変数が後の変数へ推移的に依存し、その間に独立な
  ready 変数が挟まる形で順序がずれる — 観測は初期化子の副作用順
  （host 呼出しの戻り値が状態依存なら束縛値自体もずれうる）。同じ
  クラスで `test` 宣言も eval は宣言位置で実行・compiled は全 init
  完了後。単一ファイル時代から存在する機序で、`eval.NewFiles` の
  decision §6 が制約として文書化。正しく揃えるには
  `lower.CheckInitCycles` の vardep グラフを `check.Decl` 上へ移植
  する必要がある（wave10 eval-multifile レビュー、future work 扱い）。
  参照: [decision](decisions/2026-11-06-eval-package-unit.md) §6、
  [eval/eval.go](../internal/eval/eval.go) `topLets`、
  [lower init-cycle](../internal/lower/) `CheckInitCycles`。

- [x] **design.md §2.6 の "file-wide" が V-6 以降 unit-wide とずれ**:
  builtin 名の `type` 宣言の shadowing はパッケージ単位（check の
  `TypeDeclSource`、lower の `declaredT`、codegen の `g.localT` が
  `f.Unit.Types` union）なのに文書だけ file-wide のままだった。
  対応: §2.6 を unit-wide 表記に修正（wave10 eval-multifile レビュー
  指摘、pre-existing）。

- [x] **panic 位置の E2E テストでコンパイル元と逆引きのパスが不一致**:
  `TestPanicStackResolvesToGom` は `main.gom` をコンパイラの cwd に対する相対パスで
  コンパイルするが、位置 index は実行用一時ディレクトリに対する相対パスとしていた。
  実際のスタックにある絶対パスが index に一致せず、変更前の全テストでも失敗した。
  対応: 入力と index に同じ絶対パスを渡し、元の行への復元という検査内容は維持。
  参照: [テスト](../internal/build/e2e_gopos_test.go)、[検証](sketch/2026-11-05-test-performance.md)。

- [ ] **`make check` の sumtype 検査が既存の部分 switch と未列挙ノードで失敗する**:
  design 2.4 / AGENTS.md の sealed interface の網羅検査に対し、lower の Atom snapshot、
  format の parenLed、codegen の二つの fixture walker、core の集合検査で5件失敗した。
  今回の性能改善の変更前から該当コードには列挙がなく、完全チェックの計測を阻害している。
  参照: [計測記録](sketch/2026-11-05-test-performance.md)、
  [lower](../internal/lower/expr.go)、[format](../internal/format/expr.go)、
  [codegen fixture](../internal/codegen/fixture_test.go)、[core test](../internal/core/core_test.go)。

- [x] **生成Goがimportするruntime/stdlibを外側のテスト依存にも含める**:
  buildテストは子Goプロセスでruntime/stdlibを使用するが、test binary側では明示的な
  依存になっておらず、外側のGoテスト結果cacheが変更を見落とす可能性があった（再現は未確認）。
  対応: test専用のblank importでGoの依存グラフに含め、子の実行ファイルcacheとは別に追跡する。
  参照: [harness](../internal/build/harness_test.go)、[再利用の検証](sketch/2026-11-05-test-performance.md)。

- [x] **tutorial のステップ go.mod 要求が run.sh とずれていた**:
  `run.sh` は `module tutorialrun/<name>` の scratch go.mod を合成するが、
  `tutorial_test.go` の `runStep` は各ステップの `go.mod` を読み `=> ../..` を
  置換していた。"the same flow run.sh performs" という説明と実態がずれ、
  パッケージ跨ぎも別モジュール検証もないステップに go.mod 11件が
  テストから要求され続けていた（README・テストコメント・sketch の
  「各ステップは独立 Go モジュール」という記述とも不整合）。
  対応: runStep も run.sh と同じ合成に揃え、tutorial/*/go.mod を削除して
  リポジトリルートの go.mod 1つに統一。README とテストコメントは実装に
  合わせ、sketch は日付付き記録として残置。ステップ go.mod 不在での
  全ステップ成功を確認済み。
  参照: [テスト](../tutorial/tutorial_test.go)、[README](../tutorial/README.md)、
  [decision](decisions/2026-11-05-test-performance.md)、
  [sketch](sketch/2026-11-04-v1-tutorial-findings.md)。
