# Gom concurrency

Condensed from `docs/spec.md` chapter 10. Gom uses Go's goroutines
and channels directly; the language adds exactly three pieces of
syntax: the `go` expression, channel types, and the `select`
expression.

**Implementation status:** the syntax, channel types, `select`,
`for x in ch`, and the channel/task helpers (`Chan.make`,
`Chan.send`, `Chan.recv`, `Chan.close`, `Chan.recvOnly`,
`Chan.sendOnly`, `Task.spawn`, `Task.await`) are implemented — they
forward to `rt` helpers and work in compiled programs and `gom
test`. The evaluator stays compiled-only for `go`/`select`/channel
`for` (E9001).

## `go { }`

```gom
import go "fmt"

fn worker (id: Int) -> Unit = { ignore (fmt.Println "id ${id}"); () }

fn launch (id: Int) -> Unit = {
  go { worker id }
  ()
}
```

`go { e }` evaluates `e` on a new goroutine; the expression has type
`Unit`, so `e` must be `Unit` (E0701) — return results through a
channel or `Task`. A `go` block is **not** a `try` boundary (E0702):
wrap the body in a lambda or `Task.spawn` to `try` inside it.

## Channel types

`Chan a` (bidirectional), `RecvChan a`, `SendChan a`. Restrict
direction with `Chan.recvOnly` / `Chan.sendOnly` — required at
Gom-call boundaries; when passed to a *Go* function, channels
convert implicitly by Go's assignability rules.

Stdlib surface: `Chan.make n` (`n = 0` unbuffered),
`Chan.send ch v : Unit`, `Chan.recv ch : Option a` (`None` once
closed), `Chan.close ch`, `Task.spawn : (Unit -> a) -> Task a`,
`Task.await : Task a -> a` (re-panics a captured panic).

## `select`

```gom
type Stop =
  | Closed
  | Timeout

fn next (jobs: Chan Int) (ticker: Chan Int) -> Result Int Stop =
  select {
    r <- jobs -> match r {
      Some job -> Ok job
      None -> Err Closed
    }
    _ <- ticker -> Err Timeout
  }
```

- `select` is an **expression** — every arm returns the same type.
- Arm kinds: receive `x <- ch -> e`, send `send ch v -> e`,
  `default -> e` (at most one — E0703; `select {}` is E0705).
- The receive arm's LHS is a variable or `_` only (E0704) — no
  destructuring. `x` gets `Option a`: a closed channel yields `None`.
  Branch on the value with ordinary `match` in the body — closed-
  channel handling is then covered by normal exhaustiveness.
- Evaluation order is Go's: on entry, every arm's channel expression
  and send value is evaluated once in source order — including arms
  that are not selected.
- The same channel may appear in several arms (distinct `case`s, as
  in Go). `time.After d`, `ctx.Done()` — `RecvChan`s from Go — drop
  straight into arms.
- An empty `select {}` would block forever; it is rejected.

## `for x in ch`

```gom
fn drain (ch: Chan Int) -> Unit = {
  for x in ch { ignore x }   // receives until the channel is closed
  ()
}
```

Also iterates a `List` or a `Go.Slice`. `Unit` body only.

## Sharing rules (linter warnings)

Immutable Gom values send freely. The linter warns (W0701/W0702,
planned) when a channel's element type contains mutable values
(`Ref`, `Array`, `Ptr`, `Go.Slice`, `Go.Map`) or a `go` block
captures such a variable. Uncaught panics in a non-`Task` goroutine
stop the whole program; `Task` re-panics at `await`.
