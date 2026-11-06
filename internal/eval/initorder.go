package eval

import (
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
)

// initNode is one top-level `let` of the unit in the initialization
// dependency graph — the check-level analogue of the core.VarDecl
// nodes lower.CheckInitCycles graphs. A `let` emits one hidden
// temporary VarDecl carrying the initializer plus one projection
// VarDecl per bound variable; the projections are pure, so the whole
// declaration is a single effectful initialization step and the graph
// needs a single node for it. deps holds the node's package-variable
// dependencies (vardep): every node that must initialize first.
//
// done tracks "this node's initializer ran" for nodes no symbol can
// reference — a `let _ = e` binds nothing, registers no letInit, and
// is driven by the frontier alone. Bound lets rely on init.done,
// which lazy forcing can also set (forced() covers both).
type initNode struct {
	decl *check.LetDecl
	init *letInit // nil when the pattern binds no symbols
	deps map[*initNode]bool
	done bool
}

// forced reports whether the node's initializer already ran — either
// driven by Run's frontier (done) or lazily through a read of one of
// its bound symbols (init.done).
func (n *initNode) forced() bool {
	return n.done || (n.init != nil && n.init.done)
}

// ready reports whether every dependency has initialized — Go's
// "ready for initialization" precondition of spec 5.1.
func (n *initNode) ready() bool {
	for d := range n.deps {
		if !d.forced() {
			return false
		}
	}
	return true
}

// buildInitGraph computes Go's package-initialization dependency
// graph over the unit's typed declarations — the same analysis
// lower.CheckInitCycles applies to the lowered Core files, recomputed
// on the check.Decl universe the evaluator sees (spec 5.1; decision
// 2026-11-06-eval-init-ready-order). The returned slice lists every
// top-level let in declaration order — lexical file order across
// files, source order within one — each carrying its vardep set.
//
// The two node kinds mirror the two core.Decl kinds the lowering
// emits: a check.LetDecl is a variable node (its bound symbols share
// one initializer, like the hidden temporary's VarDecl they lower
// through), and a check.FnDecl — a `fn`, a flipped `let f = <lambda>`,
// or a generalized value-let thunk — is a function node: never
// initialized itself, but references through its body propagate
// dependencies to its referencers, exactly like lower's FuncDecl set.
// TypeDecls carry no initializer; TestDecl bodies run at test time —
// neither joins the graph.
//
// refset[n] is the set of package-level declarations n's body
// references — Var, MemberExpr and Apply callees whose symbols name
// one of the unit's own decls (constructor, builtin, module-member
// and Go symbols fail the membership test, like the funcs/vars filter
// in lower). Nested lambda, let, go-spawn and match-guard bodies
// count: Go reads the initializer's references lexically, so
// `let x = fn () -> y` still depends on y. check.WalkExpr is total
// over the sealed Expr union, so no reference shape escapes it.
//
// vardep[v] is the set of variable nodes v's initialization must
// wait for — every variable reachable from v over refset paths that
// pass only through functions. A var neighbour is a dependency in
// itself; it is not traversed further.
func buildInitGraph(files []*check.File, topLets map[*resolve.Symbol]*letInit) []*initNode {
	varNode := map[*resolve.Symbol]*initNode{}
	funcs := map[*resolve.Symbol]*check.FnDecl{}
	var nodes []*initNode
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *check.LetDecl:
				n := &initNode{decl: d}
				for _, s := range boundSyms(d.Pat) {
					if s == nil {
						continue
					}
					if n.init == nil {
						n.init = topLets[s]
					}
					varNode[s] = n
				}
				nodes = append(nodes, n)
			case *check.FnDecl:
				if d.Sym != nil {
					funcs[d.Sym] = d
				}
			case *check.TestDecl, *check.TypeDecl:
				// Test bodies run after package init; type
				// declarations carry no initializer.
			default:
				panic("unreachable")
			}
		}
	}
	if len(nodes) == 0 {
		return nil
	}
	// collect records each package-level symbol a body references.
	collect := func(dst map[*resolve.Symbol]bool, body check.Expr) {
		check.WalkExpr(body, func(x check.Expr) {
			// Partial probes stay as if-assertions: check.Expr is a
			// sumtype and the checker requires exhaustive switches.
			var to *resolve.Symbol
			if x, ok := x.(*check.Var); ok {
				to = x.Sym
			}
			if x, ok := x.(*check.MemberExpr); ok {
				to = x.Sym
			}
			if x, ok := x.(*check.Apply); ok {
				to = x.Call.Callee
			}
			// Only references into the unit's own declarations
			// join the dependency set — constructor, builtin,
			// module-member and Go symbols are not graph nodes.
			if to != nil && (varNode[to] != nil || funcs[to] != nil) {
				dst[to] = true
			}
		})
	}
	varRefs := map[*initNode]map[*resolve.Symbol]bool{}
	for _, n := range nodes {
		dst := map[*resolve.Symbol]bool{}
		collect(dst, n.decl.Value)
		varRefs[n] = dst
	}
	fnRefs := map[*resolve.Symbol]map[*resolve.Symbol]bool{}
	for sym, fd := range funcs {
		dst := map[*resolve.Symbol]bool{}
		collect(dst, fd.Body)
		fnRefs[sym] = dst
	}
	for _, n := range nodes {
		deps := map[*initNode]bool{}
		seen := map[*resolve.Symbol]bool{}
		var rec func(set map[*resolve.Symbol]bool)
		rec = func(set map[*resolve.Symbol]bool) {
			for h := range set {
				// The dependency set is unordered input to
				// ready(); the frontier's declaration-order
				// scan, not traversal order, picks each step.
				if vn := varNode[h]; vn != nil {
					deps[vn] = true
				} else if funcs[h] != nil && !seen[h] {
					seen[h] = true
					rec(fnRefs[h])
				}
			}
		}
		rec(varRefs[n])
		n.deps = deps
	}
	return nodes
}
