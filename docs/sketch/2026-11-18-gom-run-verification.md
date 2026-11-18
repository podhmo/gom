# `gom run` の検証記録 (wave19 lane 2)

- 日付: 2026-11-18
- 対象: `gom run` サブコマンド — decision: [2026-11-18-gom-run](../decisions/2026-11-18-gom-run.md)

## 手動確認

`module m`（`replace github.com/podhmo/gom` 指向の go.mod 付き）に `package main` を置いて:

- `gom run ./pkg -- a b c` — 生成 `.go` が pkg 横に書かれ、プログラムが `argc 4`（バイナリ + 3 引数）を印字、`--` 以降が argv に届く
- `os.Exit 3` を出す本体で終了コード 3 がそのまま返る（`go run` 経由なら 1 に潰れる）
- 非 `package main` — "not a package main" で終了コード 2
- コンパイルエラー — 通常の診断ストリーム（`diagnostic` + `summary`）で終了コード 1、実行しない

## 自動テスト

`cmd/gom/run_test.go` の 4 ケース: 実行+argv+終了コード透過、コンパイル失敗のストリーム、非 main 拒否、複数ディレクトリ拒否。

## ゲート

`gofmt -l .` 空、`go vet ./...`、`go test ./...`、`sh scripts/check-sumtype.sh` 通過。
