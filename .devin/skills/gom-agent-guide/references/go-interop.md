# Gom ↔ Go interoperability

Condensed from `docs/spec.md` chapter 8. `import go "pkg"` reads the
package's types automatically via the Go toolchain — no declarations,
no bindings files.

## Calling Go

```gom
package main

import go "fmt"
import go "os"
import go "strconv"
import go "strings"

fn parse_sum (a: String) (b: String) -> Result Int GoError =
  strconv.Atoi a |?> fn x -> strconv.Atoi b |?> fn y -> Ok (x + y)

fn report () -> Unit = {
  let (home, ok) = os.LookupEnv "HOME"   // (T, bool) → tuple (T, Bool)
  if ok { ignore (fmt.Println "home: ${home}"); () } else { () }
  let shout = strings.Repeat (_, 3)      // `_` placeholder: one-hole fn
  ignore (fmt.Println (shout "go"))
  ()
}
```

Key rules:

- A Go function of n params looks like a Gom function taking **one
  n-tuple**: `f(a, b)` *is* the juxtaposed application `f (a, b)`.
  `strconv.Atoi(s)` and `strconv.Atoi s` are the same call. Zero-arg:
  `time.Now()` = `time.Now ()`.
- Go calls are **not curried** — no partial application; a missing
  labeled param on a Go call is E0404, not a closure.
- Go functions are values: `List.map strconv.Itoa xs`-style works
  (once `List.map` exists).
- Variadic: `fmt.Println(a, b, c)` takes any count; spread a list
  into the tail with `f(..xs)` — `..xs` must be the **last** argument
  (E0505). A variadic call through `|>`/`<|` passes one element, not
  the argument list.
- Methods: `x.M(args)` on Go-typed values only (Gom types have no
  methods — E0502/E0401). Receiver comes from the Go value.
- Go package vars/consts resolve too: `math.Pi`, `os.Args` —
  untyped constants adopt the expected Gom type.

## Result mapping — the important one

| Go signature | Gom view |
| --- | --- |
| `func f(a A, b B) R` | `(A, B) -> R` |
| `func f(...) (T, error)` | `... -> Result T GoError` |
| `func f(...) (T, bool)` | `... -> (T, Bool)` |
| `func f(...) error` | `... -> Result Unit GoError` |
| `func f(...) (A, B)` (other multi-value) | `... -> (A, B)` |

A `(T, error)` call in `-> Unit` position still *produces* a
`Result` — discard with `ignore` or handle with `try`/`|?>`/`match`;
the Result is a real value, not an annotation.

```gom
import go "strconv"

fn parse (s: String) -> Result Int GoError =
  strconv.Atoi s |?> fn n -> Ok (n * 2)   // failure wraps "strconv.Atoi: %w"
```

## Type mapping

| Go | Gom |
| --- | --- |
| `int` / `int64` / `int32` / `float64` | `Int` / `Int` / `Int32` / `Float` |
| `string` / `bool` / `rune` | `String` / `Bool` / `Char` |
| `*T` | `Ptr T` (nil deref panics, as in Go) |
| `[]T` | `Go.Slice T` today — spec intends `List T` + boundary copies ([gap](../../docs/implementation-gaps.md)) |
| `map[K]V` | `Go.Map K V` (opaque; `Go.Map` module ops) |
| `chan T` / `<-chan` / `chan<-` | `Chan T` / `RecvChan T` / `SendChan T` |
| `error` | `GoError` |
| `interface{}` / `any` | `Go.Any` |
| named type (`type UserID int64`) | nominal `pkg.UserID`; convert with `Go.conv` |
| named struct | nominal `pkg.T`; exported fields read `v.Field` |
| interface (`io.Reader`) | nominal `io.Reader`; Go-assignable values pass |

- Implicit numeric conversion only where lossless: `Int ↔
  int/int64`, `Float ↔ float64`. Anything else is E0503 — write
  `Go.conv x : Int32` (checked against Go's conversion rules;
  invalid pairs are E0507).
- `any`-typed parameters accept any Gom value — the only implicit
  boxing site.
- Go interface params accept Go-derived values satisfying `go/types`
  assignability (`*os.File` → `io.Writer`); the same rule applies at
  annotated bindings, returns, and Gom-param boundaries. Gom types
  cannot implement Go interfaces in v1.
- Exported struct fields read via `v.Field` (promoted fields and Go's
  ambiguity rules apply; unexported fields are invisible). Field
  writes and composite literals don't exist yet.
- Not representable → hidden + reason on use (E0501): `uint8`–`uint64`
  /`byte`, `float32`, complex, `[N]T` arrays, `unsafe.Pointer`,
  generic types, generic fns with non-`any`/`comparable` constraints
  (see `docs/problems/2026-11-04-generic-go-fns-hidden.md`), funcs
  taking/returning func types, anonymous interfaces with methods.
- `error` return values become `GoError`; call `.Error ()` for the
  message or wrap `fmt.Errorf`-style via `|?>`'s auto-wrap.

## `_` placeholder (cute)

```gom
import go "strconv"
import go "strings"

let shout = strings.Repeat (_, 3)          // fn s -> strings.Repeat(s, 3)
let parse_i64 = strconv.ParseInt (_, 10, 64)
```

- Exactly one `_` per call, Go calls only (E0504).
- Non-`_` arguments are evaluated **once at function-creation time**
  (Scheme `cute` semantics) — not per call.

## extern (escape hatch, P3-5 pending)

```gom
package main

extern go "io" {
  fn Copy (~dst: Writer) (~src: Reader) -> Result Int64 GoError
}

fn main () -> Unit = ()
```

`extern go "pkg" { fn sig }` overrides the imported signature —
declared conversion rules include comma-ok `(T, bool)` → `Option T`
and `*T` → `Option (Ptr T)`. The syntax parses and checks today;
treat the declared-shape semantics as in-flight and prefer the
automatic mapping plus `Go.conv` where possible.

## nil and contract boundaries

- Go-side `nil` on `*T`/interface values panics on use, Go-style —
  Gom doesn't add null checks.
- A Gom ADT value received from Go holding `nil`/an invalid variant
  panics at `match` with a type-named message (`rt.InvalidVariant`).
- `List`↔slice copies at the boundary are spec'd both ways (the
  `Go.Slice` view above is today's reality); `Go.Slice` is the
  explicit no-copy mutable escape via `extern`.
- Generated `.go` is a normal package: Go code calls Gom functions
  directly; ADTs surface as sealed interfaces (type-switch them);
  `Result` is a tagged struct via `Ok`/`Err`/`Tag()`/`Unwrap()`.
