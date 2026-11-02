package format

import (
	"bytes"

	"github.com/podhmo/gom/internal/syntax"
)

// This file renders patterns. Pattern precedence follows
// docs/grammar.ebnf section 4: an or-pattern is top-level only, an
// as-pattern binds looser than constructor application, and a
// constructor's positional arguments are pat-atoms — so a non-atom
// nested there takes parens ("Add (A | B) x", "Pair (Pair a b) c").
//
//	ppOr  — or-pattern level (arm/let/for heads take any pattern)
//	ppAs  — inside an or alternative or left of "as"
//	ppApp — a positional constructor argument: pat-atoms only

const (
	ppOr  = 10
	ppAs  = 20
	ppApp = 30
)

// patPrec returns the precedence class of pat's outermost form.
func patPrec(pat syntax.Pat) int {
	switch pat.(type) {
	case *syntax.OrPat:
		return ppOr
	case *syntax.AsPat:
		return ppAs
	case *syntax.CtorPat:
		// Any constructor pattern — applied or bare — is not a
		// pat-atom (docs/grammar.ebnf: pat-arg = pat-atom), so it
		// needs parens in argument position; everywhere else it binds
		// tighter than "as"/"or".
		return ppAs + 1
	case *syntax.ListPat, *syntax.LitPat, *syntax.RecordPat,
		*syntax.TuplePat, *syntax.VarPat, *syntax.WildPat:
		return ppApp
	default:
		panic("unreachable")
	}
}

// pat renders a pattern for context prec.
func (p *printer) pat(pat syntax.Pat, prec int) *doc {
	d := p.patRaw(pat)
	if patPrec(pat) < prec {
		d = concat(text("("), d, text(")"))
	}
	return p.sweep(pat.Span().Start, pat.Span().End, d)
}

// patRaw renders a pattern without a precedence check.
func (p *printer) patRaw(pat syntax.Pat) *doc {
	switch pat := pat.(type) {
	case *syntax.WildPat:
		return text("_")
	case *syntax.VarPat:
		return text(pat.Name.Name)
	case *syntax.LitPat:
		return text(pat.Lit.Text)

	case *syntax.CtorPat:
		var kids []*doc
		kids = append(kids, p.expr(pat.Ctor, pPost, false))
		prev := pat.Ctor.Span().End
		for i := range pat.Args {
			a := &pat.Args[i]
			mid := p.gapInline(prev, a.Sp.Start)
			kids = append(kids, text(" "+mid), p.ctorArg(a))
			prev = a.Sp.End
		}
		if pat.Fields != nil {
			open := findByte(p.src, prev, firstFieldOff(pat.Fields, pat.Rest, pat.Sp.End), '{')
			kids = append(kids, text(" "), p.fieldPats(open, pat.Fields, pat.Rest, pat.Sp.End))
		}
		return concat(kids...)

	case *syntax.TuplePat:
		items := make([]item, 0, len(pat.Elems))
		for _, el := range pat.Elems {
			items = append(items, item{lo: el.Span().Start, hi: el.Span().End, d: p.pat(el, ppOr)})
		}
		return p.braceSeq(docNil, '(', ')', pat.Sp.Start, pat.Sp.End-1, nil, items,
			seqSpec{sep: ",", flatSep: ", ", trailSep: true})

	case *syntax.RecordPat:
		return p.fieldPats(pat.Sp.Start, pat.Fields, pat.Rest, pat.Sp.End)

	case *syntax.ListPat:
		items := make([]item, 0, len(pat.Elems)+1)
		for _, el := range pat.Elems {
			items = append(items, item{lo: el.Span().Start, hi: el.Span().End, d: p.pat(el, ppOr)})
		}
		if pat.Rest != nil {
			dots := p.dotDotBefore(pat.Rest.Span().Start, pat.Sp.Start)
			items = append(items, item{lo: dots, hi: pat.Rest.Span().End, d: p.restPat(pat.Rest)})
		}
		return p.braceSeq(docNil, '[', ']', pat.Sp.Start, pat.Sp.End-1, nil, items,
			seqSpec{sep: ",", flatSep: ", ", trailSep: true})

	case *syntax.OrPat:
		kids := make([]*doc, 0, 2*len(pat.Alts))
		for i, alt := range pat.Alts {
			if i > 0 {
				mid := p.gapInline(pat.Alts[i-1].Span().End, alt.Span().Start)
				kids = append(kids, docLine, text("| "+mid))
			}
			kids = append(kids, p.pat(alt, ppAs))
		}
		return group(concat(kids...))

	case *syntax.AsPat:
		mid := p.gapInline(pat.Pat.Span().End, pat.Name.Span().Start)
		// "as" cannot lead a line — it stays on the pattern's line.
		return concat(p.pat(pat.Pat, ppAs+1), text(" as"+pad(mid)+pat.Name.Name))

	default:
		panic("unreachable")
	}
}

// ctorArg renders one constructor-pattern argument: a positional
// pat-atom or "~label: pat".
func (p *printer) ctorArg(a *syntax.CtorArg) *doc {
	return p.sweep(a.Sp.Start, a.Sp.End, p.ctorArgRaw(a))
}

func (p *printer) ctorArgRaw(a *syntax.CtorArg) *doc {
	if a.Label != nil {
		mid := p.gapInline(a.Sp.Start, a.Pat.Span().Start)
		// "~label:" takes a pat-atom (grammar.ebnf pat-arg), so
		// non-atoms keep parens — "~y: (N x)", "~y: (a | b)",
		// "~z: (p as r)". Without them a bare ctor name fails to
		// reparse at all, and "|"/"as" bind outside the argument:
		// "B ~y: a | b" is "(B ~y: a) | b", not "B ~y: (a | b)".
		return concat(text("~"+a.Label.Name+":"+pad(mid)), p.pat(a.Pat, ppApp))
	}
	return p.pat(a.Pat, ppApp)
}

// restPat renders a "..rest" pattern tail.
func (p *printer) restPat(rest syntax.Pat) *doc {
	return concat(text(".."), p.pat(rest, ppOr))
}

// fieldPats renders "{ name = pat, name, .. }" — the field list of a
// record pattern or a constructor pattern's named form. open is the
// "{" offset; closeOff is the "}" offset.
func (p *printer) fieldPats(open int, fields []syntax.FieldPat, rest bool, closeOff int) *doc {
	items := make([]item, 0, len(fields)+1)
	for i := range fields {
		f := &fields[i]
		var d *doc
		if f.Punned() {
			d = text(f.Name.Name)
		} else {
			mid := p.gapInline(f.Name.Span().End, f.Pat.Span().Start)
			d = concat(text(f.Name.Name+" ="+pad(mid)), p.pat(f.Pat, ppOr))
		}
		items = append(items, item{lo: f.Sp.Start, hi: f.Sp.End, d: p.sweep(f.Sp.Start, f.Sp.End, d)})
	}
	if rest {
		dots := p.dotDotBefore(closeOff, open)
		items = append(items, item{lo: dots, hi: dots + 2, d: text("..")})
	}
	return p.braceSeq(docNil, '{', '}', open, closeOff, nil, items,
		seqSpec{sep: ",", flatSep: ", ", trailSep: true})
}

// dotDotBefore finds the ".." token ending a pattern list: the last
// ".." between lo and hi.
func (p *printer) dotDotBefore(hi, lo int) int {
	if hi > len(p.src) {
		hi = len(p.src)
	}
	if i := bytes.LastIndex(p.src[lo:hi], []byte("..")); i >= 0 {
		return lo + i
	}
	return hi
}

// firstFieldOff returns where the "{" search for a named-field
// pattern list should stop.
func firstFieldOff(fields []syntax.FieldPat, rest bool, dflt int) int {
	if len(fields) > 0 {
		return fields[0].Sp.Start
	}
	return dflt
}
