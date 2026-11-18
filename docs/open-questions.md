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

- [x] 生成コード特殊化ポリシー（curried HOF 側）: 呼出側 uncurry（flat fn params）を採択 — 宣言 >=2 本矢印パラメータを flat `func(T1',...,Tk') R'` にし、`Fold` の要素ごと確保を解消（~12×）。wave20 lane 2、[decision](decisions/2026-11-18-flat-fn-params.md)、[検証](sketch/2026-11-18-flat-fn-params-verify.md)。コピーしない共有スライス型（View）の案は [problems](problems/2026-11-04-shared-slice-view-type.md)
- [x] `concat` の O(k) スタック: `@extern fn` で `Concat` を impl.go の buffer loop へ — 総長 prealloc の 1 回コピー、O(1) スタック・O(n)・1 alloc（k=512 で 1.51ms → 21µs、~72×）。wave19 lane 3、[sketch](sketch/2026-11-18-concat-buffer-impl.md)
- [x] 独立 dir 並列コンパイルの実測ベンチ: wave21 lane3 で Kahn 層 + GOMAXPROCS ワーカー化済み（[decision](decisions/2026-11-18-parallel-dir-compile.md)）だが wall-clock の効果は未計測。複数 dir・dep 連鎖 fixture での計測は wave22 候補（prompts 草案 案4） → wave22 lane1 で実測: serial 比約 2.8 倍（単一プロセス化が支配的）、層内並列でさらに -22〜-32%（[sketch](sketch/2026-11-18-wave22-parallel-bench.md)）

### ツール

- [ ] 冗長な腕は spec 4 章どおりエラーのまま。残る問いは、書きかけのコードで冗長な腕などの一部エラーを一時的にフィルタして書き進めたい用途（`--strict` の有無で切り替えるようなオプション）があるかどうか。オプション自体を入れるかも未決
- [ ] ソース位置対応表（P4-5 のアンカー表）について、LSP はアンカー粒度（生成行 → 内側構文の span）より細かい列・範囲単位の対応を要するか。`//line` ドリフトの行エイリアシングへの抜本対応は wave13 で基盤着地（`ResolveMatch` の exact/drift/ambiguous + 曖昧時 union fallback — [decision](decisions/2026-11-06-gotool-position-inverse.md)）、wave16 の per-line re-pin で drift そのものを消去（[decision](decisions/2026-11-17-sourcemap-repin.md)）。LSP 側の go tool 起動点は wave15 の `gom lsp -go` で着地（[decision](decisions/2026-11-17-lsp-gotool-invocation.md)）
- [ ] `gom fix` でも `.gomi` 適合検査を走らせるか（`gom check`/`build`/`test` は走らせると決定済み — [decision](decisions/2026-11-04-gomi-conformance-in-gom-test.md)。`info`/`dump` は対象外）。2026-11-05 に検討して見送り: 適合診断は fix 候補プールに加わるだけ・ゲート無し・診断非表示という形が妥当だが、現状 E0601/E0603/E0604 に safe fix がなく観測差ゼロのため先送り。interface 診断に safe fix が付くなど価値が具体化した時点で再検討する（[検討の記録](decisions/2026-11-05-gomi-conformance-in-gom-fix.md)）
- [x] ~~`gom run`（`go run` 相当の実行経路）~~ → wave19 で採用・実装済み: compile → temp dir への `go build -o` → 子プロセス exec（引数 passthrough・exit code 保全・signal kill は 128+signal）。実行中の出力は対象プログラムが stdout/stderr を所有し、gom の JSON message stream は失敗時のみ（[decision](decisions/2026-11-18-gom-run.md)）
## Future work（v1 では扱わないと決めたもの）

- ラベル付き引数の制限の緩和（型が未知の関数へのラベル付き適用、ラベル付き／なし関数の暗黙変換）
- 省略可能引数とデフォルト値
- `|?>` の自動ラップの文言を設定可能にする。残る未決は文言のカスタマイズのみ — 書式（関数名・ソース位置・任意の文言）をパッケージ単位で指定する方法か、ステップごとに文言を書ける構文か。ステップ名由来の `step: message` 文言・構造化フレーム `rt.StepError{Step, Err}`・`strconv` 二重名は provenance として容認する方針は [result-pipe-wrap-frame](decisions/2026-11-05-result-pipe-wrap-frame.md) で採用済み
- ローカル let 多相（入れる場合は relaxed value restriction を検討）
- `List` のハイブリッド表現（chunked / unrolled list、所有権フラグ付きの反転ストレージなど）。先頭追加を償却 O(1) に戻す案。P0-3 でスライス表現（`type List[A] []A`）を採用したが、ユーザーレベルの `x :: acc` 型の逐次構築が支配的なワークロードで再検討する余地がある（[sketch: P0-3](sketch/2026-11-02-p0-3-list-representation.md)）
- チャネルで送れる値を型で制限する仕組み（Rust の `Send` 相当）
- `Char` ↔ `Int32` の暗黙の橋渡しを許すか: Go の `rune` 引数 API には `Int32` を渡す必要があり `Char` 値は `Go.conv` を通す。両者の Go 表現は同じ `rune` なので機械的に相互変換可能だが、暗黙変換や専用組み込みを足すかは未決 — v1 は `Go.conv` 明示のまま、ユースケースが集まってから再検討（[problem](problems/2026-11-06-rune-char-correspondence.md)、wave20 lane4 で台帳化判定）
- 型クラス、`deriving` 相当の自動生成
- LSP の Go import oracle の共有: `go list -export` 自体は wave17 の file cache で消えた（[decision](decisions/2026-11-18-golist-export-cache.md)）が、compile 毎の Loader 作り直しで export data の parse と member table 構築は残る（per-change 数 ms オーダー）。`*types.Named` の identity が importer 単位のため session 間で member table を共有できない点が本筋の制約。wave19 再評価: 制約は importer を compile 毎に捨てていること自体にある — LSP が workspace dir ごとに**長寿命な `goimport.Loader` を1本持ち回る**（session-scoped shared importer）設計にすれば `*types.Named` identity は安定し、member table も compile 間で共有できる。invalidation の粒度は **Loader 全体** — `Loader.Load` の `l.pkgs` と `go/importer` の fset キャッシュは exportCache の freshness 検証より先に hit するため、compile のたびに exportcache fingerprint（srcStat: dep dir 単位の max ファイル mtime+件数）を先に照合し、miss なら Loader ごと作り直す（go.mod/go.sum の変更だけでなく同一バージョンの dep ソース編集も拾える）。srcStat はファイル単位の mtime を見るため既存ファイルへの編集も検出される。実装コストは LSP server が Loader を fingerprint つきで保持・差し替える程度 — 実行経路はあり、per-change の ms オーダー削減としては費用対効果は中程度
- インクリメンタルビルドのキャッシュ（P4。それまでは毎回変換し、Go のキャッシュに任せる）
- cgo、`unsafe`、`reflect` の本格的な扱い
- セルフホスト（コンパイラを Gom で書き直す）
- W0502 `lintErrorsAsTarget` の `Ptr (Ptr E)` ケース: `**int` は `*int` が error 非実装かつ非 interface なので panic が証明可能だが、現状は nested-Ptr 除外で沈黙。証明可能な違反の網羅を広げるかは将来の検討（wave9 error-tail レビュー nit）
- `CtorApp`/`CtorRef` の `TypeArgs` を廃止して `Typ` の `Con.Args` を唯一の正にするか: wave21 lane1 は (a) optional-but-authoritative（存在時は結果型と完全一致を要求）を採択した（[decision](decisions/2026-11-18-ctor-typeargs-authoritative.md)）。(b) への移行は lower/codegen のフィールド撤去を伴う規模のため future work とする

型クラス、エフェクトシステム、高度な所有権型、独自 GC は急いで導入しない。
