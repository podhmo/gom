# record literal の文脈推論

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave50-lane-e-record-context-verify.md)）
- 日付: 2026-11-27

## 決定

型名を前置しない record literal は、静的に分かる期待型が record 型なら field-set 推論より先にその型を使う。使う期待型は、型注釈、関数の戻り値、呼出し引数、constructor payload、record field のように式の位置から前向きに分かるものに限る。後続の使用から逆方向に型を決めない。

型名前置 literal は常に最優先する。`Other { count = 0 }` を `Acc` が期待される位置に書けば、期待型ではなく前置した `Other` を選び、通常どおり型不一致を報告する。

generic な関数や constructor の適用では、適用結果に静的な期待型があり、引数に無名 record literal を含み、供給スロットが静的に決まるとき、結果型と残余型の単一化を speculative に試してパラメータの期待型へ使う。この試行は推論 meta を clone して行い、周囲の推論状態を早く bind しない。実際の引数は従来どおり callee 自身のパラメータ型へソース順で適合させる。`|>` / `<|` の片側が無名 record literal を持つ部分適用なら、pipe の結果の期待型に record 型が含まれるときだけそれを callee 側へ渡し、pipe される入力型は開いたままにする。

期待型の関数パラメータを受け取る無名ラムダでは、そのパラメータの期待型が record 型のときだけ、注釈のないパラメータをその record 型で初期化する。これは `List.fold (fn acc x -> { acc with ... }) seed xs` の accumulator のように、record literal 自体だけでなく fold 関数内の record update まで同じ文脈を必要とするための限定例外である。record 型でない expected parameter は従来どおり fresh meta にし、本体の使用から推論する。

匿名 record 型を新しく作ったり、record update の base 不明なまま型を発明したりはしない。静的な期待型がなく、field-set が複数 record に一致する場合は従来どおり E0407 になる。

## 背景

段階型では同名フィールドを持つ record が並び、`Ok { ... }` や `List.fold` の accumulator で E0407 が頻発していた。wave49 の型名前置 literal は明示的な回避面を作ったが、return 型や constructor payload にすでに静的な型がある位置まで注釈を要求するのは重複していた。

型推論へ期待型を広く流すと、局部 `let` の単相性や `Go.Any` を期待するラムダの診断など既存の意味を変えやすい。したがって record literal を含む適用に限定し、speculative unification では外部 meta を bind せず、通常の適合は callee の本来のパラメータ型へ行う。

## 実装

`checkGomApply` は静的に fill できる適用について、結果の期待型と残余型を clone 上で単一化し、具体化が分かったパラメータだけを文脈期待型として引数チェックに使う。引数自体はそのあと本来のパラメータ型へ `conformExpr` するため、モノモーフィックな局所推論を素通りしない。`checkLambda` は expected parameter が record 型のときだけ注釈なしパラメータへその型を seed する。pipe は無名 record literal を含む部分適用 callee に限り、結果期待型を `input -> expected` の形で渡す。

## 検証

- spec fixture: `ok_record_context_simple_positions`（注釈・return・generic 呼出し引数・record field）、`ok_record_context_constructor_payload`、`ok_record_context_list_fold_acc`（直接適用と `|>`）
- negative fixture: `E0407_record_context_no_backward`（後続使用から逆推論しない）、`E0401_record_context_prefix_priority`（型名前置優先）
- unit test: `TestApplyParamExpectationsDoesNotBindInferenceState`
