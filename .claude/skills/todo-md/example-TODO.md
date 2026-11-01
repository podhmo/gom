# TODO

> **Note on updating this file:** upkeep rules live in
> [`.claude/skills/todo-md/SKILL.md`](.claude/skills/todo-md/SKILL.md) —
> follow them whenever you add or complete a task. Short version: this file
> lists actionable work only (`[ ]` / `[-]`); mark finished work `[x]` — a
> periodic GC sweep appends such entries to `TODO-archive.md`.

This file tracks actionable tasks only; completed work is swept to
`TODO-archive.md` by periodic GC.

## To Be Implemented

### Renderer

- [ ] **Add `PrettyRenderer`** ([design](docs/ja/design.md)): indented
  HTML output from the same Node tree. Entries stay brief — detail lives in
  linked docs.
- [-] **HTML correctness** ([design](docs/ja/design.md)): a `[-]` parent
  stays open while it has open children.
  - [ ] **Void elements**: render `<br>`, `<img>` etc. without closing tags.
  - [x] **Escape text and attribute values**: marked but not yet swept —
    the next GC pass moves it to `TODO-archive.md` with a `> parent:`
    context line.
- [x] **`PlainRenderer`**: feature PRs only mark `[x]`; they never edit
  `TODO-archive.md` themselves.
