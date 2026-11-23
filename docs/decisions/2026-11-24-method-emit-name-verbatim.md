# `@method(GoName)` — verbatim emit name for Go interface methods

- 状態: 採用（実装・検証した — wave38 lane G）
- 日付: 2026-11-24
- 関連: [go-iface-impl](2026-11-21-go-iface-impl.md)、[emit-name-collision-check](2026-11-23-emit-name-collision-check.md)

## Context

`@method` の Go メソッド名は `core.ExportName` で導出されるため、
`serve_http` は `ServeHttp` までしか出せない。`ServeHTTP`・`URL`・
`ID` のような initialism を含む名前は生成できず、record を
`http.Handler` や `sort.Interface` などの interface に適合させられ
ない（E0401）。

## Decision

属性引数で verbatim 名を指定する: `@method(ServeHTTP)`。

- 構文は `@method` に限る任意引数 `( UIdent )` — 大文字始まりの
  識別子のみ受理（それ以外は E0101）。Gom 側の関数名は小文字の
  まま変わらない。
- `syntax.FnDecl.MethodName` → `check.FnDecl` → `core.FuncDecl` と
  伝搬し、`GomMethod.Name`（interface 適合の照合名）と
  `codegen.methodDecl` の emit 名の両方で verbatim 名を使う。
  未指定時は従来どおり `core.ExportName(sym.Name)`。
- `.gomi` は `@method(GoName)` 行として往復する
  （iface/draft.go が引数付きで書き出す）。

Rejected alternatives:

- **initialism 辞書を camelizer に組み込む** — `user_id` → `UserID`
  のように全 emit 名が黙って変わり、既存の生成 API を壊す。
  opt-out もできない。
- **verbatim な fn 名**（`fn (h: H) ServeHTTP`）— Gom の関数名規則
  （小文字 ident）を破り、呼び出し側の見た目も不自然になる。

## Verification

`@method(ServeHTTP) fn (h: Handler) serve_http (w: http.ResponseWriter)
(r: Ptr http.Request) -> Unit` が `func (h Handler) ServeHTTP(...)` を
emit し、`Handler` が `http.Handler` に適合することを probe で確認。
spec fixture: `ok_go_method_verbatim.gom`（適合込み）、
`E0101_method_arg_lowercase.gom`（非 UIdent 引数の拒否）。
