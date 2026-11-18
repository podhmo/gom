# dir グループの並列コンパイルと Loader の共有境界

- 状態: 採用
- 日付: 2026-11-18
- コンテキスト: design §5 項目 6 の残件（implementation-gaps, wave20 lane3 評価で「小さくない」と記録）。wave21 prompts で (a)「dep グラフから topological にワーカー配置 + Loader 全体の同期」が確定済み — 本 decision はその実装形の細部を記録する。

## 決定

- `compileInputs` は dir グループを dep グラフの Kahn 層に分け、層内だけ並列化する。辺は各 `.gom` の `import` 宣言から引く（short name は canonical パスに展開）。import サイクルの残件は引数順の直列にフォールバック。
- `goimport.Loader` は go.mod dir（モジュール文脈）単位で共有し、全 public メソッドを内部 mutex で直列化する。
- `gomimport.Loader` は dir ごとのまま。
- `buildCache` は mutex 直列化。Go 側 dep 集合はモジュール単位の depSeen にプールする。

## 却下した案

- **Loader を全て dir ごとに生成（共有なし）**: 並行安全は最も簡単だが、`go list -export` と export data のパースを dir ごとに重複して払い、共有の利点を捨てる — prompts の確定方針に反する。
- **`gomimport.Loader` も共有**: `cfg.Dir` が `EntryPath()`（`SelfImportPath`）と impl-scan の seed を担い dir ごとの entry identity なので、共有すると self-import の解決と E0606 の検出を誤る。
- **go/importer の fset だけ包む狭いロック**: mutation 面は `pkgs`/`syms`/`msyms`/`named`/`nextVar` に及ぶため、メソッド入口の mutex が単純で漏れがない。
- **Go dep 集合を dir ごとに帰属**: 共有 Loader の `OnLoad` はどの dir が起点か分からない。モジュール単位でプールすると保守的に共有 dir 全てを無効化する（偽陰性はない）。

## 影響

- `gom build`/`check` の独立 dir が並列化され、依存関係にある dir も dep 順序が保証される。
