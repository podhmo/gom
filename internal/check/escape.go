package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// The Scope escape check of spec section 6.1 (SC-2,
// decisions/2026-11-21-scoped-cleanup). Two rules keep a `Scope`
// value inside the `Scope.run` body it belongs to:
//
//   - type positions: `Scope` may appear only at a parameter's leaf
//     or inside function-argument positions — never in a result,
//     record field, variant payload, type argument, or extern
//     signature (E0419, checked at signature/annotation elaboration).
//   - use sites: a `Scope`-typed variable — and any lambda capturing
//     one — may reach only contexts that cannot retain it: an
//     argument slot of a non-escaping parameter, a `let` binding, or
//     a consumed position. Passing it to a Go call, a `go {}` body,
//     `Task.spawn`, a constructor or collection literal, or an
//     escaping parameter is E0419.
//
// Escaping of *parameters* is inferred per package by a monotone
// fixpoint: a parameter escapes when the body places it (or an
// alias) in a retaining position. The flags are exported into
// `.gomi` as `//gom:noescape` directive lines so importers keep the
// same judgment; an absent directive means "assume escaping". Go
// callees always escape. A Gom callee's `Scope`-typed parameter is
// non-escaping by construction — its own compile already enforced
// the rule. See docs/decisions/2026-11-22-scope-escape-check.md.

// isScopeType reports whether t is exactly the builtin `Scope`.
func isScopeType(t types.Type) bool {
	con, ok := types.Chase(t).(*types.Con)
	return ok && con.Qual == "" && con.Name == "Scope" && len(con.Args) == 0
}

// containsScope reports whether `Scope` occurs anywhere inside t.
func (c *checker) containsScope(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Meta, *types.Var:
		return false
	case *types.Func:
		for _, p := range t.Params {
			if c.containsScope(p.Type) {
				return true
			}
		}
		return c.containsScope(t.Ret)
	case *types.Tuple:
		for _, e := range t.Elems {
			if c.containsScope(e) {
				return true
			}
		}
		return false
	case *types.Con:
		if t.Qual == "" && t.Name == "Scope" {
			return true
		}
		for _, a := range t.Args {
			if c.containsScope(a) {
				return true
			}
		}
		return false
	case *types.Go:
		for _, a := range t.Args {
			if c.containsScope(a) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// scopeParamOK — `Scope` allowed at the leaf and at argument
// positions of nested function types: `Scope`, `Scope -> a`,
// `(Scope -> a) -> b`. Result positions are scopeResultOK.
func (c *checker) scopeParamOK(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Meta, *types.Var:
		return true
	case *types.Func:
		for _, p := range t.Params {
			if !c.scopeParamOK(p.Type) {
				return false
			}
		}
		return c.scopeResultOK(t.Ret)
	case *types.Con, *types.Go, *types.Tuple:
		return isScopeType(t) || !c.containsScope(t)
	default:
		panic("unreachable")
	}
}

// scopeResultOK — `Scope` may not be produced: the leaf is banned,
// nested function types keep the param/result rules.
func (c *checker) scopeResultOK(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Meta, *types.Var:
		return true
	case *types.Func:
		for _, p := range t.Params {
			if !c.scopeParamOK(p.Type) {
				return false
			}
		}
		return c.scopeResultOK(t.Ret)
	case *types.Con, *types.Go, *types.Tuple:
		return !c.containsScope(t)
	default:
		panic("unreachable")
	}
}

func (c *checker) scopeTypeErr(sp diag.Span, what string) {
	c.diags.Add(diag.Error("E0419",
		fmt.Sprintf("a %s cannot mention `Scope` here — it is allowed only as a parameter's leaf type or inside function-argument positions", what)).
		At(sp))
}

// checkScopeTypeParam validates a parameter or annotation type: the
// leaf may be `Scope` and nested function types may carry `Scope` in
// argument positions.
func (c *checker) checkScopeTypeParam(te syntax.TypeExpr, t types.Type, what string) {
	if te == nil || t == nil {
		return
	}
	if !c.scopeParamOK(t) {
		c.scopeTypeErr(te.Span(), what)
	}
}

// checkScopeTypeResult validates a result type: `Scope` may not be
// produced anywhere in it.
func (c *checker) checkScopeTypeResult(te syntax.TypeExpr, t types.Type, what string) {
	if te == nil || t == nil {
		return
	}
	if !c.scopeResultOK(t) {
		c.scopeTypeErr(te.Span(), what)
	}
}

// checkScopeTypeFree validates a type where `Scope` may not appear
// at all: record fields, variant payloads, extern signatures.
func (c *checker) checkScopeTypeFree(te syntax.TypeExpr, t types.Type, what string) {
	if te == nil || t == nil {
		return
	}
	if c.containsScope(t) {
		c.scopeTypeErr(te.Span(), what)
	}
}

// --- use-site escape analysis ---

// escapeBody is the per-body working state of the escape walk:
// aliases marks let-bound vars whose value mentions a tainted
// variable; danger records symbols seen in escaping positions (the
// param-flag input); forward marks lambda arguments whose produced
// value the callee returns as the call's own result — the scope
// runner calls are the transparent case, so an escaping call
// position reaches the body lambda's tail through them.
type escapeBody struct {
	aliases map[*resolve.Symbol]bool
	danger  map[*resolve.Symbol]bool
	forward map[Expr]bool
	// taintMemo caches produced-value taint of let-bound
	// expressions so alias chains resolve in linear time.
	taintMemo map[Expr]bool
	// expanding guards runnerArgTainted's declared-body lookup
	// against a runner calling itself through its body argument.
	expanding map[*resolve.Symbol]bool
	// declTaint caches a declared body's produced-value taint so
	// chains of runners over named helpers stay linear.
	declTaint map[*resolve.Symbol]bool
	// lets maps let-bound symbols of the body under the walk to
	// their bound expressions, so a named body argument of a
	// scope runner resolves like the inline lambda.
	lets map[*resolve.Symbol]Expr
	// bindOf maps each let-bound symbol to the free variables of
	// its bound value — an escaping use of the binding may carry
	// any of them, so markDanger propagates through it. A bound
	// lambda's own parameters do not appear (capturedVars reads
	// only free uses).
	bindOf map[*resolve.Symbol][]*resolve.Symbol
	// flagsChanged reports that a let-bound lambda's inferred
	// escape flags grew during this walk — the fixpoint needs
	// another pass so callers can see them.
	flagsChanged bool
}

func (b *escapeBody) tainted(s *resolve.Symbol) bool { return b.aliases[s] }

// markDanger marks sym as escaping, then every variable a let
// binding chained it from — `let x = p` followed by `ret x` must
// land on p's parameter flag, not only on the alias.
func (b *escapeBody) markDanger(sym *resolve.Symbol) {
	for queue := []*resolve.Symbol{sym}; len(queue) > 0; {
		s := queue[0]
		queue = queue[1:]
		if b.danger[s] {
			continue
		}
		b.danger[s] = true
		queue = append(queue, b.bindOf[s]...)
	}
}

// checkScopeEscape runs the use-site pass: first a monotone
// fixpoint infers each top-level parameter's escape flag, then a
// second pass reports E0419 at tainted variables in escaping
// positions. Flags land on sym.Noescape for .gomi emission.
func (c *checker) checkScopeEscape() {
	// Pass 0 — fill each named callee's ResultTaint flags, so the
	// use-site queries below and the `.gomi` emitter read the
	// inferred answer rather than the assume-tainted default.
	for _, d := range c.decls {
		if fn, ok := d.(*FnDecl); ok && fn.Sym != nil &&
			fn.Body != nil && len(fn.Params) > 0 {
			c.resultTaintOf(fn.Sym)
		}
		if ld, ok := d.(*LetDecl); ok {
			if _, isLam := ld.Value.(*Lambda); isLam {
				for _, s := range patBoundSyms(ld.Pat) {
					c.resultTaintOf(s)
				}
			}
		}
	}
	// Pass 1 — fixpoint over param escape flags.
	for changed := true; changed; {
		changed = false
		for _, d := range c.decls {
			body := c.declBody(d)
			if body == nil {
				// .gomi declarations have no body — their
				// flags come from `//gom:noescape`
				// directives, and absent those they must
				// keep the nil (assume-escaping) default.
				continue
			}
			if fn, ok := d.(*FnDecl); ok && fn.Sym != nil &&
				len(fn.Sym.Escapes) == 0 && len(fn.Params) > 0 {
				fn.Sym.Escapes = make([]bool, len(fn.Params))
			}
			b := c.newEscapeBody(d)
			c.escWalk(body, true, b, false)
			if b.flagsChanged {
				changed = true
			}
			if fn, ok := d.(*FnDecl); ok && fn.Sym != nil {
				for sym := range b.danger {
					if i, ok := c.declParamIndex(d, sym); ok && i < len(fn.Sym.Escapes) {
						if !fn.Sym.Escapes[i] {
							fn.Sym.Escapes[i] = true
							changed = true
						}
					}
				}
			}
		}
		// A top-level `let f = fn ...` is a named callee too: give
		// its bound symbol the same parameter inference declared
		// functions get so callers do not default to escaping. A
		// `let f = g` alias names the very callable g resolved
		// to — g's proven positions hold for f as well.
		for _, d := range c.decls {
			if ld, ok := d.(*LetDecl); ok {
				if lam, isLam := unascribe(ld.Value).(*Lambda); isLam {
					for _, s := range patBoundSyms(ld.Pat) {
						if c.inferLetLambdaEscapes(s, lam) {
							changed = true
						}
					}
				}
				for _, s := range patBoundSyms(ld.Pat) {
					if c.propagateAliasEscapes(s, ld.Value) {
						changed = true
					}
				}
			}
		}
	}
	// Pass 2 — report tainted uses in escaping positions.
	for _, d := range c.decls {
		c.escWalk(c.declBody(d), true, c.newEscapeBody(d), true)
	}
}

// declParamIndex maps a top-level declaration's parameter symbol to
// its flattened index inside sym.Escapes.
func (c *checker) declParamIndex(d Decl, sym *resolve.Symbol) (int, bool) {
	switch d := d.(type) {
	case *FnDecl:
		for i, p := range d.Params {
			for _, s := range patBoundSyms(p.Pat) {
				if s == sym {
					return i, true
				}
			}
		}
	case *LetDecl, *TestDecl, *TypeDecl:
	}
	return 0, false
}

func (c *checker) declBody(d Decl) Expr {
	switch d := d.(type) {
	case *FnDecl:
		return d.Body
	case *LetDecl:
		return d.Value
	case *TestDecl:
		return d.Body
	case *TypeDecl:
		return nil
	default:
		panic("unreachable")
	}
}

func (c *checker) newEscapeBody(d Decl) *escapeBody {
	return &escapeBody{
		aliases: map[*resolve.Symbol]bool{},
		danger:  map[*resolve.Symbol]bool{},
		forward: map[Expr]bool{},
	}
}

// inferLetLambdaEscapes runs the parameter-escape inference of a
// declared function on a `let f = fn ...` binding: the lambda's body
// is walked in the producing position, and each parameter landing in
// `danger` flips the corresponding bit of sym.Escapes. Flags only
// ever grow — the monotone fixpoint's other sources keep filling
// theirs on the next pass. The return value reports growth.
//
// A let-bound callee used to default to "escaping" at every call
// site: a local helper like `let apply = fn f -> f ()` forced its
// callers to inline it or trip E0419 on a scope-tainted argument.
func (c *checker) inferLetLambdaEscapes(sym *resolve.Symbol, lam *Lambda) bool {
	if len(lam.Params) == 0 {
		return false
	}
	if len(sym.Escapes) < len(lam.Params) {
		grown := make([]bool, len(lam.Params))
		copy(grown, sym.Escapes)
		sym.Escapes = grown
	}
	b := c.newEscapeBody(nil)
	c.escWalk(lam.Body, true, b, false)
	changed := false
	for i, p := range lam.Params {
		for _, ps := range patBoundSyms(p.Pat) {
			if b.danger[ps] && !sym.Escapes[i] {
				sym.Escapes[i] = true
				changed = true
			}
		}
	}
	return changed || b.flagsChanged
}

// propagateAliasEscapes copies parameter-escape flags through a
// `let f = g` alias: the bound symbol names the very callable g
// resolved to, so g's proven positions hold for f as well — a
// let-bound alias used to default to "escaping" at every call
// site and re-raised E0419 on arguments the original accepted.
// Flags only ever grow, the monotone fixpoint's discipline.
func (c *checker) propagateAliasEscapes(sym *resolve.Symbol, v Expr) bool {
	var src *resolve.Symbol
	if x, ok := v.(*Var); ok {
		src = x.Sym
	} else if x, ok := v.(*MemberExpr); ok {
		src = x.Sym
	}
	if src == nil || len(src.Escapes) == 0 {
		return false
	}
	dstF := c.funcOf(sym)
	srcF := c.funcOf(src)
	if dstF == nil || srcF == nil {
		return false
	}
	// An annotated alias may declare the callee's labeled parameters
	// in a different order — unifyFunc pairs them by name, so flags
	// must travel by the same pairing, not by position.
	m := aliasParamMap(dstF.Params, srcF.Params)
	n := len(dstF.Params)
	if len(sym.Escapes) < n {
		grown := make([]bool, n)
		copy(grown, sym.Escapes)
		sym.Escapes = grown
	}
	changed := false
	for j := 0; j < n; j++ {
		k := m[j]
		esc := true
		if k >= 0 && k < len(src.Escapes) {
			esc = src.Escapes[k]
		}
		if esc && !sym.Escapes[j] {
			sym.Escapes[j] = true
			changed = true
		}
	}
	return changed
}

// funcOf returns the callable signature of a symbol, or nil when it
// is not function-typed.
func (c *checker) funcOf(sym *resolve.Symbol) *types.Func {
	if t, ok := c.vals[sym]; ok {
		f, _ := types.Chase(t).(*types.Func)
		return f
	}
	if c.src != nil {
		if sg, ok := c.src.SignatureOf(sym); ok {
			f, _ := types.Chase(sg.Type).(*types.Func)
			return f
		}
	}
	return nil
}

// aliasParamMap pairs two parameter lists the way unifyFunc pairs
// them: unlabeled parameters in order of appearance, labeled ones by
// name. dst index j answers the matching src index, or -1.
func aliasParamMap(dst, src []types.FuncParam) []int {
	m := make([]int, len(dst))
	for j := range m {
		m[j] = -1
	}
	var dstFree, srcFree []int
	for j, p := range dst {
		if p.Label == "" {
			dstFree = append(dstFree, j)
		}
	}
	for i, p := range src {
		if p.Label == "" {
			srcFree = append(srcFree, i)
		}
	}
	for x := 0; x < len(dstFree) && x < len(srcFree); x++ {
		m[dstFree[x]] = srcFree[x]
	}
	byLabel := map[string]int{}
	for i, p := range src {
		if p.Label != "" {
			if _, dup := byLabel[p.Label]; !dup {
				byLabel[p.Label] = i
			}
		}
	}
	for j, p := range dst {
		if p.Label != "" {
			if i, ok := byLabel[p.Label]; ok {
				m[j] = i
			}
		}
	}
	return m
}

// fnDeclOf resolves a top-level function symbol to its checked
// declaration, so a scope runner's named body argument can be read
// precisely. Imported functions have no local body — the caller
// falls back to the produced value's type.
func (c *checker) fnDeclOf(sym *resolve.Symbol) *FnDecl {
	for _, d := range c.decls {
		if f, ok := d.(*FnDecl); ok && f.Sym == sym {
			return f
		}
	}
	return nil
}

// isFuncType reports whether the expression's type is a function —
// an under-applied unknown callee produces one that retains the
// arguments already supplied.
func isFuncType(t types.Type) bool {
	_, ok := types.Chase(t).(*types.Func)
	return ok
}

// containsFunc reports whether a function type occurs anywhere
// inside t — a call producing t may carry a closure.
func containsFunc(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Meta, *types.Var:
		return false
	case *types.Func:
		return true
	case *types.Tuple:
		for _, e := range t.Elems {
			if containsFunc(e) {
				return true
			}
		}
		return false
	case *types.Con:
		for _, a := range t.Args {
			if containsFunc(a) {
				return true
			}
		}
		return false
	case *types.Go:
		for _, a := range t.Args {
			if containsFunc(a) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// isScopeRunner reports whether the call is one of the two scope
// runners — `Scope.run` / `Scope.run_result` — which return their
// body argument's produced value as the call's own result.
func isScopeRunner(call CallInfo) bool {
	s := call.Callee
	return s != nil && s.IsGo && (s.Name == "ScopeRun" || s.Name == "ScopeRunResult")
}

// patBoundSyms collects the symbols a typed pattern binds.
func patBoundSyms(p Pat) []*resolve.Symbol {
	var out []*resolve.Symbol
	var walk func(p Pat)
	walk = func(p Pat) {
		if p == nil {
			return
		}
		switch p := p.(type) {
		case *VarPat:
			if p.Sym != nil {
				out = append(out, p.Sym)
			}
		case *AsPat:
			if p.Sym != nil {
				out = append(out, p.Sym)
			}
			walk(p.Pat)
		case *CtorPat:
			for _, a := range p.Args {
				walk(a.Pat)
			}
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *TuplePat:
			for _, e := range p.Elems {
				walk(e)
			}
		case *RecordPat:
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *ListPat:
			for _, e := range p.Elems {
				walk(e)
			}
			walk(p.Rest)
		case *OrPat:
			for _, a := range p.Alts {
				walk(a)
			}
		case *WildPat, *LitPat:
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return out
}

// callParamEscapes reports whether the i-th argument slot of a call
// may retain its argument. Go callees, unknown callees, and variant
// constructors always escape; a Gom callee's Scope-typed parameter
// is non-escaping by construction; other positions consult the
// inferred/annotated flag.
func (c *checker) callParamEscapes(call CallInfo, i int) bool {
	p := i
	if call.ArgToParam != nil && i < len(call.ArgToParam) && call.ArgToParam[i] >= 0 {
		p = call.ArgToParam[i]
	}
	if call.Kind == CallUnknown {
		return true
	}
	sym := call.Callee
	if sym == nil || sym.Kind == resolve.KindCtor {
		return true
	}
	return c.symParamEscapes(sym, p)
}

// argEscapes answers the same question for an Apply node: a
// CallUnknown through a `let`-bound variable still resolves to a
// symbol whose inferred flags the walk can consult — CallUnknown
// means "no declared callee", not "uninspectable".
func (c *checker) argEscapes(e *Apply, i int) bool {
	if e.Call.Kind == CallUnknown {
		var sym *resolve.Symbol
		if v, ok := e.Fn.(*Var); ok {
			sym = v.Sym
		} else if v, ok := e.Fn.(*MemberExpr); ok {
			sym = v.Sym
		}
		if sym == nil {
			return true
		}
		// CallUnknown still maps reordered labeled arguments —
		// read flags by parameter position, not argument order.
		p := i
		if e.Call.ArgToParam != nil && i < len(e.Call.ArgToParam) && e.Call.ArgToParam[i] >= 0 {
			p = e.Call.ArgToParam[i]
		}
		return c.symParamEscapes(sym, p)
	}
	return c.callParamEscapes(e.Call, i)
}

// calleeSymOfApply resolves the function symbol a call applies —
// `e.Call.Callee` for classified calls, the fn expression's own
// symbol for CallUnknown (a let-bound variable may still name an
// inspectable declaration).
func calleeSymOfApply(e *Apply) *resolve.Symbol {
	if e.Call.Callee != nil {
		return e.Call.Callee
	}
	if v, ok := e.Fn.(*Var); ok {
		return v.Sym
	}
	if v, ok := e.Fn.(*MemberExpr); ok {
		return v.Sym
	}
	return nil
}

// argParamIndex maps an argument position to the flattened
// parameter it fills — CallUnknown still maps reordered labeled
// arguments, so flags read by parameter position, not argument
// order.
func argParamIndex(e *Apply, i int) int {
	if e.Call.ArgToParam != nil && i < len(e.Call.ArgToParam) && e.Call.ArgToParam[i] >= 0 {
		return e.Call.ArgToParam[i]
	}
	return i
}

// resultCarriesArg reports whether argument i's products can ride
// out through the call's own result — the missing channel of the
// flag model: `f` non-escaping only means the callee does not keep
// the argument, not that it does not return what the argument
// produces. The callee's ResultTaint flags answer the question:
// `f 3` returns f's product; `const_f` discarding f does not.
// Locally-declared callees get precise inferred flags
// (transitively, through the callees they call); imported ones get
// `//gom:result-notaint` annotations, and anything else — Go, or
// a non-symbol callee — has no inspectable answer, so it is
// conservative.
func (c *checker) resultCarriesArg(e *Apply, i int) bool {
	return c.resultTaintAt(calleeSymOfApply(e), argParamIndex(e, i))
}

// resultCarriesPipe is resultCarriesArg for a pipe: the piped value
// lands on the callee's first parameter. A non-symbol callee has no
// inspectable answer — the answer is conservative.
func (c *checker) resultCarriesPipe(fn Expr) bool {
	var sym *resolve.Symbol
	if v, ok := fn.(*Var); ok {
		sym = v.Sym
	} else if v, ok := fn.(*MemberExpr); ok {
		sym = v.Sym
	}
	return c.resultTaintAt(sym, 0)
}

// resultTaintAt reads one flattened parameter position of a
// callee's result-taint flags — an absent or short flag slice
// means "assume tainted", the conservative default.
func (c *checker) resultTaintAt(sym *resolve.Symbol, p int) bool {
	if sym == nil {
		return true
	}
	t := c.resultTaintOf(sym)
	if p < len(t) {
		return t[p]
	}
	return true
}

// resultTaintOf returns sym's per-parameter result-taint flags —
// sym.ResultTaint when already filled (an earlier inference, or a
// `//gom:result-notaint` directive on an imported signature), and
// otherwise an inference over the inspectable body: a top-level
// `fn` or a top-level `let f = fn` lambda. A callee still being
// analyzed (mutual recursion) and one without an inspectable body
// answer nil — "assume tainted".
func (c *checker) resultTaintOf(sym *resolve.Symbol) []bool {
	if c.resultTaintMemo == nil {
		c.resultTaintMemo = map[*resolve.Symbol][]bool{}
		c.resultTaintDoing = map[*resolve.Symbol]bool{}
	}
	if t, ok := c.resultTaintMemo[sym]; ok {
		return t
	}
	if len(sym.ResultTaint) > 0 {
		return sym.ResultTaint
	}
	if c.resultTaintDoing[sym] {
		return nil
	}
	var body Expr
	var pats []Pat
	if d := c.fnDeclOf(sym); d != nil && d.Body != nil && len(d.Params) > 0 {
		body = d.Body
		for _, p := range d.Params {
			pats = append(pats, p.Pat)
		}
	} else if lam := c.letLambdaOf(sym); lam != nil {
		body = lam.Body
		for _, p := range lam.Params {
			pats = append(pats, p.Pat)
		}
	}
	if body == nil {
		c.resultTaintMemo[sym] = nil
		return nil
	}
	c.resultTaintDoing[sym] = true
	t := c.inferResultTaint(body, pats)
	delete(c.resultTaintDoing, sym)
	sym.ResultTaint = t
	c.resultTaintMemo[sym] = t
	return t
}

// letLambdaOf resolves a top-level `let f = fn ...` binding's
// symbol to its lambda — like fnDeclOf for declared functions, it
// is the inspectable-body side of a named callee.
func (c *checker) letLambdaOf(sym *resolve.Symbol) *Lambda {
	for _, d := range c.decls {
		ld, ok := d.(*LetDecl)
		if !ok {
			continue
		}
		for _, s := range patBoundSyms(ld.Pat) {
			if s == sym {
				lam, _ := ld.Value.(*Lambda)
				return lam
			}
		}
	}
	return nil
}

// inferResultTaint marks the parameters whose products the body's
// result may carry.
func (c *checker) inferResultTaint(body Expr, pats []Pat) []bool {
	prod := c.resultProducers(body)
	t := make([]bool, len(pats))
	for i, pat := range pats {
		for _, s := range patBoundSyms(pat) {
			if prod[s] {
				t[i] = true
			}
		}
	}
	return t
}

// resultProducers returns the symbols whose products e's result
// value may carry — the producer-channel form of retainedVars,
// transitive through callees' ResultTaint flags. The
// walk is the same value-retention one (result positions only):
// constructors, partial and hole applications, and function-valued
// pipes retain their inputs structurally; a saturated call is its
// callee's product — so the callee itself rides — and carries its
// argument's products exactly where the callee's ResultTaint
// marks the parameter position. A callee without flags answers
// "assume tainted", so uninspectable callees stay conservative;
// mutual recursion bottoms out the same way.
func (c *checker) resultProducers(e Expr) map[*resolve.Symbol]bool {
	var out map[*resolve.Symbol]bool
	var rec func(e Expr)
	rec = func(e Expr) {
		add := func(s *resolve.Symbol) {
			if s == nil {
				return
			}
			if out == nil {
				out = map[*resolve.Symbol]bool{}
			}
			out[s] = true
		}
		switch e := e.(type) {
		case nil:
		case *Var:
			add(e.Sym)
		case *MemberExpr:
			add(e.Sym)
		case *Lambda:
			for s := range capturedVars(e.Body, lamBound(e)) {
				add(s)
			}
		case *Apply:
			ctor := e.Call.Callee != nil && e.Call.Callee.Kind == resolve.KindCtor
			producesClosure := e.Call.Kind == CallPartial || e.Call.HoleIndex >= 0 ||
				(e.Call.Kind == CallUnknown && isFuncType(e.Type()))
			switch {
			case ctor || producesClosure:
				rec(e.Fn)
				for _, a := range e.Args {
					rec(a.Value)
				}
			case isScopeRunner(e.Call):
				rec(e.Fn)
				for _, a := range e.Args {
					if lam, ok := a.Value.(*Lambda); ok {
						rec(lam.Body)
					} else {
						rec(a.Value)
					}
				}
			default:
				rec(e.Fn)
				for i, a := range e.Args {
					if c.resultTaintAt(calleeSymOfApply(e), argParamIndex(e, i)) {
						rec(a.Value)
					}
				}
			}
		case *Ascribe:
			rec(e.X)
		case *If:
			rec(e.Then)
			rec(e.Else)
		case *Match:
			for _, a := range e.Arms {
				rec(a.Body)
			}
		case *Block:
			if n := len(e.Stmts); n > 0 {
				rec(e.Stmts[n-1])
			}
		case *Try:
			rec(e.X)
		case *ListLit:
			for _, el := range e.Elems {
				rec(el)
			}
		case *TupleLit:
			for _, el := range e.Elems {
				rec(el)
			}
		case *RecordLit:
			rec(e.With)
			for _, f := range e.Fields {
				rec(f.Value)
			}
		case *Spread:
			rec(e.X)
		case *SelectExpr:
			for _, a := range e.Arms {
				switch a := a.(type) {
				case *RecvArm:
					rec(a.Body)
				case *SendArm:
					rec(a.Body)
				case *DefaultArm:
					rec(a.Body)
				default:
					panic("unreachable")
				}
			}
		case *Binary:
			switch e.Op {
			case syntax.BinPipeFwd, syntax.BinPipeBack, syntax.BinPipeTry:
				arg, fn := e.X, e.Y
				if e.Op == syntax.BinPipeBack {
					arg, fn = e.Y, e.X
				}
				var fnSym *resolve.Symbol
				if f, ok := fn.(*Var); ok {
					fnSym = f.Sym
				} else if f, ok := fn.(*MemberExpr); ok {
					fnSym = f.Sym
				}
				switch {
				case isScopeRunner(CallInfo{Callee: fnSym}):
					rec(fn)
					if lam, ok := arg.(*Lambda); ok {
						rec(lam.Body)
					} else {
						rec(arg)
					}
				case isFuncType(e.Type()):
					// A function-valued result is a partial
					// application capturing both operands.
					rec(e.X)
					rec(e.Y)
				default:
					// A saturated pipe's result is the callee's
					// product — `x |> f` carries f, and x's own
					// products where f's first parameter taints.
					// `|?>`'s failure arm forwards the argument's
					// error product unconditionally.
					rec(fn)
					if e.Op == syntax.BinPipeTry || c.resultTaintAt(fnSym, 0) {
						rec(arg)
					}
				}
			case syntax.BinComposeFwd, syntax.BinComposeBack:
				rec(e.X)
				rec(e.Y)
			}
		case *Lit, *Hole, *Let, *Ignore, *Unary, *FieldAssign,
			*For, *GoExpr, *StrInterp:
			// Consumed positions and statement results retain
			// nothing — mirror retainedVarsR.
		default:
			panic("unreachable")
		}
	}
	rec(e)
	return out
}

// symParamEscapes reads one parameter position of a Gom callee: a
// `Scope`-typed parameter is non-escaping by construction — the
// callee's own compile enforced it — and other positions consult
// the inferred/annotated flag. The signature is looked up quietly
// (the use site already ran sigOf's diagnostics).
func (c *checker) symParamEscapes(sym *resolve.Symbol, p int) bool {
	var sig *types.Func
	if t, ok := c.vals[sym]; ok {
		sig, _ = types.Chase(t).(*types.Func)
	} else if c.src != nil {
		if sg, ok := c.src.SignatureOf(sym); ok {
			sig, _ = types.Chase(sg.Type).(*types.Func)
		}
	}
	if sig != nil {
		if p >= len(sig.Params) {
			return true
		}
		if isScopeType(sig.Params[p].Type) {
			return false
		}
	}
	if p < len(sym.Escapes) {
		return sym.Escapes[p]
	}
	return true
}

// escWalk walks e: `danger` is true when a tainted variable used in
// this subtree escapes. Transparent wrappers (blocks, if/match
// branches, try) propagate it; call argument slots re-derive it from
// parameter flags; bindings and consumed positions reset it.
func (c *checker) escWalk(e Expr, danger bool, b *escapeBody, report bool) {
	if e == nil {
		return
	}
	switch e := e.(type) {
	case *Lit, *Hole:
	case *Var:
		if !danger {
			return
		}
		if b.tainted(e.Sym) || isScopeType(e.Type()) {
			if report {
				c.diags.Add(diag.Error("E0419",
					fmt.Sprintf("`%s` is a `Scope` value (or closes over one) and escapes its scope here", e.Sym.Name)).
					At(e.Sp))
			}
		}
		b.markDanger(e.Sym)
	case *Apply:
		if m, ok := e.Fn.(*MemberExpr); ok && m.Sym != nil && m.Sym.Kind == resolve.KindMethod {
			// A method call's receiver is its implicit first
			// argument — Go methods retain it (CallGo), Gom
			// methods follow the flat-parameter flags.
			c.escWalk(m.Recv, danger || e.Call.Kind == CallGo, b, report)
		} else {
			c.escWalk(e.Fn, false, b, report)
		}
		ctor := e.Call.Callee != nil && e.Call.Callee.Kind == resolve.KindCtor
		// A partial or hole application produces a closure that
		// retains every supplied argument; a CallUnknown whose
		// result is still a function is the same shape — a curried
		// step that binds arguments for a later call. Supplied
		// arguments escape when the call's own result does.
		producesClosure := e.Call.Kind == CallPartial || e.Call.HoleIndex >= 0 ||
			(e.Call.Kind == CallUnknown && isFuncType(e.Type()))
		for i, a := range e.Args {
			// The call's own position does not leak into its
			// arguments: an argument escapes only when the callee
			// retains that parameter (or the construction stores
			// it) — or when the call produces a closure that
			// captures it.
			d := ctor || c.argEscapes(e, i) || (producesClosure && danger)
			if danger && isScopeRunner(e.Call) {
				if lam, ok := unascribe(a.Value).(*Lambda); ok {
					b.forward[lam] = true
				} else if c.runnerArgTainted(a.Value, b, nil) && report {
					// An opaque body argument — a function
					// reference or a call — produces a value
					// the runner forwards out of the scope
					// unchecked. When that value can carry a
					// scope, escaping here is an error.
					c.scopeEscapeErr(a.Value.Span())
				}
			} else if danger && !d && containsFunc(e.Type()) &&
				c.resultCarriesArg(e, i) {
				// The call's result may carry this argument's
				// products: `run_cb (fn x -> fn () -> s)`
				// lets the inner closure ride out through
				// run_cb's result even though the argument
				// lambda itself is consumed. Walk the
				// argument's result positions at danger —
				// the lambda itself is not escaping, so its
				// own captures stay quiet.
				if lam, ok := unascribe(a.Value).(*Lambda); ok {
					b.forward[lam] = true
				}
			}
			c.escWalk(a.Value, d, b, report)
		}
		// A constructor application stores its arguments: a
		// result type containing Scope means a slot held it.
		if ctor && c.containsScope(e.Type()) {
			if report {
				c.scopeEscapeErr(e.Sp)
			}
		}
	case *MemberExpr:
		c.escWalk(e.Recv, danger, b, report)
	case *Lambda:
		// An escaping closure retains every variable it
		// captured — whatever the type, so parameter inference
		// marks all of them; only Scope-tainted ones report.
		// The body's tail keeps the escaping context (plus the
		// scope-runner forwarding mark): a closure the body
		// produces escapes with it.
		if danger {
			for sym, v := range capturedVars(e.Body, lamBound(e)) {
				b.markDanger(sym)
				if report && (isScopeType(v.Type()) || b.aliases[sym]) {
					c.diags.Add(diag.Error("E0419",
						fmt.Sprintf("`%s` is a `Scope` value (or closes over one) captured by a closure that escapes its scope", sym.Name)).
						At(v.Sp))
				}
			}
		}
		c.escWalk(e.Body, danger || b.forward[e], b, report)
		if c.containsScope(e.Type()) && !c.scopeParamOK(e.Type()) {
			// A lambda whose type mentions Scope outside
			// argument positions — e.g. `fn () -> s` — produces
			// the value.
			if report {
				c.scopeEscapeErr(e.Sp)
			}
		}
	case *If:
		c.escWalk(e.Cond, false, b, report)
		c.escWalk(e.Then, danger, b, report)
		c.escWalk(e.Else, danger, b, report)
	case *Match:
		c.escWalk(e.Subj, false, b, report)
		// A pattern-bound name aliases (part of) the subject —
		// an escaping use through a `let` alias or an arm's own
		// result must still land on the subject's retained names.
		subjRV := retainedVars(e.Subj)
		for _, a := range e.Arms {
			b.bindAlias(patBoundSyms(a.Pat), subjRV)
			c.escWalk(a.Guard, danger, b, report)
			c.escWalk(a.Body, danger, b, report)
		}
	case *Block:
		saved := b.lets
		b.lets = nil
		if len(saved) > 0 {
			b.lets = make(map[*resolve.Symbol]Expr, len(saved)+len(e.Stmts))
			for s, v := range saved {
				b.lets[s] = v
			}
		}
		for i, s := range e.Stmts {
			tail := i == len(e.Stmts)-1
			c.escWalk(s, danger && tail, b, report)
			if l, ok := s.(*Let); ok {
				c.bindLet(l, b)
				for _, s := range patBoundSyms(l.Pat) {
					if b.lets == nil {
						b.lets = map[*resolve.Symbol]Expr{}
					}
					b.lets[s] = l.Value
					if !report {
						if lam, isLam := unascribe(l.Value).(*Lambda); isLam && c.inferLetLambdaEscapes(s, lam) {
							b.flagsChanged = true
						}
						if c.propagateAliasEscapes(s, l.Value) {
							b.flagsChanged = true
						}
					}
				}
			}
		}
		b.lets = saved
	case *Let:
		c.escWalk(e.Value, false, b, report)
		c.bindLet(e, b)
		for _, s := range patBoundSyms(e.Pat) {
			if b.lets == nil {
				b.lets = map[*resolve.Symbol]Expr{}
			}
			b.lets[s] = e.Value
			if !report {
				if lam, isLam := unascribe(e.Value).(*Lambda); isLam && c.inferLetLambdaEscapes(s, lam) {
					b.flagsChanged = true
				}
				if c.propagateAliasEscapes(s, e.Value) {
					b.flagsChanged = true
				}
			}
		}
	case *Ignore:
		c.escWalk(e.X, false, b, report)
	case *Try:
		c.escWalk(e.X, danger, b, report)
	case *FieldAssign:
		c.escWalk(e.Recv, danger, b, report)
		c.escWalk(e.Value, true, b, report)
	case *GoExpr:
		// A `go` block retains its captures for the goroutine's
		// lifetime — an outliving goroutine is the escaping
		// closure case with no produced value.
		for sym, v := range capturedVars(e.Body, map[*resolve.Symbol]bool{}) {
			b.markDanger(sym)
			if report && (isScopeType(v.Type()) || b.aliases[sym]) {
				c.diags.Add(diag.Error("E0419",
					fmt.Sprintf("`%s` is a `Scope` value (or closes over one) captured by a `go` block that escapes its scope", sym.Name)).
					At(v.Sp))
			}
		}
		c.escWalk(e.Body, false, b, report)
	case *For:
		c.escWalk(e.Iter, false, b, report)
		// The bound name aliases each iterated element — an
		// escaping use reaches back to the iterable.
		b.bindAlias(patBoundSyms(e.Pat), retainedVars(e.Iter))
		c.escWalk(e.Body, danger, b, report)
	case *SelectExpr:
		for _, a := range e.Arms {
			switch a := a.(type) {
			case *RecvArm:
				c.escWalk(a.Chan, false, b, report)
				b.bindAlias(patBoundSyms(a.Bind), retainedVars(a.Chan))
				c.escWalk(a.Body, danger, b, report)
			case *SendArm:
				c.escWalk(a.Chan, false, b, report)
				c.escWalk(a.Value, true, b, report)
				c.escWalk(a.Body, danger, b, report)
			case *DefaultArm:
				c.escWalk(a.Body, danger, b, report)
			default:
				panic("unreachable")
			}
		}
	case *ListLit:
		for _, el := range e.Elems {
			c.escWalk(el, true, b, report)
		}
		if c.containsScope(e.Type()) && report {
			c.scopeEscapeErr(e.Sp)
		}
	case *TupleLit:
		for _, el := range e.Elems {
			c.escWalk(el, true, b, report)
		}
		if c.containsScope(e.Type()) && report {
			c.scopeEscapeErr(e.Sp)
		}
	case *Spread:
		c.escWalk(e.X, danger, b, report)
	case *RecordLit:
		c.escWalk(e.With, danger, b, report)
		for _, f := range e.Fields {
			c.escWalk(f.Value, true, b, report)
		}
		if c.containsScope(e.Type()) && report {
			c.scopeEscapeErr(e.Sp)
		}
	case *Ascribe:
		c.escWalk(e.X, danger, b, report)
	case *Unary:
		c.escWalk(e.X, false, b, report)
	case *Binary:
		switch e.Op {
		case syntax.BinPipeFwd, syntax.BinPipeBack, syntax.BinPipeTry:
			// `x |> f` / `f <| x` / `x |?> f` — the pipe applies
			// fn to arg; check keeps the operands as a Binary.
			// A partial-application callee retains the piped
			// argument too, so it walks under the pipe's own
			// position.
			arg, fn := e.X, e.Y
			if e.Op == syntax.BinPipeBack {
				arg, fn = e.Y, e.X
			}
			_, fnIsApply := fn.(*Apply)
			c.escWalk(fn, fnIsApply && danger, b, report)
			d := c.pipeParamEscapes(fn) || (fnIsApply && danger)
			if danger && !d && containsFunc(e.Type()) && c.resultCarriesPipe(fn) {
				// The pipe's result may carry the argument's
				// products — `Scope.run <| fn s -> mk ()`
				// forwards a closure the body produced. Walk
				// a lambda argument's result positions at
				// danger; the lambda itself is not escaping.
				if lam, ok := unascribe(arg).(*Lambda); ok {
					b.forward[lam] = true
				}
			}
			c.escWalk(arg, d, b, report)
		case syntax.BinComposeFwd, syntax.BinComposeBack:
			// `f >> g` / `f << g` produce a closure retaining
			// both operands — they escape with the composition.
			c.escWalk(e.X, danger, b, report)
			c.escWalk(e.Y, danger, b, report)
		default:
			c.escWalk(e.X, false, b, report)
			c.escWalk(e.Y, false, b, report)
		}
	case *StrInterp:
		for _, p := range e.Parts {
			switch p := p.(type) {
			case *StrExpr:
				c.escWalk(p.X, false, b, report)
			case *StrText:
			default:
				panic("unreachable")
			}
		}
	default:
		panic("unreachable")
	}
}

// bindLet registers a `let`'s bound symbols as tainted when the
// bound value can carry a live Scope — a lambda closing over one,
// or an alias of a tainted variable.
func (c *checker) bindLet(l *Let, b *escapeBody) {
	if c.exprTainted(l.Value, b) {
		for _, s := range patBoundSyms(l.Pat) {
			b.aliases[s] = true
		}
	}
	// A bound name carries the variables its value's result
	// retains — markDanger follows these so an escaping use lands
	// on the source parameter, not only on the alias.
	b.bindAlias(patBoundSyms(l.Pat), retainedVars(l.Value))
}

// bindAlias records that each dst symbol's value retains the src
// symbols — markDanger's breadth-first walk follows the edge.
func (b *escapeBody) bindAlias(dsts []*resolve.Symbol, src map[*resolve.Symbol]bool) {
	if len(dsts) == 0 || len(src) == 0 {
		return
	}
	syms := make([]*resolve.Symbol, 0, len(src))
	for s := range src {
		syms = append(syms, s)
	}
	if b.bindOf == nil {
		b.bindOf = map[*resolve.Symbol][]*resolve.Symbol{}
	}
	for _, s := range dsts {
		b.bindOf[s] = syms
	}
}

// retainedVars returns the variables an escaping use of e's
// result carries — the names its value retains. Unlike
// capturedVars it only reads result positions: a full
// application's callee and arguments are consumed by the call
// itself (`let x = f ()` does not retain f — the callee's escape
// flags already mark whatever it keeps), so only partial/hole
// applications, constructors, and function-valued pipes count.
// Pattern-bound and inner-let names may leak in; they are inert
// for parameters and the inner let's own bindOf entry continues
// the chain.
func retainedVars(e Expr) map[*resolve.Symbol]bool {
	return retainedVarsR(e)
}

func retainedVarsR(e Expr) map[*resolve.Symbol]bool {
	var out map[*resolve.Symbol]bool
	var rec func(e Expr)
	rec = func(e Expr) {
		add := func(s *resolve.Symbol) {
			if s == nil {
				return
			}
			if out == nil {
				out = map[*resolve.Symbol]bool{}
			}
			out[s] = true
		}
		switch e := e.(type) {
		case nil:
		case *Var:
			add(e.Sym)
		case *MemberExpr:
			// The selected member's value is the result; the
			// receiver itself is read, not retained.
			add(e.Sym)
		case *Lambda:
			for s := range capturedVars(e.Body, lamBound(e)) {
				add(s)
			}
		case *Apply:
			ctor := e.Call.Callee != nil && e.Call.Callee.Kind == resolve.KindCtor
			producesClosure := e.Call.Kind == CallPartial || e.Call.HoleIndex >= 0 ||
				(e.Call.Kind == CallUnknown && isFuncType(e.Type()))
			switch {
			case ctor || producesClosure:
				rec(e.Fn)
				for _, a := range e.Args {
					rec(a.Value)
				}
			case isScopeRunner(e.Call):
				// A scope runner returns its body argument's
				// produced value — the body's result position
				// is retained by the call's own result.
				rec(e.Fn)
				for _, a := range e.Args {
					if lam, ok := unascribe(a.Value).(*Lambda); ok {
						rec(lam.Body)
					} else {
						rec(a.Value)
					}
				}
			}
		case *Ascribe:
			rec(e.X)
		case *If:
			rec(e.Then)
			rec(e.Else)
		case *Match:
			for _, a := range e.Arms {
				rec(a.Body)
			}
		case *Block:
			if n := len(e.Stmts); n > 0 {
				rec(e.Stmts[n-1])
			}
		case *Try:
			rec(e.X)
		case *ListLit:
			for _, el := range e.Elems {
				rec(el)
			}
		case *TupleLit:
			for _, el := range e.Elems {
				rec(el)
			}
		case *RecordLit:
			rec(e.With)
			for _, f := range e.Fields {
				rec(f.Value)
			}
		case *Spread:
			rec(e.X)
		case *SelectExpr:
			for _, a := range e.Arms {
				switch a := a.(type) {
				case *RecvArm:
					rec(a.Body)
				case *SendArm:
					rec(a.Body)
				case *DefaultArm:
					rec(a.Body)
				default:
					panic("unreachable")
				}
			}
		case *Binary:
			switch e.Op {
			case syntax.BinPipeFwd, syntax.BinPipeBack, syntax.BinPipeTry:
				arg, fn := e.X, e.Y
				if e.Op == syntax.BinPipeBack {
					arg, fn = e.Y, e.X
				}
				var fnSym *resolve.Symbol
				if f, ok := fn.(*Var); ok {
					fnSym = f.Sym
				} else if f, ok := fn.(*MemberExpr); ok {
					fnSym = f.Sym
				}
				switch {
				case isScopeRunner(CallInfo{Callee: fnSym}):
					// The runner forwards its body argument's
					// produced value — the body's result
					// position is retained.
					rec(fn)
					if lam, ok := unascribe(arg).(*Lambda); ok {
						rec(lam.Body)
					} else {
						rec(arg)
					}
				case isFuncType(e.Type()):
					// A function-valued result is a partial
					// application capturing both operands.
					rec(e.X)
					rec(e.Y)
				}
			case syntax.BinComposeFwd, syntax.BinComposeBack:
				rec(e.X)
				rec(e.Y)
			}
		case *Lit, *Hole, *Let, *Ignore, *Unary, *FieldAssign,
			*For, *GoExpr, *StrInterp:
			// Consumed positions and statement results retain
			// nothing — a `let` statement's own bindings alias
			// through its bindOf entry instead.
		default:
			panic("unreachable")
		}
	}
	rec(e)
	return out
}

// lamBound collects the symbols a lambda's own parameter patterns
// bind — the starting bound set for capturedVars.
func lamBound(lam *Lambda) map[*resolve.Symbol]bool {
	bound := map[*resolve.Symbol]bool{}
	for _, p := range lam.Params {
		for _, s := range patBoundSyms(p.Pat) {
			bound[s] = true
		}
	}
	return bound
}

// capturedVars returns every variable an expression uses but binds
// outside itself — its captures — mapped to a use site. bound is
// the caller-seeded bound set (a lambda's params, empty for a `go`
// block); the walk adds every pattern the body can introduce
// (nested lambdas, lets, for/match bindings), so a var the
// expression declares itself does not count as a capture.
func capturedVars(body Expr, bound map[*resolve.Symbol]bool) map[*resolve.Symbol]*Var {
	var out map[*resolve.Symbol]*Var
	WalkExpr(body, func(y Expr) {
		switch y := y.(type) {
		case *Lambda:
			for _, p := range y.Params {
				for _, s := range patBoundSyms(p.Pat) {
					bound[s] = true
				}
			}
		case *Let:
			for _, s := range patBoundSyms(y.Pat) {
				bound[s] = true
			}
		case *For:
			for _, s := range patBoundSyms(y.Pat) {
				bound[s] = true
			}
		case *Match:
			for _, a := range y.Arms {
				for _, s := range patBoundSyms(a.Pat) {
					bound[s] = true
				}
			}
		case *Var:
			if bound[y.Sym] {
				return
			}
			if out == nil {
				out = map[*resolve.Symbol]*Var{}
			}
			if _, seen := out[y.Sym]; !seen {
				out[y.Sym] = y
			}
		case *Apply, *Ascribe, *Binary, *Block, *FieldAssign, *GoExpr,
			*Hole, *If, *Ignore, *ListLit, *Lit, *MemberExpr, *RecordLit,
			*SelectExpr, *Spread, *StrInterp, *Try, *TupleLit, *Unary:
		default:
			panic("unreachable")
		}
	})
	return out
}

// pipeParamEscapes reports whether the operand a pipe feeds into a
// callee may be retained — `x |> f`'s f gets x as an argument. The
// pipe node records no CallInfo, so the callee's first parameter's
// rule applies directly; a partial-application callee's next open
// slot is ambiguous under labeled arguments, and a callee that is
// not a named symbol gets the conservative answer.
func (c *checker) pipeParamEscapes(fn Expr) bool {
	switch f := fn.(type) {
	case *Var:
		return c.callParamEscapes(CallInfo{Callee: f.Sym}, 0)
	case *MemberExpr:
		return c.callParamEscapes(CallInfo{Callee: f.Sym}, 0)
	case *Lambda:
		// An immediately-applied literal cannot retain — its
		// parameter bindings die with the call.
		return false
	case *Apply, *Ascribe, *Binary, *Block, *FieldAssign, *For,
		*GoExpr, *Hole, *If, *Ignore, *Let, *ListLit, *Lit, *Match,
		*RecordLit, *SelectExpr, *Spread, *StrInterp, *Try, *TupleLit,
		*Unary:
		// An *Apply callee is a partial application appending the
		// piped value to an already-retaining closure; anything
		// else cannot name a callee's parameter — the conservative
		// answer.
		return true
	default:
		panic("unreachable")
	}
}

// exprTainted reports whether the VALUE e produces can carry a live
// Scope into a binding: the expression's type is Scope, the value is
// or contains a lambda that closes over a tainted variable, or an
// under-application's produced closure retains a tainted argument.
// The walk follows only positions that propagate the produced value —
// a lambda consumed as a saturated call's argument is not part of the
// call's result (the callee's escape rule decides its fate at the
// call site), so `let y = helper p` binding the Int `y` is clean.
func (c *checker) exprTainted(e Expr, b *escapeBody) bool {
	return c.exprTaintedL(e, b, nil)
}

// runnerArgTainted reads a scope runner's body argument — the call
// produces whatever the body produces. A literal lambda's tail is
// inspectable directly; a let-bound lambda resolves through locals;
// a declared function resolves to its own body's produced value.
// Anything else is opaque, so the produced value's type decides — a
// scope or function result can carry a live scope out of the run.
func (c *checker) runnerArgTainted(e Expr, b *escapeBody, locals map[*resolve.Symbol]Expr) bool {
	if lam, ok := unascribe(e).(*Lambda); ok {
		return c.exprTaintedL(lam.Body, b, nil)
	}
	var sym *resolve.Symbol
	switch f := e.(type) {
	case *Var:
		sym = f.Sym
		bound, ok := locals[f.Sym]
		if !ok {
			bound, ok = b.lets[f.Sym]
		}
		if ok {
			if lam, isLam := unascribe(bound).(*Lambda); isLam {
				return c.exprTaintedL(lam.Body, b, nil)
			}
		}
	case *MemberExpr:
		sym = f.Sym
	case *Apply, *Ascribe, *Binary, *Block, *FieldAssign, *For, *GoExpr,
		*Hole, *If, *Ignore, *Lambda, *Let, *ListLit, *Lit, *Match,
		*RecordLit, *SelectExpr, *Spread, *StrInterp, *Try, *TupleLit,
		*Unary:
	default:
		panic("unreachable")
	}
	if sym != nil && !b.expanding[sym] {
		if t, ok := b.declTaint[sym]; ok {
			return t
		}
		if d := c.fnDeclOf(sym); d != nil && d.Body != nil {
			if b.expanding == nil {
				b.expanding = map[*resolve.Symbol]bool{}
			}
			b.expanding[sym] = true
			t := c.exprTaintedL(d.Body, b, nil)
			delete(b.expanding, sym)
			if b.declTaint == nil {
				b.declTaint = map[*resolve.Symbol]bool{}
			}
			b.declTaint[sym] = t
			return t
		}
	}
	ret := e.Type()
	if fn, ok := types.Chase(ret).(*types.Func); ok {
		if len(fn.Params) > 1 {
			// The body's produced value is the residual arrow —
			// a closure that may carry a captured scope.
			return true
		}
		ret = fn.Ret
	}
	if isScopeType(ret) {
		return true
	}
	_, isFn := types.Chase(ret).(*types.Func)
	return isFn
}

// exprTaintedL is exprTainted with locals — a map of let-bound
// symbols visible inside e to their bound expressions, so a bound
// variable's taint resolves through its own binding when the
// expression under inspection is a scope-runner body's tail (the
// runner forwards exactly that value).
func (c *checker) exprTaintedL(e Expr, b *escapeBody, locals map[*resolve.Symbol]Expr) bool {
	if e == nil {
		// An absent else arm produces Unit — never tainted.
		return false
	}
	if isScopeType(e.Type()) {
		return true
	}
	switch x := e.(type) {
	case *Var:
		if v, ok := locals[x.Sym]; ok {
			// A bound expression's taint is stable under its own
			// locals map — chains like `let b = if .. {a} {a}`
			// would otherwise re-expand the same bindings at every
			// reference, growing exponentially with depth.
			if b.taintMemo == nil {
				b.taintMemo = map[Expr]bool{}
			}
			if t, ok := b.taintMemo[v]; ok {
				return t
			}
			t := c.exprTaintedL(v, b, locals)
			b.taintMemo[v] = t
			return t
		}
		return b.aliases[x.Sym]
	case *Lambda:
		for sym, v := range capturedVars(x.Body, lamBound(x)) {
			if isScopeType(v.Type()) || b.aliases[sym] {
				return true
			}
		}
		return false
	case *Block:
		if len(x.Stmts) == 0 {
			return false
		}
		inner := make(map[*resolve.Symbol]Expr, len(locals)+len(x.Stmts))
		for s, v := range locals {
			inner[s] = v
		}
		for _, st := range x.Stmts[:len(x.Stmts)-1] {
			if l, ok := st.(*Let); ok {
				for _, s := range patBoundSyms(l.Pat) {
					inner[s] = l.Value
				}
			}
		}
		return c.exprTaintedL(x.Stmts[len(x.Stmts)-1], b, inner)
	case *If:
		return c.exprTaintedL(x.Then, b, locals) || c.exprTaintedL(x.Else, b, locals)
	case *Match:
		for _, a := range x.Arms {
			if c.exprTaintedL(a.Body, b, locals) {
				return true
			}
		}
		return false
	case *Try:
		return c.exprTaintedL(x.X, b, locals)
	case *Ascribe:
		return c.exprTaintedL(x.X, b, locals)
	case *SelectExpr:
		for _, a := range x.Arms {
			switch a := a.(type) {
			case *RecvArm:
				if c.exprTaintedL(a.Body, b, locals) {
					return true
				}
			case *SendArm:
				if c.exprTaintedL(a.Body, b, locals) {
					return true
				}
			case *DefaultArm:
				if c.exprTaintedL(a.Body, b, locals) {
					return true
				}
			default:
				panic("unreachable")
			}
		}
		return false
	case *RecordLit:
		if x.With != nil && c.exprTaintedL(x.With, b, locals) {
			return true
		}
		for _, f := range x.Fields {
			if c.exprTaintedL(f.Value, b, locals) {
				return true
			}
		}
		return false
	case *TupleLit:
		for _, el := range x.Elems {
			if c.exprTaintedL(el, b, locals) {
				return true
			}
		}
		return false
	case *ListLit:
		for _, el := range x.Elems {
			if c.exprTaintedL(el, b, locals) {
				return true
			}
		}
		return false
	case *Spread:
		return c.exprTaintedL(x.X, b, locals)
	case *MemberExpr:
		// The field's own value propagates — tainted only when the
		// record it was read from holds a tainted closure.
		return c.exprTaintedL(x.Recv, b, locals)
	case *Apply:
		if isScopeRunner(x.Call) {
			// The runner forwards its body argument's produced
			// value — the binding's taint is that value's.
			for _, a := range x.Args {
				if c.runnerArgTainted(a.Value, b, locals) {
					return true
				}
			}
			return false
		}
		// Only a produced closure retains the callee's captures
		// and the arguments already supplied; a saturated call's
		// arguments are consumed under the callee's own escape
		// rule.
		if x.Call.Kind == CallPartial || x.Call.HoleIndex >= 0 ||
			(x.Call.Kind == CallUnknown && isFuncType(x.Type())) {
			if c.exprTaintedL(x.Fn, b, locals) {
				return true
			}
			for _, a := range x.Args {
				if c.exprTaintedL(a.Value, b, locals) {
					return true
				}
			}
		}
		return false
	case *Binary:
		switch x.Op {
		case syntax.BinComposeFwd, syntax.BinComposeBack:
			// The composed closure retains both operands.
			return c.exprTaintedL(x.X, b, locals) || c.exprTaintedL(x.Y, b, locals)
		case syntax.BinPipeFwd, syntax.BinPipeBack, syntax.BinPipeTry:
			// The pipe's produced value is the callee's result: a
			// literal callee's body tail is inspectable, anything
			// else is the callee's own value — not a capture the
			// pipe manufactures.
			fn := x.Y
			if x.Op == syntax.BinPipeBack {
				fn = x.X
			}
			if lam, ok := unascribe(fn).(*Lambda); ok {
				return c.exprTaintedL(lam.Body, b, nil)
			}
			return false
		default:
			// Arithmetic and comparisons consume their operands.
			return false
		}
	case *GoExpr, *Let, *Ignore, *For, *FieldAssign,
		*Lit, *StrInterp, *Hole, *Unary:
		// Unit producers, literals, and consumers that cannot carry
		// a closure in their produced value.
		return false
	default:
		panic("unreachable")
	}
}

func (c *checker) scopeEscapeErr(sp diag.Span) {
	c.diags.Add(diag.Error("E0419",
		"this construction would hold a `Scope` value — `Scope` cannot be stored or returned").
		At(sp))
}
