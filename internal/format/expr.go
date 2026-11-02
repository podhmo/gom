package format

import (
	"strings"

	"github.com/podhmo/gom/internal/syntax"
)

// This file renders expressions to docs. The two parameters carried
// through every recursive call mirror the parser:
//
//	prec — the precedence of the surrounding context; an expression
//	   whose own precedence is lower wraps in parens. The AST drops
//	   grouping parens, so they are re-derived from the grammar's
//	   table (docs/grammar.ebnf section 5) — "restoring parens needed
//	   for parsing" (spec 1.7) without adding gratuitous ones.
//
//	nb — "no brace": true inside head positions (if/match/for heads
//	   and select subjects), where a leading "{" would bind as a block
//	   instead of an atom (spec 1.6). A brace-led atom under nb
//	   prints parenthesized; nb propagates through the head's operand
//	   and argument positions exactly like the parser's flag and
//	   resets inside any bracketed sub-expression.
//
// Comments inside an expression anchor to the gaps between a node's
// children via gapInline: they print inline as /* */ — a // comment's
// swallowed newline is unavailable mid-expression.

// expr renders e for precedence context prec.
func (p *printer) expr(e syntax.Expr, prec int, nb bool) *doc {
	d := p.exprRaw(e, nb)
	if needsParens(e, d, prec, nb) {
		d = concat(text("("), d, text(")"))
	}
	return p.sweep(e.Span().Start, e.Span().End, d)
}

// needsParens reports whether e must be parenthesized in context
// prec; d is e's already-rendered doc — a leading "{" there is the
// atom class a head expression may not begin (spec 1.6).
func needsParens(e syntax.Expr, d *doc, prec int, nb bool) bool {
	if nb && firstByte(d) == '{' {
		return true
	}
	if prec <= pAny {
		return false
	}
	if control(e) {
		return true // not an operand — the grammar requires parens
	}
	return precOf(e) < prec
}

// head renders a head expression (if/match/for subject): nb position,
// with control expressions parenthesized — legal either way, but
// "if if c {..} else {..} {" is unreadable. A head ending in a type
// tail takes parens too: printed bare, the "{" opening the construct's
// own block reparses as a record-type atom of that type — "if x : T
// {a : Int}" reads "{a : Int}" as a type argument, not as the if's
// block.
func (p *printer) head(e syntax.Expr) *doc {
	d := p.expr(e, pAny, true)
	if control(e) || isLet(e) || typeTail(e) {
		return concat(text("("), d, text(")"))
	}
	return d
}

func isLet(e syntax.Expr) bool {
	_, ok := e.(*syntax.Let)
	return ok
}

// exprRaw renders e without a precedence check.
func (p *printer) exprRaw(e syntax.Expr, nb bool) *doc {
	switch e := e.(type) {
	case *syntax.Ident:
		return text(e.Name)
	case *syntax.Hole:
		return text("_")
	case *syntax.Lit:
		return text(e.Text)
	case *syntax.StrInterp:
		return p.strInterp(e)
	case *syntax.TupleLit:
		return p.braceSeq(docNil, '(', ')', e.Sp.Start, e.Sp.End-1, nil,
			p.exprItems(e.Elems), seqSpec{sep: ",", flatSep: ", ", trailSep: true, lines: 1})
	case *syntax.ListLit:
		return p.braceSeq(docNil, '[', ']', e.Sp.Start, e.Sp.End-1, nil,
			p.exprItems(e.Elems), seqSpec{sep: ",", flatSep: ", ", trailSep: true, lines: 1})
	case *syntax.RecordLit:
		return p.recordLit(e)
	case *syntax.Select:
		mid := p.gapInline(e.Recv.Span().End, e.Sel.Sp.Start)
		return concat(p.expr(e.Recv, pPost, nb), text("."+mid+e.Sel.Name))
	case *syntax.Apply:
		return p.apply(e, nb)
	case *syntax.Unary:
		mid := p.gapInline(e.Sp.Start+len(e.Op.String()), e.X.Span().Start)
		return concat(text(e.Op.String()+mid), p.expr(e.X, pUnary, nb))
	case *syntax.Try:
		mid := p.gapInline(e.Sp.Start+3, e.X.Span().Start)
		return concat(text("try"+pad(mid)), p.expr(e.X, pUnary, nb))
	case *syntax.Ignore:
		mid := p.gapInline(e.Sp.Start+6, e.X.Span().Start)
		return concat(text("ignore"+pad(mid)), p.expr(e.X, pUnary, nb))
	case *syntax.Binary:
		return p.binary(e, nb)
	case *syntax.Ascribe:
		// " : T" cannot break before ":" (not a line-start token) —
		// the ascription stays flat or breaks inside X / the type.
		mid := p.gapInline(e.X.Span().End, e.Type.Span().Start)
		x := p.expr(e.X, pPipe, nb)
		if colonTail(e.X) {
			// "(f ~x) : T" needs the parens: bare "f ~x : T" lets
			// the punned label capture the ":" — it reparses as
			// "f (~x: T)" (grammar.ebnf app-arg's greedy label ":").
			x = concat(text("("), x, text(")"))
		}
		return concat(x, text(" :"+pad(mid)), p.typeExpr(e.Type))
	case *syntax.If:
		return p.ifDoc(e)
	case *syntax.Match:
		return p.matchDoc(e)
	case *syntax.For:
		return p.forDoc(e)
	case *syntax.GoExpr:
		mid := p.gapInline(e.Sp.Start+len("go"), e.Body.Span().Start)
		return concat(text("go"+pad(mid)), p.expr(e.Body, pAny, false))
	case *syntax.SelectExpr:
		return p.selectDoc(e)
	case *syntax.Lambda:
		return p.lambdaDoc(e, nb)
	case *syntax.Let:
		return p.letDoc(e)
	case *syntax.Spread:
		mid := p.gapInline(e.Sp.Start+2, e.X.Span().Start)
		return concat(text(".."+mid), p.expr(e.X, pAny, nb))
	case *syntax.Block:
		return p.blockDoc(e)
	default:
		panic("unreachable")
	}
}

// pad normalizes an inline gap's leading space: "" stays "", a
// comment's " /* c */ " becomes "/* c */ " so the prefix reads
// "kw /* c */ x" without doubling spaces.
func pad(mid string) string {
	if mid == "" {
		return " "
	}
	return strings.TrimSpace(mid) + " "
}

// exprItems wraps expressions as collection items.
func (p *printer) exprItems(elems []syntax.Expr) []item {
	items := make([]item, 0, len(elems))
	for _, el := range elems {
		items = append(items, item{lo: el.Span().Start, hi: el.Span().End, d: p.expr(el, pAny, false)})
	}
	return items
}

// strInterp renders `"parts..."` — flat; a splice's expression keeps
// its own group structure but never introduces a newline (the ${}
// interior is width-indifferent trivia-wise).
func (p *printer) strInterp(e *syntax.StrInterp) *doc {
	kids := []*doc{text("\"")}
	for _, part := range e.Parts {
		switch part := part.(type) {
		case *syntax.StrText:
			kids = append(kids, text(part.Text))
		case *syntax.StrExpr:
			pre := p.gapInline(part.Sp.Start+2, part.X.Span().Start)
			post := p.gapInline(part.X.Span().End, part.Sp.End-1)
			kids = append(kids, text("${"+pre), p.expr(part.X, pAny, false), text(post+"}"))
		default:
			panic("unreachable")
		}
	}
	kids = append(kids, text("\""))
	return concat(kids...)
}

// ------------------------------------------------------------------
// collections
//

// blockDoc renders "{ stmt; ... }" — flat while it fits and no trivia
// intervenes, otherwise one statement per line with blank lines and
// comments preserved.
func (p *printer) blockDoc(b *syntax.Block) *doc {
	items := make([]item, 0, len(b.Stmts))
	for _, it := range b.Stmts {
		items = append(items, item{lo: it.Span().Start, hi: it.Span().End, d: p.itemExpr(it)})
	}
	return p.braceSeq(docNil, '{', '}', b.Sp.Start, b.Sp.End-1, nil, items,
		seqSpec{flatSep: "; ", blanks: true, lines: 1})
}

// itemExpr renders a block-level expression item. An item whose first
// token is "-" wraps in parens: the previous item's expression would
// otherwise absorb it as binary subtraction across the newline.
func (p *printer) itemExpr(e syntax.Expr) *doc {
	d := p.expr(e, pAny, false)
	if p.src[e.Span().Start] == '-' {
		return concat(text("("), d, text(")"))
	}
	return d
}

// recordLit renders "{ f = v }" and "{ base with f = v }".
func (p *printer) recordLit(r *syntax.RecordLit) *doc {
	items := make([]item, 0, len(r.Fields))
	for i := range r.Fields {
		f := &r.Fields[i]
		mid := p.gapInline(f.Name.Span().End, f.Value.Span().Start)
		d := concat(text(f.Name.Name+" ="+pad(mid)), p.expr(f.Value, pAny, false))
		items = append(items, item{lo: f.Sp.Start, hi: f.Value.Span().End, d: p.sweep(f.Sp.Start, f.Value.Span().End, d)})
	}
	var head *doc
	if r.Base != nil {
		mid := p.gapInline(r.Base.Span().End, firstLo(items, r.Sp.End))
		// "with" cannot lead a line — it stays glued to the base.
		head = concat(text(" "), p.expr(r.Base, pAny, false), text(mid+" with"))
	}
	return p.braceSeq(docNil, '{', '}', r.Sp.Start, r.Sp.End-1, head, items,
		seqSpec{sep: ",", flatSep: ", ", trailSep: true, lines: 1})
}

// apply renders "f a1 a2" — flat while it fits. Broken, it wraps in
// parens with one argument per line (spec 1.5): "(f\n  a\n  b)". A
// single argument that is itself a bracketed collection instead
// breaks inside its own brackets — "f(\n  a,\n)" keeps its shape.
// The callee is rendered one level tighter than application so a
// nested "(f a) b" keeps its parens: it is a different tree from
// "f a b", not a re-association — arity is judged per Apply node, so
// flattening the spine would ascribe the inner args to the outer call.
func (p *printer) apply(e *syntax.Apply, nb bool) *doc {
	fn := p.expr(e.Fn, pApply+1, nb)
	args := make([]item, 0, len(e.Args))
	gaps := make([][]*cmt, 0, len(e.Args))
	prevEnd := e.Fn.Span().End
	for i := range e.Args {
		a := &e.Args[i]
		gaps = append(gaps, p.take(prevEnd, a.Sp.Start))
		args = append(args, item{lo: a.Sp.Start, hi: a.Sp.End, d: p.arg(a, nb)})
		prevEnd = a.Sp.End
	}
	singleBracket := len(args) == 1 && bracketFirst(args[0].d)

	var flatKids []*doc
	flatKids = append(flatKids, fn)
	for i, a := range args {
		flatKids = append(flatKids, text(" "), text(inlineTextAny(gaps[i])), a.d)
	}
	flat := concat(flatKids...)

	return lazy(func(col, ind int) *doc {
		if w, ok := flatWidth(flat, col); ok && col+w <= Width {
			return flat
		}
		if singleBracket {
			// "f (\n  arg,\n)" — open the argument's own brackets.
			return concat(fn, text(" "), args[0].d)
		}
		var kids []*doc
		kids = append(kids, fn)
		for i, a := range args {
			kids = append(kids, docHard, text(inlineTextAny(gaps[i])), a.d)
		}
		return concat(text("("), nest(indentStep, concat(kids...)), text(")"))
	})
}

// bracketFirst reports whether the doc's first rendered character is
// an opening bracket — the apply rule's "f (one big arg)" case opens
// the argument's own brackets instead of wrapping it in "()".
func bracketFirst(d *doc) bool {
	switch firstByte(d) {
	case '(', '[', '{':
		return true
	default:
		return false
	}
}

// firstByte returns the first byte the doc renders, or 0 when it
// renders nothing (or cannot be determined without layout).
func firstByte(d *doc) byte {
	for d != nil {
		switch d.kind {
		case dText:
			if d.text == "" {
				return 0
			}
			return d.text[0]
		case dConcat:
			for _, k := range d.kids {
				if b := firstByte(k); b != 0 {
					return b
				}
			}
			return 0
		case dNest, dGroup:
			d = d.d
		case dLazy:
			// First-byte questions are branch-independent: flat and
			// broken forms of a collection start with the same
			// delimiter or open doc.
			d = d.eval(0, 0)
		default:
			return 0
		}
	}
	return 0
}

// arg renders one application argument — "~label: v" / "~label" when
// labeled, otherwise the bare value.
func (p *printer) arg(a *syntax.Arg, nb bool) *doc {
	return p.sweep(a.Sp.Start, a.Sp.End, p.argRaw(a, nb))
}

func (p *printer) argRaw(a *syntax.Arg, nb bool) *doc {
	mid := p.gapInline(a.Sp.Start, a.Value.Span().Start)
	v := p.expr(a.Value, pPost, nb)
	switch {
	case a.Label == nil:
		return concat(text(mid), v)
	case a.Punned:
		return text("~" + a.Label.Name + mid)
	default:
		return concat(text("~"+a.Label.Name+":"+pad(mid)), v)
	}
}

// inlineTextAny renders gap comments inline for unbracketed gaps:
// every comment becomes a trailing "/* ... */ ".
func inlineTextAny(cs []*cmt) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.renderBlock())
		b.WriteByte(' ')
	}
	return b.String()
}

// ------------------------------------------------------------------
// binary chains
//

// binary renders "a op b" — flat while it fits; broken at the
// operator, one operand per line at the enclosing indent:
// "x\n<ind>|> f\n<ind>|> g". The left spine of same-level operators
// flattens into one chain (they are all left-associative).
func (p *printer) binary(e *syntax.Binary, nb bool) *doc {
	prec := precOf(e)
	var ops []syntax.BinOp
	var rands []syntax.Expr
	cur := syntax.Expr(e)
	// Comparisons are non-associative (grammar.ebnf: "at most one") —
	// a same-level left operand keeps its parens instead of merging
	// into the chain.
	if prec != pRel {
		for {
			b, ok := cur.(*syntax.Binary)
			if !ok || precOf(b) != prec {
				break
			}
			ops = append([]syntax.BinOp{b.Op}, ops...)
			rands = append([]syntax.Expr{b.Y}, rands...)
			cur = b.X
		}
	} else {
		b := e
		ops = append(ops, b.Op)
		rands = append(rands, b.Y)
		cur = b.X
	}
	rands = append([]syntax.Expr{cur}, rands...)

	kids := make([]*doc, 0, 3*len(rands))
	lp := prec
	if prec == pRel {
		lp = prec + 1 // "(a < b) == c" keeps the parens
	}
	kids = append(kids, p.expr(rands[0], lp, nb))
	for i := 1; i < len(rands); i++ {
		mid := p.gapInline(rands[i-1].Span().End, rands[i].Span().Start)
		rprec := prec + 1
		if prec == pPipe && i == len(rands)-1 {
			if _, ok := rands[i].(*syntax.Lambda); ok {
				// A bare lambda is legal as pipe-rhs (grammar.ebnf)
				// and the layout keeps it bare — but only as the
				// chain's last operand: a mid-chain lambda's body
				// would swallow the following "|>"s on reparse.
				rprec = pAny
			}
		}
		kids = append(kids, docLine, text(ops[i-1].String()+" "+mid), p.expr(rands[i], rprec, nb))
	}
	return group(concat(kids...))
}

// ------------------------------------------------------------------
// control expressions
//

// ifDoc renders "if c { a } else { b }" — each block decides flat or
// broken on its own via braceSeq.
func (p *printer) ifDoc(e *syntax.If) *doc {
	mid := p.gapInline(e.Sp.Start+len("if"), e.Cond.Span().Start)
	cond := p.head(e.Cond)
	mid2 := p.gapInline(e.Cond.Span().End, e.Then.Span().Start)
	then := p.expr(e.Then, pAny, false)
	if e.Else == nil {
		return concat(text("if"+pad(mid)), cond, text(" "+mid2), then)
	}
	var els *doc
	if _, ok := e.Else.(*syntax.If); ok {
		els = concat(text("else "), p.exprRaw(e.Else, false))
	} else {
		els = concat(text("else "), p.expr(e.Else, pAny, false))
	}
	mid3 := p.gapInline(e.Then.Span().End, e.Else.Span().Start)
	return concat(text("if"+pad(mid)), cond, text(" "+mid2), then, text(" "+mid3), els)
}

// matchDoc renders "match s { arms }" — arms always one per line.
func (p *printer) matchDoc(e *syntax.Match) *doc {
	subj := p.head(e.Subj)
	items := make([]item, 0, len(e.Arms))
	for i := range e.Arms {
		a := &e.Arms[i]
		items = append(items, item{lo: a.Sp.Start, hi: a.Sp.End, d: p.matchArm(a)})
	}
	end := e.Sp.End
	if len(items) > 0 {
		end = items[0].lo
	}
	brace := findByte(p.src, e.Subj.Span().End, end, '{')
	if brace < 0 {
		brace = e.Subj.Span().End
	}
	mid := p.gapInline(e.Sp.Start+len("match"), e.Subj.Span().Start)
	mid2 := p.gapInline(e.Subj.Span().End, brace)
	return p.braceSeq(concat(text("match"+pad(mid)), subj, text(" "+mid2)), '{', '}', brace, e.Sp.End-1, nil, items,
		seqSpec{blanks: true, always: true, lines: 1})
}

// matchArm renders "pat [if guard] -> body".
func (p *printer) matchArm(a *syntax.MatchArm) *doc {
	var kids []*doc
	pd := p.pat(a.Pat, ppOr)
	// A pattern whose first token is "-" wraps in parens: the previous
	// arm's body would absorb it as binary subtraction otherwise
	// (the same merge the parser's "- lit" pattern produces).
	if p.src[a.Pat.Span().Start] == '-' {
		pd = concat(text("("), pd, text(")"))
	}
	kids = append(kids, pd)
	anchor := a.Pat.Span().End
	if a.Guard != nil {
		mid := p.gapInline(anchor, a.Guard.Span().Start)
		kids = append(kids, text(" if"+pad(mid)), p.armOperand(a.Guard))
		anchor = a.Guard.Span().End
	}
	return p.sweep(a.Sp.Start, a.Sp.End, concat(concat(kids...), p.arrowBody(anchor, a.Body)))
}

// arrowBody renders " -> body": a bracket-led or control body glues
// to the arrow ("-> {", "-> [", "-> match s {"), anything else takes
// " ->" plus a breakable line so a long body drops to +2.
func (p *printer) arrowBody(anchorOff int, body syntax.Expr) *doc {
	mid := p.gapInline(anchorOff, body.Span().Start)
	bd := p.expr(body, pAny, false)
	if control(body) || bracketFirst(bd) {
		return concat(text(" ->"+mid+" "), bd)
	}
	return group(concat(text(" ->"+mid), nest(indentStep, concat(docLine, bd))))
}

// gluesBody reports whether a "= v" / "-> v" value binds to the line
// rather than taking the breakable line: blocks, bracket-led values
// (the bracket opens on the head line and carries the break inside)
// and control heads ("if c {", "match s {"). It takes the rendered
// doc because expr() consumes gap comments while it builds — a doc
// built only for probing must never be discarded.
func gluesBody(v syntax.Expr, d *doc) bool {
	return control(v) || bracketFirst(d)
}

// forDoc renders "for pat in iter { body }".
func (p *printer) forDoc(e *syntax.For) *doc {
	mid0 := p.gapInline(e.Sp.Start+len("for"), e.Pat.Span().Start)
	mid := p.gapInline(e.Pat.Span().End, e.Iter.Span().Start)
	mid2 := p.gapInline(e.Iter.Span().End, e.Body.Span().Start)
	return concat(
		text("for"+pad(mid0)), p.pat(e.Pat, ppOr),
		text(" in"+pad(mid)), p.head(e.Iter),
		text(" "+mid2), p.expr(e.Body, pAny, false),
	)
}

// selectDoc renders "select { arms }" — arms always one per line.
func (p *printer) selectDoc(e *syntax.SelectExpr) *doc {
	items := make([]item, 0, len(e.Arms))
	for _, a := range e.Arms {
		items = append(items, item{lo: a.Span().Start, hi: a.Span().End,
			d: p.sweep(a.Span().Start, a.Span().End, p.selectArm(a))})
	}
	brace := findByte(p.src, e.Sp.Start+len("select"), firstLo(items, e.Sp.End), '{')
	if brace < 0 {
		brace = e.Sp.Start + len("select")
	}
	mid := p.gapInline(e.Sp.Start+len("select"), brace)
	return p.braceSeq(text("select"+pad(mid)), '{', '}', brace, e.Sp.End-1, nil, items,
		seqSpec{blanks: true, always: true, lines: 1})
}

// armOperand renders an expression sitting before an arm's "->" — a
// match guard or a receive arm's channel. A type tail takes parens:
// bare, the arrow folds into the type — "x <- ch : Int -> b" reads
// "Int -> b" as a function type and the arm loses its arrow
// (docs/problems/2026-11-02-guard-colon-arrow.md).
func (p *printer) armOperand(e syntax.Expr) *doc {
	d := p.expr(e, pAny, false)
	if typeTail(e) {
		return concat(text("("), d, text(")"))
	}
	return d
}

// selectArm renders one arm: "x <- ch -> e", "send ch v -> e",
// "default -> e".
func (p *printer) selectArm(a syntax.SelectArm) *doc {
	switch a := a.(type) {
	case *syntax.RecvArm:
		mid := p.gapInline(a.Bind.Span().End, a.Chan.Span().Start)
		return concat(
			p.pat(a.Bind, ppOr), text(" <-"+pad(mid)), p.armOperand(a.Chan),
			p.arrowBody(a.Chan.Span().End, a.Body),
		)
	case *syntax.SendArm:
		mid := p.gapInline(a.Sp.Start+len("send"), a.Chan.Span().Start)
		mid2 := p.gapInline(a.Chan.Span().End, a.Value.Span().Start)
		return concat(
			text("send"+pad(mid)), p.expr(a.Chan, pPost, false),
			text(pad(mid2)), p.expr(a.Value, pPost, false),
			p.arrowBody(a.Value.Span().End, a.Body),
		)
	case *syntax.DefaultArm:
		return concat(text("default"), p.arrowBody(a.Sp.Start+len("default"), a.Body))
	default:
		panic("unreachable")
	}
}

// lambdaDoc renders "fn p1 p2 -> body" — lambda params are bare
// names, "_" or "()" (the provisional grammar). A bare lambda is
// legal as a pipe right-hand side; everywhere else the precedence
// check wraps it.
func (p *printer) lambdaDoc(e *syntax.Lambda, nb bool) *doc {
	var kids []*doc
	kids = append(kids, text("fn"))
	anchor := e.Sp.Start + len("fn")
	for i := range e.Params {
		mid := p.gapInline(anchor, e.Params[i].Sp.Start)
		kids = append(kids, text(" "+mid), p.lambdaParam(&e.Params[i]))
		anchor = e.Params[i].Sp.End
	}
	mid := p.gapInline(anchor, e.Body.Span().Start)
	if _, isBlock := e.Body.(*syntax.Block); isBlock {
		return concat(concat(kids...), text(" ->"+mid+" "), p.expr(e.Body, pAny, nb))
	}
	return concat(concat(kids...),
		group(concat(text(" ->"+mid), nest(indentStep, concat(docLine, p.expr(e.Body, pAny, nb))))))
}

// lambdaParam renders a lambda parameter — ident, "_" or "()".
func (p *printer) lambdaParam(pr *syntax.Param) *doc {
	switch pt := pr.Pat.(type) {
	case *syntax.VarPat:
		return text(pt.Name.Name)
	case *syntax.WildPat:
		return text("_")
	case *syntax.LitPat:
		return text(pt.Lit.Text)
	case *syntax.AsPat, *syntax.CtorPat, *syntax.ListPat, *syntax.OrPat,
		*syntax.RecordPat, *syntax.TuplePat:
		return p.pat(pr.Pat, ppOr)
	default:
		panic("unreachable")
	}
}

// letDoc renders "let pat [: T] = value".
func (p *printer) letDoc(e *syntax.Let) *doc {
	mid0 := p.gapInline(e.Sp.Start+len("let"), e.Pat.Span().Start)
	kids := []*doc{text("let" + pad(mid0)), p.pat(e.Pat, ppOr)}
	anchor := e.Pat.Span().End
	if e.Type != nil {
		mid := p.gapInline(anchor, e.Type.Span().Start)
		kids = append(kids, text(":"+pad(mid)), p.typeExpr(e.Type))
		anchor = e.Type.Span().End
	}
	return concat(concat(kids...), p.eqBody(anchor, e.Value))
}

// eqBody renders " = value": a bracket-led or control value glues to
// "=", anything else takes " =" plus a breakable line so a long value
// drops to +2.
func (p *printer) eqBody(anchorOff int, value syntax.Expr) *doc {
	mid := p.gapInline(anchorOff, value.Span().Start)
	vd := p.expr(value, pAny, false)
	if gluesBody(value, vd) {
		return concat(text(" ="+mid+" "), vd)
	}
	return group(concat(text(" ="+mid), nest(indentStep, concat(docLine, vd))))
}

// param renders one fn parameter: "()", "(x)", "(x: T)" or
// "(~label: T)".
func (p *printer) param(pr *syntax.Param) *doc {
	return p.sweep(pr.Sp.Start, pr.Sp.End, p.paramRaw(pr))
}

func (p *printer) paramRaw(pr *syntax.Param) *doc {
	if pr.Label != nil {
		mid := p.gapInline(pr.Sp.Start, pr.Type.Span().Start)
		return concat(text("(~"+pr.Label.Name+":"+pad(mid)), p.typeExpr(pr.Type), text(")"))
	}
	if pr.Type == nil {
		if isUnitPat(pr.Pat) {
			mid0 := p.gapInline(pr.Sp.Start+1, pr.Sp.End-1)
			return text("(" + mid0 + ")")
		}
		mid0 := p.gapInline(pr.Sp.Start+1, pr.Pat.Span().Start)
		return concat(text("("+mid0), p.pat(pr.Pat, ppOr), text(")"))
	}
	mid0 := p.gapInline(pr.Sp.Start+1, pr.Pat.Span().Start)
	mid := p.gapInline(pr.Pat.Span().End, pr.Type.Span().Start)
	return concat(text("("+mid0), p.pat(pr.Pat, ppOr), text(":"+pad(mid)), p.typeExpr(pr.Type), text(")"))
}

// isUnitPat reports whether pat is the literal "()".
func isUnitPat(pat syntax.Pat) bool {
	l, ok := pat.(*syntax.LitPat)
	return ok && l.Lit.Kind == syntax.LitUnit
}
