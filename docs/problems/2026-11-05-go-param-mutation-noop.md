# Go functions that mutate a slice parameter compile and silently do nothing

- 状態: 解消（2026-11-05 — W0501 warns on the shape: a Unit-result
  Go call that passes a `List` into a copied `[]T` boundary. It is a
  heuristic, not a mutation detector — the `signal.Notify` family
  warns without mutating, a mutator returning a real result escapes,
  and `List`s nested in tuple/record arguments are uninspected. The
  wording stays conditional so the message is true either way. See
  [decision](../decisions/2026-11-05-go-mutation-warning.md),
  [verification](../sketch/2026-11-05-p3-7-interop-ops-verification.md))
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
- Mutation through an explicit `Go.Slice` view *does* land — but
  only on backing the Go side already shares: `Go.conv` between
  `List` and `Go.Slice` now copies
  ([decision](../decisions/2026-11-04-goconv-list-slice-copies.md)),
  so the idiom is a `Go.Slice` value obtained from Go (or
  forwarded `..gs` / `...[]T`), passed verbatim into a
  `Go.Slice`-declared parameter. A `List` the user wants Go to
  mutate in place has no spelling left — by design; the W0501
  wording stays conditional for it.
