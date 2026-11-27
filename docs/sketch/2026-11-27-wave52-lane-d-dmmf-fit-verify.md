# wave52 lane D: DMMF fit example の現行機能更新調査

- 状態: 検証済み
- 対象: [experiments/dmmf-fit](../../experiments/dmmf-fit/)
- 元レポート: [DMMF fit](ja/2026-11-26-dmmf-order-taking-fit.md)
- 関連 decision: [record contextual inference](../decisions/2026-11-27-record-contextual-inference.md)、[match nested narrowing](../decisions/2026-11-27-match-nested-narrowing.md)、[labeled lambda record seeding](../decisions/2026-11-27-labeled-lambda-record-seeding.md)

## wave54 追記（recommended form の現行値）

wave53 lane A で pipe 経由 `Result.map` lambda 本体の無名 record literal へ前向き propagation が入り、wave54 lane B で `experiments/dmmf-fit` の recommended form を更新した。本文中の「generic higher-order pipe の lambda 本体では残る」「この位置では型名前置 literal が推奨形」という記述は wave52 時点の観測であり、現在の推奨は [wave54 lane B 検証](2026-11-27-wave54-lane-b-dmmf-form-verify.md) を参照する。

## 目的と範囲

`experiments/dmmf-fit` を wave49 以降に入った言語機能で実際に更新し、元レポートの不足項目が解消したか、よりよい書き方ができるようになったか、何が残るかを分類した。compiler / 言語機能の新規実装は行っていない。DMMF boundary guide の全面改訂も行わず、example 更新から明らかに stale だった agent skill の record 推論説明 1 箇所だけを修正した。

## example へ入れた更新

1. `validate_singleton_order`
   - `match validate_line line { ... Ok valid -> Ok { id = input.id, lines = [valid] } }` を追加
   - constructor payload の静的期待型から無名 record literal を推論できることを確認
2. `fold_order_lines` / `order_line_totals`
   - `~line` / `~acc` を持つ labeled callback へ、flat positional lambda `fn line acc -> { acc with ... }` を渡す形を追加
   - lane A の label-erased seeding により accumulator record `LineTotals` の推論が通ることを確認
3. `shipment_code_length`
   - `CommentLine` を外側で処理し、`product -> match product { ProductLine code -> ... }` と nested match する probe を追加
   - same-scrutinee narrowing により不可能な `CommentLine` 腕なしで網羅性が通ることを確認
4. Go 側 test
   - 上記 3 probe の実行結果を `main_test.go` で固定

## 元レポート不足項目の分類

| 元の不足 | 現状 | 分類 |
| --- | --- | --- |
| 段階型 record の型推論が弱い | 型名前置 literal は主経路として使える。direct `Ok { ... }` も戻り値型が constructor payload を pin すれば通る。labeled callback の positional lambda でも record accumulator を推論できる。ただし generic higher-order pipe の lambda 本体では残る | 大幅改善 / 一部残 |
| `Result.sequence` / `traverse` がない | `List.traverse_result` / `Result.sequence` は既に example で使用中 | 解消 |
| `&&` / `||` の短絡ずれ | 生成コードでも短絡する probe が既に example にある | 解消 |
| E0301 text の不足パターン詳細 | compiler 側で改善済み。example の runtime probe 対象外 | 解消 |
| nested match の網羅性が実用形で弱い | same-scrutinee root constructor narrowing で DMMF 形は通る。guard 条件や literal/field path は対象外 | 主用途は解消 / 精度化は残 |
| 型名 module / namespace がない | 変更なし。package 分割・命名規約で回す | 残 |
| visibility は package 単位 | 変更なし。boundary guide の運用ルールで回す | 残 |
| panic capture | 変更なし。必要なら Go 側 `@extern` adapter | 残 |
| JSON sum type / `Option` の external schema | 変更なし。DTO 境界規約は guide 済み | 残（運用で回避） |
| exact decimal | 変更なし。integer minor units / 外部 decimal の運用 | 残（運用で回避） |
| constructor 名の package 内一意性 | 変更なし。命名・package 分割で回す | 残 |
| abstract 型の zero-value bypass | 変更なし。DTO → smart constructor 境界で防ぐ | 残（運用で回避） |
| applicative validation の糖衣 | `Validation` / `validation_map2` probe はあるが専用糖衣はない | 残（優先度低） |

## 新たに確認した残課題

`input.lines |> List.traverse_result validate_line |> Result.map (fn lines -> { id = input.id, lines = lines })` に書き換える probe では、無名 record literal が E0407 のままだった。加えて `expected ValidatedOrder, got Unit` の E0401 が出て、record 未解決の二次診断として分かりにくい。spec §3.3 の generic/pipe propagation がこの形を含むように読めるため、implementation-gaps へ登録した。wave52 時点の推奨形はこの位置では `ValidatedOrder { ... }` の型名前置 literal か、constructor payload が静的に pin される direct `Ok { ... }` への書き分けだった。wave54 lane B で pipe 経由 `Result.map` lambda 内の無名 literal が実用 probe で通ることを確認し、現行の推奨は更新済みである。

この課題は lane D の scope では修正しない。一方で、labeled callback 内の record accumulator は lane A によって通るため、「labeled function parameter を含む文脈では contextual propagation が止まる」という agent skill の古い一文は修正した。

## 検証

```sh
go build -o /tmp/gom ./cmd/gom
cd experiments/dmmf-fit
/tmp/gom build domain main/main.gom
go test ./... -count=1
```

結果: `gom build` は diagnostics なし、`go test ./...` は `domain` / `main` ともに pass。

負の probe（意図的失敗）:

```sh
# validate_order_input の型名前置 literal を無名 literal へ一時的に戻した状態
cd experiments/dmmf-fit
/tmp/gom check main/main.gom
```

結果: record literal に E0407、加えて `expected ValidatedOrder, got Unit` の E0401。probe 後は型名前置 literal へ戻した。

## lane 外へ送るもの

- implementation gap: pipe 経由 `Result.map` lambda 内の無名 record literal と二次 E0401（登録済み）
- closeout 候補: DMMF boundary guide 全面改訂は不要。agent skill の stale 一文のみ修正済み。残る namespace / visibility / panic capture / JSON / decimal / constructor collision / zero-value boundary は既存 guide と open questions の運用課題として継続
