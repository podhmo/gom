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

`fmt.Errorf` with a `%w` verb works directly — Go-typed arguments
(an `error`, a named Go value) reach `any` parameters unboxed, so
`go vet`'s printf check sees the real `error` and accepts the
wrap; Gom values still box as before.

## Type mapping

| Go | Gom |
| --- | --- |
| `int` / `int64` / `int32` / `float64` | `Int` / `Int` / `Int32` / `Float` |
| `byte` / `uint8` | `Byte` — `List Byte` is `[]byte` (byte I/O); no literals or arithmetic, `==`/`!=` only, convert with `Go.conv` |
| `string` / `bool` / `rune` | `String` / `Bool` / `Char` |
| `*T` | `Ptr T` (nil deref panics, as in Go; ops in the `Ptr` module) |
| `[]T` | `List T` at a signature boundary (param/result/member type — copied both ways); `Go.Slice T` when nested inside another type |
| `map[K]V` | `Go.Map K V` (opaque; `Go.Map.<op>` type-member ops) |
| `chan T` / `<-chan` / `chan<-` | `Chan T` / `RecvChan T` / `SendChan T` |
| `error` | `GoError` |
| `interface{}` / `any` | `Go.Any` |
| named type (`type UserID int64`) | nominal `pkg.UserID`; convert with `Go.conv`; if the underlying is numeric, `+ - * / %` and comparisons work on two values of the same named type, and an untyped literal conforms — `30 * time.Second` is `time.Duration` (mixing with `Int` is E0401, same as Go) |
| named struct | nominal `pkg.T`; exported fields read `v.Field` |
| interface (`io.Reader`) | nominal `io.Reader`; Go-assignable values pass |

- Implicit numeric conversion only where lossless: `Int ↔
  int/int64`, `Float ↔ float64`. Anything else is E0503 — write
  `Go.conv x : Int32` (checked against Go's conversion rules;
  invalid pairs are E0507). A one-field record (`type UserId = {
  value: Int }`) peels to its field type first — `Go.conv u : Int`
  and `Go.conv n : UserId` both work (emits `int64(u)` /
  `UserId(n)`), including imported records and chains. Exception: a
  record whose only field is a bare type parameter (`type Box a = {
  item: a }`) stays a Go struct (`type X[T] T` is forbidden), so no
  peel — `Go.conv b : Int` rejects. What passes is only what stays
  legal with a *named* operand: identical underlying, both-numeric,
  or `Chan`→`SendChan`/`RecvChan` narrowing (never back to `Chan`)
  with one literally-spelled side — List↔Go.Slice copies,
  named-to-named chan direction changes, and interface
  satisfaction still reject.
- `any`-typed parameters accept any Gom value — the only implicit
  boxing site.
- Go interface params accept Go-derived values satisfying `go/types`
  assignability (`*os.File` → `io.Writer`); the same rule applies at
  annotated bindings, returns, and Gom-param boundaries.
- **Records can implement Go interfaces via `@method`**: write the
  receiver first — `@method fn (g: Greeter) string () -> String` —
  and the emit is dual: a plain `string` function (Gom call sites
  stay `string g`) plus a forwarding `func (g Greeter) String()`.
  The record then satisfies `fmt.Stringer`/`error` at interface
  boundaries (`let st : fmt.Stringer = g`). Method names follow the
  Go convention (`string` → `String`) — for names ExportName
  cannot produce (initialisms like `ServeHTTP`, `URL`, `ID`), pin
  the emitted name verbatim: `@method(ServeHTTP) fn (h: Handler)
  serve_http ...`. `Unit` params drop out of
  the method; `Ptr T` receivers land on the pointer only.
  Constraints (E0420): the receiver must be a record of the same
  package (no generics — Go methods take no type args), no
  `@extern`. `Result _ GoError` returns are allowed and unwrap —
  `Result T GoError` emits `(T, error)`, `Result Unit GoError` /
  `Result (Option T) GoError` emit `error`/`(T, error)`, covering
  interfaces like `io.Writer` or `UnmarshalText([]byte) error`;
  a non-`GoError` payload is still E0420.
  Width matters at the boundary: the conformance check compares
  against the interface's raw Go signature, so positions the Go
  side spells `int` want gom `Int` and positions spelled `int64`
  want `Int64` — `io.Seeker`'s `Seek(int64, int)` is
  `(offset: Int64) (whence: Int)`. Writing `Int` at an `int64`
  position fails E0401 instead of passing a method set the
  generated Go would not carry.
- Exported struct fields read via `v.Field` (promoted fields and Go's
  ambiguity rules apply; unexported fields are invisible). Through
  `Ptr T` (T a named Go struct) they also write — `p.Field = v` —
  which with `Ptr.new ()` covers construction like
  `http.Client{Timeout: ...}`; composite literals still don't exist
  (decisions/2026-11-19-struct-field-write).
- Not representable → hidden + reason on use (E0501): unsigned
  ints other than `byte`/`uint8`, `int8`/`int16`, `float32`,
  complex, `[N]T` arrays, `unsafe.Pointer`, generic types, funcs
  with arity > 3 or labeled params, anonymous interfaces with
  methods, 3+ return values. Func params/returns of arity 0–3
  surface as `Go.FuncN` raw views — a `func(...)` param accepts a
  Go-derived func value; pass a Gom fn via `Go.funcN f` (explicit
  adapter, E0510 on non-fn/wrong-arity input). The reverse
  direction is `Go.fnN f` — it turns a `Go.FuncN` value (e.g. a
  callback returned by a Go API) into an ordinary curried Gom fn:
  `func() error` reads as `Result Unit GoError`,
  `func(A) (T, error)` as `Result T GoError`, and a nil-able
  interface payload wraps in `Option` (`(nil, nil)` → `Ok None`).
  A method-less named func type (`context.CancelFunc`,
  `fs.WalkDirFunc`) reads as its `Go.FuncN`, so `Go.fn0 stop` calls
  `WithCancel`'s stop directly and a `Go.func0` value feeds a named
  param; a method-bearing one (`http.HandlerFunc`) stays a named
  type — `Go.fnN` rejects it, its methods work, and `Go.conv f :
  pkg.F` casts a `Go.FuncN` into it.
- Generic Go fns: callable when every constraint is `any`,
  `comparable`, a structural constraint (`S ~[]E`, `M ~map[K]V`),
  or a type-param-free union (`cmp.Ordered` and friends — including
  unions wrapped in a named interface). So `slices`/`maps`/`cmp`
  mostly work now. How they read:
  - `s S` under `S ~[]E` is a `Go.Slice E` parameter — a `List`
    argument crosses the boundary's copy, so the callee can't
    mutate your list; the copy means in-place shapes like
    `slices.Sort xs` warn W0501 (sort the `Go.Slice` itself).
  - Constraint-only type params (`maps.Equal`'s K/V) infer from
    ordinary argument conformance; union terms are judged on the
    emitted Go type — `Int` is `int64`, so `~int64` fits, `~int`
    does not. `~` approx terms also peel declared types that emit
    as defined types (`type UserId = {v: Int}` → `type UserId
    int64` fits `~int64`); exact terms and type-param unions
    reject them.
  - `comparable` folds declared types' emitted shapes: a sum is
    comparable only when every variant's payload is, `Option A`
    when `A` is, `Task` always (handle compare), imported decls'
    fields count too — so `slices.Contains` over a sum that
    carries a `List` payload is E0508, not a runtime panic.
  - A `List` type arg cannot satisfy a slice/map union term
    (`~[]byte`, `[]byte | string`) → E0508 — the output is named
    `list.List[T]` outside the term; write `Go.Slice Byte` where
    you need the anonymous form. Forwarding a Gom-generic's type
    var into a Go generic is also E0508 (no Go repr).
  - Still hidden (E0506): constraints with methods, intersections
    of approx/union terms, unions containing type params,
    recursive `S ~[]S`. Instantiated generic result types like
    `iter.Seq` are a separate gap → E0501 (e.g. `maps.Keys`).
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
  let n2 : Ptr Int = Go.nil ()   // universal spelling — works for Ptr/Chan/Go.Slice/Go.Map/Go.Any/GoError/named Go types
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

## `Go.Any` extraction

```gom
fn extract (x: Go.Any) -> Option Int =
  Go.try_as x : Option Int         // v, ok := x.(int64) → Option
```

`Go.try_as x : Option T` is the only extraction surface from
`Go.Any` (the Go comma-ok type assertion). The target type is the
ascription's `T`; it fails cleanly to `None` on a mismatch (use
`Option.get` to panic). `Go.Any` and type parameters as the target
are E0514.


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

## @extern fn (hand-written Go body)

`@extern` on a `fn` declaration makes it signature-only — `= body`
is forbidden and every parameter plus the result needs an
annotation. Resolution, type checking, `.gomi` conformance, and
call-site output are identical to a normal `fn`; the declaration
just emits no code. The body is a hand-written Go function in the
generated package with the emitted name (`fn map` → `func Map`),
so name/signature drift is reported by `go build`/`go test`, not
Gom. stdlib uses it to give the cons-built combinators buffer
loops (`stdlib/list/impl.go`). Not allowed in `.gomi`
(decisions/2026-11-18-cons-buffer-array-extern-impl.md).

## @extern let + //gom:embed

`@extern` on a `let` marks the binding Go-supplied: the Gom side
declares name and type, the generated file emits nothing for it,
and a hand-written Go `var` in the same package provides the value
(drift is caught by `go build`, same as `@extern fn`).

```gom
//gom:embed hello.txt
@extern let greeting : String

@extern let token : String   // supplied by a Go-side var
```

A `//gom:embed <patterns>` comment immediately above the decl
(same contiguous comment run — a blank line detaches it) emits
`//go:embed` + `var` instead; the type must be `String`,
`Go.Slice Byte`, or a named Go type such as `embed.FS` (E0512).
Named-type admissibility is nominal only — Gom cannot see the
underlying type, so an incompatible named type (e.g. `time.Time`)
passes check and fails at `go build`. Patterns follow Go's quoting
rules — `"a b.txt"` and `` `c d.txt` ``. `_ "embed"` is added
automatically; `import go _ "path"` also works as a general blank
import (decisions/2026-11-23-extern-let-embed.md).

An `@extern let` *without* `//gom:embed` that no Gom code
references emits nothing, so a missing Go-side var goes unnoticed
until some caller or test uses the binding — keep a reference or a
Go-side test for every extern variable. (An embed extern always
emits its `var` — the `//go:embed` directive needs it.)

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
