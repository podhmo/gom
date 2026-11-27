# wave55 lane C: E0401 provenance の if/match 腕への伝播

## 結論

注釈から同じ expected type が供給される if/match 腕について、最終 conformance mismatch の E0401 に `expected type declared here` label を付ける実装を入れた。nested if/match と腕本体 block の最終式にも同じ経路で伝わる。腕内の独立した演算 mismatch、bare `Go.Any` へ bounding するだけの分岐には label を付けない。

## red-first

1. `E0401_if_arm_provenance`: nested if の文字列腕に return 注釈 label を要求する fixture を先に追加し、実装前は `no remaining actual label` で red
2. `E0401_match_arm_provenance`: match 腕の文字列に同じ label を要求し、実装前は同じく red
3. `E0401_if_arm_provenance_no_leak`: `2 + "bad"` の独立した numeric constraint mismatch に label がないことを要求。spec harness の `labels: []` は実際に label がないことだけを通すよう修正し、空配列が vacuous に pass しないようにした

red 確認コミット: `d30147e4 check: pin expected type provenance in branch arms`

## 実装後の確認

- `E0401_if_arm_provenance`: `"nested"` の E0401 が `fn choose ... -> Int` の `Int` span を label に持つ
- `E0401_match_arm_provenance`: `"none"` の E0401 が return 注釈の `Int` span を labelに持つ
- `E0401_if_arm_provenance_no_leak`: `expected a numeric type, got String` に label がない
- `E0401_if_arm_provenance_go_any_no_leak`: `-> Go.Any` は branch type を seed しないので、先行腕の `x: Int` が決めた branch join との mismatch `expected Int, got String` に注釈 label がない
- JSON output に `labels` が出ること、text output に副 span snippet として出ることを `gom check` で確認

text 出力例:

```text
error[E0401]: type mismatch: expected Int, got String
  --> testdata/spec/E0401_if_arm_provenance.gom:5:23
   |
5  |     if d { 1 } else { "nested" }
   |                       ^^^^^^^^
   |
   = label: expected type declared here
  --> testdata/spec/E0401_if_arm_provenance.gom:3:34
   |
3  | fn choose (c: Bool) (d: Bool) -> Int =
   |                                  ^^^
```

## 実装メモ

`checkExprAt` が受け取る注釈 span を `checkIf` / `checkMatch` / `checkBlock` へ明示的に渡し、実際に同じ `want` を受け取る腕と block 最終式だけを `checkExprAt` で検査する。join を detached にした腕と bare `Go.Any` では provenance を落とす。呼出し引数・lambda 本体・record field・演算子内部へは広げていない。

実装コミット: `50fb41a3 check: propagate expected type provenance into branch arms`

## 検証

- `go test ./internal/build -run 'TestSpec/E0401_(if|match)_arm_provenance' -count=1` — pass
- `go test ./...` — pass
- `gofmt -l .` — 空
- `go vet ./...` — pass
- review 補強: `-update` 再生成でも explicit empty `notes` / `labels` assertion を同じ diagnostic index/code に限り保持し、numeric literal constraint failure には注釈 label を付けない例外を spec/error-codes へ明記
