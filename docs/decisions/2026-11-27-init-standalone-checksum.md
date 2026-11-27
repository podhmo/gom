# `gom init` の standalone checksum

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave52-lane-b-init-checksum-verify.md)）
- 日付: 2026-11-27
- Supersedes: [module-external-run-init](2026-11-27-module-external-run-init.md) のうち `gom init` の checksum 解決を caller の module/workspace context で試してよいという扱い

## 決定

`gom init` は新しい project を standalone module として成立させる。init 時の toolchain dependency 解決は常に `GOWORK=off` で行い、caller の `go.work` / `go.work.sum` / workspace 内 `replace` を使わない。`go mod download` に加えて init template の generated code が必要とする `github.com/podhmo/gom/rt` / `github.com/podhmo/gom/stdlib/result` を同じ環境で解決し、local `replace` の参照先が存在しない、または release artifact が toolchain package を持たない状態を成功扱いにしない。

- init 先の `go.mod` が release module を `require` する場合は、その module だけで `go.sum` の checksum を解決できる必要がある
- init 先の `go.mod` 自身が開発用 `replace` を持つ場合は、その replace によって standalone に解決できる。replace 先には generated code が import する toolchain package が必要である。checksum がなくてもよいのはこのように module 自身が解決手段を持つ場合だけである
- standalone に解決できない場合は init を成功扱いにせず、release version を使うか project の `go.mod` に explicit な `replace` を書くよう促すエラーにする
- module 外 `gom run` の scratch module とは契約を分ける。scratch の寿命・cache・workspace 無効化の決定は変えない

## 背景

wave51 lane A の review では、parent workspace の `replace` で `go mod download` が成功すると checksum が `go.work.sum` 側へ入り、init 先に `go.sum` が残らない場合があることが指摘された。この project は workspace 内ではビルドできても、workspace 外では read-only mode の Go tool が checksum 不足で失敗する。`gom init` は project を始めるコマンドなので、caller context への暗黙依存を成功とみなさない。

## 検証

`cmd/gom/scratch_test.go` で次を固定した。

- parent `go.work` の local replace だけでは standalone resolution が成功せず、エラーが `GOWORK=off` と explicit replace / release version の方向を示す
- release module を file-based module proxy から解決すると、既存 `go.sum` の有無にかかわらず init 先の `go.sum` に checksum が残る
- local `replace` の参照先がない module は `go mod download` が成功しても toolchain package resolution で失敗する
- parent workspace 内で release module を要求する project を init し、workspace 外の `-mod=readonly` で `gom run` できることを固定した
- 既存 `go.mod` への additive edit と init template の実行は従来の `cmd/gom/run_test.go` fixture が固定する
