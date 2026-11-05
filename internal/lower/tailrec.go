package lower

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
)

// This file implements the `@tailrec` requirement check
// (docs/decisions/2026-11-02-tailrec-requirement.md). The attribute
// turns the unconditional self-tail-call conversion of
// markTailCalls into a proof obligation: after every `fn` body is
// lowered and marked, a declaration written `@tailrec` must satisfy
//
//   - R1: it contains at least one self-recursive call in tail
//     position — a TailCall, the call markTailCalls converts. A
//     function whose only self-calls are residual (E0902 reports
//     them instead) or spawned — `go { ... }` bodies are exempt
//     from the position rule but are not convertible, so a
//     go-only recursion gets no loop — does not satisfy the
//     requirement;
//   - R2: every self-recursive call outside a `go { ... }` body is a
//     TailCall — a residual CallDirect means the call is not in tail
//     position (calls inside Spawn are exempt: a spawned call runs
//     on a fresh stack and cannot grow the function's own
//     activation, the same special status spec section 10 gives go);
//   - R3: the function is never used as a value inside its own body
//     — a FuncRef would let aliases hide self-calls from
//     CallDirect-based detection. A declared arity above 1 reaches
//     value position as the adapter's flagged CallDirect (a
//     *value's* firing call, not a call site — CallDirect.Adapter),
//     so it reports here rather than under R1/R2. Unlike R2 there
//     is no Spawn exemption: a call inside `go` provably runs on
//     the new goroutine's stack, but a function *value* is not
//     bound to that stack — it can leave the goroutine entirely
//     (sent over a channel, handed to Go code) and be applied
//     anywhere, including back inside the function's own loop;
//   - R4: the function's strongly-connected component in the
//     unit's direct-reference graph is itself alone — Go has no
//     general TCO, so a mutual cycle grows the stack even when
//     every call sits "in tail position". The graph's nodes are
//     the unit's FuncDecls *and* VarDecls: a package-level `let`
//     can hold a function value (`let g = f`, `let (g, _) = (fn
//     ..., _)`), so a cycle routed through a var — `f` applies
//     `g`, `g`'s value reaches `f` — grows the stack just the
//     same. The analysis is still static: recursion hidden behind
//     function values the compiler cannot resolve (a parameter
//     fed back, a returned function) stays outside the guarantee.
//
// The check runs on the lowered Core IR, reading markTailCalls'
// output, so it cannot drift from what codegen does: anything ANF
// moved to a Let.X — an operand of a call, `try`, `|?>`, a tuple,
// a `for` head — is automatically non-tail.
//
// R1–R3 inspect only the marked function's own body and run in the
// per-file pass (checkTailRec). R4's reference graph is a property
// of the whole compilation unit — a cycle routed through a
// sibling file's declarations is the same unprovable recursion —
// so it runs after every file lowers, in CheckTailRecMutual, the
// same way CheckInitCycles covers the unit (docs/decisions/
// 2026-11-05-package-initialization-order.md).

// The lowering stage's user-facing codes; E0901 stays the internal
// contract-gap code (docs/problems/2026-11-02-lower-input-contract-gaps.md).
const (
	codeTailrecCall   diag.Code = "E0902" // self-call not in tail position
	codeTailrecRef    diag.Code = "E0903" // marked function used as a value
	codeTailrecMutual diag.Code = "E0904" // mutual recursion through another decl
	codeTailrecNoRec  diag.Code = "E0905" // no self-call in tail position
)

// tailrecSpan picks the span the requirement-level diagnostics
// point at — the function's name when it can be recovered from the
// symbol's syntax definition, else the declaration itself.
func tailrecSpan(fd *core.FuncDecl) diag.Span {
	if fd.Sym != nil {
		if f, ok := fd.Sym.Def.(*syntax.FnDecl); ok && f.Name != nil {
			return f.Name.Sp
		}
	}
	return fd.Sp
}

// checkTailRec validates every marked declaration against R1–R3 on
// the lowered file — the rules the marked function's own body
// decides. R4's direct-reference graph is a property of the whole
// unit and runs in CheckTailRecMutual. The pass runs after the decl
// loop — markTailCalls has already rewritten the convertible
// self-calls — and before core.Validate: a user-facing violation
// should be reported as such, not masked behind an
// internal-invariant diagnostic.
func (l *lowerer) checkTailRec(out *core.File) {
	for _, d := range out.Decls {
		fd, ok := d.(*core.FuncDecl)
		if ok && fd.TailRec && fd.Sym != nil && fd.Fn != nil {
			l.checkTailRecFn(fd)
		}
	}
}

// CheckTailRecMutual applies R4 — a marked function's
// strongly-connected component in the direct-reference graph is
// itself alone — over the lowered Core files of one compilation
// unit. Mutual recursion is a unit property, not a file property:
// a.gom's `fn f` calling b.gom's `fn g` while `g` calls `f` grows
// the stack exactly like a same-file cycle, so the graph is built
// over every member's Decls the same way CheckInitCycles builds its
// initialization graph (docs/decisions/
// 2026-11-05-package-initialization-order.md). A one-file unit is
// the degenerate case. The per-file pass already ran R1–R3.
func CheckTailRecMutual(files []*core.File) []diag.Diagnostic {
	funcs := map[*resolve.Symbol]*core.FuncDecl{}
	vars := map[*resolve.Symbol]*core.VarDecl{}
	var marked []*core.FuncDecl
	for _, out := range files {
		for _, d := range out.Decls {
			switch d := d.(type) {
			case *core.FuncDecl:
				if d.Sym == nil {
					continue
				}
				funcs[d.Sym] = d
				if d.TailRec && d.Fn != nil {
					marked = append(marked, d)
				}
			case *core.VarDecl:
				if d.Sym != nil {
					vars[d.Sym] = d
				}
			case *core.TestDecl, *core.TypeDecl:
				// Test bodies are not call-graph members; type
				// declarations carry no code.
			default:
				panic("unreachable")
			}
		}
	}
	if len(marked) == 0 {
		return nil
	}
	// nodes is the vertex set of the reference graph — every symbol
	// a cycle could route through.
	nodes := make(map[*resolve.Symbol]bool, len(funcs)+len(vars))
	for s := range funcs {
		nodes[s] = true
	}
	for s := range vars {
		nodes[s] = true
	}
	adj := tailrecGraph(files, funcs, vars)
	var diags []diag.Diagnostic
	for _, fd := range marked {
		if through, path := tailrecCycle(fd.Sym, adj, nodes); through != nil {
			diags = append(diags, diag.Error(codeTailrecMutual,
				fmt.Sprintf("`@tailrec` function `%s` is mutually recursive through `%s`", fd.Sym.Name, through.Name)).
				At(tailrecSpan(fd)).
				Set("fn", fd.Sym.Name).
				Set("through", through.Name).
				Set("path", path))
		}
	}
	return diags
}

// checkTailRecFn applies R1–R3 to one marked function.
func (l *lowerer) checkTailRecFn(fd *core.FuncDecl) {
	m := fd.Sym
	sp := tailrecSpan(fd)
	residual := 0  // non-spawned CallDirects to self — each earns E0902
	tailCalls := 0 // converted self TailCalls — R1's requirement
	walkTailrec(fd.Fn.Body, func(e core.Expr, inSpawn, _ bool) {
		// Single-case assertions, not a type switch: most Expr
		// kinds are irrelevant here and the sumtype check would
		// demand a case for every one.
		if c, ok := e.(*core.CallDirect); ok {
			if c.Callee != m {
				return
			}
			if c.Adapter {
				// The firing call of the synthesized value
				// adapter — `let g = f` on a multi-arity f is
				// the same value escape a FuncRef is, so R3
				// reports it here. No Spawn exemption: the
				// adapted value can leave the goroutine and
				// be applied on any stack. It is not a call
				// site, so R1/R2 never count it.
				l.diags = append(l.diags, diag.Error(codeTailrecRef,
					fmt.Sprintf("`@tailrec` function `%s` is used as a value", m.Name)).
					At(c.Sp).Set("fn", m.Name))
				return
			}
			if inSpawn {
				// R2's exemption: a spawned self-call runs on a
				// fresh goroutine stack (spec section 10).
				return
			}
			residual++
			l.diags = append(l.diags, diag.Error(codeTailrecCall,
				fmt.Sprintf("`@tailrec` function `%s` has a recursive call that is not in tail position", m.Name)).
				At(c.Sp).Set("fn", m.Name))
			return
		}
		if t, ok := e.(*core.TailCall); ok {
			if t.To == fd.Fn {
				tailCalls++
			}
			return
		}
		if r, ok := e.(*core.FuncRef); ok {
			if r.Sym != m {
				return
			}
			l.diags = append(l.diags, diag.Error(codeTailrecRef,
				fmt.Sprintf("`@tailrec` function `%s` is used as a value", m.Name)).
				At(r.Sp).Set("fn", m.Name))
		}
	})
	if tailCalls == 0 && residual == 0 {
		// R1: no convertible self-call at all — the function has
		// none, every self-call hides behind a value or alias
		// (R3/E0904 explain the escape), or the only self-calls sit
		// inside `go` bodies, which are exempt from R2 but are not
		// convertible. Residual calls are covered by E0902, so a
		// function already reported there is not reported again.
		l.diags = append(l.diags, diag.Error(codeTailrecNoRec,
			fmt.Sprintf("`@tailrec` function `%s` has no self-recursive call in tail position", m.Name)).
			At(sp).Set("fn", m.Name))
	}
}

// tailrecGraph builds the unit's direct-reference graph for R4: an
// edge g → h means g's body — nested Closures counted, Spawn bodies
// not — can hand control to h inside one activation. Three node
// kinds produce edges, over two declaration kinds:
//
//   - CallDirect to a unit FuncDecl — a plain call;
//   - FuncRef of a unit FuncDecl — a function handed to a
//     combinator or bound to a variable can still be applied inside
//     the caller's activation, which is the same stack-growing cycle
//     a direct call would make;
//   - Var naming a unit VarDecl — a package-level `let` is a
//     graph node because it can hold a function value: applying it
//     (`g (n - 1)` lowers to CallFunc on the Var) is a call, and any
//     other use may carry the value to an application — the same
//     conservative argument as FuncRef.
//
// A VarDecl's own initializer runs once at package-init time, so a
// CallDirect sitting at its top level — `let r = f 0` — is not a way
// the stored value can call f back: those edges would read an
// initialization-order dependency as recursion. Only calls *inside a
// Closure* in the initializer belong to the stored function value,
// so only they count — the FuncRef and Var edges elsewhere in the
// body already cover `let g = f` aliases and `let (g, _) = (fn ...,
// _)` destructurings (which route through a FieldGet's Var).
func tailrecGraph(files []*core.File, funcs map[*resolve.Symbol]*core.FuncDecl, vars map[*resolve.Symbol]*core.VarDecl) map[*resolve.Symbol]map[*resolve.Symbol]bool {
	adj := map[*resolve.Symbol]map[*resolve.Symbol]bool{}
	for _, out := range files {
		for _, d := range out.Decls {
			var from *resolve.Symbol
			var b *core.Body
			var init bool // a VarDecl initializer — see the doc comment
			if fd, ok := d.(*core.FuncDecl); ok && fd.Sym != nil && fd.Fn != nil {
				from, b = fd.Sym, fd.Fn.Body
			} else if vd, ok := d.(*core.VarDecl); ok && vd.Sym != nil {
				from, b, init = vd.Sym, vd.Body, true
			} else {
				continue
			}
			walkTailrec(b, func(e core.Expr, inSpawn, inClosure bool) {
				if inSpawn {
					return
				}
				var to *resolve.Symbol
				// Assertions again — only three Expr kinds produce edges.
				if c, ok := e.(*core.CallDirect); ok {
					if !init || inClosure {
						to = c.Callee
					}
				} else if r, ok := e.(*core.FuncRef); ok {
					to = r.Sym
				} else if v, ok := e.(*core.Var); ok {
					to = v.Sym
				}
				if to != nil && (funcs[to] != nil || vars[to] != nil) {
					if adj[from] == nil {
						adj[from] = map[*resolve.Symbol]bool{}
					}
					adj[from][to] = true
				}
			})
		}
	}
	return adj
}

// tailrecCycle finds a node h ≠ f that shares a reference cycle with
// f — f reaches h and h reaches f through the direct-reference
// graph; h may be a FuncDecl or a VarDecl (a package-level `let`
// holding a function value routes a cycle just the same). It returns
// one such partner (the smallest name, for determinism) and a
// witness path f → … → h → … → f of symbol names, or (nil, nil) when
// f's strongly-connected component is {f} alone. The analysis is
// unit-wide, which is complete: Go forbids import cycles, so
// recursion cannot cross packages.
func tailrecCycle(f *resolve.Symbol, adj map[*resolve.Symbol]map[*resolve.Symbol]bool, nodes map[*resolve.Symbol]bool) (*resolve.Symbol, []string) {
	fwd := tailrecReach(adj, f)
	var best *resolve.Symbol
	var bestPath []string
	for h := range nodes {
		if h == f || !fwd[h] || !tailrecReach(adj, h)[f] {
			continue
		}
		path := append(tailrecPath(adj, f, h), tailrecPath(adj, h, f)[1:]...)
		if best == nil || h.Name < best.Name {
			best, bestPath = h, path
		}
	}
	return best, bestPath
}

// tailrecReach is the set of nodes reachable from `from` through
// adjacency, `from` included. Files are small, so plain BFS over
// the map graph is enough — no Tarjan needed.
func tailrecReach(adj map[*resolve.Symbol]map[*resolve.Symbol]bool, from *resolve.Symbol) map[*resolve.Symbol]bool {
	seen := map[*resolve.Symbol]bool{from: true}
	queue := []*resolve.Symbol{from}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for h := range adj[n] {
			if !seen[h] {
				seen[h] = true
				queue = append(queue, h)
			}
		}
	}
	return seen
}

// tailrecNeighbors lists an adjacency set's targets in a fixed
// order — (name, definition offset) — so the BFS path a diagnostic
// reports is deterministic.
func tailrecNeighbors(set map[*resolve.Symbol]bool) []*resolve.Symbol {
	out := make([]*resolve.Symbol, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b *resolve.Symbol) int {
		if c := cmp.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.DefSpan().Start, b.DefSpan().Start)
	})
	return out
}

// tailrecPath returns a shortest symbol path from `from` to `to`
// (names only, for diagnostic data). Callers guarantee one exists.
func tailrecPath(adj map[*resolve.Symbol]map[*resolve.Symbol]bool, from, to *resolve.Symbol) []string {
	prev := map[*resolve.Symbol]*resolve.Symbol{}
	queue := []*resolve.Symbol{from}
	prev[from] = nil
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if n == to {
			break
		}
		for _, h := range tailrecNeighbors(adj[n]) {
			if _, ok := prev[h]; !ok {
				prev[h] = n
				queue = append(queue, h)
			}
		}
	}
	var rev []string
	for n := to; n != nil; n = prev[n] {
		rev = append(rev, n.Name)
	}
	slices.Reverse(rev)
	return rev
}

// walkTailrec calls f on every expression node — atoms included —
// inside body b, at every depth: Let.X values, branch bodies,
// decision-tree leaves, and nested Closure / ForRange / Spawn
// bodies. inSpawn reports whether the node sits under a Spawn body
// and inClosure whether it sits under a Closure body (each flag is
// monotone once set). Boundary back-pointers (TailCall.To,
// EscapeResult.To) are never followed — they name the enclosing
// function, not subtrees.
func walkTailrec(b *core.Body, f func(e core.Expr, inSpawn, inClosure bool)) {
	var body func(b *core.Body, inSpawn, inClosure bool)
	var expr func(e core.Expr, inSpawn, inClosure bool)
	var atom func(a core.Atom, inSpawn, inClosure bool)
	var atoms func(as []core.Atom, inSpawn, inClosure bool)
	var callGo func(c *core.CallGo, inSpawn, inClosure bool)
	var tree func(t core.DTree, inSpawn, inClosure bool)
	var selArm func(a core.SelArm, inSpawn, inClosure bool)

	body = func(b *core.Body, inSpawn, inClosure bool) {
		if b == nil {
			return
		}
		for _, l := range b.Lets {
			if l != nil {
				expr(l.X, inSpawn, inClosure)
			}
		}
		expr(b.Tail, inSpawn, inClosure)
	}
	atom = func(a core.Atom, inSpawn, inClosure bool) {
		if a == nil {
			return
		}
		f(a, inSpawn, inClosure)
		switch a := a.(type) {
		case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef:
		case *core.GoFuncRef:
			if a.Recv != nil {
				atom(a.Recv, inSpawn, inClosure)
			}
		case *core.FieldGet:
			atom(a.Recv, inSpawn, inClosure)
		default:
			panic("unreachable")
		}
	}
	atoms = func(as []core.Atom, inSpawn, inClosure bool) {
		for _, a := range as {
			atom(a, inSpawn, inClosure)
		}
	}
	callGo = func(c *core.CallGo, inSpawn, inClosure bool) {
		if c == nil {
			return
		}
		if c.Recv != nil {
			atom(c.Recv, inSpawn, inClosure)
		}
		atoms(c.Args, inSpawn, inClosure)
	}
	tree = func(t core.DTree, inSpawn, inClosure bool) {
		switch t := t.(type) {
		case *core.DLeaf:
			body(t.Body, inSpawn, inClosure)
		case *core.DPanic:
			atom(t.Subj, inSpawn, inClosure)
		case *core.DGuard:
			atom(t.Cond, inSpawn, inClosure)
			tree(t.Then, inSpawn, inClosure)
			tree(t.Else, inSpawn, inClosure)
		case *core.DSwitch:
			atom(t.Subj, inSpawn, inClosure)
			for i := range t.Cases {
				body(t.Cases[i].Body, inSpawn, inClosure)
			}
			if t.Default != nil {
				tree(t.Default, inSpawn, inClosure)
			}
		default:
			panic("unreachable")
		}
	}
	selArm = func(a core.SelArm, inSpawn, inClosure bool) {
		switch a := a.(type) {
		case *core.SelRecv:
			atom(a.Chan, inSpawn, inClosure)
			body(a.Body, inSpawn, inClosure)
		case *core.SelSend:
			atom(a.Chan, inSpawn, inClosure)
			atom(a.Value, inSpawn, inClosure)
			body(a.Body, inSpawn, inClosure)
		case *core.SelDefault:
			body(a.Body, inSpawn, inClosure)
		default:
			panic("unreachable")
		}
	}
	expr = func(e core.Expr, inSpawn, inClosure bool) {
		if e == nil {
			return
		}
		f(e, inSpawn, inClosure)
		switch e := e.(type) {
		case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef:
		case *core.GoFuncRef:
			if e.Recv != nil {
				atom(e.Recv, inSpawn, inClosure)
			}
		case *core.FieldGet:
			atom(e.Recv, inSpawn, inClosure)
		case *core.Closure:
			if e.Fn != nil {
				body(e.Fn.Body, inSpawn, true)
			}
		case *core.CallDirect:
			atoms(e.Args, inSpawn, inClosure)
		case *core.CallFunc:
			atom(e.Fn, inSpawn, inClosure)
			atoms(e.Args, inSpawn, inClosure)
		case *core.CallGo:
			callGo(e, inSpawn, inClosure)
		case *core.CallBuiltin:
			atoms(e.Args, inSpawn, inClosure)
		case *core.CtorApp:
			atoms(e.Args, inSpawn, inClosure)
		case *core.Unary:
			atom(e.X, inSpawn, inClosure)
		case *core.Binary:
			atom(e.X, inSpawn, inClosure)
			atom(e.Y, inSpawn, inClosure)
		case *core.If:
			atom(e.Cond, inSpawn, inClosure)
			body(e.Then, inSpawn, inClosure)
			body(e.Else, inSpawn, inClosure)
		case *core.Match:
			tree(e.Tree, inSpawn, inClosure)
		case *core.SelectExpr:
			for _, a := range e.Arms {
				selArm(a, inSpawn, inClosure)
			}
		case *core.Spawn:
			body(e.Body, true, inClosure)
		case *core.ForRange:
			atom(e.Iter, inSpawn, inClosure)
			body(e.Body, inSpawn, inClosure)
		case *core.TupleNew:
			atoms(e.Elems, inSpawn, inClosure)
		case *core.ListNew:
			atoms(e.Elems, inSpawn, inClosure)
			if e.Rest != nil {
				atom(e.Rest, inSpawn, inClosure)
			}
		case *core.RecordNew:
			for _, fi := range e.Fields {
				atom(fi.Value, inSpawn, inClosure)
			}
		case *core.RecordUpdate:
			atom(e.Rec, inSpawn, inClosure)
			for _, fi := range e.Fields {
				atom(fi.Value, inSpawn, inClosure)
			}
		case *core.TailCall:
			atoms(e.Args, inSpawn, inClosure)
		case *core.EscapeResult:
			atom(e.X, inSpawn, inClosure)
		case *core.EscapeCall:
			callGo(e.Call, inSpawn, inClosure)
		case *core.ResultFromGo:
			callGo(e.Call, inSpawn, inClosure)
		case *core.OptionFromGo:
			callGo(e.Call, inSpawn, inClosure)
		case *core.TupleFromGo:
			callGo(e.Call, inSpawn, inClosure)
		case *core.ErrorFromGo:
			callGo(e.Call, inSpawn, inClosure)
		case *core.Conv:
			atom(e.X, inSpawn, inClosure)
		case *core.Equal:
			atom(e.X, inSpawn, inClosure)
			atom(e.Y, inSpawn, inClosure)
		default:
			panic("unreachable")
		}
	}
	body(b, false, false)
}
