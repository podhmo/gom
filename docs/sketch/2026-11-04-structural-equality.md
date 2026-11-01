# Structural equality emission — verification report

- 目的: verify that the `rt.Equal` (`reflect.DeepEqual`) vs native Go
  `==` split lowered for spec 3.3's structural equality is correct.
- 関連: [decision: structural-equality-emission](../decisions/2026-11-04-structural-equality-emission.md) (draft),
  TODO.md bug "structural equality",
  docs/sketch/2026-11-04-v1-tutorial-findings.md
- 検証日: 2026-11-04, go1.27.1 linux/amd64, worktree `task/bugfix-lower`.

## What was checked

### Lowering split (unit coverage in `TestEqLowering`)

| operand type | Core node | generated Go |
| --- | --- | --- |
| `Int` | `core.Binary` | `a == b` |
| `(Int, String)` | `core.Binary` | `a == b` (rt.Tuple2 struct) |
| `List Int` | `core.CallBuiltin{eq}` | `rt.Equal(x, y)` |
| `(Int, List Int)` | `core.CallBuiltin{eq}` | `rt.Equal(a, b)` |
| record (`Pair`) | `core.CallBuiltin{eq}` | `rt.Equal(a, b)` |
| ADT (`Shape`) | `core.CallBuiltin{eq}` | `rt.Equal(a, b)` |
| `List Int`, `!=` | `core.CallBuiltin{ne}` | `!rt.Equal(x, y)` |

The golden fixture `testdata/golden/builtins.gom` pins the same
output end-to-end.

### Real-module build + run

`gom build` output dropped into a scratch module with
`replace github.com/podhmo/gom` and run with `go run` — every case
answered correctly:

- `[1,2] == [1,2]` true; `[1,2] != [1,3]` true
- `(1,"a") == (1,"a")` and `(1,[1]) == (1,[1])` true
- `{ x = 1, xs = [2] }` equal to itself; `!=` against a record whose
  `xs` differs
- `Circle 1 == Circle 1`, `Square ([1]) == Square ([1])` true
- `Circle 1 != Square ([1])` true (different variants)
- `[Circle 1] == [Circle 1]` true (list of ADTs)

ADT values are Go interfaces: under `==`, `Shape_Square` (a
`list.List` payload) would panic instead of comparing; `rt.Equal`
always answers.

## Notes

- `let g = todo` and `assert` outside tests are diagnosed
  independently of this change (E0407 for the unbound `todo` type
  argument; E0901 "may only be called inside a test block" until
  docs/problems/2026-11-02-assert-outside-test.md decides the
  semantic rule).
- `types.Go` (named Go types) and `GoError` always take `rt.Equal`:
  comparable named types still compare structurally, and pointers
  get a deeper-than-`==` answer — the chosen reading of
  "structural" in spec 3.3.
- nil vs empty `List`: `rt.Equal` normalizes the top level (two
  empty same-type slices are equal) after review found
  `reflect.DeepEqual(nil, [])` answers false where eval's
  element-wise `eqValue` answers true — reachable through the Go
  boundary (`FromSlice`, `Go.Slice`). Nested nil/empty (inside a
  record, ADT or outer list) still follows DeepEqual; `GoError`
  keeps DeepEqual's concrete-error comparison where eval compares
  message text. Both are recorded as interim warts in the decision
  doc until per-type comparators land. `rt.AssertEq` now routes
  through `rt.Equal` so `assert_eq` gives the `==` answer.
