# wave49 lane D: E0301 text 不足パターン検証

## 実施

- `internal/diag` の text emitter が E0301 の `data.missing_patterns` を読み、`patterns not covered:` ブロックへ描くようにした
- 汎用 `labels` / `details` wire や他コードの data 描画は追加していない
- spec §4 はすでにこの人間向け出力を示していたため、spec 本文の変更は不要だった

## 結果

- `go test ./internal/diag -run TestWriteTextE0301MissingPatterns -count=1`: pass
- `go run ./cmd/gom check --format text /tmp/e0301check.gom`: E0301 の実出力に `patterns not covered:` と `None` が出ることを確認
- `go test ./cmd/gom -run TestCheckE0301TextMissingPatterns -count=1`: pass（compiler 全体を通した real E0301 で同ブロックを確認）
- `go test ./...` / `go vet ./...` / `gofmt -l .` / `scripts/check-sumtype.sh`: pass
- text 出力は source snippet のあとに反例を列挙し、note/fix の順序を保つ

## 境界

- E0401 の期待型由来 span など、他コードの詳細表現は対象外
- 汎用 diagnostic details / labels wire は open question のまま残す
- JSON の schema と `missing_patterns` の意味は変更しない
