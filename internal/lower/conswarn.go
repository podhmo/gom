package lower

import (
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
//   - `List.append` onto a value produced by a recursive call —
//     chained-append accumulation.
//   - `List.fold` whose folder appends onto its accumulator
//     parameter — fold-internal concat accumulation.
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
	// The recursion set: syms that can reach themselves through the
	// file-local call graph — self-loops and multi-function cycles
	// alike.
	rec := map[*resolve.Symbol]bool{}
	for _, f := range fns {
		if reaches(edges, f.Sym, f.Sym, map[*resolve.Symbol]bool{}) {
			rec[f.Sym] = true
		}
	}
	for _, f := range fns {
		l.lintAccumFn(f, declOf, rec)
	}
}

func (l *lowerer) lintAccumFn(f *core.FuncDecl, declOf map[*resolve.Symbol]*core.FuncDecl, rec map[*resolve.Symbol]bool) {
	if f.Sym == nil || f.Fn == nil || f.Fn.Body == nil {
		return
	}
	warned := map[string]bool{}
	warn := func(key string, sp diag.Span, msg string) {
		if warned[key] {
			return
		}
		warned[key] = true
		l.diags = append(l.diags, diag.Warning(codeAccum, msg).At(sp))
	}
	name := f.Sym.Name

	eachBody(f.Fn.Body, func(b *core.Body) {
		// boundCall maps each let-bound var to its CallDirect value
		// (nil for non-call bindings) for this body's lookups.
		bound := map[*resolve.Symbol]*core.CallDirect{}
		boundSp := map[*resolve.Symbol]diag.Span{}
		for _, lt := range b.Lets {
			if lt == nil || lt.Name == nil || lt.Name.Sym == nil {
				continue
			}
			if c, ok := lt.X.(*core.CallDirect); ok {
				bound[lt.Name.Sym] = c
				boundSp[lt.Name.Sym] = lt.Sp
			}
		}
		isRecCall := func(c *core.CallDirect) bool {
			return c != nil && c.Callee != nil &&
				(c.Callee == f.Sym || rec[c.Callee])
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
			case "append", "concat":
				if len(cd.Args) >= 2 {
					if v, ok := cd.Args[0].(*core.Var); ok && v.Sym != nil {
						if isRecCall(bound[v.Sym]) {
							warn("append", boundSp[v.Sym],
								"List.append onto the result of recursive call `"+bound[v.Sym].Callee.Name+"` is O(n^2): every call copies the whole accumulated list — prefer Array builder or collect conses and one final List.concat")
						}
					}
				}
			case "fold", "foldl":
				if len(cd.Args) >= 1 {
					if v, ok := cd.Args[0].(*core.Var); ok && v.Sym != nil {
						for _, lt := range b.Lets {
							if lt == nil || lt.Name == nil || lt.Name.Sym != v.Sym {
								continue
							}
							if folderAppendsAcc(lt.X) {
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
// inner Func that calls list.append with an enclosing parameter as
// the first argument — the fold-internal concat shape.
func folderAppendsAcc(e core.Expr) bool {
	found := false
	var inspect func(e core.Expr)
	var scanFn func(fn *core.Func, outerParams map[*resolve.Symbol]bool)
	inspect = func(e core.Expr) {
		switch x := e.(type) {
		case *core.Uncurry:
			inspect(x.X)
		case *core.Closure:
			scanFn(x.Fn, nil)
		case *core.GoFunc:
			for _, in := range x.In {
				inspect(in)
			}
		// Everything else is a leaf for this check — calls, literals,
		// atoms and control containers carry no function value worth
		// inspecting (fold args are always closures/uncurry/adapters).
		case *core.Binary, *core.CallBuiltin, *core.CallDirect,
			*core.CallFunc, *core.CallGo, *core.Conv, *core.CtorApp,
			*core.CtorRef, *core.Equal, *core.ErrorFromGo, *core.EscapeCall,
			*core.EscapeResult, *core.FieldGet, *core.ForRange, *core.FuncRef,
			*core.GoFuncRef, *core.If, *core.ListNew, *core.Lit, *core.Match,
			*core.OptionFromGo, *core.RecordNew, *core.RecordUpdate,
			*core.ResultFromGo, *core.SelectExpr, *core.Spawn, *core.TailCall,
			*core.TupleFromGo, *core.TupleNew, *core.Unary, *core.Var:
		}
	}
	scanFn = func(fn *core.Func, _ map[*resolve.Symbol]bool) {
		if fn == nil {
			return
		}
		params := map[*resolve.Symbol]bool{}
		for _, p := range fn.Params {
			if p != nil && p.Sym != nil {
				params[p.Sym] = true
			}
		}
		eachCallDirect(fn.Body, func(c *core.CallDirect) {
			if found || c.Callee == nil ||
				!strings.HasSuffix(c.Callee.Package, "/list") ||
				(c.Callee.Name != "append" && c.Callee.Name != "concat") {
				return
			}
			if len(c.Args) >= 2 {
				if v, ok := c.Args[0].(*core.Var); ok && v.Sym != nil && params[v.Sym] {
					found = true
				}
			}
		})
	}
	inspect(e)
	return found
}

const codeAccum = "W0801"

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
