# DMMF 型ドメインモデリングの境界ガイド

DMMF (Domain Modeling Made Functional) 風に「ドメイン型を不正状態のない核」と「外部 schema / Go runtime の境界」へ分けるときの推奨形。根拠は [DMMF fit report](../sketch/ja/2026-11-26-dmmf-order-taking-fit.md)、決定は [decision](../decisions/2026-11-27-dmmf-boundary-guide.md)。

## JSON / external schema

- 外部 schema が安定している・利用者に公開する API では、sum type や `Option` を direct marshal しない。implementation shape（`{"V0": ...}` や `{}`）を外部契約にしない。
- tagged union は boundary 専用の DTO へ手書き変換する。小さく動的な event envelope や 1 フィールド object では `Go.Map String Go.Any`、2 つ以上の固定フィールドを持つ object なら struct tag 付きの専用 record を使う。Gom の 1 フィールド record は Go の defined scalar として emit され JSON object にならないため、1 フィールド DTO record を object schema に使わない。
- nullable string / optional field が外部 schema 上重要なら DTO 側で `Ptr` nullable を使う。ドメイン型では `Option` を使い、境界で相互変換する。空文字を optional の意味に使うのは、既存 schema がそうなっている場合の割り切りに限定する。`Ptr` だけでは field absence と explicit `null` を区別できない。PATCH のように「未指定=変更なし」と `null`=clear を区別する schema では、presence flag 付き DTO か手書き Go boundary を使う。
- 外部入力は必ず DTO → smart constructor → domain type の順で検証する。`Ptr.new` / `encoding/json` は abstract 型の不変条件を通さないため、domain type へ直接 decode しない。

## bounded context / visibility

- 1 bounded context は Go module 内の 1 subtree に置き、context 内部 implementation は Go の `internal/` 相当の位置へ寄せる。外部へ見せるのは `api` / public types 側だけにする。
- Gom の公開単位は package + `.gomi`。`.gomi` に書かない型・関数は package-private なので、型ごとに小さな package を分け、public surface を `.gomi` で絞る。
- F# の `module String50` に相当する型名 namespace は作らない。小さい value type は `string50_create` / `string50_value` 命名か、型ごとの package に分けて `string50.create` 風に呼ぶ。大きな context では後者を優先する。
- friend visibility（選択した sibling package だけへ公開する仕組み）はない前提で設計する。共有が必要なら public `.gomi` に出すか、同じ package 内に置く。fine-grained friend export を模倣するために全 implementation を public にしない。

## 金額 / exact decimal

- 金額の既定表現は integer minor units（例: cents / 銭）と unit を持つ nominal record にする。`Float` は binary64 なので DMMF の `decimal` 相当には使わない。
- 通貨単位や scale が複数ある場合は、unit/scale を型または値で明示する。加算は同じ unit/scale 同士に限定し、乗算や丸めは domain function へ集約する。
- 任意精度 decimal が必要な場合は外部 decimal package を許容するが、boundary adapter と domain helper 経由で使う。`+` / `*` の演算子は使えないため、decimal メソッド呼び出しを domain 全体へ散らさない。
- 金額を外部 schema へ出すときは integer minor units か decimal 文字列にし、binary float にしない。
