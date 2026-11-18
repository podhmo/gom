# 未決事項と future work

決着したら [decisions/](decisions/) に 1 件 1 ファイルで記録し、[spec.md](spec.md) / [design.md](design.md) を更新してから、この一覧から消す。決まっていて作業するだけのものは [../TODO.md](../TODO.md) に移す。

## 未決事項

### 型システム

- [ ] 型クラスの代替: v2 で型クラスを入れるか、明示的な辞書渡し（`Eq a` レコードを引数で渡す）で済ませるか
- [ ] 実装の型がインターフェースより一般的な場合（実装が多相、`.gomi` が単相）を許すか

- [ ] 可変性: `Ref a` と可変配列 `Array a` 以外に可変構造を許すか。なお W0701/W0702 lint の可変判定は spec 10 章の閉じた builtin リスト（`Ref`/`Array`/`Ptr`/`Go.Slice`/`Go.Map`）を使う — ユーザー宣言型の可変構造そのものは引き続き未決（[decision](decisions/2026-11-04-mutable-sharing-lint.md)）

### Go 相互運用

- [ ] Go ABI の方針: 生成される Go の API（型名・関数のシグネチャ・バリアントの struct 名）を、Gom のバージョンをまたいでどこまで安定させるか
- [ ] Gom の型が Go の interface を実装できるようにするか（v1 ではできない）。できるなら、どう宣言するか

### 最適化

- [ ] 生成コード特殊化ポリシー（curried HOF 側）: curried HOF の flat 二面公開や呼出側 uncurry を入れるか — `Fold` の要素ごとクロージャ確保が対象。buffer 化の経路問題は wave18 で決着（`Array` builder＋`@extern fn` — [decision](decisions/2026-11-18-cons-buffer-array-extern-impl.md)、[problems](problems/2026-11-05-stdlib-cons-built-combinators-quadratic.md)）。コピーしない共有スライス型（View）の案は [problems](problems/2026-11-04-shared-slice-view-type.md)
- [ ] `concat` の O(k) スタック: 右畳み込み化で O(1) スタックの fold を O(k) 再帰に替えた（深さ = 部分リスト数）。buffer 経路ができたらより良い形状を再検討する余地

### ツール

- [ ] 冗長な腕は spec 4 章どおりエラーのまま。残る問いは、書きかけのコードで冗長な腕などの一部エラーを一時的にフィルタして書き進めたい用途（`--strict` の有無で切り替えるようなオプション）があるかどうか。オプション自体を入れるかも未決
- [ ] ソース位置対応表（P4-5 のアンカー表）について、LSP はアンカー粒度（生成行 → 内側構文の span）より細かい列・範囲単位の対応を要するか。`//line` ドリフトの行エイリアシングへの抜本対応は wave13 で基盤着地（`ResolveMatch` の exact/drift/ambiguous + 曖昧時 union fallback — [decision](decisions/2026-11-06-gotool-position-inverse.md)）、wave16 の per-line re-pin で drift そのものを消去（[decision](decisions/2026-11-17-sourcemap-repin.md)）。LSP 側の go tool 起動点は wave15 の `gom lsp -go` で着地（[decision](decisions/2026-11-17-lsp-gotool-invocation.md)）
- [ ] `gom fix` でも `.gomi` 適合検査を走らせるか（`gom check`/`build`/`test` は走らせると決定済み — [decision](decisions/2026-11-04-gomi-conformance-in-gom-test.md)。`info`/`dump` は対象外）。2026-11-05 に検討して見送り: 適合診断は fix 候補プールに加わるだけ・ゲート無し・診断非表示という形が妥当だが、現状 E0601/E0603/E0604 に safe fix がなく観測差ゼロのため先送り。interface 診断に safe fix が付くなど価値が具体化した時点で再検討する（[検討の記録](decisions/2026-11-05-gomi-conformance-in-gom-fix.md)）
## Future work（v1 では扱わないと決めたもの）

- ラベル付き引数の制限の緩和（型が未知の関数へのラベル付き適用、ラベル付き／なし関数の暗黙変換）
- 省略可能引数とデフォルト値
- `|?>` の自動ラップの文言を設定可能にする。残る未決は文言のカスタマイズのみ — 書式（関数名・ソース位置・任意の文言）をパッケージ単位で指定する方法か、ステップごとに文言を書ける構文か。ステップ名由来の `step: message` 文言・構造化フレーム `rt.StepError{Step, Err}`・`strconv` 二重名は provenance として容認する方針は [result-pipe-wrap-frame](decisions/2026-11-05-result-pipe-wrap-frame.md) で採用済み
- ローカル let 多相（入れる場合は relaxed value restriction を検討）
- `List` のハイブリッド表現（chunked / unrolled list、所有権フラグ付きの反転ストレージなど）。先頭追加を償却 O(1) に戻す案。P0-3 でスライス表現（`type List[A] []A`）を採用したが、ユーザーレベルの `x :: acc` 型の逐次構築が支配的なワークロードで再検討する余地がある（[sketch: P0-3](sketch/2026-11-02-p0-3-list-representation.md)）
- チャネルで送れる値を型で制限する仕組み（Rust の `Send` 相当）
- 型クラス、`deriving` 相当の自動生成
- LSP の Go import oracle の共有: `go list -export` 自体は wave17 の file cache で消えた（[decision](decisions/2026-11-18-golist-export-cache.md)）が、compile 毎の Loader 作り直しで export data の parse と member table 構築は残る（per-change 数 ms オーダー）。`*types.Named` の identity が importer 単位のため session 間で member table を共有できない点が本筋の制約
- インクリメンタルビルドのキャッシュ（P4。それまでは毎回変換し、Go のキャッシュに任せる）
- lower の cons-recursion 特殊化: match-cons 再帰形（`map`/`filter`/`append` 様）を検出して buffer loop を emit する自動変換 — ユーザーコードの cons 蓄積も救えるが検出ポリシー・等価性検証の保守が重い。wave18 で (a) `Array` builder と (b) `@extern fn` を先に採択し、本項は future work として後送り（[decision](decisions/2026-11-18-cons-buffer-array-extern-impl.md)）
- cgo、`unsafe`、`reflect` の本格的な扱い
- セルフホスト（コンパイラを Gom で書き直す）
- W0502 `lintErrorsAsTarget` の `Ptr (Ptr E)` ケース: `**int` は `*int` が error 非実装かつ非 interface なので panic が証明可能だが、現状は nested-Ptr 除外で沈黙。証明可能な違反の網羅を広げるかは将来の検討（wave9 error-tail レビュー nit）

型クラス、エフェクトシステム、高度な所有権型、独自 GC は急いで導入しない。
