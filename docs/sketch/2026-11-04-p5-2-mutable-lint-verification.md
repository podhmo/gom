# P5-2 mutable-sharing lint — verification

Verification of the decisions in
[2026-11-04-mutable-sharing-lint](../decisions/2026-11-04-mutable-sharing-lint.md)
against the implementation in `internal/check/lint.go`, exercised
through `gom check` and the spec-fixture suite
(`testdata/spec/`). All inputs below type-check clean except where a
diagnostic is the point.

## W0701 — `Chan.make` of a mutable element

```gom
type Cell a = | Cell a | EmptyCell
type Counter = { n: Ref Int }

fn make_channels () -> Unit = {
  let _ : Chan (Ref Int) = Chan.make 1          // warns
  let _ : Chan (Array Int) = Chan.make 1        // warns
  let _ : Chan (Ptr Int) = Chan.make 1          // warns
  let _ : Chan (Go.Slice Int) = Chan.make 1     // warns
  let _ : Chan (Go.Map String Int) = Chan.make 1 // warns
  let _ : Chan (Ref Int, Bool) = Chan.make 1    // warns (tuple)
  let _ : Chan (Cell (Ref Int)) = Chan.make 1   // warns (ADT payload)
  let _ : Chan Counter = Chan.make 1            // warns (record field)
  let _ : Chan Int = Chan.make 1                // silent
  let _ : Chan (List Int) = Chan.make 1         // silent
  ()
}
```

Result: exactly the eight mutable cases warn, each at its `Chan.make`
application span with `data.type` naming the element (`a channel's
element type Cell (Ref Int) contains mutable values`). Pinned by
`testdata/spec/W0701_chan_mutable_elem.gom`.

Inference-side confirmation — `ok_chan_elem_types.gom`'s existing
`slice_elem` makes `Chan ?e` and lets `goiface.SendSlice(ch, v)` solve
`?e` to `Go.Slice Int` at the Go boundary: the warning still lands on
`Chan.make 1`, confirming the sweep sees post-default solutions, not
call-site metas. Its expected JSON now pins that warning — this is the
only pre-existing fixture the lint changes.

## W0702 — `go { ... }` captures a mutable variable

```gom
fn worker (r: Ref Int) -> Unit = ignore r

fn spawn (r: Ref Int) (xs: Array Int) (c: Counter) (n: Int) (f: Int -> Int) -> Unit = {
  go { ignore (worker r); ignore xs; ignore c; () }   // warns: r, xs, c
  go { go { ignore (worker r); () } }                 // warns: r ×2 — one per boundary
  go { ignore n; ignore (f 1); () }                   // silent (Int, function)
  go { let r2 : Ref Int = todo (); ignore (worker r2); () } // silent (bound inside)
  ()
}
```

Result: five warnings — one per distinct capture per `go` body, at the
first use span inside the body. The nested-`go` case emits two
warnings on the same `r` use, one for each boundary the variable
crosses; that duplication is the literal spec reading ("`go { ... }`
がそれらの型の変数を捕捉するとき") and is accepted deliberately.
Pinned by `testdata/spec/W0702_go_capture_mutable.gom`.

Member-selection captures behave the same: `go { ignore os.Stdin; () }`
warns (`Stdin : Ptr os.File`), while `go { ignore os.Args; () }` stays
silent — `os.Args` maps to `List String` at the boundary, not
`Go.Slice`. Generalized value-lets (`let x = (r, 1)` → the `thunks`
flipped `KindFunc`) still count as variables and warn when their
value type contains mutable constructors; declared `fn`s and
let-lambda flips (function types) never warn.

## Shadowing — builtins judged by declaration

```gom
type Ref = | Boxed Int
fn worker (r: Ref) -> Unit = ignore r
fn spawn (r: Ref) -> Unit = { go { ignore (worker r); () } }
```

Result: no warning — the unqualified `Ref` denotes the local ADT,
whose body (an `Int` payload) is expanded and found immutable.
Pinned by `testdata/spec/ok_go_capture_shadowed_ref.gom` (empty
diagnostics). The symmetric positive case — a local type whose body
*does* contain mutable state — is covered by `Counter`/`Cell` above.

## Companion diagnostic fixes (same check area)

1. `select` channel operands spelling a file-local `type Chan` /
   `type RecvChan` / `type SendChan` now report E0415 ("a select
   receive arm's channel needs the builtin `Chan`…") instead of the
   generic E0401 mismatch — the same branch `for` already had.
   Pinned by `E0415_select_recv_local_chan.gom` and
   `E0415_select_send_local_chan.gom`.

2. `for`'s shadowed-builtin list gained `SendChan`: `for x in ch`
   with a local `type SendChan` used to emit "type mismatch: expected
   a List or a channel, got SendChan Int"; it now reports E0415.
   Pinned by `E0415_for_local_sendchan.gom`.

3. `Chan.close (Chan.recvOnly ch)` used to emit three diagnostics —
   E0407 for `close`'s unbound type argument `?3`, E0401
   "expected SendChan ?3, got RecvChan ?4", and E0407 for `?4`.
   `conformExpr`'s failed-unify recovery now silences every meta
   inside `want` via `metasIn` (previously `markQuiet` only covered a
   top-level meta, leaving `?3` inside `SendChan ?3` to pile on).
   Result: E0401 plus the single legitimate E0407 for the genuinely
   ambiguous element — pinned by `testdata/spec/E0401_close_of_recvonly.gom`.

## Non-regression

`gofmt -l .` clean; `go vet ./...` clean; `go test ./...` green
including `TestSpec`/`TestGolden` (`internal/build`), the
`gom`-binary end-to-end tests, and the skill-snippet verification.
The only fixture whose expectations changed is
`ok_chan_elem_types` (documented above); no golden output changed —
the lint adds diagnostics only.

## Known limitations (accepted in the decision)

- `let mk = Chan.make; mk 0` does not warn — the lint identifies the
  call by the resolved `rt.MakeChan` callee symbol, which a local
  re-export discards.
- `Task.spawn (fn () -> ...)` thunk captures are out of scope; spec
  names `go { ... }` only.
- Opaque Go named types (`sync.Mutex`-style) are not inspected; their
  type arguments still count (`pkg.Box[Ref Int]`).
- Closure captures are invisible in types: `let f = fn () -> r` then
  `go { f () }` warns on `f` only if `f`'s *type* contains mutable
  constructors — function types never do, so that path is silent by
  design.
