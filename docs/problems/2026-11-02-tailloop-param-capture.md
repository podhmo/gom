# TailLoop rebinding changes the value a captured parameter reads

- 状態: 解決済み (fixed 2026-11-02 — per-iteration snapshot, see below)
- 日付: 2026-11-02

## Situation

`lower.markTailCalls` (internal/lower/expr.go) rewrites self-tail
calls to `TailCall`, and codegen emits the function body inside
`for { ... }` with the parameters rebound by a multi-assignment
(internal/codegen/emit.go:580, 852). The parameters stay one set of Go
variables for the whole loop.

Closures capture those Go variables by reference. A closure that
captures a parameter therefore observes the parameter's *latest*
value, not the value of the call that created it:

```gom
fn keep (n: Int) (k: Unit -> Int) -> Int = {
  let kk = if n == 5 { fn () -> n } else { k }
  if n <= 0 { kk () } else { keep (n - 1) kk }
}
```

`keep 10 k` should return 5 — the closure bound when `n` was 5. The
generated code returns 0 instead:

```go
func keep(n int64, k func(struct{}) int64) int64 {
	for {
		var kk func(struct{}) int64
		if n == 5 {
			kk = func(_ struct{}) int64 { return n } // reads the shared n
		} else {
			kk = k
		}
		if n <= 0 {
			return kk(struct{}{}) // n is 0 here, not 5
		} else {
			n, k = n-1, kk
			continue
		}
	}
}
```

In the Gom source semantics each call binds a fresh `n`, so the
closure created in the `n == 5` call must see 5 forever. The loop
compilation shares one Go variable across all activations and the
closure reads it after reassignment. (This is the same capture
problem Go itself fixed for `for i, v := range` in Go 1.22.)

Only *parameters* are affected: `let` binders are declared inside the
loop body and are fresh each iteration. A `go { ... }` body capturing
a parameter has the additional problem that the spawned goroutine may
read `n` concurrently with the rebind — a data race, not just a wrong
value.

## Why it is a problem

The conversion is unconditional (every self-tail-recursive function
gets it), so the miscompile fires without any user opt-in. It
contradicts spec 1 item 7's guarantee that self-tail recursion "is
converted to a Go `for`" — the conversion is not semantics-preserving
when a parameter is captured by a closure.

## Resolution

The snapshot option was implemented in `lower.snapshotCapturedParams`
(internal/lower/tail.go), invoked from `markTailCalls` when a function
becomes `TailLoop`. For every parameter referenced under a capture
position — inside a nested `Closure` or `Spawn` — the body is
rewritten: a copy `p' = p` is prepended to `fn.Body.Lets` and *all*
uses of `p` in the body are rebound to `p'` (capture positions and
synchronous positions alike, including `TailCall` arguments). The
parameters keep their symbols: they stay the loop registers the
rebind-and-continue writes, and each copy is a `Let` inside the loop
body — hence a fresh Go variable per iteration. `keep 10 k` now
returns 5, and a `go {}` capture reads a stable per-iteration
variable instead of racing the rebind.

```go
for {
	n1 := n                        // per-iteration copy
	// ... body uses n1, not n ...
	n, k = n1-1, kk                // rebind the registers
	continue
}
```

Only *captured* parameters get a copy, so an ordinary
`fn loop (acc) = ...` keeps the zero-overhead shape. The conversion
stays unconditional, preserving spec 1 item 7's guarantee for every
self-tail-recursive function.

## Possible responses

- Snapshot: when a parameter is referenced from a nested `Closure` or
  `Spawn` inside a `TailLoop` function, lower/copy it to a fresh
  per-iteration variable at the top of the loop body and rebind uses
  (`for { n_ := n; ... }`). Requires renaming inside the body, which
  the lowering pass already does for duplicated match arms (`subst`).
- Reject: make the validator refuse `TailLoop` functions whose
  parameters are captured — an E0901-class diagnostic — until a
  snapshot lowering exists. Sound but rejects useful code
  (`fn loop (acc) = ... f (next acc)` where an innocent closure
  mentions `acc`).
- A `@tailrec` requirement modifier (see
  [decisions/2026-11-02-tailrec-requirement](../decisions/2026-11-02-tailrec-requirement.md))
  could report the capture as a violation of its contract — but the
  bug exists without the modifier, so it deserves its own fix either
  way.

## Related

- internal/lower/expr.go `markTailCalls`, internal/lower/tail.go `snapshotCapturedParams`
- internal/codegen/emit.go `tailCall`, `funcBody`
- docs/spec.md section 1 item 7
