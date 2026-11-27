# wave55 lane A: record quiet recovery

- 日付: 2026-11-28
- lane: [wave55 prompts](2026-11-27-wave55-prompts.md) lane A
- decision: [record-ambiguity-secondary-mismatch](../decisions/2026-11-27-record-ambiguity-secondary-mismatch.md)

## 実施内容

record literal の field-set resolution が失敗し、`recordByFields` が ambiguity/no-match をすでに報告した経路で、recovery type を `Unit` にしないようにした。

- `wantT` が meta のときは `Unit` へ bind せず、その meta を `markQuiet` する
- `wantT` がないときは新しい quiet meta を recovery type に使う
- field-set resolution が成功する通常の推論、型名前置 literal、callback result propagation の規則は変えない
- backward inference は導入しない。quiet meta が後続使用から解かれるのは、すでに erroneous な入力の recovery だけである

## red-first

`testdata/spec/E0407_record_context_no_backward.expected.json` を先に E0407 1 件だけの期待へ更新し、実装前に次を確認した。

```text
diagnostic count: expected 1, got 2
type mismatch: expected Acc, got Unit
```

実装後は同 fixture が green になった。追加した independent mismatch/no-match fixture も、lane B 時点の worktree へコピーして確認すると、実装前はそれぞれ expected 2 / actual 3、expected 1 / actual 2 で red だった。

## fixture

- `E0407_record_context_no_backward`: ambiguous record の root E0407 だけが出て、後続使用に `got Unit` の二次 E0401 が出ない
- `E0407_record_quiet_independent_mismatch`: quiet meta が `Other` へ解かれたあと `Acc` として返すと、独立した `expected Acc, got Other` が残る
- `E0401_record_no_match_quiet`: no matching record の経路でも root diagnostic だけが出て、recovery artifact の mismatch は出ない
- `E0407_record_update_quiet_no_duplicate`: quiet recovery した record を `with` 更新しても、同じ ambiguity を update 側で二重に報告しない
- `E0407_record_no_want_quiet`: 期待型のない mid-block record literal でも新規 quiet meta が余計な診断を足さない
- `E0401_record_quiet_want_still_resolves`: shared want が別腕の recovery で quiet でも、後続腕の independent no-match resolution は行う

## 検証

- `go test ./internal/build -run 'TestSpec/E0407_record_context_no_backward$' -count=1`
- `go test ./internal/check ./internal/build -count=1`
- `gofmt -l .`（空）
- `go vet ./...`
- `go test ./...`

## 残件

- E0407 に候補 record 名や宣言位置を出す改善は wave55 lane D の調査対象で、この lane では実装しない
