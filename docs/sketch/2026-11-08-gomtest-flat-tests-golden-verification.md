# Verification: flat `tests` golden (`x_test.golden.go`)

検証対象: [decision: gomtest-flat-tests-golden](../decisions/2026-11-08-gomtest-flat-tests-golden.md)
日付: 2026-11-08

## 何を確かめたか

`case.json` に `{"tests": true}` を書いた flat ケース `x.gom` が
`x.go` と `x_test.go` を生成し、それぞれ `x.golden.go` /
`x_test.golden.go` に対比されること。併せて:

- `tests` なしで `x_test.golden.go` だけ置くと discovery の
  orphan 報告に乗ること（片方だけの期待を許さない）
- 実物の fixture が golden スイートで緑になること

## 手順

1. `internal/gomtest` の出力数チェックを「flat は `.go` 1 個
   +tests なら `_test.go` を足せる」に緩め、`goldenFor` に
   `x_test.go` → `x_test.golden.go`（manifest が `tests` のとき
   のみ）を足した。
2. `discoverCases` に `flatTestGoldens` マークを足し、tests あり
   flat ケースの `x_test.golden.go` だけ orphan 判定を免除。
3. `TestGoldenFlatTests`（TempDir、`x.gom` + manifest + 2 出力の
   generate で `-update` が両 golden を書く）と
   `TestGoldenFlatTestsOrphan`（tests true/false/manifest 無しの
   3 通りで orphan 判定）を追加。
4. `testdata/golden/test_flat.gom` + `test_flat.case.json` を置き、
   `go test ./internal/build -run TestGolden -update` で
   `test_flat.golden.go` / `test_flat_test.golden.go` を生成。

## 結果

- `go test ./internal/gomtest/ -run TestGolden` — 新旧テスト全部
  緑。`x_test.golden.go` が `-update` で書かれ、比較でも参照
  される。
- `go test ./internal/build -run 'TestGolden/test_flat'` — 緑。
  `test_flat_test.golden.go` は `TestAdd` を pin している。
- orphan 3 ケース（tests true で免除、tests false / manifest 無
  しで `x_test.golden.go` を orphan 報告）— 期待通り。

## 残る差分なし

ディレクトリケースの `<member>_test.golden.go` 規約は不変
（`test_name_collision/` がそのまま緑）。spec ケースには影響
なし（`tests` は golden スイート専用の manifest キー）。
