# wave47 prompts 草案

- 日付: 2026-11-25
- 採否: 未確定（merge 前インタビュー予定）

wave46 の残件と TODO/open questions から。順序案は A → B → C → D → E。

## 候補

### A. type-erased composite の `%v` 表示 parity（小〜中）
generic 位置の composite は compiled が Go `%v`、eval が Gom comparison display 寄りで、
複数要素 list/tuple の区切りや struct 綴りがずれる。lane A で括弧と fallback 境界は
揃えたので、残りの内容表示を台帳から潰す。evaluator に fmt 風 rendering を足すか、
spec が両者の差を許すかを先に決める。

### B. `gom info` の stdlib 短名解決（小）
`gom list` が出す `list` / `option` / `result` / `cmp` をそのまま
`gom info list` に渡せない。stdlib signature の入口として短名を解決する
（TODO 登録済み、sandbox explorer run 2 の gotcha）。

### C. `.gomi` 再生成での著者メタデータ引き継ぎ（中）
`gom info` の再生成が `//gom:noescape` / `//gom:result-notaint` を落とし、
abstract 化や非公開化の意図も残らない。`.gom` 属性か `.gomi` ディレクティブで
表し、再生成が引き継ぐ仕組みを決める。scope は noescape/result-notaint だけに
絞るか、abstract/private 意図まで入れるかが採否点。

### D. 符号なし整数の第一級対応（中〜大）
`uint` / `uint16` / `uint32` / `uint64` / `uintptr` が direct Go signature で
E0501 になる。wave46 lane C で named Go unsigned の eval 算術・表示は入ったので、
first-class type としてどこまで開くかを決める。literal 範囲、算術、`Go.conv`、
`byte` との関係を含む。

### E. Go 定数セット / `iota` 流宣言（中）
Go API 向けの定数群を gom 側から作る面がない。sum type や `let` 定数で代替できる
一方、Go 側に exported const 群を出したい用途は残る。優先度は低めの候補。

## インタビュー確認点

1. A〜E の採否と順序。特に E は今 wave に入れるか future work に残すか
2. C の scope は directive 引き継ぎのみか、abstract/private 意図までか
3. D を採用する場合、対象を `uint`/`uint64` 中心に絞るか全 unsigned 幅か
4. A は evaluator に fmt 風表示を実装する方向でよいか、spec 上の許容差にするか
