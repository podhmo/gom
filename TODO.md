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

- [x] **[task INIT-2] `gom init` failure atomicity**（依存: なし）: 失敗時は init 由来の変更をすべて rollback する。validation failure で残る新規 `go.mod` / `go.sum` に加え、validation 後の `main.gom` write failure でも、新規 init が作成した `go.mod` / `go.sum` と既存 module への additive edit / `go.sum` 変更を対象にする。init が作っていない file と non-overwrite 契約は変えない（[wave53 prompts](docs/sketch/2026-11-27-wave53-prompts.md)）
- [ ] **[task RCTX-1] callback result への前向き record propagation**（依存: なし）: generic higher-order callback の本体結果位置にある無名 record literal へ、pipe / generic application の静的に分かる result expectation を伝える。後続使用からの逆推論は入れず、二次 E0401 抑制可否も fixture で確認する（[wave53 prompts](docs/sketch/2026-11-27-wave53-prompts.md)、[gap](docs/implementation-gaps.md)）

### owner improvement ideas audit 由来（[PR #256](https://github.com/podhmo/gom/pull/256)、[audit](docs/sketch/2026-11-26-wave48-lane-g-owner-ideas-audit.md)）

- [ ] **[task] checker の状態を関心ごとの部分構造体に分ける**: `typeTables`・deferred revisit queue・taint・lint state などを分け、`expr.go` を構文種別ごとに分割する。振る舞いは変えない。現行の非コメント field 数は 49（PR 本文の 70 超は過大）だが、`expr.go` 4,549 行と責務集中は有効な観測
- [ ] **[task] 値表示を rt の単一実装へさらに寄せる**: wave47 lane A の runtime renderer 共通化を前提に、eval 独自の表示・整数幅・float32 丸め・structural equality を rt 公開関数へ寄せ、eval 側は評価順・pattern 振分け・部分適用の oracle に絞る
- [ ] **[task] 生成コードの benchmark 基盤**: `bench/` に代表的な gom/手書き Go のペアを置き、`go test -bench` の比率を記録する。回帰警告の閾値や CI 実行は導入時に決める

### ドキュメント

- [ ] **D-3 `README.md` を書く**: 言語の概要・ビルド/実行の quick start・ツール群（`gom build`/`check`/`fmt`/`info`）・ドキュメントへの導線（spec/design/tutorial）を載せる。**プロジェクト終盤に書く** — 機能が増えるたびに更新対象になるのを避けるため、内容が安定してから一括で書く（ユーザー指定）。PR #3 で英語 README の初版は入ったが、十分性の最終判定は終盤に行う
- [ ] **D-4 tutorial/examples の module 配置の最終整理**: tutorial の章別 go.mod は削除済みで、現行は repository root の go.mod + test/run 時の scratch module 合成。examples/tutorial が利用者からどう見えるかを決める終盤に、quick start・`gom init` 要否・外部依存の扱いと合わせて最終確認する。**D-3 と同程度の後の作業**（ユーザー指定。2026-11-26 に stale な前提を修正）

### P0 Go 出力の実現可能性の検証

完成したコンパイラを作る工程ではない。Gom 構文からの自動変換はなくてよく、各案の Go 実装を手書きし、ビルド・ベンチマーク・Go からの使いやすさを比べる。実験コードは `experiments/p0/<topic>/` に置き、結果は [docs/sketch/](docs/sketch/) にレポートとして書いてから decisions を更新する。各タスクは独立で、すべて並列に進められる。

### P1 最小の型付き言語

中間表現の型（P1-1, P1-2）を先に固めれば、以降の各フェーズは手書きの入力（AST や IR のリテラル）でテストしながら並列に実装できる。フェーズ間の依存は一方向（`syntax` → `resolve` → `check` → `pattern` → `lower` → `codegen`）。

### P3 Go 相互運用の本格実装

Go 相互運用を変えたら両方向（Gom → Go、Go → Gom）のテストを足す。`nil`、スライス、named type、interface を忘れない。

## Bugs

（見つけたバグはここに `- [ ]` で足す）
