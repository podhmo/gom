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
- [ ] フィールド名の共通する record を並べたときの record literal の曖昧さ（E0407）を緩和するか: `UnvalidatedOrder` → `ValidatedOrder` のような段階型パターンでは同じフィールド名が自然に並び、literal ごとに `let v : T` 注釈が要る。フィールド型・文脈での絞り込みは考えられるか（run 2 の shop/ — [recheck](sketch/ja/2026-11-24-sandbox-explorer2-recheck.md)、[log](sketch/ja/2026-11-24-sandbox-explorer2-log.md)）。PR #256 の F2 は推論強化より先に `ValidatedOrder { id = 1, ... }` の型名前置 literal を足す案。DMMF OrderTaking の段階型でも最大の摩擦として再観測した（[fit](sketch/ja/2026-11-26-dmmf-order-taking-fit.md)）。文脈推論と明示構文のどちらを正にするかは未決（[audit](sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)）
- [ ] stdlib `str` の最小面を超える文字列操作を広げるか: wave48 lane E で byte length/index/range と `runes` は stdlib package `str` に決着したが、`fold_chars` 相当の中間 List を作らない走査、panic しない slice、Unicode 一般処理は未提供。PR #256 F1 の組み込み `String` module 案自体は採用済み decision と競合するため戻さない（[decision](decisions/2026-11-26-string-byte-surface.md)、[audit](sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)）
- [ ] stdlib の Result/Option 横断 helper をどこまで用意するか: `Result.to_option` は既存だが、`Result.sequence` / `List.traverse_result` / Option を Result へ畳む traverse 系はない。DMMF の `List.map ... |> Result.sequence` は頻出だが、名前・配置・引数順を決める必要がある（[fit](sketch/ja/2026-11-26-dmmf-order-taking-fit.md)）
- [ ] bounded context の package / visibility 設計ガイドを作るか: `.gomi` に書かない定義は package-private だが、F# の型名 module に相当するものや、選択した sibling package だけへ公開する friend visibility はない。Go の `internal/` layout、型ごとの package 分割、`string50_create` 命名をどう組み合わせ、`public types` / `internal types` / `implementation` / `api` を分けるかは未決（[fit](sketch/ja/2026-11-26-dmmf-order-taking-fit.md)）
- [ ] 金額・exact decimal の推奨表現を決めるか: `Float` は binary64 で DMMF の `decimal` と性質が違う。integer minor units を推奨するか、外部 decimal 型を許容するか。外部 decimal では `+` / `*` が使えないため、ドメインの核でどう書くかが未決（[fit](sketch/ja/2026-11-26-dmmf-order-taking-fit.md)）

### 開発体験 / 診断

- [ ] 継続行らしき字下げ行の誤パースを専用診断に畳み込むか: `List.fold` の適用を複数行に書くと E0407/E0402/E0401 が連鎖し、本因（改行で式が切れた）が見えない。NL-2 の文法変更は既 decision どおり採らないが、診断だけを改善する余地はある。字下げだけで括る自動 fix は独立式を別意味に書き換えうるため unsafe（PR #256 U1、Devin Review 指摘、[audit](sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)）
- [ ] `diag.Diagnostic` に `labels` / `details` を足すか: E0301 の `missing_patterns` は JSON の `data` に既にあるが text 出力へ詳細行として描く面がなく、E0401 の「期待型の由来」のような副 span も表せない。DMMF の進化テストでも、追加された variant のどれが不足かが最重要情報として再観測された。format 層に `data` を解釈させず phase 側が作る案だが、汎用の wire shape と spec の変更が要る（[problem](problems/2026-11-02-text-format-detail-block.md)、PR #256 U2/U6、[audit](sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)、[fit](sketch/ja/2026-11-26-dmmf-order-taking-fit.md)）
- [ ] E0201 に typo 候補や import 補完 fix を付けるか: `aera` → `area` のような編集距離候補、module member の typo、未 import の Go package に対する `import go` fix の提供境界が未決（PR #256 U4、[audit](sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)）
- [ ] resolve/type error 後のエラー回復をどこまで広げるか: 未定義名を error type として check に通す、pattern を宣言単位で走らせると 1 回の check で見える情報は増えるが、連鎖診断の抑止と fixture 設計が未決（PR #256 U5、[audit](sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)）
- [ ] module 外での `gom run` scratch module 実行と `gom init` を提供するか: module-local transitive emit は wave48 lane C で解消済み。残りは go.mod のない directory での一時 module 合成と、新規 project の `go.mod`/`main.gom` 雛形生成。tutorial の章別 go.mod 生成ではない（PR #256 U7、[audit](sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)）
- [ ] `gom eval` / REPL を利用者へ公開するか: `internal/eval` は oracle としてあるが、Go interop や並行のカバー範囲が compiled より狭い。eval で返す式と compiled へ落とす式の境界、表示方法、REPL の需要が未決（PR #256 U9、[audit](sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)）
- [ ] 型シグネチャで stdlib を引く `gom search` を提供するか: Hoogle 相当の検索は名前を知らない利用者に有効だが、検索対象・単一化の柔軟さ・curry 引数順の扱いが未決。`filter_map` は `(a -> Option b) -> List a -> List b` なので PR #256 の例は引数順の修正が要る（PR #256 U10、[audit](sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)）

### Go 相互運用

- [ ] JSON tagged union / nullable field の推奨 DTO 表現を決めるか: record の struct tag は使えるが、sum type の direct marshal は implementation shape、`Option` は nullable にならない。`Go.Map String Go.Any`、専用 DTO record、`Ptr` nullable、空文字割り切りのどれを推奨形とするか未決（[fit](sketch/ja/2026-11-26-dmmf-order-taking-fit.md)）
- [ ] abstract 型の不変条件を Go の zero value / boundary までどこまで守るか: `.gomi` の abstract 宣言は通常の literal / constructor / conversion を塞ぐが、`Ptr.new` や `encoding/json` 経由では未検証値を作れる。`Ptr.new` を制限する、lint する、DTO 境界規約に留めるのかが未決（[fit](sketch/ja/2026-11-26-dmmf-order-taking-fit.md)）
- [ ] panic capture の boundary adapter を用意するか: spec は言語機能としての `recover` を提供しないが、外部サービス呼び出しなどで Go 側 helper に panic を捕まえさせ `Result` 化する余地はある。`Go.func0` / `@extern fn` の形、置き場所、そもそも定型化するかが未決（[fit](sketch/ja/2026-11-26-dmmf-order-taking-fit.md)）
- [x] Go ABI の方針: 生成される Go の API（型名・関数のシグネチャ・バリアントの struct 名）を、Gom のバージョンをまたいでどこまで安定させるか — 案 B 採択（決定的 emit の保証 + emit 名の衝突検出、名前の安定保証はしない — [decision](decisions/2026-11-23-go-abi-stability.md)）。衝突検出器は wave35 lane C で実装済み（emit 台帳 + W0601/W0602 + `gom dump -phase=emit` — [decision](decisions/2026-11-23-emit-name-collision-check.md)）
- [x] Gom の型が Go の interface を実装できるようにするか（v1 ではできない）。できるなら、どう宣言するか — wave25 lane D で実現可能性を調査済み、wave29 lane D で post-defined-type 世界の設計を decision まで固め（`@method` 属性 + 規約命名、.gomi は fn リスト、`InterfaceMethods` oracle — [decision](decisions/2026-11-21-go-iface-impl.md)）、2026-11-22 に採択。実装は wave30 lane C（[prompts](sketch/2026-11-22-wave30-prompts.md)）
- [x] `Go.Any` の検査面（型アサーション相当）— **wave38 lane F（調査→decision）として採択**: `Go.conv` では具象型へ取り出せない（E0507 — spec が明示的に拒否する設計）が、代替の面がないため `json.Unmarshal` で `any` に decode した値を触れない。panic 式 / `Option` 返り / `match` 拡張のどれで供給するか（[decision](decisions/2026-11-24-go-any-extraction.md) 採択 — comma-ok の `Option` 返り `Go.try_as` を唯一の面とする。**wave39 lane A で実装済み** — `Go.try_as x : Option T`（E0514））
- [x] `Go.Slice` の要素に Gom 宣言型を置けるか: wave37 lane D — decision [2026-11-23-goslice-gom-elem](decisions/2026-11-23-goslice-gom-elem.md)（`~` 制約の要素一致を Gom 側 `types.Equal` で見る案）。**2026-11-24 採択** — 実装は wave38 lane D

- [x] `Go.Slice` の gom 側コンストラクタ — **解消**: `let gs : Go.Slice T = xs`（`xs : List T`）の ascription で `List` → `Go.Slice` が通る。check の `conformExpr` → `sliceConv` が `ConvSliceToGo` を返し、lower が `.ToSlice()` コピーを emit する（run 2 の recheck は「面がない」として登録したが、レビュー指摘で発見・実測確認。`Go.conv` の明示キャストは gom 要素型で E0507、`Array` は `~[]E` を満たさず E0401 のまま）。コピーのため gom 側の `List` 本体は変わらないが、`slices.Sort`（順序のある要素）や `slices.SortFunc`（record — `Go.func2` 比較器）の結果は `Go.Slice` 側から読める — 用途は足りる（run 2 — [recheck](sketch/ja/2026-11-24-sandbox-explorer2-recheck.md)）

- [x] `%w` error wrapping の第一級サポート: 生成 `fmt.Errorf` が引数を box するため vet が `%w` を弾き、`wrap`/`wrapf` は `@extern` shim 退避。`Result` がある分第一級にしたい（selfish 移植 — [sketch](sketch/ja/2026-11-24-selfish-port.md)、wave43 lane F）— **wave44 lane A で解消**: Go 呼び出しの `any` 引数位置で Go 型の引数は `any(x)` に box せず素通しする emit に変更（`fmt.Errorf("%w", e)` が vet を通る。declared `Go.Any` 位置は従来どおり box）— [decision](decisions/2026-11-24-goany-arg-passthrough.md)、[検証](sketch/2026-11-24-goany-arg-passthrough.md)
- [x] `@method` の `Result` 返り許可（E0420 緩和）: `encoding.TextUnmarshaler` 系など error を返す Go interface を gom 型に実装できない。現状は同パッケージ `shim.go` に Go で書く回避（selfish 移植 — 同上 sketch、実装の制約: [implementation-gaps.md](implementation-gaps.md)）— **wave44 lane C で実装**: `Result T GoError` を許容し `(T, error)` に unwrap emit。非 `GoError` payload は引き続き E0420
- [x] `Result (Option T, U)` の payload Option を呼出し側でも宣言できるか: メソッド直呼びの n-ary result は verbatim 固定で `Ptr.to_option` の後包みが要る（selfish 移植 stage 4 — 同上 sketch）— **決着（wave45 lane G）**: verbatim 維持。nil ガードは後包み（pointee コピー）、同一性が要るときは `Ptr.is_nil` — [decision](decisions/2026-11-25-nary-result-ptr-verbatim.md)
- [x] スカラーリテラルの meta bind 遅延: 共有メタを持つ部分適用で供給リテラルが `?b` を先に builtin へ commit し want 側の宣言型に materialize できない（`pair "x"` の E0401 誤拒否、#215 escalate）。数値リテラルの pendingLit 式遅延をスカラーへ広げるか、部分適用の conform 順序を want-aware にするか — **解消（wave44 lane B）**: pendingLit 式の bind 遅延ではなく、静的に決まる fill の残余パラメータを want に pin する方式を採用 — [decision](decisions/2026-11-24-partial-app-pin-params.md)

### 最適化

- [x] 生成コード特殊化ポリシー（curried HOF 側）: 呼出側 uncurry（flat fn params）を採択 — 宣言 >=2 本矢印パラメータを flat `func(T1',...,Tk') R'` にし、`Fold` の要素ごと確保を解消（~12×）。wave20 lane 2、[decision](decisions/2026-11-18-flat-fn-params.md)、[検証](sketch/2026-11-18-flat-fn-params-verify.md)。コピーしない共有スライス型（View）の案は [problems](problems/2026-11-04-shared-slice-view-type.md)
- [x] `concat` の O(k) スタック: `@extern fn` で `Concat` を impl.go の buffer loop へ — 総長 prealloc の 1 回コピー、O(1) スタック・O(n)・1 alloc（k=512 で 1.51ms → 21µs、~72×）。wave19 lane 3、[sketch](sketch/2026-11-18-concat-buffer-impl.md)
- [x] 独立 dir 並列コンパイルの実測ベンチ: wave21 lane3 で Kahn 層 + GOMAXPROCS ワーカー化済み（[decision](decisions/2026-11-18-parallel-dir-compile.md)）だが wall-clock の効果は未計測。複数 dir・dep 連鎖 fixture での計測は wave22 候補（prompts 草案 案4） → wave22 lane1 で実測: serial 比約 2.8 倍（単一プロセス化が支配的）、層内並列でさらに -22〜-32%（[sketch](sketch/2026-11-18-wave22-parallel-bench.md)）
- [ ] goimport の stdlib export data decode を process 内で共有できるか: module pool ごとの再 decode は大きな build で hotspot になりうるが、`importer.ForCompiler` の lookup は `*types.Package` ではなく export data reader を返す。decode 済み型 identity を保つ共有構成を prototype で確かめるまで実装不可（PR #256 P1、Devin Review 指摘、[audit](sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)）

### ツール

- [x] `--strict` / 診断フィルタ → 採用・実装済み（[decisions/strict-mode](decisions/2026-11-22-strict-mode.md)）: リラックスは `//gom:allow CODE` ディレクティブで relaxable コードのみ（初期 E0301/E0302）、厳格方向は warnings→errors の exit code（`-strict` / `-strict=CODES`、wave32 PR #129）
- [ ] `gom fix` でも `.gomi` 適合検査を走らせるか（`gom check`/`build`/`test` は走らせると決定済み — [decision](decisions/2026-11-04-gomi-conformance-in-gom-test.md)。`info`/`dump` は対象外）。2026-11-05 に検討して見送り: 適合診断は fix 候補プールに加わるだけ・ゲート無し・診断非表示という形が妥当だが、現状 E0601/E0603/E0604 に safe fix がなく観測差ゼロのため先送り。interface 診断に safe fix が付くなど価値が具体化した時点で再検討する（[検討の記録](decisions/2026-11-05-gomi-conformance-in-gom-fix.md)）。2026-11-24 wave43 lane E で再確認 — 状況不変、保留を維持
- [x] ~~`gom run`（`go run` 相当の実行経路）~~ → wave19 で採用・実装済み: compile → temp dir への `go build -o` → 子プロセス exec（引数 passthrough・exit code 保全・signal kill は 128+signal）。実行中の出力は対象プログラムが stdout/stderr を所有し、gom の JSON message stream は失敗時のみ（[decision](decisions/2026-11-18-gom-run.md)）

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
- [x] インクリメンタルビルドのキャッシュ — wave18 lane 2 で実装済み（[decision](decisions/2026-11-18-incremental-build-cache.md)）。依存 fingerprint の compile 時観測は wave48 lane B で修正済み
- `iota` 流の Go 定数セット宣言 — sum type や `let` 定数で代替になるが、Go API の定数群を gom 側から作る面はない（sandbox 探索 — [sketch](sketch/2026-11-23-sandbox-missing-features.md)）。wave47 では見送り（2026-11-25、[prompts](sketch/2026-11-25-wave47-prompts.md)）
- cgo、`unsafe`、`reflect` の本格的な扱い
- `gom install` — 書き換え後の selfish リポジトリのような gom 製バイナリをリポジトリ指定でインストールする経路（`go install` 相当）。module-local な依存の推移的 emit は wave48 lane C で解消済み。残りは外部 module / install 先 / cache との関係の設計（podhmo 要望、2026-11-24、wave43 lane F）
- セルフホスト（コンパイラを Gom で書き直す）
- [x] Core 検証器で Go 呼び出しの関数引数の表現を検査する — wave34 lane D で実装済み: `CallGo.FlatArgs` に宣言型由来のスロット arity を載せ、`core.Validate` が emitted repr（`callbackArity`）と照合して E0901。宣言型（rigid）を読む点が要 — instantiated 型は型変数下の関数型で arity が広がる（`internal/core/validate.go`、`TestCallGoCtorRefCallbackArity`）
- [x] W0502 `lintErrorsAsTarget` の `Ptr (Ptr E)` ケース: `**int` は `*int` が error 非実装かつ非 interface なので panic が証明可能（wave9 error-tail レビュー nit） → wave22 lane2 で実装済み: E が unnamed/interface 表現なら warn、named Go 型・record・abstract・メタは沈黙維持（[decision](decisions/2026-11-19-errors-as-nested-ptr-lint.md)）
- [x] `CtorApp`/`CtorRef` の `TypeArgs` を廃止して `Typ` の `Con.Args` を唯一の正にするか: podhmo 判定 (b) 採用（廃止）→ wave24 lane A で実装済み — `Typ` の `Con.Args` が唯一の正（[decision](decisions/2026-11-19-ctor-typeargs-removed.md)、元 [decision](decisions/2026-11-18-ctor-typeargs-authoritative.md)、棚卸し [sketch](sketch/2026-11-19-ctor-targs-b-inventory.md)）
- [x] `gom list` の列挙対象を外部 Gom パッケージまで広げる — wave41 lane C で `gom list -external` として実装: main module + `go list -m all` の build list 内 `.gomi` 所在を import パスで列挙（vendor 列挙は別項目のまま future work）


エフェクトシステム、高度な所有権型、独自 GC は急いで導入しない。型クラスは不採用（明示的辞書渡し — [decision](decisions/2026-11-19-ad-hoc-poly-dict-passing.md)）。
