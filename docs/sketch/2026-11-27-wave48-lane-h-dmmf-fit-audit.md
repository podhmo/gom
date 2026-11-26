# wave48 lane H: PR #259 DMMF fit report の精査

- 日付: 2026-11-27
- 対象: [PR #259](https://github.com/podhmo/gom/pull/259) `docs/sketch/ja/2026-11-26-dmmf-order-taking-fit.md`（DMMF OrderTaking 適合性レポート）
- 基準: PR #259 マージ後の main（`bbd7f27`）
- 方法: レポートの優先度 A〜C と「追加で見えた不足」を spec / stdlib / TODO / open-questions / implementation-gaps / decisions と突き合わせた。`Result.to_option` の存在と `internal/` layout の positive probe も確認した

## 判定表

| 項目 | 判定 | 台帳への反映 |
| --- | --- | --- |
| A1 record literal 推論 / 型名前置 literal | 有効。既存 open question の最重要 evidence として再確認 | 既存 open question へ DMMF evidence を追記 |
| A2 stdlib Result/Option helper | 一部誤り。`Result.to_option` は既存。`Result.sequence` / traverse 系の不足は有効 | 新規 open question。report 本文を訂正 |
| A3 bounded context package 設計 | 有効。ただし「`internal` 相当がない」は不正確で、Go の `internal/` layout は package 境界として使える。型 module / package 内限定公開がないという問いに修正 | 新規 open question。report 本文を訂正 |
| A4 JSON tagged union / nullable DTO | 有効。言語機能より先に推奨 DTO 形を決める論点 | 新規 open question |
| A5 abstract 型と zero value / boundary | 有効。`Ptr.new` / `encoding/json` は smart constructor を通らない | 新規 open question |
| B6 panic capture adapter | spec は言語 `recover` を提供しないが、Go helper で boundary adapter を作るかは別問い。形状と置き場所が未決 | 新規 open question |
| B7 金額 / exact decimal | 有効。DMMF 移植前に integer minor units か外部 decimal かを決める必要がある | 新規 open question |
| B8 E0301 text の不足パターン | 既存 gap/open question と同じ。DMMF 進化テストで重要度が再上昇 | 既存 gap/open question へ追記 |
| C9 constructor 名の型スコープ化 | 現行は `Type.Ctor` 参照があり、宣言側の flat term namespace は既 decision。命名規約で回るという report 自身の評価どおり需要待ち | 登録しない |
| C10 等価性の禁止 / custom equality | `deriving` 見送り decision と需要不足の範囲。DMMF でも代替可能 | 登録しない |
| C11 Applicative validation 糖衣 | report 自身が「fail-fast 主軸なら不要」と評価。具体需要がない | 登録しない |
| X1 constructor 名衝突 | C9 と同じ根 | 登録しない |
| X2 zero value / direct JSON / reflection bypass | A5 に畳み込む | A5 の open question に含める |
| X3 Go export fn の `(T, error)` 規約 | アプリケーション側の公開面規約で、gom 本体の未決事項ではない。`Go.FuncN` / `@method` は既存 | 登録しない |
| X4 E0301 の人間向け出力 | B8 と同じ | 既存項目へ追記 |
| X5 `&&` / `||` の eval/compiled ずれ | correctness bug として実施可能。spec 明記も必要 | TODO へ登録 |

## 再確認した点

### `Result.to_option` は存在する

spec §5 と [stdlib-conv-placement](../decisions/2026-11-18-stdlib-conv-placement.md) の正では、`Result.to_option : Result a e -> Option a` と `Option.to_list : Option a -> List a` は compiler-owned forwarding conversion として存在する。一方 `stdlib/result/result.gom` と `stdlib/option/option.gom` には wave21 以前の「存在しない」というコメントが残っていた。PR #259 はこの stale なコメントに沿って `Result.to_option` を不足扱いしていた。

この lane では report 本文を訂正し、stdlib 側の stale コメントも現行配置に合わせて修正した。残る不足は `Result.sequence` / `List.traverse_result` / Option を Result へ畳む traverse 系であり、`bind_option` という名前自体は採用が決まっていない。

### `internal/` layout は package 境界として使える

`/tmp/gom-internal-probe` で `example.com/root/internal/shared` を同一 module の `main` から import する probe を作り、`gom build main/main.gom` と `go build ./...` が通ることを確認した。したがって「bounded context の外部隠蔽に使える仕組みがまったくない」ではない。足りないのは、F# の型名 module 相当の namespace と、同一 package 内だけで共有する細かい可視性である。設計ガイドの問いは package 分割・`internal/` layout・`string50_create` 命名の組み合わせに修正する。

### constructor 名衝突は新規の未決事項ではない

現行の name resolution decision は、constructor をファイルの term scope と型ごとの constructor table の両方に束縛する。`Type.Ctor` による限定参照はあるが、宣言側の同名衝突は flat term namespace で E0202 になる。DMMF では F# 風の型スコープがあると楽だが、report 自身も命名規約と package 分割で回る優先度 C としている。既 decision を覆す具体需要がないので open question へは戻さない。

### `&&` / `||` は TODO 化できる

evaluator は短絡し、生成コードは右辺を先に atom 化する。言語として短絡しない選択には利用者向けの利点がなく、Go の `&&` / `||` とも逆なので、設計の分岐というより spec 明記を伴う correctness 修正として扱う。wave49 以降の実作業として TODO に登録した。

## この lane での台帳反映

- TODO: wave48 lane H を追加。`&&` / `||` の生成コードを短絡評価に揃える task を追加
- open-questions: Result/Option traverse helpers、bounded context package/visibility guide、exact decimal、JSON DTO、abstract 型の zero value/boundary、panic capture adapter を追加。record literal E0407 と `labels` / `details` の既存項目へ DMMF evidence を追記
- implementation-gaps: E0301 と `&&` / `||` の既存項目を更新。stdlib コメントの stale さを解消済み項目として登録
- problems: 追加なし
- report / stdlib comments: `Result.to_option` の誤り、`internal` visibility の表現、stdlib の変換コメントを訂正

## 次への入力

wave49 prompts では、少なくとも次を lane 候補として扱える。

1. `&&` / `||` の短絡修正（TODO 化済み、correctness）
2. record literal E0407 の決着（DMMF で最大の摩擦。設計重め）
3. Result/Option traverse helpers（決定は名前・配置・引数順）
4. E0301 `labels` / `details`（進化テストの DX。設計重め）
5. JSON DTO / abstract invariant / bounded context guide のどれかを先に決めるか（ドメインモデリング用途の整備）
