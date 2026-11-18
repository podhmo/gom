package lower

import (
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
)

// Advisory warnings for list-accumulation shapes the cons-recursion
// specialization cannot cover (docs/sketch/2026-11-19-wave23-noncons-accum.md,
// option γ). The specialization's silent bail leaves four measured
// O(n²) shapes indistinguishable from correct code; this pass names
// them so careless code surfaces the Array-builder idiom instead.
// Warnings only — nothing here changes codegen.
//
// Detected:
//
//   - cons-tail with call-bound rest: `[h, .., f args]` where the
//     rewrite declined (extra rest read, non-identity instantiation)
//     or f is another file-local function in the same recursion
//     (mutual accumulation).
//   - `List.append`/`List.concat` onto a value produced by a
//     recursive call — chained-append accumulation (concat's
//     list-of-lists argument may carry the result inside a literal).
//   - `List.fold`/`foldl` whose folder appends onto its accumulator
//     parameter — fold-internal concat accumulation. The folder is
//     followed through closures, uncurry adapters and file-local
//     named functions.
func (l *lowerer) lintQuadraticAccum(out *core.File) {
	// File-local function symbols and their decls.
	declOf := map[*resolve.Symbol]*core.FuncDecl{}
	var fns []*core.FuncDecl
	for _, d := range out.Decls {
		if f, ok := d.(*core.FuncDecl); ok && f.Sym != nil &&
			!f.Sym.TestOnly && f.Fn != nil && f.Fn.Body != nil {
			declOf[f.Sym] = f
			fns = append(fns, f)
		}
	}
	if len(fns) == 0 {
		return
	}
	// Call edges between file-local functions.
	edges := map[*resolve.Symbol][]*resolve.Symbol{}
	for _, f := range fns {
		eachCallDirect(f.Fn.Body, func(c *core.CallDirect) {
			if c.Callee != nil {
				if _, ok := declOf[c.Callee]; ok {
					edges[f.Sym] = append(edges[f.Sym], c.Callee)
				}
			}
		})
	}
	for _, f := range fns {
		l.lintAccumFn(f, declOf, edges)
	}
}

func (l *lowerer) lintAccumFn(f *core.FuncDecl, declOf map[*resolve.Symbol]*core.FuncDecl, edges map[*resolve.Symbol][]*resolve.Symbol) {
	if f.Sym == nil || f.Fn == nil || f.Fn.Body == nil {
		return
	}
	// Dedup per (rule, site) — the same rule at a second accumulation
	// site is a second warning, not a repeat.
	warned := map[string]bool{}
	warn := func(key string, sp diag.Span, msg string) {
		k := key + "@" + strconv.Itoa(sp.Start)
		if warned[k] {
			return
		}
		warned[k] = true
		l.diags = append(l.diags, diag.Warning(codeAccum, msg).At(sp))
	}
	name := f.Sym.Name

	eachBody(f.Fn.Body, func(b *core.Body) {
		// bound/boundSp map each let-bound var to its CallDirect value
		// and span; boundX keeps every let value so a concat's
		// list-of-lists literal can be looked through.
		bound := map[*resolve.Symbol]*core.CallDirect{}
		boundSp := map[*resolve.Symbol]diag.Span{}
		boundX := map[*resolve.Symbol]core.Expr{}
		for _, lt := range b.Lets {
			if lt == nil || lt.Name == nil || lt.Name.Sym == nil {
				continue
			}
			boundX[lt.Name.Sym] = lt.X
			boundSp[lt.Name.Sym] = lt.Sp
			if c, ok := lt.X.(*core.CallDirect); ok {
				bound[lt.Name.Sym] = c
			}
		}
		// A call counts as "recursive accumulation" only when the
		// callee is in the same SCC as f — f reaches it and it
		// reaches f back. A linear wrapper that calls a recursive
		// sibling once copies that result once (O(n), no warning).
		isRecCall := func(c *core.CallDirect) bool {
			return c != nil && c.Callee != nil &&
				sameCycle(edges, f.Sym, c.Callee)
		}
		// accumVar reports whether an atom is a var bound to a
		// recursive call — the accumulated list itself.
		accumVar := func(a core.Atom) bool {
			v, ok := a.(*core.Var)
			return ok && v.Sym != nil && isRecCall(bound[v.Sym])
		}
		// accumArg is accumVar plus a look through a let-bound list
		// literal — for `concat([rest, [x]])` where the rec call's
		// result (itself a list-of-lists element) sits inside the
		// literal. Scalars inside a literal are NOT accumulation:
		// `List.append [f (n-1)] [0]` copies one element.
		accumArg := func(a core.Atom) bool {
			if accumVar(a) {
				return true
			}
			v, ok := a.(*core.Var)
			if !ok || v.Sym == nil {
				return false
			}
			if ln, ok := boundX[v.Sym].(*core.ListNew); ok {
				for _, el := range ln.Elems {
					if v2, ok := el.(*core.Var); ok && v2.Sym != nil &&
						isRecCall(bound[v2.Sym]) {
						return true
					}
				}
			}
			return false
		}
		// Rule A: tail `[.., ..rest]` where rest is a bound call
		// that consLeaf declined — declined self-calls and calls to
		// recursion-linked siblings both accumulate quadratically.
		if ln, ok := b.Tail.(*core.ListNew); ok && ln.Rest != nil {
			if rv, ok := ln.Rest.(*core.Var); ok && rv.Sym != nil {
				if c := bound[rv.Sym]; isRecCall(c) {
					warn("cons", boundSp[rv.Sym],
						"list accumulation `[.., .."+name+"/"+c.Callee.Name+" args]` is O(n^2): the cons-loop specialization does not apply here — build with Array.make + Array.set and finish with Array.to_list")
				}
			}
		}
		// Rules B/C run over both let values and the tail.
		checkExpr := func(e core.Expr) {
			cd, ok := e.(*core.CallDirect)
			if !ok || cd.Callee == nil ||
				!strings.HasSuffix(cd.Callee.Package, "/list") {
				return
			}
			switch cd.Callee.Name {
			case "append":
				if len(cd.Args) >= 2 && accumVar(cd.Args[0]) {
					warn("append", spanOf(cd.Args[0], boundSp),
						"List.append onto the result of recursive call `"+nameOf(cd.Args[0], bound)+"` is O(n^2): every call copies the whole accumulated list — prefer Array builder or collect conses and one final List.concat")
				}
			case "concat":
				// concat : List (List a) -> List a — one argument.
				if len(cd.Args) >= 1 && accumArg(cd.Args[0]) {
					warn("concat", spanOf(cd.Args[0], boundSp),
						"List.concat over the result of recursive call `"+nameOf(cd.Args[0], bound)+"` is O(n^2): every call re-flattens the accumulated lists — prefer Array builder or one final List.concat outside the recursion")
				}
			case "fold", "foldl":
				if len(cd.Args) >= 1 {
					if v, ok := cd.Args[0].(*core.Var); ok && v.Sym != nil {
						for _, lt := range b.Lets {
							if lt == nil || lt.Name == nil || lt.Name.Sym != v.Sym {
								continue
							}
							if folderAppendsAcc(lt.X, declOf) {
								warn("fold", lt.Sp,
									"List.fold folder appends onto its accumulator each step — O(n^2) list concat; collect into an Array builder instead")
							}
						}
					}
				}
			}
		}
		for _, lt := range b.Lets {
			if lt != nil {
				checkExpr(lt.X)
			}
		}
		checkExpr(b.Tail)
	})
}

// folderAppendsAcc reports whether a function-valued expression
// (the closure/uncurry chain bound for a fold argument) contains an
// inner Func that calls list.append/concat with an enclosing
// parameter in its first argument — the fold-internal concat shape.
// File-local named folders are followed through FuncRef via declOf.
func folderAppendsAcc(e core.Expr, declOf map[*resolve.Symbol]*core.FuncDecl) bool {
	found := false
	var inspect func(e core.Expr)
	var scanFn func(fn *core.Func)
	inspect = func(e core.Expr) {
		switch x := e.(type) {
		case *core.Uncurry:
			inspect(x.X)
		case *core.Closure:
			scanFn(x.Fn)
		case *core.GoFunc:
			for _, in := range x.In {
				inspect(in)
			}
		case *core.FuncRef:
			// A named folder: `List.fold helper` where helper is a
			// file-local fn.
			if d := declOf[x.Sym]; d != nil {
				scanFn(d.Fn)
			}
		// Everything else is a leaf for this check — calls, literals,
		// atoms and control containers carry no function value worth
		// inspecting.
		case *core.Binary, *core.CallBuiltin, *core.CallDirect,
			*core.CallFunc, *core.CallGo, *core.Conv, *core.CtorApp,
			*core.CtorRef, *core.Equal, *core.ErrorFromGo, *core.EscapeCall,
			*core.EscapeResult, *core.FieldGet, *core.ForRange,
			*core.GoFuncRef, *core.If, *core.ListNew, *core.Lit, *core.Match,
			*core.OptionFromGo, *core.RecordNew, *core.RecordUpdate,
			*core.ResultFromGo, *core.SelectExpr, *core.Spawn, *core.TailCall,
			*core.TupleFromGo, *core.TupleNew, *core.Unary, *core.Var:
		default:
			panic("unreachable")
		}
	}
	scanFn = func(fn *core.Func) {
		if fn == nil {
			return
		}
		// The accumulator is the folder's first parameter —
		// `fold`'s convention `acc -> x -> acc`. Marking every
		// parameter would flag folders that append onto the
		// element `x` while ignoring acc (linear per step).
		params := map[*resolve.Symbol]bool{}
		if len(fn.Params) > 0 && fn.Params[0] != nil && fn.Params[0].Sym != nil {
			params[fn.Params[0].Sym] = true
		}
		eachBody(fn.Body, func(b *core.Body) {
			// Let values, so `concat([acc, [x]])` — arg bound to a
			// list literal — resolves its accumulator element.
			bnd := map[*resolve.Symbol]core.Expr{}
			for _, lt := range b.Lets {
				if lt != nil && lt.Name != nil && lt.Name.Sym != nil {
					bnd[lt.Name.Sym] = lt.X
				}
			}
			// accVar: the accumulator parameter itself (append).
			accVar := func(a core.Atom) bool {
				v, ok := a.(*core.Var)
				return ok && v.Sym != nil && params[v.Sym]
			}
			// accArg: accVar plus a let-bound list literal whose
			// elements hold acc — `concat [acc, [x]]` (concat's
			// list-of-lists elements are lists, so acc inside the
			// literal is the accumulator).
			accArg := func(a core.Atom) bool {
				if accVar(a) {
					return true
				}
				v, ok := a.(*core.Var)
				if !ok || v.Sym == nil {
					return false
				}
				if ln, ok := bnd[v.Sym].(*core.ListNew); ok {
					for _, el := range ln.Elems {
						if v2, ok := el.(*core.Var); ok && v2.Sym != nil &&
							params[v2.Sym] {
							return true
						}
					}
				}
				return false
			}
			eachCallDirect(b, func(c *core.CallDirect) {
				if found || c.Callee == nil ||
					!strings.HasSuffix(c.Callee.Package, "/list") {
					return
				}
				switch c.Callee.Name {
				case "append":
					if len(c.Args) >= 2 && accVar(c.Args[0]) {
						found = true
					}
				case "concat":
					if len(c.Args) >= 1 && accArg(c.Args[0]) {
						found = true
					}
				}
			})
		})
	}
	inspect(e)
	return found
}

const codeAccum = "W0801"

// sameCycle reports whether two file-local syms sit in the same
// cycle of the call graph — each reaches the other. `a == b` covers
// the self-recursive case.
func sameCycle(edges map[*resolve.Symbol][]*resolve.Symbol, a, b *resolve.Symbol) bool {
	return a == b ||
		(reaches(edges, a, b, map[*resolve.Symbol]bool{}) &&
			reaches(edges, b, a, map[*resolve.Symbol]bool{}))
}

// spanOf returns the let-binding span of the var atom's value, for
// warning placement (falls back to a zero span).
func spanOf(a core.Atom, boundSp map[*resolve.Symbol]diag.Span) diag.Span {
	if v, ok := a.(*core.Var); ok && v.Sym != nil {
		if sp, ok := boundSp[v.Sym]; ok {
			return sp
		}
	}
	return diag.Span{}
}

// nameOf returns the callee name of the recursive call bound to the
// var atom (for warning text); falls back to "recursive call".
func nameOf(a core.Atom, bound map[*resolve.Symbol]*core.CallDirect) string {
	if v, ok := a.(*core.Var); ok && v.Sym != nil {
		if c := bound[v.Sym]; c != nil && c.Callee != nil {
			return c.Callee.Name
		}
	}
	return "recursive call"
}

// reaches reports whether `from` can reach `target` through the
// file-local call graph (DFS; `from` itself counts when target ==
// from — a self-loop or a multi-function cycle).
func reaches(edges map[*resolve.Symbol][]*resolve.Symbol, from, target *resolve.Symbol, seen map[*resolve.Symbol]bool) bool {
	for _, next := range edges[from] {
		if next == target {
			return true
		}
		if !seen[next] {
			seen[next] = true
			if reaches(edges, next, target, seen) {
				return true
			}
		}
	}
	return false
}

// eachBody yields every Body inside a function: the top body plus
// bodies nested inside if/match/select/spawn/for/closure/go-func
// expressions.
func eachBody(b *core.Body, f func(*core.Body)) {
	var body func(b *core.Body)
	var expr func(e core.Expr)
	var tree func(t core.DTree)
	var selArm func(a core.SelArm)
	body = func(b *core.Body) {
		if b == nil {
			return
		}
		f(b)
		for _, lt := range b.Lets {
			if lt != nil {
				expr(lt.X)
			}
		}
		expr(b.Tail)
	}
	tree = func(t core.DTree) {
		switch t := t.(type) {
		case *core.DSwitch:
			for i := range t.Cases {
				body(t.Cases[i].Body)
			}
			if t.Default != nil {
				tree(t.Default)
			}
		case *core.DGuard:
			tree(t.Then)
			tree(t.Else)
		case *core.DLeaf:
			body(t.Body)
		case *core.DPanic:
		default:
			panic("unreachable")
		}
	}
	selArm = func(a core.SelArm) {
		switch a := a.(type) {
		case *core.SelRecv:
			body(a.Body)
		case *core.SelSend:
			body(a.Body)
		case *core.SelDefault:
			body(a.Body)
		default:
			panic("unreachable")
		}
	}
	expr = func(e core.Expr) {
		switch e := e.(type) {
		case *core.If:
			body(e.Then)
			body(e.Else)
		case *core.Match:
			tree(e.Tree)
		case *core.SelectExpr:
			for _, a := range e.Arms {
				selArm(a)
			}
		case *core.Spawn:
			body(e.Body)
		case *core.ForRange:
			body(e.Body)
		case *core.Closure:
			body(e.Fn.Body)
		case *core.GoFunc:
			for _, in := range e.In {
				expr(in)
			}
		case *core.Uncurry:
			expr(e.X)
		// Leaf expressions and atom payloads carry no nested bodies —
		// a missed container costs a warning, never correctness.
		case *core.Binary, *core.CallBuiltin, *core.CallDirect,
			*core.CallFunc, *core.CallGo, *core.Conv, *core.CtorApp,
			*core.CtorRef, *core.Equal, *core.ErrorFromGo, *core.EscapeCall,
			*core.EscapeResult, *core.FieldGet, *core.FuncRef, *core.GoFuncRef,
			*core.ListNew, *core.Lit, *core.OptionFromGo, *core.RecordNew,
			*core.RecordUpdate, *core.ResultFromGo, *core.TailCall,
			*core.TupleFromGo, *core.TupleNew, *core.Unary, *core.Var:
		default:
			panic("unreachable")
		}
	}
	body(b)
}

// eachCallDirect yields every CallDirect anywhere inside a body,
// descending through closures and control structures.
func eachCallDirect(b *core.Body, f func(*core.CallDirect)) {
	eachBody(b, func(bd *core.Body) {
		for _, lt := range bd.Lets {
			if c, ok := lt.X.(*core.CallDirect); ok {
				f(c)
			}
		}
		if c, ok := bd.Tail.(*core.CallDirect); ok {
			f(c)
		}
	})
}
