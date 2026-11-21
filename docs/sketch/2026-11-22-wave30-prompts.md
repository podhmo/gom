# Wave 30 orchestration prompt drafts

State as of 2026-11-22: wave29 の 5 lane（#105–#109、defined
type / pun / spread / iface impl 設計 / type alias）は全て PR 化
済み、Devin Review 反復・podhmo マージ待ち。wave29 締め作業
（round 記録・skill 同期・台帳掃除）は本ファイルと同じ
`devin/wave29-close` ブランチで実施。

このファイルは wave30 の prompts 草案。テーマは scoped
cleanup の仕上げ + wave29 の残件処理 — SC-1（rt-backed
`Scope` モジュール）は TODO.md 上で済、SC-2/SC-3 が残っている
のが最大の未消化塊。lane 順は下からチェーンする前提。

## Lanes

### A. `Scope` 値の脱出の静的検査（SC-2） — `devin/wave30-scope-escape`

```
[impl] TODO.md SC-2。SC-1 で入った `Scope` 値がスコープ外に
漏れる経路を型エラーにする: `Scope` を返す、record・
コンストラクタ・可変セル（`Ref`/`Array`/Go.Map）に格納する、
`Task.spawn` や Go の関数値へ渡す。ラムダ引数が逃げないかは
パッケージ内で推論し、`.gomi` には注釈として出す（import 側
でも同じ判定が効くように）。Go の関数は常に「逃げる」扱い。
escapes 判定の粒度（フィールド全体 vs 値レベル）は実装時
decision 記録。spec の Scope 節・error-codes に診断を登録。
Ownership: internal/check, internal/iface, docs/spec.md,
docs/error-codes.md, testdata/spec, decision.
依存: なし（SC-1 済）
```

### B. ネイティブ `defer` への lowering（SC-3） — `devin/wave30-scope-defer`

```
[impl] TODO.md SC-3（依存: SC-2 = lane A）。cleanup 登録が
`Scope.run` と同じ Go 関数内にあるとき、Core IR で判定して
登録位置に Go の `defer` を出す。結果に触る cleanup だけ
生成 Go 側で named result を使う（目標形は
`experiments/defer/cgen`）。それ以外は SC-1 のランタイム
スタックのまま維持 — 判定に落ちた経路がランタイムに戻る
両棲構造の検証が本 lane の核心。golden で両形を確認。
Ownership: internal/core, internal/lower, internal/codegen,
testdata/golden, decision.
依存: lane A（脱出検査が無いと安全に lowering できない）
```

### C. Go interface impl の実装 — `devin/wave30-go-iface-impl-impl`

```
[impl] wave29 lane D の decision draft
（docs/decisions/2026-11-21-go-iface-impl.md）を採用した
場合の実装 lane: `@method` 属性 + 規約命名による record の
メソッド宣言、生成 Go への method emit、`.gomi` は fn リスト
形式、Go 側の interface パラメータに record を渡すときの
ラップ（`InterfaceMethods` oracle）。defined type 世界
（wave29 lane A）での設計済み。
**前提: podhmo が lane D の設計を採用した場合のみ着手。**
不採用・設計差し戻しならこの lane は下記候補へ差替え。
Ownership: internal/check, internal/codegen, internal/iface,
stdlib, docs/spec.md, testdata/golden, decision（状態確定）.
依存: なし（lane A/B と独立）
```

### D. `--strict` / 診断フィルタの調査 — `devin/wave30-strict-mode`

```
[investigate] open-questions の「書きかけコードで一部エラーを
一時フィルタしたい用途」。`--strict` 相当のオプションを
`gom check`/`build` に入れるか — 用途の具体例（途中の match
穴・冗長腕を抑えて先に進みたい）、診断をフィルタする粒度
（コード単位・ファイル単位・severity 単位）、出力に残す
記録（silent にしない仕組み）、lint レイヤとの関係を調査して
採否を decision まで。実装はしない。
Ownership: docs/decisions, docs/sketch, cmd/gom, docs/open-questions.
依存: なし
```

### E. single-field record の残ギャップ整理 — `devin/wave30-defined-type-gaps`

```
[investigate→impl可] lane A 着地後の残りを洗う:
- `{x,}` 1 フィールド pun（lane B 済み）と defined type の
  併用で `UserId{v,}` 型の読み書きに抜けがないか
- 匿名 record `{to = 1}` が spread で E0401 になる制約は
  spec に明記済みか（lane C のレビュー起因制約 —
  skill には追記済み）
- defined type 化で変わる boundary（`Go.conv`、`.gomi`、
  gocall 引数）の記述が spec/design と齟齬ないか
小さい差分なら本 lane で直し、大きいものは台帳へ。
Ownership: docs/spec.md, docs/design.md, internal/check,
testdata/spec.
依存: なし（wave29 のマージ結果に依存せず調査可能）
```

## 採択されなかった候補（次回以降の参考）

- **D-3 README 整備** — podhmo 指定で「プロジェクト終盤」まで
  先送り（TODO 維持）
- **Go ABI 安定化ポリシー**（open-questions）— v1 前に一度は
  決めるべきだが、record/alias の表現変更が落ち着くまで計測
  対象が動くため wave30 では見送り
- **`.gomi` 多相実装の許容**（open-questions）— iface impl が
  採用された場合に一緒に考えるのが効率的

## 申し送り

- wave の回し方: `docs/ja/wave-process.md` が規範文書
  （prompts 間の継承では脱落した実績あり）。
- CI は push-to-main / PR-to-main でしか走らない — チェーン
  PR は Devin Review のみ。マージは podhmo が retarget →
  `gh pr merge --merge` を下から順に。
- prompts-drafting ルール: 採択が要る点は prompts 作成時点で
  podhmo に聞いて確定。lane 本文に実行時採択を残さない。
- wave の締め作業: round 記録、`skills/gom-agent-guide` 同期、
  台帳（open-questions / implementation-gaps / TODO.md）掃除、
  索引類（README・error-codes・spec の一覧）の更新忘れ拾い、
  次 wave prompts 草案。
- grammar を触る lane: tree-sitter-cli は 0.25.10 固定
  （0.27/0.26 は GLIBC 不足）。`npm exec --yes --package=
  tree-sitter-cli@0.25.10 -- tree-sitter generate` で再生成。
  corpus テストは `package m` で始めること（無いと `type` が
  ident 化して ERROR — 文法バグではない）。
- spec fixture 再生: `go test ./internal/build -update -run
  TestSpec`。golden: 同 -run TestGolden。gomtest の case は
  bare `expected.json`/`golden.go`/`case.json` のいずれか必須。
- scratch 環境: `/tmp/ta2` 方式 — go.mod に
  `replace github.com/podhmo/gom => /home/ubuntu/repos/gom`、
  パッケージ dir に .gom+.gomi、ローカルビルドの
  `/tmp/gom-local` で `gom check ./app`（`go run ...@latest`
  は stale な remote を引くので禁止）。
- decision 書式: 冒頭に `- 状態:`・`- 日付:` bullet 必須
  （TestDecisionsReadmeInSync）。覆す decision は消さず
  Supersedes。
- wave29 ラウンド記録: `2026-11-22-wave29-round.md` —
  alias lazy 解決（aliasBod + applyTypeCtor）、qualified 名の
  tyBySym は sigOf の後に引く（E0415 shadowedSigDiag を
  bypass しない）、iface drafter の `type alias` 綴り、
  spread は named record のみ、を申し送り。
- コミットメッセージにバッククォート/`$()` を入れない。
