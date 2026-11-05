# Golden directory cases: one `.golden.go` per generated file

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-gomtest-golden-dir-cases-verification.md)）
- 日付: 2026-11-06
- Supersedes: [gomtest-multifile-cases](2026-11-05-gomtest-multifile-cases.md) のうち Consequences の「golden のディレクトリ case は未対応」と member-named side file の load error 規則（`*.golden.go` はメンバーの期待値として許可）

## Background

[gomtest-multifile-cases](2026-11-05-gomtest-multifile-cases.md)
made a suite subdirectory holding a bare side file one case over its
member `*.gom`/`*.gomi` files, compiled as a package unit. The spec
suite adopted it (`DiagnosticsFunc func([]File)`); the golden suite
rejects directory cases with `t.Fatalf` because a group's output is
one Go file *per member*, so the golden format itself needed
deciding (the earlier decision's Consequences deferred exactly this).

Two invariants already hold and constrain the choice:

- The golden suite's `<name>.golden.go` convention exists so a
  golden is a *valid, gofmt-checked, editor-highlighted Go file*
  ([test-harness](2026-11-02-test-harness.md)) — `gofmt -l .` in CI
  parses every `.go` file in the repo, including `testdata/`.
- Discovery is suite-agnostic: a directory is a case iff it directly
  holds a *bare* side file (`expected.json` / `golden.go` /
  `case.json`); member-named side files inside a case directory are
  load errors, keeping "a group has exactly one expectation".

This is a harness convention — design.md-level; spec.md untouched.

## Options

- **(i) One concatenated `<case>/golden.go`.** Members' outputs are
  joined with a `// ===== <name> =====` separator. Reuses the
  existing marker and `sidePath` unchanged — but two `package`
  clauses in one file cannot parse, so `gofmt -l .` fails in CI. A
  `.go`-named file that is not Go is worse than a non-`.go` name;
  renaming to `golden.txt` dodges gofmt but abandons the
  golden-is-Go invariant and highlighting, and leaves `golden.go`
  itself as a marker file the runner never reads — a trap.
- **(ii) One golden per generated output file.** Member `a.gom`'s
  output `a.go` pins to `a.golden.go` inside the case directory; the
  `_test.go` half (test mode, see the companion decision) pins to
  `a_test.golden.go`. Every golden stays a valid gofmt-checked Go
  file — the flat convention applied per output. The mapping is
  mechanical: golden path = output path with `.golden.go`
  substituted for `.go`. Costs: a `*.golden.go` inside a case dir
  stops being a load error (the member-named-side-file rule was
  written for the one-expectation `expected.json`/`case.json`
  shape), and the runner must check member↔golden correspondence
  itself since the walk never descends into a case dir.
- **(iii) A manifest golden** (`golden.go`/`case.json` listing the
  members and their output names). Pins names, not content — a
  golden that cannot be eyeballed in a diff fails its purpose.

## Decision

**Option (ii).** A directory case's generated output `x.go` is
pinned by `x.golden.go` in the same directory — including test-mode
`x_test.go` → `x_test.golden.go`. The flat format becomes the
degenerate case of one rule: *every generated file has a
`.golden.go` namesake beside it*. No new file kind, no second
expectation shape, every golden stays a parseable Go file.

Details:

- **Marker**: unchanged — only bare side files mark a directory
  case. A golden-only group therefore carries a `case.json` (often
  `{}`, or `{"tests": true}` — the same file carries per-case
  options, which is what it was reserved for). Member `*.golden.go`
  files never mark. A forgotten marker fails loudly: members become
  flat cases and the golden suite reports missing expectations.
- **`dirCase`**: named `*.golden.go` files inside a case directory
  are member goldens, not the group-expectation violation — only
  named `*.expected.json` / `*.case.json` still error. In a spec
  suite dir a member golden is simply inert (the golden runner
  never sees it); in the golden suite the runner validates it.
- **`RunGoldenTests`**: each returned output maps to its golden
  (`<out>.go` → `<out>.golden.go`). For a flat case this lands on
  `<name>.golden.go` exactly as today — existing fixtures are
  untouched. Inside a case dir the runner additionally lists
  `*.golden.go` files and reports ones no output produced, and
  requires every output to name a file directly inside the case dir
  (`<case>/<base>.go`) — a generated file that maps outside the
  case, or two outputs claiming one golden, is a case failure.
  `-update` writes each output's golden like it writes the flat
  one; stray member goldens are reported, never auto-deleted (the
  same posture as suite-level orphans).
- **`GenerateFunc`**: widened to `func(fs []File) ([]GeneratedFile,
  error)` — the signature shown here predates the `opts` parameter;
  [the tests option](2026-11-06-gomtest-tests-option.md) carries the
  final form `func(fs []File, opts CaseOptions) ([]GeneratedFile,
  error)` — the same `func([]File)` widening `DiagnosticsFunc` got:
  a case *is* a compilation unit and produces its generated files
  in member order. `GeneratedFile{Path, Src}` names the output as
  `gom build`/`gom test` would write it (`<case>/a.go`,
  `<case>/a_test.go`). Interface (`.gomi`) members yield no output.
  A flat case returns exactly one entry whose `Path` is
  `<name>.go` — validated, so a misnamed output cannot silently
  shift the flat golden's location.

## Rejected alternatives

- **Concatenated `golden.go` (i)**: unparseable `.go` file breaks
  `gofmt -l .` in CI (`make fmt-check` exits non-zero on parse
  errors); exempting it weakens the invariant the `.golden.go`
  suffix was chosen for. As `golden.txt` it survives but goldens
  lose the check and highlighting, and bare `golden.go` stays a
  legal marker that names nothing.
- **Member goldens as markers**: letting `a.golden.go` alone mark a
  directory case would couple discovery to golden-named files in
  *every* suite — a spec-suite namespace dir containing a stray
  `a.golden.go` would silently become a case. Bare-marker-only
  keeps the rule uniform; `case.json` marking is the established
  escape.
- **Manifest golden (iii)**: pins a file list, not the generated
  text — not a golden.

## Consequences

- `testdata/golden/<case>/` groups pin package-unit emission —
  shared package scope, per-file name claiming — in the same diff
  format as flat goldens, regenerable with `-update`.
- `internal/build`'s `goldenGenerate` switches to
  `build.CompileFiles` for all cases (one file degenerates), so the
  golden suite exercises the same unit pipeline the spec suite and
  the CLI do.
- The `dirCase` relaxation and the `testCase`/`loadManifest`
  additions are small additive edits in `gomtest.go` (owned by a
  parallel lane for discovery work — flagged in the lane report).
- doc.go's directory-case rules and design.md's `testdata/` notes
  gain the golden side; the "golden suite rejects directory cases"
  line goes away.
