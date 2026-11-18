# 並列 dir コンパイルの検証メモ（2026-11-18, wave21 lane3）

## 検証項目

1. `go test ./...` — 既存の全 spec/golden/単体テストが並列スケジューラ経由でも同一の結果。
2. `go test -race` で cmd/gom, internal/goimport の競合がないこと。
3. 依存のある multi-dir ケース（`testdata/spec` の multi-package fixtures 経由）で dep 層が正しい順で走ること — spec テストが複数 dir を含む場合に等価。

実施結果はこのファイルの下部に追記する。

## 結果（2026-11-18）

- `go test ./...` 全パッケージ pass（デッドロック修正: commit/Hit は envKeyLocked を持ち回り、`go:` プールの depSeen を manifest に併記する修正込み）。
- `go test -race ./cmd/gom/` pass — goimport 共有 Loader・buildCache の並行アクセスにレースなし。
- 手動確認（/tmp/w21dep, module example.com/w21）: `gom build ./b/b.gom ./a/a.gom ./c.gom` — b が a に import 依存・引数順は逆でも `b/b.go a/a.go c.go` 全て生成。dep 層で a が先に走る。
- 初回 `gom info`（a.gomi 未存在）で b は E0203 — 直列と同一の挙動（.gomi ドラフトの書き込みは全コンパイル後）。2 回目で ok:true。
