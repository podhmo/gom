# Go functions that mutate a slice parameter compile and silently do nothing

- 状態: 未決
- 日付: 2026-11-05 (found by the P3-7 real-program audit —
  [sketch](../sketch/2026-11-05-p3-7-realworld-interop-audit.md))

## Situation

```gom
import go "sort"

fn main () -> Unit = {
  let xs = ["b", "a", "c"]
  sort.Strings xs
  ignore fmt.Println "${xs}"   // [b a c] — unchanged
  ()
}
```

`check`/`build`/`go build`/run all pass, yet `sort.Strings` does
nothing. Per spec 8.5 the `List` is **copied** into a `[]string` at
the boundary; the copy is sorted and thrown away. `sort.Ints` dies on
E0501 anyway (`[]int` element int/int64 split — the recorded
divergence), but `[]string` matches element-for-element so the call
type-checks, and a function whose contract is "mutate the parameter
in place" becomes dead code.

The trap is not `sort`-specific: every Go API that mutates a `[]T`
parameter behaves this way (the `slices` generics are already hidden
by structural constraints, but non-generic mutators like a
`sort.Reverse` equivalent or binary-write helpers), as are calls that
share state through `*T`/`map`/interface parameters.
`rand.Shuffle` is hidden by its func parameter; `sort.IsSorted` is
hidden by its anonymous-interface method parameter — so the
"compiles yet silent" band is narrow (non-generic, same-element-type
mutators), but `sort.Strings` is its representative.

## Why it is a problem

- The behavior is per spec (immutable `List`, copy both directions),
  so it is not a bug — but the user's intent ("sort it") and the
  outcome diverge silently. Whether to diagnose "calls that are
  spec-correct yet useless" is undecided.
- This is the mirror image of the boundary copy-depth question
  ([2026-11-05-go-boundary-copy-depth](2026-11-05-go-boundary-copy-depth.md)):
  there a shallow copy leaks *too much* visibility of Go-side state;
  here the copy prevents Go-side mutation from reaching *at all*.
  Both are decided by the same design question — what the boundary
  guarantees.

## Direction notes

- A warning diagnostic for "a Go call whose result is discarded and
  that takes a copied `[]T` parameter" (same soil as the P5-2 warning
  family). The snag: read-only `[]T` parameters
  (`sort.SearchStrings`-style) are fine, and the Go signature alone
  does not reveal mutation. If a list of mutating APIs is needed, it
  belongs to the standard-bindings work.
- Mutation through an explicit `Go.Slice` view (the `Go.conv` pun)
  *does* land. One route: document `Go.Slice` conversion as the
  idiom for "I want Go to mutate this" in the spec (the no-copy
  `Go.conv` pun is already recorded in the gaps ledger).
