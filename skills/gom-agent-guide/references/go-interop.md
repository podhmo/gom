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
- Go functions are values: `List.map strconv.Itoa xs`-style works.
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
| `func f(...) error` | `... -> Result Unit GoError` — unless a declared view applies: `GoError` (never-nil claim, panic-checked at the boundary) or `Option GoError` (nil-able value). `errors.New`/`fmt.Errorf` are `GoError`, `errors.Join`/`errors.Unwrap` are `Option GoError` by standard binding |
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
| `*T` | `Ptr T` (nil deref panics, as in Go; ops in the `Ptr` module) |
| `[]T` | `List T` at a signature boundary (param/result/member type — copied both ways); `Go.Slice T` when nested inside another type |
| `map[K]V` | `Go.Map K V` (opaque; `Go.Map.<op>` type-member ops) |
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
- `error` return values become `Result Unit GoError` (or `GoError`/
  `Option GoError` under a declared view — see the result table);
  call `.Error ()` for the message or wrap via `|?>`'s auto-wrap.

## `Ptr` module (pointer ops)

```gom
fn ptr_ops () -> Int = {
  let p : Ptr Int = Ptr.make 1   // pointer to a fresh copy — set never reaches the arg
  Ptr.set p (Ptr.get p + 1)      // *p = v / *p — nil panics, Go-style
  let t : Ptr Int = Ptr.new ()   // new(T) — out-pointer APIs (errors.As targets, decode destinations)
  let n : Ptr Int = Ptr.nil ()   // nil — optional *T params (tls.Dial's *tls.Config)
  if Ptr.is_nil n {              // p == nil; to_option is the safe read (nil -> None)
    Ptr.get p + Ptr.get t
  } else {
    0
  }
}
```

`p1 == p2` on `Ptr` is Go pointer identity; nil tests go through
`is_nil`/`to_option`. Passing a `Ptr` to an `any`/`Go.Any`
parameter is verbatim — `Ptr.new ()` + an annotation is the
`errors.As` target shape.

## `Go.Map` ops

```gom
fn map_ops () -> Int = {
  let m = Go.Map.make ()           // make(map[K]V) — never nil
  Go.Map.set m "k" 3               // m[k] = v — nil map panics, like Go
  let v = match Go.Map.get m "k" { // v, ok := m[k] — nil map ok
    Some x -> x
    None -> 0
  }
  Go.Map.delete m "k"              // nil map is a no-op
  v + Go.Map.length m              // len(m) — nil map is 0; to_list snapshots pairs
}
```

`k` is `comparable` — a `List` key (or any non-comparable Go
representation) is E0508. Nil-map semantics are Go's: get/delete/
length work, set panics.

## `Go.Slice` ops (read-only)

```gom
fn slice_ops (gs: Go.Slice Int) -> Int = {
  let n = Go.Slice.length gs            // len(s) — nil is 0
  let first = match Go.Slice.get gs 0 { // None on out-of-bounds / negative
    Some v -> v
    None -> 0
  }
  let tail : Go.Slice Int =
    Go.Slice.slice gs 1 n               // s[lo:hi] — shared view, panics on bad bounds
  let total = Go.Slice.fold tail (fn acc v -> acc + v) 0  // left fold; nil -> acc
  first + total + List.length (Go.Slice.to_list tail)     // snapshot into a List
}
```

Every member is a read — `to_list` is the only one that copies.
`for x in gs { ... }` iterates a `Go.Slice` directly — index order,
nil iterates zero times, same as Go's `range []T` — so
`Go.Slice.fold` / `Go.Slice.to_list` are needed only for the fold
result or a snapshot. The exclusions: `Go.Map` stays E0401, named
Go slice types (`Go.Named`-style handles) are out of scope, and
`Go.String`/`Go.Bytes` iteration is deferred. To mutate, do it on
the Go side or copy into a `List`; `Go.conv` between `List` and
`Go.Slice` copies both ways (the same semantics as the implicit
boundary), so `Go.Slice`-to-`Go.Slice` forwarding is the only
no-copy path.

## Copied-`List` warning (W0501)

A `List` crossing into a `[]T` parameter copies — so a Unit-result
Go call that could only matter by mutating that parameter
(`sort.Strings`) has no effect on the Gom value, and W0501 warns
at the argument. Read-only consumers return values
(`strings.Join`), so they stay silent; `signal.Notify`-style
Unit-but-non-mutating calls can still warn (wording is
conditional). The escape hatch is verbatim `Go.Slice`: declare the
parameter `Go.Slice` via `extern` and pass a `Go.Slice` value, or
spread/fill with `..gs` / `...[]T` element — bare `Go.Slice`
arguments never copy. A `List` argument warns even into a
`Go.Slice`-declared parameter (it still copies).

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

## extern (signature override)

```gom
package main

extern go "io" {
  fn Copy (~dst: Writer) (~src: Reader) -> Result Int64 GoError
}

fn main () -> Unit = ()
```

`extern go "pkg" { fn sig }` overrides one exported **function**
member's imported signature. The declaration is validated against
the raw Go signature (mismatch → E0509): same parameter count,
compatible types per position, monomorphic only, and a result the
raw shape admits — `(T, bool)` → `Option T` or `(T, Bool)`, `*T` →
`Option (Ptr T)` (payload keeps the pointer), `(T, error)` →
`Result T GoError`, a lone `error` → `Result Unit GoError` (the
default), `Option GoError` (nil-able value, same
`option.FromGo(v, v != nil)` materialization), or `GoError` (a
never-nil claim — the call emits `if v == nil { panic(...) }`), and
`(A, B)` → `(A, B)`. The generated call stays `pkg.F(args)`;
`Option` over `*T` materializes as `option.FromGo(v, v != nil)`.
Type/var/const/method members cannot be overridden; generics
neither. A zero-parameter Go function is declared `fn F () -> T`
(or `(u: Unit)`), and the call spells `pkg.F ()` like any Go call.

**Standard bindings** are compiler-curated declared views through
the same admission: `errors.New` and `fmt.Errorf` are `-> GoError`,
`errors.Join` and `errors.Unwrap` are `-> Option GoError`. An
explicit extern wins — `extern go "errors" { fn New (msg: String)
-> Result Unit GoError }` restores the default. (P3-5 +
error-result-bindings — see
docs/decisions/2026-11-03-extern-override-binding.md and
docs/decisions/2026-11-05-error-result-bindings.md.)

Evaluator note: `gom test`/eval does not run the Go side, so an
extern-bound member still needs a stub — and the stub returns the
*declared* shape directly. For `Option`/`Result` overrides that
means a `Some`/`None`/`Ok`/`Err` variant value (`VCtor`), never a
raw pointer or `(T, error)` pair; eval has no conversion path for
extern result rewrites.

## nil and contract boundaries

- Go-side `nil` on `*T`/interface values panics on use, Go-style —
  Gom doesn't add null checks.
- A Gom ADT value received from Go holding `nil`/an invalid variant
  panics at `match` with a type-named message (`rt.InvalidVariant`).
- Top-level `[]T` parameters and results copy to/from `List` at the
  boundary; a `[]T` nested inside another type stays `Go.Slice T`
  (no copy). `Go.Slice` is the explicit no-copy mutable view —
  `Go.conv` between `List` and `Go.Slice` copies both ways like
  the implicit boundary, so only `Go.Slice`-to-`Go.Slice`
  forwarding shares backing. Read a `Go.Slice` in place with the
  `Go.Slice.<op>` members above.
- Generated `.go` is a normal package: Go code calls Gom functions
  directly; ADTs surface as sealed interfaces (type-switch them);
  `Result` is a tagged struct via `Ok`/`Err`/`Tag()`/`Unwrap()`.
- At that generated API surface (Go calling Gom) the boundary places
  no copies: `list.List[A]` args and results share backing with the
  caller, and an exported `let` appears as a writable Go package `var`.
  The contract (spec §8.7) forbids Go from writing elements or
  rebinding — violations are undiagnosed, like OCaml's FFI posture.
