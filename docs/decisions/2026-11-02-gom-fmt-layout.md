# `gom fmt` layout: engine, width, and canonical style (P4-2)

- 状態: adopted
- 日付: 2026-11-02

## Background

Spec section 1.7 fixes the contract: output depends only on the
syntax tree and comments, line width 100, indent 2, no options,
no alignment except trailing comments on consecutive lines, blank
lines inside blocks collapse to one, one blank line between
top-level declarations, imports sorted and merged, redundant parens
removed and required parens restored, `fmt(fmt(x)) = fmt(x)` and
`parse(fmt(x))` produces the same tree.

What the spec does not fix is the *concrete* canonical style: where
a broken line breaks, which constructs are always multi-line, and
how comments reattach when the tree is reflowed. This decision
records those choices. The formatter lives in `internal/format`
and shares the parser (docs/design.md section 6).

## Decisions

### Layout engine

- **Wadler-style documents.** The printer builds a document IR —
  `text`, `concat`, `nest`, `group`, `line` (breakable space),
  `softline` (breakable nothing), `hardline` — and lays it out with
  the standard greedy `fits` check: each `group` renders flat when
  its flat width fits the remaining columns, broken otherwise.
  Simpler "break at fixed points" rules were rejected: the choice
  must depend on the column the construct lands on, which is exactly
  what a group captures.

- **Fixed policy breaks.** `match`, `select`, `extern`, and sum-type
  bodies always break (their items are line-oriented constructs, not
  expressions that inline gracefully). Everything else tries flat
  first — including `if c { a } else { b }` and `{ s1; s2 }` blocks,
  which stay on one line while they fit.

### Line width and indent

- 100 columns, 2-space indent — spec 1.7. The signature of a
  declaration cannot break between parameter groups (a newline there
  is a separator), so an overlong signature may exceed 100; breaks
  inside parameter and return-type parentheses stay legal because
  `(...)` suppresses newline significance.

### Canonical break shapes

- Pipe chains (`|>`, `<|`, `|?>`, `>>`, `<<` and other binary ops at
  any level) break *before* the operator, one operand per line, all
  continuation lines at the same indent — spec 1.5's own example.
- A long application wraps in parens and puts one argument per
  line: `(f\n  a\n  b)`. A single tuple argument instead breaks
  inside the call parens, `f(\n  a,\n  b,\n)` — `fmt.Println(a, b)`
  keeps its Go shape; a single list/record argument breaks inside
  its own brackets the same way.
- Multi-line lists, tuples, records, and record types put one
  element per line **with a trailing comma**, closer on its own
  line. The trailing comma is required by the newline rules
  (`,` suppresses the separator), and it makes diffs minimal.
- `match` / `select` / `extern` braces and multi-line blocks are
  brace-on-head-line, items at +2, `}` on its own line.
- Sum types print `type X =` then one `| Variant` per line at +2,
  including single-variant types.
- `let` / `fn` bodies that do not fit break after `=` or `->` with
  the body at +2 (a line ending in `=`/`->` always continues).
- Records updates print `{ base with` then fields one per line —
  `with` cannot lead a line under the newline rules.
- Type expressions break only after `->`; type arrows cannot lead a
  line, and signature-level types do not break at all outside
  parentheses.
- Ascription `e : T` cannot break before `:` (not a line-start
  token); it stays flat or breaks inside `e`/`T`.

### Parentheses

The AST drops grouping parens, so the printer re-derives them from
the grammar's precedence table (member > apply > unary > `* / %` >
`+ -` > relations > `&&` > `||` > pipes > `:`). Additionally:

- Control expressions (if/match/for/go/select/lambda) get parens
  wherever the grammar needs them (operands, arguments, ascription
  left side) — and also as `if`/`match`/`for` heads, where they are
  legal but unreadable.
- A `{`-led atom in a head position gets parens (`match ({x = 1})`),
  per spec 1.6; the flag propagates through the head's precedence
  levels exactly like the parser's `nb` flag.
- Lambda arguments are always parenthesized; a bare lambda after a
  pipe operator is kept bare (the grammar's `pipe-rhs` exception).

### Comments

Every comment is attached to a *gap*: the source interval between
two adjacent items of the smallest enclosing item list (decls,
block statements, arms, variants, fields, elements, arguments) or,
inside an expression, between adjacent child nodes.

- `///` runs the parser attached as `Doc` print above their
  declaration; all other comments anchor by position.
- Own-line comments print on their own lines at the gap they fall
  in. Trailing comments (code precedes them on their source line)
  print after the item ending that line — on its last output line
  when the item itself broke.
- Trailing comments on **consecutive single-line items** align to a
  common column (spec 1.7's one alignment rule), one space minimum.
- `//` comments inside `()`, `[]`, `${}` stay `//` (newlines there
  are whitespace). A `//` comment in a non-bracketed expression gap
  would force an illegal newline, so it prints as `/* ... */`
  (a literal `*/` inside its text is written `* /` — the comment is
  preserved, and the next format pass sees a stable block comment).
- `/* */` comments print inline wherever their gap is.
- Blank lines are preserved only between block statements, match and
  select arms, sum variants, and extern entries — collapsed to one.
  Inside lists, tuples, and records they collapse entirely.

### Imports

All `import` decls gather into one group at the position of the
first import, sorted by path (`import` before `import go` on a tie,
then alias). Comments on an import's own line, or on the lines
directly above it inside the import run, move with the import.

### Idempotence and round-trip

`fmt` output re-parses to the same tree: verified by property tests
over every `.gom` file in the repo — `parse(fmt(x))` dumped with
positions stripped equals `parse(x)`, and `fmt(fmt(x)) = fmt(x)`.

## Consequences

- testdata/spec and testdata/golden carry byte-offset expectations
  (`expected.json` spans, `//line` directives), so the repo-wide
  reformat excludes them; tutorial/ and other .gom files are
  reformatted as the dogfood.
- A comment the anchor machinery cannot place is appended at end of
  file rather than dropped; the property tests fail if any comment
  is lost.
