# `reclassifyCalls` `ArgToParam` nil-guard — reachable, load-bearing, and still not a divergence

Non-normative record closing a TODO suspicion: "`reclassifyCalls`
nils `ArgToParam` past declared arity — the evaluator still applies
`f.Remaining()`'s mapping while compiled code falls back to
positional order, possibly diverging." The suspicion has two halves,
and they separate cleanly:

- **The nil is reachable and the guard is load-bearing** — remove it
  and `a.Validate()` panics on a real program.
- **The eval/compiled divergence does not exist** — eval reads the
  *same* reclassified `Call.ArgToParam`, so both sides go
  positional; and the nil is exactly what the declared-fn path would
  have produced had the flip already happened.

## The suspected mechanism, corrected

`checkLetDecl` flips a *referenced* `let f = <lambda>` to
`KindFunc` (check.go:723-730) and reclassifies the applications
recorded while `f` was still a variable — forward references in
earlier declarations and recursive calls inside `f`'s own RHS —
from `CallUnknown` to `CallDirect`/`CallPartial`.

My earlier version of this note claimed `ArgToParam` is always nil
at every site the pass can reach, because a forward reference sees
only the unbound pre-registered meta (`flat=0`, labels → E0408).
That is wrong: **the meta can already be bound** when a later
forward *call* is checked. A non-call use of `f` — a return-value
position, an `e : T` recovery site, anything that unifies
`c.vals[f]` — gives the pre-registered meta a concrete `Func`
type early. `unifyFunc`'s labeled-run pairing (unify.go:793-799)
matches labeled params by name, so the bound shape can even carry
labels; `let f`'s own RHS then unifies against it cleanly —
Ret-position labels pair up, the `?x` head is what the declared
arity forgets.

Repro (`fn` decl `g` has labeled params):

```gom
fn g (~a: Int) (~b: Int) -> Int = a - b
fn take () -> (Int -> ~a: Int -> ~b: Int -> Int) = f
fn use2 () -> Int = f 0 ~b: 20 ~a: 1
let f = fn x -> g
```

- `take` returns `f` at a concrete `Func` type — `c.vals[f]` binds
  to `Int -> ~a: Int -> ~b: Int -> Int` before `use2` checks.
- `use2`'s call checks against `flat=[x,~a,~b]`, so `0`→0, `~b`→2,
  `~a`→1: `CallUnknown` with `ArgToParam=[0,2,1]` (a real
  non-identity mapping on a *value* call).
- `let f`'s `bindSym` unifies the bound type with `?x -> (~a ->
  ~b -> Int)` — clean via labeled-run pairing — and flips `f` to
  `KindFunc` with declared arity 1 (`sig.Params=[x]`).
- `reclassifyCalls` finds `Applied=3 >= Arity=1`, `ArgToParam=[0,2,1]`
  with entries `2,1 >= 1` (params of the *returned* function —
  over-application) → guard nils the mapping → `CallDirect` →
  emitted `f(0)(20)(1)` → prints `19`.

An earlier `e : T` site also works — `(f : Int -> ~a -> ~b -> Int)
0 ~b: 20 ~a: 1` records the same `[0,2,1]` under recovery (the
ascription itself is E0401, but `fn.Type()` is already the ascribed
type when the call checks).

## Why the guard is load-bearing

`CallInfo.Validate` (call.go:~157-169) rejects a non-Go call whose
`ArgToParam` is non-nil while `Applied > Arity`, or any entry
`>= Arity`. The reclassified record above — `CallDirect`,
`Arity=1`, `Applied=3`, `ArgToParam=[0,2,1]` — trips both clauses.
Without the nil-ing, `a.Validate()` returns the error and
`reclassifyCalls` *panics* (`check: reclassified CallInfo for f:
...`) — a compiler crash on a valid program. The guard is what
makes the reclassified record structurally legal.

## Why there is still no divergence

Two independent reasons, either sufficient:

1. **Shared record.** `evalApply` passes `x.Call.ArgToParam`
   straight into `applyArgs` (eval.go:~1088) — the *same* field
   the guard just nil'd, not a preserved pre-reclassification
   copy. Eval therefore binds `f`'s `Remaining()` slots
   positionally too: `0`→`x`, `20`→`~a`, `1`→`~b` → `19`, matching
   the emitted `f(0)(20)(1)`. The TODO's worry — eval consulting a
   mapping the compiler discarded — does not happen: there is only
   one mapping, and both sides read it after the repair.
2. **Same answer as the declared path.** Had `use2` been written
   *after* `let f` — or had `f` been an `fn` decl from the start —
   `checkGomApply` would run with `callee != nil`: `pi >= declArity`
   marks `overDeclared`, `allMapped` fails, `ArgToParam` is never
   set — and the emitted call is the identical positional
   `f(0)(20)(1)`. The guard makes reclassification produce exactly
   the record the real classification would have made. Whether
   `f 0 ~b: 20 ~a: 1` *should* bind `~a`/`~b` by label through the
   returned function is a separate question — that positional
   scramble is the same on both sides of the flip (and on
   already-declared fns), so it is a pre-existing spec question,
   not a reclassification bug.

## Closure

The TODO item can close: the suspected eval/compiled divergence
does not exist — the guard fires (reachable), is load-bearing
(prevents a `Validate` panic), and produces exactly the record the
post-flip checker would have produced. No code change.
