# Go struct の構築・書き込みは Go 側の factory 関数に限る

- 状態: 採用
- 日付: 2026-11-17
- 問い: [open-questions](../open-questions.md)「Go の struct 型の構築・書き込み」
  — `http.Server{Addr: ...}` のようなコンポジットリテラルや field への代入を
  許すか、構築は Go の factory 関数・extern に限るか

## 決定

v1 では Go struct の構築・field 書き込みの言語面を導入しない。構築は Go 側の
factory 関数（`extern` や `import go` 経由の普通の関数）に限る — 選択肢 (c)、
現状の constructor-extern 慣行を決定として記録する。`http.Client` のような
値は引き続き host 側の小さな factory で作る。

## 選択肢

検証対象は gist-cli が抱える 2 つの adapter — `NewClient`
（`http.Client{Timeout: 30*time.Second, CheckRedirect: ...}`）と
`StringField`（`json.Unmarshal` の decode destination）。

- (a) コンポジットリテラル構文 — `http.Client{Timeout: ..., CheckRedirect: ...}`
  に相当する Go-scoped の record 構文。syntax→resolve→check→lower→codegen の
  全面更新 + Go の partial 初期化・embedded field・`omitempty` 相当の規則を
  定義する必要がある。最大の surface
- (b) field 書き込みのみ — `Ptr` / addressable な値への `v.Field = x`。
  addressability 規則が要る。`Timeout: 30*time.Second` は `time.Second` を
  掛ける Duration 算術で、func-typed field（CheckRedirect）は Gom の関数値を
  Go の func 型へ渡す interop が要る — どちらも field 書き込みでは解けない
- (c) constructor-extern — 構築は Go 側に置く。新しい言語面ゼロ

## 調査結果

`StringField` は構築面なしに Gom で書けた — decode destination は
`Ptr.new ()` で作る `Ptr T` を `json.Unmarshal` / `Decode` の `any`
パラメータにそのまま渡せ、`Go.Map.get`・`errors.Is`・`Go.conv` で残りも揃う
（[sketch](../sketch/2026-11-17-struct-construction.md) — prototype 節）。
gist-cli 側に `string_field` として移し、`host.StringField` は削除した
（integration test 緑のまま）。

`NewClient` はどの構築面を足しても届かない:
- `CheckRedirect` — func-typed field への供給は Gom → Go func interop が要る
  （未決・別の問いとして open-questions に残す）
- `Timeout: 30 * time.Second` — `time.Duration` の乗算は現行の数値面では
  綴れない（`time.Second` は int64 の typed 定数であり Gom の Int 算術は
  Duration 型を生まない）

検証済みの障害が 0 件の (a) を全面実装する根拠がなく、(b) も 2 つの障害を
解かない。(c) を決定とする。

## 影響

- spec 8.3 節: 「まだ定めない」の記述を「構築は Go 側の factory 関数に限る」
  に更新
- open-questions: 本問を解決済みとして除去。残差「Gom 関数値を Go の func
  型として渡す方法」を別項目として追加
- 境界の別名・可変性（[sketch](../sketch/2026-11-04-boundary-aliasing-hypotheses.md)）:
  Go が作った struct を Gom 経由で Go に戻すのは既存の所有規則そのまま。
  本決定は Gom 側に構築面を持たないため新しい共有パターンを生まない
