# extern メンバの escape directive は `.gomi` 再生成で引き継ぐ

- 状態: 採用
- 日付: 2026-11-25

## 文脈

`//gom:noescape` / `//gom:result-notaint` は、パッケージ内で推論した
escape / result-taint 情報を import 側へ伝える `.gomi` directive である。
本体のある Gom 宣言なら `gom info` が再生成のたびに推論結果から書き直せるが、
`@extern fn` や関数型の `@extern let` には検査できる Gom 本体がない。
このため `stdlib/list` の extern 関数に与えた directive が、既存 `.gomi` を
入力に `gom info` を再実行すると消えていた（implementation-gaps 登録済み、
wave47 lane C）。

## 決定

`gom info` の再生成では、directive の出所を本体の有無で分ける。

- 本体のある `fn` / `let` は常に現在の本体から推論し直す。既存 `.gomi` の
  directive は引き継がない — 実装が escape / taint するよう変わったあとに
  stale な保証を残さないため
- `@extern fn` と関数型の `@extern let` は本体を検査できないので、既存
  `.gomi` の同名メンバの `//gom:noescape` / `//gom:result-notaint` を
  再生成後のシグネチャへ引き継ぐ。ただし引き継ぎ元は同じ package directory の
  interface に限る — `gom info -o` で別 directory の draft を seed に使っても
  保証を移植しない。同名 package であっても directory が違えば Go では別
  package である。さらに directive の index は位置への
  主張なので、canonical なシグネチャ文言が変わったメンバでは引き継がない
  — パラメータの並べ替えや型修正で古い位置の保証が別の入力へ移るのを防ぐ。
  無変更のときも index は現在の flat パラメータ数の範囲に限定し、範囲外と
  重複は落とす
- メンバ自体が再生成 draft から消える場合は directive も残さない。
  abstract/private 化の意図保持はこの決定の対象外

これは `//gom:mutable` の引き継ぎ規則と矛盾しない。`//gom:mutable` も
著者の意図表明で推論元がないため、同じ package の中では無条件に引き継ぐ。
一方 noescape / result-notaint は本体から推論できるときは推論を正とし、
extern のように推論元がないときだけ既存の表明を引き継ぐ。package directory が違う
seed からは mutable を含めて一切移植しない。

## 却下した案

- 全メンバの directive を無条件に引き継ぐ → 本体のある関数が escape /
  taint する実装に変わっても古い保証が残り、import 側の E0419 判定を
  誤らせる
- 何も引き継がない → extern 実装の保証を書けない。stdlib/list の
  `take` / `sort_by` / `filter_map` / `find_map` の directive が再生成で
  消える現行 bug のまま
- `.gom` の `@extern` 属性引数に escape 情報を足す → 正しくはあるが新しい
  言語面であり、既存 `.gomi` directive の信頼モデルで足りる。wave47 lane C
  の scope 外

## 検証

prototype 記録: [wave47 lane C .gomi directive verify](../sketch/2026-11-25-wave47-lane-c-gomi-directives-verify.md)。

- extern fn / extern let の directive が再生成後も残り、本体のある stale な
  directive は残らず、extern のパラメータを並べ替えた signature では位置の
  保証を引き継がないことを `TestInfoCarriesExternEscapeDirectives` で固定。
  `gom fmt` 済みの `let x: T` 形式も canonical 化して同じ signature と
  見なす
- `stdlib/list/list.gom` と既存 `stdlib/list/list.gomi` のコピーに対する
  `gom info` 再生成が no-op になることを確認
