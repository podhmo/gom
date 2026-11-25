# wave47 prompts

- 日付: 2026-11-25
- 採否: B/F/C/A/D 採用、E 見送り（2026-11-25 インタビュー）— 順: B → F → C → A → D

wave46 の残件と TODO/open questions から。E（Go 定数セット / `iota` 流宣言）は future work に残す。

## 採用 lane

### B. `gom info` の stdlib 短名解決（小）

`gom list` が出す `list` / `option` / `result` / `cmp` をそのまま `gom info list` に渡せない。stdlib のシグネチャを見る入口として、short name を `gom list` と同じ表で解決する。

scope:

- stdlib short name の解決だけを対象にする
- short name 側は read-only inspection とし、module cache や cwd に `.gomi` draft を書かない
- 既存の file/dir 入力による draft generation は変えない
- external Gom package 全般の viewer 化や CLI 再設計は対象外

### F. tutorial での snapshot dogfooding（ごく小）

wave46 lane B で入れた `snapshot` を tutorial/17-tests でだけ使い、利用者向けの教材と最小限の dogfooding を兼ねる。

scope:

- `tutorial/17-tests/main.gom` に `snapshot` の例を 1 つ入れ、tutorial 文面と expected 出力を同期する
- `gom test --update` で生成した `tutorial/17-tests/__snapshot__/*.snap` を commit し、clean checkout では baseline missing にしない
- stdlib や compiler harness への大規模適用はしない
- `testdata/spec` / `testdata/golden` / `tutorial/*/expected.txt` の既存 golden harness は `internal/gomtest` のまま変えない

### C. `.gomi` 再生成での directive 引き継ぎ bug 修正（中）

`gom info` の再生成が `//gom:noescape` / `//gom:result-notaint` を落とす。spec はこれらを `gom info` が書き出すディレクティブとしているので、推論・scan・emit のどこで落ちるかを特定して修正する。

scope:

- `//gom:noescape` / `//gom:result-notaint` の再生成だけを直す
- `//gom:mutable` の既存引き継ぎ規則と矛盾しないことを確認する
- abstract 化・非公開化の意図を `.gom` / `.gomi` に残す仕組みは wave47 ではやらない

### A. type-erased composite 表示の runtime renderer（中）

型パラメータを残す composite の nested 表示で、compiled は Go `%v`、evaluator は Gom display 寄りに描き、要素区切りや struct 綴りがずれる。compiled/eval 共通の runtime renderer を作り、Gom 表示側へ寄せる。

scope:

- Go `%v` の完全再現はしない
- rt が表現できる Gom 値（scalar、tuple、list/array、record、variant、Go 境界の opaque 値）の runtime 表示を canonical 化する
- evaluator oracle も同じ canonical に合わせる
- 括弧 canonical や fallback 境界は wave46 lane A の決定を維持し、ここでは内容表示を揃える
- Go 値そのものの fmt 細則までは追わない。opaque 値は既存の Go 値表示規則に留める

### D. `uint` / `uint64` の第一級対応（中〜大）

`byte`/`uint8` 以外の符号なし整数は direct Go signature で E0501 になる。`text/tabwriter` の `flags uint` のような実例があるため、`uint` / `uint64` 中心に first-class type として開く。

scope:

- 対象は `uint` / `uint64` に絞る。`uint16` / `uint32` / `uintptr` は wave47 では開かない
- `Byte` の既存仕様は変えない
- literal 範囲、算術、比較、`Go.conv`、display/equality、eval/codegen、Go signature mapping を spec 決定して実装する
- 需要が出るまで `uint16` / `uint32` / `uintptr` は future work に残す

## 見送り

### E. Go 定数セット / `iota` 流宣言

Go API 向けの exported const 群を Gom 側から作る面はないが、sum type や `let` 定数で代替できる場面も多く、具体的な阻塞が少ない。wave47 では採用せず future work に残す。

## インタビュー確定済み

1. 採否と順序: B → F → C → A → D。E は見送り
2. C の scope: directive 再生成 bug の修正だけ。abstract/private 意図は後続候補
3. D の scope: `uint` / `uint64` 中心
4. A の方向: compiled/eval 共通の runtime renderer を作り Gom 表示へ寄せる
5. F の scope: tutorial/17-tests のみのごく小さい dogfooding
