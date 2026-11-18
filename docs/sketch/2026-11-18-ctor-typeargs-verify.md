# CtorApp/CtorRef TypeArgs 検査 — 検証メモ（wave21 lane1）

decision: [ctor-typeargs-authoritative](../decisions/2026-11-18-ctor-typeargs-authoritative.md)
problem: [ctor-typeargs-unvalidated](../problems/2026-11-02-ctor-typeargs-unvalidated.md)

## 実装

`internal/core/validate.go` に `ctorTArgs` を追加。CtorApp/CtorRef で
`TypeArgs` が非空のとき、結果型が chase して辿る `Con` の `Args` と
要素ごとに `types.Equal` で一致を要求する。結果 Con は `resultCon` が
返す — CtorApp では `Typ` 自身、CtorRef では関数型の最終戻り値まで
矢印チェーンを降りる（ネストした `*types.Func` も辿る）。`TypeArgs`
欠損は合法（codegen `ctorTArgs` のフォールバック経路）なので
「存在して食い違う」形だけを violation にする。

個数（宣言 arity との一致）は従来どおり `targs` が担う — ファイル内・
unit sibling の宣言がある場合のみ。imported ctor は宣言 arity を
引けないが、結果型との完全一致で同じ穴は塞がる（decision の
却下した案に記録）。

## 検証

- `TestCtorTypeArgs`（internal/core/validate_test.go）: 合法 3 形
  （TypeArgs 欠損・一致・CtorRef のネストした矢印チェーン越しの
  一致）と違反 4 形（要素不一致・個数不一致・非 ADT 結果・CtorRef
  不一致）を pin。
- `go test ./...` 全緑 — testdata/golden・gomtest の全 fixture が
  実経路の lowered IR を通るため、「lower が emit する TypeArgs が
  結果型と必ず一致する」ことの回帰確認にもなる（偽陽性なし）。
- `gofmt -l .` 空、`go vet ./...` 通過、`scripts/check-sumtype.sh`
  通過（`resultCon` は部分 switch を避け if-assert 連鎖で記述）。

## 補足

`TypeArgs` を持つノードは他にもある（`CallDirect`/`FuncRef`/
`GoFuncRef`）が、本 lane の対象は ctor のみ — 関数シンボルの
TypeArgs は結果型から読み直せない（戻り値は引数を含まない）ため
同じ規則は適用できない。
