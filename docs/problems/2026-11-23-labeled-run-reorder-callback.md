# labeled run の並べ替え受理と位置発行の矛盾

- 状態: 未解決
- 発見: wave36 lane B の Devin Review finding

## 状況

spec 2.1 は連続 labeled run の型等価を「並べ替えを許す」と規定し、
`unifyFunc` の run-pairing もラベル名で対を取る。しかし labeled
呼び出しの発行は callee の「宣言された」パラメータ順で位置引数化
するため、高階経路で値が入れ替わる:

```gom
fn apply (f: ~a: Int -> ~b: Int -> Int) -> Int = f ~a: 1 ~b: 2
fn cb (~b: Int) (~a: Int) -> Int = b - a

let _ = apply cb
```

`apply` 内の `f ~a:1 ~b:2` は `f(1, 2)` に発行されるが、`cb` の
宣言順は `(~b, ~a)` — 実行時に `b=1, a=2` と交差する。型レベルの
等価は正しくても発行が一致しない。

## 選択肢

- 案A: spec の「並べ替えを許す」を縮小し、run は宣言順一致を要求
  （unifyFunc の pairing を位置対応に）。`~b ~a` と `~a ~b` は
  別型になる
- 案B: 受理は維持し、並びが違う produced を渡す境界で η 展開の
  並べ替え closure を発行（`ArgToParam` 系の発行機構の拡張）

## 影響範囲

`fn` 宣言を渡す高階呼び出しでラベル順が食い違うケースのみ。
`fn ~x` ラムダ構文（wave36 lane F）が来ると到達面が広がる。
