# P3-2 residuals + P3-6 Go-calls-Gom verification

Date: 2026-11-04. Branch: `wt/p3-2-p3-6-interop`. Verifies the
conversion oracle and `Go.Any` boxing fixes plus the P3-6 e2e
fixture. Non-normative.

## What was checked

### `Go.conv` legality goes through `go/types`

`convOK` asks `ConvertSource.GoConvertible`, implemented in
`internal/goimport/boundary.go` via `types.ConvertibleTo` on the
boundary-mapped types. Verified through spec fixtures:

- `testdata/spec/E0507_go_conv_invalid.gom` — `Int` →
  `net/http.Server`, `List String` → `net/http.Server`, `Int` → a
  Gom ADT, and `String` → `net/http.Server` all E0507.
- `testdata/spec/ok_go_conv.gom` — `5 : time.Duration` (literal →
  named numeric), `d : Int` (named → underlying), `os.Args : List
  String`, `"ab" : List Char` (string → []rune), `97 : Char` all
  accepted.
- `Go.conv 5 : http.Server` — the operand's meta was unsolved at
  first check; `pendingConvs` revalidates after defaulting and the
  conversion is E0507. Without the deferral this escaped.

### `{Go.conv}` member-style misuse stays at E0507

`testdata/spec/E0507_go_conv_member.gom` binds `{Go.conv}` and
`Go.conv 1` as function values. Both report E0507
("`Go.conv` is only valid as a conversion") in checking — no path
reaches lowering's E0901 "member builtin is not an expression".
The E0901 regression the task mentions is already guarded; the
fixture locks the behavior in.

### `Go.Any` boxing keeps the Gom numeric type

`internal/lower/expr.go` materializes a `ConvNum` before
`ConvBoxAny` when the value's type is Gom-numeric, so generated
code emits `any(int64(42))` rather than boxing the untyped
literal's Go default `int`. Verified in
`testdata/gocall/geom/geom_test.go` (TestAnyBoxingFromGo): `%T`
reads `int64` for a bare literal, a bound `Int`, and an `x + y`
result, and `helper.Kind`'s Go type switch lands on `int64`.
`testdata/golden/tailloop_capture.golden.go` regenerated for the
new `int64(x)` before `any`.

### P3-6: the generated package used from Go

`testdata/gocall` + `cmd/gom/gocall_test.go` (TestEndToEnd_GoCallsGom)
copy the module, `gom check`/`gom build`, then `go build`/`go test`
with the real toolchain. The consumer test covers, per spec 8.5–8.7:

- ADT: `geom.Shape_Circle`/`Shape_Rect` construction, `Area`, and a
  type switch over the sealed `Shape` interface.
- Invalid values: `Describe(nil)` panics with "Shape"/"invalid" via
  `rt.InvalidVariant`; a zero `result.Result` panics in `IsOk`
  (spec 8.6).
- `Result`/`Option`: `HeadOr`/`HeadOpt` results inspected via
  `Tag()`/`Unwrap()` and type switches; `result.Ok`/`result.Err`
  values passed back into `IsOk`.
- nil: `FileName(nil *os.File)` and `NameOf(nil fmt.Stringer)` panic
  Go-style; `NameOf(2*time.Second)` exercises interface
  assignability.
- Slices: `KeepSlice` is the no-copy `Go.Slice` view (mutation
  visible both ways); `Freeze`/`Thaw` use implicit boundary
  conformance, which copies (`list.FromSlice`/`.ToSlice()`) — mutating
  either side afterward does not reach the other. `nil` `List` is
  the empty list.
- Named types: `time.Duration` in and out via `Go.conv` inside the
  Gom functions.

## Deferred / known limits

- `Go.conv` between `Go.Slice T` and `List T` is a no-copy
  representation pun — identical `go/types` underlying type, so
  `ConvertibleTo` is true and no `FromSlice`/`ToSlice` runs. The
  spec's List↔slice copy rule is observed at implicit boundary
  positions only. Recorded in the conv-oracle decision.
- Gom-side stdlib modules (`import "list"`) are not importable from
  an external module — E0203 "outside the main module". The fixture
  hand-rolls the small List folds it needs; whether stdlib should
  resolve for downstream modules is an open packaging question.
- `Go.Any` cannot be named in `.gom`/`.gomi` signatures, so boxing
  is observed through a Go `any` parameter (the helper package)
  rather than an `any`-typed Gom return.
