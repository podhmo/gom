# 診断 UX の次段実装候補

- 状態: 採用（調査済み・実装候補の決定。実装は未実施）
- 日付: 2026-11-27

## 決定

次に実装する診断 UX は **E0401 expected type provenance の if/match 腕への伝播**とする。呼出し引数や record field の provenance、details 昇格基準、snippet 統合、fragment example 検証は今回の実装候補にしない。

## 理由

- E0401 は利用頻度が高く、wave50 lane C で `labels` wire と直接の let/return 注釈 label だけは既にある。分岐へ expected type が渡ると mismatch 位置だけが腕の中へ移り、注釈位置を辿れないままになる
- wave53 lane A で pipe/generic application 経由の expected type propagation を広げたため、期待型が静的に遠くへ伝わる場面が増えた。provenance がないと、正しい型推論ほど mismatch の発生位置と注釈位置が離れる
- 既存の `checkExprAt` / `expectedFrom` / `labels` を利用できる。formatter や JSON wire の shape を変えず、checker の provenance threading に限定できる見込みが大きい

## 実装候補の境界

- 最初の実装では、注釈からそのまま同じ expected type が供給される if/match 腕に限って provenance を伝える。nested if/match でも同じ expected type がそのまま腕へ渡る経路では伝えるが、呼出し引数・lambda 本体・record field・演算子の中へは広げない
- 腕の内部で独立に失敗した constraint mismatch へ注釈 label を漏らさない。ラベルはその expected type との最終 conformance が失敗した診断にだけ付ける
- 呼出し引数の parameter 宣言位置、record field の宣言位置は別の provenance 源が要るため今回の候補に含めない
- 仕様上の診断形式は変えず、`labels` の既存 wire を使う

## red-first fixture 方針

1. `fn f (c: Bool) -> Int = if c { 1 } else { "s" }` で、文字列側の E0401 が return 注釈 span を `expected type declared here` label に持つことを先に固定する
2. `match` の各腕でも同じ label が出る fixture を足す
3. 腕内の独立した演算 mismatch には label が付かない negative fixture を足し、wave50 の non-leak 規則を保つ
4. 実装後に `testdata/spec` の期待 JSON と text output の両方を確認する

## 見送った論点

- structured data の details 昇格基準: 現状の production use は E0301 が中心で、昇格判断の観測幅が狭い。汎用基準を先に決めると過剰設計になりやすい
- 近接 span の snippet 統合: label 増加後の可読性問題ではあるが、現状は重複表示があっても情報は読める。text renderer の layout 変更は影響面が広い
- `gom explain` fragment example 検証: 台帳・coverage の衛生としては有効だが、利用者向け診断そのものの改善ではない。self-contained 化は件数が多く、調査 lane では実装費用を正当化しにくい

検証記録: [sketch/2026-11-27-wave53-lane-c-diagnostic-ux-verify](../sketch/2026-11-27-wave53-lane-c-diagnostic-ux-verify.md)。
