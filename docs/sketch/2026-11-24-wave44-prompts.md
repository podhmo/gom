# wave44 prompts 草案

- 日付: 2026-11-24
- 前提: wave43 チェーン（#213→#214→#215→#216→#217→#218）
  マージ後の main から開始。候補は lane F で登録した selfish
  移植の観測 + 台帳の open 項目から — **採否インタビューは
  merge 前に実施する**

## 候補

### A. `%w` error wrapping の第一級サポート（規模: 中）

生成 `fmt.Errorf` が引数を `any(...)` に box するため `go vet`
が `%w` を弾く — `wrap`/`wrapf` は `@extern` shim 退避が必要。
`Result` がある分 error wrapping は言語側で持ちたい（selfish
移植の最大の改善要望）。専用構文か生成規約の見直しか調査→
decision。参照: [open-questions](../open-questions.md)

### B. スカラーリテラルの meta bind 遅延（規模: 中〜大）

共有メタを持つ部分適用で供給リテラルが `?b := String` に先に
commit し、want 側の宣言型に materialize できず E0401
（`fn pair (x: b) (y: b) -> String` + `let g : Name -> String
= pair "x"`、#215 escalate）。数値リテラルの pendingLit 式の
遅延をスカラーにも広げるか、部分適用の conform 順序を
want-aware にするか。参照: [implementation-gaps](../implementation-gaps.md)

### C. `@method` の `Result` 返り許可（E0420 緩和）（規模: 中）

`encoding.TextUnmarshaler` 系（`UnmarshalText([]byte) error` 等
error を返す Go interface）を gom 型に実装できない。現状は
同パッケージ `shim.go` に Go で書く回避。許す場合の型変換
（`error` ↔ `Result T GoError`）を decision まで。

### D. `host.gom`→`host.go` clobber 対策（規模: 小）

同名の手書き `.go` が生成で上書きされる。marker ガード
（`isGomGenerated` 拡張）か手書きは `shim.go` に寄せる慣習の
文書化か — 方針を決めて実装。

### E. `List.filter_map` / `List.find_map` + 複数行カリー適用（規模: 小〜中）

stdlib 追加（`fold` 回しの解消）と、`List.fold\n  (fn ...)` が
E0401 になるパース弱さの調査。別 lane に分けてもよい。

### F. `gom install`（規模: 中〜大・Future work）

書き換え後の selfish リポジトリ等を指定してインストールする
経路（`go install` 相当）。依存パッケージの推移的 emit
（TODO の `gom run` 不ビルド問題）が前提課題。
