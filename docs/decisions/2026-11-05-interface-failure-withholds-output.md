# `gom build` withholds a failed package's generated output

- 状態: draft（prototype verified — [sketch: V-6 residual verification](../sketch/2026-11-05-v6-residual-verification.md)）
- 日付: 2026-11-05

## Context

`.gomi` conformance (spec §7.4–7.6) runs *after* compilation and
codegen — the interface check needs the whole package's typed
surface. When it reported E0601 (missing `.gomi`) or E0603 (impl ↔
interface mismatch), `gom build` exited 1 **but had already written
the `.go` files**: the build directory was left holding generated
code for a package whose contract could not be verified — a stale
artifact `go build` would happily compile, masking the failed
boundary.

## Decision

`checkInterfaces` returns the `build.Result`s of every failed
package group, and `runBuild` skips writing their outputs. The
withhold is **package-granular and all-or-nothing**: one E0601/E0603
in a package withholds every member file's output — a package whose
interface is unverifiable must not exist as a half-written artifact,
matching the atomicity the unit decision gives pattern/lowering
failures. A clean package in another directory still writes its
outputs in the same run.

Consequences:

- The emitted "wrote N file(s)" summary counts only files actually
  written — a fully withheld package reports its diagnostics, not a
  write.
- `-o` usage validation runs *before* compilation and withholding:
  `gom build -o out.go a.gom b.gom` is a usage error (exit 2)
  regardless of what would have been withheld.
- `gom check` is unaffected — it never wrote outputs.

## Alternatives

- *Write then delete on failure.* Rejected: two failure modes
  (partial writes, deletion errors) and a window where bad Go
  exists on disk; not writing is the same atomicity the unit
  already uses for compile failures.
- *Withhold only on E0603, still write on E0601.* Rejected: a
  package with no interface at all has no verified contract —
  same boundary failure, same treatment.
