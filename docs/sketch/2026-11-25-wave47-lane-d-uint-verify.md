# wave47 lane D `Uint` / `Uint64` の検証

2026-11-25。対象は `uint` / `uint64` に限定した第一級 unsigned integer 対応である。

## prototype / regression

- `testdata/spec/ok_uint_first_class.gom` は `Uint` literal、`math/bits.OnesCount64`
  への引数、`Uint` 算術、`Uint64` への `Go.conv`、`bits.Reverse64` の roundtrip を
  通す
- `internal/build` compare:
  - `uint_first_class_bits_boundary` — 最大値 literal、unsigned division、
    `uint64` 引数 boundary が compiled/eval で一致する
  - `uint_go_result_roundtrip` — `bits.Reverse64` の `uint64` 結果が high bit を
    失わず Gom 側へ戻る
  - `uint_type_erased_runtime_display` — type-erased generic display で
    `18446744073709551615` が負数に見えない
- `uint_go_raw_uint_roundtrip` — `bits.Reverse` の raw `uint` 引数・結果が境界で
  `uint64` と変換される
- `testdata/spec/E0417_uint_lit_range.gom` は `-1` と `2^64` の拒否を固定する
- `testdata/spec/E0414_uint_div_width.gom` は constant-fold が unsigned division を
  使うことを固定する
- review regression: high-bit shift count、negative fractional conversion、extern
  `Uint64`、shadowed `Uint` の method/type-test、raw uint field write、host-width
  dependent `bits.Reverse` expectation を固定する
- `go_try_as_uint_type_erased_box` — type-erased `Uint` は `Go.Any` 内で uint64 として
  assert できる
- `TestUintGoFuncCallbackRuns` — raw `func(uint) uint` について `Go.func1` / `Go.fn1`
  の両方向変換が生成 Go で動く
- `testdata/spec/ok_method_iface_uint_width.gom` は raw `uint` に `Uint`、raw
  `uint64` に `Uint64` を要求する `@method` 境界を固定する
- `testdata/golden/method_iface.golden.go` は method boundary が `uint(x)` /
  `uint64(x)` 変換を emit することを固定する
- `math/bits.Len32` は `uint32` 未対応のままなので、E0501 の hidden-member fixture
  をその理由へ更新した

## 残りの境界

- `uint16` / `uint32` / `uintptr` は公開しない。`Byte` の literal・演算なし仕様は
  変更しない
- コンテナ内部で raw `uint` と `uint64` を要素ごとに変換する仕組みは持たない。
  nested raw `uint` の interface method は公開されない
