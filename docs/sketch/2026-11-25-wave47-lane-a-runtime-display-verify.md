# wave47 lane A type-erased runtime display の検証

2026-11-25。対象は型パラメータを残す composite の nested 表示である。

## prototype

- `rt.ShowRuntime` を追加し、`ShowRuntime` recipe の compiled 出力を
  `fmt.Sprint` から差し替えた。root の production splice は scalar の裸表示を
  保つ `rt.ShowDisplay` を使う
- record / variant 宣言から generated Go へ package-scope の show metadata
  登録を出し、reflection が Gom の field / constructor 綴りを復元する
- tuple、list/array の slice representation、builtin Option / Result、一
  フィールド record の defined representation、opaque Go struct を確認した
- evaluator oracle は runtime fallback 内の `VChar` を Int32/rune representation
  と同じ数値表示へ倒す。`Char` の区別は erase 後の compiled reflection に残ら
  ないためである

## compare oracle / compiled

`internal/build/compare_test.go` に追加した往復:

- `splice_generic_tuple_runtime_renderer` — `show (1, "x")` が
  `[(1, "x")]`
- `splice_generic_nested_list_runtime_renderer` — nested list が
  `[[1, 2]]`
- `splice_generic_record_variant_runtime_renderer` — record / variant が
  `[({ x = 1, y = 2 })]:[(Circle ~radius: 1.5)]`
- `splice_generic_one_field_record_runtime_renderer` — defined
  representation でも `[({ on = true })]`
- `splice_generic_wrapped_record_runtime_renderer` / `splice_generic_wrapped_list_runtime_renderer` —
  defined representation の composite / slice field に再帰する
- `splice_generic_wide_tuple_runtime_renderer` — Tuple8 を超える anonymous
  struct representation も tuple として描く
- `splice_generic_option_result_runtime_renderer` — builtin が
  `[(Some 1)]:[(Ok 2)]`
- `splice_generic_char_record_runtime_renderer` — runtime fallback 内では
  `'y'` ではなく representation 上の `121`

`rt/show_test.go` は unregistered Go struct / named Go slice が `%v` 綴りの
まま operand 括弧を取ること、`Err` payload では静的に error interface のときだけ
`Error()` を優先し concrete payload の record metadata を残すことも固定する。
multi-file regression `sibling_show_metadata_init_order` は package-level initializer が
sibling type の registration より先に走らないことを固定し、
`cmd/gom.TestEndToEnd_ImportedRuntimeShowMetadata` は importer の initializer-time
generic splice が imported package 初期化済みの record metadata を見ることを固定する。

## 残りの境界

- Go `%v` の完全再現はしない。map、pointer、function、unregistered struct は
  fmt に委ねる
- `Char` / Int32 のように representation が同一の scalar 区別は runtime
  fallback で復元しない
- static type が残る通常の typed renderer と括弧の境界は wave46 lane A のまま
