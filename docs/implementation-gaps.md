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

- [ ] **集約値経由の循環関数値は初期化循環が残る**: 単純別名は直ったが、
  `let (again, _) = (count, 0)` を関数本体から参照すると check/build 成功後に Go が拒否する。
  参照: [再現](sketch/2026-11-03-static-function-alias-verification.md)、[TODO P2-2c](../TODO.md)。

- [x] **`|?>` の失敗が外側の関数を脱出する**: spec 9.2 と評価器は Result 値を返すが、
  lower は外側から早期 return し、パイプ外の処理が続かないことがあった。
  対応: 局所境界と明示的 `try` の決定木経路で仕様に合わせ、短絡・継続・error chain を検証。
  参照: [spec 9.2](spec.md#92-意味)、[検証](sketch/2026-11-03-result-pipe-local-boundary.md)。

- [x] **Result パイプの Go 流入だけ数値・Unit の扱いが異なる**: 通常呼び出しでは行う
  `int` → `Int` 変換が抜け、戻り値なし呼び出しを値として代入する生成 Go もあった。
  対応: 通常の境界変換を適用し、数値・多値・Unit の生成 Go を実行検証。
  参照: [検証](sketch/2026-11-03-result-pipe-local-boundary.md)。

- [ ] **明示的 `try` を含むパイプは中間 Result が残る**: design 4.6 の中間 Result を省く生成に対し、
  脱出先を保存する保守的な経路には未融合の Result が残る。値の意味は検証済み。
  参照: [design](design.md)、[検証](sketch/2026-11-03-result-pipe-local-boundary.md)、[TODO P2-4b](../TODO.md)。

- [ ] **Go slice の既定対応が spec と異なる**: spec 8.3/8.5 は `[]T` → `List T` と両方向コピー、
  loader は `Go.Slice T` を返す。`Go.conv os.Args : List String` も E0507。
  参照: [spec](spec.md#83-型の対応)、[mapping](../internal/goimport/mapping.go)、[Gist probe](sketch/2026-11-03-gist-cli-prototype.md)。

- [ ] **Go interface の代入規則が未実装**: spec 8.3 は Go 由来の値を代入規則に従って渡せるとするが、
  `*strings.Reader` → `io.Reader` や `*os.File` → `io.Writer` が E0401 になる。
  参照: [spec](spec.md#83-型の対応)、[Gist probe](sketch/2026-11-03-gist-cli-prototype.md)。

- [ ] **interface へ渡す整数リテラルの動的型が Gom の型と異なる**: HTTP 境界の修正試作を
  独立レビューしたところ、`Int` に解決した `1` が named empty interface へ渡る時に Go の
  `int` となり、想定する `int64` と異なることを `%T` で再現した。
  参照: [型の対応](spec.md#83-型の対応)。試作 `codex/http-boundary` の追加修正対象。

- [ ] **既存の Go.Any 経路でも整数リテラルの動的型が int になる**:
  `fmt.Sprintf ("%T", 1)` を main `5e792f4` で生成・実行すると `int` を返した。
  Gom `Int` の `int64` 表現と異なり、named interface の修正だけではこの既存経路は直らない。
  参照: [型の対応](spec.md#83-型の対応)、[実行記録](sketch/2026-11-03-parallel-implementation-round.md#goany-numeric-probe)。

- [ ] **Go チャネルを interface に渡せない**: 同じ修正試作で、Go から返る `chan string` を
  named empty interface の引数へ渡すと E0401 になった。Go の代入規則では可能。
  参照: [interface の規則](spec.md#83-型の対応)。試作 `codex/http-boundary` の追加修正対象。

- [ ] **型が後で分かる Go field を interface に渡せない**: 同じ試作で、高階関数のラムダ内の
  `model.Accept x.Count` はレシーバの型が後で決まると E0401 になる。
  数値リテラル以外の未解決型についても、field 解決後まで代入判定を待つ必要がある。
  参照: [interface の規則](spec.md#83-型の対応)。試作 `codex/http-boundary` の追加修正対象。

- [ ] **Go struct フィールドの参照が未決・未実装**: HTTP の Header/StatusCode/Body を使いたいが、
  spec には対応表がなく、open-questions に未決として残り、実装は参照を拒否する。
  参照: [未決事項](open-questions.md)、[Gist probe](sketch/2026-11-03-gist-cli-prototype.md)。

- [ ] **Go ジェネリック関数の扱いが文書内でも異なる**: spec 8.3 は未対応、8.5 は
  `any` / `comparable` 制約なら呼べるとするが、loader はすべて非公開にする。
  参照: [spec](spec.md#83-型の対応)、[詳細](problems/2026-11-04-generic-go-fns-hidden.md)。

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
