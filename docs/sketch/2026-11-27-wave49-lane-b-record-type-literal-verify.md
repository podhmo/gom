# wave49 lane B: record 型名前置 literal 検証

## 実施

- `ValidatedOrder { id = id, total = total }` / `pkg.T { ... }` の型名前置 record literal を追加した
- 型パラメータを持つ record は `Box { value = x }` のようにフィールド値から型引数を推論する。期待型が `Box Go.Any` など整合する形のときはその型引数を使い、alias prefix は展開後の record へ解決する。alias が固定・再利用するパラメータ制約は期待型で上書きせず、展開に現れない alias パラメータは E0407 にしない。期待型の適合は speculative な通常 unification で判定し、カリー化 tail と共有型引数の伝播を扱う
- 頭がコンストラクタに解決できる `Wrap { value = x }` は従来どおりコンストラクタ適用として残る
- 一般的な expected-type / field-type 推論強化は入れていない

## 結果

- `go run ./cmd/gom check /tmp/recprefix.gom` / `/tmp/recprefix2.gom` / `/tmp/recprefix3.gom` / `/tmp/recprefix-aliasbugs.gom`: pass（ローカル型、generic record、Go.Any expectation、alias、unused alias parameter、fixed/repeated/shared alias、curried tail、constructor 衝突形）
- `go test ./internal/build -run 'TestSpec/ok_record_type_literal' -count=1`: pass（local / generic / constructor / imported qualified type）
- `go test ./internal/build -run 'TestGolden/record_type_literal' -update -count=1`: pass

## 境界

- `T { ... }` は型名が値として解決できないときだけ型名前置 literal になる。コンストラクタ適用との曖昧さは既存優先で決定的に潰す
- record 更新 `{ base with x = v }` の型名前置構文は追加していない

- `testdata/spec/E0401_record_type_alias_mismatch.gom`: fixed alias と矛盾する期待型が上書きされず E0401 になることを固定
- `testdata/spec/E0401_record_type_alias_repeated_mismatch.gom`: repeated alias parameter が `(Int, String)` を取れないことを固定

### review round 4 — speculative replay boundary

- The expected-type trial still uses real unification over cloned metas, but it no longer replays the whole unification onto the real types. Only concrete solutions for metas owned by the record prefix are replayed. External inference metas are left for ordinary checking order, so a numeric constraint on a surrounding meta cannot flow backwards into an unresolved record type argument before the surrounding expression itself is checked.
- Direct regression coverage: `internal/check/record_prefix_test.go` checks both that a failed trial leaves external/prefix metas, diagnostics, and the meta table untouched, and that a numeric constraint on the surrounding expression is not propagated into the record type argument by lookahead.
- Validation after the change: `go test ./...`, `go vet ./...`, `gofmt -l .` (empty), and `sh scripts/check-sumtype.sh` pass.

Partially concrete expectations are retained: unknown expected metas become fresh open metas inside the replayed prefix type, while concrete structure such as `Go.Any` still reaches field checking. Regression coverage is in `record_prefix_test.go` and `ok_record_type_literal.gom` (`partially_concrete_expected`).

Final replay boundary: unshared wholly open solutions are skipped before reification, so no unused artifact meta is minted. Shared open solutions — including a bare argument whose mate occurs only nested inside another argument — are replayed through ordinary (non-quiet, non-silent) fresh metas. This preserves equality constraints without suppressing E0401/E0407 when inference truly cannot determine the type. Direct coverage is in `internal/check/record_prefix_test.go`; end-to-end coverage is `ok_record_type_literal_nested_shared` and `E0407_record_type_shared_open`.
