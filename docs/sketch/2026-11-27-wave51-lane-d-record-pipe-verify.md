# wave51 lane D: record pipe 二重 mismatch 抑制 検証

## 実施

- red-first: `E0401_record_pipe_mismatch_once` を追加し、型名前置 literal と変数 operand の 2 ケースで pipe ごとに 1 件の E0401 だけを期待する JSON を先に置いた。実装前の `go test ./internal/build -run 'TestSpec/E0401_record_pipe_mismatch_once' -count=1` は expected 2 / actual 6 で fail し、各 pipe が operand span の E0401、pipe span の同文 E0401、回復用 E0407 の 3 件を出すことを確認した
- `pipeArgWant` で使った record seed と operand の settled type から、operand の trailing conformance がすでに失敗した場合だけを検出し、その事実を `pipeCall` へ渡すようにした
- 失敗済みのときは `pipeCall` で parameter conformance を再実行せず、`conformCalleeAfterParamFailure` が result tail だけを通常どおり settle する。これにより pipe span の重複 E0401 と回復用 E0407 の両方を抑える
- 正しいコードでは seed 適合後に operand type と record seed が一致するため、従来どおり `conformCallee` を通る。推論規則や変換規則は変更していない

## 結果

- `go test ./internal/build -run 'TestSpec/E0401_record_pipe_mismatch_once' -count=1`: pass
- `go test ./internal/check ./internal/build -count=1`: pass
- 最終確認コマンドの結果は下記のとおり:
  - `go test ./...`: pass
  - `go vet ./...`: pass
  - `gofmt -l .`: 出力なし
  - `sh scripts/check-sumtype.sh` / `sh scripts/check-staticcheck.sh`: pass

## 境界・残件

- 抑制するのは pipe operand が `pipeArgWant` の record seed にすでに失敗した同じ mismatch だけである。別の型不適合や Go boundary の診断は従来どおり報告する
- record seed がない pipe、Go callee / saturated Go call の特殊経路、composition、`Result` pipe の規則は変更していない
- error recovery の result tail は callee から分かる型へ settle するが、失敗した引数自体の型を変えたり、boundary conversion を追加したりはしない
