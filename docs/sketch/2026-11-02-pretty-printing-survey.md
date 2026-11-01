# Survey: pretty-printing techniques for `gom fmt`

- Date: 2026-11-02
- Target: TODO task **P4-2 `gom fmt`** ([spec 1.7](../spec.md),
  [design.md section 6](../design.md), `internal/format`)
- Scope: literature + implementation survey. This is a record, not a
  normative document; where it contradicts spec.md/design.md, the spec
  and design win and the discrepancy should be reported.

Sources were checked on 2026-11-02. URLs are listed per section and
collected at the end.

---

## 1. Requirements recap (what we are building)

From spec 1.7 and design.md §6:

- Output is a function of the **syntax tree and comments only**; the
  input's line breaks and indentation are *not* inherited. This is the
  prettier/ormolu model (canonical re-layout), **not** the gofmt model
  (gofmt keeps the author's line-splitting decisions — see §3.3).
- Line width 100, indent 2 spaces. No vertical alignment of `->`/`=`;
  only runs of trailing `//` comments are aligned.
- Inherited from source: comment placement and blank lines *inside
  blocks*, collapsing runs of blanks to 1. Top-level decls get exactly
  one blank line; imports are sorted and merged.
- Redundant parens are dropped, required parens re-added (parens are
  not in the AST — `paren-expr` in grammar.ebnf §4 parses away).
- Guarantees: `fmt(fmt(x)) = fmt(x)` and `parse(fmt(x)) ≡ parse(x)`
  (same AST).
- Property tests over generated trees; boundary cases for newlines,
  comments, and parse-error recovery (design.md §6).

The grammar's NL rules (grammar.ebnf §3, spec 1.5) are the dominant
constraint on *where a pretty printer may break*; §4.3 below derives a
concrete safe-break table. Comments are file-level trivia
(`File.Comments`, `File.Blanks` in `internal/syntax/ast.go`), not in
the tree — they must be reattached by position.

---

## 2. The classic algorithms

### 2.1 Oppen, "Prettyprinting" (TOPLAS 1980)

- Streaming algorithm: two coroutines (scan + print) communicate
  through a buffer of size O(m) where m = line width. Time O(n) in the
  input stream, space O(m). Output can begin before the whole input is
  seen.
- Input is a token stream with `begin-block`/`end-block`, `break` and
  string commands — the ancestor of every later combinator design.
- When is it still relevant? (a) When the document cannot or should
  not be materialized — interactive editors, infinite/huge output
  (the eXene library does pretty-printing of *infinite* syntax trees);
  (b) as the engine behind stream APIs. OCaml's `Format` module and
  SML/NJ's `PrettyPrint` structure are Oppen-family: a stream of
  box/break/string commands over an implicit lookahead queue. For a
  batch formatter over files that already fit in memory as an AST, the
  streaming property buys nothing — the Doc-tree designs below are
  simpler to reason about. **Not recommended for gom fmt**, but its
  vocabulary (blocks, break hints, consistent/inconsistent breaking)
  survives verbatim in OCaml `Format`.
  - Source: doi.org/10.1145/357114.357115; Stanford CS-TR-79-770;
    smlnj.org "The Visible Compiler" (`structure PrettyPrint` is
    explicitly "Oppen-style").

### 2.2 Hughes, "The Design of a Pretty-printing Library" (1995)

- The first combinator *algebra*. Two concatenation operators:
  horizontal `<>` (documents side by side) and vertical `$$`/`<+>`
  (stacked). Key op is `sep`/`choice`: choose horizontal if it fits,
  else vertical.
- Algebraic warts Wadler later removed: horizontal concat has a right
  unit but no left unit; vertical concat has neither unit. Deciding
  "fits" requires measuring, which forced awkward operators.
- Still the basis of GHC's `Text.PrettyPrint` (Hughes–Peyton Jones)
  and the `pretty` package. Its h/v box terminology lives on in OCaml
  `Format` (hbox/vbox/hvbox/hovbox/bbox — see §3.5).
- For gom: interesting mainly as history. One concatenation op beats
  two.

### 2.3 Wadler, "A Prettier Printer" (1997/2003)

- Single associative concat `<>` with `nil` as two-sided unit. Core
  ops: `text s`, `line`, `nest i`, `<>` — plus `group`, defined not
  primitively but as `group d = union (flatten d) d`: the same doc in
  two variants, flattened (all `line`s become spaces) or not.
- Evaluation is a greedy `best w k` walk producing a union-free
  `SDoc`; at each `union (x, y)` it tries `x` and accepts it iff
  `fits remainingWidth x`. `fits` scans only to the **next** line
  break, so decisions are local and fast; `nest` is folded into `line`
  nodes so indentation is recomputed lazily. `layout` renders `SDoc`.
- Greedy consequence: a group fits-or-breaks as a unit
  ("all-or-nothing"), decided on the text up to the next newline only.
  It can produce layouts Bernardy calls wasteful (breaks a group whose
  remainder *would* have fit further on) — fine for a canonical
  formatter, since consistency matters more than optimality.
- Lineage: Leijen's `wl-pprint`/`prettyprinter` (Haskell), the OCaml
  port, **prettier's Doc IR** (with extensions), `prettyplease`
  (Rust). This is what design.md §6 already commits to.
- Source: homepages.inf.ed.ac.uk/wadler/papers/prettier/prettier.pdf

### 2.4 Bernardy, "A Pretty but Not Greedy Printer" (ICFP 2017)

- Reformulates pretty-printing as optimization: Visibility ≤ width,
  then Legibility (respect hierarchy), then Frugality (fewest lines).
  Proves the spec *precludes* greediness, then derives a linear-time
  (in output size) non-greedy implementation — essentially bounded
  lookahead over candidate layouts.
- Takeaway for us: exists, is strictly better-looking, but needs a
  laziness-friendly or more complex implementation and buys layout
  *optimality* a canonical formatter doesn't need. Keep in back pocket
  if greedy Wadler output is ever judged ugly.
- Source: dl.acm.org/doi/10.1145/3110250

### 2.5 Comparison snapshot

| Algorithm | Doc model | Break decision | Fits gom? |
| --- | --- | --- | --- |
| Oppen '80 | stream commands | greedy, O(width) buffer | overkill; no materialized Doc |
| Hughes '95 | tree, two concats | `sep` = h-or-v choice | superseded by Wadler |
| Wadler '97 | tree, one concat | `group` = union(flat, d), greedy `fits` | **yes** (design.md already says so) |
| Bernardy '17 | tree + optimization | non-greedy, optimal | better output, more machinery |

---

## 3. What production formatters actually do

### 3.1 prettier (JS) — the most relevant reference

- Printer converts AST → **Doc IR**, then a Wadler-derived printer
  walks a command stack of `(indent, mode, doc)` with `MODE_FLAT` /
  `MODE_BREAK`. `fits()` peeks ahead like Wadler's.
- Commands worth copying (github.com/prettier/prettier `commands.md`):
  - `group(doc, {shouldBreak, id})` — try-flat; `id` enables
    `ifBreak` keyed to a *named* group's state.
  - `line` (space-or-break), `softline` (empty-or-break), `hardline`
    (always break), `literalline` (break that resets to column 0 —
    used for text that must not be re-indented).
  - `indent`, `align(n|" ", doc)` — align to *column*, not level.
  - `fill([d0, sep, d1, sep, ...])` — pack items per line (fill a
    paragraph), unlike group's all-or-nothing.
  - `ifBreak(broken, flat)` — e.g. trailing comma only when broken.
  - `lineSuffix(doc)` — defer `doc` to just before the next newline;
    **this is how trailing comments are printed**: code can never
    leak past a `//` onto its line. Companion `lineSuffixBoundary`.
  - `breakParent` / propagateBreaks: a `hardline` (or anything forced)
    inside a group **forces every enclosing group to break**. Static
    pass over the Doc before printing.
- Comment attachment is a **separate pass before Doc construction**
  (prettier/pull/482, issues/4290): classify each comment
  - *own-line* (nothing but whitespace before it on its line) →
    **leading** of the following node;
  - *end-of-line* (code precedes it on its line) → **trailing** of
    the preceding node;
  - *remaining* (tokens both sides) → dangling/tie-broken by the gap
    text (`isGap` regex — whitespace-or-parens between comment and
    node picks the side).
  The 482 refactor's core lesson: *leading vs trailing cannot be
  decided from AST structure* — `f(a, // c\n b)` puts `// c`
  structurally "before `b`" but visually after `a`. Only the source
  line/column tells. Our `commentMeta.lineOnly` flag
  (`internal/syntax/lexer.go:46`) is exactly this bit.
- Each comment is marked `printed` once emitted — the standard guard
  against the classic double-print bug.

### 3.2 rustfmt vs prettyplease (Rust)

- rustfmt: heavyweight, node-type-specific "shapes" (a width budget +
  indent pair threaded through every emitter), tries overflow lists
  and multiple candidate layouts per construct; famously can give up.
  Not a Doc algebra. Its *testing* model is the one to copy (§5).
- prettyplease (dtolnay): deliberately the *simplest possible
  algorithm* — a fixed modern-Rust style with small local lookahead
  (no shape search, no config), ~97–98% line-identical to rustfmt, 20×
  faster, 3 deps vs 72. Lesson: a formatter for a *canonical* style
  needs neither search nor backtracking; a Wadler group is already
  more lookahead than prettyplease uses.
- Sources: github.com/dtolnay/prettyplease README;
  rustfmt Contributing.md.

### 3.3 gofmt (go/printer) — important *contrast*, not a model

- go/printer is **not** a generic pretty printer and not a re-breaking
  formatter: it walks the Go AST with node-type-specific emit
  functions, normalizes spaces/indent, and respects the author's
  one-line-vs-multi-line choice per construct (that is why two
  semantically identical files can both be gofmt-stable). gom fmt
  deliberately does more: it *re-flows* — do not port go/printer's
  architecture.
- What *is* worth stealing:
  - **text/tabwriter for alignment.** gofmt's only alignment is
    trailing comments and struct-field runs, emitted as tabwriter
    cells; comments wrapped in `tabwriter.Escape` bytes so they don't
    affect cell widths. Gom's "align runs of trailing `//`" is the
    same feature — either reuse `text/tabwriter` in the emit stage or
    do a post-pass over recorded comment columns.
  - **Comments interleaved by source offset**: the printer keeps a
    comment cursor (`commentBefore`, `commentSizeBefore`) and flushes
    comments whose offset precedes the next node — plus `\f` vs `\n`
    to distinguish printer-introduced breaks from preserved blank
    lines. Functional equivalent of prettier's leading/trailing pass.
- Sources: go.dev/src/go/printer/printer.go, comment.go, gobuild.go.

### 3.4 ormolu / fourmolu (Haskell) — closest in *spirit*, one key difference

- ormolu gives the programmer exactly one bit of control: whether a
  construct is written broken or not is *read from the input* (any
  newline inside the CST node → fully broken everywhere possible in
  ancestors). Everything else is canonical. Gom goes further — spec
  1.7 does not inherit even that bit — so gom fmt is *simpler* than
  ormolu: no input-layout inspection at all.
- **Fourmolu's comment lessons are the most valuable data point in
  this whole survey** (fourmolu changelog):
  - Comments must be attached **to nodes by position, before
    printing**, not by a cursor advanced as the printer walks. With a
    cursor, comments "escape" their construct when the printer reorders
    (sorted imports!) or regroups — a comment inside an import list
    stayed attached to its import only after they moved to
    position-based attachment. gom fmt sorts imports → same hazard.
  - Layout must *know about* comments: a comment inside a construct
    forbids the single-line rendering (it would move the comment).
    Concretely: interior `//` ⇒ enclosing group forced broken.
  - A comment after `=`/`->` must stay on that line (their issues
    786/810/936 — the canonical non-idempotent comment bug).
  - Print comments as written (a `/* */` stays a block), re-indent
    block doc comments to their owner.
- Sources: github.com/tweag/ormolu DESIGN.md;
  fourmolu.github.io/changelog; deepwiki fourmolu comment handling.

### 3.5 OCaml `Format` and SML/NJ — the box vocabulary

- OCaml `Format` is Oppen-style: five box kinds give five
  line-splitting policies —
  `hbox` never breaks, `vbox` always breaks, `hvbox` breaks all hints
  iff the box doesn't fit (**= Wadler `group`**), `hovbox`/compact
  breaks hints only as needed (**= `fill`**), `bbox`. Break hints
  carry (spaces, indent) pairs. Confirms the group/fill pair covers
  the useful policies; everything else is sugar.
- SML/NJ `PrettyPrint` is a raw Oppen stream API
  (`begin_block`/`add_break`/consistent vs inconsistent — consistent ≈
  group, inconsistent ≈ fill). Same two policies again.
- Sources: OCaml stdlib Format docs; "Format Unraveled"
  (hal.science/hal-01503081); smlnj.org docs.

---

## 4. Concrete guidance for our AST

### 4.1 Recommended architecture

Two stages, matching prettier/design.md §6:

1. `internal/format`: AST+trivia → `Doc` (an intermediate tree, not
   text). Comment/blank attachment happens **here**, before any Doc is
   built, as a pure pass over `(File.Decls, File.Comments,
   File.Blanks)` — per the fourmolu lesson.
2. A ~300-line Wadler printer: `Doc` → string via the command-stack
   greedy algorithm (width 100 as the only parameter).

### 4.2 Minimal Doc op set

Sufficient for everything spec 1.7 demands, nothing more:

| Op | Semantics | Needed for |
| --- | --- | --- |
| `Text(s)` | literal, no `\n` inside | everything |
| `Concat`/`[]Doc` | sequencing | everything |
| `Group(d)` | flat if the rest fits to next break, else all `Line`s break | every wrap decision |
| `Indent(n, d)` | nest +n (2) under group's column | blocks, arms, continuations |
| `Line` | space-or-break | arg lists, operands |
| `SoftLine` | empty-or-break | after `(`, before `)`, `|` variant leads |
| `HardLine` | always break **and force all enclosing groups broken** (prettier's propagation) | `sep` between block items/arms/decls |
| `LineSuffix(d)` | defer to end of line | `//` trailing comments — the *only* safe way to emit them (prettier) |
| `IfBreak(b,f)` | emit b when enclosing group broken, f when flat | trailing commas in `[`,`(`,`{` literals — grammar allows `[","]` everywhere (grammar.ebnf list/record/tuple/pat productions) |
| `Nil` | empty | glue |

Deliberately excluded: `Fill` (no pack-multiple-per-line style in
spec; add later if wanted, e.g. for binary chains), `Align` (spec bans
column alignment — trailing-comment alignment is a separate post-pass,
§4.5), `literalline` (needed **only if** a `/* */` comment's interior
newlines are emitted verbatim — likely yes: a `Verbatim(s)` op or
literal-line is the one probable addition for multi-line block
comments; decide when comment printing lands).

HardLine's *propagation* is the part people forget: it must be a
static pre-pass (`propagateBreaks` in prettier) so that `group`s
containing a hardline are born broken — otherwise a parent group can
"fit" only because it never saw the forced break inside.

### 4.3 The NL-safety table — where a `Line` may legally break

Because Gom newlines are *significant* inside `{ }` and at top level,
a pretty-printed `'\n'` is not free: it may insert an `NL` separator
and **split one expression into two** — an instant round-trip failure.
NL-1/NL-2 give a complete classification:

| Break site (inside `{` or top level) | NL inserted? | Safe? |
| --- | --- | --- |
| after a token **not** in `can-end` (`=`, `->`, `,`, binop, `(` …) | no — continuation | ✔ intra-expression |
| before a token in `line-start` (binops, `\|>`, `\|`, `else`) | no — continuation | ✔ intra-expression |
| between two complete items (PREV in `can-end`, NEXT not in `line-start`) | yes — `sep` | ✔ **only** at real item boundaries (block items, match/select arms, decls) |
| anywhere else inside one expression (e.g. between `f` and `x` of an application — ident is `can-end`, ident is not `line-start`) | **yes** | ✘ splits the expression → AST change |

Consequences for the Doc builder:

- Inside `( )`, `[ ]`, `${ }` every `Line` is unconditionally safe
  (NL-1). All ordinary wrap decisions — call args, list/tuple
  literals, parenthesized exprs — live here.
- Inside `{ }` (blocks, match/select braces) `Line` may appear only at
  the three safe sites. In particular, **a function application that
  must break inside brace/top-level context cannot break between
  function and argument** — it would emit NL. Spec 1.5's own answer is
  the fix: wrap the application in parens, turning the context into
  NL-1 (`(render_page\n    header\n    body\n    footer)`). Rule:
  when an `apply` Doc (or any expr whose internal break sites are all
  unsafe) is built directly under a brace context and might break,
  emit it inside `(` … `)` and let the group break inside the parens.
  A cheaper alternative already in the grammar: break *after* `=`,
  `->`, binary ops, or *before* `|>`/`|`/`else` — all safe continuations.
- `else` is in `line-start`: `} else {` may legally put `else` on a
  fresh line without producing NL. Style decision, not a constraint.
- `->` is **not** in `line-start` (grammar.ebnf §3 note): an arm may
  never break *before* `->`; break after `->` is fine (PREV not
  `can-end`).
- `.`, `:`, `~label`, `in`, `with`, `<-`, `if` are not `line-start`:
  never place a `Line` before them; after is fine where the grammar
  allows the split at all.
- Brace-ban (spec 1.6): the `{` of `if`/`match`/`for`/`select`/`test`
  heads must stay on the head's line — `match s\n{` would insert NL.
  Emit `{` glued by a plain `Line` only inside the same group as the
  head, or a non-breaking space.
- Since `;` is always equivalent to NL (grammar.ebnf §3), the
  formatter never needs it — a single-item `{ e }` can flatten to one
  line, multi-item blocks always contain `HardLine`s anyway.
- Answer to the obvious question: the output does **not** need to
  re-lex to the same token stream — only `parse(fmt(x)) ≡ parse(x)`.
  The printer fully controls where NLs materialize via the table
  above; comments are trivia and never affect NL (NL-3), except that a
  `//` comment forces its line to end (see `LineSuffix`).

### 4.4 Comment attachment strategy (concrete)

Inputs: `File.Comments` (source order) + the lexer's `commentMeta`
(`startLine`, `endLine`, `lineOnly` = "nothing but whitespace precedes
it on its line" — prettier's own-line/end-of-line bit). Doc comments
`///` are already on `Decl.Doc`/`Variant.Doc` — emit them with the
decl, skip them in the generic pass.

Per comment, in a single pre-pass (attach-by-position, fourmolu rule):

1. `lineOnly == false` → **trailing** of the deepest preceding node in
   the enclosing construct → printed via `LineSuffix` (+ `breakParent`
   semantics: it must end the line).
2. `lineOnly == true` → **leading** of the following node; printed as
   `Text(comment) + HardLine` before that node — which also
   force-breaks every enclosing group, giving fourmolu's "a comment
   inside a construct forbids the single-line rendering" for free.
3. No following node in scope (end of block/file, or tokens on both
   sides — `remaining` in prettier's terms, always a `/* */`) →
   **dangling** inside the enclosing node, emitted on its own line
   (indent under the enclosing group).
4. Import sorting: comments attached to an `ImportDecl` travel with
   the decl node — this is exactly the bug fourmolu fixed by
   position-first attachment; replicate that, not the bug.
5. Mark every comment printed exactly once; assert coverage in tests
   (prettier's `printed` flag — the double-print bug is the classic
   failure).

Multi-line `/* */` comments contain literal newlines: emit their text
verbatim (`Verbatim`/`literalline` semantics — no re-indent of
interior lines), and treat them as force-break for the same reason as
`HardLine`.

### 4.5 Blank lines and alignment

- `File.Blanks` = one span per blank line. Rule per spec 1.7:
  **inside blocks** preserve a blank between items iff ≥1 existed in
  source; collapse runs to 1. Since "had blank → 1 blank" and "1 blank
  → 1 blank" is a fixed point, this is idempotent by construction.
- Top level: exactly one blank between decls (normalized, not
  inherited); `package` clause and import group separated the same
  way.
- Blanks adjacent to comments are a policy detail the implementation
  must pick and pin: recommended = treat a blank before a leading
  comment as "preserved" (comment travels with its leading blank, like
  gofmt's paragraph preservation), never preserve *inside* a
  comment-to-decl gap (a `///` doc comment glued to its decl never
  takes a blank — it would detach in Doc terms).
- Trailing `//` alignment (the one allowed alignment, spec 1.7):
  post-pass over the emit buffer tracking recorded comment columns
  (don't rescan text for `//` — strings can contain it). Group maximal
  runs of consecutive lines ending in trailing comments inside one
  block, pad to the max column + 1. Deterministic on output text ⇒
  idempotent. (`text/tabwriter` is an alternative; a bespoke pass is
  ~50 lines and keeps us at zero deps.)

### 4.6 Parens and precedence

Parens are not AST nodes, so all parens in output are
formatter-chosen: (a) precedence parens — standard
parenthesize-if-child-precedence-lower rule for `Binary`/`Apply`/
`Select`/`Unary`/`Ascribe`; (b) NL-escape parens from §4.3 — a
breaking application inside brace context; (c) brace-ban parens around
record literals in head position (spec 1.6). Never remove parens that
change association — the round-trip test pins this.

---

## 5. Testing methodology

Precedents:

- **rustfmt**: `tests/source/X.rs` → `tests/target/X.rs` exact-match;
  *also* formats `target/` files themselves — catches both regressions
  and *converging non-idempotence* (input takes 2 runs to reach
  canonical). Plus rustfmt-must-format-itself (bootstrap).
  (rustfmt Contributing.md)
- **gofmt/go/printer**: `testdata/*.input`/`*.golden` pairs; real-world
  corpus = the whole Go repo (`gofmt -l` in CI).
- **prettier**: Jest snapshots per fixture dir + an idempotency pass
  on each snapshot; large external corpus runs for regressions.
- **design.md §6 for us**: property tests over generated syntax trees;
  boundary cases for newline placement, comments, error recovery.

Recommended suite (mapped to existing machinery):

1. **Fixtures** — `testdata/fmt/<name>.gom` + `<name>.expected.gom`
   (the dir already exists with `.gitkeep`). Test: `fmt(input) ==
   expected` **and** `fmt(expected) == expected` — the second clause
   is the rustfmt dual-check that catches 2-step convergence. One
   fixture per spec-1.7 rule and per NL-table row (esp. the unsafe
   break sites: `f\nx` in a block, `p\n-> e`, `match s\n{`).
2. **Idempotency property** — `fmt(fmt(x)) == fmt(x)` over the whole
   corpus below.
3. **Round-trip AST** — `treedump.DumpSyntax` exists
   (`internal/treedump/syntax.go`) but embeds `@start:end` spans and
   interleaves trivia. Compare with spans stripped (`@\d+:\d+` → ``)
   and **excluding** `blank`/`comment` items for the semantic check;
   then separately assert every input comment appears exactly once, in
   order (catches double-print and drop), and blanks follow the §4.5
   policy.
4. **Output validity** — `parse(fmt(x))` produces no diagnostics when
   `x` parsed cleanly (strongest single check: catches stray NLs).
5. **Corpus** — `testdata/spec/*.gom` (the `ok_*` files and the
   error-expectation files that still parse — odd constructs by
   design), `tutorial/*/main.gom` (10 comment-heavy real files),
   `stdlib/**/*.gom`, `testdata/golden` `.gom` inputs, plus any `.gom`
   emitted by generators.
6. **Fuzz** — `FuzzFormat` alongside `FuzzParse`: if input parses
   diag-free → fmt → reparse diag-free → stripped-dump equality →
   fixed point. Reuses `internal/fuzzutil` (seeds, sandbox,
   KnownBug registry) — same contract as the parser fuzzer
   (`internal/syntax/fuzz_test.go`). For structured generation
   (design.md's "randomly generated syntax trees"), a small random
   `.gom` *source* generator — emit syntactically valid token
   streams — is cheaper than building ASTs directly and doubles as
   parser-fuzz seed material.
7. **Parse-error recovery** — fmt on files with diags: either refuse
   (recommended v1 policy — format only clean parses) or degrade
   gracefully; pin whichever choice as a fixture + a decision note.

---

## 6. Go ecosystem — is there a library?

Searched 2026-11-02 for Go ports of Wadler/Hughes:

- `github.com/tanishiking/prettier` (2018, ~7★, unmaintained): literal
  Wadler port — `Text`, `Concat`, `Group`, `Nest`, `LineOrSpace`,
  `Pretty(width, doc)`. Correct-looking but tiny adoption, no comment
  machinery.
- `github.com/takoeight0821/pprint` (2025, 0★): `wl-pprint` port —
  `Hsep`/`Vsep`/`Indent`/`PutDoc`. Even younger.
- `go/printer` (stdlib): Go-AST-specific, wrong architecture for a
  re-flowing formatter (§3.3); `text/tabwriter` *is* reusable for the
  trailing-comment alignment post-pass.
- Nothing else mature; the Wadler ports that exist are hobby-scale.

**Recommendation: hand-roll.** The greedy printer (command stack +
`fits`) is a well-bounded ~300 lines; the repo's `go.mod` currently
has zero dependencies and the Doc needs Gom-specific ops anyway
(`LineSuffix`, `Verbatim`, hardline propagation) that neither port
ships. tanishiking/prettier is still worth a read — its `Pretty`
driver is the same algorithm shape.

---

## 7. Anti-patterns — what makes formatters produce bad output/diffs

- **Over-aggressive rebreaking**: layouts where an edit rewraps
  unrelated siblings. Wadler's all-or-nothing group is the guard —
  keep break sites few and structural (never "search for the best
  place to break" like clang-format's penalty model, whose diffs are
  famously unstable). ormolu/prettier keep this discipline.
- **Comment drift between passes** (the canonical non-idempotent bug):
  a comment attaches to node A on pass 1, to node B on pass 2 because
  ownership depended on layout. Fix: attachment is a pure function of
  (AST, trivia) computed once, pre-print — fourmolu's position-first
  fix, prettier's own-line/end-of-line classification.
- **Comment double-print / drop**: same comment reachable from two
  nodes (prettier's pre-482 bug). Fix: single owner + `printed` flag +
  coverage assertion in tests.
- **Letting code follow a `//` comment on its line**: silently
  comments code out — the round-trip test catches it, `LineSuffix`
  prevents it.
- **Flat-rendering a construct containing a line comment**: moves the
  comment's text relative to what it documented — fourmolu's
  "layout decisions take comments into account"; our interior-comment
  ⇒ force-break rule.
- **Cursor-driven reorder bugs**: anything the formatter reorders
  (imports) must move attached comments — attach before reordering.
- **Unstable alignment**: column alignment of `=`/`->` diffs churn
  when a longer name enters a block — spec 1.7 already bans it; keep
  only the local, run-bounded trailing-comment alignment.
- **Reassociation**: `a + (b + c)` printed as `a + b + c` keeps the
  same *text* but the AST records grouping — parens must be re-added
  by precedence, never dropped where they carry structure. The
  `Binary` AST's actual nesting is the spec; print exactly that.
- **Width-dependent behavior that differs between passes**: e.g. a
  decision keyed to a comment's *original* column — on pass 2 the
  column differs → drift. All decisions must be functions of the
  canonical form only.
- **gofmt's restraint model, imported by accident**: gofmt never
  rejoins lines; copying its printer structure would produce a
  formatter that can't satisfy spec 1.7's "fits → one line" rule.

---

## 8. Open questions for the implementation to pin

1. May `;` ever appear in output? (Recommended: never — newlines only.)
2. Single-item blocks `{ e }` and `if c { a } else { b }` — flat
   allowed when they fit, or always broken? (Spec silent; pick and
   fixture it.)
3. Blank line between a leading `//` comment and its node when the
   source had one: preserve or collapse? (Recommended: preserve 1 —
   matches gofmt's paragraph model.)
4. `Verbatim`/`literalline` op for multi-line `/* */` — confirm need
   when comment printing lands.
5. `Fill` for long binary chains (`a |> b |> c |> d` over width) —
   ormolu-style "break every pipe" (vbox) vs pack-per-line (fill)?
   Spec examples suggest break-all (`|>` leads each line, which is
   also the NL-safe choice since `|>` ∈ `line-start`).

---

## 9. References

- Wadler, *A Prettier Printer* (1997/2003):
  https://homepages.inf.ed.ac.uk/wadler/papers/prettier/prettier.pdf
- Hughes, *The Design of a Pretty-printing Library* (1995) —
  via Wadler's comparison and Bernardy's intro (above).
- Oppen, *Prettyprinting*, TOPLAS 1980:
  https://doi.org/10.1145/357114.357115,
  http://i.stanford.edu/TR/CS-TR-79-770.html
- Bernardy, *A Pretty but Not Greedy Printer*, ICFP 2017:
  https://dl.acm.org/doi/10.1145/3110250
- prettier Doc commands:
  https://github.com/prettier/prettier/blob/main/commands.md ;
  printer internals `doc.mjs`; comment attachment:
  https://github.com/prettier/prettier/pull/482 ,
  https://github.com/prettier/prettier/issues/4290
- prettyplease: https://github.com/dtolnay/prettyplease (README)
- rustfmt testing model:
  https://github.com/rust-lang/rustfmt/blob/main/Contributing.md
- go/printer internals:
  https://go.dev/src/go/printer/printer.go (commentBefore/SizeBefore),
  https://go.dev/src/go/printer/comment.go,
  https://go.dev/src/go/printer/gobuild.go (tabwriter.Escape use)
- ormolu DESIGN: https://github.com/tweag/ormolu/blob/master/DESIGN.md
- fourmolu changelog (comment-attachment fixes):
  https://fourmolu.github.io/changelog/
- OCaml Format: https://ocaml.org/manual/4.05/libref/Format.html and
  *Format Unraveled* https://hal.science/hal-01503081
- SML/NJ: *The Visible Compiler* (`structure PrettyPrint`,
  "Oppen-style") https://www.smlnj.org/doc/Compiler/pages/compiler.html
- Go Wadler ports: https://github.com/tanishiking/prettier ,
  https://github.com/takoeight0821/pprint

In-repo anchors: spec.md §1.5–1.7; design.md §6;
grammar.ebnf §3 (NL-1/2/3, can-end, line-start);
internal/syntax/ast.go (File.Comments/Blanks);
internal/syntax/lexer.go:43-58 (commentMeta);
internal/format/doc.go; internal/treedump/syntax.go (DumpSyntax);
internal/syntax/fuzz_test.go + internal/fuzzutil (fuzz harness).
