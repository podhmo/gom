# wave45 prompts 草案

- 日付: 2026-11-24
- 前提: wave44 チェーン（#222→#223→#225→#226→#227→#228）
  マージ後の main から開始。**採否インタビューは merge 前に
  実施する**

## 候補

### A. `@method` の境界綴り `Int` → `int`/`int64` 選択（小〜中）

lane C で発見: `int` と `int64` は Gom 側では同じ `Int` に畳まれる
ため、メソッド emit は interface 側が期待する `int` を選ぶ。
`io.Seeker.Seek(int64)` や `io.ReaderAt.ReadAt` に実装が適合しない
（check は通るが生成 Go がコンパイル不一致）。interface 側の
シグネチャから emitted 綴りを引く仕組みが要る。台帳済み。

### B. eval の float32 値表現（中）

#226 で escalate: compiled は `u == lit` を float32 で比較するが
eval の VFloat は f64 オンリー — `16777217.0` arm が compiled で
match して eval で miss する。width-aware な値表現（record 生成時に
フィールド幅で round）が要る。

### C. 宣言型 splice 表示の正（wave44 で不採用・再提示）

compiled は `{ on = true }`、テスト oracle は `true` — 同一値が
2系統で違う表示。正を決める decision + 揃え実装。台帳済み。

### D. `host.gom`→`host.go` clobber 対策（wave44 で不採用・再提示）

同名の手書き .go が生成で上書きされる。marker ガードか「手書きは
shim.go」慣習の文書化か。

### E. `gom install` — 書き換え後リポジトリのインストール経路（中）

selfish 移植の未来形: 書き換え後の gom リポジトリを指定して
インストールする経路。open-questions の Future work 済み。

### F. MoonBit 式スナップショットテスト内蔵 + assert→go-cmp（中〜大）

open-questions Future work: `gom test --update` で期待値更新する
組み込み snapshot 機構、assert/assert_eq 失敗差分の go-cmp 化。

### G. `Result (Option T, U)` の payload Option 宣言は extern 側のみ（小〜中）

メソッド直呼びの n-ary result は verbatim 固定 — `Ptr.to_option`
の後包みが要る（selfish 観測・台帳済み）。
