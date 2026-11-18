# `errors.As` target の `Ptr (Ptr E)` — 証明可能な要素側違反を W0502 に足す

- 状態: 採用（wave22 prompts で podhmo 採択済み。実装・検証した — spec.md §8.4 と fixture を更新済み）
- 日付: 2026-11-19
- 関連: [errors-as-target-lint](2026-11-05-errors-as-target-lint.md)（W0502 本体の設計）
- 出典: open-questions「W0502 lintErrorsAsTarget の `Ptr (Ptr E)` ケース」（wave9 error-tail レビュー nit 由来）

## Context

`errors.As` の target が `Ptr (Ptr E)` — Go でいう `**E` — のとき、
指される要素型 `*E` が契約を満たせるかは E の shape で決まる:

- `*E` はポインタ型なので interface 型ではあり得ない
- `*E` のメソッド集合は E の宣言に載る（`func (*E) Error()` は
  E が named かつ非 interface 型のときだけ書ける）

つまり E が unnamed 表現（スカラ、`List`/`Map`/`Slice`、関数・タプル、
`Ptr` 自身 — `***X` も同型）か interface 表現（宣言 sum、`Option`、
`GoError`、`Go.Any`）なら `*E` は error を実装できず interface でもなく、
panic が静的に証明できる。従来は `notAsTargetElem` の `case "Ptr"` が
無条件に沈黙していた。

## Decision

`notAsTargetElem` の `case "Ptr"` で E を `ptrElemProvablyInvalid`
に渡して判定する。warn するのは E の表現が provably unnamed か
interface のときだけで、次は従来どおり沈黙する（named 非 interface 型
であり得るため `*E` のメソッド集合を否定できない）:

- `types.Go` — named Go 型。`*E` の Error() は見えない
- `*types.Meta` / `*types.Var` — 未解決。`Ptr` や named 型に解決し得る
- 宣言 record（named struct — `*R` の Error() は否定しきれない、
  単一段 `Ptr R` が warn する判定とは対象が違うので非対称は意図的）
- abstract 宣言（表現が隠れ named 型を隠し得る）

警告文言は既存の要素側 W0502 と同系 — 表示される `argument` は
`Ptr Int` のような要素型そのもの（`*E` が「interface にも error
実装にもなり得ない」ので文言はそのまま正しい）。

型 alias（`AliasBody`）は resolve 経路で保持する — 表面構文で
書けないので到達は `.gomi` 由来のみだが、`seen` 付きの再帰は
kindAlias をそのまま辿る。

## 検証

`testdata/spec/W0502_errors_as_target.gom` に warn 系 5 件
（`Ptr (Ptr Int)` / `Ptr (Ptr (List Int))` / `Ptr (Ptr Sum)` /
`Ptr (Ptr GoError)` / `Ptr (Ptr (Ptr Int))`）と沈黙系 3 件
（`Ptr (Ptr Rec)` / `Ptr (Ptr io.Reader)` / `Ptr (Ptr a)`）を追加。
