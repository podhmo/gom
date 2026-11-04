package syntax

import (
	"fmt"
	"strings"

	"github.com/podhmo/gom/internal/diag"
)

// Expression, pattern, and type-expression parsing
// (docs/grammar.ebnf sections 5-7).
//
// The expression grammar is parsed as a precedence chain — each
// level calls the tighter one (grammar.ebnf section 5):
//
//	expr             control-expr | ascribed-expr
//	ascribed-expr    pipe-expr [ ":" type-expr ]          (non-assoc)
//	pipe-expr        or-expr { pipe-op (or-expr|lambda) } (left-assoc)
//	or-expr          and-expr { "||" and-expr }           (left-assoc)
//	and-expr         rel-expr { "&&" rel-expr }           (left-assoc)
//	rel-expr         add-expr [ rel-op add-expr ]         (non-assoc)
//	add-expr         mul-expr { ("+"|"-") mul-expr }      (left-assoc)
//	mul-expr         unary-expr { ("*"|"/"|"%") unary }   (left-assoc)
//	unary-expr       "-" unary | "try" unary | "ignore" unary | apply-expr
//	apply-expr       postfix-expr { app-arg }             (juxtaposition)
//	postfix-expr     atom { "." member-name }
//
// Every expression function takes nb ("no brace"): when true, "{"
// is not allowed to begin an atom — grammar.ebnf section 5's
// head-expr rule, the same rule Rust uses to keep struct literals
// out of if/match conditions. Head positions (if/match/for
// subjects) call the chain with nb=true so "if c { ... }" reads the
// "{" as the construct's block, not as a record literal. The flag
// resets inside every bracketed context — "(", "[", "${", and the
// "{ ... }" item list itself — because "match (f {x = 1})" is legal
// while "match f {x = 1}" is not (grammar.ebnf section 5, the note
// under head-expr).

// parseExpr parses an expression in the default context.
func (p *parser) parseExpr() Expr { return p.parseExprNB(false) }

// parseHead parses a head expression (grammar.ebnf head-expr).
func (p *parser) parseHead() Expr { return p.parseExprNB(true) }

func (p *parser) parseExprNB(nb bool) Expr {
	if isControlStart(p.kind()) {
		return p.parseControl(nb)
	}
	e := p.parsePipe(nb)
	if p.at(tokColon) {
		p.next()
		t := p.parseTypeExpr()
		e = &Ascribe{
			Sp:   p.span(e.Span().Start, t.Span().End),
			X:    e,
			Type: t,
		}
		// ":" is non-associative (grammar.ebnf section 5's
		// precedence table): a second ":" does not parse.
	}
	return e
}

// isControlStart reports whether k begins a control expression.
func isControlStart(k tokenKind) bool {
	switch k {
	case tokIf, tokMatch, tokFor, tokGo, tokSelect, tokFn:
		return true
	default:
		return false
	}
}

// ------------------------------------------------------------------
// precedence chain

// pipeOp maps a pipe-chain operator token to its BinOp.
func pipeOp(k tokenKind) (BinOp, bool) {
	switch k {
	case tokPipeFwd:
		return BinPipeFwd, true
	case tokPipeBack:
		return BinPipeBack, true
	case tokPipeTry:
		return BinPipeTry, true
	case tokComposeF:
		return BinComposeFwd, true
	case tokComposeB:
		return BinComposeBack, true
	default:
		return 0, false
	}
}

// relOp maps a comparison token to its BinOp.
func relOp(k tokenKind) (BinOp, bool) {
	switch k {
	case tokEqEq:
		return BinEq, true
	case tokNe:
		return BinNe, true
	case tokLt:
		return BinLt, true
	case tokLe:
		return BinLe, true
	case tokGt:
		return BinGt, true
	case tokGe:
		return BinGe, true
	default:
		return 0, false
	}
}

func (p *parser) parsePipe(nb bool) Expr {
	x := p.parseOr(nb)
	for {
		op, ok := pipeOp(p.kind())
		if !ok {
			return x
		}
		p.next()
		var y Expr
		if p.at(tokFn) {
			// pipe-rhs grants one exception: a bare lambda follows
			// the operator directly (grammar.ebnf pipe-rhs).
			y = p.parseLambda(nb)
		} else {
			y = p.parseOr(nb)
		}
		x = &Binary{Sp: p.span(x.Span().Start, y.Span().End), Op: op, X: x, Y: y}
	}
}

func (p *parser) parseOr(nb bool) Expr {
	x := p.parseAnd(nb)
	for p.at(tokOr) {
		p.next()
		y := p.parseAnd(nb)
		x = &Binary{Sp: p.span(x.Span().Start, y.Span().End), Op: BinOr, X: x, Y: y}
	}
	return x
}

func (p *parser) parseAnd(nb bool) Expr {
	x := p.parseRel(nb)
	for p.at(tokAnd) {
		p.next()
		y := p.parseRel(nb)
		x = &Binary{Sp: p.span(x.Span().Start, y.Span().End), Op: BinAnd, X: x, Y: y}
	}
	return x
}

func (p *parser) parseRel(nb bool) Expr {
	x := p.parseAdd(nb)
	op, ok := relOp(p.kind())
	if !ok {
		return x
	}
	p.next()
	y := p.parseAdd(nb)
	x = &Binary{Sp: p.span(x.Span().Start, y.Span().End), Op: op, X: x, Y: y}
	// Comparisons are non-associative (grammar.ebnf rel-expr).
	if _, again := relOp(p.kind()); again {
		t := p.cur()
		p.errf(p.spanOf(t),
			"comparison operators do not chain; parenthesize, e.g. \"(a < b) == c\"")
		// Recover left-associatively so the rest parses cleanly.
		for {
			op, ok := relOp(p.kind())
			if !ok {
				break
			}
			p.next()
			y = p.parseAdd(nb)
			x = &Binary{Sp: p.span(x.Span().Start, y.Span().End), Op: op, X: x, Y: y}
		}
	}
	return x
}

func (p *parser) parseAdd(nb bool) Expr {
	x := p.parseMul(nb)
	for {
		var op BinOp
		switch p.kind() {
		case tokPlus:
			op = BinAdd
		case tokMinus:
			op = BinSub
		default:
			return x
		}
		p.next()
		y := p.parseMul(nb)
		x = &Binary{Sp: p.span(x.Span().Start, y.Span().End), Op: op, X: x, Y: y}
	}
}

func (p *parser) parseMul(nb bool) Expr {
	x := p.parseUnary(nb)
	for {
		var op BinOp
		switch p.kind() {
		case tokStar:
			op = BinMul
		case tokSlash:
			op = BinDiv
		case tokPercent:
			op = BinMod
		default:
			return x
		}
		p.next()
		y := p.parseUnary(nb)
		x = &Binary{Sp: p.span(x.Span().Start, y.Span().End), Op: op, X: x, Y: y}
	}
}

func (p *parser) parseUnary(nb bool) Expr {
	t := p.cur()
	switch t.kind {
	case tokMinus:
		p.next()
		x := p.parseUnary(nb)
		return &Unary{Sp: p.span(t.start, x.Span().End), Op: UnNeg, X: x}
	case tokTry:
		p.next()
		x := p.parseUnary(nb)
		return &Try{Sp: p.span(t.start, x.Span().End), X: x}
	case tokIgnore:
		p.next()
		x := p.parseUnary(nb)
		return &Ignore{Sp: p.span(t.start, x.Span().End), X: x}
	default:
		return p.parseApply(nb)
	}
}

// parseApply parses juxtaposition application: an expression
// followed by one or more app-args. A "." that follows a
// parenthesized argument belongs to the application, continuing the
// chain on its result — "x.M(1).N(2)" is "((x.M 1).N) 2", the same
// left-associative method chain Go parses — rather than a selector
// on the argument tuple
// (docs/decisions/2026-11-05-post-arg-selector-binding.md).
func (p *parser) parseApply(nb bool) Expr {
	x := p.parsePostfix(nb)
	for {
		var args []Arg
		for p.atAppArg(nb) {
			args = append(args, p.parseAppArg(nb))
		}
		if len(args) == 0 {
			return x
		}
		x = &Apply{Sp: p.span(x.Span().Start, p.prevEnd()), Fn: x, Args: args}
		for p.at(tokDot) {
			p.next()
			m := p.memberName()
			x = &Select{Sp: p.span(x.Span().Start, m.Span().End), Recv: x, Sel: m}
		}
	}
}

// atAppArg reports whether the current token begins an application
// argument — a simple-expr starter plus "~"; "{" counts except in a
// head expression (nb).
func (p *parser) atAppArg(nb bool) bool {
	switch p.kind() {
	case tokIdent, tokUIdent, tokInt, tokFloat, tokChar, tokString,
		tokStrOpen, tokTrue, tokFalse, tokUnder, tokLParen, tokLBrack,
		tokTilde:
		return true
	case tokLBrace:
		return !nb
	default:
		return false
	}
}

// parseAppArg parses "~" ident [ ":" simple-expr ] | simple-expr.
// The ":" after a label is greedy: it belongs to the argument, so
// "f ~x : T" passes T as the labeled value (grammar.ebnf app-arg).
// The value is one simple-expr (postfix level): "~to: f x" binds
// "~to: f" and makes "x" a separate positional argument.
func (p *parser) parseAppArg(nb bool) Arg {
	start := p.cur().start
	if p.at(tokTilde) {
		p.next()
		nameTok := p.cur()
		if nameTok.kind != tokIdent {
			p.expected("a label name after \"~\"")
			v := p.parsePostfix(nb)
			return Arg{Sp: p.span(start, v.Span().End), Value: v}
		}
		p.next()
		label := &Ident{Sp: p.spanOf(nameTok), Name: p.text(nameTok)}
		if p.at(tokColon) {
			p.next()
			v := p.parseAppArgValue(nb)
			return Arg{Sp: p.span(start, v.Span().End), Label: label, Value: v}
		}
		// Punned label "~x" is "~x: x".
		return Arg{
			Sp:     p.span(start, nameTok.end),
			Label:  label,
			Punned: true,
			Value:  &Ident{Sp: p.spanOf(nameTok), Name: label.Name},
		}
	}
	v := p.parseAppArgValue(nb)
	return Arg{Sp: v.Span(), Value: v}
}

// parseAppArgValue parses the value of one application argument —
// one simple-expr, except that a leading "(" stops at the
// paren-expr itself: a selector following the ")" belongs to the
// enclosing application so "f(x).M(y)" chains on the result like
// Go (docs/decisions/2026-11-05-post-arg-selector-binding.md). To
// select on the parenthesized value write "(e.f)" or drop the
// parens — "f e.f" already binds the "." inside the argument.
func (p *parser) parseAppArgValue(nb bool) Expr {
	if p.at(tokLParen) {
		return p.parseParen()
	}
	return p.parsePostfix(nb)
}

// parsePostfix parses atom { "." member-name }.
func (p *parser) parsePostfix(nb bool) Expr {
	x := p.parseAtom(nb)
	for p.at(tokDot) {
		p.next()
		m := p.memberName()
		x = &Select{Sp: p.span(x.Span().Start, m.Span().End), Recv: x, Sel: m}
	}
	return x
}

// memberName parses the identifier after ".". Keywords and
// uppercase names are permitted (grammar.ebnf member-name).
func (p *parser) memberName() *Ident {
	t := p.cur()
	if t.kind == tokIdent || t.kind == tokUIdent || t.kind.isKeyword() {
		p.next()
		return &Ident{Sp: p.spanOf(t), Name: p.text(t)}
	}
	p.expected("a member name after \".\"")
	return &Ident{Sp: diag.Point(p.file, t.start), Name: "_"}
}

// parseAtom parses the atomic expressions. It returns an errExpr
// placeholder (never nil) on failure.
func (p *parser) parseAtom(nb bool) Expr {
	t := p.cur()
	switch t.kind {
	case tokInt:
		p.next()
		return &Lit{Sp: p.spanOf(t), Kind: LitInt, Text: intLitText(p.text(t))}
	case tokFloat:
		p.next()
		return &Lit{Sp: p.spanOf(t), Kind: LitFloat, Text: p.text(t)}
	case tokChar:
		p.next()
		return &Lit{Sp: p.spanOf(t), Kind: LitChar, Text: p.text(t)}
	case tokString:
		p.next()
		return &Lit{Sp: p.spanOf(t), Kind: LitStr, Text: p.text(t)}
	case tokStrOpen:
		return p.parseStrInterp()
	case tokTrue, tokFalse:
		p.next()
		return &Lit{Sp: p.spanOf(t), Kind: LitBool, Text: p.text(t)}
	case tokIdent, tokUIdent:
		p.next()
		return &Ident{Sp: p.spanOf(t), Name: p.text(t)}
	case tokUnder:
		p.next()
		return &Hole{Sp: p.spanOf(t)}
	case tokLParen:
		return p.parseParen()
	case tokLBrack:
		return p.parseList()
	case tokLBrace:
		if nb {
			// A "{" in a head expression ends the head — but when
			// it begins the head itself there is no expression at
			// all. Report and recover by parsing the brace expr:
			// "if {x = 1} {y}" keeps its record as the condition.
			p.errf(p.spanOf(t),
				"\"{\" cannot start an expression in a head position (parenthesize it)")
			return p.parseBraceExpr()
		}
		return p.parseBraceExpr()
	default:
		p.errf(p.spanOf(t), "expected an expression, found %s", p.describe(t))
		return p.errExpr(p.spanOf(t))
	}
}

// ------------------------------------------------------------------
// control expressions (grammar.ebnf section 5)

func (p *parser) parseControl(nb bool) Expr {
	switch p.kind() {
	case tokIf:
		return p.parseIf()
	case tokMatch:
		return p.parseMatch()
	case tokFor:
		return p.parseFor()
	case tokGo:
		return p.parseGo()
	case tokSelect:
		return p.parseSelect()
	case tokFn:
		return p.parseLambda(nb)
	default:
		panic("unreachable")
	}
}

// parseIf parses "if" head-expr block [ "else" block ]. "else if"
// is not in the grammar; a following "if" is recovered by wrapping
// it in a block — docs/problems/2026-11-01-else-if.md.
func (p *parser) parseIf() Expr {
	start := p.next() // 'if'
	cond := p.parseHead()
	then := p.parseRequiredBlock("if")
	e := &If{
		Sp:   p.span(start.start, then.Span().End),
		Cond: cond,
		Then: then,
	}
	if p.at(tokElse) {
		p.next()
		if p.at(tokIf) {
			t := p.cur()
			p.errf(p.spanOf(t),
				"\"else\" must be followed by a block; write \"else { if ... }\"")
			inner := p.parseIf()
			e.Else = &Block{
				Sp:    p.span(t.start, inner.Span().End),
				Stmts: []Expr{inner},
			}
		} else {
			e.Else = p.parseRequiredBlock("else")
		}
		e.Sp.End = e.Else.Span().End
	}
	return e
}

// parseMatch parses
// "match" head-expr "{" [ match-arm { sep match-arm } [ sep ] ] "}".
func (p *parser) parseMatch() Expr {
	start := p.next() // 'match'
	subj := p.parseHead()
	m := &Match{Sp: p.span(start.start, p.prevEnd()), Subj: subj}
	if !p.expectBlockOpen("match") {
		p.syncItem()
		return m
	}
	closed := false
	for {
		p.skipSep()
		if p.at(tokRBrace) {
			t := p.next()
			m.Sp.End = t.end
			closed = true
			break
		}
		if p.atForeignClose(tokRBrace) {
			break
		}
		// "|" starts a variant alternative or or-pattern — match
		// arms are separated by newlines, not "|".
		if p.at(tokBar) {
			t := p.next()
			p.errf(p.spanOf(t),
				"match arms are separated by newlines, not \"|\"")
		}
		if arm := p.parseMatchArm(); arm != nil {
			m.Arms = append(m.Arms, *arm)
			m.Sp.End = arm.Sp.End
		}
		if !p.sepHere() && !p.at(tokBar) {
			t := p.cur()
			p.errf(p.spanOf(t),
				"expected a newline, \";\" or \"}\" between match arms, found %s",
				p.describe(t))
			p.syncItem()
		}
	}
	if !closed {
		p.expected("\"}\"")
	}
	return m
}

// parseMatchArm parses pattern [ "if" expr ] "->" expr.
func (p *parser) parseMatchArm() *MatchArm {
	start := p.cur().start
	pat := p.parsePattern()
	var guard Expr
	lhs := p.srcText(pat.Span())
	if p.at(tokIf) {
		p.next()
		guard = p.parseExpr()
		lhs += " if"
	}
	if !p.at(tokArrow) && (p.at(tokNL) || p.at(tokSemi)) {
		p.errf(p.spanOf(p.cur()),
			"a match arm's \"->\" must stay on the same line as its pattern")
		p.skipSep()
	}
	p.expectArmArrow(lhs, guard,
		"expected \"->\" between the pattern and the arm body, found %s",
		p.describe(p.cur()))
	body := p.parseExpr()
	return &MatchArm{
		Sp:    p.span(start, body.Span().End),
		Pat:   pat,
		Guard: guard,
		Body:  body,
	}
}

// parseFor parses "for" pattern "in" head-expr block.
func (p *parser) parseFor() Expr {
	start := p.next() // 'for'
	pat := p.parsePattern()
	p.expect(tokIn)
	iter := p.parseHead()
	body := p.parseRequiredBlock("for")
	return &For{
		Sp:   p.span(start.start, body.Span().End),
		Pat:  pat,
		Iter: iter,
		Body: body,
	}
}

// parseGo parses "go" block.
func (p *parser) parseGo() Expr {
	start := p.next() // 'go'
	body := p.parseRequiredBlock("go")
	return &GoExpr{Sp: p.span(start.start, body.Span().End), Body: body}
}

// parseSelect parses
// "select" "{" [ select-arm { sep select-arm } [ sep ] ] "}".
// The "{" belongs to the construct, so the head restriction does
// not apply to it.
func (p *parser) parseSelect() Expr {
	start := p.next() // 'select'
	s := &SelectExpr{Sp: p.span(start.start, p.prevEnd())}
	if !p.expectBlockOpen("select") {
		p.syncItem()
		return s
	}
	closed := false
	for {
		p.skipSep()
		if p.at(tokRBrace) {
			t := p.next()
			s.Sp.End = t.end
			closed = true
			break
		}
		if p.atForeignClose(tokRBrace) {
			break
		}
		if arm := p.parseSelectArm(); arm != nil {
			s.Arms = append(s.Arms, arm)
			s.Sp.End = arm.Span().End
		}
		if !p.sepHere() {
			t := p.cur()
			p.errf(p.spanOf(t),
				"expected a newline, \";\" or \"}\" between select arms, found %s",
				p.describe(t))
			p.syncItem()
		}
	}
	if !closed {
		p.expected("\"}\"")
	}
	return s
}

// parseSelectArm parses one select arm:
//
//	"send" simple-expr simple-expr "->" expr      send
//	"default" "->" expr                         default
//	(ident | "_") "<-" expr "->" expr           receive
//
// The send operands are restricted to simple-expr (postfix level)
// [PROVISIONAL — docs/problems/2026-11-01-select-send-arm-operands.md];
// the receive channel is a full expr (grammar.ebnf recv-arm).
func (p *parser) parseSelectArm() SelectArm {
	start := p.cur()
	switch start.kind {
	case tokSend:
		p.next()
		ch := p.parsePostfix(false)
		val := p.parsePostfix(false)
		p.expectArmArrow("send "+p.srcText(ch.Span()), val,
			"expected \"->\" after the send arm's channel and value, found %s",
			p.describe(p.cur()))
		body := p.parseExpr()
		return &SendArm{
			Sp:    p.span(start.start, body.Span().End),
			Chan:  ch,
			Value: val,
			Body:  body,
		}
	case tokDefault:
		p.next()
		if _, ok := p.expect(tokArrow); !ok && p.syncArmArrow() {
			p.next()
		}
		body := p.parseExpr()
		return &DefaultArm{
			Sp:   p.span(start.start, body.Span().End),
			Body: body,
		}
	case tokIdent, tokUnder:
		p.next()
		var bind Pat
		if start.kind == tokIdent {
			bind = &VarPat{
				Sp:   p.spanOf(start),
				Name: &Ident{Sp: p.spanOf(start), Name: p.text(start)},
			}
		} else {
			bind = &WildPat{Sp: p.spanOf(start)}
		}
		p.expect(tokLArrow)
		ch := p.parseExpr()
		p.expectArmArrow(p.srcText(bind.Span())+" <-", ch,
			"expected \"->\" after the receive arm's channel, found %s",
			p.describe(p.cur()))
		body := p.parseExpr()
		return &RecvArm{
			Sp:   p.span(start.start, body.Span().End),
			Bind: bind,
			Chan: ch,
			Body: body,
		}
	default:
		p.errf(p.spanOf(start),
			"expected a select arm (\"send\", \"default\", or \"name <-\"), found %s",
			p.describe(start))
		p.syncItem()
		return nil
	}
}

// expectArmArrow consumes the "->" ending the left side of a match
// or select arm, or reports it missing. operand is the expression
// the arrow follows — a match guard, a receive channel or a send
// value — and lhs is the arm's left side in source text; both are
// used only for the ascription note. When operand is a top-level
// "e : T -> U" ascription the arm's "->" was absorbed into the
// type — a genuine ambiguity of the grammar
// (docs/problems/2026-11-02-guard-colon-arrow.md) — so the
// diagnostic carries a note showing the parenthesized form.
// Recovery then looks ahead: a "->" before the next separator,
// "|" or closer becomes the arm's arrow so the body still parses.
func (p *parser) expectArmArrow(lhs string, operand Expr, format string, args ...any) {
	if p.at(tokArrow) {
		p.next()
		return
	}
	d := diag.Error("E0101", fmt.Sprintf(format, args...)).At(p.spanOf(p.cur()))
	if a, ok := operand.(*Ascribe); ok {
		if fn, ok := a.Type.(*TypeFn); ok {
			// The intended ascription is "X : T" where T is the
			// arrow's parameter list — everything up to the last
			// "->", whose target is the arm's real body.
			ty := p.srcText(p.span(
				fn.Params[0].Sp.Start,
				fn.Params[len(fn.Params)-1].Sp.End))
			d = d.AddNote(fmt.Sprintf(
				"the \"->\" was read as part of the type ascription; parenthesize it: \"%s (%s : %s) -> %s\"",
				lhs, p.srcText(a.X.Span()), ty, p.srcText(fn.Ret.Span())))
		}
	}
	p.diags.Add(d)
	if p.syncArmArrow() {
		p.next()
	}
}

// parseLambda parses "fn" lambda-param { lambda-param } "->" expr.
// Only the minimal parameter forms ident, "_" and "()" are parsed
// [PROVISIONAL — docs/problems/2026-11-01-lambda-param-forms.md].
// nb propagates to the body: a lambda inside a head expression
// cannot contain a bare "{" either.
func (p *parser) parseLambda(nb bool) Expr {
	start := p.next() // 'fn'
	var params []Param
	for {
		t := p.cur()
		switch {
		case t.kind == tokIdent:
			p.next()
			id := &Ident{Sp: p.spanOf(t), Name: p.text(t)}
			params = append(params, Param{
				Sp:  p.spanOf(t),
				Pat: &VarPat{Sp: p.spanOf(t), Name: id},
			})
		case t.kind == tokUnder:
			p.next()
			params = append(params, Param{
				Sp:  p.spanOf(t),
				Pat: &WildPat{Sp: p.spanOf(t)},
			})
		case t.kind == tokLParen && p.peek(1).kind == tokRParen:
			p.next()
			close := p.next()
			sp := p.span(t.start, close.end)
			params = append(params, Param{
				Sp:  sp,
				Pat: &LitPat{Sp: sp, Lit: &Lit{Sp: sp, Kind: LitUnit, Text: "()"}},
			})
		default:
			goto paramsDone
		}
	}
paramsDone:
	if len(params) == 0 {
		p.errf(p.spanOf(p.cur()),
			"a lambda needs at least one parameter (a name, \"_\" or \"()\")"+
				", found %s", p.describe(p.cur()))
	}
	p.expectMsg(tokArrow,
		"expected \"->\" between the lambda parameters and the body, found %s",
		p.describe(p.cur()))
	body := p.parseExprNB(nb)
	return &Lambda{
		Sp:     p.span(start.start, body.Span().End),
		Params: params,
		Body:   body,
	}
}

// ------------------------------------------------------------------
// bracketed expressions

// expectBlockOpen reports whether the current token is "{" and
// consumes it. When the token is a separator instead it reports the
// spec-1.5 violation with a targeted message — but only when a "{"
// actually follows on a later line; otherwise the block itself is
// missing and the generic expectation applies.
func (p *parser) expectBlockOpen(what string) bool {
	if p.at(tokLBrace) {
		p.next()
		return true
	}
	art := article(what)
	if p.at(tokNL) || p.at(tokSemi) {
		if p.braceAfterSep() {
			p.errf(p.spanOf(p.cur()),
				"the \"{\" of %s %s must stay on the same line as its head", art, what)
		} else {
			p.errf(p.spanOf(p.cur()),
				"expected \"{\" to open %s %s, found %s", art, what, p.describe(p.cur()))
		}
	} else {
		p.errf(p.spanOf(p.cur()), "expected \"{\" to open %s %s, found %s",
			art, what, p.describe(p.cur()))
	}
	return false
}

// braceAfterSep reports whether the next token after the current run
// of separators is a "{". It does not consume anything.
func (p *parser) braceAfterSep() bool {
	i := 0
	for p.peek(i).kind == tokNL || p.peek(i).kind == tokSemi {
		i++
	}
	return p.peek(i).kind == tokLBrace
}

// article returns the English indefinite article for a construct
// name — "an" before a vowel letter, "a" otherwise.
func article(what string) string {
	switch what[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an"
	default:
		return "a"
	}
}

// parseRequiredBlock parses "{" block "}" for constructs whose body
// is always a block (if/for/go/test/match/select braces are part of
// the construct, never an expression — grammar.ebnf block).
func (p *parser) parseRequiredBlock(what string) *Block {
	start := p.cur().start
	if !p.expectBlockOpen(what) {
		p.syncItem()
		return &Block{Sp: diag.Point(p.file, start)}
	}
	return p.blockItems(nil, start)
}

// blockItems parses [ block-item { sep block-item } [ sep ] ] "}"
// after the opening "{" was consumed. first, when non-nil, is a
// pre-parsed first item (the "{ expr" of the brace disambiguation).
// A leading ";" is tolerated (NL never leads — "{" is not can-end).
func (p *parser) blockItems(first Expr, start int) *Block {
	b := &Block{Sp: p.span(start, p.prevEnd())}
	if first != nil {
		b.Stmts = append(b.Stmts, first)
	}
	for {
		if len(b.Stmts) > 0 && !p.sepHere() {
			t := p.cur()
			p.errf(p.spanOf(t),
				"expected a newline, \";\" or \"}\" between block items, found %s",
				p.describe(t))
			p.syncItem()
		}
		p.skipSep()
		if p.at(tokRBrace) {
			t := p.next()
			b.Sp.End = t.end
			return b
		}
		if p.atForeignClose(tokRBrace) {
			p.expected("\"}\"")
			b.Sp.End = p.prevEnd()
			return b
		}
		b.Stmts = append(b.Stmts, p.parseBlockItem())
		b.Sp.End = b.Stmts[len(b.Stmts)-1].Span().End
	}
}

// parseBlockItem parses "let" pattern [":" type] "=" expr or an
// expression.
func (p *parser) parseBlockItem() Expr {
	if !p.at(tokLet) {
		return p.parseExpr()
	}
	start := p.next() // 'let'
	pat, typ := p.parseLetHead()
	p.expect(tokEq)
	v := p.parseExpr()
	return &Let{
		Sp:    p.span(start.start, v.Span().End),
		Pat:   pat,
		Type:  typ,
		Value: v,
	}
}

// parseParen parses "(", handling "()" (Unit), "(..xs)" (Spread),
// grouping with optional trailing comma, and tuples.
func (p *parser) parseParen() Expr {
	open := p.next() // '('
	if p.at(tokRParen) {
		close := p.next()
		return &Lit{Sp: p.span(open.start, close.end), Kind: LitUnit, Text: "()"}
	}
	if p.at(tokDotDot) {
		start := p.next()
		x := p.parseExpr()
		end := p.prevEnd()
		if t, ok := p.expect(tokRParen); ok {
			end = t.end
		}
		return &Spread{Sp: p.span(start.start, end), X: x}
	}
	first := p.parseExpr()
	if !p.at(tokComma) {
		if _, ok := p.expect(tokRParen); ok {
			return first // grouping parens produce no node
		}
		return first
	}
	elems := []Expr{first}
	for p.at(tokComma) {
		p.next()
		if p.at(tokRParen) {
			// Trailing comma: "(x,)" is still the grouped x
			// (grammar.ebnf paren-expr), "(x, y,)" is a tuple.
			p.next()
			if len(elems) == 1 {
				return first
			}
			return &TupleLit{Sp: p.span(open.start, p.prevEnd()), Elems: elems}
		}
		if p.at(tokDotDot) {
			start := p.next()
			x := p.parseExpr()
			elems = append(elems, &Spread{Sp: p.span(start.start, x.Span().End), X: x})
			// ".." may only appear as the last item.
			end := p.prevEnd()
			if t, ok := p.expect(tokRParen); ok {
				end = t.end
			}
			return &TupleLit{Sp: p.span(open.start, end), Elems: elems}
		}
		elems = append(elems, p.parseExpr())
	}
	end := p.prevEnd()
	if t, ok := p.expect(tokRParen); ok {
		end = t.end
	}
	return &TupleLit{Sp: p.span(open.start, end), Elems: elems}
}

// parseList parses "[" [ expr { "," expr } [ "," ".." expr ] [ "," ] ] "]".
// A trailing ".." element splices a whole list — [x, ..xs] is cons —
// spec section 6 and decisions/2026-11-03-list-literal-spread.md; the
// same tail shape as list patterns.
func (p *parser) parseList() Expr {
	open := p.next() // '['
	l := &ListLit{Sp: p.span(open.start, p.prevEnd())}
	for {
		if p.at(tokRBrack) {
			t := p.next()
			l.Sp.End = t.end
			return l
		}
		if p.atForeignClose(tokRBrack) {
			p.expected("\"]\"")
			l.Sp.End = p.prevEnd()
			return l
		}
		if p.at(tokDotDot) {
			start := p.next()
			x := p.parseExpr()
			l.Elems = append(l.Elems, &Spread{Sp: p.span(start.start, x.Span().End), X: x})
		} else {
			l.Elems = append(l.Elems, p.parseExpr())
		}
		l.Sp.End = l.Elems[len(l.Elems)-1].Span().End
		if p.at(tokComma) {
			p.next()
			continue
		}
		if p.atCloseOrEOF() {
			continue // the loop head closes or reports
		}
		t := p.cur()
		p.errf(p.spanOf(t),
			"expected \",\" or \"]\" between list elements, found %s", p.describe(t))
		p.syncItemSep()
	}
}

// parseBraceExpr implements the "{" disambiguation of grammar.ebnf
// section 8. After "{":
//
//	"}"            → empty block (the Unit-ish empty block)
//	"let"          → block
//	ident "="      → record literal
//	expr-starter   → parse an expr; then "with" → record update, else block
//	anything else  → error
func (p *parser) parseBraceExpr() Expr {
	open := p.next() // '{'
	start := open.start
	p.skipSep() // tolerate explicit ";" (NL never follows "{")
	switch {
	case p.at(tokRBrace):
		close := p.next()
		return &Block{Sp: p.span(start, close.end)}
	case p.at(tokLet):
		return p.blockItems(nil, start)
	case p.at(tokIdent) && p.peek(1).kind == tokEq:
		r := &RecordLit{Sp: p.span(start, p.prevEnd())}
		p.parseFieldInits(r)
		return r
	case canStartExpr(p.kind()):
		e := p.parseExpr()
		if p.at(tokWith) {
			p.next()
			r := &RecordLit{Sp: p.span(start, p.prevEnd()), Base: e}
			if p.at(tokRBrace) {
				p.errf(p.spanOf(p.cur()),
					"a record update needs at least one \"name = value\" field")
			}
			p.parseFieldInits(r)
			return r
		}
		return p.blockItems(e, start)
	default:
		t := p.cur()
		p.errf(p.spanOf(t),
			"unexpected %s after \"{\" (a block, a record \"{x = v}\","+
				" or \"{e with x = v}\" was expected)", p.describe(t))
		p.syncItem()
		if p.at(tokRBrace) {
			close := p.next()
			return &Block{Sp: p.span(start, close.end)}
		}
		return &Block{Sp: p.span(start, p.prevEnd())}
	}
}

// canStartExpr reports whether k can begin an expression (the
// "expr-starter" branch of the "{" disambiguation).
func canStartExpr(k tokenKind) bool {
	switch k {
	case tokIdent, tokUIdent, tokInt, tokFloat, tokChar, tokString,
		tokStrOpen, tokTrue, tokFalse, tokUnder, tokLParen, tokLBrack,
		tokLBrace, tokMinus, tokTry, tokIgnore:
		return true
	default:
		return isControlStart(k)
	}
}

// parseFieldInits parses field-init { "," field-init } [ "," ] "}"
// into r, for both record literals and record updates. The grammar
// admits no separators inside a record — fields are comma-separated
// and a newline may follow a comma only.
func (p *parser) parseFieldInits(r *RecordLit) {
	for {
		if p.at(tokRBrace) {
			t := p.next()
			r.Sp.End = t.end
			return
		}
		if p.atForeignClose(tokRBrace) {
			p.expected("\"}\"")
			r.Sp.End = p.prevEnd()
			return
		}
		if f := p.parseFieldInit(); f != nil {
			r.Fields = append(r.Fields, *f)
			r.Sp.End = f.Sp.End
		} else {
			p.skipSep()
			continue
		}
		if p.at(tokComma) {
			p.next()
			continue
		}
		if p.atCloseOrEOF() {
			continue // the loop head closes or reports
		}
		t := p.cur()
		p.errf(p.spanOf(t),
			"expected \",\" or \"}\" between record fields, found %s", p.describe(t))
		p.syncItemSep()
	}
}

// parseFieldInit parses ident "=" expr.
func (p *parser) parseFieldInit() *FieldInit {
	t := p.cur()
	if t.kind != tokIdent {
		p.errf(p.spanOf(t), "expected a field name, found %s", p.describe(t))
		p.syncItem()
		return nil
	}
	p.next()
	name := &Ident{Sp: p.spanOf(t), Name: p.text(t)}
	p.expect(tokEq)
	v := p.parseExpr()
	return &FieldInit{
		Sp:    p.span(t.start, v.Span().End),
		Name:  name,
		Value: v,
	}
}

// parseStrInterp parses an interpolated string: the lexer emitted
// tokStrOpen for the opening quote and the parts follow.
func (p *parser) parseStrInterp() Expr {
	open := p.next() // tokStrOpen
	s := &StrInterp{Sp: p.span(open.start, open.end)}
	for {
		t := p.cur()
		switch t.kind {
		case tokStrText:
			p.next()
			s.Parts = append(s.Parts, &StrText{Sp: p.spanOf(t), Text: p.text(t)})
			s.Sp.End = t.end
		case tokInterpStart:
			p.next()
			x := p.parseExpr()
			p.expectMsg(tokInterpEnd,
				"expected \"}\" to close the string interpolation, found %s",
				p.describe(p.cur()))
			s.Parts = append(s.Parts, &StrExpr{
				Sp: p.span(t.start, p.prevEnd()),
				X:  x,
			})
			s.Sp.End = p.prevEnd()
		case tokStrClose:
			p.next()
			s.Sp.End = t.end
			return s
		default:
			p.errf(p.spanOf(t),
				"expected a string part, found %s", p.describe(t))
			p.next() // drop the offender so the caller can continue
			s.Sp.End = p.prevEnd()
			return s
		}
	}
}

// ------------------------------------------------------------------
// patterns (grammar.ebnf section 6)

// parsePattern parses pattern = or-pattern [ "as" ident ].
func (p *parser) parsePattern() Pat {
	pat := p.parseOrPat()
	if p.at(tokAs) {
		p.next()
		t := p.cur()
		if t.kind == tokIdent {
			p.next()
			pat = &AsPat{
				Sp:   p.span(pat.Span().Start, t.end),
				Pat:  pat,
				Name: &Ident{Sp: p.spanOf(t), Name: p.text(t)},
			}
		} else {
			p.expected("a name after \"as\"")
		}
	}
	return pat
}

// parseOrPat parses ctor-pattern { "|" ctor-pattern }.
func (p *parser) parseOrPat() Pat {
	first := p.parseCtorPat()
	if !p.at(tokBar) {
		return first
	}
	alts := []Pat{first}
	for p.at(tokBar) {
		p.next()
		alts = append(alts, p.parseCtorPat())
	}
	return &OrPat{Sp: p.span(first.Span().Start, p.prevEnd()), Alts: alts}
}

// parseCtorPat parses ctor-name { pat-arg } or a pat-atom.
func (p *parser) parseCtorPat() Pat {
	if !p.atCtorStart() {
		return p.parsePatAtom()
	}
	ctor := p.parseCtorName()
	var args []CtorArg
	for p.atPatArgStart() {
		save := p.pos
		args = append(args, p.parsePatArg())
		if p.pos == save {
			// The argument produced an error node without consuming
			// anything — bail out rather than rebuild error args
			// forever (the "ctor pattern + stray '-'" infinite loop
			// of TODO.md's bug list; every pat-atom path is expected
			// to consume, this guard is defense in depth).
			break
		}
	}
	c := &CtorPat{
		Sp:   p.span(ctor.Span().Start, p.prevEnd()),
		Ctor: ctor,
		Args: args,
	}
	// A single record-pattern argument is lifted to Fields
	// (pat.go: "Let { name = n, .. }" is the Fields form, not a
	// positional argument holding a RecordPat).
	if len(args) == 1 && args[0].Label == nil {
		if rp, ok := args[0].Pat.(*RecordPat); ok {
			c.Fields = rp.Fields
			c.Rest = rp.Rest
			c.Args = nil
			c.Sp.End = rp.Span().End
		}
	}
	return c
}

// atCtorStart reports whether the pattern begins with a
// constructor name: UIdent, or (ident|UIdent) "." UIdent.
func (p *parser) atCtorStart() bool {
	switch p.kind() {
	case tokUIdent:
		return true
	case tokIdent:
		return p.peek(1).kind == tokDot && p.peek(2).kind == tokUIdent
	default:
		return false
	}
}

// parseCtorName parses [(ident|UIdent) {"." UIdent} "."] UIdent —
// a bare `Some`, a one-hop `T.Some` / `pkg.Some`, or a deeper
// `pkg.T.Some`. The name builds a left-leaning Select chain so the
// resolver sees the same shape an expression `pkg.Type.Ctor` makes.
func (p *parser) parseCtorName() Expr {
	t := p.cur()
	if t.kind == tokIdent || t.kind == tokUIdent {
		p.next()
		e := Expr(&Ident{Sp: p.spanOf(t), Name: p.text(t)})
		for p.at(tokDot) && p.peek(1).kind == tokUIdent {
			p.next()
			n := p.next()
			e = &Select{
				Sp:   p.span(t.start, n.end),
				Recv: e,
				Sel:  &Ident{Sp: p.spanOf(n), Name: p.text(n)},
			}
		}
		return e
	}
	p.expected("a constructor name")
	return &Ident{Sp: diag.Point(p.file, t.start), Name: "_"}
}

// atPatArgStart reports whether the current token begins a pattern
// argument — a pat-atom starter or "~". A bare UIdent is NOT a
// pat-atom ("Some Other" is rejected; a nested constructor needs
// parens) — grammar.ebnf pat-atom.
func (p *parser) atPatArgStart() bool {
	switch p.kind() {
	case tokUnder, tokIdent, tokInt, tokFloat, tokChar, tokString,
		tokStrOpen, tokTrue, tokFalse, tokLParen, tokLBrack, tokLBrace,
		tokTilde, tokMinus:
		return true
	default:
		return false
	}
}

// parsePatArg parses pat-atom | "~" ident [ ":" pat-atom ].
func (p *parser) parsePatArg() CtorArg {
	start := p.cur().start
	if p.at(tokTilde) {
		p.next()
		t := p.cur()
		if t.kind != tokIdent {
			p.expected("a label name after \"~\"")
			sub := p.parsePatAtom()
			return CtorArg{Sp: p.span(start, sub.Span().End), Pat: sub}
		}
		p.next()
		label := &Ident{Sp: p.spanOf(t), Name: p.text(t)}
		if p.at(tokColon) {
			p.next()
			sub := p.parsePatAtom()
			return CtorArg{Sp: p.span(start, sub.Span().End), Label: label, Pat: sub}
		}
		// Punned "~name" binds the variable name.
		return CtorArg{
			Sp:    p.span(start, t.end),
			Label: label,
			Pat:   &VarPat{Sp: p.spanOf(t), Name: &Ident{Sp: p.spanOf(t), Name: label.Name}},
		}
	}
	sub := p.parsePatAtom()
	return CtorArg{Sp: sub.Span(), Pat: sub}
}

// parsePatAtom parses the atomic patterns.
func (p *parser) parsePatAtom() Pat {
	t := p.cur()
	switch t.kind {
	case tokUnder:
		p.next()
		return &WildPat{Sp: p.spanOf(t)}
	case tokIdent:
		p.next()
		return &VarPat{Sp: p.spanOf(t), Name: &Ident{Sp: p.spanOf(t), Name: p.text(t)}}
	case tokInt:
		p.next()
		return p.litPat(t, LitInt)
	case tokFloat:
		p.next()
		return p.litPat(t, LitFloat)
	case tokChar:
		p.next()
		return p.litPat(t, LitChar)
	case tokString:
		p.next()
		return p.litPat(t, LitStr)
	case tokTrue, tokFalse:
		p.next()
		return p.litPat(t, LitBool)
	case tokStrOpen:
		p.errf(p.spanOf(t), "an interpolated string cannot be a pattern")
		// Consume the parts for recovery and pretend it was a
		// plain string literal.
		end := t.end
		for !p.atEOF() && p.kind() != tokStrClose {
			p.next()
		}
		if p.at(tokStrClose) {
			end = p.next().end
		}
		sp := p.span(t.start, end)
		return &LitPat{Sp: sp, Lit: &Lit{Sp: sp, Kind: LitStr, Text: p.srcText(sp)}}
	case tokMinus:
		if p.peek(1).kind == tokInt || p.peek(1).kind == tokFloat {
			p.next()
			n := p.next()
			kind := LitInt
			if n.kind == tokFloat {
				kind = LitFloat
			}
			sp := p.span(t.start, n.end)
			// Build Text from the token lexemes, not from the span:
			// whitespace or comments between "-" and the digits
			// ("- 080", "- /* c */ 1") would otherwise reach codegen
			// verbatim — the space defeats the leading-zero
			// normalization and the bytes are not a literal in Go.
			text := "-" + p.text(n)
			if kind == LitInt {
				text = intLitText(text)
			}
			return &LitPat{Sp: sp, Lit: &Lit{Sp: sp, Kind: kind, Text: text}}
		}
		// Consume the stray "-" so pattern-argument loops make
		// progress: "-" itself starts a pat-atom, so leaving it
		// unconsumed spins the caller forever on the same token.
		p.next()
		p.errf(p.spanOf(t), "expected a number after \"-\" in a pattern")
		return p.errPat(p.spanOf(t))
	case tokLParen:
		return p.parsePatParen()
	case tokLBrack:
		return p.parseListPat()
	case tokLBrace:
		return p.parseBracePat()
	default:
		p.errf(p.spanOf(t), "expected a pattern, found %s", p.describe(t))
		return p.errPat(p.spanOf(t))
	}
}

// litPat wraps a literal token in a LitPat.
func (p *parser) litPat(t token, kind LitKind) Pat {
	text := p.text(t)
	if kind == LitInt {
		text = intLitText(text)
	}
	return &LitPat{
		Sp:  p.spanOf(t),
		Lit: &Lit{Sp: p.spanOf(t), Kind: kind, Text: text},
	}
}

// intLitText normalizes an integer literal to the spelling of the
// value it denotes in Gom. dec-int accepts leading zeros — "080" is
// decimal 80 — while Go reads the same spelling as an (invalid)
// octal literal and even "077" would silently change value. A bare
// leading-zero decimal is therefore rewritten to plain decimal
// digits for the codegen-facing Text ("080" → "80", "0_0" → "0").
// Base-prefixed literals (0x/0o/0b) spell the same value in Go and
// pass through; a leading "-" of a negative pattern literal is kept.
// See docs/problems/2026-11-02-leading-zero-decimal-ints.md.
func intLitText(text string) string {
	s := text
	neg := ""
	if s != "" && s[0] == '-' {
		neg, s = "-", s[1:]
	}
	if len(s) < 2 || s[0] != '0' {
		return text
	}
	switch s[1] {
	case 'x', 'X', 'o', 'O', 'b', 'B':
		return text
	}
	digits := strings.TrimLeft(strings.ReplaceAll(s, "_", ""), "0")
	if digits == "" {
		digits = "0"
	}
	return neg + digits
}

// srcText returns the source text of a span.
func (p *parser) srcText(sp diag.Span) string {
	return string(p.src[sp.Start:sp.End])
}

// parsePatParen parses "(" ")" (Unit), "(" pat ")" grouping, and
// tuple patterns.
func (p *parser) parsePatParen() Pat {
	open := p.next() // '('
	if p.at(tokRParen) {
		close := p.next()
		sp := p.span(open.start, close.end)
		return &LitPat{Sp: sp, Lit: &Lit{Sp: sp, Kind: LitUnit, Text: "()"}}
	}
	first := p.parsePattern()
	if !p.at(tokComma) {
		p.expect(tokRParen)
		return first // grouping parens produce no node
	}
	elems := []Pat{first}
	for p.at(tokComma) {
		p.next()
		if p.at(tokRParen) {
			t := p.next()
			if len(elems) == 1 {
				// "(p,)" is not in the grammar for patterns.
				p.errf(p.spanOf(t),
					"a tuple pattern needs at least two elements")
				return first
			}
			return &TuplePat{Sp: p.span(open.start, t.end), Elems: elems}
		}
		elems = append(elems, p.parsePattern())
	}
	end := p.prevEnd()
	if t, ok := p.expect(tokRParen); ok {
		end = t.end
	}
	return &TuplePat{Sp: p.span(open.start, end), Elems: elems}
}

// parseListPat parses the list-pattern forms of grammar.ebnf:
// "[]", "[..rest]", "[p, p, ...]", "[p, ..., ..rest]".
func (p *parser) parseListPat() Pat {
	open := p.next() // '['
	l := &ListPat{Sp: p.span(open.start, p.prevEnd())}
	for {
		if p.at(tokRBrack) {
			t := p.next()
			l.Sp.End = t.end
			return l
		}
		if p.atForeignClose(tokRBrack) {
			p.expected("\"]\"")
			l.Sp.End = p.prevEnd()
			return l
		}
		hadRest := false
		var dd token
		if p.at(tokDotDot) {
			dd = p.next()
			hadRest = true
			t := p.cur()
			switch t.kind {
			case tokIdent:
				p.next()
				l.Rest = &VarPat{Sp: p.spanOf(t), Name: &Ident{Sp: p.spanOf(t), Name: p.text(t)}}
			case tokUnder:
				p.next()
				l.Rest = &WildPat{Sp: p.spanOf(t)}
			default:
				p.expected("a name or \"_\" after \"..\"")
				l.Rest = p.errPat(p.spanOf(dd))
			}
			l.Sp.End = p.prevEnd()
		} else {
			e := p.parsePattern()
			l.Elems = append(l.Elems, e)
			l.Sp.End = e.Span().End
		}
		if p.at(tokComma) {
			if hadRest {
				// The grammar allows ".." only as the last item.
				p.errf(p.spanOf(dd),
					"\"..rest\" must be the last element of a list pattern")
			}
			p.next()
			continue
		}
		if p.atCloseOrEOF() {
			continue
		}
		t := p.cur()
		p.errf(p.spanOf(t),
			"expected \",\" or \"]\" between list pattern elements, found %s",
			p.describe(t))
		p.syncItemSep()
	}
}

// parseBracePat parses "{" [ pat-field { "," pat-field } [ "," ] ] "}".
// Like record literals, fields are strictly comma-separated.
func (p *parser) parseBracePat() Pat {
	open := p.next() // '{'
	r := &RecordPat{Sp: p.span(open.start, p.prevEnd())}
	for {
		if p.at(tokRBrace) {
			t := p.next()
			r.Sp.End = t.end
			return r
		}
		if p.atForeignClose(tokRBrace) {
			p.expected("\"}\"")
			r.Sp.End = p.prevEnd()
			return r
		}
		if p.at(tokDotDot) {
			dd := p.next()
			if r.Rest {
				p.errf(p.spanOf(dd), "duplicate \"..\" in a record pattern")
			}
			r.Rest = true
			r.Sp.End = dd.end
		} else if f := p.parsePatField(); f != nil {
			r.Fields = append(r.Fields, *f)
			r.Sp.End = f.Sp.End
		} else {
			p.skipSep()
			continue
		}
		if p.at(tokComma) {
			p.next()
			continue
		}
		if p.atCloseOrEOF() {
			continue
		}
		t := p.cur()
		p.errf(p.spanOf(t),
			"expected \",\" or \"}\" between record pattern fields, found %s",
			p.describe(t))
		p.syncItemSep()
	}
}

// parsePatField parses ident [ "=" pattern ].
func (p *parser) parsePatField() *FieldPat {
	t := p.cur()
	if t.kind != tokIdent {
		p.errf(p.spanOf(t), "expected a field name, found %s", p.describe(t))
		p.syncItem()
		return nil
	}
	p.next()
	f := &FieldPat{
		Sp:   p.spanOf(t),
		Name: &Ident{Sp: p.spanOf(t), Name: p.text(t)},
	}
	if p.at(tokEq) {
		p.next()
		f.Pat = p.parsePattern()
		f.Sp.End = f.Pat.Span().End
	}
	return f
}

// ------------------------------------------------------------------
// type expressions (grammar.ebnf section 7)

// parseTypeExpr parses arrow-type: right-associative, with optional
// "~" ident ":" labeled parameters.
func (p *parser) parseTypeExpr() TypeExpr {
	start := p.cur().start
	var first TypeParam
	if p.at(tokTilde) {
		p.next()
		t := p.cur()
		if t.kind == tokIdent {
			p.next()
			first.Label = &Ident{Sp: p.spanOf(t), Name: p.text(t)}
		} else {
			p.expected("a label name after \"~\"")
		}
		p.expect(tokColon)
		first.Type = p.parseAppType()
	} else {
		first.Type = p.parseAppType()
	}
	first.Sp = p.span(start, first.Type.Span().End)
	if !p.at(tokArrow) {
		if first.Label != nil {
			p.errf(first.Type.Span(),
				"a labeled parameter type is only meaningful before \"->\"")
		}
		return first.Type
	}
	p.next()
	rest := p.parseTypeExpr()
	params := []TypeParam{first}
	var ret TypeExpr
	if tf, ok := rest.(*TypeFn); ok {
		params = append(params, tf.Params...)
		ret = tf.Ret
	} else {
		ret = rest
	}
	return &TypeFn{
		Sp:     p.span(start, ret.Span().End),
		Params: params,
		Ret:    ret,
	}
}

// parseAppType parses type-atom { type-atom }.
func (p *parser) parseAppType() TypeExpr {
	head := p.parseTypeAtom()
	var args []TypeExpr
	for p.atTypeAtomStart() {
		args = append(args, p.parseTypeAtom())
	}
	if len(args) == 0 {
		return head
	}
	return &TypeApp{Sp: p.span(head.Span().Start, p.prevEnd()), Ctor: head, Args: args}
}

// atTypeAtomStart reports whether the current token begins a
// type-atom.
func (p *parser) atTypeAtomStart() bool {
	switch p.kind() {
	case tokUIdent, tokIdent, tokLParen, tokLBrace:
		return true
	default:
		return false
	}
}

// parseTypeAtom parses the atomic type expressions.
func (p *parser) parseTypeAtom() TypeExpr {
	t := p.cur()
	switch t.kind {
	case tokUIdent:
		p.next()
		name := &Ident{Sp: p.spanOf(t), Name: p.text(t)}
		if p.at(tokDot) && p.peek(1).kind == tokUIdent {
			p.next()
			n := p.next()
			return &TypeName{
				Sp:   p.span(t.start, n.end),
				Qual: name,
				Name: &Ident{Sp: p.spanOf(n), Name: p.text(n)},
			}
		}
		return &TypeName{Sp: p.spanOf(t), Name: name}
	case tokIdent:
		p.next()
		id := &Ident{Sp: p.spanOf(t), Name: p.text(t)}
		if p.at(tokDot) && p.peek(1).kind == tokUIdent {
			p.next()
			n := p.next()
			return &TypeName{
				Sp:   p.span(t.start, n.end),
				Qual: id,
				Name: &Ident{Sp: p.spanOf(n), Name: p.text(n)},
			}
		}
		return &TypeVar{Sp: p.spanOf(t), Name: id}
	case tokLParen:
		return p.parseTypeParen()
	case tokLBrace:
		fields, end := p.parseRecordType()
		return &TypeRecord{Sp: p.span(t.start, end), Fields: fields}
	default:
		p.errf(p.spanOf(t), "expected a type, found %s", p.describe(t))
		return p.errType(p.spanOf(t))
	}
}

// parseTypeParen parses "(" ")" (the Unit type), "(" type ")" and
// tuple types. "(T,)" is not in the grammar — a tuple needs two.
func (p *parser) parseTypeParen() TypeExpr {
	open := p.next() // '('
	if p.at(tokRParen) {
		close := p.next()
		return &TypeName{
			Sp:   p.span(open.start, close.end),
			Name: &Ident{Sp: p.span(open.start, close.end), Name: "Unit"},
		}
	}
	first := p.parseTypeExpr()
	if !p.at(tokComma) {
		p.expect(tokRParen)
		return first // grouping parens produce no node
	}
	elems := []TypeExpr{first}
	for p.at(tokComma) {
		p.next()
		if p.at(tokRParen) {
			t := p.next()
			if len(elems) == 1 {
				p.errf(p.spanOf(t), "a tuple type needs at least two elements")
				return first
			}
			return &TypeTuple{Sp: p.span(open.start, t.end), Elems: elems}
		}
		elems = append(elems, p.parseTypeExpr())
	}
	end := p.prevEnd()
	if t, ok := p.expect(tokRParen); ok {
		end = t.end
	}
	return &TypeTuple{Sp: p.span(open.start, end), Elems: elems}
}

// parseRecordType parses "{" [ record-field { "," record-field }
// [ "," ] ] "}" and returns the fields plus the end offset. Shared
// by type-atom and type-body record types. Fields are strictly
// comma-separated (the grammar admits no separators inside).
func (p *parser) parseRecordType() (fields []Field, end int) {
	p.next() // '{'
	end = p.prevEnd()
	for {
		if p.at(tokRBrace) {
			t := p.next()
			return fields, t.end
		}
		if p.atForeignClose(tokRBrace) {
			p.expected("\"}\"")
			return fields, end
		}
		if f := p.parseRecordTypeField(); f != nil {
			fields = append(fields, *f)
			end = f.Sp.End
		} else {
			p.skipSep()
			continue
		}
		if p.at(tokComma) {
			p.next()
			continue
		}
		if p.atCloseOrEOF() {
			continue
		}
		t := p.cur()
		p.errf(p.spanOf(t),
			"expected \",\" or \"}\" between record type fields, found %s",
			p.describe(t))
		p.syncItemSep()
	}
}

// parseRecordTypeField parses ident ":" type-expr.
func (p *parser) parseRecordTypeField() *Field {
	t := p.cur()
	if t.kind != tokIdent {
		p.errf(p.spanOf(t), "expected a field name, found %s", p.describe(t))
		p.syncItem()
		return nil
	}
	p.next()
	name := &Ident{Sp: p.spanOf(t), Name: p.text(t)}
	p.expect(tokColon)
	typ := p.parseTypeExpr()
	return &Field{
		Sp:   p.span(t.start, typ.Span().End),
		Name: name,
		Type: typ,
	}
}
