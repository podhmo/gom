# wave49 lane B: record 型名前置 literal 検証

## 実施

- `ValidatedOrder { id = id, total = total }` / `pkg.T { ... }` の型名前置 record literal を追加した
- 型パラメータを持つ record は `Box { value = x }` のようにフィールド値から型引数を推論する。期待型が `Box Go.Any` など整合する形のときはその型引数を使い、alias prefix は展開後の record へ解決する。alias が固定・再利用するパラメータ制約は期待型で上書きせず、展開に現れない alias パラメータは E0407 にしない
- 頭がコンストラクタに解決できる `Wrap { value = x }` は従来どおりコンストラクタ適用として残る
- 一般的な expected-type / field-type 推論強化は入れていない

## 結果

- `go run ./cmd/gom check /tmp/recprefix.gom` / `/tmp/recprefix2.gom` / `/tmp/recprefix3.gom` / `/tmp/recprefix-aliasbugs.gom`: pass（ローカル型、generic record、Go.Any expectation、alias、unused alias parameter、fixed/repeated alias、constructor 衝突形）
- `go test ./internal/build -run 'TestSpec/ok_record_type_literal' -count=1`: pass（local / generic / constructor / imported qualified type）
- `go test ./internal/build -run 'TestGolden/record_type_literal' -update -count=1`: pass

## 境界

- `T { ... }` は型名が値として解決できないときだけ型名前置 literal になる。コンストラクタ適用との曖昧さは既存優先で決定的に潰す
- record 更新 `{ base with x = v }` の型名前置構文は追加していない

- `testdata/spec/E0401_record_type_alias_mismatch.gom`: fixed alias と矛盾する期待型が上書きされず E0401 になることを固定
