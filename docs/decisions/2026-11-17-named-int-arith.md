# Named Go 整数型の算術・比較（`30 * time.Second` が `Duration` を生む）

- 状態: accepted
- 日付: 2026-11-17
- 問い: wave-15 lane 3 の verified residual — `http.Client` の `Timeout` が `30 * time.Second` と綴れず、`NewClient` factory 除去の残り半分
- podhmo 決定（2026-11-17）: (a) 方向で良いが、現状を pin してから確定

## 現状 pin（実施済み）

- `time.Second` は goimport で named Go 型 `time.Duration`（`*types.Go{Ref}`、underlying `int64`）として surface する — Int 互換ではない
- `30 * time.Second` は E0401「expected a numeric type, got time.Duration」×2 — untyped literal は named 型に conform しない
- `a + b`（Duration × Duration）も同じ E0401、`a + n`（Duration × Int）は unify 失敗で「expected time.Duration, got Int」
- literal の range 判定は `litRange`/`goLitRange` が既に UnderlyingSource oracle を引く（`Go.conv` operand 用に実装済み）

## 選択肢

- **(a) named-int 算術 admit（採用）**: `satisfies` の num/intg/ord 判定に「`*types.Go` + oracle の underlying basic kind」を加える。両 operand が同じ named 型なら演算可・結果もその型、untyped literal は operand の named 型へ conform — Go の規則と同型
- **(b) literal のみ admit（非対称）**: **却下** — `30 * time.Second` は通るが `deadline - now` や `a + b`（named × named）は残る非対称で、wave-15 残件（Duration の API 結果の演算）を解かない
- **(c) 変換 API のみ**: **却下** — 現状どおり `Go.conv` を義務付けると `30 * time.Second` が `Go.conv 30 : time.Duration * time.Second` と書けない美しさで、Go 利用者の期待（untyped const conform）とずれる
- **(d) 現状維持**: **却下** — `Timeout` など Duration を要する Go API が Gom だけでは書けず、ホスト factory を残し続ける

## 決定

- underlying が整数種（`Int`/`Int8..64`、`Uint`/`Uint8..64`、`Uintptr`）の named Go 型は `num`・`intg`・`ord` を満たす — `+ - * / %` と `< <= > >=` が使える
- underlying が浮動小数点種（`Float32`/`Float64`）の named Go 型は `num`・`ord` を満たす
- `d / 0` のような定数ゼロ除算は underlying に従い Go と同じ E0414（`revisitDivs` も named 判定を引く）
- named 型と組み込み数値型・別 named 型との混在はこれまでどおり E0401 — `unify` 不変（型を増やさず、制約の判定だけを広げるので additive）
- 判定の oracle は `UnderlyingSource`（`Go.conv` operand の range 判定と同じ仕組み）— source を実装しないチェッカでは named 型は従来どおり非数値
- sized int（`int64` 等の basic Go 型）は対応表ですでに builtin 数値型に写るので**別問いとしない** — 同じ枠に入れるべきは named 型だけだった
- `==`/`!=` は `eq` 判定が宣言展開で既に named 型を扱うので変更なし
- `Float`（float64 しか存在しない）との混在 `d + 1.5` も型なし float literal の conform で通る — Go と同じ

## 影響

- `30 * time.Second` が `time.Duration` を生み、`deadline - now`（`time.Until`/`Sub` の返り値）が算術に載る — wave-15 lane 3 の `NewClient` removal condition の残り半分が消える（残るは callback interop = lane 2 着地済み、struct 構築再検討の前提が揃う）
- 生成 Go は `30 * time.Second` をそのまま出力 — Go の untyped-const 規則と同じで変換コード不要
- spec.md §3.3（算術の overload 拡大）・§8.3（named type 行に算術可の注記）
- spec テスト: `ok_named_int_arith`（named×named、named×literal、比较）、`E0401_named_int_mixed`（混在は正直なエラー）、`E0414_named_int_divzero`
- golden `named_int_arith.golden.go` — `30 * time.Second`・`a + b`・`deadline - now`・`a > b` の emissão
