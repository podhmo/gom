package format

import (
	"github.com/podhmo/gom/internal/syntax"
)

// This file renders type expressions. Precedence follows
// docs/grammar.ebnf section 6:
//
//	tpFn   — the loosest: "p1 -> p2 -> r" (right-associative)
//	tpApp  — type application "Ctor a b" (left-associative)
//	tpAtom — names, variables, tuples, records, parens
//
// The only breakable point is after "->" (a line ending in "->"
// always continues); everything else stays flat or breaks inside a
// bracketed collection.

const (
	tpFn   = 10
	tpApp  = 20
	tpAtom = 30
)

// typePrec returns the precedence class of t's outermost form.
func typePrec(t syntax.TypeExpr) int {
	switch t.(type) {
	case *syntax.TypeFn:
		return tpFn
	case *syntax.TypeApp:
		return tpApp
	case *syntax.TypeName, *syntax.TypeRecord, *syntax.TypeTuple,
		*syntax.TypeVar:
		return tpAtom
	default:
		panic("unreachable")
	}
}

// typeAt renders t for context prec.
func (p *printer) typeAt(t syntax.TypeExpr, prec int) *doc {
	d := p.typeExpr(t)
	if typePrec(t) < prec {
		return concat(text("("), d, text(")"))
	}
	return d
}

// typeExpr renders a type expression at its own level.
func (p *printer) typeExpr(t syntax.TypeExpr) *doc {
	return p.sweep(t.Span().Start, t.Span().End, p.typeExprRaw(t))
}

// typeExprRaw renders t without the leftover-comment sweep.
func (p *printer) typeExprRaw(t syntax.TypeExpr) *doc {
	switch t := t.(type) {
	case *syntax.TypeVar:
		return text(t.Name.Name)
	case *syntax.TypeName:
		if t.Qual != nil {
			return text(t.Qual.Name + "." + t.Name.Name)
		}
		// The unit type can be written "( )" — the parser folds it
		// into TypeName "Unit" spanning the parens; canonical output
		// is "Unit" unless the parens carry comments.
		if t.Name.Name == "Unit" && t.Sp.Start < len(p.src) && p.src[t.Sp.Start] == '(' {
			mid := p.gapInline(t.Sp.Start+1, t.Sp.End-1)
			return text("(" + mid + ")")
		}
		return text(t.Name.Name)

	case *syntax.TypeApp:
		kids := make([]*doc, 0, len(t.Args)+1)
		// app-type is "type-atom { type-atom }" — the ctor is an atom
		// like the args, so "(F A) B" keeps its parens instead of
		// flattening into "F A B".
		kids = append(kids, p.typeAt(t.Ctor, tpAtom))
		prev := t.Ctor.Span().End
		for _, a := range t.Args {
			mid := p.gapInline(prev, a.Span().Start)
			kids = append(kids, text(" "+mid), p.typeAt(a, tpAtom))
			prev = a.Span().End
		}
		return concat(kids...)

	case *syntax.TypeFn:
		var kids []*doc
		for i := range t.Params {
			kids = append(kids, p.typeParam(&t.Params[i]))
			next := t.Ret.Span().Start
			if i+1 < len(t.Params) {
				next = t.Params[i+1].Sp.Start
			}
			mid := p.gapInline(t.Params[i].Sp.End, next)
			kids = append(kids, text(" ->"+mid), nest(indentStep, docLine))
		}
		kids = append(kids, p.typeAt(t.Ret, tpFn))
		return group(concat(kids...))

	case *syntax.TypeTuple:
		items := make([]item, 0, len(t.Elems))
		for _, el := range t.Elems {
			items = append(items, item{lo: el.Span().Start, hi: el.Span().End, d: p.typeExpr(el)})
		}
		return p.braceSeq(docNil, '(', ')', t.Sp.Start, t.Sp.End-1, nil, items,
			seqSpec{sep: ",", flatSep: ", ", trailSep: true})

	case *syntax.TypeRecord:
		items := make([]item, 0, len(t.Fields))
		for i := range t.Fields {
			f := &t.Fields[i]
			mid := p.gapInline(f.Sp.Start, f.Type.Span().Start)
			items = append(items, item{lo: f.Sp.Start, hi: f.Sp.End, d: concat(text(f.Name.Name+":"+pad(mid)), p.typeExpr(f.Type))})
		}
		return p.braceSeq(docNil, '{', '}', t.Sp.Start, t.Sp.End-1, nil, items,
			seqSpec{sep: ",", flatSep: ", ", trailSep: true})

	default:
		panic("unreachable")
	}
}

// typeParam renders one parameter of a function type: "T" or
// "~name: T".
func (p *printer) typeParam(pr *syntax.TypeParam) *doc {
	return p.sweep(pr.Sp.Start, pr.Sp.End, p.typeParamRaw(pr))
}

func (p *printer) typeParamRaw(pr *syntax.TypeParam) *doc {
	if pr.Label != nil {
		mid := p.gapInline(pr.Sp.Start, pr.Type.Span().Start)
		return concat(text("~"+pr.Label.Name+":"+pad(mid)), p.typeAt(pr.Type, tpApp))
	}
	return p.typeAt(pr.Type, tpApp)
}
