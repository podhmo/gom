// Package staticalias classifies the top-level `let` bindings
// lowering emits as named ValueAlias FuncDecl wrappers rather than
// package variables: a bound function-typed variable whose
// initializer element is a pure reference rooted in a local,
// non-thunk function declaration (decisions
// 2026-11-03-static-function-alias-declarations,
// 2026-11-05-aggregate-function-bindings). The classification
// decides the shape a `let` takes in the compiled unit — a function
// node, a hidden variable, or both — so the two consumers that
// model that shape share it: lower emits the declarations it
// selects, and the evaluator's initialization graph gives the same
// `let`s the same node kinds (decision
// 2026-11-06-eval-init-ready-order §5). Everything here operates on
// check.Decl/check.Expr only, so it can sit between the two
// consumers without either depending on the other.
package staticalias

import (
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Index is the static-alias classification of one declaration set.
type Index struct {
	// Aliases marks each let-bound symbol the classification emits
	// as a ValueAlias FuncDecl — a named wrapper applying the
	// binding's element expression instead of a package variable.
	Aliases map[*resolve.Symbol]bool
	// Elems maps each function-typed binding whose element the
	// trace reached to that element — a *check.Var (transparent
	// ascriptions peeled) naming the binding's reference target.
	// Every Aliases member has an entry; bindings whose static
	// root fails keep an entry here without joining Aliases.
	Elems map[*resolve.Symbol]check.Expr
	// Init marks VarPat alias bindings whose initializer still
	// needs an eager hidden variable — the element trace peeled a
	// block to reach the element, so its non-tail statements carry
	// evaluation the wrapper cannot stand in for.
	Init map[*resolve.Symbol]bool
}

// Classify computes the alias Index over decls' top-level `let`s.
// universe supplies the local-function index the transitive root
// test consults — declared functions and thunk marks of the scanned
// declarations plus the rest of their compilation unit: a lowering
// pass hands the file's own decls as decls and the file plus its
// sibling files' decls as universe; a whole-unit consumer (the
// evaluator) hands the unit's every declaration as both.
//
// The scan stays per declaration set like the lowering it mirrors:
// alias chains do not cross out of decls — a `let h = f` whose `f`
// is bound by a *sibling* file's `let` is not an alias here even
// when that sibling classifies `f` as one, because the file's
// lowering never saw the sibling's binding shape.
func Classify(decls, universe []check.Decl) *Index {
	// arity/thunks index the local non-thunk functions an alias
	// chain may root in — presence in arity means a check.FnDecl
	// declared the symbol in this unit (the parameter count is
	// kept for parity with lowering's declared-arity index).
	arity := map[*resolve.Symbol]int{}
	thunks := map[*resolve.Symbol]bool{}
	for _, d := range universe {
		if fd, ok := d.(*check.FnDecl); ok {
			arity[fd.Sym] = len(fd.Params)
			if fd.Thunk {
				thunks[fd.Sym] = true
			}
		}
	}

	// refs follows only pure references: an application or closure
	// factory must still execute eagerly, so an alias candidate
	// counts only when its element is a `Var` — through transparent
	// ascriptions — the emitted wrapper can call directly instead of
	// reading the aggregate at initialization time (the dependency
	// shape Go cannot order). Candidates are every variable a
	// top-level `let` binds — a plain `let g = f` and every
	// variable a destructuring pattern binds (`let (again, _) =
	// (count, 0)`).
	refs := map[*resolve.Symbol]*resolve.Symbol{}
	idx := &Index{
		Aliases: map[*resolve.Symbol]bool{},
		Elems:   map[*resolve.Symbol]check.Expr{},
		Init:    map[*resolve.Symbol]bool{},
	}
	for _, d := range decls {
		ld, ok := d.(*check.LetDecl)
		if !ok {
			continue
		}
		elems := bindElems(ld.Pat, ld.Value)
		for _, v := range boundVars(ld.Pat) {
			if v.sym == nil || !isFunc(v.typ) {
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
				idx.Elems[v.sym] = e
				if _, plain := ld.Pat.(*check.VarPat); plain && initNeedsEval(ld.Value) {
					idx.Init[v.sym] = true
				}
			}
		}
	}
	// static resolves each candidate transitively: a binding is an
	// alias when its reference chain ends at a local, non-thunk
	// function declaration — an alias-only cycle has no static
	// function root.
	visited := map[*resolve.Symbol]bool{}
	var static func(*resolve.Symbol) bool
	static = func(sym *resolve.Symbol) bool {
		if _, local := arity[sym]; local && !thunks[sym] {
			return true
		}
		if visited[sym] {
			return idx.Aliases[sym]
		}
		visited[sym] = true
		if target := refs[sym]; target != nil && static(target) {
			idx.Aliases[sym] = true
		}
		return idx.Aliases[sym]
	}
	for sym := range refs {
		static(sym)
	}
	return idx
}

// isFunc reports whether t is a function type after chasing type
// aliases — the shape the ValueAlias wrapper needs (lowering's
// flatSig nil test).
func isFunc(t types.Type) bool {
	_, ok := types.Chase(t).(*types.Func)
	return ok
}

type boundVar struct {
	sym *resolve.Symbol
	typ types.Type
}

// boundVars collects a pattern's bound variables in traversal order.
// Or-patterns read their first alternative — the checker guarantees
// the same names and types on every branch.
func boundVars(p check.Pat) []boundVar {
	var out []boundVar
	var walk func(p check.Pat)
	walk = func(p check.Pat) {
		switch p := p.(type) {
		case *check.VarPat:
			out = append(out, boundVar{sym: p.Sym, typ: p.Type()})
		case *check.AsPat:
			out = append(out, boundVar{sym: p.Sym, typ: p.Type()})
			walk(p.Pat)
		case *check.TuplePat:
			for _, e := range p.Elems {
				walk(e)
			}
		case *check.RecordPat:
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *check.CtorPat:
			for _, a := range p.Args {
				walk(a.Pat)
			}
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *check.ListPat:
			for _, e := range p.Elems {
				walk(e)
			}
			if p.Rest != nil {
				walk(p.Rest)
			}
		case *check.OrPat:
			if len(p.Alts) > 0 {
				walk(p.Alts[0])
			}
		case *check.WildPat, *check.LitPat:
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return out
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
