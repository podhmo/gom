# DMMF boundary guide

- 状態: 採用（文書化 — [guide](../ja/dmmf-boundary-guide.md)）
- 日付: 2026-11-27

## 決定

DMMF 型のドメインモデリングでは、次の 3 点を標準の境界ルールとする。

1. JSON tagged union / nullable field は external schema 専用の DTO 境界で表現する。sum type・`Option` の direct marshal を外部契約にせず、tagged union は専用 DTO record または `Go.Map String Go.Any` へ手書き変換し、nullable は DTO 側の `Ptr` に限定する。ドメイン型では `Option` と smart constructor を使う。
2. bounded context の visibility は package + `.gomi` + Go の `internal/` layout で設計する。型名 module や friend visibility は前提にしない。公開が必要なものだけを `.gomi` に出し、小さい value type は `string50_create` 命名または型ごとの小 package で整理する。
3. 金額は integer minor units を既定とする。任意精度が必要な場合だけ外部 decimal を boundary adapter / domain helper 経由で許容し、`Float` を金額に使わない。

運用上の詳細は [guide](../ja/dmmf-boundary-guide.md) を正とする。

## 背景

[DMMF fit report](../sketch/ja/2026-11-26-dmmf-order-taking-fit.md) で、言語機能不足とは別に「外部 schema / package visibility / decimal をどう運用するか」が移植可否を左右すると分かった。これらはコンパイラ変更より先に boundary 規約として決められる。

abstract 型の zero value 制限や panic capture adapter のような言語/ランタイム変更は wave49 lane E の対象外とする。zero value については、言語制限を足す前に「外部入力は DTO + smart constructor 経由」という boundary 規約で運用する。具体的な回避不能ケースが出た時点で lint / 制限を再検討する。

## 影響

- 言語仕様・コンパイラ実装は変更しない
- open questions の JSON DTO、bounded context visibility、金額表現はこの decision で close する
- abstract zero value は言語制限を導入しない運用決定として close し、将来の lint/制限は具体需要待ちとする
