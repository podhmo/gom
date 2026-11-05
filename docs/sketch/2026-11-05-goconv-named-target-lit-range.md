# `Go.conv` literal at a named Go target — oracle verification

Non-normative record of closing the wave8 residual: a `Go.conv`
literal operand at a *named Go* target skipped the range judgment
(`litRange` returned not-ok for `Qual != ""`), so
`Go.conv 5000000000 : os.FileMode` was accepted and emitted
`fs.FileMode(5000000000)` — a constant conversion Go rejects.

Decision: [2026-11-05-int-literal-range-check](../decisions/2026-11-05-int-literal-range-check.md)
("the judgment is at the conversion's target"; named targets were the
accepted residual). Spec: `docs/spec.md` 3.3 (E0417 tail).

## Mechanism

- `check.UnderlyingSource` (`internal/check/env.go`) — optional
  protocol, same shape as `SignatureSource`/`ConvertSource`:
  `GoUnderlyingKind(t types.Type) (gotypes.BasicKind, bool)`.
- `goimport.Loader.GoUnderlyingKind` (`internal/goimport/underlying.go`)
  — resolves `*types.Go` via the loader's `named` map (only types the
  file actually imported), reads `(*types.Named).Underlying()`'s basic
  kind. go/types resolves the underlying recursively, so
  `type M = fs.FileMode`-style aliases in the imported package and
  Gom's own `type D = time.Duration` aliases both land on the basic
  kind.
- `gomimport.Loader` forwards the protocol from `Config.Sigs` (the
  checker is always configured through this wrapper — without
  forwarding the oracle is unreachable).
- `litRange` (`internal/check/expr.go`) — for a `*types.Go` judgment
  type it consults the oracle and maps the basic kind to
  `(bits, unsigned, floating)`; `int`/`uint`/`uintptr` read as
  64-bit (the spec reads `Int` as int64; `Go.conv lit : unsafe.Pointer`
  targets were already rejected by E0507 so pointer kinds are moot).
  Unknown/unavailable kinds stay skipped — a target the oracle cannot
  classify (struct, interface, function underlying, or a Go type the
  importer never loaded) falls back to the literal's own-type
  judgment, exactly the old behavior for everything else.
- `watchLit` also watches `LitChar`: `'𐀀'` is a character literal
  whose emitted form is the rune constant `65536` — against a named
  integer target it needs the same range check as an int literal
  (`Go.conv '𐀀' : syntax.Flags` — Flags is `uint16`).
- Bounds in `revisitLitRanges` split on `unsigned`:
  `[-2^(bits-1), 2^(bits-1)-1]` vs `[0, 2^bits-1]`. The float
  integer-target rule (integral + in-range, via `constant.ToInt`) is
  unchanged.

## Verified

Direct observation (probe + `testdata/spec/` fixtures):

| spelling | kind | result |
| --- | --- | --- |
| `Go.conv 5000000000 : os.FileMode` | uint32 | E0417 overflows io/fs.FileMode |
| `Go.conv 1.5 : time.Duration` | int64 | E0417 not an integer |
| `Go.conv 1e100 : time.Duration` | int64 | E0417 overflows |
| `Go.conv 300 : syntax.Op` | uint8 | E0417 overflows regexp/syntax.Op |
| `Go.conv '𐀀' : syntax.Flags` | uint16 | E0417 overflows regexp/syntax.Flags |
| `Go.conv 4000000000 : os.FileMode` | uint32 | ok — exceeds int32, fits uint32 (proves unsigned) |
| `Go.conv 4294967295 : os.FileMode` | uint32 | ok — uint32 max |
| `Go.conv 2.0 : time.Duration` | int64 | ok — integral float |
| `Go.conv 255 : syntax.Op` / `'a'` | uint8 | ok |
| `Go.conv -1 : os.FileMode` | uint32 | E0417 — negative at unsigned |
| `Go.conv 5 : time.Duration` | int64 | ok (pre-existing fixture) |

Emitted `T(lit)` for each accept verified `go build`-clean in a
scratch module.

Non-targets unchanged: `Go.conv lit : local-ADT` still E0507-only
(gomOnly target — `litRange` finds the Con, not a `*types.Go`);
annotation-position `let a : os.FileMode = 5000000000` still E0401
(the literal's own type can never *be* a `*types.Go` — the `num`
constraint rejects it; only `pl.conv` carries the named target into
`litRange`).

## Residual

- Go named types with non-numeric underlying (`struct`, `interface`,
  func, `complex`, `string`, `bool`): no range judgment — the
  oracle's kind answers not-ok and the literal judges at its own
  type. `convOK`'s `go/types.ConvertibleTo` arm still decides the
  conversion itself, and Go's own compiler would reject a spelled
  constant that cannot convert; nothing new is lost.
- `float32` underlying judges at 32-bit representability
  (`constant.Float32Val` + `IsInf`), matching how `Float` judges at
  float64.
