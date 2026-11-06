# Verification: bare .gomi at a spec-suite root is a load error

- Date: 2026-11-06
- Decision: [spec-suite-root-bare-gomi](../decisions/2026-11-06-spec-suite-root-bare-gomi.md)

## What was checked

`internal/gomtest/testdata/rootgomi/`:

```
stray.gomi          bare interface file at the suite root
ok.gom              ordinary single-file case (+ ok.expected.json)
sub/case.json       directory-case marker
sub/m.gom           member
sub/m.gomi          member interface file
```

`discoverCases` over that tree (TestDiscoverRootGomi in
`internal/gomtest/gomtest_test.go`):

- `stray.gomi` → a case whose `loadErr` reads `gomtest: bare
  interface file stray.gomi at the suite root: a .gomi takes part
  only as a member of a directory case; a single-file case has no
  .gomi side file`.
- `ok` keeps working; `sub` is a directory case whose member list is
  `[sub/m.gom, sub/m.gomi]` — the walk never reaches inside, so
  member `.gomi` files are unaffected.
- Orphan reporting unchanged (none in the fixture).

`go test ./internal/gomtest -run TestDiscover -v` — all discovery
tests pass, including the pre-existing TestDiscoverRootSideFile.
