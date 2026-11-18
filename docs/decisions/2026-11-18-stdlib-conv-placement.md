# stdlib 変換関数の配置 — 下層モジュール名・上層パッケージ実装

- 状態: 採用（wave21 lane 2 で実装・検証した — spec fixture・golden・
  Go 単体テストが pin。[検証](../sketch/2026-11-18-stdlib-conv-verify.md)）
- 日付: 2026-11-18

## 問題

`Result.to_option`（`Ok x → Some x`、`Err → None`）と
`Option.to_list`（`Some x → [x]`、`None → []`）を stdlib に置きたい。
層は `result < option < list`（[stdlib-packaging](2026-11-03-stdlib-packaging.md)）
で、Go の import も同じ向きの DAG である。

どの層でも自然な置き場所がない:

- **下層パッケージに実装**（`result` パッケージが `to_option` を持つ）:
  `result` が `option` を import する必要があるが、`option` は
  `to_result` のためすでに `result` を import している。Go の
  循環 import になる。
- **上層モジュール名で公開**（`Option.to_option` のように
  変換 *先* のモジュールに載せる）: ユーザーが書く名前が
  変換元の型ではなく変換先の型のモジュールになり、
  `Ptr.to_option`（`Ptr a -> Option a`）や `Go.Slice.to_list`
  など「`X.to_y : X -> Y` は `X` モジュールに載る」という
  既存の一貫した面を裏切る。
- **`to_option` を result.gom に書く**: 上と同じ循環。
- **中間の第 3 パッケージに両方を置く**: 公開モジュール名が
  `Result`/`Option` ではなくなり、こちらも面の一貫性を壊す。

## 採択（wave21 lane 2 実装中に記録 — prompts の (a) 案の具体化）

**メンバー名は下層モジュールに、実装は上層パッケージに置く。**

`Array.to_list` → `stdlib/list.ArrayFreeze` の既存の先例と同じ
`builtinMember.pkg` 上書きパターン:

- `Result.to_option : Result a e -> Option a` — Result モジュールの
  メンバーとして登録し、`stdlib/option.ResultToOption`（手書き Go、
  `stdlib/option/conv.go`）に forward する。`option` はすでに
  `result` を import しているので DAG に沿う。
- `Option.to_list : Option a -> List a` — Option モジュールの
  メンバーとして登録し、`stdlib/list.OptionToList`（手書き Go、
  `stdlib/list/impl.go`）に forward する。`list` はすでに
  `option` を import している。

どちらも `.gomi` では宣言できない（宣言すれば下層パッケージの
.gom に実装義務が生じて循環する）ため、`internal/stdlib` が
`.gomi` 由来のメンバーテーブルを読み込んだあとに手組みの
メンバーとして追記する（`conversionModules`）。`.gomi` 側には
「面に存在するが実装の所在は上層パッケージ」のコメントを残し、
「`.gomi` が公開面の正」の主張を文面で保つ。

## 却下した案

- **(実装を下層に、変換先 import)**: Go 循環 import で不可能。
- **(変換先モジュール名で公開)**: `X.to_y` が `X` に載る規則と矛盾。
- **(変換専用の新モジュール)**: 同上。モジュール数も増える。
- **(generics なし・手書き match をユーザーに書かせる)**: そもそも
  この lane の目的である標準化の放棄。

## 検証

[sketch/2026-11-18-stdlib-conv-verify.md](../sketch/2026-11-18-stdlib-conv-verify.md) —
spec fixture・golden emission・Go 単体テスト・DAG 維持を確認。
