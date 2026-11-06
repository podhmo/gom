# nil-able interface pairs: end-to-end verification

- 日付: 2026-11-06
- 状態: verified — 派生・宣言・境界検査の 3 経路すべてで確認済み

## スコープと結論

[decision](../decisions/2026-11-06-nilable-iface-pair-ok-payload.md)
の検証。`(error, error)` で確立した規則 — 型由来ビューは正直に、
厳格な主張は宣言して境界で検査する — を nil 許容 interface 全般
(`net.Error`、`io.Reader`、ユーザ定義 interface、`any`、named な
空 interface) に広げた。

## 変更点

| 箇所 | 内容 |
| --- | --- |
| `internal/goimport/mapping.go` | `methodIface(t)` ヘルパー (underlying がメソッドを持つ interface)。`(I, error)` で I がメソッド持ちなら派生 `Result (Option T) GoError` (旧来の `isError(t0)` 分岐を包含)。`GoSig.ResNilable` を設定 |
| `internal/goimport/extern.go` | `nilablePresence` を `*T` または interface underlying へ拡大 (`any`・named 空 interface も含む)。lone result `Option T` の許可を interface まで拡大。`BindExtern` が `ResNilable` を登録 |
| `internal/lower/lower.go` | `GoSig.ResNilable` フィールド |
| `internal/core/call.go` | `CallGo.ResNilable` フィールド (Core IR が境界の事実を運ぶ) |
| `internal/lower/expr.go` | `payloadNilGuard` を一般化 — `ResNilable && payload が verbatim (Option でない) && Equal(payload, call.Typ)`。`ResultFromGo`/`EscapeCall` の両経路で共有。全 `CallGo` 構築点で `sig.ResNilable` を転送 |
| `internal/codegen/emit.go` | panic 文を "never-nil GoError payload" から "never-nil payload" へ一般化 |
| `internal/check/pat.go` | ctor パターンのインスタンス化変数が `Go.Any` want で束縛されず E0407 に残る穴を修正 (`unify` の `isGoAny` 受理ルールは any の引数側を意図するもので、scrutinee 由来の want には適さない — 照合後に位置対応で bind) |

## 検証結果

- **`(net.Error, error)` 派生**: `goiface.NetErr` が `Result (Option net.Error) GoError` を導出。golden `iface_pair_views.golden.go` で `result.FromGo(option.FromGo(v, v != nil), err)` を確認。
- **ユーザ定義 interface**: `goiface.TagOr` (`Tagger`) も同形に派生。
- **`(any, error)` / named 空 interface**: verbatim `Result Go.Any GoError` / `Result goiface.Empty GoError` を保持 — `(nil, nil)` は `Ok <nil>` (eval/compiled 比較 `iface_pair_views` で "ok" を確認)。
- **strict claim**: `Result NetError GoError` over `NetErrNil` が `(nil, nil)` で `err == nil && v == nil { panic("gom: goiface.NetErrNil returned (nil, nil) — its binding declares a never-nil payload") }` を生成。`TestNeverNilIfacePayloadPanic` が eval (E9003) と compiled panic の両側で契約違反を確認。`try` 経由も `v == nil` panic が err 脱出の直後に出る (golden)。
- **lone interface `Option`**: `Option Tagger` / `Option Empty` extern が `option.FromGo(v, v != nil)` にマッピング (golden + extern tests)。
- **eval 一致**: compare ケース `iface_pair_views` が `"timeout:none:err:tag:none:ok:ok:some:none"` — evaluator (host stub) とコンパイル済みバイナリが一致。
- **extern テスト**: `Result (Option NetError) GoError`、`Result NetError GoError`、`Result (Option Tagger) GoError`、`Result (Option Empty) GoError`、`Option Tagger`、`Option Empty` が受理されることを `TestExternIfacePairViews`/`TestExternEmptyIfacePairOptionView`/`TestExternLoneIfaceOptionView` で確認。スライス・マップ等は依然拒否。

## 補足

`Go.Any` はソース上で綴れないので、`Option` over `any` や
`Result Go.Any GoError` の宣言ビューは書き方として到達しない —
admission は実装されているが、ユーザは派生 verbatim ビューと
`Result (Option NetError) GoError` のような alias 経由の宣言を
使う。`goiface.NetError = net.Error` alias はそのために追加した。
