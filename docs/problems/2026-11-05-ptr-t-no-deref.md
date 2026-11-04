# `Ptr T` is write-only: no dereference, no nil, no construction

- 状態: 未決
- 日付: 2026-11-05 (found by the P3-7 real-program audit —
  [sketch](../sketch/2026-11-05-p3-7-realworld-interop-audit.md))

## Situation

The automatic `*T` → `Ptr T` mapping imports Go pointers into Gom,
but the language offers no operation on `Ptr T` at all. There is no
`Ptr` module (`builtinGoMembers` lists only `conv`/`Slice`/`Map`),
no dereference syntax, no `nil` literal, and no `new` equivalent.
Field reads (through `*T` struct fields, P3-4) and method calls
resolve by peeling the `Ptr`, so reading the *contents* of a pointer
under Go's rules works — but the pointer value itself cannot be
inspected, produced, or compared.

Consequences hit in real programs:

- **flag values are unreadable**: `flag.String`/`flag.Int` return
  `*string`/`*int`, and without a dereference there is no way to
  extract the bound value. (`flag.Int` additionally dies on E0501 via
  the recorded int/int64 container split; `flag.Int64` works.) The
  workaround is `flag.Lookup name` → `f.Value.String ()` — the
  `greet` fixture uses this shape.
- **nil can neither be written nor detected**: when a `*T`-returning
  Go function may return nil, `f == nil` cannot be expressed. The
  `extern` `*T` → `Option (Ptr T)` override is the only nil-safe path
  (literally the spec 8.4 example). Conversely, optional `*T`
  parameters (`tls.Dial`'s `*tls.Config`, etc.) cannot receive nil —
  neither a value nor nil can be constructed, so those Go functions
  are simply uncallable.
- **pointer-in APIs compile but cannot be used**:
  `json.Unmarshal`, `json.Decoder.Decode`, `errors.As`, `flag.Var`,
  the `fmt.Fscan` family are designed around caller-supplied `*T`s.
  They compile (an `any` parameter admits `Ptr`), but no useful
  target can be produced. `errors.As (e, os.Stdout)` compiles and
  panics at runtime ("errors: *target must be interface or implement
  error") — APIs whose contract the type system cannot state are
  exposed anyway.
- **no construction either**: with composite literals unsupported,
  there is no `new` to build a `*T` for an out-pointer API. The only
  way to obtain a `Ptr` is a Go function return (`flag.String`,
  `os.Open`, `http.NewRequest`, `bytes.NewBufferString`).

## Why it is a problem

Go API design leans heavily on out-pointers (decode/unmarshal/scan/
as, optional `*T` parameters, writable flag values). With `Ptr`
unreadable, all of these are either "callable but unusable" or
"uncallable". Spec 8.3's `*T` → `Ptr T` row guarantees the type
passes through, but no meaning crosses. The v1 minimum — Go returns
a `*T`, Gom hands it to the next Go call — does hold, so urgency is
moderate; but how to treat APIs like `errors.As` that pass type
checking yet can only panic at runtime needs a separate decision
(hide them, transcribe the contract into types, or accept the
exposure).

## Direction notes

- A `Ptr` member table (`Ptr.get` / `Ptr.set` / `Ptr.is_nil` /
  `Ptr.make`?) or a `Go.deref` builtin — same family of pending
  decisions as the still-unimplemented `Ref`/`Array` member tables.
- Whether `*T` results default to `Option (Ptr T)` is bound up with
  the open-questions "*T デフォルト" entry — Option-ifying does not
  by itself make the pointee readable.
- Contracts like `errors.As`'s "an `any` that must be a non-nil
  pointer" cannot be written in the type system. If targets are
  limited to `Ptr`s obtained from Go returns, options include a lint
  for "`any` arguments that demand `Ptr`", or standard bindings in
  the shape `fn As (err: GoError) (target: Ptr T) -> Bool` — though
  the element type is chosen by the caller, which exceeds what extern
  matching validates today (needs study).

Related: open-questions "*T default",
[goerror-construction](2026-11-04-goerror-construction.md),
[extern binding decision](../decisions/2026-11-03-extern-override-binding.md).
