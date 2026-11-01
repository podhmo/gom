# Sketch: an "agent guide" skill for Gom (MoonBit pattern)

Status: rough notes, pending refinement by a research task.

## Motivation

Gom is a new language with essentially zero presence in LLM training
data. When an LLM writes `.gom`, it will fall back to habits from
OCaml / Haskell / F# / Go — wrong surface syntax, wrong assumptions
about currying, wrong mental model of the pipe operators. MoonBit (a
similarly young language) solved this by shipping an **agent skill**:
a condensed, LLM-oriented language tour plus pointers into real docs.

We want the same for Gom: a skill that gives an agent just enough
correct knowledge to write/read/debug Gom without hallucinating a
different language onto it.

## The MoonBit pattern (what we know so far)

- Repo `github.com/moonbitlang/skills` ships multiple skills; the
  language one is `moonbit-agent-guide` (canonical home:
  `github.com/moonbitlang/moonbit-agent-guide`, vendored into the
  skills repo).
- Its `SKILL.md` contains a `# MoonBit Language Tour` section — a
  compact "core facts" rundown (expression-oriented, visibility,
  packages, error handling, toolchain commands) — plus a
  `references/` directory with deeper material
  (e.g. `moonbit-language-fundamentals.mbt.md`).
- `moon new` generates a thin `AGENTS.md` for the project itself;
  the language knowledge lives in the skill, not the project file.
- A separate `moonbit-orientation` skill routes "does MoonBit have
  X?" questions to official sources instead of copying the spec.
- Distribution: `npx skills@latest add moonbitlang/skills --skill
  moonbit-agent-guide` copies the skill into the agent's skill dir.
  (We do NOT need the npx path to work; only the format matters —
  our skills live in `.devin/skills/` or `.claude/skills/`.)
- Human-facing interactive tour (`tour.moonbitlang.com`) is a
  separate artifact — different role, not what we copy.

## What a `gom` agent skill needs (candidate content)

Trigger: any `.gom` file, `gom build`/`check`/`dump` commands,
"write Gom" requests.

Condensed language tour (body of SKILL.md — keep small):
- File/module basics: `package`, top-level `fn`/`let`/`type`/`test`.
- Expression-oriented; `fn` signatures, call syntax, curried
  partial application and the flat/nested distinction that bites
  newcomers.
- The three pipe operators and their evaluation order (left-to-
  right; the try-pipe wraps errors via `%w`); guards in `match`.
- Types: ADTs, records, tuples, List, Option/Result; pattern
  matching is exhaustive (E0301); `_` placeholder.
- `import go`: what resolves, `(T, error)` → `Result` conversion,
  method calls, constants/variables; what is hidden and why.
- Error model: `try`, `Result`, `GoError`.
- Diagnostic format: error codes E0xxx, JSON output.
- Common LLM mistakes: assuming OCaml/F# syntax, assuming `main`
  special forms, assuming mutation/loops exist, assuming Go
  semantics where Gom diverges (structural equality, eval order).
- Toolchain: `gom check` / `gom build` / `gom dump -phase=`,
  `make check`, where golden/spec fixtures live.
- Pointers: docs/spec.md chapters, docs/grammar.ebnf, tutorial/01–10
  (executable), docs/error-codes.md.

`references/` (deep, loaded on demand):
- condensed spec excerpts per topic, or pointers to spec chapters;
- copyable canonical examples (interop, match, pipes, test blocks);
- generated-code conventions (what Gom constructs emit in Go, so
  agents debugging output know what to expect).

## Open questions for the implementing task

- Exact SKILL.md frontmatter schema Devin supports (check
  `.devin/skills/` / devin-cli skill docs) — name, description,
  trigger rules?
- Body size budget: MoonBit keeps the tour compact; ours should
  stay ≤ ~200 lines with references/ for depth.
- Should the skill double as the "how to develop the compiler"
  guide, or strictly the language? (Recommend: language only;
  compiler-dev info stays in AGENTS.md/TODO.)
- Single skill vs split (gom-language + gom-orientation)?

## Research findings (moonbitlang/skills, moonbit-agent-guide)

### SKILL.md anatomy (measured)

- Frontmatter is minimal — `name` + `description` ONLY (no
  `allowed-tools` etc.). `description` IS the trigger: "Use when
  working in MoonBit modules… moon tooling… following
  MoonBit-specific conventions". `moonbit-orientation` goes further:
  "Use it even when MoonBit is only implied by `.mbt` files,
  `moon.mod.json`, moon commands…" — fires without the user naming
  the language. **Gom should copy this: `.gom`/`.gomi` files +
  `gom` commands as implicit triggers.**
- Size: ~1000–1467 lines / ~14.3k tokens — deliberately over the
  usual "≤500 lines" guidance because the language knowledge itself
  is the payload. Don't slim artificially; DO keep the body
  sectioned so references/ can carry depth.
- Body structure (~21 sections, observed order):
  1. `# Agent Workflow` — canonical 7-step loop: locate package
     boundaries → **discover APIs via tooling BEFORE coding** →
     minimal edits → tight check/test loop → fmt+info to finish.
  2. `## Fast Task Playbooks` — per-task minimal procedures (bug
     fix vs new public API).
  3. `# MoonBit Language Tour` → `## Core facts`: ~10 high-density
     bullets — absolute statements ("private by default", "last
     expression is the return"), ONE canonical idiom each (no
     menus of options), contrast with trained languages ("Unlike
     Rust, like F#"), deprecated syntax callouts to kill stale
     LLM habits.
  4. Project layouts, coding rules you MUST follow, toolchain
     commands with real gotchas, test conventions, doc-test
     generation.
- ````mbt check` code fences: examples inside the skill are
  themselves compile-tested — worth copying (`gom check` blocks).

### Layering that emerged

- `moonbit-agent-guide` = **fat SKILL.md** (all core knowledge in
  body; references/ only for deep dives — just 2 files).
- `moonbit-orientation` = **thin SKILL.md** (a situation-routing
  table) + **fat references/** (9 files: question-routing,
  source-map, diagnostics-playbook, idioms, mental-models…).
  "Load the smallest relevant reference file"; freshness gate:
  never answer from memory, answer with a verification command.
- Generated project `AGENTS.md` = **thin router**: "This is a
  MoonBit project" + link to skills repo + "Read the MoonBit
  skills before writing code — model priors are routinely wrong
  here" + task→skill routing table + two non-negotiables (never
  name an unverified API — check via tooling; verify toolchain
  version). Contains NO language knowledge itself.
- So: **AGENTS.md routes, SKILL.md carries the payload,
  references/ carries depth.** Our current AGENTS.md is
  compiler-developer-facing and should stay; add one routing
  section for .gom authors.

### Distribution / discovery

- `npx skills` = `vercel-labs/skills`: clones repo, detects
  `skills/*/SKILL.md`, copies/symlinks into agent dirs:
  `.claude/skills/`, `.agents/skills/` (universal),
  `.devin/skills/`, `~/.config/devin/skills/`, `.github/skills/`.
- Format is the Anthropic agent-skills convention (SKILL.md +
  YAML frontmatter) — tool-cross-compatible; no submodule/symlink
  inside the skills repo (copier requires real files).
- moonbitlang/skills vendors upstream skills via
  `skills.sources.json` + a sync script — same pattern if we keep
  canonical skills in `skills/` and vendor into `.devin/skills/`.
- Optional `agents/openai.yaml` per skill (Codex display/policy
  metadata).
- Skills catalog: ONE language skill + per-task routers/skills
  (orientation, refactoring, spec-test-dev, proof, c-binding,
  migration). For Gom: `gom-agent-guide` first; `gom-orientation`
  only if the routing table grows; `gom-compiler-dev` later to
  lift internals out of AGENTS.md.

## Open questions for the implementing task

- Exact SKILL.md frontmatter schema Devin supports (check
  `.devin/skills/` / devin-cli skill docs) — name, description,
  trigger rules? **(answered above: name + description only;
  description is the trigger, write it to fire on `.gom`/`.gomi`/
  `gom` commands even unnamed)**
- Body size budget: MoonBit keeps the tour compact; ours should
  stay ≤ ~200 lines with references/ for depth. **(revised: MoonBit
  is ~1000+ lines; aim ~500–800 lines body + references/)**
- Should the skill double as the "how to develop the compiler"
  guide, or strictly the language? (Recommend: language only;
  compiler-dev info stays in AGENTS.md/TODO; split a
  `gom-compiler-dev` skill later if desired.)
- Single skill vs split (gom-language + gom-orientation)?
  **(revised: start with one fat `gom-agent-guide`; split an
  orientation router later)**
- spec.md is in… verify which spec sections need translating for
  the English-language skill body (rule: docs/**/*.md English
  except docs/**/ja/; skill files follow docs rules).

## Recommended `gom-agent-guide` shape (from research)

```
skills/gom-agent-guide/
  SKILL.md                    # ~500-800 lines, English
  agents/openai.yaml          # optional Codex metadata
  references/
    language-fundamentals.md  # spec ch1-6 condensed
    go-interop.md             # spec ch8: import go, conversions
    concurrency.md            # spec ch10: go/select/Task
    interface-files.md        # spec ch7: .gomi
    diagnostics.md            # spec ch12 + error codes reading
    project-layout.md         # build/check/test/fmt, tutorial/
```

SKILL.md body order (borrowed):
1. `# Agent Workflow` — locate file/package → read the spec
   chapter → tight `gom check` loop → `gom fmt`.
2. `## Fast Task Playbooks` — write .gom / fix .gom / implement a
   spec rule (→ testdata/spec fixture) / read generated Go.
3. `# Gom Language Tour` → `## Core facts` bullets (one canonical
   idiom each, contrasts with OCaml/F#/Haskell/Go habits):
   - expression-oriented, no offside rule, `gom fmt` canonical
   - curried `fn add (a: Int) (b: Int) -> Int = …`, juxtaposition
     application, partial application semantics
   - the pipe family and evaluation order; `|?>` Result
     propagation with `%w` auto-wrap
   - labeled required args — label omission means partial
     application (docs/problems note)
   - ADTs + exhaustive `match` (E0301), records, tuples, List,
     Option/Result, `_` placeholder
   - `import go` member resolution, `(T,error)` → `Result`,
     hidden members + reasons
   - `try` boundary = innermost fn; `GoError`; `fn main () -> Unit`
   - `test` blocks, `assert`/`assert_eq` (test-only)
4. `## Common LLM mistakes` NG/OK pairs — our highest-value
   section. Sources: docs/problems/* is literally a catalog of
   "ambiguities an LLM will hit"; add Go/OCaml/F# habit clashes
   (comma application, `let … in`, `:=`, `if err != nil`,
   `f $ x`, `::` annotations, nil).
5. `# Toolchain` — gom check/build/dump/test, JSON diagnostics
   (spec ch12) + jq recipes, error-code layout →
   references/diagnostics.md.
6. `## References` routing table: task shape → reference file or
   repo doc (spec §N, tutorial/NN, grammar.ebnf, error-codes.md).

Trigger `description` draft: "Use when writing, checking, or
debugging `.gom` or `.gomi` files, using the `gom` toolchain
(check/build/dump/test/fmt), or following Gom-specific currying,
pipe, try/Result, match, or Go-interop conventions — even when Gom
is only implied by file extensions or `gom` commands."

Placement: canonical `skills/gom-agent-guide/` in-repo (future
`npx skills add podhmo/gom` compatible) + vendored copy in
`.devin/skills/` for Devin sessions; if a second copy is needed,
use moonbit's `skills.sources.json` sync pattern rather than
hand-editing both.

## Next step

Second research task is extracting the exact `moon new` AGENTS.md
template + the project-file→skill linkage; once it reports, file
P4-9 as an implementation task.
