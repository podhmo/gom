# Stdlib `.gomi` type members register like imported-package ones

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-08-stdlib-gomi-type-members-verification.md)）
- 日付: 2026-11-08

## Background

`internal/stdlib`'s builtin-module wiring skipped `iface.MemberType`
when building `resolve.Config.Modules` tables — `List.SomeType`
could not resolve even if a stdlib interface declared it. The
imported-package path (`internal/gomimport`) registers the same
`.gomi` type member in `p.Members` plus the tables its phases
consume, so the two paths skewed — dormant only because no embedded
`.gomi` declares a type today (the gap is recorded in
implementation-gaps).

## Options

- **(i) Register the type symbol only** — a `KindType` entry in the
  module table, nothing else. `Mod.Type` then resolves but
  `Mod.Type.Ctor` reports "has no constructor", labeled payloads
  cannot bind, `match` arms cannot see the variants, and check
  never registers the foreign declaration — a strictly worse skew
  than not resolving.
- **(ii) Full parity: register every table gomimport's extract
  registers.** Member entries (type + variant ctor symbols), the
  type/ctor signatures, `TypeDecls` through the signature source,
  `Ctors`/`CtorFields` through the `Loader` capability seam, and
  the `Types` lowering oracle. Requires replicating gomimport's
  unexported extraction helpers (`requalify`, `fieldTable`,
  `conArgs`, `instantiate`, the `byCon` index) inside stdlib —
  ~200 lines — since gomimport stays unshared.
- **(iii) Defer** until an embedded `.gomi` actually declares a
  type. Keeps the dormant skew; a future stdlib `type` silently
  resolves on one path and not the other.

## Decision

(ii). `internal/stdlib` gained an `env` carrying the same
cross-phase tables a gomimport `Loader` builds — `tdecls`,
`byCon`, `ctorFields`, `typeCtors` — filled by `loadType` for
each `MemberType` the embedded interface declares. `Apply` wires
them through the existing seams, no phase edits:

- `Sigs` chains `memberSigs`, which also answers
  `check.TypeDeclSource` (check.Chain already forwards it) — the
  foreign-type registration an imported package gets.
- `Loader` wraps the caller's with `ctorLoader`, answering
  `CtorsOf`/`CtorFieldsOf` for module types before delegating —
  `build.resolveConfig` probes `Options.Loader` for exactly these
  capabilities, so `pkg.Type.Ctor` and labeled payloads resolve.
- `Lower.Types` chains a `typeOracle` answering variants/fields
  for the modules' types ahead of the imported-package oracle.

The `requalify`/`fieldTable`/`conArgs`/`instantiate` helpers are
replicated, not shared: they are unexported in gomimport and the
type-node sets they walk are fixed (`types.Type`'s five cases,
`check.TypeBody`'s four), so drift costs a line per case. If a
shared `internal` package for .gomi extraction ever forms, this
mirror is the obvious move target.

Module *names* stay resolve's hardcoded builtin set —
`Config.Modules` injects tables only, never names — so a stdlib
type member is reachable only through an existing module
(List/Option/Result/...), which is exactly the intended surface.

## Consequences

- `internal/stdlib/types.go` holds the extraction + seam types;
  `stdlib.go` splits `load` into `loadPackage` (per-interface
  compilation) and `Apply` into `env.apply` so tests wire a
  synthetic env identically.
- `internal/stdlib/types_test.go` loads one synthetic `.gomi`
  through both paths and asserts member/kind/ctor/signature
  parity, plus an end-to-end compile asserting the emitted Go
  names the same constructors.
- The gap-ledger entry on `.gomi` type members being skipped can
  now be marked resolved.
