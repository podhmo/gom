# 未決事項と future work

決着したら [decisions/](decisions/) に 1 件 1 ファイルで記録し、[spec.md](spec.md) / [design.md](design.md) を更新してから、この一覧から消す。決まっていて作業するだけのものは [../TODO.md](../TODO.md) に移す。

## 未決事項

### 型システム

- [x] 実装の型がインターフェースより一般的な場合（実装が多相、`.gomi` が単相）を許すか — 許さない（厳格一致維持、案 B 採択・2026-11-23）。狭めたい場合は明示的な単相ラッパを書く（[decision](decisions/2026-11-23-gomi-monomorphic.md)）

- [x] 可変性: `Ref a` と可変配列 `Array a` 以外に可変構造を許すか — 可変構造の入口は builtin 集合のまま、宣言型はフィールド推移律で導出（`containsMutable` は tyCons 展開済み）。残る隙間は実装がパッケージ境界の向こうに隠れた型（extern 裏付け・abstract export 不透明化の両方）で `.gomi` の `//gom:mutable` directive を採択（wave36 lane G、[decision](decisions/2026-11-23-user-mutable-structures.md)）。**2026-11-24 実装済み** — wave39 lane D（spec §10）。`Ref` のメンバー面は wave25 で実装済み（[decision](decisions/2026-11-19-ref-members.md)）

### 言語機能

- [x] ビット演算子の提供形: `|`/`&`/`^`/shift がなく、Go のフラグ合成（`os.O_APPEND|os.O_CREATE|os.O_WRONLY`）を `+` で代用するしかない。`<<`/`>>` は関数合成に使用済みで名称の余地が狭い（[problem](problems/2026-11-01-operator-coverage.md)、sandbox 探索 jot で再観測 — [sketch](sketch/2026-11-23-sandbox-missing-features.md)）。wave37 lane B で decision 化 — 三重字 `&&&`/`|||`/`^^^`/`<<<`/`>>>`（+`&&^`）を推薦（[decision](decisions/2026-11-23-bit-operators.md)、[prompts](sketch/2026-11-23-wave37-prompts.md)）。**2026-11-24 採択・実装済み** — wave38 lane B で `&&&`/`&&^`/`|||`/`^^^`/`<<<`/`>>>` として実装
- [x] nil の綴り: wave37 lane C — decision [2026-11-23-nil-spelling](decisions/2026-11-23-nil-spelling.md)（`Go.nil` ユニバーサル builtin）。**2026-11-24 採択** — 実装は wave38 lane C
- [x] 部分適用・`Unit` 残りの関数値が値位置に流れたときの lint: `fmt.Println` 等に残りパラメータを持つ関数値を渡すとエラーにならず `0x...` が印字される（同 sketch、probes/method の `string i` 事例）。wave39 lane C で調査 → [decision](decisions/2026-11-24-partial-application-lint.md) 採択: `ignore`・`_` 束縛・match `_` 腕の 3 面で `*types.Func` の捨てを警告（`//gom:allow` の warning 拡張が前提）。実装は wave40 lane A。**残問**: `any` 引数経由で Go へ渡る関数値は対象外（decision で明示的に open のまま。run 2 の recheck/partapp でも `fmt.Println f` が無警告で `0x...` 表示になることを再確認 — [recheck](sketch/ja/2026-11-24-sandbox-explorer2-recheck.md)）
- [ ] 文字列の操作面（len・部分列・添字）をどう提供するか: `strings.Join`/`Split` は Go の `strings` 経由で足りるが、`len(s)`・`s[i]`・`s[lo:hi]` 相当がなく、文字集合の走査は `strings.Split (s, "")` + `List.get` の迂回になる（run 2 の passgen）。組み込み `String` のメンバー面か stdlib パッケージか（sandbox 探索 run 2 — [recheck](sketch/ja/2026-11-24-sandbox-explorer2-recheck.md)）
- [ ] フィールド名の共通する record を並べたときの record literal の曖昧さ（E0407）を緩和するか: `UnvalidatedOrder` → `ValidatedOrder` のような段階型パターンでは同じフィールド名が自然に並び、literal ごとに `let v : T` 注釈が要る。フィールド型・文脈での絞り込みは考えられるか（run 2 の shop/ — [recheck](sketch/ja/2026-11-24-sandbox-explorer2-recheck.md)、[log](sketch/ja/2026-11-24-sandbox-explorer2-log.md)）

### Go 相互運用

- [x] Go ABI の方針: 生成される Go の API（型名・関数のシグネチャ・バリアントの struct 名）を、Gom のバージョンをまたいでどこまで安定させるか — 案 B 採択（決定的 emit の保証 + emit 名の衝突検出、名前の安定保証はしない — [decision](decisions/2026-11-23-go-abi-stability.md)）。衝突検出器は wave35 lane C で実装済み（emit 台帳 + W0601/W0602 + `gom dump -phase=emit` — [decision](decisions/2026-11-23-emit-name-collision-check.md)）
- [x] Gom の型が Go の interface を実装できるようにするか（v1 ではできない）。できるなら、どう宣言するか — wave25 lane D で実現可能性を調査済み、wave29 lane D で post-defined-type 世界の設計を decision まで固め（`@method` 属性 + 規約命名、.gomi は fn リスト、`InterfaceMethods` oracle — [decision](decisions/2026-11-21-go-iface-impl.md)）、2026-11-22 に採択。実装は wave30 lane C（[prompts](sketch/2026-11-22-wave30-prompts.md)）
- [x] `Go.Any` の検査面（型アサーション相当）— **wave38 lane F（調査→decision）として採択**: `Go.conv` では具象型へ取り出せない（E0507 — spec が明示的に拒否する設計）が、代替の面がないため `json.Unmarshal` で `any` に decode した値を触れない。panic 式 / `Option` 返り / `match` 拡張のどれで供給するか（[decision](decisions/2026-11-24-go-any-extraction.md) 採択 — comma-ok の `Option` 返り `Go.try_as` を唯一の面とする。**wave39 lane A で実装済み** — `Go.try_as x : Option T`（E0514））
- [x] `Go.Slice` の要素に Gom 宣言型を置けるか: wave37 lane D — decision [2026-11-23-goslice-gom-elem](decisions/2026-11-23-goslice-gom-elem.md)（`~` 制約の要素一致を Gom 側 `types.Equal` で見る案）。**2026-11-24 採択** — 実装は wave38 lane D

- [x] `Go.Slice` の gom 側コンストラクタ — **解消**: `let gs : Go.Slice T = xs`（`xs : List T`）の ascription で `List` → `Go.Slice` が通る。check の `conformExpr` → `sliceConv` が `ConvSliceToGo` を返し、lower が `.ToSlice()` コピーを emit する（run 2 の recheck は「面がない」として登録したが、レビュー指摘で発見・実測確認。`Go.conv` の明示キャストは gom 要素型で E0507、`Array` は `~[]E` を満たさず E0401 のまま）。コピーのため gom 側の `List` 本体は変わらないが、`slices.Sort`（順序のある要素）や `slices.SortFunc`（record — `Go.func2` 比較器）の結果は `Go.Slice` 側から読める — 用途は足りる（run 2 — [recheck](sketch/ja/2026-11-24-sandbox-explorer2-recheck.md)）

- [ ] `%w` error wrapping の第一級サポート: 生成 `fmt.Errorf` が引数を box するため vet が `%w` を弾き、`wrap`/`wrapf` は `@extern` shim 退避。`Result` がある分第一級にしたい（selfish 移植 — [sketch](sketch/ja/2026-11-24-selfish-port.md)、wave43 lane F）
- [ ] `@method` の `Result` 返り許可（E0420 緩和）: `encoding.TextUnmarshaler` 系など error を返す Go interface を gom 型に実装できない。現状は同パッケージ `shim.go` に Go で書く回避（selfish 移植 — 同上 sketch、実装の制約: [implementation-gaps.md](implementation-gaps.md)）
- [ ] `Result (Option T, U)` の payload Option を呼出し側でも宣言できるか: メソッド直呼びの n-ary result は verbatim 固定で `Ptr.to_option` の後包みが要る（selfish 移植 stage 4 — 同上 sketch）

### 最適化

- [x] 生成コード特殊化ポリシー（curried HOF 側）: 呼出側 uncurry（flat fn params）を採択 — 宣言 >=2 本矢印パラメータを flat `func(T1',...,Tk') R'` にし、`Fold` の要素ごと確保を解消（~12×）。wave20 lane 2、[decision](decisions/2026-11-18-flat-fn-params.md)、[検証](sketch/2026-11-18-flat-fn-params-verify.md)。コピーしない共有スライス型（View）の案は [problems](problems/2026-11-04-shared-slice-view-type.md)
- [x] `concat` の O(k) スタック: `@extern fn` で `Concat` を impl.go の buffer loop へ — 総長 prealloc の 1 回コピー、O(1) スタック・O(n)・1 alloc（k=512 で 1.51ms → 21µs、~72×）。wave19 lane 3、[sketch](sketch/2026-11-18-concat-buffer-impl.md)
- [x] 独立 dir 並列コンパイルの実測ベンチ: wave21 lane3 で Kahn 層 + GOMAXPROCS ワーカー化済み（[decision](decisions/2026-11-18-parallel-dir-compile.md)）だが wall-clock の効果は未計測。複数 dir・dep 連鎖 fixture での計測は wave22 候補（prompts 草案 案4） → wave22 lane1 で実測: serial 比約 2.8 倍（単一プロセス化が支配的）、層内並列でさらに -22〜-32%（[sketch](sketch/2026-11-18-wave22-parallel-bench.md)）

### ツール

- [x] `--strict` / 診断フィルタ → 採用・実装済み（[decisions/strict-mode](decisions/2026-11-22-strict-mode.md)）: リラックスは `//gom:allow CODE` ディレクティブで relaxable コードのみ（初期 E0301/E0302）、厳格方向は warnings→errors の exit code（`-strict` / `-strict=CODES`、wave32 PR #129）
- [ ] `gom fix` でも `.gomi` 適合検査を走らせるか（`gom check`/`build`/`test` は走らせると決定済み — [decision](decisions/2026-11-04-gomi-conformance-in-gom-test.md)。`info`/`dump` は対象外）。2026-11-05 に検討して見送り: 適合診断は fix 候補プールに加わるだけ・ゲート無し・診断非表示という形が妥当だが、現状 E0601/E0603/E0604 に safe fix がなく観測差ゼロのため先送り。interface 診断に safe fix が付くなど価値が具体化した時点で再検討する（[検討の記録](decisions/2026-11-05-gomi-conformance-in-gom-fix.md)）。2026-11-24 wave43 lane E で再確認 — 状況不変、保留を維持
- [x] ~~`gom run`（`go run` 相当の実行経路）~~ → wave19 で採用・実装済み: compile → temp dir への `go build -o` → 子プロセス exec（引数 passthrough・exit code 保全・signal kill は 128+signal）。実行中の出力は対象プログラムが stdout/stderr を所有し、gom の JSON message stream は失敗時のみ（[decision](decisions/2026-11-18-gom-run.md)）
- [ ] `gom info` が stdlib 短名を解決しない — `gom list` で出す名前（`list`/`option`/`result`/`cmp`）をそのまま `gom info <name>` で引ける想定だったが、現状はファイル/ディレクトリパスしか取らず（import 名は解決しない） `stat list: no such file or directory` になる。stdlib のシグネチャ一覧を dump する口がなく、利用者は repo の `stdlib/*/*.gomi` を読むか名前を推測するしかない（sandbox 探索 run 2 — [gotchas](sketch/ja/2026-11-24-sandbox-explorer2-gotchas.md) §1）
- [ ] `.gomi` を手で管理せず `gom info` の生成物にしたい場合の機能不足 — (1) 再生成が spec 上「`gom info` が書き出し」の `//gom:noescape`/`//gom:result-notaint` を落とす（[implementation-gaps.md](implementation-gaps.md) 登録済み）。(2) abstract 化・非公開化の意図を生成に残す経路がなく、draft への手編集が再生成で消える — `.gom` 属性か `.gomi` ディレクティブで表し `gom info` が引き継ぐ仕組みが要る（`//gom:mutable` の「著者記述・再生成引き継ぎ」モデルを広げる案がありうる）（sandbox 探索 run 2 — [gotchas](sketch/ja/2026-11-24-sandbox-explorer2-gotchas.md) §3）
## Future work（v1 では扱わないと決めたもの）

- [x] ~~ラベル付き引数の制限の緩和~~ → 調査済み、unlabeled→labeled の一方向 introduction を採択・実装済み（2026-11-23、[decision](decisions/2026-11-23-labeled-arg-relaxation.md)、wave36 lane B）。meta へのラベル制約の後送りと位置引数の labeled 充填は reject
- [x] ~~`fn ~x -> x` のラムダラベル構文~~ → 実装済み（2026-11-23、wave36 lane F）。`fn ~x -> e` / `fn (~x: T) -> e` — `fn` 宣言の `(~x: T)` と対称の形
- 省略可能引数とデフォルト値 → defaults record + `{base with x = v}` パターンで代替する方針に収束（2026-11-19、[sketch](sketch/2026-11-19-labeled-args.md)）。record→ラベル引数の spread は wave29 lane C で実装済み（`f ~..opts`、[decision](decisions/2026-11-21-record-spread-arg.md)、spec 3 章）
- `|?>` の自動ラップの文言を設定可能にする — 案 C（現状維持 + 回避経路の文書化）を採択（2026-11-23、[decision](decisions/2026-11-23-pipe-try-wording.md)）。文言を変えたい消費側は `*rt.StepError.Step` の読み出し・`rt.StepErr` + `Result.map_err`・`try`+`match` で足りる（spec 9.2 節）。書式指定やステップごとの構文（案 A/B）は需要が出た時点で再検討

- ローカル let 多相 — v1 は単相のまま維持（案 A 現状維持、2026-11-23 採択 — [decision](decisions/2026-11-23-local-let-polymorphism.md)）。障壁は relaxed value restriction ではなく Go 側の emission（ローカル generics が存在しない）
- `List` のハイブリッド表現（chunked / unrolled list、所有権フラグ付きの反転ストレージなど）。先頭追加を償却 O(1) に戻す案。P0-3 でスライス表現（`type List[A] []A`）を採用したが、ユーザーレベルの `x :: acc` 型の逐次構築が支配的なワークロードで再検討する余地がある（[sketch: P0-3](sketch/2026-11-02-p0-3-list-representation.md)）
- vendoring 関連（wave42 lane D/E で解消）: (1) ~~Go 1.22+ の workspace vendoring（`<workroot>/vendor`、`go work vendor`）~~ — wave42 lane E で loader が `<workroot>/vendor` を module vendor より優先して見るように（`vendorDir()`、workspace では `-mod=readonly` リトライ）(2) ~~`gom list` 外部列挙への module 側 `vendor/` walk の追加~~ — wave42 lane D で実装: vendor mode 有効時に `<modRoot>/vendor` 配下の `.gomi` 所在を vendored パスで列挙（dirOf の vendor 優先解決と同じ規則）
- チャネルで送れる値を型で制限する仕組み（Rust の `Send` 相当）
- `Char` ↔ `Int32` の暗黙の橋渡しを許すか: Go の `rune` 引数 API には `Int32` を渡す必要があり `Char` 値は `Go.conv` を通す。両者の Go 表現は同じ `rune` なので機械的に相互変換可能だが、暗黙変換や専用組み込みを足すかは未決 — v1 は `Go.conv` 明示のまま、ユースケースが集まってから再検討（[problem](problems/2026-11-06-rune-char-correspondence.md)、wave20 lane4 で台帳化判定）
- `deriving` 相当の自動生成（辞書渡しの辞書値をコンパイラが合成する方向）— `Ord` 限定導入は見送り決定（[decision](decisions/2026-11-19-deriving-declined.md)）。対象が広がり十分便利になるなら再考の余地あり
- scoped cleanup（[decision](decisions/2026-11-21-scoped-cleanup.md)）の拡張: 案 D 現状維持 + 代替経路の文書化を採択（2026-11-23、[decision](decisions/2026-11-23-scope-cleanup-extension.md)）— `GoError` 以外への合流は `run_result` → `Result.map_err`、取得+登録はユーザー helper で足りる（文書化は wave35 lane B で実施済み）。案 A–C は将来の余地として残す。result-leak 検査の案 B（result-taint flag + `.gomi` export）は wave35 lane E で採択・wave36 lane C/D で実装済み（[decision](decisions/2026-11-23-result-taint-gomi-export.md)）
- インクリメンタルビルドのキャッシュ（P4。それまでは毎回変換し、Go のキャッシュに任せる）
- `iota` 流の Go 定数セット宣言 — sum type や `let` 定数で代替になるが、Go API の定数群を gom 側から作る面はない（sandbox 探索 — [sketch](sketch/2026-11-23-sandbox-missing-features.md)）
- cgo、`unsafe`、`reflect` の本格的な扱い
- `gom install` — 書き換え後の selfish リポジトリのような gom 製バイナリをリポジトリ指定でインストールする経路（`go install` 相当）。依存パッケージの推移的 emit（TODO の `gom run` 不ビルド問題）と合わせて検討（podhmo 要望、2026-11-24、wave43 lane F）
- 符号なし整数（`byte`/`uint8` 以外 — spec §8.3 で v1 非対応と明記済み）: run 2 で `text/tabwriter` の `flags uint` が E0501 で取れず手書きパディングに退避した事例あり（[recheck](sketch/ja/2026-11-24-sandbox-explorer2-recheck.md) tabwriter）。`uint16`/`uint32`/`uint64`/`uintptr` が要るかは需要を見て再検討
- セルフホスト（コンパイラを Gom で書き直す）
- [x] Core 検証器で Go 呼び出しの関数引数の表現を検査する — wave34 lane D で実装済み: `CallGo.FlatArgs` に宣言型由来のスロット arity を載せ、`core.Validate` が emitted repr（`callbackArity`）と照合して E0901。宣言型（rigid）を読む点が要 — instantiated 型は型変数下の関数型で arity が広がる（`internal/core/validate.go`、`TestCallGoCtorRefCallbackArity`）
- [x] W0502 `lintErrorsAsTarget` の `Ptr (Ptr E)` ケース: `**int` は `*int` が error 非実装かつ非 interface なので panic が証明可能（wave9 error-tail レビュー nit） → wave22 lane2 で実装済み: E が unnamed/interface 表現なら warn、named Go 型・record・abstract・メタは沈黙維持（[decision](decisions/2026-11-19-errors-as-nested-ptr-lint.md)）
- [x] `CtorApp`/`CtorRef` の `TypeArgs` を廃止して `Typ` の `Con.Args` を唯一の正にするか: podhmo 判定 (b) 採用（廃止）→ wave24 lane A で実装済み — `Typ` の `Con.Args` が唯一の正（[decision](decisions/2026-11-19-ctor-typeargs-removed.md)、元 [decision](decisions/2026-11-18-ctor-typeargs-authoritative.md)、棚卸し [sketch](sketch/2026-11-19-ctor-targs-b-inventory.md)）
- [x] `gom list` の列挙対象を外部 Gom パッケージまで広げる — wave41 lane C で `gom list -external` として実装: main module + `go list -m all` の build list 内 `.gomi` 所在を import パスで列挙（vendor 列挙は別項目のまま future work）

エフェクトシステム、高度な所有権型、独自 GC は急いで導入しない。型クラスは不採用（明示的辞書渡し — [decision](decisions/2026-11-19-ad-hoc-poly-dict-passing.md)）。
