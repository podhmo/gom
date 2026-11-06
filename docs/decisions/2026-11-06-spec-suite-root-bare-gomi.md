# A bare .gomi at the spec-suite root is a load error

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-spec-suite-root-bare-gomi-verification.md)）
- 日付: 2026-11-06

## Context

[spec-suite-root-bare-side-files](2026-11-06-spec-suite-root-bare-side-files.md)
made a bare side file (`expected.json` / `golden.go` / `case.json`)
directly under a suite root a load error. A `.gomi` directly under
the root has the same shape of hole: it is not a `.gom` file (so it
never starts a single-file case), it is not a side file, and the
walk never descends into a case directory — inside one, `dirCase`
collects it as a member. A root-level `.gomi` therefore takes part
in nothing and was silently dropped.

## Options

- **(i) Report it as a load error** — the same synthetic-`loadErr`
  channel the bare side files use.
- **(ii) Document the ignore.** The file is harmless; a stray
  interface file just does nothing.
- **(iii) Treat `x.gomi` as a side file of file case `x.gom`** —
  let a single-file case carry an interface.

## Decision

Option (i): report it. A `.gomi` is a member of a directory case
and nothing else in suite semantics; a stray one at the root almost
always means a directory case that lost its marker, or an interface
file misplaced next to a single-file case. Silently accepting it
hides the mistake exactly like the bare side files did.

Option (ii) was rejected for the same reason as before: no layout
makes a root-level `.gomi` meaningful. Option (iii) was rejected:
interface membership is what directory cases are for — giving file
cases a second member channel would add a layout rule without a
need, and `x.gom` + `x.gomi` would silently change what compiles
where today nothing compiles.

Boundary deliberately kept: a `.gomi` inside a *non-case*
subdirectory stays ignored. Namespace directories may hold helper
files that take no part in the suite (the `_`-prefix convention is
for helpers, but dropping an inert file there hides no marker —
unlike the root, where the file sits where cases live).

Details:

- The check is a new branch in `discoverCases`'s file walk: a
  `*.gomi` whose parent is `suiteDir` becomes a `testCase` with
  `loadErr` naming the file; discovery order and orphan reporting
  are unchanged.

## Consequences

- `testdata/<suite>/x.gomi` now fails the suite with a `gomtest:`
  load error instead of passing unnoticed.
- `.gomi` members inside directory cases are unaffected — the walk
  never reaches them.
