# wave51 lane E: labeled lambda seeding / structural record 調査

- 日付: 2026-11-27
- lane: wave51 lane E
- decision: [labeled lambda の record seeding / structural record](../decisions/2026-11-27-labeled-lambda-record-seeding.md)

## 調査範囲

実装は wave52 以降。今回は labeled expected signature と positional lambda の相互作用、および structural record の需要・影響を probe で確認し、decision のみ行った。

## Probe 1: labeled callback × positional lambda

```gom
type Acc = { count: Int }
type Other = { count: Int }

fn apply_unlabeled (f: a -> a) (seed: a) -> a = f seed
fn apply_labeled (f: ~acc: a -> a) (seed: a) -> a = f ~acc: seed

fn positional_unlabeled () -> Acc =
  apply_unlabeled (fn acc -> { acc with count = acc.count + 1 }) { count = 0 }

fn positional_labeled () -> Acc =
  apply_labeled (fn acc -> { acc with count = acc.count + 1 }) { count = 0 }

fn labeled_labeled () -> Acc =
  apply_labeled (fn ~acc -> { acc with count = acc.count + 1 }) { count = 0 }
```

現行結果:

- `positional_unlabeled` は通る
- `positional_labeled` は E0401/E0407/E0401 に落ちる。`{ acc with ... }` の `Acc` が推論できず、seed literal `{ count = 0 }` も `Acc` / `Other` の field-set 衝突で ambiguous になる
- `labeled_labeled` は通る

positional lambda は spec 2.1 の unlabeled → labeled introduction で最終的には合法である。したがってこれは positional lambda の意味を広げなくても、推論時に label-erased な文脈を使えば解消できる不一致である。

## Probe 2: named type のない ad hoc accumulator

```gom
fn adhoc (xs: List Int) -> Int =
  (List.fold (fn acc x -> { acc with count = acc.count + x }) { count = 0 } xs).count
```

現行結果は E0401/E0407 の連鎖。named record が 1 つもなければ field-set 推論の対象自体がない。

## selfish / DMMF の需要確認

- selfish 移植では無名 record accumulator が実際の摩擦として観測されていたが、named accumulator 型を置く回避で実装可能だった
- DMMF fit では `UnvalidatedOrder` / `ValidatedOrder` / `PricedOrder` のように、状態や概念を nominal な domain type として名付ける方向が中心だった。anonymous structural record がないと写せないパターンは観測されなかった
- stdlib の公開 HOF には labeled callback がなく、labeled seeding の需要は主にユーザー定義 HOF である

## 影響調査

structural record を入れると次の境界が同時に動く。

- nominal type identity と field-set ambiguity resolution
- generated Go の struct / 1-field defined type と export 名
- struct tag、`@method`、`.gomi` の interface contract
- equality/display/runtime representation
- recursive record E0413 の意味

一方 label-erased seeding は既存の unlabeled → labeled introduction の前段推論に限られ、最終 conformance を変えない。

## 結論

- wave52 実装候補: label を inference contract に入れず、parameter 順序と型だけを残す label-erased view で positional lambda の record parameter を seed する
- structural record: 不採用。named record 必須を維持する
