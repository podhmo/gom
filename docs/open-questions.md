# 未決事項と future work

決着したら [decisions/](decisions/) に 1 件 1 ファイルで記録し、[spec.md](spec.md) / [design.md](design.md) を更新してから、この一覧から消す。決まっていて作業するだけのものは [../TODO.md](../TODO.md) に移す。

## 未決事項

### 型システム

- [x] 実装の型がインターフェースより一般的な場合（実装が多相、`.gomi` が単相）を許すか — 許さない（厳格一致維持、案 B 採択・2026-11-23）。狭めたい場合は明示的な単相ラッパを書く（[decision](decisions/2026-11-23-gomi-monomorphic.md)）

- [x] 可変性: `Ref a` と可変配列 `Array a` 以外に可変構造を許すか — 可変構造の入口は builtin 集合のまま、宣言型はフィールド推移律で導出（`containsMutable` は tyCons 展開済み）。残る隙間は extern 裏付け型のみで `.gomi` の `//gom:mutable` directive を採択（wave36 lane G、[decision](decisions/2026-11-23-user-mutable-structures.md)）。`Ref` のメンバー面は wave25 で実装済み（[decision](decisions/2026-11-19-ref-members.md)）

### 言語機能

- [ ] ビット演算子の提供形: `|`/`&`/`^`/shift がなく、Go のフラグ合成（`os.O_APPEND|os.O_CREATE|os.O_WRONLY`）を `+` で代用するしかない。`<<`/`>>` は関数合成に使用済みで名称の余地が狭い（[problem](problems/2026-11-01-operator-coverage.md)、sandbox 探索 jot で再観測 — [sketch](sketch/2026-11-23-sandbox-missing-features.md)）
- [ ] nil の綴り: `Ptr.nil` しか nil を書く面がなく、`Go.Slice`/`Go.Map`/Go 関数型の nil を作れない（`h.Sum nil` は `[]` で代用できた — 同 sketch）
- [ ] 部分適用・`Unit` 残りの関数値が値位置に流れたときの lint: `fmt.Println` 等に残りパラメータを持つ関数値を渡すとエラーにならず `0x...` が印字される（同 sketch、probes/method の `string i` 事例）。警告 lint を切るか

### Go 相互運用

- [x] Go ABI の方針: 生成される Go の API（型名・関数のシグネチャ・バリアントの struct 名）を、Gom のバージョンをまたいでどこまで安定させるか — 案 B 採択（決定的 emit の保証 + emit 名の衝突検出、名前の安定保証はしない — [decision](decisions/2026-11-23-go-abi-stability.md)）。衝突検出器は wave35 lane C で実装済み（emit 台帳 + W0601/W0602 + `gom dump -phase=emit` — [decision](decisions/2026-11-23-emit-name-collision-check.md)）
- [x] Gom の型が Go の interface を実装できるようにするか（v1 ではできない）。できるなら、どう宣言するか — wave25 lane D で実現可能性を調査済み、wave29 lane D で post-defined-type 世界の設計を decision まで固め（`@method` 属性 + 規約命名、.gomi は fn リスト、`InterfaceMethods` oracle — [decision](decisions/2026-11-21-go-iface-impl.md)）、2026-11-22 に採択。実装は wave30 lane C（[prompts](sketch/2026-11-22-wave30-prompts.md)）
- [ ] `Go.Any` の検査面（型アサーション相当）: `Go.conv` では具象型へ取り出せない（E0507 — spec が明示的に拒否する設計）が、代替の面がないため `json.Unmarshal` で `any` に decode した値を触れない。panic 式 / `Option` 返り / `match` 拡張のどれで供給するか（sandbox 探索 — [sketch](sketch/2026-11-23-sandbox-missing-features.md)、回避は `json.RawMessage`/`Go.Map`/reflect 経由）
- [ ] `Go.Slice` の要素に Gom 宣言型を置けるか（E0507 緩和）: record が `[]MyRec` として `slices.SortFunc` 等の Go API に渡せない（"no Go representation"）。`List T`→`[]T` の境界コピーの意味論と合わせて要 decision（同 sketch、回避は gom 側でのソート実装）

### 最適化

- [x] 生成コード特殊化ポリシー（curried HOF 側）: 呼出側 uncurry（flat fn params）を採択 — 宣言 >=2 本矢印パラメータを flat `func(T1',...,Tk') R'` にし、`Fold` の要素ごと確保を解消（~12×）。wave20 lane 2、[decision](decisions/2026-11-18-flat-fn-params.md)、[検証](sketch/2026-11-18-flat-fn-params-verify.md)。コピーしない共有スライス型（View）の案は [problems](problems/2026-11-04-shared-slice-view-type.md)
- [x] `concat` の O(k) スタック: `@extern fn` で `Concat` を impl.go の buffer loop へ — 総長 prealloc の 1 回コピー、O(1) スタック・O(n)・1 alloc（k=512 で 1.51ms → 21µs、~72×）。wave19 lane 3、[sketch](sketch/2026-11-18-concat-buffer-impl.md)
- [x] 独立 dir 並列コンパイルの実測ベンチ: wave21 lane3 で Kahn 層 + GOMAXPROCS ワーカー化済み（[decision](decisions/2026-11-18-parallel-dir-compile.md)）だが wall-clock の効果は未計測。複数 dir・dep 連鎖 fixture での計測は wave22 候補（prompts 草案 案4） → wave22 lane1 で実測: serial 比約 2.8 倍（単一プロセス化が支配的）、層内並列でさらに -22〜-32%（[sketch](sketch/2026-11-18-wave22-parallel-bench.md)）

### ツール

- [x] `--strict` / 診断フィルタ → 採用（[decisions/strict-mode](decisions/2026-11-22-strict-mode.md)）: リラックスは `//gom:allow CODE` ディレクティブで relaxable コードのみ（初期 E0301/E0302）、厳格方向は warnings→errors の exit code として別旗。実装は wave36 lane E（[prompts](sketch/2026-11-23-wave36-prompts.md)）
- [ ] `gom fix` でも `.gomi` 適合検査を走らせるか（`gom check`/`build`/`test` は走らせると決定済み — [decision](decisions/2026-11-04-gomi-conformance-in-gom-test.md)。`info`/`dump` は対象外）。2026-11-05 に検討して見送り: 適合診断は fix 候補プールに加わるだけ・ゲート無し・診断非表示という形が妥当だが、現状 E0601/E0603/E0604 に safe fix がなく観測差ゼロのため先送り。interface 診断に safe fix が付くなど価値が具体化した時点で再検討する（[検討の記録](decisions/2026-11-05-gomi-conformance-in-gom-fix.md)）
- [x] ~~`gom run`（`go run` 相当の実行経路）~~ → wave19 で採用・実装済み: compile → temp dir への `go build -o` → 子プロセス exec（引数 passthrough・exit code 保全・signal kill は 128+signal）。実行中の出力は対象プログラムが stdout/stderr を所有し、gom の JSON message stream は失敗時のみ（[decision](decisions/2026-11-18-gom-run.md)）
## Future work（v1 では扱わないと決めたもの）

- [x] ~~ラベル付き引数の制限の緩和~~ → 調査済み、unlabeled→labeled の一方向 introduction を採択・実装済み（2026-11-23、[decision](decisions/2026-11-23-labeled-arg-relaxation.md)、wave36 lane B）。meta へのラベル制約の後送りと位置引数の labeled 充填は reject
- `fn ~x -> x` のラムダラベル構文 → wave36 lane F で採択（[prompts](sketch/2026-11-23-wave36-prompts.md)）。`fn` 宣言の `(~x: T)` と対称の形
- 省略可能引数とデフォルト値 → defaults record + `{base with x = v}` パターンで代替する方針に収束（2026-11-19、[sketch](sketch/2026-11-19-labeled-args.md)）。record→ラベル引数の spread は wave29 lane C で実装済み（`f ~..opts`、[decision](decisions/2026-11-21-record-spread-arg.md)、spec 3 章）
- `|?>` の自動ラップの文言を設定可能にする — 案 C（現状維持 + 回避経路の文書化）を採択（2026-11-23、[decision](decisions/2026-11-23-pipe-try-wording.md)）。文言を変えたい消費側は `*rt.StepError.Step` の読み出し・`rt.StepErr` + `Result.map_err`・`try`+`match` で足りる（spec 9.2 節）。書式指定やステップごとの構文（案 A/B）は需要が出た時点で再検討
- ローカル let 多相 — v1 は単相のまま維持（案 A 現状維持、2026-11-23 採択 — [decision](decisions/2026-11-23-local-let-polymorphism.md)）。障壁は relaxed value restriction ではなく Go 側の emission（ローカル generics が存在しない）
- `List` のハイブリッド表現（chunked / unrolled list、所有権フラグ付きの反転ストレージなど）。先頭追加を償却 O(1) に戻す案。P0-3 でスライス表現（`type List[A] []A`）を採用したが、ユーザーレベルの `x :: acc` 型の逐次構築が支配的なワークロードで再検討する余地がある（[sketch: P0-3](sketch/2026-11-02-p0-3-list-representation.md)）
- チャネルで送れる値を型で制限する仕組み（Rust の `Send` 相当）
- `Char` ↔ `Int32` の暗黙の橋渡しを許すか: Go の `rune` 引数 API には `Int32` を渡す必要があり `Char` 値は `Go.conv` を通す。両者の Go 表現は同じ `rune` なので機械的に相互変換可能だが、暗黙変換や専用組み込みを足すかは未決 — v1 は `Go.conv` 明示のまま、ユースケースが集まってから再検討（[problem](problems/2026-11-06-rune-char-correspondence.md)、wave20 lane4 で台帳化判定）
- `deriving` 相当の自動生成（辞書渡しの辞書値をコンパイラが合成する方向）— `Ord` 限定導入は見送り決定（[decision](decisions/2026-11-19-deriving-declined.md)）。対象が広がり十分便利になるなら再考の余地あり
- scoped cleanup（[decision](decisions/2026-11-21-scoped-cleanup.md)）の拡張: 案 D 現状維持 + 代替経路の文書化を採択（2026-11-23、[decision](decisions/2026-11-23-scope-cleanup-extension.md)）— `GoError` 以外への合流は `run_result` → `Result.map_err`、取得+登録はユーザー helper で足りる（文書化は wave35 lane B で実施済み）。案 A–C は将来の余地として残す。result-leak 検査の案 B（result-taint flag + `.gomi` export）は wave35 lane E で採択（[decision](decisions/2026-11-23-result-taint-gomi-export.md)）— 実装は別 lane
- インクリメンタルビルドのキャッシュ（P4。それまでは毎回変換し、Go のキャッシュに任せる）
- `iota` 流の Go 定数セット宣言 — sum type や `let` 定数で代替になるが、Go API の定数群を gom 側から作る面はない（sandbox 探索 — [sketch](sketch/2026-11-23-sandbox-missing-features.md)）
- cgo、`unsafe`、`reflect` の本格的な扱い
- セルフホスト（コンパイラを Gom で書き直す）
- [x] Core 検証器で Go 呼び出しの関数引数の表現を検査する — wave34 lane D で実装済み: `CallGo.FlatArgs` に宣言型由来のスロット arity を載せ、`core.Validate` が emitted repr（`callbackArity`）と照合して E0901。宣言型（rigid）を読む点が要 — instantiated 型は型変数下の関数型で arity が広がる（`internal/core/validate.go`、`TestCallGoCtorRefCallbackArity`）
- [x] W0502 `lintErrorsAsTarget` の `Ptr (Ptr E)` ケース: `**int` は `*int` が error 非実装かつ非 interface なので panic が証明可能（wave9 error-tail レビュー nit） → wave22 lane2 で実装済み: E が unnamed/interface 表現なら warn、named Go 型・record・abstract・メタは沈黙維持（[decision](decisions/2026-11-19-errors-as-nested-ptr-lint.md)）
- [x] `CtorApp`/`CtorRef` の `TypeArgs` を廃止して `Typ` の `Con.Args` を唯一の正にするか: podhmo 判定 (b) 採用（廃止）→ wave24 lane A で実装済み — `Typ` の `Con.Args` が唯一の正（[decision](decisions/2026-11-19-ctor-typeargs-removed.md)、元 [decision](decisions/2026-11-18-ctor-typeargs-authoritative.md)、棚卸し [sketch](sketch/2026-11-19-ctor-targs-b-inventory.md)）

エフェクトシステム、高度な所有権型、独自 GC は急いで導入しない。型クラスは不採用（明示的辞書渡し — [decision](decisions/2026-11-19-ad-hoc-poly-dict-passing.md)）。
