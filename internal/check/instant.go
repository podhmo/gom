package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// flowNode is a (declaration, type-parameter index) pair in the
// parameter-flow graph — the position `f.a` the check
// tracks.
type flowNode struct {
	sym *resolve.Symbol
	idx int
}

// flowProbe is a growing edge recorded with its call site and the
// details the diagnostic reports: param is the caller's variable
// that grows, tparam the callee's parameter it instantiates.
type flowProbe struct {
	from, to flowNode
	sp       diag.Span
	callee   *resolve.Symbol
	param    *types.Var
	tparam   *types.Var
	arg      types.Type
}

// checkInstantiationCycles reports call sites whose type arguments
// grow a type parameter inside a recursive instantiation —
// `wdepth inner` inside `fn wdepth (w: W a)` instantiates `wdepth`
// at `W (List a)`. Go rejects the emitted generics as an
// instantiation cycle (`A instantiated as list.List[A]`) because the
// set of required instantiations is unbounded, so the program is
// diagnosed here: an accepted program must compile.
//
// The check builds a parameter-flow graph over the file-local
// generic function declarations (declared `fn`s, generalized
// lambda-`let`s, and generalized value-thunks all carry TypeParams):
// a call `g[T₁,…]` inside generic `f` contributes an edge
// `f.a → g.b` for each rigid variable `a` of `f` occurring in the T
// for `g`'s parameter `b` — a renaming edge when T is exactly `a`, a
// growing edge when `a` sits under a type constructor. A growing
// edge whose target reaches its source back lies on a cycle and is
// reported at the call site. Constructor instantiations contribute
// no edges: `W (List a)` inside a generic declaration is legal Go —
// only function instantiation cycles are rejected.
//
// See docs/decisions/2026-11-03-generic-instantiation-cycles.md.
func (c *checker) checkInstantiationCycles() {
	funcs := map[*resolve.Symbol]*FnDecl{}
	var order []*FnDecl
	for _, d := range c.decls {
		fd, ok := d.(*FnDecl)
		if !ok || fd.Sym == nil || len(fd.TypeParams) == 0 {
			continue
		}
		funcs[fd.Sym] = fd
		order = append(order, fd)
	}
	if len(order) == 0 {
		return
	}

	adj := map[flowNode][]flowNode{}
	var sites [][]flowProbe
	for _, fd := range order {
		fvars := map[*types.Var]int{}
		for i, v := range fd.TypeParams {
			fvars[v] = i
		}
		heads := map[Expr]bool{}
		WalkDecl(fd, func(e Expr) {
			var callee *resolve.Symbol
			var targs []types.Type
			var sp diag.Span
			switch e := e.(type) {
			case *Apply:
				// The call records the instantiation; the
				// Var/MemberExpr head carries the same TypeArgs
				// and is skipped below.
				heads[e.Fn] = true
				if e.Call.Kind == CallUnknown || e.Call.Kind == CallGo {
					return
				}
				callee, targs, sp = e.Call.Callee, e.Call.TypeArgs, e.Sp
			case *Var:
				if heads[e] {
					return
				}
				callee, targs, sp = e.Sym, e.TypeArgs, e.Sp
			case *MemberExpr:
				// File-local generics are always Var references
				// — a member selection only reaches foreign
				// symbols — so funcs[e.Sym] misses today; the
				// case stays for robustness.
				if heads[e] {
					return
				}
				callee, targs, sp = e.Sym, e.TypeArgs, e.Sp
			case *Ascribe, *Binary, *Block, *For, *GoExpr, *Hole, *If,
				*Ignore, *Lambda, *Let, *ListLit, *Lit, *Match,
				*RecordLit, *SelectExpr, *Spread, *StrInterp, *Try,
				*TupleLit, *Unary:
				return
			default:
				panic("unreachable")
			}
			g, ok := funcs[callee]
			if !ok {
				return
			}
			var grew []flowProbe
			for j, ta := range targs {
				if j >= len(g.TypeParams) {
					break
				}
				t := types.Chase(ta)
				_, renaming := t.(*types.Var)
				varsIn(t, func(v *types.Var) {
					ci, ok := fvars[v]
					if !ok {
						return
					}
					from := flowNode{fd.Sym, ci}
					to := flowNode{callee, j}
					adj[from] = append(adj[from], to)
					if !renaming {
						grew = append(grew, flowProbe{
							from: from, to: to, sp: sp,
							callee: callee, param: v,
							tparam: g.TypeParams[j], arg: t})
					}
				})
			}
			if len(grew) > 0 {
				sites = append(sites, grew)
			}
		})
	}

	for _, site := range sites {
		var p *flowProbe
		for i := range site {
			if reachesFlow(adj, site[i].to, site[i].from) {
				p = &site[i]
				break
			}
		}
		if p == nil {
			continue
		}
		c.diags.Add(diag.Error("E0416",
			fmt.Sprintf("recursive call to `%s` instantiates `%s`'s `%s` at the growing type `%s` — the instantiation does not converge",
				p.callee.Name, p.callee.Name, p.tparam, p.arg)).
			At(p.sp).
			Set("fn", p.from.sym.Name).
			Set("callee", p.callee.Name).
			Set("param", p.tparam.String()).
			Set("grows", p.param.String()).
			Set("arg", p.arg.String()))
	}
}

// reachesFlow reports whether `from` can reach `want` along the
// parameter-flow edges — the path back that closes the
// instantiation cycle a growing edge sits on.
func reachesFlow(adj map[flowNode][]flowNode, from, want flowNode) bool {
	seen := map[flowNode]bool{}
	stack := []flowNode{from}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == want {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		stack = append(stack, adj[n]...)
	}
	return false
}

// varsIn reports every *types.Var occurring in t — like rigidVars,
// but flowing each occurrence to fn so the graph records which
// caller parameter a call's type argument mentions.
func varsIn(t types.Type, fn func(*types.Var)) {
	switch t := types.Chase(t).(type) {
	case *types.Var:
		fn(t)
	case *types.Meta:
	case *types.Con:
		for _, a := range t.Args {
			varsIn(a, fn)
		}
	case *types.Func:
		for _, p := range t.Params {
			varsIn(p.Type, fn)
		}
		varsIn(t.Ret, fn)
	case *types.Tuple:
		for _, e := range t.Elems {
			varsIn(e, fn)
		}
	case *types.Go:
		for _, a := range t.Args {
			varsIn(a, fn)
		}
	default:
		panic("unreachable")
	}
}
