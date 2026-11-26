# nested match の same-scrutinee narrowing

- 状態: 採択（実装は wave51 以降 — [検証](../sketch/2026-11-27-wave50-lane-d-match-precision-verify.md)）
- 日付: 2026-11-27

## 決定

`match` の腕本体の中で、同じ安定なローカル変数を再度 `match` するときに限り、pattern phase が root constructor の narrowing を行う。wave51 以降の実装対象とし、wave50 では回帰 fixture を現行挙動の記録として追加する。

狭める範囲は次に限る。

- **positive narrowing**: 腕本体では、その腕の root pattern が当たったことが分かる。`Add n -> ...` の本体では scrutinee は `Add`
- **negative narrowing**: 後続腕へ到達した値は、それより前の guard なし腕が確実に除外した constructor ではない。`Add _ -> ...; rest -> ...` の `rest` 本体では `Add` は来ない
- **alias**: `rest ->` のような root 変数 pattern は scrutinee と同じ値を束縛する。nested match が元変数と alias 変数のどちらを対象にしても同じ narrowing を使う
- constructor payload がすべて `_` / 変数 / 全体を覆う pattern の constructor 腕だけを negative narrowing の根拠にする。`Add 0` のような一部 payload は `Add 1` を残すので constructor 自体を除外しない
- or-pattern は、各選択肢が同じく確実に除外できる constructor 単位で畳み込む

適用しないもの:

- guard 条件の意味解析。前の腕に guard があれば negative narrowing には使わない。本体の中では pattern 自体が当たった事実は使える
- フィールド参照、関数呼び出し結果、mutable cell、タプル/record path、別名をさらに辿る解析
- リテラル集合や数値範囲の推論
- 関数をまたぐ interprocedural な narrowing

## 到達不能 arm の扱い

実装の第一段階では、この narrowing を **E0301 の網羅性判定だけ**に使う。context を考慮した E0302（到達不能 arm）までは広げない。

理由は移行互換である。現状の workaround は、nested match に不可能な constructor の `todo ()` 腕を置くことであり、narrowing を E0302 にも即時適用すると、その workaround が一斉に compile error になる。まず E0301 が消えることでユーザーが `todo ()` 腕を削除できるようにし、context 込みの E0302 を有効にするかは別途移行影響を見て決める。

## `todo ()` を消せる範囲

消せるのは、narrowing 後の残り constructor 集合を nested match の腕がちょうど覆う場合である。例えば `Add / Remove / Archive` の外側で `Add` を処理し、catch-all 内で `Remove` と `Archive` を網羅する形は、nested match の `Add -> todo ()` を不要にできる。

残る範囲:

- catch-all が本当に複数の未処理 constructor を残す場合。narrowing は残余を消さないので、partial match は依然 E0301
- guard 条件でしか到達不能にならない場合。`n > 0` のような条件は解析しない
- DMMF の進化テストで問題になった「広い `_` があると新ケース追加を検知できない」問題。これは narrowing では解けない。進化検知が必要な場所では、分類専用の sum type を返す関数 + flat な exhaustive match に書くのが正しい

## 実装方針

`internal/pattern` の matrix checker を壊さず、match walker が腕本体へ入るときに「root constructor を除いた / 絞った signature view」を渡す。型検査フェーズや型そのものは変えない。scrutinee identity は typed AST のローカル変数 symbol と、root catch-all / as 束縛が作る alias に限る。

これは完全な flow analysis ではなく、match の腕境界に限定した構文的な narrowingである。未知の pattern 形や式形では narrowing しない方向に倒し、false positive より現行どおりの保守的な E0301 を優先する。

## 却下した案

- **常に flat な match へ書き換えるよう guide だけで解決する**: 書ける場合はそれがよいが、外側の制御フローを保ったまま残余 constructor を処理する action dispatch ではネストが自然で、compiler が分かる情報を毎回 `todo ()` で補わせるのは noisy
- **`unreachable` / `absurd` 腕構文を足す**: ユーザー assertion を別構文に変えるだけで、compiler の証明能力は増えない。構文追加のコストに見合わない
- **guard 条件まで含む一般的な flow analysis / SMT 的な推論**: 言語コアの複雑さが大きく、今回の実害に対して過剰
- **現状維持**: `todo ()` が compile-time の約束ではなく runtime panic の逃げ道として残り、網羅性の信頼を薄める

## 回帰 fixture

現行の限界を示す fixture を spec harness に追加する。これらは現行では E0301 を期待するが、実装 lane では red-first の起点として期待を更新する。

- `testdata/spec/E0301_nested_match_selfish_action.gom`: catch-all alias 内の negative narrowing
- `testdata/spec/E0301_nested_match_dmmf_line.gom`: DMMF 形の negative narrowing
- `testdata/spec/E0301_nested_match_positive_context.gom`: constructor 腕本体内の positive narrowing
- `testdata/spec/E0301_nested_match_guard_boundary.gom`: guard 付き腕を negative narrowing に使わない境界
