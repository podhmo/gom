# wave56 lane B: E0201 module member typo candidates

- 日付: 2026-11-28
- lane: [wave56 prompts](2026-11-28-wave56-prompts.md) lane B
- decision: [e0201-member-typo-candidates](../decisions/2026-11-28-e0201-member-typo-candidates.md)

## 実施内容

- E0201 `namespace: "member"` に、lookup に使った member table 内の optimal string alignment 距離 1 の候補を最大 3 件付けるようにした
- 候補は距離昇順・名前昇順。`data.candidates` に `{ "name": ..., "edit_distance": 1 }`、note に `did you mean ...?` を出す。`fixes` は付けない
- 通常 module table に加え、`Go.Map` / `Go.Slice` operation table の miss（constructor fallback 経路）にも同じ候補表示を付けた
- spec harness の explicit-empty assertion を `fixes` に拡張し、`-update` でも `fixes: []` を保持するようにした
- spec 12 と E0201 explain / generated error-codes を更新した

## red-first

先に positive / negative / truncate fixture を用意し、実装前に次を確認した。

```text
diagnostics[0].data["candidates"]: key missing from actual data
diagnostics[0].notes[0]: expected note not found: "did you mean `map`?"
diagnostics[0].notes[0]: expected note not found: "did you mean `length`?"
diagnostics[0].notes[0]: expected note not found: "did you mean one of `foo1`, `foo2`, or `foo3`?"
```

実装後は同 fixture が green になった。

## fixture

- `E0201_list_member`: stdlib module `List.mapp` → `map` の positive case
- `E0201_go_package_member_typo`: import した Go package `fmt.Printlm` → `Println`
- `E0201_go_slice_member_typo`: `Go.Slice.lenght` → `length`（隣接転置）
- `E0201_go_map_member_typo`: `Go.Map.lengt` → `length`（1 文字削除）
- `E0201_member_typo_negative`: 距離 1 の候補がない `List.zzzz` で note/fix がないことを固定
- `E0201_member_typo_truncate`: import した Gom package `foreignpkg.foo` が `foo1`–`foo4` に距離 1 で、先頭 3 件だけ出ることを固定

## 出力確認

- JSON: 上記 spec fixture が `data.candidates` / note / `fixes: []` を含めて通る
- text: `go run ./cmd/gom check -format text testdata/spec/E0201_list_member.gom` で `= note: did you mean \`map\`?` が出ることを確認した
- `go generate ./internal/diag` 後の `gom explain E0201` 相当の generated error-codes が候補表示を説明する

## 検証

- `go test ./internal/resolve ./internal/gomtest -count=1`
- `go test ./internal/build -run 'TestSpec/(E0201_list_member|E0201_go_package_member_typo|E0201_go_slice_member_typo|E0201_go_map_member_typo|E0201_member_typo_negative|E0201_member_typo_truncate)$' -count=1`
- `gofmt -l .`（空）
- `go vet ./...`
- `go test ./...`

## 残件

- bare name typo、未 import stdlib module、module/type fallback constructor の候補は lane B の scope 外（lane E の調査対象）
