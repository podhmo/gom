# wave50 prompts（草案）

- 日付: 2026-11-27
- 状態: 草案（採否インタビュー未実施）
- 入力: [wave49 round](2026-11-27-wave49-round.md)、[TODO](../../TODO.md)、[open-questions](../open-questions.md)、[implementation-gaps](../implementation-gaps.md)、[problems](../problems/)

wave49 は短絡 correctness、stdlib Result traversal、型名前置 record literal、E0301 text、DMMF boundary guide、gaps archive まで進んだ。wave50 は wave49 で残った最大級の DX 摩擦（record literal の文脈推論）を主軸にしつつ、診断・ツール面の残件を小さい順に潰す構成を推薦する。

## 推薦 lane

### A. 診断コード表のメタテスト（小〜中）

TODO の「診断コード表を軸にしたメタテスト」。spec harness / CLI test / `gom explain` example / `docs/error-codes.md` の対応を分類して検査し、診断の退行を wave50 の後続 lane から守る。retired code は対象外。まず現状分類を出し、不足 fixture を足す範囲は lane 内で最小化する。

### B. module 外 `gom run` と `gom init`（中）

go.mod のない directory でも scratch module を合成して `gom run` でき、新規 project では `gom init` が `go.mod` / `main.gom` の雛形を作る状態にする。tutorial の章別 go.mod 生成ではなく、利用者の quick start を対象にする。module-local transitive emit は wave48 で済んでいる。cache、scratch module の寿命、`gom init` が既存ファイルを上書きしない規則を lane 内で decision 化する。

### C. 汎用 diagnostic `labels` / `details` wire（中）

wave49 lane D は E0301 限定で `missing_patterns` を text へ出した。残問の汎用 wire を設計し、JSON wire、text renderer、spec §12、少なくとも 1 つの E0301 以外の利用箇所を揃える。対象候補は E0401 の期待型由来や関連 span。全診断へ一律適用せず、wire shape と代表利用に限定する。

### D. match 到達不能 arm / `todo ()` 依存の改善（中〜大）

implementation-gaps に残る「catch-all の内側で sub-match すると網羅性の恩恵が薄れる」問題を調査し、到達不能 arm・guard・nested match のどこを checker/pattern 側で精度化できるか決める。仕様変更が要る場合は decision 先行。少なくとも selfish/DMMF 形の回帰 fixture を作り、`todo ()` を消せる範囲と残る範囲を明確にする。

### E. record literal の文脈推論（大）

wave49 lane B は型名前置 literal で決定的な回避面を作ったが、`Ok { ... }` や無名 record accumulator、expected type / field type からの一般推論は残っている。wave49 で入れた speculative lookahead の境界（外部 meta を早く bind しない、shared open equality を保つ、quiet/silent で診断を潰さない）を前提に、一般化の scope を決めて実装する。`List.fold` の無名 record accumulator が解消するかを必須 probe とし、型名前置 literal との優先順位も spec へ書く。

推薦順: A → B → C → D → E。A は後続の診断変更を守り、B/C/D は独立して進め、最大の E を最後に置く。E が大きいため checker 分割や benchmark 基盤は推薦 wave に入れない。

## 採否を確認したい点

1. 推薦 5 lane（A〜E）を採用するか。落とす lane があれば指定する
2. B は `gom run` scratch module と `gom init` を両方入れるか、どちらかに絞るか
3. C の汎用 diagnostic wire は wave50でやるか。E0401 の副 span まで代表利用に含めるか
4. D は実装 laneにするか、まず調査/decision lane に絞るか
5. E を wave50 の主 laneにするか。大きい場合は D か C を落とすか
6. E を採用しない場合、代わりに checker 分割 / rt 表示一元化 / benchmark 基盤のどれを入れるか

## 見送り候補（少なくとも現時点では lane にしない）

- checker の状態分割 — 有効だが E と併用すると compiler 中心部の衝突が大きい。E を採らない場合の第一代替
- 値表示の rt 一元化、生成コード benchmark 基盤 — 有効な内部改善だが、今回はユーザー可視 DX を優先
- stdlib `str` 拡張、Option traverse — 需要が弱く、DMMF 主軸の阻塞ではない
- stack-carrying error 値の生成 — `@extern` shim で回避可能。具体的な頻出ユースケースをもう少し集める
- panic capture adapter — wave49 と同じく見送り。boundary guide の運用で十分か観察する
- 継続行誤パース診断、E0201 typo 候補、エラー回復拡大 — 価値はあるが C の wire 設計と重なるため、C の結果を見て分割する
- `gom eval` / REPL、`gom search`、goimport export data 共有、`gom fix` の `.gomi` 検査 — 設計または需要確認が大きい
- `gom install`、Go 定数セット、cgo/unsafe/reflect、セルフホスト — future work 維持
