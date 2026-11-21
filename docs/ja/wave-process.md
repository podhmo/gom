# Wave 開発プロセス

Gom の開発は「wave」という単位で回す。1 wave = 複数 lane（細切れの
作業項目）の束。各 lane は 1 ブランチ = 1 PR として進め、wave 末に
締め作業をしてから次の wave の prompts を作る。

この文書は wave の回し方そのものを記録する。過去は prompts ファイル
間の申し送りで運用ルールを引き継いでいたが、wave24 頃に複数の項目が
脱落したため（skill 同期・prompts-drafting rules など）、規範側に
独立して置く。

## 前提となる実行モデル

### 本来の形（並列）

- レーンを依存関係を考慮して切り分け、各 lane をサブエージェントが
  並行開発する。
- メインエージェントはオーケストレーターとして振る舞い、各 lane の
  PR をレビューし、マージ作業とレーン間の調整を担う。
- マージ後の作業: `skills/gom-agent-guide` の同期、各 lane の作業を
  まとめた際の不整合調整、pitfalls の記録（round 記録）。
- サブエージェントは仕様を更新しても索引側を忘れることがある
  （README の表、エラーコード一覧など）。こういう簡易な差分は
  メインがレビュー・マージのタイミングで拾い、調整を乗せて
  squash merge する。

### 現在の形（単一エージェント）

コスト（クォータ）の関係で、全 lane を 1 つのセッションが直列に
実行する。仮想的には上のモデルの延長線上にある — つまり
「オーケストレーターの仕事」（レビュー対応・締め作業・台帳管理）も
同じエージェントが lane 間の合間に担う。

- 並列数が 1 なので lane 間の依存関係をあまり気にしなくてよく
  なった（副次的な効果）。それでも lane は base をチェーンした
  独立 PR として切り分ける。
- レビューは Devin Review が PR ごとに担う。CI は push-to-main /
  PR-to-main synchronize でしか走らないため、チェーン途中の PR は
  Devin Review のみが品質ゲートになる。
- マージは podhmo が行う（エージェントはマージしない）。base の
  retarget → `gh pr merge --merge` を下から順に。

## フェーズ

### 1. 情報収集・実験

- 調査・検証は `docs/sketch/` にレポートを残す（規範ではない記録）。
  実験 → sketch → 仕様（spec/design/decision）にまとめる → 実施、
  が基本の流れ。
- 仕様・実装・文書のずれを見つけたら `docs/implementation-gaps.md`
  に追記する（AGENTS.md のルールどおり、大小・原因確定を問わない）。
- レビューや作業中に見つかった別件のタスクは `TODO.md` に積み、
  未決事項は `docs/open-questions.md` に書く。

### 2. Prompts 作成（次 wave の立案）

- 次 wave の lane 候補を `docs/sketch/YYYY-MM-DD-waveN-prompts.md`
  として草案化する。
- **意思決定はこの時点で人間（podhmo）と会話して済ませる。**
  採択が要る点は prompts 作成時点で聞いて確定し、lane 本文に
  「計測結果なら (a)、そうでなければ (b)」型の実行時採択を
  残さない（wave21 の Prompt-drafting rules）。
- 実装中の細かい設計決定は decision (draft) → prototype 検証 →
  状態更新、の通常手順で記録する。これは分岐ではなく記録手順。
- 採択されなかった候補の sketch/experiments は記録として残す
  （decision から却下理由つきで参照）。

### 3. 実施（lane 実行）

- wave 実施時点は完全自動運転。lane 本文は確定済みの形だけを
  書いてあるので、実行中に人間へ意思確認しない。
- 1 lane = 1 ブランチ = 1 PR。前の PR が未マージなら前 lane の
  ブランチからチェーンして分岐する（A→B→C…）。
- Devin Review の finding は lane 内でトリアージして潰す
  （resolved = 修正 commit、rejected/escalated は理由つき）。

### 4. 締め作業（全 lane 完了後 — lane ではない独立作業）

- `docs/sketch/YYYY-MM-DD-waveN-round.md` の round 記録
  （各 lane の結果・pitfalls・次への申し送り）。
- `skills/gom-agent-guide` の同期（その wave で変わった
  ユーザー可視の振る舞い・診断・構文の分）。
- 台帳の掃除: open-questions / implementation-gaps / TODO.md の
  解消済みチェック漏れ sweep、problems/ 残件の台帳化判定。
- 索引系の更新忘れを拾う: README の表、`docs/error-codes.md`、
  decisions README（`go generate ./internal/tool/decisionsdoc`）
  など、サブタスクが更新し忘れやすい一覧物。
- 次 wave の prompts 草案を作り、フェーズ 2 に戻る。

## 運用上の細目

- チェーン PR 群の base 変更（retarget）と merge は podhmo 側の
  操作。エージェント側は PR とブランチを整えるだけ。
- prompts ファイル自体も PR（最終 lane のブランチの先）に乗せて
  記録する。
- wave 中に立てた decision は冒頭に `- 状態:`・`- 日付:` を
  必須とする（`TestDecisionsReadmeInSync` が検査する）。
