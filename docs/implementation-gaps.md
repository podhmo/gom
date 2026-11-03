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

- [x] **Go interface の代入規則が未実装**: spec 8.3 は Go 由来の値を代入規則に従って渡せるとするが、
  `*strings.Reader` → `io.Reader` や `*os.File` → `io.Writer` が E0401 になる。
  対応: Go 由来の引数・注釈境界で代入規則を照会し、明示的 Core 変換を生成。Gist の接続関数も削減。
  参照: [spec](spec.md#83-型の対応)、[検証](sketch/2026-11-03-go-http-boundary-verification.md)。

- [x] **interface へ渡す整数リテラルの動的型が Gom の型と異なる**: HTTP 境界の修正試作を
  独立レビューしたところ、`Int` に解決した `1` が named empty interface へ渡る時に Go の
  `int` となり、想定する `int64` と異なることを `%T` で再現した。
  対応: named interface へ包む前に `ConvNum` で数値型を具体化し、動的型を実行検証。Go.Any は別項目。
  参照: [型の対応](spec.md#83-型の対応)、[レビュー対応と再レビュー](sketch/2026-11-03-go-http-boundary-verification.md)。

- [ ] **既存の Go.Any 経路でも整数リテラルの動的型が int になる**:
  `fmt.Sprintf ("%T", 1)` を main `5e792f4` で生成・実行すると `int` を返した。
  Gom `Int` の `int64` 表現と異なり、named interface の修正だけではこの既存経路は直らない。
  参照: [型の対応](spec.md#83-型の対応)、[実行記録](sketch/2026-11-03-parallel-implementation-round.md#goany-numeric-probe)。

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

- [ ] **Float の評価器と生成 Go で丸めが異なる**: 評価器は各リテラルを float64 にして演算し、
  生成 Go は任意精度定数式を畳み込むため、同じ式の数値・文字列表示が異なる。
  参照: [詳細と未決の意味](problems/2026-11-02-float-const-fold-arithmetic.md)。

- [ ] **Float に推論された整数リテラル束縛が Go では int のままになる**:
  `fn f () -> Float = { let n = 1; n + 0.5 }` は Float に型解決されるが、
  生成 Go の `n := 1` が int を推論し `0.5 truncated to int` でコンパイルできない。
  参照: [既存の懸念](problems/2026-11-02-let-int-literal-go-type.md)、[codegen](../internal/codegen/emit.go)。

- [ ] **別バリアントのペイロード参照の結果が異なる**: `s.r` を `Rect` に適用すると、
  eval は別名の同じ位置の値を返し、生成コードは型アサーションで panic する。spec の失敗時の意味も未定義。
  参照: [詳細](problems/2026-11-02-variant-field-wrong-variant.md)。

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

- [ ] **ignore の problem file が実装済みなのに未解決のまま**:
  problems/2026-11-04-ignore-discard-syntax-unparsed.md は「パーサーに ignore がない（E0201）」
  と主張するが、現在は `ignore` がキーワードとして実装済みで tutorial 全章も使用している。
  ファイル自体の状態が更新されていない。
  参照: [problem](problems/2026-11-04-ignore-discard-syntax-unparsed.md)、[lexer](../internal/syntax/token.go)、[tutorial](../tutorial/01-hello/main.gom)。

- [ ] **overapply-labeled-params の problems doc の記述が陳腐化**:
  `mk () 5` が位置引数で `~x` を埋めると説明するが、`flattenFunc` が全
  arrow を畳むため `overApply` の `rparams` 分岐は到達不能になり、
  現在は常に meta 拡張で E0401 になる (spec 2 規則3と結果は整合)。
  到達不能分岐の削除と E0401 文言の是非が残る未決。
  参照: [problem](problems/2026-11-03-overapply-labeled-params.md)（追記済み）、
  [overApply](../internal/check/apply.go)、
  [fixture](../testdata/spec/E0401_positional_for_labeled.gom)、
  [検証](sketch/2026-11-05-p2-3-labeled-args.md)。

- [x] **E0404 のコード表の例が発火しない形を示していた**:
  例の `transfer alice ~to: bob` は Gom 呼び出しでは合法な部分適用で、
  E0404 は Go callee でしか発火しない。対応: 例を `extern` 経由の
  Go 呼び出しに差し替えた。Gom 側の文言の未決は problems doc が追跡。
  参照: [error-codes](error-codes.md#e0404--missing-labeled-argument)、
  [problem](problems/2026-11-02-gom-missing-label-is-partial.md)、
  [fixture](../testdata/spec/E0404_go_missing_label.gom)。

- [ ] **CLI usage の計画中コマンド名が P4-1 の決定と食い違う**:
  `cmd/gom/main.go` の usage は計画コマンドとして `gomi` を列挙するが、
  TODO P4-1 は `moon info` 流の `info` という名前を決めている（spec 7.5 も `gom gomi` 表記）。
  実装はまだ無いが、名前の正が文書間で不一致。
  参照: [usage](../cmd/gom/main.go)、[TODO](../TODO.md)、[spec 7](spec.md#7-インターフェースファイル-gomi)、[sketch](sketch/2026-11-05-moon-cli-command-structure.md)。

- [ ] **`struct{}` の Gom 側の対応が定められていない**: spec 8.3 の対応表にも v1 未対応リストにも
  `struct{}` が現れないが、spec 10 章の例は `ctx.Done()` の `<-chan struct{}` を `select` の腕に書く
  （`Unit` → `struct{}` なら `RecvChan Unit` が自然）。実装は "unsupported Go type struct{}" で
  E0501 にするため、10 章の例はメンバー未実装とは別の理由でもコンパイルできない。
  参照: [spec 8.3](spec.md#83-型の対応)、[spec 10](spec.md#10-並行処理)、
  [mapping](../internal/goimport/mapping.go)、[監査](sketch/2026-11-03-spec-examples-audit.md)。

- [ ] **spec の例の `Result Int64 GoError` が 8.3 節の対応表と食い違う**: 9.2 節の
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
- [ ] **`.gomi` の `let` メンバーは表面に出るが外部参照との形が一致しない**:
  generalized let は thunk 実装になるが、`internal/stdlib` は `MemberLet` を
  KindVar シンボルとして登録し var 参照を生成する — 単相 let なら動くが
  一般化 let は不整合。現行 stdlib の `.gomi` は `fn` しか宣言しないので不活性。
  参照: [internal/stdlib](../internal/stdlib/stdlib.go)、
  [iface](../internal/iface/iface.go)。
- [ ] **`.gomi` の type メンバーはモジュールメンバ表に乗らない**:
  `internal/stdlib` は `fn`/`let` メンバーだけを `Modules` 表に登録し、
  `MemberType` はスキップする（モジュール内型 `List.Foo` の参照経路は未実装）。
  現行の `.gomi` は型を宣言しないので不活性。
  参照: [internal/stdlib](../internal/stdlib/stdlib.go)。
- [ ] **`SelfImportPath` は `stdlib/<pkg>/` の予約ディレクトリ規約**:
  `gomstd.SelfPath` は入力パスに `stdlib/list|option|result/` を含むと
  そのパッケージの canonical import path を返す。ユーザーのモジュールが
  同名レイアウトを使うと同じ規約が適用される（builtin 型の参照が
  無修飾でそのローカルパッケージを指す）。
  参照: [internal/stdlib](../internal/stdlib/stdlib.go)、
  [decision](decisions/2026-11-03-stdlib-packaging.md)。
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

- [ ] **`checkInitCycles` は file-local で Go のパッケージ単位の分析とずれる**:
  E0907 の初期化依存グラフは 1 ファイルの Core IR 上に構築するが、Go は
  パッケージ単位で初期化順を決める。複数 `.gom` ファイルのパッケージが実現すると、
  ファイル跨ぎの初期化サイクルが Gom 診断を通り抜け `go build` のエラーとして表れる。
  参照: [initcycle.go](../internal/lower/initcycle.go)、
  [decision](decisions/2026-11-05-aggregate-function-bindings.md)。

- [ ] **spec にパッケージ初期化順序の節がない**: E0907（初期化循環の拒否）の
  `Spec:` 参照先が未定義 — `@tailrec` 系の E09xx は spec 1.1 を参照するが、
  初期化順序の言語仕様自体は spec にない。spec 節を新設するかは未決。
  参照: [codes.go](../internal/diag/codes.go)、[spec](spec.md)。

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
- [ ] **メソッド呼び出しの連鎖 `x.M(1).N(2)` が arg タプルへの select として parse される**:
  postfix の selector が適用の引数タプルより強く結合するため、`.N` は `(1)` の
  後続ではなくタプル要素とみなされる。`x.AddDate(1,0,0).AddDate(0,1,0)` は
  E0401（arity / "tuple type has no AddDate"）になるが、明示的な
  `(x.AddDate(1,0,0)).AddDate(0,1,0)` は動く。解決済み receiver でも同じ
  失敗 — 推論の問題ではなく grammar の優先順位の問題（spec 8.1.3 は連鎖を定めていない）。
  参照: [problem](problems/2026-11-05-method-chain-select-binds-args.md)、
  [検証の観測](sketch/2026-11-05-deferred-member-application.md)。

- [ ] **eval が Go 呼び出し内の `..e` spread を評価できない**: `*check.Spread` は
  list literal 内でのみ処理され、Go call の引数位置では `CodeUnsupported`。
  CallGo/variadic を eval 側が完全にカバーするには spread 展開の実装が要る
  （P3-4 レビューで観測、pre-existing）。
  参照: [検証](sketch/2026-11-05-deferred-member-application.md) の Remaining boundary。

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
- [ ] **兄弟ファイルで同名 `test` ブロックは Go 側で重複シンボルになる**:
  パッケージ単位コンパイルで `gomEq*`/`tmp*` 系はファイルタグで回避するが、
  `test "name"` 由来の `func TestX` は無修飾のため、2 ファイルに同名 test が
  あると生成コードの `go build` が重複定義で失敗する（Gom 診断ではない）。
  参照: [problems](problems/2026-11-05-gom-package-imports.md)、
  [decision](decisions/2026-11-05-package-compilation-unit.md)。

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

- [ ] **`gom build` が E0601（`.gomi` 不整合）でも `.go` を書き出す**: `checkInterfaces` は
  ファイルごとの codegen の後に走るため `r.Output` は既に設定済みで、exit 1 ながら出力は
  書かれる（サマリには正直に列挙される）。F3 のユニット原子性（失敗したユニットは出力を
  出さない）との整合が取れていない — interface 不整合をコンパイルエラーと見なして
  出力を抑止するかは spec 判断が要る。
  参照: [cli.go](../cmd/gom/cli.go)、[build.go](../internal/build/build.go)。

- [ ] **exported `let foo` と `type Foo` が生成 Go で同名 `Foo` になる**: 単一ファイルでも
  再現（pre-existing）。export 値名と型名が大文字名前空間を共有し dedup されない。
  参照: [codegen/gen.go](../internal/codegen/gen.go)。
