# checker は整数リテラルの範囲を検査しない — 受理したプログラムの生成 Go が build できない

- 状態: 未解決
- 日付: 2026-11-05

Found while verifying width-aware eval arithmetic
([decision](../decisions/2026-11-05-eval-int-width-aware-ops.md),
[sketch](../sketch/2026-11-05-eval-int-width.md)).

## What happens

check は整数リテラルに静的型の範囲検査をしないので、型の範囲外の
スペリングを受理し、codegen はそのリテラルをそのまま出力する。
Go は型付き定数変換の範囲外リテラルを拒否するので、生成コードは
`go build` できない:

```gom
let n : Int32 = 3000000000
// -> var n int32 = 3000000000
// go build: cannot use 3000000000 (untyped int constant) as int32 value (overflows)

let x = 99999999999999999999
// -> var x int64 = 99999999999999999999
// go build: constant overflows int64
```

per-operation 決定（定数畳み込み回避）がこの穴を「結果は範囲内でも」
広げる: 左被演算子を変数化すると、右のリテラルは単独で被演算子型に
変換される必要がある —

```gom
let k : Int32 = 0 - 2147483648
// -> tmp := int32(0); return tmp - 2147483648
// go build: 2147483648 (untyped int constant) overflows int32
```

Go 自身は `var k int32 = 0 - 2147483648` を受理する（定数式全体を
評価してから範囲判定する）のに対し、Gom の出力はリテラル単独の
変換を要求する場所を作る。

## What spec does not say

spec 3.3 は演算の wrap-around を定めるが、リテラルの範囲について
書かれていない。解消方向は少なくとも 2 つある:

1. **Go の定数モデルに揃える** — リテラルは静的型の範囲に収まる
   ことを要求し、check が範囲外のスペリングを診断する
   （`-9223372036854775808` のような「単項 - の被演算子としてだけ
   範囲外」の形は unary 込みで判定）。`0 - 2147483648 : Int32` は
   この読みではリテラル位置の `2147483648` が違反になるか、
   定数式の最終値で判定するかでさらに揺れる
2. **wrap させる** — リテラルは `int32(text)` の変換意味で実体化
   する。生成側は `int32(int64var(3000000000))` 相当の非定数化が
   必要。eval はこの読みを既に実装している（VInt は常に静的型幅で
   canonical — [decision](../decisions/2026-11-05-eval-int-width-aware-ops.md)）

いずれにしても「受理するが生成コードが build できない」現状は
契約違反（clean 実行が不可能な divergence — 以前の
`9223372036854775807 + 1` 定数オーバーフローと同型）なので、
spec の一文と check/codegen 側の対応が要る。

## Interaction with eval

eval は上記 (2) に相当する実体化を行う（`litValue` が Int32 型の
リテラルを `int64(int32(text))` に丸め、int64 を超えるマグニチュードは
uint64 経由で wrap）。範囲検査が入った場合は到達不能の防御分岐に
なる; wrap が規範になった場合はそのまま正規の意味になる。
