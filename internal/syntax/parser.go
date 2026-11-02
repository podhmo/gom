package syntax

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/podhmo/gom/internal/diag"
)

// This file implements the parser: phase 2 of the compiler
// (docs/design.md section 2), a recursive-descent parser over the
// lexed token stream with a precedence-climbing expression chain.
// The grammar it implements is docs/grammar.ebnf verbatim; the
// places where the grammar needed an implementation decision are
// documented inline and summarized in
// docs/decisions/2026-11-02-p1-3-parser-interpretations.md.
//
// Error recovery: parse errors emit E0101 diagnostics and the
// parser continues. Recovery combines (a) "insert-missing" — a
// required token that is absent is reported but not consumed, so
// the parse proceeds as if it were there — and (b) panic-mode
// synchronization that skips to the next separator (NL / ";" /
// "}" at the current nesting) or declaration keyword. Nodes that
// cannot be produced are replaced by placeholders (*Hole for
// expressions, *WildPat for patterns, a synthetic *TypeVar for type
// expressions); the result of a parse with errors is never passed
// to later phases.

// Parse parses src as a .gom source file and returns the surface
// File plus the diagnostics produced. File is always non-nil; an
// error-free parse returns an empty diagnostic list.
func Parse(src []byte, file string) (*File, diag.List) {
	return parse(src, file, false)
}

// ParseInterface parses src as a .gomi interface file: the signature
// subset of the grammar (docs/grammar.ebnf section 1.1). Declarations
// that the interface grammar does not admit (function bodies, test
// blocks, extern blocks, attributes) are still parsed for recovery
// but reported with E0101.
func ParseInterface(src []byte, file string) (*File, diag.List) {
	return parse(src, file, true)
}

func parse(src []byte, file string, gomi bool) (*File, diag.List) {
	lx := lex(src, file)
	p := &parser{
		src:      src,
		file:     file,
		toks:     lx.toks,
		comments: lx.comments,
		meta:     lx.meta,
		blanks:   lx.blanks,
		diags:    lx.diags,
		gomi:     gomi,
		li:       diag.NewLineIndex(src),
	}
	f := p.parseFile()
	return f, p.diags
}

type parser struct {
	src  []byte
	file string
	toks []token
	pos  int

	comments []*Comment
	meta     []commentMeta
	blanks   []diag.Span
	diags    diag.List

	gomi bool
	li   *diag.LineIndex
}

// ------------------------------------------------------------------
// token access

func (p *parser) cur() token          { return p.toks[p.pos] }
func (p *parser) kind() tokenKind     { return p.toks[p.pos].kind }
func (p *parser) at(k tokenKind) bool { return p.kind() == k }
func (p *parser) atEOF() bool         { return p.at(tokEOF) }

// peek returns the token n positions ahead, clamped at the final
// EOF token.
func (p *parser) peek(n int) token {
	i := p.pos + n
	if i >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[i]
}

func (p *parser) next() token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

// text returns the source slice of a token.
func (p *parser) text(t token) string { return string(p.src[t.start:t.end]) }

func (p *parser) spanOf(t token) diag.Span {
	return diag.SpanIn(p.file, t.start, t.end)
}

func (p *parser) span(start, end int) diag.Span {
	return diag.SpanIn(p.file, start, end)
}

// prevEnd is the end offset of the last consumed token.
func (p *parser) prevEnd() int {
	if p.pos == 0 {
		return 0
	}
	return p.toks[p.pos-1].end
}

// ------------------------------------------------------------------
// diagnostics

// errf reports an E0101 at sp. The message is a single line of
// English; structured detail goes through Set.
func (p *parser) errf(sp diag.Span, format string, args ...any) {
	p.diags.Add(diag.Error("E0101", fmt.Sprintf(format, args...)).At(sp))
}

// expected reports "expected X, found Y" at the current token and
// returns false. It does not consume the token (insert-missing):
// the caller continues as if the expected token were present.
func (p *parser) expected(what string) bool {
	t := p.cur()
	p.diags.Add(diag.Error("E0101",
		fmt.Sprintf("expected %s, found %s", what, p.describe(t))).
		At(p.spanOf(t)).
		Set("expected", what).
		Set("found", p.describe(t)))
	return false
}

// describe renders a token for a diagnostic's "found" detail.
func (p *parser) describe(t token) string {
	switch t.kind {
	case tokIdent, tokUIdent, tokInt, tokFloat, tokChar, tokString:
		return t.kind.String() + " '" + quoteLexeme(p.text(t)) + "'"
	default:
		return t.kind.String()
	}
}

// describeLexemeMax bounds the lexeme bytes a "found" detail may
// embed — a string literal can run to megabytes, and the message
// must stay a readable single line.
const describeLexemeMax = 64

// quoteLexeme renders token text inside the '...' of a "found"
// detail: control characters are escaped (the raw bytes of a string
// literal may contain '\r' or '\n', which would violate the
// single-line rule of diag.Diagnostic.Validate) and overlong input
// is truncated before quoting.
func quoteLexeme(s string) string {
	if len(s) > describeLexemeMax {
		cut := describeLexemeMax
		for cut < len(s) && !utf8.RuneStart(s[cut]) {
			cut++
		}
		s = s[:cut] + "..."
	}
	q := strconv.Quote(s)
	return q[1 : len(q)-1] // strip the surrounding quotes; '...' is added by the caller
}

// expect consumes a token of kind k, or reports it missing and
// returns ok=false without consuming.
func (p *parser) expect(k tokenKind) (tok token, ok bool) {
	if p.at(k) {
		return p.next(), true
	}
	p.expected(k.String())
	return token{kind: k, start: p.cur().start, end: p.cur().start}, false
}

// expectMsg is expect with a custom message.
func (p *parser) expectMsg(k tokenKind, format string, args ...any) (tok token, ok bool) {
	if p.at(k) {
		return p.next(), true
	}
	p.errf(p.spanOf(p.cur()), format, args...)
	return token{kind: k, start: p.cur().start, end: p.cur().start}, false
}

// ------------------------------------------------------------------
// synchronization

// skipSep consumes separators (NL and ";") and returns how many
// were seen.
func (p *parser) skipSep() int {
	n := 0
	for p.at(tokNL) || p.at(tokSemi) {
		p.next()
		n++
	}
	return n
}

// sepHere reports whether a separator or the end of the enclosing
// context follows: NL, ";", EOF, or a closing delimiter. A closing
// delimiter also counts — a mismatched close ends the item list the
// same way the matching close does, and the caller reports it.
func (p *parser) sepHere() bool {
	switch p.kind() {
	case tokNL, tokSemi, tokEOF,
		tokRBrace, tokRParen, tokRBrack, tokInterpEnd:
		return true
	default:
		return false
	}
}

// atForeignClose reports whether the current token closes some
// enclosing construct rather than the caller's own list: any close
// other than own, or EOF. Item-list loops break on it so that a
// mismatched close lands in the caller's "expected close"
// diagnostic instead of spinning the list parser forever.
func (p *parser) atForeignClose(own tokenKind) bool {
	k := p.kind()
	if k == own {
		return false
	}
	switch k {
	case tokRParen, tokRBrack, tokRBrace, tokInterpEnd, tokEOF:
		return true
	default:
		return false
	}
}

// atCloseOrEOF reports whether the current token is any closing
// delimiter or EOF — the "end of the enclosing construct" test for
// post-item boundary checks.
func (p *parser) atCloseOrEOF() bool {
	switch p.kind() {
	case tokRParen, tokRBrack, tokRBrace, tokInterpEnd, tokEOF:
		return true
	default:
		return false
	}
}

// syncDecl skips tokens until a separator, a declaration-start
// keyword, or EOF at delimiter depth 0 — the recovery point after
// a broken top-level declaration. A declaration keyword can never
// continue a broken declaration ("let" cannot end an expression,
// so a trailing "let" suppresses the NL that would otherwise stop
// the skip), so stopping there preserves the next declaration.
func (p *parser) syncDecl() { p.syncDepth(true) }

// syncItem skips tokens until a separator, a closing delimiter of
// the enclosing construct, or EOF — the recovery point inside a
// "{ ... }" item list. Opening delimiters are balanced while
// skipping. It does not stop at declaration keywords: item loops
// rely on consuming at least one token per iteration, and the
// keyword may legitimately be their next item ("let" in a block,
// "fn" in an extern block).
//
// The skip STOPS at a separator without consuming it — the caller
// decides whether separators end the item list. A loop whose head
// does not treat them as trivia must therefore follow syncItem
// with skipSep (see syncItemSep), or it re-reports the same token
// forever.
func (p *parser) syncItem() { p.syncDepth(false) }

// syncItemSep is the usual recovery tail of an item-list loop:
// sync to the next item boundary, then eat the separator syncItem
// stopped at. Without the skipSep a stray newline inside "[", "{"
// reached via a recovery desync is re-reported forever (the list
// literal hang of TODO.md's bug list).
func (p *parser) syncItemSep() {
	p.syncItem()
	p.skipSep()
}

// syncArmArrow looks ahead for a "->" at the current nesting depth
// before the next separator, "|" or closing delimiter. When one is
// found the tokens in between are consumed so the "->" becomes the
// current token — the arm's body then parses normally. It reports
// whether an arrow was found. Match and select arms use it after a
// missing-"->" diagnostic so a stray token does not also swallow
// the arm's real "->" and body.
func (p *parser) syncArmArrow() bool {
	depth := 0
	for i := 0; ; i++ {
		k := p.peek(i).kind
		switch {
		case k == tokArrow && depth == 0:
			for ; i > 0; i-- {
				p.next()
			}
			return true
		case k == tokEOF:
			return false
		case depth == 0 && (k == tokNL || k == tokSemi || k == tokBar ||
			k == tokRBrace || k == tokRParen || k == tokRBrack || k == tokInterpEnd):
			return false
		case k == tokLParen || k == tokLBrack || k == tokLBrace || k == tokInterpStart:
			depth++
		case k == tokRParen || k == tokRBrack || k == tokRBrace || k == tokInterpEnd:
			depth--
		}
	}
}

// syncDepth is the shared panic-mode skipper: it stops at the
// first separator or closing delimiter at the current nesting
// depth (or EOF), leaving that token unconsumed for the caller.
// At the top level (stopAtDecl) it also stops before a
// declaration-start keyword.
func (p *parser) syncDepth(stopAtDecl bool) {
	depth := 0
	for !p.atEOF() {
		k := p.kind()
		switch {
		case (k == tokNL || k == tokSemi) && depth == 0:
			return
		case stopAtDecl && depth == 0 && declStart(k):
			return
		case k == tokLBrace || k == tokLParen || k == tokLBrack ||
			k == tokInterpStart:
			depth++
		case k == tokRBrace || k == tokRParen || k == tokRBrack ||
			k == tokInterpEnd:
			if depth == 0 {
				return // belongs to the enclosing construct
			}
			depth--
		}
		p.next()
	}
}

// ------------------------------------------------------------------
// doc comments

// docBefore returns the merged /// doc comment immediately above
// the declaration starting at byte offset off, or nil. A doc group
// is a run of /// comments, each alone on its line, on consecutive
// lines ending directly above the declaration. The merged comment's
// span covers the run and its Text joins the lines with "\n".
func (p *parser) docBefore(off int) *Comment {
	line := p.li.Pos(off).Line
	want := line - 1
	var group []*Comment
	// Walk backwards through the comments preceding off.
	for i := len(p.comments) - 1; i >= 0; i-- {
		c := p.comments[i]
		m := p.meta[i]
		if c.Sp.Start >= off {
			continue // inside or after the declaration
		}
		if c.Kind != CommentDoc || !m.lineOnly || m.startLine != m.endLine {
			break
		}
		if m.endLine != want {
			break
		}
		group = append(group, c)
		want = m.startLine - 1
	}
	if len(group) == 0 {
		return nil
	}
	// Merge into one doc comment, first line first.
	texts := make([]string, 0, len(group))
	for i := len(group) - 1; i >= 0; i-- {
		texts = append(texts, group[i].Text)
	}
	return &Comment{
		Sp:   diag.SpanIn(p.file, group[len(group)-1].Sp.Start, group[0].Sp.End),
		Kind: CommentDoc,
		Text: strings.Join(texts, "\n"),
	}
}

// ------------------------------------------------------------------
// file

func (p *parser) parseFile() *File {
	f := &File{
		Comments: p.comments,
		Blanks:   p.blanks,
	}
	p.skipSep() // leading separators before the package clause
	f.Package = p.parsePackageDecl()

	// { sep , top-decl } , [ sep ] — a separator is required before
	// each declaration once the package clause is present (and before
	// every later declaration).
	needSep := f.Package != nil
	for !p.atEOF() {
		n := p.skipSep()
		if p.atEOF() {
			break
		}
		if needSep && n == 0 {
			t := p.cur()
			p.errf(p.spanOf(t),
				"expected a newline or \";\" between declarations, found %s",
				p.describe(t))
			if !declStart(p.kind()) {
				save := p.pos
				p.syncDecl()
				if p.pos == save {
					// A bare closer halts sync without consuming:
					// drop it so the loop makes progress.
					p.next()
				}
				continue
			}
			// A declaration keyword without a separator is still a
			// declaration start — report and keep it.
		}
		needSep = true
		if d := p.parseTopDecl(); d != nil {
			f.Decls = append(f.Decls, d)
		}
	}
	f.Sp = diag.SpanIn(p.file, 0, len(p.src))
	return f
}

// parsePackageDecl parses "package" ident. A missing clause is one
// diagnostic; parsing continues at the declarations.
func (p *parser) parsePackageDecl() *Ident {
	if !p.at(tokPackage) {
		t := p.cur()
		p.errf(p.spanOf(t), "expected a package clause, found %s", p.describe(t))
		return nil
	}
	p.next()
	t := p.cur()
	switch t.kind {
	case tokIdent, tokUIdent:
		p.next()
		if t.kind == tokUIdent {
			p.errf(p.spanOf(t),
				"a package name must be a lowercase identifier, found %s", p.describe(t))
		}
		return &Ident{Sp: p.spanOf(t), Name: p.text(t)}
	default:
		p.expected("a package name")
		return nil
	}
}

// ------------------------------------------------------------------
// top-level declarations

// declStart reports whether k begins a top-level declaration or an
// attribute.
func declStart(k tokenKind) bool {
	switch k {
	case tokAt, tokFn, tokLet, tokType, tokImport, tokExtern, tokTest:
		return true
	default:
		return false
	}
}

func (p *parser) parseTopDecl() Decl {
	if p.at(tokAt) {
		return p.parseAttrDecl()
	}
	doc := p.docBefore(p.cur().start)
	switch p.kind() {
	case tokFn:
		return p.parseFnDecl(declAttrs{}, doc)
	case tokLet:
		return p.parseLetDeclWithFlag(false, doc)
	case tokType:
		return p.parseTypeDecl(doc)
	case tokImport:
		return p.parseImportDecl()
	case tokExtern:
		return p.parseExternDecl()
	case tokTest:
		return p.parseTestDecl()
	default:
		t := p.cur()
		p.errf(p.spanOf(t), "expected a declaration, found %s", p.describe(t))
		p.next() // drop the offender so the loop makes progress
		p.syncDecl()
		return nil
	}
}

// gomOnly reports a declaration that the .gomi grammar does not
// admit; the declaration is still parsed for recovery.
func (p *parser) gomOnly(sp diag.Span, what string) {
	if p.gomi {
		p.errf(sp, "%s is not allowed in an interface file", what)
	}
}

// parseAttrDecl parses "@" member-name followed by a fn or let
// declaration. The grammar writes [ attribute ] , ( fn-decl |
// let-decl ); separators between the attribute and the declaration
// are accepted because spec's own style puts @test on its own line —
// see docs/problems/2026-11-02-attribute-newline.md.
func (p *parser) parseAttrDecl() Decl {
	at := p.next() // '@'
	attrSp := p.spanOf(at)
	p.gomOnly(attrSp, "an attribute")
	nameTok := p.cur()
	var name string
	switch {
	case nameTok.kind == tokIdent || nameTok.kind == tokUIdent || nameTok.kind.isKeyword():
		p.next()
		name = p.text(nameTok)
	default:
		p.expected("an attribute name after \"@\"")
		p.syncDecl()
		return nil
	}
	p.skipSep()
	if !p.at(tokFn) && !p.at(tokLet) {
		t := p.cur()
		p.errf(diag.SpanIn(p.file, at.start, nameTok.end),
			"an attribute applies only to a \"fn\" or \"let\" declaration, found %s",
			p.describe(t))
		// Recover by parsing the declaration normally; the
		// attribute is simply dropped.
		return p.parseTopDecl()
	}
	attrs := declAttrs{testOnly: name == "test", tailRec: name == "tailrec"}
	if !attrs.testOnly && !attrs.tailRec {
		// The AST cannot carry an unknown attribute (no field
		// records it): report at parse time so the error is not
		// silently lost. E0204 remains the resolve-phase code once
		// the AST gains an attribute slot.
		p.errf(p.spanOf(nameTok),
			"unknown attribute \"@%s\": only \"@test\" and \"@tailrec\" are defined", name)
	}
	doc := p.docBefore(at.start)
	if p.at(tokFn) {
		return p.parseFnDecl(attrs, doc)
	}
	if attrs.tailRec {
		// A let-bound function calls itself through the package
		// variable, which the tail-call conversion cannot see —
		// @tailrec is defined on fn only (spec section 1).
		p.errf(p.spanOf(nameTok),
			"the \"@tailrec\" attribute applies only to a \"fn\" declaration")
	}
	return p.parseLetDeclWithFlag(attrs.testOnly, doc)
}

// declAttrs records the attributes a top-level declaration was
// written with. The grammar admits one attribute per declaration
// (docs/grammar.ebnf: [ attribute ] , ( fn-decl | let-decl )).
type declAttrs struct {
	testOnly bool // `@test` (fn or let)
	tailRec  bool // `@tailrec` (fn only)
}

// parseFnDecl parses a fn declaration or signature:
//
//	"fn" ident fn-param { fn-param } "->" type-expr [ "=" expr ]
//
// The "=" body is required in .gom and forbidden in .gomi.
func (p *parser) parseFnDecl(attrs declAttrs, doc *Comment) Decl {
	start := p.next() // 'fn'
	nameTok := p.cur()
	var name *Ident
	switch nameTok.kind {
	case tokIdent, tokUIdent:
		p.next()
		if nameTok.kind == tokUIdent {
			p.errf(p.spanOf(nameTok),
				"a function name must be a lowercase identifier, found %s",
				p.describe(nameTok))
		}
		name = &Ident{Sp: p.spanOf(nameTok), Name: p.text(nameTok)}
	default:
		p.expected("a function name")
		name = &Ident{Sp: diag.Point(p.file, p.cur().start), Name: "_"}
	}
	params := p.parseFnParams()
	var ret TypeExpr
	if p.at(tokArrow) {
		p.next()
		ret = p.parseTypeExpr()
	} else {
		// grammar.ebnf fn-sig requires "-> type"; the AST tolerates
		// a nil Ret for robustness.
		p.expected("\"->\" followed by the return type")
	}
	d := &FnDecl{
		Sp:       diag.SpanIn(p.file, start.start, p.prevEnd()),
		Doc:      doc,
		TestOnly: attrs.testOnly,
		TailRec:  attrs.tailRec,
		Name:     name,
		Params:   params,
		Ret:      ret,
	}
	if p.at(tokEq) {
		if p.gomi {
			eq := p.cur()
			p.errf(p.spanOf(eq), "a signature in an interface file has no \"= body\"")
		}
		p.next()
		d.Body = p.parseExpr()
		d.Sp.End = d.Body.Span().End
	} else if !p.gomi {
		p.expected("\"=\" followed by the function body")
	}
	return d
}

// parseFnParams parses { fn-param }: at least one "( ... )" group.
func (p *parser) parseFnParams() []Param {
	var params []Param
	for p.at(tokLParen) {
		params = append(params, p.parseFnParam())
	}
	if len(params) == 0 {
		p.errf(p.spanOf(p.cur()),
			"a function declaration needs at least one \"(param: Type)\" group, found %s",
			p.describe(p.cur()))
	}
	return params
}

// parseFnParam parses "(" ")" | "(" [ "~" ] pattern ":" type ")".
// A bare "()" is the Unit parameter.
func (p *parser) parseFnParam() Param {
	open := p.next() // '('
	if p.at(tokRParen) {
		close := p.next()
		sp := p.span(open.start, close.end)
		return Param{
			Sp:  sp,
			Pat: &LitPat{Sp: sp, Lit: &Lit{Sp: sp, Kind: LitUnit, Text: "()"}},
		}
	}
	var tilde token
	labeled := false
	if p.at(tokTilde) {
		tilde = p.next()
		labeled = true
	}
	pat := p.parsePattern()
	var typ TypeExpr
	var end int
	if p.at(tokRParen) {
		// "(x)" without ": Type" — a single targeted error.
		close := p.next()
		end = close.end
		p.errf(p.span(open.start, close.end),
			"a function parameter needs a \": Type\" annotation")
		typ = p.errType(diag.Point(p.file, close.end))
	} else {
		p.expect(tokColon)
		typ = p.parseTypeExpr()
		end = p.prevEnd()
		if t, ok := p.expect(tokRParen); ok {
			end = t.end
		}
	}
	param := Param{Sp: p.span(open.start, end), Pat: pat, Type: typ}
	if labeled {
		if vp, ok := pat.(*VarPat); ok {
			param.Label = vp.Name // shared *Ident: one def site for resolve
		} else {
			p.errf(p.spanOf(tilde),
				"a labeled parameter binds a plain name, found a complex pattern")
		}
	}
	return param
}

// parseLetDeclWithFlag parses a top-level "let" binding or, in
// .gomi, the "let name : T" signature.
func (p *parser) parseLetDeclWithFlag(testOnly bool, doc *Comment) Decl {
	start := p.next() // 'let'
	pat, typ := p.parseLetHead()
	d := &LetDecl{
		Sp:       diag.SpanIn(p.file, start.start, p.prevEnd()),
		Doc:      doc,
		TestOnly: testOnly,
		Pat:      pat,
		Type:     typ,
	}
	if p.gomi {
		// Partial probe — only VarPat/WildPat are legal; use assertions
		// instead of a type switch so the sumtype checker doesn't
		// require enumerating every Pat variant.
		_, isVar := pat.(*VarPat)
		_, isWild := pat.(*WildPat)
		if !isVar && !isWild {
			p.errf(pat.Span(), "a constant signature binds a plain name")
		}
	}
	if p.at(tokEq) {
		if p.gomi {
			eq := p.cur()
			p.errf(p.spanOf(eq), "a let signature in an interface file has no \"= value\"")
		}
		p.next()
		d.Value = p.parseExpr()
		d.Sp.End = d.Value.Span().End
	} else if !p.gomi {
		p.expected("\"=\" followed by the value")
	} else if typ == nil {
		// .gomi let-sig is "let" ident ":" type-expr — the colon is
		// required there.
		p.expected("\":\" followed by the type")
	}
	return d
}

// parseLetHead parses the "pattern [":" type]" left side shared by
// let declarations and block lets. The ":" annotation is not in
// grammar.ebnf's let-decl but the AST (Let.Type / LetDecl.Type) and
// the E0503 example require it — see
// docs/problems/2026-11-02-let-type-annotation.md.
func (p *parser) parseLetHead() (pat Pat, typ TypeExpr) {
	pat = p.parsePattern()
	if p.at(tokColon) {
		p.next()
		typ = p.parseTypeExpr()
	}
	return pat, typ
}

// parseTypeDecl parses "type" UIdent { ident } [ "=" type-body ].
// The "=" may be absent only in .gomi (abstract types); elsewhere it
// is an error but recovered as AbstractBody.
func (p *parser) parseTypeDecl(doc *Comment) Decl {
	start := p.next() // 'type'
	nameTok := p.cur()
	var name *Ident
	if nameTok.kind == tokUIdent {
		p.next()
		name = &Ident{Sp: p.spanOf(nameTok), Name: p.text(nameTok)}
	} else {
		p.expected("a type name (uppercase identifier)")
		name = &Ident{Sp: diag.Point(p.file, p.cur().start), Name: "_"}
	}
	var params []*Ident
	for p.at(tokIdent) {
		t := p.next()
		params = append(params, &Ident{Sp: p.spanOf(t), Name: p.text(t)})
	}
	d := &TypeDecl{
		Sp:     diag.SpanIn(p.file, start.start, p.prevEnd()),
		Doc:    doc,
		Name:   name,
		Params: params,
	}
	if p.at(tokEq) {
		p.next()
		d.Body = p.parseTypeBody()
	} else {
		if !p.gomi {
			p.expected("\"=\" followed by the type body")
		}
		d.Body = &AbstractBody{Sp: diag.Point(p.file, p.prevEnd())}
	}
	d.Sp.End = d.Body.Span().End
	return d
}

// parseTypeBody parses variant-list | record-type.
func (p *parser) parseTypeBody() TypeBody {
	start := p.cur().start
	if p.at(tokLBrace) {
		fields, end := p.parseRecordType()
		return &RecordBody{Sp: p.span(start, end), Fields: fields}
	}
	// variant-list: [ "|" ] variant { "|" variant }
	if p.at(tokBar) {
		p.next()
	}
	var variants []Variant
	for {
		if v := p.parseVariant(); v != nil {
			variants = append(variants, *v)
		}
		if !p.at(tokBar) {
			break
		}
		p.next()
	}
	return &SumBody{Sp: p.span(start, p.prevEnd()), Variants: variants}
}

// parseVariant parses UIdent { variant-field }. On a missing name
// it reports and returns nil without consuming, so the caller's "|"
// loop can still find the next variant.
func (p *parser) parseVariant() *Variant {
	t := p.cur()
	if t.kind != tokUIdent {
		p.expected("a variant name (uppercase identifier)")
		return nil
	}
	p.next()
	v := &Variant{
		Sp:   p.spanOf(t),
		Doc:  p.docBefore(t.start),
		Name: &Ident{Sp: p.spanOf(t), Name: p.text(t)},
	}
	for p.atVariantFieldStart() {
		v.Params = append(v.Params, p.parseVariantField())
	}
	v.Sp.End = p.prevEnd()
	return v
}

// atVariantFieldStart reports whether the next token begins a
// variant field: a "(" group (named/labeled) or a type-atom
// (positional). "|" and separators end the variant.
func (p *parser) atVariantFieldStart() bool {
	switch p.kind() {
	case tokLParen, tokUIdent, tokIdent, tokLBrace:
		return true
	default:
		return false
	}
}

// lookNamedField reports whether the upcoming tokens have the
// named-field shape "(" [ "~" ] ident ":" — distinguishing a
// named/labeled field from a parenthesized type-atom.
func (p *parser) lookNamedField() bool {
	i := 1
	if p.peek(i).kind == tokTilde {
		i++
	}
	return p.peek(i).kind == tokIdent && p.peek(i+1).kind == tokColon
}

// parseVariantField parses "(" [ "~" ] ident ":" type ")" or a
// positional type-atom.
func (p *parser) parseVariantField() Field {
	start := p.cur().start
	if p.at(tokLParen) && p.lookNamedField() {
		open := p.next()
		var label, name *Ident
		if p.at(tokTilde) {
			p.next()
			t := p.next() // ident
			label = &Ident{Sp: p.spanOf(t), Name: p.text(t)}
		} else {
			t := p.next() // ident
			name = &Ident{Sp: p.spanOf(t), Name: p.text(t)}
		}
		p.expect(tokColon)
		typ := p.parseTypeExpr()
		end := p.prevEnd()
		if t, ok := p.expect(tokRParen); ok {
			end = t.end
		}
		return Field{Sp: p.span(open.start, end), Name: name, Label: label, Type: typ}
	}
	typ := p.parseTypeAtom()
	return Field{Sp: p.span(start, p.prevEnd()), Type: typ}
}

// parseImportDecl parses "import" [ "go" ] [ ident ] STRING. The
// bound package name is the explicit alias or synthesized from the
// last path element.
func (p *parser) parseImportDecl() Decl {
	start := p.next() // 'import'
	isGo := false
	if p.at(tokGo) {
		p.next()
		isGo = true
	}
	var alias *Ident
	if p.at(tokIdent) && p.peek(1).kind == tokString {
		t := p.next()
		alias = &Ident{Sp: p.spanOf(t), Name: p.text(t)}
	}
	t := p.cur()
	var path string
	var pathSp diag.Span
	if t.kind == tokString {
		p.next()
		pathSp = p.spanOf(t)
		path = p.stringContents(t)
	} else {
		p.expected("an import path string")
		pathSp = diag.Point(p.file, t.start)
	}
	d := &ImportDecl{
		Sp:     diag.SpanIn(p.file, start.start, p.prevEnd()),
		Go:     isGo,
		Path:   path,
		PathSp: pathSp,
		Alias:  alias != nil,
	}
	if alias != nil {
		d.Name = alias
	} else {
		d.Name = &Ident{Sp: pathSp, Name: lastPathSegment(path)}
	}
	return d
}

// lastPathSegment returns the text after the final "/" of an import
// path — the synthesized package name. It is used verbatim; paths
// that do not end in identifier-shaped segments are resolve's
// problem (docs/problems/2026-11-01-gom-import-syntax.md).
func lastPathSegment(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// parseExternDecl parses
//
//	"extern" "go" STRING "{" [ extern-item { sep extern-item } [ sep ] ] "}"
//
// The "{" is part of the construct — always the item list, never an
// expression.
func (p *parser) parseExternDecl() Decl {
	start := p.next() // 'extern'
	p.gomOnly(p.spanOf(start), "an extern declaration")
	if !p.at(tokGo) {
		p.expected("\"go\" after \"extern\"")
	} else {
		p.next()
	}
	var path string
	var pathSp diag.Span
	if t := p.cur(); t.kind == tokString {
		p.next()
		path = p.stringContents(t)
		pathSp = p.spanOf(t)
	} else {
		p.expected("a package path string after \"extern go\"")
		pathSp = diag.Point(p.file, t.start)
	}
	d := &ExternDecl{
		Sp:     diag.SpanIn(p.file, start.start, p.prevEnd()),
		Path:   path,
		PathSp: pathSp,
	}
	if !p.expectBlockOpen("extern declaration") {
		p.syncDecl()
		return d
	}
	closed := false
	for {
		p.skipSep()
		if p.at(tokRBrace) {
			t := p.next()
			d.Sp.End = t.end
			closed = true
			break
		}
		if p.atForeignClose(tokRBrace) {
			break
		}
		if e := p.parseExternItem(); e != nil {
			d.Entries = append(d.Entries, e)
			d.Sp.End = e.Span().End
		}
		if !p.sepHere() {
			t := p.cur()
			p.errf(p.spanOf(t),
				"expected a newline, \";\" or \"}\" between extern items, found %s",
				p.describe(t))
			p.syncItem()
		}
	}
	if !closed {
		p.expected("\"}\"")
	}
	return d
}

// parseExternItem parses "fn" ( ident | UIdent ) fn-param+
// "->" type-expr inside an extern block.
func (p *parser) parseExternItem() ExternEntry {
	start := p.cur()
	if !p.at(tokFn) {
		p.errf(p.spanOf(start),
			"expected an extern item (a \"fn\" signature), found %s", p.describe(start))
		p.syncItem()
		return nil
	}
	p.next()
	nameTok := p.cur()
	var name *Ident
	if nameTok.kind == tokIdent || nameTok.kind == tokUIdent {
		p.next()
		name = &Ident{Sp: p.spanOf(nameTok), Name: p.text(nameTok)}
	} else {
		p.expected("a function name")
		name = &Ident{Sp: diag.Point(p.file, p.cur().start), Name: "_"}
	}
	params := p.parseFnParams()
	var ret TypeExpr
	if p.at(tokArrow) {
		p.next()
		ret = p.parseTypeExpr()
	} else {
		p.expected("\"->\" followed by the return type")
	}
	return &ExternFn{
		Sp:     p.span(start.start, p.prevEnd()),
		Doc:    p.docBefore(start.start),
		Name:   name,
		Params: params,
		Ret:    ret,
	}
}

// parseTestDecl parses "test" STRING block.
func (p *parser) parseTestDecl() Decl {
	start := p.next() // 'test'
	p.gomOnly(p.spanOf(start), "a test declaration")
	var name string
	var nameSp diag.Span
	if t := p.cur(); t.kind == tokString {
		p.next()
		name = p.stringContents(t)
		nameSp = p.spanOf(t)
	} else {
		p.expected("a test name string")
		nameSp = diag.Point(p.file, t.start)
	}
	body := p.parseRequiredBlock("test declaration")
	return &TestDecl{
		Sp:     diag.SpanIn(p.file, start.start, body.Span().End),
		Name:   name,
		NameSp: nameSp,
		Body:   body,
	}
}

// stringContents returns the raw contents between the quotes of a
// plain string token, uninterpreted (escapes are not decoded).
func (p *parser) stringContents(t token) string {
	s := p.src[t.start:t.end]
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return string(s[1 : len(s)-1])
	}
	return string(s)
}

// ------------------------------------------------------------------
// placeholders for error recovery

// errExpr is the placeholder used where an expression failed to
// parse: a hole so consumers never meet nil.
func (p *parser) errExpr(sp diag.Span) Expr { return &Hole{Sp: sp} }

// errPat is the placeholder pattern for failed pattern parses.
func (p *parser) errPat(sp diag.Span) Pat { return &WildPat{Sp: sp} }

// errType is the placeholder type expression for failed type parses.
func (p *parser) errType(sp diag.Span) TypeExpr {
	return &TypeVar{Sp: sp, Name: &Ident{Sp: sp, Name: "_"}}
}
