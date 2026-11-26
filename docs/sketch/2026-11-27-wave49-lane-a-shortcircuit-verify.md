# wave49 lane A: `&&` / `||` 短絡評価 検証

## 実施

- evaluator は `false && x` / `true || x` で右辺を評価しない一方、lower は両 operand を先に atom 化していたため、compiled 側では `todo ()` などの右辺が短絡前に実行されていた
- lower で `&&` / `||` の右辺を独立した seq へ下ろし、atom だけで済む場合は Go 自身が lazy な `&&` / `||` を使い、複雑な場合は `core.If` の分岐内へ評価を閉じ込める形へ変更した
- spec §1.2 に短絡規則を明記し、guide の「generated Go では短絡しない」という古い注意を現行仕様へ合わせた
- `no () && boom ()` / `yes () || boom ()`（`boom` は `todo ()`）について evaluator と compiled が同じ結果になることを `internal/build/shortcircuit_test.go` で固定した。条件を literal ではなく関数返り値にし、constant folding に頼らず右辺の抑止を確認する
- 構文と型検査の fixture を `testdata/spec/ok_bool_short_circuit.gom` に追加し、生成コード差分は `testdata/golden/let_rec.golden.go` と `stdlib/cmp` の generated artifact で確認した

## 結果

- `go test ./internal/build -run TestBooleanShortCircuitEvalCompiled -count=1`: pass
- `go test ./...`: pass
- `stdlib/cmp/cmp.go` / `stdlib/cmp/cmp_test.go` は repo root から `go run ./cmd/gom test stdlib/cmp` で再生成

## 境界

- RHS が atom（変数・literal・field read など、Core IR の「評価しても effect を持たない operand」）なら `core.Binary` のまま生成する。atom の事前評価は意味を変えないうえ、Go の `&&` / `||` 自身が lazy なため readable な生成形を保てる
- RHS に call や match などの complex な評価がある場合だけ `if` へ展開する。`false && f ()` のように literal condition でも constant folding はしない — この lane は意味修正に限定し、最適化は入れない
