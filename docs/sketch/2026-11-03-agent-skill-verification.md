# gom-agent-guide skill: implementation and snippet verification (P4-9)

- 日付: 2026-11-03
- Go: `go1.27.0 linux/amd64`
- Base: `6927cea954b62e20bb14008385a213a42f9368a9`

## What was built

Following the design in
[2026-11-02-agent-skill-for-gom.md](2026-11-02-agent-skill-for-gom.md):

```text
skills/gom-agent-guide/
  SKILL.md                      # 678 lines, English, name+description frontmatter
  agents/openai.yaml            # optional Codex display/policy metadata
  references/
    language-fundamentals.md    # spec ch1–6, 11 condensed
    go-interop.md               # spec ch8
    concurrency.md              # spec ch10 (+ P4-8/P5-1 pending markers)
    interface-files.md          # spec ch7 (+ P4-1 pending markers)
    diagnostics.md              # spec ch12 + error-code categories
    project-layout.md           # user project + repo layout, toolchain table
.devin/skills/gom-agent-guide/  # byte-identical vendor copy (`make vendor-skills`)
skills/snippets_test.go         # extraction test (see decision)
```

`AGENTS.md` gained the `## .gom を書くとき` routing section pointing
`.gom`/`.gomi` work at `$gom-agent-guide` (AGENTS.md routes, SKILL.md
carries the payload — the MoonBit layering).

## The decision under test

[2026-11-03-agent-skill-example-verification](../decisions/2026-11-03-agent-skill-example-verification.md):
snippets are extracted at test time (no committed fixtures), compiled
through `build.Compile` `CheckOnly` (the `gom check` wiring), with
info-string directives `fail[=CODE]` / `skip` / `gomi`, package-clause
wrapping for fragments, and a byte-identity check between `skills/`
and `.devin/skills/`.

## Executed evidence

`go test ./skills -v` on 2026-11-03: 47 snippet subtests + the
`TestVendoredSkills` identity check, all passing:

- `SKILL.md` — 30 blocks: 13 clean `gom` (including the verified
  trap `type Meters = Int` — the variant-not-alias trap that
  *compiles*), 17 `fail=E0xxx` (NG/OK pairs: comma application
  E0401, `x = 2` E0101, `let..in` E0101, `else if` E0101, record `:`
  literal E0101, `x :: rest` E0101, indexing E0401, cons literal
  E0101, `$`/`!` E0101, Gom-value method call E0401, missing labels
  E0401, mid-block drop E0402, `try` in non-Result fn E0410, `try`
  in `go` E0702, `pub` E0101, plain `import` E0203).
- references — 17 blocks: 16 clean `gom` (currying, labels+punning,
  patterns, `try`/`|?>`, `for`, tests, `import go` basics incl.
  `(T,bool)` tuple + `_` placeholder + `os.LookupEnv`, `extern`,
  `go`/`select`/`for` on param channels, `Result` pipe), 1 `gomi`.
- Negative control: temporarily appending a `fn oops () -> Int =
  "not an int"` block made the run FAIL with E0401 — the harness
  detects bit-rot, not just missing files.

Environment-dependent behavior: `import go` snippets run
`go list -export` through `internal/goimport`; without a `go` binary
on PATH those subtests skip (same guard as `tutorial_test.go`).

## Findings folded back into the docs

Writing the skill surfaced implementation facts that differ from
naive spec readings; each is now documented in the skill itself:

- The stdlib module names exist but their member tables are empty —
  `List.map`, `Chan.make`, `Task.spawn`, `Option.get`, `Ref.new` all
  report E0201 "exposes no members yet" (P4-8). The skill teaches
  hand-recursion and param-supplied channels instead.
- `..` is not allowed in list *literals* (patterns only) —
  `[x, ..xs]` is E0101; there is no cons literal in v1.
- `x.M()` on a Gom ADT reports E0401 "no field or method", not
  E0502 — the skill now names both.
- `go`/`select`/`for` already parse and type-check; only the
  `Chan`/`Task` stdlib members are missing.
- `test` blocks type-check under `gom check`/`gom build`; `gom test`
  does not exist yet.
- `pkg.Dir` resolution note: `import "fmt"` without `go` is E0203.
- `os.Args`, `math.Pi` resolve (the earlier P1-9-era bug is fixed).

## Follow-ups (filed, not part of this task)

- `gom new` generating a thin AGENTS.md template — separate task,
  needs `gom new` itself (per the P4-9 entry).
- `docs/problems/2026-11-04-ignore-discard-syntax-unparsed.md` is
  stale (`ignore` parses and the tutorial uses it throughout);
  recorded in `docs/implementation-gaps.md`.
