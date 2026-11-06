package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/resolve"
)

// reclassifyCalls repairs the applications recorded while sym was
// still a variable. checkLetDecl flips a *referenced* lambda-let to
// KindFunc when the signature stays monomorphic; its call sites
// checked earlier — the recursive calls inside its own right-hand
// side, and a mutually-recursive partner's calls in an
// already-emitted declaration — carry CallUnknown, an application
// of a function *value*. Lowering a CallUnknown whose callee
// expression produces a FuncRef is a contract violation, so every
// Apply naming sym is re-classified against the declared arity the
// way checkGomApply would have classified it had the kind already
// flipped: saturated or over-applied becomes CallDirect,
// under-applied CallPartial.
//
// Bare Var and MemberExpr references need no rewrite: lowering
// dispatches on the symbol's kind, which reads KindFunc now, and a
// FuncRef in value position is the right emission. rhs is the
// binding's own value — checked but not yet in c.decls.
func (c *checker) reclassifyCalls(sym *resolve.Symbol, arity int, rhs Expr) {
	fix := func(e Expr) {
		a, ok := e.(*Apply)
		if !ok || a.Call.Kind != CallUnknown || calleeSymOf(a.Fn) != sym {
			return
		}
		ci := &a.Call
		// CallUnknown recorded the whole flattened parameter list
		// as its arity — ArgToParam's domain. Capture it before
		// Arity narrows to the declared parameter list: a `~b:`
		// naming a returned-function level's parameter keeps its
		// mapping, and the call re-classifies against the flat
		// prefix the way checkGomApply would have classified it
		// had the kind already flipped (docs/decisions/
		// 2026-11-06-label-args-into-returned-functions.md).
		flat := ci.Flat()
		if flat < arity {
			flat = arity
		}
		ci.Callee = sym
		ci.Arity = arity
		ci.FlatArity = flat
		if ci.ArgToParam != nil {
			for _, p := range ci.ArgToParam {
				if p < 0 || p >= flat {
					// A mapping entry outside the flat list is a
					// broken invariant — drop the whole mapping
					// and classify positionally rather than panic
					// in Validate.
					ci.ArgToParam = nil
					break
				}
			}
		}
		if ci.ArgToParam != nil {
			ci.Kind, ci.Unapplied = ClassMappedCall(ci.ArgToParam, flat, arity)
		} else if ci.Applied >= arity {
			ci.Kind = CallDirect
		} else {
			ci.Kind = CallPartial
			ci.Unapplied = flat - ci.Applied
		}
		if err := a.Validate(); err != nil {
			panic(fmt.Sprintf("check: reclassified CallInfo for %s: %v", sym.Name, err))
		}
	}
	WalkExpr(rhs, fix)
	for _, d := range c.decls {
		WalkDecl(d, fix)
	}
}

// calleeSymOf returns the symbol an application callee names —
// looking through a non-conversion ascription, which lowering drops
// — when the callee's lowering produces a statically known
// reference (FuncRef) rather than a function-typed value.
func calleeSymOf(e Expr) *resolve.Symbol {
	for {
		switch x := e.(type) {
		case *Var:
			return x.Sym
		case *MemberExpr:
			return x.Sym
		case *Ascribe:
			if x.Conv {
				return nil
			}
			e = x.X
		case *Lit, *Apply, *Hole, *Unary, *Binary, *StrInterp,
			*Lambda, *If, *Match, *Block, *Let, *Ignore, *Try,
			*GoExpr, *For, *SelectExpr, *ListLit, *Spread,
			*TupleLit, *RecordLit:
			return nil
		default:
			panic("unreachable")
		}
	}
}
