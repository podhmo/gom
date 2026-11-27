# Wave 開発プロセス

Gom の開発は「wave」という単位で回す。1 wave = 複数 lane（細切れの
作業項目）の束。各 lane は 1 ブランチ = 1 PR として進め、wave 末に
締め作業をしてから次の wave の prompts を作る。

この文書は wave の回し方そのものを記録する。過去は prompts ファイル
間の申し送りで運用ルールを引き継いでいたが、wave24 頃に複数の項目が
脱落したため（skill 同期・prompts-drafting rules など）、規範側に
独立して置く。**メンテ対象の文書**（sketch のような書き切りではない）
— 運用が変わったらこの文書を更新する。

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
- オーケストレーターの役割の一部は podhmo が担う: マージは
  podhmo が行う（エージェントはマージしない）。base の
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
- 入力は前 wave の round、TODO、open-questions、implementation-gaps。
  特に round の発生タスクは、lane 候補・採否確認点・明示的な見送りの
  いずれかに載せ、prompts 作成時に取りこぼさない。小さな発見は
  必要に応じて参照する。
- **意思決定はこの時点で人間（podhmo）と会話して済ませる。**
  採択が要る点は prompts 作成時点で聞いて確定し、lane 本文に
  「計測結果なら (a)、そうでなければ (b)」型の実行時採択を
  残さない（wave21 の Prompt-drafting rules）。
- **インタビューで採択が確定したら台帳も同期する**: 採択された
  decision が既にあるときは `状態` を更新する。open-questions の
  該当項目は `[x]` + decision または採否確定済み prompts リンクに、
  採択済みの実作業を `TODO.md` に登録する（決まっていて手を動かす
  だけの lane だけ。調査 lane は open-questions 側の管理のまま）。
  採択時点の scope は prompts に記録し、実装中の技術決定は通常どおり
  lane の中で decision 化する。prompts の `状態` 更新だけでは各台帳が
  stale のまま残る。
- 実装中の細かい設計決定は decision (draft) → prototype 検証 →
  状態更新、の通常手順で記録する。これは分岐ではなく記録手順。
- 採択されなかった候補の sketch/experiments は記録として残す
  （decision から却下理由つきで参照）。

### 3. 実施（lane 実行）

- wave 実施時点は完全自動運転。lane 本文は確定済みの形だけを
  書いてあるので、実行中に人間へ意思確認しない。
- 1 lane = 1 ブランチ = 1 PR。前の PR が未マージなら前 lane の
  ブランチからチェーンして分岐する（A→B→C…）。
- PR 作成後の CI / Devin Review 待ちだけでは作業を止めない。
  単一セッションでも仮想的な並列 lane を一人で順に追うので、
  review findings や CI failure の通知にはその場で対応しつつ、
  待ち時間は次 lane の実装か締め作業を進める。停止して人間へ
  返すのは採否・大きな設計判断など人間の決定が必要なときだけにし、
  merge / base retarget は最後の調整点として podhmo に残す。
- Devin Review の finding は lane 内でトリアージして潰す
  （resolved = 修正 commit、rejected/escalated は理由つき）。
- 既存のメタテストや診断 fixture が足場としてある領域を変える lane
  では、実装へ入る前に期待値・fixture を追加・更新し、red を確認
  してから実装で green にする。red を再現できない場合は、その理由を
  lane の検証レポートに書く
- lane 内で scope を削る・後続へ送る場合は、lane では実装と検証に
  集中し、残件を lane の検証レポート（`docs/sketch/...-verify.md`）と
  round 入力へ具体的に記録する。TODO / open-questions への登録は
  締め作業で行う。検証レポート側に残件の中身がなければ closeout で
  拾えないため、「対象外」の一言だけで済ませない。ただし
  仕様・実装・文書のずれは AGENTS.md どおり発見時に
  `docs/implementation-gaps.md` へ即時登録する。

### 4. 締め作業（lane ではない独立作業）

規範としては **lane PR 群とは別の closeout PR** でやる作業。
lane PR は実装・decision/spec・lane 検証レポートまでに絞り、
podhmo は lane PR 群を先に merge してよい。wave の完了は lane PR
ではなく closeout PR の merge とする。単一エージェント運用では
2 段に分かれる:

- **closeout PR 作成（エージェント）**: 全 lane の PR が揃った
  時点で、stack 最上位または lane merge 後の follow-up として
  closeout PR を作る。round 草案、残件台帳登録、索引・guide 同期、
  不足調整をここに入れる。lane merge 前でも内容はレビュー可能に
  してよいが、round の merge 状態と main CI 結果は未確定のまま
  明記する。
- **仕上げ（podhmo + エージェント）**: podhmo は closeout PR の
  1 つ手前までの lane PR を merge する。merge 後に round の merge
  状態と main CI を最終確認・更新し、closeout PR を merge して
  wave を閉じる。lane merge 後に既存実装の review/CI 修正だけが
  見つかった場合は、closeout に混ぜず小さい follow-up PR として
  分離し、その merge と main CI を確認してから closeout を閉じる。
  実装の意味変更や新規 scope が必要になった場合は次 lane / 次 wave
  扱いにする。

締め作業の内容:

- `docs/sketch/YYYY-MM-DD-waveN-round.md` の round 記録。round は
  個別 lane を読まない評価者が wave を把握する summary。中心は
  lane 結果と、実行・review で分かったことを蒸留した pitfalls。
  詳細本文や網羅監査の再録ではない。

  wave53 以降の実例で安定している構成を使う: 先頭の日付・
  prompts・実行形態・merge/CI 状態、`lane 結果`、`wave の経過`、
  `発見メモ`、`pitfall 一般化`、`lane PR の残件監査`、
  `発生タスクと台帳の動き`、`締め sweep`、`次への申し送り`。
  `wave の経過`と`発見メモ`は記録すべき内容がなければ省略してよいが、
  書く場合は具体的な発見メモと一般化した pitfall を分ける。残件監査と
  締め sweep は省略せず、なければ「なし」と書く。`pitfalls`
  を review finding の列挙や lane 本文の再録だけで済ませない。

  必須:

  - lane 表: lane/PR/結果/レビュー経過。レビュー経過には修正ラウンドの
    回数と主な論点を wave の概観に必要な範囲で書く。正確な全件記録は不要
  - pitfalls: 発見や事後理解から次回以降に効く形へ蒸留したもの。
    round の主な蓄積
  - lane PR の残件監査: lane ごとの残件と登録先。なければ「なし」と書く
  - 発生タスクと台帳の動き: 新規/解消した TODO、open question、
    implementation gap、decision、problem のうち次 wave prompts の
    入力になるもの。なければ「なし」と書く
  - 締め sweep: decisions/error-codes/guide/TODO archive/索引/main CI の
    確認結果。なければ「なし」と書く
  - 次への申し送り

  必要に応じて:

  - 発見メモ: 一般化しきれない小さな発見、事後的に気づいて対応した
    こと、計画や当時の認識とずれたこと。網羅ではなく思い出すための
    数行の index
  - wave の経過: review/merge/closeout をまたいで方針や実装が変わった
    部分の概観。commit 群のおおまかな把握を助ける程度でよい

  ただし finding→fix の対応表、commit 全件の把握、ドキュメント差分の
  精密な追跡は round の目的ではない。詳細は PR の review thread、
  decision、sketch、台帳を正とし、round からリンクする。
- `skills/gom-agent-guide` の同期（その wave で変わった
  ユーザー可視の振る舞い・診断・構文の分）。
- **全体を眺めての修整・調整**: 個別のレビュー・マージ時点では
  人間が注意力を持てないため、締め作業の中で wave 全体の差分を
  読み、索引系の更新忘れ（README の表、`docs/error-codes.md`、
  decisions README — `go generate ./internal/tool/decisionsdoc`）
  やレーン間の不整合を拾って直す。
- **scope 外・後送り残件の台帳登録**: 各 lane の検証レポート、review、
  round 入力から残件を集め、対応意思のある実作業は wave 番号を持たない
  独立な `[task]` として `TODO.md` に、未決定の設計判断は
  `docs/open-questions.md` に登録する。round や検証レポートに
  「将来 wave」「今回は対象外」と書くだけでは管理したことにしない。
  仕様・実装・文書のずれは発見時登録が正なので、closeout では
  `docs/implementation-gaps.md` への登録漏れがないかを確認するに留める。
  `implementation-gaps` はずれの台帳であり、実作業の管理先ではない。
  この登録を終えるまで wave を閉じない。
- 台帳の掃除: open-questions / implementation-gaps / TODO.md の
  解消済みチェック漏れ sweep、problems/ 残件の台帳化判定。
  TODO.md の `[x]` エントリは todo-md skill の GC 手順どおり
  `TODO-archive.md` へ移し、live な TODO.md には `[ ]` / `[-]`
  だけを残す。wave ごとの見出しは進行中の作業 queue であり、
  完了済み lane の進捗表として残さない。GC は feature PR とは分け、
  独立した consolidation PR で行う。
- 次 wave の prompts 草案を作り、採否インタビューと採択後の台帳同期まで
  済ませてから closeout PR の merge 準備ができたものとする。
  prompts 確定後はフェーズ 2 の採択済み状態から次 wave へ進む。

## 運用上の細目

- チェーン PR 群の base 変更（retarget）と merge は podhmo 側の
  操作。エージェント側は PR とブランチを整えるだけ。
- prompts ファイル自体も PR（最終 lane のブランチの先）に乗せて
  記録する。
- wave 中に立てた decision は冒頭に `- 状態:`・`- 日付:` を
  必須とする（`TestDecisionsReadmeInSync` が検査する）。
