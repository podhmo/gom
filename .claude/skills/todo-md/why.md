# Why the task tracker is organized this way

This scheme is ported from [podhmo/minigo](https://github.com/podhmo/minigo),
where it was adopted after the tracker had become unusable.

There, `TODO.md` grew to ~135KB (241 lines): ~179 completed `[x]` items
(~99KB) accumulated under one `## To Be Implemented` section because the old
AGENTS.md discipline never moved anything out and entries carried full fix
narratives. It stopped working as a task tracker — nobody, human or LLM,
could read it whole.

minigo restructured it on 2026-10-04 under
[issue #386](https://github.com/podhmo/minigo/issues/386). Three alternative
schemes were implemented and compared (PRs
[#384](https://github.com/podhmo/minigo/pull/384),
[#385](https://github.com/podhmo/minigo/pull/385),
[#387](https://github.com/podhmo/minigo/pull/387)); the comparison report is
attached to the issue comments. The single-archive scheme (TODO.md +
TODO-archive.md) won, and html-sandbox adopts it from the start so the
same failure never happens here.

The principle that survives every scheme: **the tracker holds actionable
work only — detail lives in linked docs (e.g. `docs/ja/design.md`),
archives, or git history, never inline.**

One deliberate looseness: consolidation is *asynchronous GC*, not part of
the completing PR. Feature branches just mark `[x]` (or delete); a periodic
sweep appends stragglers to the archive. That keeps `TODO-archive.md`
conflict-free across parallel branches and moves every judgment call
(parent renames, context lines) into one place — the GC pass.
