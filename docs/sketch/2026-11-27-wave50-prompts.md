# wave50 prompts

- 日付: 2026-11-27
- 状態: 採択（podhmo、2026-11-27）。順 A→C→D→E。B は次回へ延期
- 入力: [wave49 round](2026-11-27-wave49-round.md)、[TODO](../../TODO.md)、[open-questions](../open-questions.md)、[implementation-gaps](../implementation-gaps.md)、[problems](../problems/)

wave49 は短絡 correctness、stdlib Result traversal、型名前置 record literal、E0301 text、DMMF boundary guide、gaps archive まで進んだ。wave50 は診断の検証基盤を先に整え、diagnostic wire、match 精度の設計、record literal の文脈推論へ進む。module 外 `gom run` / `gom init` は価値はあるが wave50 では実施しない。

## lane

### A. 診断コード表のメタテスト（小〜中）

TODO の「診断コード表を軸にしたメタテスト」。目的は診断出力を固定することではなく、live code に対応する検証面の抜け漏れを防ぐこと。spec harness / CLI test / `gom explain` example / `docs/error-codes.md` の対応を分類して検査し、C/D/E の診断・推論変更で説明や fixture が無言で腐らないようにする。retired code は対象外。`docs/error-codes.md` と表の一致は既存 `TestErrorCodesDocInSync` に任せ、この lane では重複実装しない。まず現状分類を出し、不足 fixture を足す範囲は lane 内で最小化する。

### C. 汎用 diagnostic `labels` / `details` wire（中）

wave49 lane D は E0301 限定で `missing_patterns` を text へ出した。残問の汎用 wire を設計し、JSON wire、text renderer、spec §12、代表利用として E0401 の期待型由来を示す副 span まで揃える。全診断へ一律適用せず、wire shape と代表利用に限定する。typo 候補、error recovery 拡大、継続行誤パース診断はこの lane に入れない。

### D. match 到達不能 arm / `todo ()` 依存の改善（調査 + decision、中）

implementation-gaps に残る「catch-all の内側で sub-match すると網羅性の恩恵が薄れる」問題を調査し、到達不能 arm・guard・nested match のどこを checker/pattern 側で精度化できるか決める。wave50 では調査、decision、selfish/DMMF 形の回帰 fixture までに絞り、大きな checker/pattern 実装は wave51 以降へ送る。`todo ()` を消せる範囲と残る範囲を明確にする。

### E. record literal の文脈推論（大）

wave49 lane B は型名前置 literal で決定的な回避面を作ったが、`Ok { ... }` や無名 record accumulator、expected type / field type からの一般推論は残っている。wave49 で入れた speculative lookahead の境界（外部 meta を早く bind しない、shared open equality を保つ、quiet/silent で診断を潰さない）を前提に、次の scope で実装する。

- 型名前置 literal を常に最優先とし、spec に優先順位を書く
- 静的に分かる expected type だけを使う。対象は type annotation、return、呼出し引数、constructor payload、record field
- 後続使用からの逆推論は入れない
- `List.fold` の無名 record accumulator が解消するかを必須 probe とする

推薦順: A → C → D → E。A は後続の診断変更を守り、C/D は比較的独立して進め、最大の E を最後に置く。E が大きいため checker 分割や benchmark 基盤はこの wave に入れない。

## 採否インタビュー結果

1. A/C/D/E を採用し、B は podhmo の判断で wave50 から外し次回へ延期した
2. A は出力固定ではなく、live code の fixture / CLI test / `gom explain` example の欠落を防ぐ保険として採用
3. C は E0401 の期待型由来を示す副 span を代表利用に含める。typo 候補と error recovery は対象外
4. D は実装 lane ではなく、調査 + decision + 回帰 fixture に絞る
5. E は型名前置 literal 優先、静的に分かる expected type のみ使用、後続使用からの逆推論なし、`List.fold` probe 必須で採用

## 採否を確認した点（記録）

1. 推薦 5 lane（A〜E）を採用するか。落とす lane があれば指定する
2. B は `gom run` scratch module と `gom init` を両方入れるか、どちらかに絞るか
3. C の汎用 diagnostic wire は wave50 でやるか。E0401 の副 span まで代表利用に含めるか
4. D は実装 laneにするか、まず調査/decision lane に絞るか
5. E を wave50 の主 laneにするか。大きい場合は D か C を落とすか
6. E を採用しない場合、代わりに checker 分割 / rt 表示一元化 / benchmark 基盤のどれを入れるか

## 見送り候補（少なくとも現時点では lane にしない）

- module 外 `gom run` scratch module / `gom init` — 価値はあるが wave50 では負荷を避け、次回へ延期（podhmo、2026-11-27）
- checker の状態分割 — 有効だが E と併用すると compiler 中心部の衝突が大きい。E を採らない場合の第一代替
- 値表示の rt 一元化、生成コード benchmark 基盤 — 有効な内部改善だが、今回はユーザー可視 DX を優先
- stdlib `str` 拡張、Option traverse — 需要が弱く、DMMF 主軸の阻塞ではない
- stack-carrying error 値の生成 — `@extern` shim で回避可能。具体的な頻出ユースケースをもう少し集める
- panic capture adapter — wave49 と同じく見送り。boundary guide の運用で十分か観察する
- 継続行誤パース診断、E0201 typo 候補、エラー回復拡大 — 価値はあるが C の wire 設計と重なるため、C の結果を見て分割する
- `gom eval` / REPL、`gom search`、goimport export data 共有、`gom fix` の `.gomi` 検査 — 設計または需要確認が大きい
- `gom install`、Go 定数セット、cgo/unsafe/reflect、セルフホスト — future work 維持
