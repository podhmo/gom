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

## Next step

A research task is running to extract the concrete MoonBit SKILL.md
anatomy (frontmatter fields, section list, size) and the `skills`
npm package's file conventions; refine this sketch when it reports,
then file the implementation task.
