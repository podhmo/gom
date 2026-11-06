package lsp

import (
	"fmt"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// hoverTarget is one position's hover answer: the source range the
// answer covers and its markdown payload.
type hoverTarget struct {
	sp   diag.Span
	text string
}

// hoverSearch accumulates hover candidates covering one byte offset
// and keeps the smallest. Candidates are collected in one pass over
// the typed file — expression nodes, pattern binders, declaration
// heads and import names — with more deeply nested (smaller-span)
// candidates always winning.
type hoverSearch struct {
	off   int
	best  *hoverTarget
	namer func(*types.Meta) string
}

// findHover locates the most specific hover target covering byte
// offset off in the typed file, or nil when nothing covers it.
func findHover(tf *check.File, off int) *hoverTarget {
	h := &hoverSearch{off: off, namer: metaNamer()}
	h.pick(tf.PkgSp, "package "+tf.Package)
	for _, imp := range tf.Imports {
		h.import_(imp)
	}
	for _, d := range tf.Decls {
		h.decl(d)
	}
	return h.best
}

// pick records sp+text when sp covers the search offset and is no
// larger than the best candidate so far; equal sizes prefer the
// later (more deeply collected) candidate.
func (h *hoverSearch) pick(sp diag.Span, text string) {
	if !sp.Valid() || !sp.Contains(h.off) {
		return
	}
	if h.best == nil || sp.Len() <= h.best.sp.Len() {
		h.best = &hoverTarget{sp: sp, text: text}
	}
}

// display renders a type the way diagnostics do: surface syntax with
// unbound metas renumbered ?1, ?2, ... by the shared namer so one
// answer keeps one spelling per meta.
func (h *hoverSearch) display(t types.Type) string {
	return types.Display(t, h.namer)
}

// decl collects the candidates of one top-level declaration: its
// head (fn/type name), its binders and its body expressions.
func (h *hoverSearch) decl(d check.Decl) {
	if !d.Span().Contains(h.off) {
		return
	}
	switch d := d.(type) {
	case *check.FnDecl:
		// Sym can be nil on a resolver gap — the declaration still
		// type-checked (the checker is total over gaps), so guard
		// rather than assume.
		if d.Sig != nil && d.Sym != nil {
			h.pick(symNameSpan(d.Sym), d.Sym.Name+" : "+d.Sig.String())
		}
		for _, p := range d.Params {
			h.pat(p.Pat)
		}
		h.expr(d.Body)
	case *check.LetDecl:
		h.pat(d.Pat)
		h.expr(d.Value)
	case *check.TypeDecl:
		if d.Sym != nil {
			h.pick(symNameSpan(d.Sym), "type "+typeDeclHead(d))
		}
		switch body := d.Body.(type) {
		case *check.SumType:
			for _, v := range body.Variants {
				if v.Sym != nil {
					h.pick(symNameSpan(v.Sym),
						v.Sym.Name+" — variant of "+symName(d.Sym))
				}
				for _, f := range v.Fields {
					h.tfield(f)
				}
			}
		case *check.RecordType:
			for _, f := range body.Fields {
				h.tfield(f)
			}
		case *check.AliasType, *check.AbstractType:
			// Type expressions carry no value-level hover yet.
		default:
			panic("unreachable")
		}
	case *check.TestDecl:
		h.expr(d.Body)
	default:
		panic("unreachable")
	}
}

// typeDeclHead renders `type`'s name with its declared parameters —
// `Option a`, `Result a e`.
func typeDeclHead(d *check.TypeDecl) string {
	s := symName(d.Sym)
	for _, p := range d.TypeParams {
		s += " " + p.Name
	}
	return s
}

// tfield collects a record field / payload-slot name hover.
func (h *hoverSearch) tfield(f check.TField) {
	if f.Sym == nil {
		return
	}
	name := f.Name
	if name == "" {
		name = "~" + f.Label
	}
	h.pick(fieldNameSpan(f), name+" : "+h.display(f.Type))
}

// import_ collects the bound module name's hover — `module "fmt"`.
func (h *hoverSearch) import_(imp check.Import) {
	if imp.Sym == nil || imp.Sym.Def == nil {
		return
	}
	d, ok := imp.Sym.Def.(*syntax.ImportDecl)
	if !ok || d.Name == nil {
		return
	}
	h.pick(d.Name.Span(), fmt.Sprintf("module %q", imp.Path))
}

// expr descends one typed expression collecting candidates: every
// node's own type, the selector of a member expression, and the
// patterns hanging off let/match/for/lambda/select arms.
func (h *hoverSearch) expr(e check.Expr) {
	if e == nil || !e.Span().Contains(h.off) {
		return
	}
	// A Var's Sym is nil when the resolver left the reference unbound
	// (check stores useSym(e), which is nil for unbound names) —
	// degrade to the bare type like any other expression.
	if v, ok := e.(*check.Var); ok && v.Sym != nil {
		h.pick(e.Span(), v.Sym.Name+" : "+h.display(e.Type()))
	} else {
		h.pick(e.Span(), h.display(e.Type()))
	}
	switch e := e.(type) {
	case *check.Lit, *check.Var, *check.Hole:
	case *check.Apply:
		h.expr(e.Fn)
		for _, a := range e.Args {
			h.expr(a.Value)
		}
	case *check.MemberExpr:
		if e.Sel != nil {
			h.pick(e.Sel.Span(), e.Sel.Name+" : "+h.display(e.Type()))
		}
		h.expr(e.Recv)
	case *check.Unary:
		h.expr(e.X)
	case *check.Binary:
		h.expr(e.X)
		h.expr(e.Y)
	case *check.StrInterp:
		for _, p := range e.Parts {
			switch p := p.(type) {
			case *check.StrExpr:
				h.expr(p.X)
			case *check.StrText:
			default:
				panic("unreachable")
			}
		}
	case *check.Lambda:
		for _, p := range e.Params {
			h.pat(p.Pat)
		}
		h.expr(e.Body)
	case *check.If:
		h.expr(e.Cond)
		h.expr(e.Then)
		h.expr(e.Else)
	case *check.Match:
		h.expr(e.Subj)
		for _, a := range e.Arms {
			h.pat(a.Pat)
			h.expr(a.Guard)
			h.expr(a.Body)
		}
	case *check.Block:
		for _, st := range e.Stmts {
			h.expr(st)
		}
	case *check.Let:
		h.pat(e.Pat)
		h.expr(e.Value)
	case *check.Ignore:
		h.expr(e.X)
	case *check.Try:
		h.expr(e.X)
	case *check.GoExpr:
		h.expr(e.Body)
	case *check.For:
		h.pat(e.Pat)
		h.expr(e.Iter)
		h.expr(e.Body)
	case *check.SelectExpr:
		for _, a := range e.Arms {
			switch a := a.(type) {
			case *check.RecvArm:
				h.pat(a.Bind)
				h.expr(a.Chan)
				h.expr(a.Body)
			case *check.SendArm:
				h.expr(a.Chan)
				h.expr(a.Value)
				h.expr(a.Body)
			case *check.DefaultArm:
				h.expr(a.Body)
			default:
				panic("unreachable")
			}
		}
	case *check.ListLit:
		for _, el := range e.Elems {
			h.expr(el)
		}
	case *check.TupleLit:
		for _, el := range e.Elems {
			h.expr(el)
		}
	case *check.Spread:
		h.expr(e.X)
	case *check.RecordLit:
		h.expr(e.With)
		for _, f := range e.Fields {
			h.pick(f.Sp, f.Name+" : "+h.display(f.Value.Type()))
			h.expr(f.Value)
		}
	case *check.Ascribe:
		h.expr(e.X)
	default:
		panic("unreachable")
	}
}

// pat descends one typed pattern collecting candidates: bound
// variables (`x : T`), constructor names (`Some — variant of
// Option`), field names and the pattern's own type for compound
// shapes.
func (h *hoverSearch) pat(p check.Pat) {
	if p == nil || !p.Span().Contains(h.off) {
		return
	}
	switch p := p.(type) {
	case *check.VarPat:
		if p.Sym != nil {
			h.pick(p.Span(), p.Sym.Name+" : "+h.display(p.Type()))
		} else {
			h.pick(p.Span(), h.display(p.Type()))
		}
	case *check.WildPat, *check.LitPat:
		h.pick(p.Span(), h.display(p.Type()))
	case *check.CtorPat:
		if p.Name != nil {
			name := "_"
			if p.Ctor != nil {
				name = p.Ctor.Name
			}
			h.pick(p.Name.Span(),
				name+" — variant of "+h.display(p.Type()))
		}
		for _, a := range p.Args {
			h.pat(a.Pat)
		}
		for _, f := range p.Fields {
			h.fieldPat(f)
		}
	case *check.TuplePat:
		h.pick(p.Span(), h.display(p.Type()))
		for _, el := range p.Elems {
			h.pat(el)
		}
	case *check.RecordPat:
		h.pick(p.Span(), h.display(p.Type()))
		for _, f := range p.Fields {
			h.fieldPat(f)
		}
	case *check.ListPat:
		h.pick(p.Span(), h.display(p.Type()))
		for _, el := range p.Elems {
			h.pat(el)
		}
		h.pat(p.Rest)
	case *check.OrPat:
		h.pick(p.Span(), h.display(p.Type()))
		for _, a := range p.Alts {
			h.pat(a)
		}
	case *check.AsPat:
		if p.Sym != nil {
			h.pick(p.Sym.DefSpan(), p.Sym.Name+" : "+h.display(p.Type()))
		}
		h.pat(p.Pat)
	default:
		panic("unreachable")
	}
}

// fieldPat collects a `{ name = pat }` entry's hover. The written
// name's own span is not recorded on FieldPat — the whole
// `name = pat` span stands in; nested patterns still outrank it.
func (h *hoverSearch) fieldPat(f check.FieldPat) {
	if f.Sym != nil && f.Pat != nil {
		h.pick(f.Sp, f.Name+" : "+h.display(f.Pat.Type()))
	}
	h.pat(f.Pat)
}

// symName returns sym's name, tolerating a nil symbol — a resolver
// gap can leave a check node symbol-less; hover degrades to an empty
// name instead of panicking.
func symName(sym *resolve.Symbol) string {
	if sym == nil {
		return ""
	}
	return sym.Name
}

// symNameSpan returns the span of a declaration symbol's written
// name. Symbol.Def is the whole declaration node for fn/type/ctor
// heads, so the name is fished back out of the surface node; a
// missing name or an unexpected Def shape falls back to the def's
// own span.
func symNameSpan(sym *resolve.Symbol) diag.Span {
	if sym == nil || sym.Def == nil {
		return diag.Span{}
	}
	switch n := sym.Def.(type) {
	case *syntax.FnDecl:
		if n.Name != nil {
			return n.Name.Span()
		}
	case *syntax.ExternFn:
		if n.Name != nil {
			return n.Name.Span()
		}
	case *syntax.TypeDecl:
		if n.Name != nil {
			return n.Name.Span()
		}
	case *syntax.Variant:
		if n.Name != nil {
			return n.Name.Span()
		}
	case *syntax.Ident:
		return n.Span()
	}
	return sym.Def.Span()
}

// fieldNameSpan returns the written name half of a declaration-side
// TField — the `name` or `~label` ident, falling back to the field's
// whole span.
func fieldNameSpan(f check.TField) diag.Span {
	if f.Sym == nil || f.Sym.Def == nil {
		return f.Sp
	}
	if fld, ok := f.Sym.Def.(*syntax.Field); ok && fld != nil {
		if fld.Label != nil {
			return fld.Label.Span()
		}
		if fld.Name != nil {
			return fld.Name.Span()
		}
	}
	return f.Sp
}

// metaNamer returns a types.Display namer spelling each unbound meta
// `?1`, `?2`, ... in first-occurrence order — the same convention
// check's diagnostics use, kept package-local so hover answers share
// the dialect.
func metaNamer() func(*types.Meta) string {
	names := map[*types.Meta]string{}
	return func(m *types.Meta) string {
		s, ok := names[m]
		if !ok {
			s = fmt.Sprintf("?%d", len(names)+1)
			names[m] = s
		}
		return s
	}
}
