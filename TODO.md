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

- [-] **P2-4 `try` と `|?>`**: 脱出境界は最も内側の関数（[decisions/try-boundary](docs/decisions/2026-11-01-try-boundary.md)）、`|?>` の `%w` 自動ラップ（[spec 9 章](docs/spec.md)）。中間の `Result` を作らない生成（design.md 4.6）。依存: P2-1, P1-13
  - [ ] **P2-4c unguarded ステップの `EscapeCall` 融合（残る最適化）**: unguarded ステップの引数が Go `(T,error)` 呼び出しの場合、`ResultFromGo` で物質化してから次セグメントの `EscapeResult` が検査するため中間 Result が 1 個残る。引数評価（try を含むのでユーザー側の列）と呼び出し本体（セグメント内 `EscapeCall`）を分離すれば融合可能。併せて `pipeRest` の span 不統一（束縛は `lv.Y.Span()`・enter escape は `lv.Sp`）も直す（P2-4b レビュー F5/F6）。依存: P2-4b


### P3 Go 相互運用の本格実装

Go 相互運用を変えたら両方向（Gom → Go、Go → Gom）のテストを足す。`nil`、スライス、named type、interface を忘れない。

- [x] **P3-7 P3 の完了確認**: `os`、`io`、`strconv`、`encoding/json` などを使うプログラムが動く。依存: P3-2, P3-3, P3-4, P3-5, P3-6
- [ ] **[task] extern の「標準のバインディング」集の整備**: P3-5 で `extern` 機構は実装済みだが、それを使う標準バインディング集（`(T, bool)` → `Option` などの既定上書きの一覧）は未着手。管理場所は [open-questions](docs/open-questions.md) の未決事項。P3-7 の完了確認で `os`/`io`/`strconv`/`encoding/json` のプログラムが extern を必要とするかの判断材料になる。依存: P3-5
- [ ] **[task] `Ptr` の操作面の設計**: P3-7 監査で `Ptr T` は read-through（メソッド・フィールド参照）はできるが deref・nil 比較・構築ができないと判明（`errors.As` はコンパイルして実行時 panic、`flag` 値は `extern` → `Option (Ptr T)` 経由でしか安全に読めない）。`Go.deref`/`Ptr.is_nil` 等の操作面か、open-questions の `*T` 既定と合わせた設計が要る。[problems](docs/problems/2026-11-05-ptr-t-no-deref.md)
- [ ] **[task] `Go.Map` の操作面（member table またはモジュール）**: `Go.Map.make` が E0201 — `builtinGoMembers` は conv/Slice/Map の型名だけで操作がなく、`Go.Map` を持つ Go API は実質読めない。make/get/set/delete 相当の surface が要る。P3-7 監査で判明
- [ ] **[task] `[]T` を in-place 変更する Go 関数への警告**: `sort.Strings` がコンパイルして**無音で no-op** になる（境界でコピーされる List/Slice の帰結）。lint 警告（W07xx 系）か `Go.Slice` pun の spec 明文化で対処する。[problems](docs/problems/2026-11-05-go-param-mutation-noop.md)

### P4 公開 API と開発体験

- [ ] **P4-6 LSP**: 型表示、網羅性エラーの即時表示、UTF-16 位置への変換。依存: P4-5
- [ ] **P4-7 インクリメンタルビルドのキャッシュ**: キャッシュキーは [design.md 5 章](docs/design.md)。依存: P4-1

### P5 並行処理と最適化

- [x] **P5-2 可変な値の送信・捕捉に対するリンター警告**。依存: P5-1
- [ ] **P5-3 生成コードの最適化と性能検証**: `List.map` などの特殊化、ベンチマーク、エスケープ解析、`go test -race`。依存: P4-8

### 検証

- [ ] **gomtest spec ハーネスの複数ファイル対応**: testdata/spec の fixture は 1 case = 1 ファイルのため、パッケージ単位の規則（兄弟ファイルをまたぐ初期化サイクル、兄弟 test 名タグ付けなど）を spec fixture として書けない。現状は `internal/build` ドライバテストで代替済み（`TestCompileFilesInitCycleAcrossSiblings` 等）。複数ファイル case の fixture 形式を決めて対応する

## Bugs

（見つけたバグはここに `- [ ]` で足す）

- [x] **[bug] 兄弟ファイルをまたぐ初期化サイクルが Gom 診断をすり抜ける**: `a.gom: let x = y + 1` / `b.gom: let y = x + 1` は `gom build` を通り、`var x = y + 1` / `var y = x + 1` を生成して `go build` が "initialization cycle" で落ちる。`checkInitCycles` は file-local の Core IR 上でしか辺を張らない — unit スコープの初期化解析が要る（V-6 で複数 `.gom` パッケージが実現して到達可能になった）。[台帳](docs/implementation-gaps.md)、[実測](docs/sketch/2026-11-05-v6-residual-verification.md)

- [ ] **[divergence] Float リテラル演算が生成 Go の定数畳み込みで評価器と値がずれる**: `"${869.32 * 38.29}"` は eval がリテラルごとに float64 化してから乗算するため `33286.262800000004` を出すが、生成 Go は untyped 定数式のまま出力するため Go の任意精度定数畳み込み＋単一丸めで `33286.2628` になる。同じ `'g' -1` 表示を通るのに値自体がずれる。spec の Float 演算の意味を決める必要がある: IEEE754 ベースに寄せるなら codegen がオペランドを非定数化（変数経由か変換の挿入）する必要があり、Go の定数意味論に寄せるなら eval の各演算を定数畳み込み順に直す必要がある（V-3 の eval-vs-compiled 差分テストで発見、knownDivergences 登録済み。[docs/problems/2026-11-02-float-const-fold-arithmetic.md](docs/problems/2026-11-02-float-const-fold-arithmetic.md)）
- [ ] **[divergence] 異なるバリアント値への名前付きペイロードアクセス `.f` で eval とコンパイルがずれる**: `fn getr (s: Shape) -> Float = s.r`（`Shape = | Circle (r: Float) | Rect (w: Float) (h: Float) | Empty`）に `Rect 2.0 3.0` を渡すと、eval は index 0 のスロット `w` の値 `2.0` を返すのに対し、コンパイル側は `s.(Shape_Circle).R` の型アサーションで panic する（`Empty` では eval も E9004 "variant has no such field" で失敗するが、コンパイル panic と失敗の形が違う）。check は「そのフィールド名を持つバリアントがちょうど 1 つ」のときだけ `.f` を受理するので、所有者が静的に証明できないケースの意味を spec が定めていないのが根。アサーション意味（違約 = panic、match の DPanic 系と同系）を spec に明文化して eval 側も ctor 一致を検査するのが自然案。生成コード側は `core.FieldGet.Ctor` でアサート対象を IR 確定済み（[docs/problems/2026-11-02-variant-field-wrong-variant.md](docs/problems/2026-11-02-variant-field-wrong-variant.md)。fix-diff-bugs の `s.r` 回帰修正中に検証で発見）
- [x] **[quality] E0415 の診断品質フォローアップ（fix-local-result レビューの nice-to-have）**: ③ は P5-2 で修復（select recv/send arm に shadowed-builtin 分岐、fixture `E0415_select_recv_local_chan`/`E0415_select_send_local_chan`）。① `resultArgs` 二重報告 ② `|?>` 経路の誤構文名（construct 名の引数化）④ `for` メッセージの連結改善 ⑤ quiet-meta 伝搬で E0407/E0401 カスケード削減＋`shadowedNamesInType` が全 shadowed 名を報告、は Wave 5 hardening (g) で修復。⑤ の残件（unify 失敗経路の非 mismatch E0401 抑制）は過剰抑制リスクで意図的に見送り — ただし `metasIn(want)` 化で隣接の共有 want 抑制リスクが台帳に新規登録されている

- [-] **[task] Gist CLIのGo境界**: byte I/Oと`Go.Slice String`→`List String`を実装して残りのexample-local adapterを縮小する。最小再現と必要操作は[Gist prototype](docs/sketch/2026-11-03-gist-cli-prototype.md)、関連: P3-2/P3-3。

- [x] **[divergence] Float 型の巨大整数リテラルが eval で CodeInternal になる**: `let m: Float = 99999999999999999999`（int64 超）は eval の `litValue` が `strconv.ParseInt` を Float 判定より先に呼ぶため "invalid int literal" で失敗するが、生成 Go は `float64(...)` を出してコンパイル・実行できる（Wave 5 hardening レビュー指摘、exotic）。`isFloatCon` 時は `ParseFloat` を先に試す。[lit.go](internal/eval/lit.go)
- [x] **[bug] `for` の shadowed-con E0415 リストに `SendChan` がない**: `checkFor` の shadowed builtin 一覧は `List`/`Chan`/`RecvChan` を挙げるが `SendChan` を含まないため、ファイルローカルの `type SendChan` 下の `for` は E0415 ではなく素の mismatch になる（pre-existing、Wave 5 hardening レビュー指摘）。`internal/check/expr.go` の checkFor 付近
