# P1-3 parser: interpretations made while implementing grammar.ebnf

- 状態: 採用
- 日付: 2026-11-02

## Background

`internal/syntax` implements `docs/grammar.ebnf` literally, but several
spots needed a reading the grammar text does not fully pin down (or a
recovery choice the grammar cannot express). Genuinely open questions
were filed under `docs/problems/` and are linked where relevant.

This file is the P1-3 companion of
`2026-11-01-formal-grammar-interpretations.md`; the parser validated
that document's readings except where noted below.

## Interpretations

### Separators between match/select arms

Arms are separated by `sep` (NL or `;`), never by `|`. A `|` between
arms is a parse error ("match arms are separated by newlines, not
\"|\"") with recovery that drops the `|` and keeps parsing arms. `|`
at arm-head position stays legal *inside* a pattern (or-pattern) and
in `type X = | A | B` variant lists.

### `else if` is rejected, not sugared

grammar.ebnf gives `else` only a block, so `else if` is an E0101
(`"else" must be followed by a block; write "else { if ... }"`). The
parser recovers as `else { if ... }`. Whether the sugar should be
added is still open (docs/problems/2026-11-01-else-if.md).

### Head expressions reject a leading `{`

`if`, `match`, and `for ... in` heads cannot begin with `{`
(parenthesize instead). This is the grammar's documented
disambiguation between a condition and the following block; the
parser applies it to `for`'s iterable too, as the earlier decision
already recorded.

### Spread is not a list-literal element

`[..xs]` inside `[ ... ]` is an E0101 ("spread \"..\" is not
allowed in a list literal"). `..` is honored in call parens
(`f(..xs)`), in list patterns, and in record patterns, which are the
positions grammar.ebnf admits.

### Parenthesized expressions collapse

`(e)` yields `e` itself — the AST has no paren node. `()` is the
unit literal; a tuple needs at least two elements (trailing comma
allowed). `(a, b)` is `TupleLit`.

### `Ctor { fields }` in patterns becomes CtorPat.Fields

`Let { name = n, .. }` parses the `{ ... }` as the constructor's
record-pattern argument and stores it on `CtorPat.Fields`/`Rest`
rather than as a nested `RecordPat` in `Args`. A parenthesized
record pattern `Let ({ ... })` stays an ordinary argument.

### `fn` lambdas need at least one parameter

grammar.ebnf `lambda = "fn" , lambda-param , { lambda-param } ,
"->" , expr` with `lambda-param = ident | "_" | "(" ")"` requires
one `lambda-param`; `fn -> e` is rejected ("a lambda needs at
least one parameter"). The minimum forms are `fn x -> e`,
`fn _ -> e` and `fn () -> e`. Lambda parameters carry no type
annotation — `fn (x: T) -> e` fails with E0101 ("a lambda needs
at least one parameter", at the `(`). Broader lambda shapes are
docs/problems/2026-11-01-lambda-param-forms.md.

### `extern` entries always carry `-> T`

`fn` items inside `extern go "m" { ... }` require `-> type-expr`;
there is no signature elision anywhere in the surface grammar
(docs/problems/2026-11-02-fn-return-type-required.md).

### `.gom` fn declarations also require `-> T`

Same production for `fn f () -> Unit = ()` — a return type is
mandatory even though the body would make it inferable. The AST
tolerates `FnDecl.Ret == nil` for recovery, but the parser always
reports the missing `->`.

### Comparisons do not chain

`a < b < c` is an E0101 at the second operator ("comparison
operators do not chain; parenthesize"), matching rel-expr being
non-associative in the grammar.

### Precedence as implemented

From loosest to tightest (grammar.ebnf section 5's table):
`:` ascription (non-associative); the pipe and composition
operators `|>`, `<|`, `|?>`, `>>`, `<<` (left-associative,
the right operand is a bool-or or a bare lambda); `||`; `&&`;
comparisons `== != < <= > >=` (non-associative); `+ -`; `* / %`;
prefix `-` and `try` (the operand is application-level, so
"-f x" is "-(f x)" and "try f x" is "try (f x)");
application by juxtaposition; postfix `.` member access. So
`a |> b || c` is `a |> (b || c)`: the pipe operators are the
loosest binary operators, and `:` binds looser still. There
is no unary `!`, no indexing suffix, and no call-parens
operator — `f(x, y)` is the juxtaposition of `f` and the
tuple `(x, y)`.

### Attribute and following declaration may be separated

The grammar writes `[ attribute ] , ( fn-decl | let-decl )` with no
separator, but spec's own examples put `@test` on its own line. The
parser accepts `sep` between the attribute and the declaration —
recorded as a grammar gap
(docs/problems/2026-11-02-attribute-newline.md).

### `let` head carries an optional `: T`

`let pat : T = e` and `.gomi`'s `let name : T` share
`parseLetHead`. The `: T` is not in grammar.ebnf's `let-decl` at
all — a known gap (docs/problems/2026-11-02-let-type-annotation.md).

### `type X = Y` parses as a one-variant sum

There is no alias production: `type X = Int` yields a `SumBody`
with a single payload-less variant `Int`, and `type X = a`
errors, since variants start with uppercase identifiers. AliasBody
exists in the AST but is unreachable from the grammar
(docs/problems/2026-11-02-type-alias-body.md).

### Unknown attributes are parse errors

`@test` is the only attribute the AST can represent (`TestOnly`).
Any other `@name` is E0101 at parse time and the attribute is
dropped; the declaration still parses
(docs/problems/2026-11-02-unknown-attribute-representation.md).

### Recovery: syncDecl also stops at declaration keywords

Panic-mode sync at the top level stops before `fn let type import
extern test @` as well as at separators. A declaration keyword can
never continue a broken declaration, and stopping preserves the
next declaration even when the broken line ends with `let` — which
suppresses the inserted NL that would otherwise delimit the skip.
Inside item lists sync keeps the old stop set (separators,
closers): item loops rely on consuming ≥1 token per iteration and
their items may legitimately start with `let`/`fn`.

## Permissive parsing notes

A few spots accept a slightly wider input than grammar.ebnf
writes. None of these changes the meaning of valid programs,
so they are recorded here instead of tightened — tightening
is a spec decision, not a parser one.

- A leading `;` inside a `{ ... }` item list is skipped:
  `{ ; a }`, `match e { ; A -> 1 }`, `select { ; ... }` and
  `extern go "m" { ; ... }` all parse. The grammar's
  `[ item , { sep , item } , [ sep ] ]` shape starts with an
  item, not a sep (a leading NL can not occur — `{` is not
  can-end). A leading `;` before the package clause is
  skipped the same way.
- `.gomi`'s `let _ : T` is accepted: `let-sig` admits only
  `ident`, but the shared let head parses `_` as a `WildPat`
  and the interface-file check allows it alongside `VarPat`.

Two wider deviations have their own sections above (the
attribute/declaration separator and the `let` head's `: T`)
and their own `docs/problems/` entries.

## Lexer notes

- `1.x` lexes as `1` `.` `x` (member access), never `1.` float —
  the fraction requires a digit after `.`.
- `_` digit separators must sit between digits: `0x_1` and
  friends report a malformed base literal, matching
  grammar.ebnf's `hex-digit , { hex-digit | "_" }` (a digit is
  required before the first `_`).
- Maximal munch orders `|> |?> ||` before `|`, `<< >>` before
  `< >`, `..` before `.`, `<-` before `<`.
- `///` doc comments merge into one `Comment` when consecutive
  lines sit directly above a declaration; they attach to that
  declaration (`Decl.Doc`).
