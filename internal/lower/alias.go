package lower

import (
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
)

// indexStaticAliases follows only pure references rooted in local,
// non-thunk functions. An application or closure factory must still
// execute eagerly, and an alias-only cycle has no static function
// root.
//
// Candidates are every variable a top-level `let` binds — a plain
// `let g = f` and every variable a destructuring pattern binds
// (`let (again, _) = (count, 0)`). bindElems recovers the
// sub-expression of the initializer a binding reads; a candidate
// counts only when that element is a pure reference — a `Var`
// through transparent ascriptions — so the emitted wrapper can call
// the root function directly instead of reading the aggregate at
// initialization time (the dependency shape Go cannot order).
func (l *lowerer) indexStaticAliases(f *check.File) map[*resolve.Symbol]bool {
	refs := map[*resolve.Symbol]*resolve.Symbol{}
	for _, d := range f.Decls {
		ld, ok := d.(*check.LetDecl)
		if !ok {
			continue
		}
		elems := bindElems(ld.Pat, ld.Value)
		for _, v := range boundVars(ld.Pat) {
			if v.sym == nil || flatSig(v.typ) == nil {
				continue
			}
			e, ok := elems[v.sym]
			if !ok {
				continue
			}
			for {
				a, ok := e.(*check.Ascribe)
				if !ok || a.Conv {
					break
				}
				e = a.X
			}
			if r, ok := e.(*check.Var); ok && r.Sym != nil {
				refs[v.sym] = r.Sym
				l.aliasElems[v.sym] = e
				if _, plain := ld.Pat.(*check.VarPat); plain && initNeedsEval(ld.Value) {
					l.aliasInit[v.sym] = true
				}
			}
		}
	}
	aliases := map[*resolve.Symbol]bool{}
	visited := map[*resolve.Symbol]bool{}
	var static func(*resolve.Symbol) bool
	static = func(sym *resolve.Symbol) bool {
		if _, local := l.arity[sym]; local && !l.thunks[sym] {
			return true
		}
		if visited[sym] {
			return aliases[sym]
		}
		visited[sym] = true
		if target := refs[sym]; target != nil && static(target) {
			aliases[sym] = true
		}
		return aliases[sym]
	}
	for sym := range refs {
		static(sym)
	}
	return aliases
}

// bindElems maps each variable a pattern binds to the
// sub-expression of the initializer that supplies its value, when
// that supply is statically knowable: the pattern and the literal
// must compose — tuples by position, records by field, list-prefix
// elements up to the first spread, a single-variant constructor
// pattern against a saturated application of the same constructor.
// `as` binds the whole expression while its inner pattern continues
// on it; a block supplies its tail's value; transparent ascriptions
// pass through. Anything else — a variable, a call, a conditional,
// an update literal, an or-pattern — stops the trace and simply
// records nothing for the bindings below it.
func bindElems(p check.Pat, e check.Expr) map[*resolve.Symbol]check.Expr {
	out := map[*resolve.Symbol]check.Expr{}
	var walk func(p check.Pat, e check.Expr)
	walk = func(p check.Pat, e check.Expr) {
		for {
			if a, ok := e.(*check.Ascribe); ok && !a.Conv {
				e = a.X
				continue
			}
			if bl, ok := e.(*check.Block); ok && len(bl.Stmts) > 0 {
				e = bl.Stmts[len(bl.Stmts)-1]
				continue
			}
			break
		}
		switch p := p.(type) {
		case *check.VarPat:
			if p.Sym != nil {
				out[p.Sym] = e
			}
		case *check.AsPat:
			if p.Sym != nil {
				out[p.Sym] = e
			}
			walk(p.Pat, e)
		case *check.TuplePat:
			if t, ok := e.(*check.TupleLit); ok && len(t.Elems) == len(p.Elems) {
				for i, ep := range p.Elems {
					walk(ep, t.Elems[i])
				}
			}
		case *check.RecordPat:
			if r, ok := e.(*check.RecordLit); ok && r.With == nil {
				for _, fp := range p.Fields {
					for _, fi := range r.Fields {
						if fi.Sym != nil && fi.Sym == fp.Sym {
							walk(fp.Pat, fi.Value)
							break
						}
					}
				}
			}
		// ListPat is effectively unreachable — every list pattern is
		// refutable, so a `let` never carries one (E0303) — but the
		// switch still covers the sealed Pat interface.
		case *check.ListPat:
			if ll, ok := e.(*check.ListLit); ok && len(ll.Elems) >= len(p.Elems) {
				for i, ep := range p.Elems {
					if _, spread := ll.Elems[i].(*check.Spread); spread {
						break
					}
					walk(ep, ll.Elems[i])
				}
			}
		case *check.CtorPat:
			ap, ok := e.(*check.Apply)
			if !ok || ap.Call.Callee != p.Ctor {
				break
			}
			switch {
			case p.Fields != nil:
				for _, fp := range p.Fields {
					for _, a := range ap.Args {
						if a.Label == fp.Name {
							walk(fp.Pat, a.Value)
							break
						}
					}
				}
			case len(ap.Args) == len(p.Args):
				for i, a := range p.Args {
					if a.Label != ap.Args[i].Label {
						break
					}
					walk(a.Pat, ap.Args[i].Value)
				}
			}
		// LitPat and OrPat are likewise unreachable (refutable);
		// they — and WildPat, which binds nothing — simply record
		// no element.
		case *check.WildPat, *check.LitPat, *check.OrPat:
		default:
			panic("unreachable")
		}
	}
	walk(p, e)
	return out
}

// initNeedsEval reports whether a `let g = e` initializer carries
// evaluation the alias wrapper cannot stand in for — anything but
// the pure `Var` (through transparent ascriptions) the wrapper
// calls. For a `VarPat` the only other element shape `bindElems`
// can reach is a peeled block tail, whose non-tail statements still
// have to run eagerly.
func initNeedsEval(e check.Expr) bool {
	for {
		if a, ok := e.(*check.Ascribe); ok && !a.Conv {
			e = a.X
			continue
		}
		_, ok := e.(*check.Var)
		return !ok
	}
}
