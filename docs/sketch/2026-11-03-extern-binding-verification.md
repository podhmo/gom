# P3-5 extern override binding verification

- 日付: 2026-11-03

Verification for
[decisions/2026-11-03-extern-override-binding.md](../decisions/2026-11-03-extern-override-binding.md):
`extern go` entries bind to the Go member they override, the
declared signature is validated against the raw signature (E0509),
and `Option` over `*T` lowers through `core.OptionFromGo` with a
`v != nil` presence test.

## Method

- `internal/goimport` pipeline tests (`compileSource` +
  `runGenerated`): a `.gom` is carried through
  syntax → resolve → check → pattern → lower → codegen and the
  generated Go is compiled and run in a scratch module — the same
  E2E harness as the other Go-interop cases.
- Binding diagnostics through `compileDiags` (resolve + check only)
  against the real `flag`/`os`/`strconv`/`io` packages.
- Spec fixtures (`testdata/spec/E0509_*`, `ok_extern_flag_lookup`)
  through `build.Compile`; golden output
  (`testdata/golden/extern_flag.golden.go`) reviewed line by line.

## The motivating case

```gom
extern go "flag" {
  fn Lookup (name: String) -> Option (Ptr Flag)
}
```

Before the fix this reported `Ptr takes 3 type argument(s)` +
`expected Ptr String, got String`: inside an `extern go "flag"`
block the unqualified `String` resolved to `flag.String` (a
three-parameter func) and `Ptr`'s arity check blamed that
signature. Now `String` resolves to the builtin (resolve/typex.go
keeps the extern type table to `KindType` members) and a func used
in type position reports `` `Ptr` is a func, not a type `` (E0201)
instead of an arity lie — covered by
`TestExternBuiltinTypeNotShadowedByMember` (resolve) and
`TestGoFuncMemberType` (check).

The declaration compiles clean and generates:

```go
v := flag.Lookup(name)
return option.FromGo(v, v != nil)
```

(`testdata/golden/extern_flag.golden.go`.) Running it prints
`found` for a registered flag and `missing` for an absent one
(`TestE2EExternPtrOption`), including through a `let`-bound
function reference (`let lookup = flag.Lookup` — the adapter
closure carries the same OptionFromGo inside).

## Result shapes exercised end to end

| extern | Go signature | declared view | verified |
|---|---|---|---|
| `flag.Lookup` | `*Flag` | `Option (Ptr Flag)` | present/absent runs; `v != nil` |
| `os.LookupEnv` | `(string, bool)` | `(String, Bool)` | tuple preserved (`TestE2EExternPairTuple`) |
| `os.LookupEnv` | `(string, bool)` | `Option String` | binds OK (comma-ok → `FromGo(v, ok)`) |
| `io.Copy` | `(int64, error)` | `Result Int64 GoError`, `~dst`/`~src` labels | `5\nhello` (`TestE2EExternIoCopy`) — labels reorder to declared positions, `*bytes.Buffer` passes `io.Writer`/`io.Reader` via interface assignability, `Int64` ↔ `int64` conv |
| `strings.Fields` | `[]string` | `List String` | boundary copy `list.FromSlice` (`TestE2EExternSliceBoundary`) |
| `os.Getpid` | `int` | `fn Getpid () -> Int` — zero-parameter spelling | `os.Getpid()` emitted, `int`→`int64` conv (`TestE2EExternZeroArg`) |

Ordinary `(T, bool)` imports are untouched — `TestE2EPairResult`
(plain `import go "os"` tuple shape) still passes; the override
fires only for symbols the resolver bound as extern entries.

## Diagnostics (E0509, all asserted on message substrings)

- `fn Nope` → "names no exported member of Go package `flag`"
- `fn Flag` (a type member) → "is a type, not a function"
- `fn Getpid` variants — `fn Getpid ()` and `fn Getpid (u: Unit)`
  both bind `func() int` cleanly; `fn Atoi ()` over a 1-parameter
  callee still fails view-compat ("declares parameter 0 as Unit")
- `fn Lookup` with two params → "declares 2 parameter(s)"
- `fn Lookup (name: Int)` → "declares parameter 0 as Int"
- `Option` over `string` result → "Option needs a (T, bool) or *T
  result"
- `Result` over `(T, bool)` → "`Option T` or `(T, Bool)`"
- `Option` over `(T, error)` → "`Result T GoError`"
- `fn Lookup (name: a)` → "must be monomorphic"
- `Option Flag` over `*Flag` → "declare `Option (Ptr T)`"
  (the payload keeps the pointer)

A misdeclared extern leaves the declared signature standing:
`flag.Lookup(1)` after the `(name: Int)` mismatch keeps the
declared call shape while E0509 reports the entry
(`TestExternFallbackKeepsDeclaredSig`).

## Both directions

- Gom → Go: all the above; the generated call is the plain
  `flag.Lookup(name)` — no wrapper function.
- Go → generated Gom: `TestE2EExternGoCaller` drops a `caller.go`
  next to the generated `main.go` and calls `lookupFlag` — the
  Go-side caller reads `option.Option[*flag.Flag]` through the
  stdlib type switch, exactly the readable-API contract.

## Not covered (out of scope, documented in the decision)

- Extern overrides for vars/consts/methods/types — rejected.
- Extern over generic Go functions — rejected ("cannot be
  overridden yet").
- `Option` over `(T, bool)` where the payload needs a numeric
  conversion is accepted (e.g. `Option Int` over `(int, bool)`)
  but not exercised E2E — the conversion rides the same
  `OptionFromGo.ValConv` machinery `ResultFromGo` already uses.
