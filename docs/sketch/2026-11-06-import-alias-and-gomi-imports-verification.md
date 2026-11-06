# Verification: `import alias "path"` and `.gomi` imports

- Date: 2026-11-06
- Decision: [gom-import-alias-and-gomi-imports](../decisions/2026-11-06-gom-import-alias-and-gomi-imports.md)

Prototype checked out under a scratch module `example.com/t` (outside
the repo). Built `gom` at this branch and ran `check` / `build`.

## Alias — Go import

```gom
import go f "fmt"
import go s "strconv"
fn main () -> Unit = { ignore (f.Println(s.Itoa(42))) }
```

`gom check` clean; generated Go carries the aliases verbatim:

```go
import (
	f "fmt"
	"github.com/podhmo/gom/stdlib/result"
	s "strconv"
)
```

## Alias — Gom package import

`lib/lib.gom` exports `double`, `answer`, `Color` via `lib.gomi`.
`app/main.gom` does `import l "example.com/t/lib"` and calls
`l.double l.answer`. `gom build` generates `import l
"example.com/t/lib"` and `l.Double(l.Answer)` — the alias flows
through resolve into the emitted import name.

## `.gomi` imports

`mid/mid.gomi`:

```gom
package mid
import "example.com/t/lib"
import go "io"
fn quadruple (x: Int) -> Int
fn tinted (c: lib.Color) -> lib.Color
fn reader (r: io.Reader) -> Unit
```

- Both Gom-package and `import go` names resolve inside the
  interface's signatures (no unresolved-name diagnostics).
- Conformance works both ways: omitting `tinted` from `mid.gom`
  reports E0603 `interface member `tinted` has no implementation`;
  with the impl present the package checks and builds.
- A consumer `use/main.gom` does `import "example.com/t/mid"` and
  `mid.quadruple 5` — it never names `lib` or `io`; the alias names
  stay interface-internal as specified.

## Cycles

Two packages whose `.gomi` files import each other (and whose
`.gom` files do the same) produce E0606 with the cycle path on both
the `.gom` and `.gomi` import positions:

```
import cycle: example.com/t/c1 -> example.com/t/c2 -> example.com/t/c1
```

## Corrections after review (F-1, F-2)

Two claims above needed qualification against codegen reality:

- **Bound-name → emitted alias is conditional.** `render`
  (`internal/codegen/codegen.go`) emits `%q` unqualified when the
  bound name equals the path's last segment. Reproducer —
  `mismatch/mismatch.gom` declares `package other` with
  `other.gomi`; `app2/main.gom` does
  `import "example.com/t/mismatch"` and calls `mismatch.double`:

  ```go
  import (
      "example.com/t/mismatch"   // unqualified — Go binds `other`
  )
  mismatch.Double(3)             // vet: undefined: mismatch
  ```

  `go vet` reports `"example.com/t/mismatch" imported as other and
  not used` + `undefined: mismatch`. `import other
  "example.com/t/mismatch"` is the workaround. Unconditional
  bound-name aliasing (or piping the real package name to codegen)
  is follow-up work.

- **Pure `.gomi`↔`.gomi` cycles are E0203, not E0606.** The impl
  scan walks `*.gom` imports only; an interface-only cycle hits the
  loader's in-flight guard → `cannot resolve import` E0203 with a
  nested "does not check" reason, no cycle path:

  ```gom
  // ga/a.gomi                      // gb/b.gomi
  import "example.com/t/gb"        import "example.com/t/ga"
  fn uses_b (v: gb.BVal) -> gb.BVal fn uses_a (v: ga.AVal) -> ga.AVal
  ```

  (`a.gom`/`b.gom` do not import each other — no impl edges.)
  `gom check ga gb` reports E0203 on both `.gomi` import positions.
  Mixed cycles that do run through `.gom` edges keep E0606 with the
  cycle path (the c1/c2 check above).

## Spec suite pinning

- `testdata/spec/ok_import_go_alias.gom` — `import go s "strconv"`
  + `s.Itoa`, `{"diagnostics": []}`.
- `testdata/spec/ok_sibling_gomi_import/` — directory case: `m.gomi`
  imports `go "io"` for `fn reader (r: io.Reader) -> Unit`, `a.gom`
  implements it, `{"diagnostics": []}`.

`go test ./internal/gomtest ./cmd/gom` and the spec-suite consumers
(`internal/build`, `internal/syntax`, `internal/pattern`,
`internal/stdlib`) all pass with the fixtures added.
