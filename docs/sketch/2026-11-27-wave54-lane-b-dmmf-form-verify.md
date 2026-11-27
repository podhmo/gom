# wave54 lane B: DMMF fit recommended form 更新検証

- 日付: 2026-11-27
- lane: [wave54 prompts](2026-11-27-wave54-prompts.md) lane B
- 対象: [experiments/dmmf-fit](../../experiments/dmmf-fit/)

## 更新内容

wave53 lane A の callback result record propagation を前提に、DMMF example から不要になった型名前置 record literal を外した。

1. `validate_order_input`
   - 変更前: `input.lines |> ... |> Result.map (fn lines -> ValidatedOrder { id = input.id, lines = lines })`
   - 変更後: `input.lines |> ... |> Result.map (fn lines -> { id = input.id, lines = lines })`
   - `UnvalidatedOrder` / `ValidatedOrder` は同じ field set を持つが、pipe 結果の静的な期待型 `Result ValidatedOrder ValidationError` から callback 本体の結果位置へ型が伝わる
2. `fold_order_lines`
   - 変更前: `List.fold ... (LineTotals { quantity = 0, lines = 0 }) lines`
   - 変更後: `List.fold ... ({ quantity = 0, lines = 0 }) lines`
   - この位置でも compile が通ることを確認した。ただし `LineTotals` は field set が一意なので、staged record の曖昧性に対する主 probe は 1 である

生成 Go の意味は変えていない。`gom build` 後の `main.go` に差分は出なかった。

## 現在の recommended form

- 関数戻り値、constructor payload、generic application / pipe の callback 結果など、静的な期待型が既にある位置では無名 record literal を使ってよい。DMMF の段階型 pipeline でも `ValidatedOrder { ... }` を重ねて書く必要はない
- `let seed = { ... }` のような局所 literal を後続使用から逆推論することはない。同名 field set を持つ staged record を一旦局所変数へ置くときは、`let seed : ValidatedOrder = { ... }` の注釈か `ValidatedOrder { ... }` の型名前置 literal を残す
- 静的な期待型がなくても、field set がちょうど 1 つの record 型へ対応するなら注釈なしで推論できる。注釈または型名前置 literal が必要なのは、後続使用からの逆推論が必要な位置、または field set が曖昧・未対応で record 型が一意に決まらない位置である

## negative probe

次の一時 probe を `experiments/dmmf-fit/main/backward_probe.gom` に置いて `gom check` した。

```gom
package main

type BackUnvalidatedOrder = { id: String, lines: List BackLine }
type BackValidatedOrder = { id: String, lines: List BackLine }
type BackLine = { sku: String, qty: Int }

fn backward_probe (input: BackUnvalidatedOrder) (lines: List BackLine) -> Result BackValidatedOrder String = {
  let seed = { id = input.id, lines = lines }
  Ok seed
}
```

結果は従来どおり E0407 + 二次 E0401 だった。

```text
E0407 the record literal is ambiguous: 2 record types own these fields; annotate the type
E0401 type mismatch: expected BackValidatedOrder, got Unit
```

この二次 E0401 suppression は lane B の対象外で、wave54 lane C の調査対象である。

## 検証

```sh
go build -o /tmp/gom ./cmd/gom
cd experiments/dmmf-fit
/tmp/gom build domain main/main.gom
go test ./... -count=1
```

結果: `gom build` は diagnostics なし、`go test ./...` は `domain` / `main` ともに pass。

## 文書同期

- [wave52 lane D 検証](2026-11-27-wave52-lane-d-dmmf-fit-verify.md): wave52 時点の「この位置では型名前置 literal が推奨形」という案内へ、wave54 で supersede された旨を追記
- [DMMF fit 元レポート](ja/2026-11-26-dmmf-order-taking-fit.md): wave54 後の再検証節を追加し、pipe 経由 `Result.map` lambda 内では無名 literal を推奨へ変更
- 言語仕様や compiler の挙動は変えていないため、spec/decision の変更はなし
