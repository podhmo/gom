# wave42 lane（採用確定）

2026-11-24 採否インタビューで A〜E 全採用。実施順は B → D → E → A → C:
一本化の小粒先行 → vendor 系（実装→調査）を隣接 → 共有 loader 正規化 →
最大の spec gap（C）は最後（decision 要の可能性があるため）。

## 候補

### A. spec suite 共有 loader の record-owner 汚染を正規化（バグ修正・規模: 中）

wave41 lane B で発覚: spec harness の 1 つの `gomimport.Loader` を
subtest 間で使い回すと `registerForeignTypes` が「これまでに読んだ全
.gomi の record 宣言」を呼び出し側 `c.records` に畳み込み、後続
fixture の record literal 持ち主判定（E0407）を汚染する — 単独実行は
pass・全体実行は fail の順序依存バグ。実際に import された record だけ
を畳むよう `files.go`/`loader` の境界を整理（回避で使った foreignpkg
のユニーク名はそのままで良い）。

### B. `canonicalImport` の `cmp` 欠落（小さな正規化・規模: 小）

`cmd/gom/cli.go` の `canonicalImport` が stdlibPaths と同趣の表を持つが
`cmp` が抜けている（wave41 lane B レビュー指摘・ledger 登録済み）。
`StdlibShortNames()`/stdlibPaths との一本化で drift を消す。

### C. literal→宣言型の代入可能性（仕様側の gap・規模: 中〜大）

`Clamp64(u, 0, 100)` が Go では untyped constant の代入可能性で通るのに
gom は conform が先に E0401 で落ちる（wave40 lane D 発見・ledger 登録済
み）。`~` 制約受理とは別経路で literal→宣言型の代入可能性が要る —
調査→decision か、決まっていれば実装まで。

### D. `gom list -external` の vendor 列挙（wave41 lane C の残件・規模: 小〜中）

module 側 vendor は `vendor/modules.txt` のパッケージ行 + `.gomi` 存在
確認で列挙できる（[調査](2026-11-24-gom-list-external-enumeration.md)の
実装メモ）。vendorEnabled 時のみ vendor walk を候補に足す。

### E. workspace vendoring（`go work vendor`）対応（調査・規模: 小〜中）

Go 1.22+ の workspace vendor は `<workroot>/vendor` を使うが loader は
module 側 `vendor/` のみ見る（ledger 登録済み）。`go.work` 検出と
vendor 解決・列挙への影響を調査、必要なら decision。
