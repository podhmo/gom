# wave46 prompts 草案

- 日付: 2026-11-25
- 採否: 未確定（merge 前インタビューで確認）

wave45 で表面化したもの + 既存の台帳・future work から。

## 候補

### A. writeOperand の型駆動再設計 — foreign named scalar の括弧 canonical（中）
compiled は opaque 値を括弧で包む（`[(0.3)]`）が eval は値レベルで foreign 性を
見られず括弧なし。writeOperand を値駆動から型駆動にする再設計が要る
（wave45 lane B で台帳登録・width-32 入れ子表示の検証もここにブロックされている）

### B. `gom install`（中）
書き換え後の selfish リポジトリ等を指定してインストールする経路（future work 登録済み）

### C. MoonBit 式スナップショットテスト内蔵 + assert の go-cmp 化（中〜大）
`gom test --update` で期待値更新する方向。expected.txt 手同期の代替

### D. splice 補間の Go 値フォーマット精緻化（小〜中）
`GoError` 以外の Go 由来値は `%v` 素通し — gomShow との整合は未整備

### E. eval の width 伝播の残り（小）
conv 経由の int→float や cross-width の boxed-any 比較など、W タグの意味論を
狭めた残りの隅。台帳を見て必要なものだけ
