# wave39 lane 採択（確定版）

- 日付: 2026-11-24
- 状態: 確定 — 採否インタビューで A/B/C/D/E の 5 件採択
- 前提: wave38 スタック（#173–#179）マージ後に lane 実行を開始する
- 候補の出典: wave38 延期分（[prompts](2026-11-24-wave38-prompts.md)
  の繰越）+ wave38 lane F decision（採択時の実装）+ TODO/open-questions

## lanes

### A. `Go.try_as` — `Go.Any` 取り出し面の実装（decision→実装・規模: 中）

wave38 lane F の decision を採択: comma-ok 相当の `Option` 返り
`Go.try_as x : Option T` を唯一の取り出し面とする
（[decision](../decisions/2026-11-24-go-any-extraction.md)）。

- check で `x : Go.Any` + 目標 `T` の検査（`T` は Go.Any 不可等の
  境界確認）
- emit は rt generic helper（`rt.As[T]`/`rt.AsOption[T]` 型の
  `v, ok := x.(T)`）経由
- eval 側の内部断言、spec 8.3 追記、spec fixture + golden
- decision の状態を採択に更新

### B. record フィールドの Go struct tag（拡張調査→decision→実装・規模: 中）

採択、ただし「修飾の仕方のバリエーションをもう少し模索したい」
（podhmo 2026-11-24）— 属性引数・フィールド行 directive の2案に
限らず、decision フェーズで宣言形式の選択肢を網羅的に比較する:

- フィールド行 directive: `{ tag: String //gom:tag "json=tag" }`
- 属性引数: `@tag("tag=json.tag")` 類の decl 属性
- record 型宣言行の directive: `type R = //gom:tags ...`
- doc コメント規約（`//json:"tag"` を tag として読む）
- `{{.name}}` テンプレ参照との兼ね合い（フィールド参照文法との衝突）
- 複数 tag（`json:`・`yaml:` 併記）の表現力

- 作業: 比較 → decision → 採択形の実装（spec + fixture + golden）
- 規模: 中

### C. 部分適用・`Unit` 残り関数値の lint（調査→decision・規模: 小〜中）

`f x` の部分適用で残った関数値が意図せず再利用される懸念
（カリー由来）。警告対象の境界 — どの残り方をエラー/警告/許容と
するか — の調査 → decision。

### D. `//gom:mutable` directive（`.gomi`）実装（規模: 中・priority low）

wave36 lane G 採択済みの未実装分。extern 裏付け・abstract export の
宣言型を `.gomi` の directive で mutable 表明:

- name-only（index 無し）形のため専用スキャン（`scanDirective`
  ではなく）が要る
- ロード時に tyCons へフラグ適用、`containsMutable` が
  `Qual != ""` の `Con` で照合
- `.gomi` 再生成時は著者の directive を引き継ぐ
- ローカル `.gom` 宣言では受理しない（フィールドから導出可能）

### E. `Go.Any` への `match` 型テスト腕（調査→decision・規模: 中〜大）

lane F で棄却した案の再検討 — 開いた型への網羅性設計（default 腕
必須化の意味論）を調査 → decision。実装は wave40 以降。

## 延期した候補（次 wave 以降へ繰越）

- List ハイブリッド表現・chan Send 制限・並列化ルール・
  `|?>` 文言カスタマイズ — wave サイズに収まらない
- interp-composite-display — 繰越
- `iota` 流 Go 定数セット — Future work 区分
- `gom fix` で `.gomi` 適合検査 — 見送り決定済み
- D-3 README・D-4 go.mod 整理 — 終盤作業として後回し
