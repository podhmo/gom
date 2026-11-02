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
		ci.Callee = sym
		ci.Arity = arity
		if ci.Applied >= arity {
			ci.Kind = CallDirect
		} else {
			ci.Kind = CallPartial
			ci.Unapplied = arity - ci.Applied
		}
		// ArgToParam was normalized against the flattened
		// parameter list, whose slots the declared arity may not
		// cover: an over-applied call cannot carry a mapping at
		// all, and an entry pointing into a parameter of the
		// returned function is an over-application too.
		if ci.ArgToParam != nil {
			keep := ci.Applied <= arity
			for _, p := range ci.ArgToParam {
				if p < 0 || p >= arity {
					keep = false
					break
				}
			}
			if !keep {
				ci.ArgToParam = nil
			}
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
