# wave56 lane A: E0407 ambiguous record candidate display

- 日付: 2026-11-28
- lane: [wave56 prompts](2026-11-28-wave56-prompts.md) lane A
- decision: [record-ambiguity-candidates](../decisions/2026-11-28-record-ambiguity-candidates.md)

## 実施内容

- `recordByFields` が ambiguity 時に候補 record を deterministic に収集し、`qual` → `name` 順へ並べるようにした
- E0407 に最大 5 件の `candidate record <name> declared here` secondary label を付ける。foreign record は qualified name を使う
- `data.candidates` に全候補を `{ "name": ..., "qual": ... }` の列で入れる。5 件超は `and N more candidate records` note にする
- import した `.gomi` の declaration span は絶対パスで来るため、CLI の出力 resolver は in-memory input を優先しつつ dependency file を disk から解決するようにした。spec harness 側の `NewDirResolver` も absolute path を読めるようにした
- E0407 の `gom explain` / generated error-codes と spec 12 の診断形式説明を更新した

## red-first

先に 4 つの fixture を追加し、実装前に次を確認した。

```text
diagnostics[0].data["candidates"]: key missing from actual data
diagnostics[0].labels[0]: no remaining actual label
diagnostics[0].notes[0]: expected note not found: "and 1 more candidate records"
```

実装後は同 fixture が green になった。

## fixture

- `E0407_record_candidates`: 同一 file の local 2 候補。宣言 label と deterministic な `data.candidates` を固定
- `E0407_record_candidates_crossfile/`: sibling file に分かれた local 2 候補。cross-file declaration span を固定
- `E0407_record_candidates_foreign`: import した `foreignpkg.Point` と local `Point` の同名衝突。qualified name、foreign `.gomi` declaration span、`qual` を固定。foreign span の file 名は環境依存の absolute path なので、fixture では byte offset と message を pin する
- `E0407_record_candidates_truncate`: 6 候補で label は 5 件、`data.candidates` は全件、overflow note を固定

## 出力確認

- JSON: spec fixture が `data.candidates` / labels / overflow note を含めて通る
- text: `go run ./cmd/gom check -format text testdata/spec/E0407_record_candidates_foreign.gom` で local declaration と import `.gomi` declaration の両方の snippet が出ることを確認した
- `go run ./cmd/gom explain E0407` が候補 declaration label と `data.candidates` の説明を返すことを確認した

## 検証

- `go test ./internal/build -run 'TestSpec/E0407_record_candidates' -count=1`
- `go test ./internal/diag ./internal/check ./cmd/gom -count=1`
- `gofmt -l .`（空）
- `go vet ./...`
- `go test ./...`

## 残件

- foreign `.gomi` の出力ファイル名は現行の compiler span どおり absolute path になる。表示を module-relative などへ正規化するかはこの lane の scope 外
