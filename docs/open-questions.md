# 未決事項と future work

決着したら [decisions/](decisions/) に 1 件 1 ファイルで記録し、[spec.md](spec.md) / [design.md](design.md) を更新してから、この一覧から消す。決まっていて作業するだけのものは [../TODO.md](../TODO.md) に移す。

## 未決事項

### 型システム

- [ ] 型クラスの代替: v2 で型クラスを入れるか、明示的な辞書渡し（`Eq a` レコードを引数で渡す）で済ませるか
- [ ] 実装の型がインターフェースより一般的な場合（実装が多相、`.gomi` が単相）を許すか
- [ ] `.gomi` の `let` メンバーに多相型（`let empty : List a`）を許すか。許すなら member table に thunk 表現が要る（実装の generalized let は `func X[T]()` を出すが、member 参照は `p.X` — check/lower の thunk oracle を import 越しに届ける）。v1 では E0602 で型変数を拒否する bounded 形（[検証](sketch/2026-11-05-v6-residual-verification.md)、[台帳](implementation-gaps.md)）
- [ ] 可変性: `Ref a` と可変配列 `Array a` 以外に可変構造を許すか。なお W0701/W0702 lint の可変判定は spec 10 章の閉じた builtin リスト（`Ref`/`Array`/`Ptr`/`Go.Slice`/`Go.Map`）を使う — ユーザー宣言型の可変構造そのものは引き続き未決（[decision](decisions/2026-11-04-mutable-sharing-lint.md)）

### Go 相互運用

- [ ] Go ABI の方針: 生成される Go の API（型名・関数のシグネチャ・バリアントの struct 名）を、Gom のバージョンをまたいでどこまで安定させるか
- [ ] Gom の型が Go の interface を実装できるようにするか（v1 ではできない）。できるなら、どう宣言するか
- [ ] Go のジェネリック関数のうち、`any` / `comparable` 以外の制約を持つものをどう扱うか。P0-5 で、`any`/`comparable` だけの厳格な境界では `slices.Contains`（`[S ~[]E, E comparable]`）など `slices`/`maps`/`cmp` の主要な関数がほぼ全部呼べないことが分かった。P3-3 で `any`/`comparable` は呼び出し可能になったが、`~[]E`/`~map[K]V` などの構造制約は引き続き E0506 で非公開 — spec 8.1.6 の例とのずれは [gaps](implementation-gaps.md) 参照。広げるか、spec を「`any`/`comparable` のみ」と修正するかの判断が残る（[problems](problems/2026-11-04-generic-go-fns-hidden.md)、[go-interop-boundary](decisions/2026-11-01-go-interop-boundary.md)、[sketch: P0-5](sketch/2026-11-02-p0-5-go-types-mapping.md)）
- [ ] Go の struct 型の構築・書き込み: exported field の読み取りは [決定済み](decisions/2026-11-03-go-interface-assignment-and-fields.md)。コンポジットリテラル（`http.Server{Addr: ...}`）や field への代入を許すか、構築は Go の factory 関数や `extern` に限るか。境界での別名と可変性の仮説は [sketch](sketch/2026-11-04-boundary-aliasing-hypotheses.md)
- [ ] 外部モジュールから stdlib の Gom モジュール（`import "list"` 等）を import 可能にするか。現状はメイン module 外の Gom import が E0203 で拒否される（P3-6 で発見 — `testdata/gocall` の fixture は stdlib 依存を手書きで代替した）

### 最適化

- [ ] 生成コード特殊化ポリシー: cons 構築ループの buffer 化をどの経路で行うか（`Array` builder＋`to_list` / intrinsic・extern-impl 経路 / lower の cons-recursion パターン特殊化）、curried HOF の flat 二面公開や呼出側 uncurry を入れるか。`Map`/`Filter`/`Append`/`Rev`/`Range` の O(n²) と `Fold` の要素ごとクロージャ確保が対象（[problems](problems/2026-11-05-stdlib-cons-built-combinators-quadratic.md)、[sketch](sketch/2026-11-05-p5-3-generated-code-perf.md)。コピーしない共有スライス型（View）の案は [problems](problems/2026-11-04-shared-slice-view-type.md)）
- [ ] `concat` の O(k) スタック: 右畳み込み化で O(1) スタックの fold を O(k) 再帰に替えた（深さ = 部分リスト数）。buffer 経路ができたらより良い形状を再検討する余地

### ツール

- [ ] 冗長な腕は spec 4 章どおりエラーのまま。残る問いは、書きかけのコードで冗長な腕などの一部エラーを一時的にフィルタして書き進めたい用途（`--strict` の有無で切り替えるようなオプション）があるかどうか。オプション自体を入れるかも未決
- [ ] ソース位置対応表（P4-5 のアンカー表）について、LSP はアンカー粒度（生成行 → 内側構文の span）より細かい列・範囲単位の対応を要するか。`//line` ドリフトの行エイリアシングの抜本対応（SourceMap-aware な逆引き＋曖昧時フォールバック）も P4-6 の設計入力
- [ ] `gom fix` でも `.gomi` 適合検査を走らせるか（`gom check`/`build`/`test` は走らせると決定済み — [decision](decisions/2026-11-04-gomi-conformance-in-gom-test.md)。`info`/`dump` は対象外）。2026-11-05 に検討して見送り: 適合診断は fix 候補プールに加わるだけ・ゲート無し・診断非表示という形が妥当だが、現状 E0601/E0603/E0604 に safe fix がなく観測差ゼロのため先送り。interface 診断に safe fix が付くなど価値が具体化した時点で再検討する（[検討の記録](decisions/2026-11-05-gomi-conformance-in-gom-fix.md)）
## Future work（v1 では扱わないと決めたもの）

- eval 初期化順の static-alias 残差: ready-in-declaration-order の frontier 駆動は実装済み（[decision](decisions/2026-11-06-eval-init-ready-order.md)）で通常の let/fn は compiled と一致。残るのは lower が ValueAlias FuncDecl に落とす `let` のみ — 副作用つき alias 初期化式の temp 分割と、関数パスが alias 自身へ戻る pure-alias ループ（`let f = g` + `fn g = { ignore (f); … }`）で初期化順がずれうる。`internal/lower/alias.go` の分類は check 型上で完結しているため、共有層への引き上げはファイル移動に近い。ずれの記録は [台帳](implementation-gaps.md)

- ラベル付き引数の制限の緩和（型が未知の関数へのラベル付き適用、ラベル付き／なし関数の暗黙変換）
- 省略可能引数とデフォルト値
- `|?>` の自動ラップの文言を設定可能にする。残る未決は文言のカスタマイズのみ — 書式（関数名・ソース位置・任意の文言）をパッケージ単位で指定する方法か、ステップごとに文言を書ける構文か。ステップ名由来の `step: message` 文言・構造化フレーム `rt.StepError{Step, Err}`・`strconv` 二重名は provenance として容認する方針は [result-pipe-wrap-frame](decisions/2026-11-05-result-pipe-wrap-frame.md) で採用済み
- ローカル let 多相（入れる場合は relaxed value restriction を検討）
- `List` のハイブリッド表現（chunked / unrolled list、所有権フラグ付きの反転ストレージなど）。先頭追加を償却 O(1) に戻す案。P0-3 でスライス表現（`type List[A] []A`）を採用したが、ユーザーレベルの `x :: acc` 型の逐次構築が支配的なワークロードで再検討する余地がある（[sketch: P0-3](sketch/2026-11-02-p0-3-list-representation.md)）
- チャネルで送れる値を型で制限する仕組み（Rust の `Send` 相当）
- 型クラス、`deriving` 相当の自動生成
- インクリメンタルビルドのキャッシュ（P4。それまでは毎回変換し、Go のキャッシュに任せる）
- cgo、`unsafe`、`reflect` の本格的な扱い
- セルフホスト（コンパイラを Gom で書き直す）
- W0502 `lintErrorsAsTarget` の `Ptr (Ptr E)` ケース: `**int` は `*int` が error 非実装かつ非 interface なので panic が証明可能だが、現状は nested-Ptr 除外で沈黙。証明可能な違反の網羅を広げるかは将来の検討（wave9 error-tail レビュー nit）

型クラス、エフェクトシステム、高度な所有権型、独自 GC は急いで導入しない。
