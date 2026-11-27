# callback result への record expectation propagation

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave53-lane-a-callback-record-verify.md)）
- 日付: 2026-11-27

## 決定

generic higher-order callback の本体結果位置にある無名 record literal も、[record literal の文脈推論](2026-11-27-record-contextual-inference.md) の activation 対象に含める。pipe / generic application の結果に静的な期待型があり、供給スロットが静的に決まるとき、従来どおり結果型と残余型を speculative に単一化し、callback の戻り値側へ分かった record 型を前向きに伝える。

- 対象は callback 本体の結果位置にある literal に限る。block の最終式、if/match の分岐結果、tuple の要素、ascription の内側は結果位置として辿る
- let 内の中間 literal、呼出し引数、field 値など、callback の結果型をそのまま持たない位置からは activation しない。tuple の要素は結果値の構成要素として辿るが、record field 値は対象外のままにする
- 後続使用から逆方向に型を決めない。`let seed = { ... }` の seed を後の `List.fold` 利用から推論するような経路は入れない
- speculative propagation の clone/rollback、型名前置 literal 優先、positional/labeled lambda の既存規則、monomorphic local inference は変えない
- pipe の静的な期待型が `Go.Slice` のとき、最後に implicit boundary conversion を取る `List` producer の形でも speculative propagation を試す。期待型が `List` で producer が `Go.Slice` を返す逆方向も同様に試す。逆に hard expectation へすると部分適用の残余型を境界変換より前に誤って拒否するため、pipe callee へは soft hint として渡す。この retry は pipe に限り、通常の関数適用や関数値の戻り値の内側には広げない
- `<|` / `|>` で piped operand そのものが higher-order callback のときも、pipe を `callee operand` の適用として speculative propagation する。`Scope.run` のような Go boundary callee でも callback parameter に record result が見えるときは seed を使う
- record literal が解決できない backward inference 事例で出る二次 E0401 は今回の対象にしない。`E0407_record_context_no_backward` は従来どおり E0407 + E0401 を固定し、診断抑制は lane A の scope に入れない

## 背景

wave52 lane D の DMMF 再評価では、`input.lines |> List.traverse_result validate_line |> Result.map (fn lines -> { id = input.id, lines = lines })` が E0407 になった。`Result.map` の callback 型は `a -> b` で、pipe 後の静的な戻り値から `b = ValidatedOrder` が前向きに分かるにもかかわらず、既存の activation は「引数そのものが無名 record literal」だけを見ていた。そのため callback 本体の結果にある literal では speculative propagation が起動しなかった。

## 検証

- spec fixture `ok_record_context_pipe_callback_result`: direct generic application、pipe 経由 `Result.map`、if 分岐結果、match 分岐結果の裸 record literal が compile できる
- unit test: callback 結果位置の record literal が activation し、lookahead が inference state を bind しないこと。block 内の非結果位置にある record literal は activation しないこと
- negative fixture `E0407_record_context_no_backward` は後続使用からの逆推論をせず、従来の E0407 + E0401 を維持する
