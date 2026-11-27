# wave52 lane E: E0204/E0704/E0503 診断方向検証

- 日付: 2026-11-27
- lane: wave52 lane E
- decision: [E0204/E0704/E0503 の診断方向](../decisions/2026-11-27-diagnostic-direction-e0204-e0704-e0503.md)

## red-first

`E0101_select_recv_pattern` を `E0704_select_recv_pattern` へ改名して期待を E0704 にし、`E0204_unknown_attribute` を追加した時点で、両方とも現行 E0101 との差分で red になることを確認した。

## 実装

- unknown attribute は parser が E0204 を出す。code table の phase は syntax、spec 参照は §1
- select receive arm の左辺は parser が pattern として保持し、checker が変数・`_` 以外を E0704 にする。`x <- ch` / `_ <- ch` は従来どおり合法
- E0503 は retired。numeric conversion 違反の報告は E0401、明示変換は `Go.conv` の方針を error-codes/explain/coverage へ反映した

## fixture / 同期

- `testdata/spec/E0204_unknown_attribute`: E0204 と span を固定
- `testdata/spec/E0704_select_recv_pattern`: `Some j <- jobs` が E0704 になり、後続の型不一致を出さない形へ fixture を調整
- `internal/diag/codes_coverage_test.go`: E0204/E0704 は spec fixture coverage、E0503 は retired なので例外分類から除去
- `cmd/gom/explain_examples_test.go`: retired E0503 を fragment 分類から除去
- `docs/error-codes.md`: `go generate ./internal/diag` で同期
- 既存の attribute AST 懸念 2 件は parse 時 E0204 採用で解決済みに更新

## 検証コマンド

```sh
go test ./internal/syntax ./internal/diag ./internal/check ./internal/build ./cmd/gom -count=1
gofmt -l .
go vet ./...
go test ./...
```
