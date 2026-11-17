# .gomi の多相 let

- 状態: 採用
- 日付: 2026-11-18

## 決定

`.gomi` の `let` シグネチャは型変数を含んでよい。`let empty : List a` は
一般化された値を公開し、実装側の `.gom` が一般化に至る `let`
（`let empty : List a = []` が emit する niladic ジェネリック関数
`func Empty[T]() T`）と同じ形を期待する。v1 で採った E0602 で型変数を
拒否する bounded 形は撤去する。

## 理由

一般化した `let` は Go 側では `var` ではなく thunk（`func X[T]() T`）
として出力される（[let-polymorphism-emission](2026-11-02-let-polymorphism-emission.md)）。
`.gomi` の member table が `let` メンバーを `var` 参照として運ぶ限り、
多相 let は「適合は通るが参照の emit が正しくない」形だったので、
v1 は parser が型変数を拒否していた。member table 側に thunk 形を
表現する機構さえあれば bounded 形を残す理由はない。

## 採った形

thunk oracle を import 越しに運ぶのは `resolve.Symbol.Thunk` フラグ
1 本:

- gomimport の `extract` は、型変数を含む `let` シグネチャのメンバーを
  `KindFunc` + `Thunk` として mint し、シグネチャを `() -> T`
  （`TypeParams` は出現順）で登録する
- check の `valOf`（thunk シグネチャの `() -> T` を Ret で返す判定）、
  lower の `funcRefOrFlatten`（`x[T]()` を emit する判定）、lint の
  `go` ブロック捕捉判定は、ローカルの `thunks` マップと `sym.Thunk`
  の両方を oracle にする
- iface の適合は無変更 — impl 側の thunk FnDecl は既に
  `let x : T` のメンバー面に正規化される（iface.Surface /
  `gom info` の drafter は Thunk を `let` として表示する）

## 却下した案

- **(b) bounded 形を維持** — member table に thunk 表現を入れないまま
  E0602 で閉じる案。stdlib の `.gomi` に多相 `let` が欲しい実例は
  現状ない（全公開メンバーは `fn`）が、oracle の伝搬は Symbol フラグ
  1 本 + 判定 3 箇所の OR で済むと読み見で分かり、bounded 形を残す
  コスト（言語の非対称・pin fixture の維持）に対して実装が軽かった。

## 検証

- `testdata/spec/ok_gomi_poly_let/` — `.gomi` の `let empty : List a`
  と実装の thunk が適合する（diagnostics なし）
- `testdata/multifile` — `shapes.empty` を `List Int` / `List String`
  の両方に instantiate して値として渡す end-to-end
  （`go run ./gomapp`）
- `TestParseInterface` — 型変数を含む `let` シグネチャが受理される
