# `|?>` 自動ラップの文言とフレーム表現

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-error-result-bindings-verification.md)。spec.md §9.2 へ反映済み）
- 日付: 2026-11-05

## Context

[result-pipe-auto-wrap](2026-11-01-result-pipe-auto-wrap.md) decided
the `|?>` failure wrap: `fmt.Errorf("<step>: %w", err)` where the
step name is the written head name or `file:line` for a nameless
step. Three questions stayed open:

1. Per-step wording vs a package-level (pipeline-level) format —
   spec 9.2 kept per-step but the wording knobs were declared
   future work.
2. The `strconv`-style double function name —
   `strconv.Atoi: strconv.Atoi: parsing "x": invalid syntax` —
   where the outer name is the *call-site step* and the inner one
   is `*NumError`'s own message.
3. Structured frame information: a consumer that wants the failing
   step's name as *data* has to parse the rendered string.

## Decision

### Keep per-step wording

`<step>: <message>` names the failing step — the information a
reader cannot recover otherwise. Package-level wording
(`<enclosing fn>: <message>` or the pipeline's position) names the
pipeline, which the caller already knows from context, and loses
which step failed — strictly less information for the same cost.
Rejected.

### The double name is two provenances, not a bug

`strconv.Atoi: strconv.Atoi: parsing "x"` doubles because the outer
label is the *call site's* step name and the inner label is the
*error's own* text — `*strconv.NumError.Error()` chooses to name
its function. Deduplicating by prefix-matching the rendered string
makes a cosmetic decision depend on error text, an unstable
channel (a Go release rewords `NumError.Error` and the dedup
breaks). The text keeps both labels; the structured frame below is
the honest fix for consumers that want only the site name.

### The frame is `*rt.StepError` — structured, same wording

The emitted wrap is `rt.StepErr("<step>", err)` returning
`*rt.StepError{Step, Err}` (rt/steperr.go) instead of
`fmt.Errorf("<step>: %w", err)`:

- `Error()` renders `Step + ": " + Err.Error()` — the rendered text
  is byte-identical to the old `fmt.Errorf` frame, so spec 9.2's
  wording rule is unchanged for every consumer of `Error()`.
- `Unwrap() error` returns `Err` — `errors.Is`/`errors.As` behavior
  is identical to the `%w` wrap.
- `Step` is the structured datum: a Go-side consumer
  (`errors.As(err, &se)` with `se *rt.StepError`) or a Gom-side one
  (`import go "github.com/podhmo/gom/rt"`, `Ptr.new` at
  `Ptr (Ptr rt.StepError)`) reads the step name back without
  parsing text.

`Step` carries whatever `stepName` produces — the written head
name, or `file:line` for a nameless step — so a nameless step's
frame still names where it failed.

## Alternatives

- **Keep `fmt.Errorf("step: %w", err)`** — rejected as the emitted
  form. It is one line shorter in generated code but bakes the step
  name into text with no read-back path; `rt.StepErr` costs one
  runtime helper and keeps every existing behavior.
- **Dedup the doubled name at wrap time** — rejected: it compares
  rendered text, an unstable channel, and hides a real provenance
  difference rather than resolving it.
- **Package-level wording** — rejected: loses the failing step's
  identity, the one datum the wrap exists to carry.
- **`Pos` field alongside `Step`** — declined for now: `Step`
  already carries `file:line` for nameless steps and codegen does
  not hold a resolved line table at emit time (`//line` sentinels
  resolve later). Adding `Pos` later is backward-compatible.

## Consequences

- Generated `|?>`/`try`-fusion code now calls `rt.StepErr(...)`;
  the `rt` import appears in files that previously imported `fmt`
  only for this wrap.
- `errors.As` into `*rt.StepError` is a new public observation
  point — `rt` becomes a small part of the error contract, noted in
  spec 9.2.
- Text wording is unchanged; every existing assertion on rendered
  error strings (e.g. `cal`'s stderr) still holds.
