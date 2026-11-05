# eval の整数演算は演算位置の静的型の幅で計算する（VInt は幅を持たない）

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-eval-int-width.md)）
- 日付: 2026-11-05

## 決定

評価器（internal/eval）の整数演算は、演算ノードが持つ静的な被演算子型
から幅を引いて実行する。`VInt` は引き続き 64 ビットの `N` だけを持ち、
値自体は幅を持たない。代わりに **不変条件として「型が定める幅の範囲に
収まった VInt だけが評価器を流れる」ことを保つ**:

- `check.Binary` の整数演算（`+ - * / %`）と比較（`< <= > >=`）は、
  被演算子型 `x.X.Type()` が builtin `Int32` のとき 32 ビットで計算する
  — 被演算子を正規化してから int64 で演算し、結果を `int64(int32(n))`
  に丸める（符号拡張）。`Int` / `Int64` はそのまま 64 ビット
- `check.Unary` の `-` は被演算子型が `Int32` のとき結果を 32 ビットに
  丸める
- `check.Lit` / `check.LitPat` の整数リテラルは、リテラルの型が `Int32`
  のとき `int64(int32(text))` に丸めて実体化する
- `==` / `!=` は値レベルの構造比較のまま — 上の不変条件のおかげで
  幅を知らなくても正しく比較できる

結果として `MinInt32 / -1` は `-2147483648`、`MinInt32 % -1` は `0`、
`-MinInt32` は `-2147483648`（いずれも Go の int32 実行時意味と一致、
2 の補数 wrap）。幅の参照に localT ガードを入れ、ファイル内で宣言された
`type Int32`（builtin を隠す ADT）の Con を幅判定に使わない。

## 背景

spec 3.3 は整数演算を「64 ビット（`Int32` は 32 ビット）の 2 の補数
wrap-around」と定める（[numeric-arithmetic-per-operation](2026-11-05-numeric-arithmetic-per-operation.md)）
が、eval の `VInt` は常に int64 で、`l.N + r.N` をそのまま返していた。
`fn f (x: Int32) -> Int32 = x + 1` を `2147483647` に適用すると eval は
`2147483648`、生成 Go（`int32`）は `-2147483648` を返すずれがあった
（[台帳](../implementation-gaps.md)。生成コード側は spec 通り）。

選択肢は 2 つあった:

1. **演算位置で幅を引く（採用）** — 評価器は check の型付き AST を
   辿るので、各演算ノードは統一済みの被演算子型を持つ
   （checkArith が両オペランドを unify する）。意味は spec が置く場所
   そのもの — 演算ごとの機械型 — に実装でき、VInt の構築点を増やさない
2. **VInt に幅/kind を持たせる** — 値が自己記述的になるが、すべての
   VInt 構築に型が要り、環境・クロージャ・ホスト関数経由の値にも
   幅を伝播させる必要がある。幅を持っても表示・等価の読みは変わらない
   （canonical な値同士なら int64 比較で正しい）ので、重いだけ

(1) を採る。被演算子の正規化（入力側の丸め）で、ホスト関数や
ハンドビルド AST から来た非 canonical な VInt も演算位置で吸収する。

## 併せて直ったずれ

- `let m : Int64 = -9223372036854775808` — 内側のリテラル
  `9223372036854775808` が ParseInt の範囲外で eval は E9004「invalid
  int literal」を返していたが、生成コードは `var m int64 =
  -9223372036854775808` で正しく `MinInt64` を返す。リテラル実体化を
  「int64 範囲を超えたら uint64 として読んで 2^64 wrap」に変えて修復した

## 非対象・見つかった別のずれ

- `Char` は数値型でない（`num` 制約が拒否する — `c + 1` は E0401）ので
  Char の演算は存在しない。比較のみで幅の問題は起きない
- `Int32` と `Int` の混合演算は checker が unify で拒否する（E0401）ので
  被演算子の幅が食い違う式は到達しない
- checker は整数リテラルの範囲検査をしない — `let n : Int32 =
  3000000000` は受理され、`var n int32 = 3000000000`（build できない
  Go）が生成される。別のずれとして台帳に記録する。eval 側はこのような
  リテラルを幅に丸めて実体化する（上記の不変条件を保つため）

## 実装

`internal/eval` のみ:

- `lit.go`: `isInt32Con`（`isFloatCon` と同じ名前判定 —
  数値リテラルがファイル内宣言の同名 ADT を型に持つことはない）、
  LitInt を「型幅に丸めて実体化」に変更。ParseInt の fast path を残し、
  範囲外は ParseUint→wrap で `MinInt64` 系を拾う
- `eval.go`: `wrapTo(width, n)`（32 のとき `int64(int32(n))`、64 は
  恒等）と `intOperandWidth(t)`（builtin Int32 なら 32、他は 64。
  localT ガード付き）。`evalPrim` の VInt 分岐で被演算子を正規化して
  int64 で演算→結果を wrap、比較は正規化した被演算子どうしを比較。
  `evalUnary` の UnNeg は結果を wrap。ゼロ除数判定は正規化後の値で行う

## 検証

- `docs/sketch/2026-11-05-eval-int-width.md` に検証記録
- `internal/build/compare_test.go` に差分ケース（`int32_width_arith`）
- `internal/eval/eval_test.go` に Int32 演算・リテラルのユニットテスト
