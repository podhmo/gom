package check

import (
	"fmt"
	"slices"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// checkScopeEscapes reports E0418: a `Scope` handle outliving its
// `Scope.run` body (spec 6.1, decisions/2026-11-21-scoped-cleanup).
//
//   - By type: Scope may appear only as a whole value or as a
//     function parameter — never under a type constructor, in a tuple,
//     in a function result, or as a constructor / record field value.
//   - By capture: a lambda or `go` body capturing a Scope may only be
//     called on the spot or passed directly to a non-escaping
//     parameter (Signature.NoEscape). Function-typed parameters of this
//     package's `fn`s are inferred non-escaping by a greatest fixpoint:
//     a parameter stays non-escaping while every use calls it or passes
//     it (or a lambda capturing it) to a non-escaping parameter.
func (c *checker) checkScopeEscapes() {
	if c.shadowedBuiltin("Scope") {
		return
	}
	se := &scopeEsc{c: c, cand: map[*resolve.Symbol]bool{}, params: map[*resolve.Symbol][]*resolve.Symbol{}}
	for _, d := range c.decls {
		fd, ok := d.(*FnDecl)
		if !ok || fd.Sig == nil || fd.Sym == nil {
			continue
		}
		ps := make([]*resolve.Symbol, len(fd.Params))
		for i, p := range fd.Params {
			vp, ok := p.Pat.(*VarPat)
			if !ok || vp.Sym == nil || i >= len(fd.Sig.Params) {
				continue
			}
			if _, isFn := types.Chase(fd.Sig.Params[i].Type).(*types.Func); isFn {
				ps[i] = vp.Sym
				se.cand[vp.Sym] = true
			}
		}
		if fd.Body == nil {
			// @extern (hand-written Go) or a .gomi signature: the
			// body is out of sight, so every parameter escapes.
			for _, p := range ps {
				delete(se.cand, p)
			}
		}
		se.params[fd.Sym] = ps
	}
	for {
		se.changed = false
		se.walkDecls()
		if !se.changed {
			break
		}
	}
	for _, d := range c.decls {
		if fd, ok := d.(*FnDecl); ok && fd.Body != nil && fd.Sym != nil {
			var ne []bool
			for i, p := range se.params[fd.Sym] {
				if p != nil && se.cand[p] {
					if ne == nil {
						ne = make([]bool, len(fd.Params))
					}
					ne[i] = true
				}
			}
			fd.NoEscape = ne
		}
	}
	se.report = true
	se.walkDecls()
	for _, d := range c.decls {
		// typeErr skips a decl whose body already reported inside it
		// only when the decl span is the inner one, so check here.
		fd, ok := d.(*FnDecl)
		if !ok || fd.Sig == nil || !c.scopeBad(fd.Sig, true) || se.reportedWithin(fd.Sp) {
			continue
		}
		se.typeErr(fd.Sp, fd.Sig)
	}
}

type scopeEsc struct {
	c       *checker
	cand    map[*resolve.Symbol]bool              // function-typed params still believed non-escaping
	params  map[*resolve.Symbol][]*resolve.Symbol // fn symbol → its params (nil when not a candidate)
	changed bool
	report  bool
	typed   []diag.Span // E0418 type sites already reported (nested expressions stay quiet)
}

func (se *scopeEsc) walkDecls() {
	se.typed = nil
	for _, d := range se.c.decls {
		switch d := d.(type) {
		case *FnDecl:
			se.visit(d.Body, false)
		case *LetDecl:
			se.visit(d.Value, false)
		case *TestDecl:
			se.visit(d.Body, false)
		case *TypeDecl:
		default:
			panic("unreachable")
		}
	}
}

// isScope reports whether t is the builtin Scope.
func isScope(t types.Type) bool {
	con, ok := types.Chase(t).(*types.Con)
	return ok && con.Qual == "" && con.Name == "Scope"
}

// scopeBad reports a Scope in a position where it could outlive its
// body; top marks a position where a whole Scope value is fine.
func (c *checker) scopeBad(t types.Type, top bool) bool {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if t.Qual == "" && t.Name == "Scope" {
			return !top
		}
		for _, a := range t.Args {
			if c.scopeBad(a, false) {
				return true
			}
		}
	case *types.Func:
		for _, p := range t.Params {
			if c.scopeBad(p.Type, true) {
				return true
			}
		}
		return c.scopeBad(t.Ret, false)
	case *types.Tuple:
		for _, e := range t.Elems {
			if c.scopeBad(e, false) {
				return true
			}
		}
	case *types.Go:
		for _, a := range t.Args {
			if c.scopeBad(a, false) {
				return true
			}
		}
	case *types.Var, *types.Meta:
	default:
		panic("unreachable")
	}
	return false
}

func (se *scopeEsc) reportedWithin(sp diag.Span) bool {
	for _, r := range se.typed {
		if r.File == sp.File && sp.Start <= r.Start && r.End <= sp.End {
			return true
		}
	}
	return false
}

func (se *scopeEsc) typeErr(sp diag.Span, t types.Type) {
	for _, r := range se.typed {
		if r.File == sp.File && r.Start <= sp.Start && sp.End <= r.End {
			return
		}
	}
	se.typed = append(se.typed, sp)
	tS := types.Display(types.Chase(t), metaNamer())
	se.c.diags.Add(diag.Error("E0418",
		fmt.Sprintf("a Scope cannot be stored or returned: type %s carries it out of its scope", tS)).
		At(sp).Set("reason", "type").Set("type", tS))
}

func (se *scopeEsc) storeErr(sp diag.Span) {
	se.c.diags.Add(diag.Error("E0418", "a Scope cannot be stored in a constructor or record field").
		At(sp).Set("reason", "store"))
}

// captures returns the outside bindings body refers to that make a
// closure over it restricted: Scope values and candidate parameters.
func (se *scopeEsc) captures(body Expr, within diag.Span) (scope *Var, params []*resolve.Symbol) {
	WalkExpr(body, func(e Expr) {
		v, ok := e.(*Var)
		if !ok || v.Sym == nil || v.Sym.Kind != resolve.KindVar {
			return
		}
		if def := v.Sym.DefSpan(); def.File == within.File && def.Start >= within.Start && def.End <= within.End {
			return
		}
		if isScope(v.Type()) {
			if scope == nil {
				scope = v
			}
		} else if se.cand[v.Sym] {
			params = append(params, v.Sym)
		}
	})
	return scope, params
}

func (se *scopeEsc) escape(sym *resolve.Symbol) {
	if se.cand[sym] {
		delete(se.cand, sym)
		se.changed = true
	}
}

// closure handles a lambda or `go` body in position ok.
func (se *scopeEsc) closure(sp diag.Span, body Expr, ok bool, what string) {
	scope, params := se.captures(body, sp)
	if !ok {
		for _, p := range params {
			se.escape(p)
		}
		if scope != nil && se.report {
			se.c.diags.Add(diag.Error("E0418",
				fmt.Sprintf("%s capturing Scope `%s` escapes its scope", what, scope.Sym.Name)).
				At(sp).Set("reason", "capture").Set("name", scope.Sym.Name))
		}
	}
}

// noEscape reports whether flattened parameter i of the call's
// callee is non-escaping.
func (se *scopeEsc) noEscape(call CallInfo, i int) bool {
	if call.Callee == nil || call.Kind == CallGo || call.Kind == CallUnknown {
		return false
	}
	if ps, ok := se.params[call.Callee]; ok {
		return i < len(ps) && ps[i] != nil && se.cand[ps[i]]
	}
	if se.c.src != nil {
		if sig, ok := se.c.src.SignatureOf(call.Callee); ok {
			return i < len(sig.NoEscape) && sig.NoEscape[i]
		}
	}
	return false
}

// paramOf is the flattened parameter Args[i] fills.
func paramOf(call CallInfo, i int) int {
	if call.ArgToParam != nil && i < len(call.ArgToParam) {
		return call.ArgToParam[i]
	}
	return i
}

// nextParam is the first parameter a partial application leaves
// unfilled — the one a pipe supplies.
func nextParam(call CallInfo, nargs int) int {
	filled := map[int]bool{}
	for i := range nargs {
		filled[paramOf(call, i)] = true
	}
	for p := 0; ; p++ {
		if !filled[p] {
			return p
		}
	}
}

// visitApply visits an application; extra is the number of arguments
// an enclosing pipe supplies right away (0 or 1), which makes a
// partial application saturating.
func (se *scopeEsc) visitApply(ap *Apply, extra int) {
	se.visit(ap.Fn, true)
	direct := ap.Call.Kind == CallDirect || ap.Call.Kind == CallPartial && ap.Call.Unapplied <= extra
	isCtor := ap.Call.Callee != nil && ap.Call.Callee.Kind == resolve.KindCtor
	for i, a := range ap.Args {
		if isCtor && isScope(a.Value.Type()) {
			se.reportStore(a.Value.Span())
		}
		se.visit(a.Value, direct && se.noEscape(ap.Call, paramOf(ap.Call, i)))
	}
}

func (se *scopeEsc) reportStore(sp diag.Span) {
	if !se.report {
		return
	}
	for _, r := range se.typed {
		if r.File == sp.File && r.Start <= sp.Start && sp.End <= r.End {
			return
		}
	}
	se.storeErr(sp)
}

// pipeTarget visits the function side f of `x |> f` / `f <| x` and
// reports whether x lands in a non-escaping parameter.
func (se *scopeEsc) pipeTarget(f Expr) bool {
	switch f := f.(type) {
	case *Apply:
		se.visitApply(f, 1)
		if f.Call.Kind == CallPartial && f.Call.Unapplied == 1 {
			return se.noEscape(f.Call, nextParam(f.Call, len(f.Args)))
		}
		return false
	case *Var, *MemberExpr:
		se.visit(f, true)
		var sym *resolve.Symbol
		if v, ok := f.(*Var); ok {
			sym = v.Sym
		} else {
			sym = f.(*MemberExpr).Sym
		}
		return sym != nil && se.noEscape(CallInfo{Kind: CallDirect, Callee: sym}, 0)
	default:
		se.visit(f, false)
		return false
	}
}

// visit walks e; ok marks a non-escaping position (called on the spot
// or passed to a non-escaping parameter).
func (se *scopeEsc) visit(e Expr, ok bool) {
	if e == nil {
		return
	}
	// A reference's instantiated type only echoes the origin of a
	// bad type, so Var and MemberExpr sites stay quiet.
	switch e.(type) {
	case *Var, *MemberExpr:
	default:
		if se.report && se.c.scopeBad(e.Type(), true) {
			se.typeErr(e.Span(), e.Type())
		}
	}
	switch e := e.(type) {
	case *Lit, *Hole:
	case *Var:
		if !ok && e.Sym != nil {
			se.escape(e.Sym)
		}
	case *Lambda:
		se.closure(e.Sp, e.Body, ok, "a closure")
		se.visit(e.Body, false)
	case *GoExpr:
		se.closure(e.Sp, e.Body, false, "a `go` body")
		se.visit(e.Body, false)
	case *Apply:
		se.visitApply(e, 0)
	case *Binary:
		switch e.Op {
		case syntax.BinPipeFwd, syntax.BinPipeTry:
			se.visit(e.X, se.pipeTarget(e.Y))
		case syntax.BinPipeBack:
			se.visit(e.Y, se.pipeTarget(e.X))
		default:
			se.visit(e.X, false)
			se.visit(e.Y, false)
		}
	case *MemberExpr:
		se.visit(e.Recv, false)
	case *Unary:
		se.visit(e.X, false)
	case *StrInterp:
		for _, p := range e.Parts {
			switch p := p.(type) {
			case *StrExpr:
				se.visit(p.X, false)
			case *StrText:
			default:
				panic("unreachable")
			}
		}
	case *If:
		se.visit(e.Cond, false)
		se.visit(e.Then, false)
		se.visit(e.Else, false)
	case *Match:
		se.visit(e.Subj, false)
		for _, a := range e.Arms {
			se.visit(a.Guard, false)
			se.visit(a.Body, false)
		}
	case *Block:
		for _, s := range e.Stmts {
			se.visit(s, false)
		}
	case *Let:
		se.visit(e.Value, false)
	case *Ignore:
		se.visit(e.X, false)
	case *Try:
		se.visit(e.X, false)
	case *FieldAssign:
		se.visit(e.Recv, false)
		se.visit(e.Value, false)
	case *For:
		se.visit(e.Iter, false)
		se.visit(e.Body, false)
	case *SelectExpr:
		for _, a := range e.Arms {
			switch a := a.(type) {
			case *RecvArm:
				se.visit(a.Chan, false)
				se.visit(a.Body, false)
			case *SendArm:
				se.visit(a.Chan, false)
				se.visit(a.Value, false)
				se.visit(a.Body, false)
			case *DefaultArm:
				se.visit(a.Body, false)
			default:
				panic("unreachable")
			}
		}
	case *ListLit:
		for _, x := range e.Elems {
			se.visit(x, false)
		}
	case *TupleLit:
		for _, x := range e.Elems {
			se.visit(x, false)
		}
	case *Spread:
		se.visit(e.X, false)
	case *RecordLit:
		se.visit(e.With, false)
		for _, f := range e.Fields {
			if isScope(f.Value.Type()) {
				se.reportStore(f.Value.Span())
			}
			se.visit(f.Value, false)
		}
	case *Ascribe:
		if e.Conv && isScope(e.X.Type()) {
			se.reportStore(e.X.Span())
		}
		se.visit(e.X, false)
	default:
		panic("unreachable")
	}
}

// noEscapeAttr maps a .gomi `@noescape(f, ...)` to per-parameter
// flags; each name must be a function-typed parameter (E0419).
func (c *checker) noEscapeAttr(d *syntax.FnDecl, params []Param, sig *types.Func) []bool {
	if len(d.NoEscape) == 0 {
		return nil
	}
	out := make([]bool, len(params))
	for _, id := range d.NoEscape {
		i := slices.IndexFunc(params, func(p Param) bool {
			vp, ok := p.Pat.(*VarPat)
			return ok && vp.Sym != nil && vp.Sym.Name == id.Name
		})
		switch {
		case i < 0 || sig == nil || i >= len(sig.Params):
			c.diags.Add(diag.Error("E0419",
				fmt.Sprintf("`@noescape` names `%s`, which is not a parameter of `%s`", id.Name, d.Name.Name)).
				At(id.Sp).Set("name", id.Name))
		case !isFuncType(sig.Params[i].Type):
			c.diags.Add(diag.Error("E0419",
				fmt.Sprintf("`@noescape` names `%s`, whose type %s is not a function", id.Name, sig.Params[i].Type)).
				At(id.Sp).Set("name", id.Name))
		default:
			out[i] = true
		}
	}
	return out
}

func isFuncType(t types.Type) bool {
	_, ok := types.Chase(t).(*types.Func)
	return ok
}
