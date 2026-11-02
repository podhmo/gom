package format

import (
	"sort"
	"strings"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
)

// This file holds the formatter's driver half: the printer value with
// its comment/blank-line trivia index, the comment anchoring rules,
// and the entry point Source. AST-to-doc building lives in expr.go,
// pat.go, typeexpr.go and decl.go; the item-collection layout (gap
// trivia, blank preservation, trailing-comment alignment) lives in
// items.go. The canonical style is docs/decisions/
// 2026-11-02-gom-fmt-layout.md.
//
// Comments anchor to *gaps*: a comment whose start offset falls
// between two adjacent items of the smallest enclosing item list is
// emitted in that gap — glued after the previous item when it sat on
// the item's own source line, on its own line otherwise. Comments
// inside a node's span but between its children anchor in the
// node's own gaps the same way. /// doc comments the parser attached
// to declarations (Decl.Doc, Variant.Doc, ExternFn.Doc) print from
// the Doc field and are excluded from the free comment list.

// Width is the spec-1.7 line width of gom fmt output.
const Width = 100

// indentStep is the canonical indentation step (spec 1.7).
const indentStep = 2

// Source formats one .gom source file (or .gomi when iface is true)
// and returns the canonical text. It returns nil output when the
// input has error-severity diagnostics — unparseable input is never
// silently passed through.
func Source(src []byte, file string, iface bool) ([]byte, diag.List) {
	var f *syntax.File
	var diags diag.List
	if iface {
		f, diags = syntax.ParseInterface(src, file)
	} else {
		f, diags = syntax.Parse(src, file)
	}
	if diags.HasError() {
		return nil, diags
	}
	p := newPrinter(src, f)
	return p.run(f), diags
}

// cmt is a comment with precomputed line information.
type cmt struct {
	c         *syntax.Comment
	used      bool
	startLine int  // 1-based line of the comment's first byte
	endLine   int  // 1-based line of its last byte
	lineOnly  bool // nothing but whitespace precedes it on its start line
}

// render returns the comment's surface spelling.
func (c *cmt) render() string {
	switch c.c.Kind {
	case syntax.CommentDoc:
		return "///" + c.c.Text
	case syntax.CommentBlock:
		return "/*" + c.c.Text + "*/"
	default:
		return "//" + c.c.Text
	}
}

// renderBlock returns the comment forced into /* */ form — used for
// // comments anchored inside expressions, where the newline a //
// comment swallows would end the construct. A literal "*/" inside the
// text is split as "* /" so the comment survives.
func (c *cmt) renderBlock() string {
	if c.c.Kind == syntax.CommentBlock {
		return c.render()
	}
	// A doc comment rendered mid-expression loses its /// flag: there
	// is no doc position inside an expression anyway, and "/**/"
	// variants do not round-trip cleanly (/***/ parses to body "*").
	body := strings.ReplaceAll(c.c.Text, "*/", "* /")
	if !strings.HasPrefix(body, " ") {
		body = " " + body
	}
	if !strings.HasSuffix(body, " ") {
		body += " "
	}
	return "/*" + body + "*/"
}

// multi reports whether the comment's rendered text spans lines.
func (c *cmt) multi() bool { return strings.Contains(c.c.Text, "\n") }

type printer struct {
	src      []byte
	li       *diag.LineIndex
	comments []*cmt
	blanks   map[int]bool // 1-based line numbers that are blank
}

func newPrinter(src []byte, f *syntax.File) *printer {
	p := &printer{
		src:    src,
		li:     diag.NewLineIndex(src),
		blanks: map[int]bool{},
	}
	for _, b := range f.Blanks {
		p.blanks[p.li.Pos(b.Start).Line] = true
	}
	for _, c := range f.Comments {
		pos := p.li.Pos(c.Sp.Start)
		m := &cmt{
			c:         c,
			startLine: pos.Line,
			endLine:   p.li.Pos(max(c.Sp.End-1, c.Sp.Start)).Line,
		}
		// lineOnly: only whitespace between the line start and the comment.
		ls, _ := p.li.LineBounds(pos.Line)
		m.lineOnly = strings.Trim(string(src[ls:c.Sp.Start]), " \t\r") == ""
		p.comments = append(p.comments, m)
	}
	// Comments folded into a declaration's Doc print from there.
	for _, d := range f.Decls {
		markDocUsed(p.comments, declDoc(d))
	}
	return p
}

// declDoc collects every Doc comment attached inside a declaration
// (the decl itself, variants of a sum type, extern entries).
func declDoc(d syntax.Decl) []*syntax.Comment {
	var docs []*syntax.Comment
	add := func(c *syntax.Comment) {
		if c != nil {
			docs = append(docs, c)
		}
	}
	switch d := d.(type) {
	case *syntax.FnDecl:
		add(d.Doc)
	case *syntax.LetDecl:
		add(d.Doc)
	case *syntax.TypeDecl:
		add(d.Doc)
		if s, ok := d.Body.(*syntax.SumBody); ok {
			for i := range s.Variants {
				add(s.Variants[i].Doc)
			}
		}
	case *syntax.ExternDecl:
		for _, e := range d.Entries {
			if efn, ok := e.(*syntax.ExternFn); ok {
				add(efn.Doc)
			}
		}
	case *syntax.ImportDecl, *syntax.TestDecl:
	}
	return docs
}

// markDocUsed flags every comment covered by a Doc's merged span so
// the gap machinery skips it (it prints from the Doc field instead).
func markDocUsed(cmts []*cmt, docs []*syntax.Comment) {
	for _, d := range docs {
		for _, c := range cmts {
			if c.c.Sp.Start >= d.Sp.Start && c.c.Sp.End <= d.Sp.End {
				c.used = true
			}
		}
	}
}

// lineOf returns the 1-based line containing byte offset off.
func (p *printer) lineOf(off int) int { return p.li.Pos(off).Line }

// take returns the unused comments whose start lies in [lo, hi),
// marking them used. Comments are consumed roughly in source order;
// a binary search finds the first candidate and a linear scan
// collects the rest.
func (p *printer) take(lo, hi int) []*cmt {
	i := sort.Search(len(p.comments), func(i int) bool {
		return p.comments[i].c.Sp.Start >= lo
	})
	var out []*cmt
	for ; i < len(p.comments); i++ {
		c := p.comments[i]
		if c.c.Sp.Start >= hi {
			break
		}
		if c.used {
			continue
		}
		c.used = true
		out = append(out, c)
	}
	return out
}

// sweep consumes comments inside [lo, hi) that no gap claimed —
// typically inside grouping parens the AST dropped — and appends
// them after d. Better an imperfect position than dropping the
// comment or re-anchoring differently on the next pass: a swept
// comment lands in the same node's tail region when reparsed, so the
// output is still idempotent.
func (p *printer) sweep(lo, hi int, d *doc) *doc {
	cs := p.take(lo, hi)
	if len(cs) == 0 {
		return d
	}
	// The leading space mirrors a trailing comment's separator so the
	// comment lands where a reparse will anchor it again; the trailing
	// space is dropped so a following trailing comment's own space
	// does not double up.
	return concat(d, text(" "+strings.TrimRight(inlineTextAny(cs), " ")))
}

// needsSep reports whether the newline between two adjacent items
// would be swallowed by NL-2 — i.e. the previous item's last rendered
// token cannot end a line. The only non-canEnd token an item can end
// with is a ".keyword" member name ("0.type"); "true" and "false"
// members are canEnd and need no help. Only the RENDERED tail is
// checked: dropped grouping parens ("0 (0.type)" → "0 0.type") can
// expose a keyword the source span's ")" had hidden, and synthetic
// wrapping parens ("(f\n  0.type)") end on ")" regardless of what
// the AST span recorded. A "|"-led next item (match/select arms)
// continues without a newline anyway.
func (p *printer) needsSep(d *doc, lo int, next *doc) bool {
	if p.src[lo] == '|' || firstByte(next) == '|' {
		return false
	}
	return tailIsKw(docText(d))
}

// tailIsKw reports whether the last word in s is a reserved word
// that cannot terminate a line. Comments are stripped first — they
// are not tokens and must not mask the word that actually ends the
// line ("0.type /*c*/" still ends on "type").
func tailIsKw(s string) bool {
	s = stripComments(s)
	j := len(s) - 1
	for j >= 0 && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n') {
		j--
	}
	if j < 0 || !isIdentByte(s[j]) {
		return false
	}
	k := j
	for k >= 0 && isIdentByte(s[k]) {
		k--
	}
	w := s[k+1 : j+1]
	return syntax.IsKeyword(w) && w != "true" && w != "false"
}

// stripComments removes /* */ blocks and // line comments from
// rendered text so token-boundary checks see only real tokens.
func stripComments(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				break
			}
			b.WriteByte(' ')
			i += 2 + j + 2
			continue
		}
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '/' {
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				break
			}
			i += j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// docText flattens a doc's rendered text; line markers become '\n'
// so the caller can isolate the last line.
func docText(d *doc) string {
	var sb strings.Builder
	writeDocText(d, &sb)
	return sb.String()
}

func writeDocText(d *doc, sb *strings.Builder) {
	if d == nil {
		return
	}
	switch d.kind {
	case dText:
		sb.WriteString(d.text)
	case dConcat:
		for _, k := range d.kids {
			writeDocText(k, sb)
		}
	case dNest, dGroup:
		writeDocText(d.d, sb)
	case dLazy:
		writeDocText(d.lazy(0, 0), sb)
	case dLine, dSoft, dHard:
		sb.WriteByte('\n')
	case dNil:
	}
}

func isIdentByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' || b == '_'
}

// peek returns the unused comments in [lo, hi) without consuming them.
func (p *printer) peek(lo, hi int) []*cmt {
	i := sort.Search(len(p.comments), func(i int) bool {
		return p.comments[i].c.Sp.Start >= lo
	})
	var out []*cmt
	for ; i < len(p.comments) && p.comments[i].c.Sp.Start < hi; i++ {
		if !p.comments[i].used {
			out = append(out, p.comments[i])
		}
	}
	return out
}

// ------------------------------------------------------------------
// gap events: the ordered comments-and-blanks between two items
//

// gevt is one line's worth of gap trivia: either a comment (text) or
// a preserved blank line.
type gevt struct {
	line  int
	blank bool
	text  string
}

// ------------------------------------------------------------------
// inline gap emission inside expressions
//

// gapInline consumes comments in [lo, hi) and returns their inline
// rendering — " /* a */ /* b */ " — or "" when empty. Line comments
// are rewritten to block form: a //'s swallowed newline is not
// available inside an unbracketed expression.
func (p *printer) gapInline(lo, hi int) string {
	cs := p.take(lo, hi)
	if len(cs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(" " + c.renderBlock() + " ")
	}
	return b.String()
}

// ------------------------------------------------------------------
// entry
//

func (p *printer) run(f *syntax.File) []byte {
	d := p.fileDoc(f)
	out := layout(Width, 0, d)
	// Consume anything the anchors could not place rather than
	// dropping it — appended at end of file.
	var lost []*cmt
	for _, c := range p.comments {
		if !c.used {
			lost = append(lost, c)
		}
	}
	var b strings.Builder
	b.WriteString(out)
	for _, c := range lost {
		if !strings.HasSuffix(out, "\n") {
			b.WriteString("\n")
			out = "\n"
		}
		b.WriteString("\n" + c.render() + "\n")
	}
	s := b.String()
	// Blank lines preserved inside collections render as the line's
	// indentation alone — strip trailing whitespace line by line so
	// output is clean (the layout engine indents on every newline).
	var lines []string
	for _, ln := range strings.Split(s, "\n") {
		lines = append(lines, strings.TrimRight(ln, " \t"))
	}
	s = strings.Join(lines, "\n")
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return []byte(s)
}

// ------------------------------------------------------------------
// precedence contexts
//
// Numbers follow docs/grammar.ebnf section 5, loosest first: an
// expression whose top precedence is lower than the context needs
// parentheses. control (< 0) marks if/match/for/go/select/lambda —
// grammar-level non-operands that take parens anywhere but item or
// pipe-rhs position.
const (
	pAny   = -2 // statement/item position — never parenthesized
	pCtrl  = -1 // control expressions and lambdas
	pPipe  = 10 // |> <| |?> >> <<
	pOr    = 20 // ||
	pAnd   = 30 // &&
	pRel   = 40 // == != < <= > >= (non-associative)
	pAdd   = 50 // + -
	pMul   = 60 // * / %
	pUnary = 70 // - try ignore
	pApply = 80 // juxtaposition
	pPost  = 90 // . member access
	pAtom  = 100
)

// precOf returns the precedence class of e's outermost construct.
func precOf(e syntax.Expr) int {
	switch e := e.(type) {
	case *syntax.Binary:
		switch e.Op {
		case syntax.BinPipeFwd, syntax.BinPipeBack, syntax.BinPipeTry,
			syntax.BinComposeFwd, syntax.BinComposeBack:
			return pPipe
		case syntax.BinOr:
			return pOr
		case syntax.BinAnd:
			return pAnd
		case syntax.BinEq, syntax.BinNe, syntax.BinLt, syntax.BinLe,
			syntax.BinGt, syntax.BinGe:
			return pRel
		case syntax.BinAdd, syntax.BinSub:
			return pAdd
		case syntax.BinMul, syntax.BinDiv, syntax.BinMod:
			return pMul
		default:
			panic("unreachable")
		}
	case *syntax.Unary, *syntax.Try, *syntax.Ignore:
		return pUnary
	case *syntax.Apply:
		return pApply
	case *syntax.Select:
		return pPost
	case *syntax.Ascribe:
		return 1 // loosest of all
	case *syntax.Ident, *syntax.Lit, *syntax.StrInterp, *syntax.Hole,
		*syntax.TupleLit, *syntax.ListLit, *syntax.RecordLit,
		*syntax.Block:
		return pAtom
	case *syntax.If, *syntax.Match, *syntax.For, *syntax.GoExpr,
		*syntax.SelectExpr, *syntax.Lambda:
		return pCtrl
	case *syntax.Let, *syntax.Spread:
		return pAny
	default:
		panic("unreachable")
	}
}

// control reports whether e is one of the grammar's control
// expressions — never an operand or argument without parens.
func control(e syntax.Expr) bool { return precOf(e) == pCtrl }

// colonTail reports whether a ":" written after e would be
// captured inside e rather than ascribing it. Two tails capture:
// a punned "~label" (": T" becomes the label's value — "f ~x : T"
// is "f (~x: T)") and a bare pipe-rhs lambda (": T" sinks into the
// lambda body — "x |> fn y -> y : T" is "x |> fn y -> (y : T)").
// An operand rendered with parens ends in ")" and is safe, so the
// walk only descends into operands that render bare.
func colonTail(e syntax.Expr) bool {
	for {
		switch x := e.(type) {
		case *syntax.Apply:
			return len(x.Args) > 0 && x.Args[len(x.Args)-1].Punned
		case *syntax.Binary:
			prec := precOf(x)
			if prec == pPipe {
				if _, ok := x.Y.(*syntax.Lambda); ok {
					return true
				}
			}
			if operandParen(x.Y, prec+1) {
				return false
			}
			e = x.Y
		case *syntax.Unary:
			if operandParen(x.X, pUnary) {
				return false
			}
			e = x.X
		case *syntax.Try:
			if operandParen(x.X, pUnary) {
				return false
			}
			e = x.X
		case *syntax.Ignore:
			if operandParen(x.X, pUnary) {
				return false
			}
			e = x.X
		case *syntax.Ident, *syntax.Hole, *syntax.Lit, *syntax.StrInterp,
			*syntax.TupleLit, *syntax.ListLit, *syntax.RecordLit,
			*syntax.Select, *syntax.Ascribe, *syntax.If, *syntax.Match,
			*syntax.For, *syntax.GoExpr, *syntax.SelectExpr,
			*syntax.Lambda, *syntax.Let, *syntax.Spread, *syntax.Block:
			return false
		default:
			panic("unreachable")
		}
	}
}

// operandParen reports whether e is parenthesized as an operand at
// precedence ctx — needsParens minus the "{"-led check, which can
// only add parens (still a safe ")" tail either way).
func operandParen(e syntax.Expr, ctx int) bool {
	return control(e) || precOf(e) < ctx
}

// typeTail reports whether e's rendering ends in a type, so a "{"
// or "->" written after it would extend that type instead of playing
// its own role: "if x : T {a : Int}" folds the record type into the
// ascription, and "x <- ch : Int -> b" folds the arm's arrow into a
// function type. Head expressions and arm operands must parenthesize
// such a tail. An ascription's type is its last child; the only
// bare-rendered paths that can reach one are a lambda's body, a
// binary's right operand and a let's value — every other tail ends
// in ")" / "]" / "}" / a literal and is safe.
func typeTail(e syntax.Expr) bool {
	for {
		switch x := e.(type) {
		case *syntax.Ascribe:
			return true
		case *syntax.Lambda:
			e = x.Body
		case *syntax.Spread:
			e = x.X
		case *syntax.Let:
			if x.Value != nil {
				e = x.Value
				continue
			}
			return x.Type != nil
		case *syntax.Binary:
			prec := precOf(x)
			if prec == pPipe {
				if _, ok := x.Y.(*syntax.Lambda); ok {
					// A last pipe operand's lambda renders bare, so
					// the chain's tail is the lambda's own body.
					e = x.Y
					continue
				}
			}
			if operandParen(x.Y, prec+1) {
				return false
			}
			e = x.Y
		case *syntax.Ident, *syntax.Hole, *syntax.Lit, *syntax.StrInterp,
			*syntax.TupleLit, *syntax.ListLit, *syntax.RecordLit,
			*syntax.Select, *syntax.Apply, *syntax.Unary, *syntax.Try,
			*syntax.Ignore, *syntax.If, *syntax.Match, *syntax.For,
			*syntax.GoExpr, *syntax.SelectExpr, *syntax.Block:
			return false
		default:
			panic("unreachable")
		}
	}
}
