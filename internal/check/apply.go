package check

import (
	"fmt"
	"slices"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// checkApply types a juxtaposition application `f a b ~l: c` and
// attaches the validated CallInfo — the calling-convention decision
// the rest of the pipeline consumes.
//
// Classification:
//
//   - the callee resolves to a Go function or method (KindFunc /
//     KindMethod flagged IsGo) → CallGo: Go calling convention, no
//     currying; the surface argument tuple is unpacked into the
//     individual Go arguments.
//   - a known Gom callee (a declared or imported fn, a variant
//     constructor, a builtin) saturated or over-applied → CallDirect;
//     under-applied → CallPartial.
//   - anything else — a local variable holding a function, the result
//     of an expression — → CallUnknown: curried application.
//
// `Go.conv` is not an ordinary call: `Go.conv e : T` reaches the
// checker as an ascription over this node, so a bare `Go.conv e`
// reports E0507.
func (c *checker) checkApply(e *syntax.Apply, want types.Type) Expr {
	if ti, lit := c.typePrefixedRecordLit(e); ti != nil {
		rt, ok := c.typePrefixedRecordType(ti, e.Sp, want)
		if !ok {
			c.mismatch(e.Fn.Span(), "a record type", rt)
			return c.checkRecordLit(lit, nil)
		}
		return c.checkRecordLit(lit, rt)
	}
	fn, callee, goCall := c.calleeOf(e.Fn)
	if callee == nil {
		// A member selection deferred on an unresolved receiver meta
		// cannot pick a calling convention yet: `x.M(args)` may be a
		// curried Gom application or the tuple-shaped call of a Go
		// method, and pre-digesting the surface argument tuple as
		// curried application would under-record the call once the
		// member resolves to a method (the CallInfo the rest of the
		// pipeline consumes — see pendingApplies).
		if me, ok := fn.(*MemberExpr); ok && me.Sym == nil {
			if _, unresolved := types.Chase(me.Recv.Type()).(*types.Meta); unresolved {
				return c.checkDeferredApply(e, me)
			}
		}
	}
	if callee != nil && isConv(callee) {
		return c.checkConvCall(e, fn)
	}
	if callee != nil && isNil(callee) {
		return c.checkNilCall(e, fn, callee)
	}
	if callee != nil {
		if n, ok := goFuncCallArity(callee); ok {
			return c.checkGoFuncCall(e, fn, callee, n, want)
		}
		if n, ok := goFnCallArity(callee); ok {
			return c.checkGoFnCall(e, fn, callee, n)
		}
	}
	params, ret := flattenFunc(fn.Type())
	// A `Go.Any` element of an expected composite is the
	// constructor's instantiation answer: pin it before the
	// arguments check so the payload conforms into the `any`
	// slot — `Ok v` under `Result Go.Any GoError` builds
	// `Result[any, error]` (spec 8.1.7's box reaches generic
	// element positions; `Result[int64, error]` is not
	// `Result[any, error]`, Go generics are invariant). Only the
	// unsolved metas facing a `Go.Any` argument pin here — a
	// concrete mismatch stays the call's own conformance
	// diagnostic rather than reporting twice. `containsGoAny`
	// nests the answer one constructor deep: `Some (Some 42)`
	// under `Option (Option Go.Any)` pins the outer meta to
	// `Option Go.Any` so the inner apply's pin chains through.
	//
	// The pin pairs want and result structurally, not only
	// Con-vs-Con: a bare result meta — `id (Some 1)` under
	// `Option Go.Any`, where `?a` is shared between `id`'s
	// parameter and return — binds to the whole want so the
	// argument's own pin chains through it, and an instantiated
	// Go named type or a residual function pairs its args/
	// params/result the same way. A constrained meta (numeric,
	// ordered, ...) is skipped rather than bound: it can never
	// hold `any`, and the ordinary argument check reports the
	// honest mismatch.
	pinParams := false
	var pin func(w, r types.Type, inParam bool)
	pin = func(w, r types.Type, inParam bool) {
		wc, rc := types.Chase(w), types.Chase(r)
		if wc == nil || rc == nil {
			return
		}
		switch r := rc.(type) {
		case *types.Meta:
			// Defined types pin like `any` does — but only in
			// result position: `Some "x" : Option Name` binds
			// `a := Name` so the String literal gets its
			// materialization (scalarLitConv) instead of
			// `a := String`. A residual *parameter* pins only when
			// the fill set was decided statically (pinParams) — for
			// a spread or otherwise undecided fill the residual
			// pairs the wrong slots and the pin would retype the
			// supplied argument through them.
			if containsGoAny(wc) || ((!inParam || pinParams) && c.definedUnderlying(wc) != wc) {
				if rec := c.recOf(r); !(rec.num || rec.ord || rec.flo || rec.intg || rec.eq) {
					c.bind(r, wc, e.Sp)
				}
			}
		case *types.Con:
			if w, ok := wc.(*types.Con); ok &&
				r.Qual == w.Qual && r.Name == w.Name && len(r.Args) == len(w.Args) {
				for i := range w.Args {
					pin(w.Args[i], r.Args[i], inParam)
				}
			}
		case *types.Go:
			if w, ok := wc.(*types.Go); ok &&
				r.Ref == w.Ref && len(r.Args) == len(w.Args) {
				for i := range w.Args {
					pin(w.Args[i], r.Args[i], inParam)
				}
			}
		case *types.Func:
			if w, ok := wc.(*types.Func); ok {
				wp, wr := flattenFunc(w)
				rp, rr := flattenFunc(r)
				// Pair the common prefix of parameters; a
				// residual whose ret is still an unsolved meta
				// flattens shorter than the want, and skipping
				// the pin entirely would let a supplied literal
				// bind the shared meta before the declared type
				// reaches it (`apply f "x"` under
				// `Name -> Int -> String`). The tail pins
				// whatever remains — an unbound meta takes no
				// bind here and is left for conformance.
				n := min(len(wp), len(rp))
				for i := 0; i < n; i++ {
					pin(wp[i].Type, rp[i].Type, true)
				}
				wt, rt := wr, rr
				if len(wp) > n {
					wt = &types.Func{Params: wp[n:], Ret: wr}
				}
				if len(rp) > n {
					rt = &types.Func{Params: rp[n:], Ret: rr}
				}
				pin(wt, rt, inParam)
			}
		case *types.Tuple:
			if w, ok := wc.(*types.Tuple); ok && len(w.Elems) == len(r.Elems) {
				for i := range w.Elems {
					pin(w.Elems[i], r.Elems[i], inParam)
				}
			}
		case *types.Var:
			// A rigid variable only ever pairs with itself under
			// unification — no pin to propagate.
		}
	}
	// The pin walks the application's *residual* type — for a
	// partial application that is `Func(remaining params, ret)`,
	// not `ret` itself. Pinning `ret` directly would bind a
	// shared result meta to the whole expected function (`keep
	// "s"` under `Go.Any -> String` would pin `?a := Go.Any ->
	// String`, poisoning `keep`'s first parameter — a false
	// E0401).
	appRet := ret
	if len(e.Args) < len(params) {
		// The true residual needs the actual fill set —
		// `params[len(args):]` is only accurate for a prefix
		// fill, and pairing want against the wrong slots pins a
		// shared param/result meta to the wrong want — `pair "x"`
		// under `Name -> String` would pin `b := String` through
		// the result and the String literal could never
		// materialize as `Name "x"`.
		if used, ok := c.fillUsed(e.Args, params); ok {
			appRet = applyResidual(fn.Type(), used, 0)
			pinParams = true
		}
	}
	if appRet == ret && len(e.Args) < len(params) {
		appRet = &types.Func{Params: params[len(e.Args):], Ret: ret}
	}
	// Over-application never pins: with more args than flat params
	// the want belongs to the FINAL result of the chain, not to
	// ret — and for an unresolved meta callee (`op api` inside a
	// lambda) flattenFunc reports params empty and ret = the
	// callee type itself, so pinning would bind the callee's own
	// meta to a non-function want before the over-application
	// machinery can shape it as a function (the E0401 regression
	// of `List.fold (fn api op -> op api)`). A variadic Go call is
	// not over-applied: the surplus arguments fill the variadic
	// slot and ret IS the call's result, so the pin still fires.
	pinOK := len(e.Args) <= len(params)
	if !pinOK && goCall && callee != nil {
		if sig, ok := c.sigOf(callee, e.Sp); ok && sig.Variadic {
			pinOK = true
		}
	}
	if pinOK {
		pin(want, appRet, false)
	}
	if goCall {
		sig, _ := c.sigOf(callee, e.Sp)
		arity := c.declArity(sig, len(params))
		return c.checkGoApply(e, fn, callee, params, ret, sig.Variadic, arity)
	}
	arity := len(params)
	if callee != nil {
		if sig, ok := c.sigOf(callee, e.Sp); ok {
			arity = c.declArity(sig, arity)
		}
	}
	return c.checkGomApply(e, fn, callee, params, ret, arity, want)
}

// declArity returns the callee's own declared parameter count — what
// CallInfo.Arity records. When the signature keeps the declared
// (unflattened) shape its outer parameter list is the declaration;
// DeclArity pins it explicitly for sources that hand out flattened
// types (e.g. an imported `fn mk () -> (Int -> Int)`).
func (c *checker) declArity(sig Signature, flat int) int {
	if sig.DeclArity > 0 {
		if sig.DeclArity < flat {
			return sig.DeclArity
		}
		return flat
	}
	if f, ok := types.Chase(sig.Type).(*types.Func); ok {
		if n := f.Arity(); n <= flat {
			return n
		}
	}
	return flat
}

// checkConvCall handles `Go.conv e` written without the mandatory
// `: T` ascription — always an error, kept recoverable. The E0507
// itself was already reported by checkSelect, which types the conv
// member in expression position on the way through calleeOf.
func (c *checker) checkConvCall(e *syntax.Apply, fn Expr) Expr {
	out := &Apply{Base: baseOf(e.Sp, c.quietMeta(e.Sp, "the conversion's result")), Fn: fn}
	for _, a := range e.Args {
		var v Expr
		if a.Value != nil {
			v = c.checkExpr(a.Value, nil)
		} else {
			v = c.checkExpr(nil, nil)
		}
		out.Args = append(out.Args, Arg{Value: v})
	}
	out.Call = CallInfo{Kind: CallUnknown, Arity: 0, Applied: len(out.Args), HoleIndex: -1}
	return out
}

// typePrefixedRecordType resolves the record construction type for a
// type prefix. Aliases are transparent here just as in type
// annotations: instantiate their parameters with metas, expand the
// target, and require that target to be a record constructor. When the
// enclosing context already expects a compatible shape, use it to
// instantiate the prefix's inference metas — `Box { value = "s" }`
// under `Box Go.Any` must check the field against `Go.Any`, not infer a
// disjoint `Box String`. Unlike the old copy-in approach this never
// rewrites a shared alias body: incompatible expectations are left for
// the ordinary result-type mismatch.
func (c *checker) typePrefixedRecordType(ti *typeInfo, sp diag.Span, want types.Type) (types.Type, bool) {
	instantiate := func(params []*types.Var) ([]types.Type, types.Subst) {
		args := make([]types.Type, len(params))
		sub := types.Subst{}
		for i, v := range params {
			args[i] = c.newMeta(sp, "the record type's argument")
			sub[v.ID] = args[i]
		}
		return args, sub
	}

	var rt types.Type
	if ti.kind == kindAlias {
		args, sub := instantiate(ti.params)
		if !ti.aliasRes && ti.aliasBod != nil {
			c.resolveAlias(ti, ti.aliasBod)
		}
		var target types.Type = c.quietMeta(sp, "the type alias")
		if ti.aliasRes {
			target = sub.Apply(ti.alias)
		}
		con, ok := types.Chase(target).(*types.Con)
		if !ok {
			return target, false
		}
		rti := c.tyCons[conKey{qual: con.Qual, name: con.Name}]
		if rti == nil || rti.kind != kindRecord {
			return target, false
		}
		// An alias parameter can be absent from its expansion
		// (`type alias ConstBox a = Box Int`). It does not make the
		// constructed record any less determined, so it must not demand
		// an annotation of its own.
		used := map[*types.Meta]bool{}
		metasIn(target, func(m *types.Meta) { used[m] = true })
		for _, arg := range args {
			if m, ok := types.Chase(arg).(*types.Meta); ok && !used[m] {
				c.markQuiet(m)
			}
		}
		rt = con
	} else if ti.kind == kindRecord {
		args, _ := instantiate(ti.params)
		rt = &types.Con{Qual: ti.con.Qual, Name: ti.con.Name, Args: args}
	} else {
		return ti.con, false
	}
	c.useExpectedPrefixType(rt, want, sp)
	return rt, true
}

// anonRecordLitInResultPosition reports whether an expression's result
// can be an anonymous record literal. Nested positions that merely evaluate
// a record for its value (let bodies, call arguments, field values) do not
// count: contextual propagation follows only the expression positions whose
// type is the enclosing expression's own result type.
func anonRecordLitInResultPosition(e syntax.Expr) bool {
	switch e := e.(type) {
	case *syntax.RecordLit:
		return e.Base == nil
	case *syntax.Ascribe:
		return anonRecordLitInResultPosition(e.X)
	case *syntax.Lambda:
		return anonRecordLitInResultPosition(e.Body)
	case *syntax.Block:
		return len(e.Stmts) > 0 && anonRecordLitInResultPosition(e.Stmts[len(e.Stmts)-1])
	case *syntax.If:
		return anonRecordLitInResultPosition(e.Then) || (e.Else != nil && anonRecordLitInResultPosition(e.Else))
	case *syntax.Match:
		for _, arm := range e.Arms {
			if anonRecordLitInResultPosition(arm.Body) {
				return true
			}
		}
	}
	return false
}

// applyParamExpectations derives the parameter views a statically
// decidable application fills when its result already has an expected
// type. The derivation is speculative: inference metas on both sides
// are cloned before the trial unification, so the surrounding inference
// state is never bound early. Reified open metas act only as forwarding
// placeholders inside the argument being checked; ordinary conformance
// still links the callee's own metas in source order.
type applyParamExpectations struct {
	c           *checker
	params      []types.Type
	informative []bool
	open        map[*types.Meta]*types.Meta
	sp          diag.Span
}

func (c *checker) applyParamExpectations(fnT types.Type, params []types.FuncParam, args []syntax.Arg, want types.Type, sp diag.Span) *applyParamExpectations {
	if want == nil || len(args) > len(params) {
		return nil
	}
	hasRecordLit := false
	for _, a := range args {
		if anonRecordLitInResultPosition(a.Value) {
			hasRecordLit = true
			break
		}
	}
	if !hasRecordLit {
		return nil
	}
	used, ok := c.fillUsed(args, params)
	if !ok {
		return nil
	}
	residual := applyResidual(fnT, used, 0)
	clones := map[*types.Meta]*types.Meta{}
	cw := c.clonePrefixType(want, clones)
	cr := c.clonePrefixType(residual, clones)
	cps := make([]types.Type, len(params))
	for i, p := range params {
		cps[i] = c.clonePrefixType(p.Type, clones)
	}
	ndiag := len(c.diags)
	ok = c.unify(cw, cr, sp)
	c.diags = c.diags[:ndiag]
	for _, m := range clones {
		delete(c.metas, m)
	}
	if !ok {
		return nil
	}
	x := &applyParamExpectations{
		c:           c,
		params:      cps,
		informative: make([]bool, len(params)),
		open:        map[*types.Meta]*types.Meta{},
		sp:          sp,
	}
	for i, p := range params {
		metasIn(p.Type, func(m *types.Meta) {
			cm, ok := clones[m]
			if !ok {
				return
			}
			if _, open := types.Chase(cm).(*types.Meta); !open {
				x.informative[i] = true
			}
		})
	}
	return x
}

func (x *applyParamExpectations) at(i int, original types.Type) (types.Type, bool) {
	if x == nil || i < 0 || i >= len(x.params) || !x.informative[i] {
		return nil, false
	}
	if !x.c.containsRecordType(x.params[i]) {
		return nil, false
	}
	reifiedBefore := make(map[*types.Meta]bool, len(x.open))
	for _, m := range x.open {
		reifiedBefore[m] = true
	}
	merged := x.c.mergeContextualType(original, x.params[i], x.open, x.sp)
	if types.Equal(merged, original) {
		// Reification can mint real metas before the merge turns out not to
		// add information. Roll back only the metas introduced by this
		// attempt; earlier entries may already participate in another
		// parameter's accepted expectation.
		for clone, m := range x.open {
			if !reifiedBefore[m] {
				delete(x.c.metas, m)
				delete(x.open, clone)
			}
		}
		return nil, false
	}
	return merged, true
}

// mergeContextualType combines the callee's current parameter type
// with its speculative expectation at type-constructor granularity.
// The current type is authoritative wherever both sides are concrete;
// speculative concrete structure fills open parts. Function labels are
// acceptance syntax rather than inference structure, so the current
// side's label is kept without treating a speculative label mismatch
// as a type mismatch. Open speculative leaves are not reified when the
// current type already has a node for the position — using that node
// keeps ordinary inference sharing and avoids orphaned forwarding
// variables.
func (c *checker) mergeContextualType(original, speculative types.Type, open map[*types.Meta]*types.Meta, sp diag.Span) types.Type {
	o := types.Chase(original)
	x := types.Chase(speculative)
	if _, openLeaf := x.(*types.Meta); openLeaf {
		return o
	}
	if _, openOriginal := o.(*types.Meta); openOriginal {
		t, _ := c.reifyPrefixType(x, open, sp)
		return t
	}
	switch o := o.(type) {
	case *types.Con:
		xc, ok := x.(*types.Con)
		if !ok || o.Qual != xc.Qual || o.Name != xc.Name || len(o.Args) != len(xc.Args) {
			return o
		}
		args, changed := c.mergeContextualTypeSlice(o.Args, xc.Args, open, sp)
		if !changed {
			return o
		}
		return &types.Con{Qual: o.Qual, Name: o.Name, Args: args}
	case *types.Func:
		xf, ok := x.(*types.Func)
		if !ok {
			return o
		}
		op, or := flattenFunc(o)
		xp, xr := flattenFunc(xf)
		shared := len(op)
		extendOriginal := false
		extendSpeculative := false
		switch {
		case len(xp) < shared:
			// A speculative function can end in an open meta while the
			// already-inferred type continues curried. The open tail adds no
			// information, but the shared prefix can still be merged.
			if _, openTail := types.Chase(xr).(*types.Meta); !openTail {
				return o
			}
			shared = len(xp)
			extendOriginal = true
		case len(xp) > shared:
			// Conversely an open original return can stand for the rest of a
			// curried callback. Fill that tail from the longer speculative
			// signature instead of discarding the whole expectation.
			if _, openTail := types.Chase(or).(*types.Meta); !openTail {
				return o
			}
			extendSpeculative = true
		}
		ps := make([]types.FuncParam, shared)
		changed := false
		for i := 0; i < shared; i++ {
			fp := op[i]
			nt := c.mergeContextualType(fp.Type, xp[i].Type, open, sp)
			changed = changed || nt != fp.Type
			ps[i] = types.FuncParam{Label: fp.Label, Type: nt}
		}
		var ret types.Type
		switch {
		case extendOriginal:
			ret = types.Fn(or, op[shared:]...)
		case extendSpeculative:
			tail := types.Fn(xr, xp[shared:]...)
			ret = c.mergeContextualType(or, tail, open, sp)
			changed = true
		default:
			ret = c.mergeContextualType(or, xr, open, sp)
			changed = changed || ret != or
		}
		if !changed {
			return o
		}
		return types.Fn(ret, ps...)
	case *types.Tuple:
		xt, ok := x.(*types.Tuple)
		if !ok || len(o.Elems) != len(xt.Elems) {
			return o
		}
		elems, changed := c.mergeContextualTypeSlice(o.Elems, xt.Elems, open, sp)
		if !changed {
			return o
		}
		return &types.Tuple{Elems: elems}
	case *types.Go:
		xg, ok := x.(*types.Go)
		if !ok || o.Ref != xg.Ref || len(o.Args) != len(xg.Args) {
			return o
		}
		args, changed := c.mergeContextualTypeSlice(o.Args, xg.Args, open, sp)
		if !changed {
			return o
		}
		return &types.Go{Ref: o.Ref, Args: args}
	case *types.Meta:
		return o
	case *types.Var:
		return o
	default:
		panic("unreachable")
	}
}

func (c *checker) mergeContextualTypeSlice(original, speculative []types.Type, open map[*types.Meta]*types.Meta, sp diag.Span) ([]types.Type, bool) {
	out := make([]types.Type, len(original))
	changed := false
	for i, t := range original {
		out[i] = c.mergeContextualType(t, speculative[i], open, sp)
		changed = changed || out[i] != t
	}
	if !changed {
		return original, false
	}
	return out, true
}

// useExpectedPrefixType binds the inference metas inside rt from a
// compatible expectation. Compatibility is tested speculatively on
// cloned metas first: the trial uses the real unification rules
// (including curried-tail absorption and shared-meta propagation), but
// it must neither mutate inference state nor emit a second mismatch.
// Only structurally informative solutions for rt's own inference metas
// are replayed. A wholly open solution is replayed only when it is shared
// with another solution, where it still carries an equality constraint.
// External inference metas are never bound by this lookahead — doing
// so would let constraints flow backwards into the surrounding
// expression before its own arguments are checked.
func (c *checker) useExpectedPrefixType(rt, want types.Type, sp diag.Span) {
	if want == nil {
		return
	}
	var rtMetas []*types.Meta
	metasIn(rt, func(m *types.Meta) { rtMetas = append(rtMetas, m) })

	clones := map[*types.Meta]*types.Meta{}
	cr := c.clonePrefixType(rt, clones)
	cw := c.clonePrefixType(want, clones)
	ndiag := len(c.diags)
	ok := c.unify(cw, cr, sp)
	c.diags = c.diags[:ndiag]

	solutions := map[*types.Meta]types.Type{}
	openMetas := map[*types.Meta]*types.Meta{}
	if ok {
		sharedOpen := map[*types.Meta]int{}
		for _, m := range rtMetas {
			cm, ok := clones[m]
			if !ok {
				continue
			}
			metasIn(types.Chase(cm), func(open *types.Meta) { sharedOpen[open]++ })
		}
		for _, m := range rtMetas {
			cm, ok := clones[m]
			if !ok {
				continue
			}
			cs := types.Chase(cm)
			if target, openOnly := cs.(*types.Meta); openOnly && sharedOpen[target] < 2 {
				// A single wholly open solution carries no information. Skip it
				// before reification so no unused inference artifact is minted.
				continue
			}
			if sol, ok := c.reifyPrefixType(cs, openMetas, sp); ok {
				solutions[m] = sol
			}
		}
	}
	for _, m := range clones {
		delete(c.metas, m)
	}
	if !ok {
		return
	}
	for _, m := range rtMetas {
		if sol, ok := solutions[m]; ok {
			_ = c.bind(m, sol, sp)
		}
	}
}

// reifyPrefixType rewrites a speculative solution. Concrete structure is
// kept; an unsolved clone meta becomes a fresh ordinary real meta so that
// ordinary unification can link it to the surrounding inference variable
// in checking order instead of letting lookahead mutate that variable now.
// The meta is neither quiet nor silent: if it really remains undetermined,
// inference must still reject the program.
func (c *checker) reifyPrefixType(t types.Type, open map[*types.Meta]*types.Meta, sp diag.Span) (types.Type, bool) {
	switch t := types.Chase(t).(type) {
	case *types.Meta:
		if m, ok := open[t]; ok {
			return m, true
		}
		m := c.newMeta(sp, "the unresolved expected type")
		open[t] = m
		return m, true
	case *types.Var:
		return t, true
	case *types.Con:
		args, ok := c.reifyPrefixTypeSlice(t.Args, open, sp)
		if !ok {
			return nil, false
		}
		return &types.Con{Qual: t.Qual, Name: t.Name, Args: args}, true
	case *types.Func:
		ps := make([]types.FuncParam, len(t.Params))
		for i, p := range t.Params {
			nt, ok := c.reifyPrefixType(p.Type, open, sp)
			if !ok {
				return nil, false
			}
			ps[i] = types.FuncParam{Label: p.Label, Type: nt}
		}
		ret, ok := c.reifyPrefixType(t.Ret, open, sp)
		if !ok {
			return nil, false
		}
		return types.Fn(ret, ps...), true
	case *types.Tuple:
		elems, ok := c.reifyPrefixTypeSlice(t.Elems, open, sp)
		if !ok {
			return nil, false
		}
		return &types.Tuple{Elems: elems}, true
	case *types.Go:
		args, ok := c.reifyPrefixTypeSlice(t.Args, open, sp)
		if !ok {
			return nil, false
		}
		return &types.Go{Ref: t.Ref, Args: args}, true
	default:
		panic("unreachable")
	}
}

func (c *checker) reifyPrefixTypeSlice(ts []types.Type, open map[*types.Meta]*types.Meta, sp diag.Span) ([]types.Type, bool) {
	out := make([]types.Type, len(ts))
	for i, t := range ts {
		nt, ok := c.reifyPrefixType(t, open, sp)
		if !ok {
			return nil, false
		}
		out[i] = nt
	}
	return out, true
}

// clonePrefixType copies a type for speculative unification. Bound
// metas are chased away; unbound metas become fresh metas with copied
// constraints. Type nodes themselves are immutable and can be shared
// when no meta occurs below them.
func (c *checker) clonePrefixType(t types.Type, clones map[*types.Meta]*types.Meta) types.Type {
	switch t := types.Chase(t).(type) {
	case *types.Meta:
		if cl, ok := clones[t]; ok {
			return cl
		}
		cl := &types.Meta{ID: c.nextMeta}
		c.nextMeta++
		rec := &metaRec{}
		if r := c.metas[t]; r != nil {
			cp := *r
			rec = &cp
		}
		c.metas[cl] = rec
		clones[t] = cl
		return cl
	case *types.Var:
		return t
	case *types.Con:
		args, changed := c.clonePrefixTypeSlice(t.Args, clones)
		if !changed {
			return t
		}
		return &types.Con{Qual: t.Qual, Name: t.Name, Args: args}
	case *types.Func:
		ps := make([]types.FuncParam, len(t.Params))
		changed := false
		for i, p := range t.Params {
			nt := c.clonePrefixType(p.Type, clones)
			changed = changed || nt != p.Type
			ps[i] = types.FuncParam{Label: p.Label, Type: nt}
		}
		ret := c.clonePrefixType(t.Ret, clones)
		if !changed && ret == t.Ret {
			return t
		}
		return types.Fn(ret, ps...)
	case *types.Tuple:
		elems, changed := c.clonePrefixTypeSlice(t.Elems, clones)
		if !changed {
			return t
		}
		return &types.Tuple{Elems: elems}
	case *types.Go:
		args, changed := c.clonePrefixTypeSlice(t.Args, clones)
		if !changed {
			return t
		}
		return &types.Go{Ref: t.Ref, Args: args}
	default:
		panic("unreachable")
	}
}

func (c *checker) clonePrefixTypeSlice(ts []types.Type, clones map[*types.Meta]*types.Meta) ([]types.Type, bool) {
	var out []types.Type
	for i, t := range ts {
		nt := c.clonePrefixType(t, clones)
		if out == nil && nt != t {
			out = append([]types.Type(nil), ts...)
		}
		if out != nil {
			out[i] = nt
		}
	}
	if out == nil {
		return ts, false
	}
	return out, true
}

// typePrefixedRecordLit recognizes `T { x = v }` where resolution
// bound the head to a type symbol. The parser cannot distinguish this
// form from constructor application — `Ctor { x = v }` may pass a
// record as a constructor argument — so resolution keeps the term
// reading whenever the head is a real term and uses the type reading
// only for a head that otherwise has no value meaning.
func (c *checker) typePrefixedRecordLit(e *syntax.Apply) (*typeInfo, *syntax.RecordLit) {
	if len(e.Args) != 1 || e.Args[0].Label != nil || e.Args[0].Spread {
		return nil, nil
	}
	lit, ok := e.Args[0].Value.(*syntax.RecordLit)
	if !ok || lit.Base != nil {
		return nil, nil
	}
	var sym *resolve.Symbol
	if fn, ok := e.Fn.(*syntax.Ident); ok {
		sym = c.res.UseOf(fn)
	} else if fn, ok := e.Fn.(*syntax.Select); ok && fn.Sel != nil {
		sym = c.res.UseOf(fn.Sel)
	}
	if sym == nil || sym.Kind != resolve.KindType {
		return nil, nil
	}
	if ti := c.tyBySym[sym]; ti != nil {
		return ti, lit
	}
	return nil, nil
}

// checkNilCall types `Go.nil ()` — the universal nil literal
// (decision 2026-11-23-nil-spelling). The result is a fresh meta
// the surrounding context pins; validateNils judges the solved
// type's nil-ability after inference. The `()` argument is the
// ordinary arity-0 call marker — anything else is a plain Unit
// mismatch. The application classifies as a builtin CallDirect so
// lowering emits the `rt.Nil[T]()` call via CallBuiltin.
func (c *checker) checkNilCall(e *syntax.Apply, fn Expr, callee *resolve.Symbol) Expr {
	a := c.newMeta(e.Sp, "the nil value's type")
	// The member select's own `Unit -> ?a` never drives this call —
	// the result meta is the apply's. Quiet it so the stray `?a`
	// does not demand an annotation.
	metasIn(fn.Type(), func(m *types.Meta) { c.markQuiet(m) })
	out := &Apply{Base: baseOf(e.Sp, a), Fn: fn}
	for _, ar := range e.Args {
		var v Expr
		if ar.Value != nil {
			v = c.checkExpr(ar.Value, types.Unit)
			if _, isHole := v.(*Hole); isHole {
				// `_` is not an argument here — the placeholder's
				// E0504 already stands, and a Hole left in Args
				// panics Apply.Validate. Recover as the `()` the
				// call expects.
				v = &Lit{Base: baseOf(ar.Value.Span(), types.Unit), Kind: syntax.LitUnit, Text: "()"}
			}
		} else {
			v = c.checkExpr(nil, nil)
		}
		out.Args = append(out.Args, Arg{Value: v})
	}
	out.Call = CallInfo{Kind: CallDirect, Callee: callee, Arity: len(out.Args), Applied: len(out.Args), HoleIndex: -1}
	if err := out.Validate(); err != nil {
		panic(fmt.Sprintf("check: invalid CallInfo for Go.nil: %v", err))
	}
	c.pendingNils = append(c.pendingNils, &pendingNil{sp: e.Sp, m: a})
	return out
}

// calleeOf resolves the function part of an application. It returns
// the typed callee expression, the callee's symbol when it names a
// known function (nil for an arbitrary function-typed value), and
// whether the Go calling convention applies.
func (c *checker) calleeOf(e syntax.Expr) (Expr, *resolve.Symbol, bool) {
	fn := c.checkExpr(e, nil)
	var sym *resolve.Symbol
	switch fn := fn.(type) {
	case *Var:
		sym = fn.Sym
	case *MemberExpr:
		sym = fn.Sym
	case *Apply, *Ascribe, *Binary, *Block, *FieldAssign, *For,
		*GoExpr, *Hole, *If, *Ignore, *Lambda, *Let, *ListLit,
		*Lit, *Match, *RecordLit, *SelectExpr, *Spread,
		*StrInterp, *Try, *TupleLit, *Unary:
		// Not a named callee: an arbitrary function-typed value.
	default:
		panic("unreachable")
	}
	if sym == nil {
		return fn, nil, false
	}
	// Only Go *functions* (and methods) use the Go calling
	// convention — a Go package variable or constant is a plain
	// value: `os.Args ()` must fall through to ordinary
	// application diagnostics, not become a CallGo that lowering
	// rejects as an E0901 contract gap.
	if sym.IsGo && (sym.Kind == resolve.KindFunc || sym.Kind == resolve.KindMethod) {
		return fn, sym, true
	}
	switch sym.Kind {
	case resolve.KindFunc, resolve.KindCtor, resolve.KindBuiltin:
		return fn, sym, false
	default:
		// A variable or other value holding a function.
		return fn, nil, false
	}
}

// checkGomApply matches surface arguments against the callee's
// flattened parameter list — positional arguments fill unlabeled
// parameters in declaration order (a positional argument may never
// fill a labeled parameter) and `~label:` arguments fill labeled
// parameters by name, including parameters of a returned-function
// level (docs/decisions/2026-11-06-label-args-into-returned-functions.md)
// — then classifies the call: a prefix fill covering the declared
// arity is CallDirect, a call leaving an earlier flat slot open is
// CallPartial.
func (c *checker) checkGomApply(e *syntax.Apply, fn Expr, callee *resolve.Symbol, params []types.FuncParam, ret types.Type, declArity int, want types.Type) Expr {
	flat := len(params)
	if declArity > flat {
		declArity = flat
	}
	used := make([]bool, flat)
	posIdx := 0
	argToParam := make([]int, 0, len(e.Args))
	var extras []Expr  // positional args beyond the flat parameter list
	var extraIdx []int // each extra's index in out.Args
	unmapped := false  // an argument no flat parameter position answers

	out := &Apply{Base: baseOf(e.Sp, nil), Fn: fn}
	expectations := c.applyParamExpectations(fn.Type(), params, e.Args, want, e.Sp)

	// `g args` where `g`'s type waits on a deferred member —
	// a variable aliasing the member's standing meta
	// (`let f = x.M`) or the result of a call still waiting
	// on one (`x.M(a) (b)`) — cannot pick a calling
	// convention either: a `-> T` member reads `f ()` as the
	// zero-argument call marker, a `Unit -> T` member as the
	// parameter, and the arg list may be the curried Gom
	// form or the tuple-shaped call of a Go method. Binding
	// `Unit -> ?r` (or the arg tuple's shape) now would
	// replay into `expected Unit -> ?r, got String` — and
	// the eager binding leaks the metas it mints into the
	// member's solution when the member turns out missing.
	// Defer the application to the member's replay the way
	// `x.M ()` itself waits.
	if len(e.Args) > 0 {
		if m, ok := types.Chase(fn.Type()).(*types.Meta); ok {
			var host *MemberExpr
			for _, me := range c.pendingSels {
				if types.Chase(me.Type()) == m {
					host = me
					break
				}
			}
			if host == nil {
				for _, q := range c.pendingApplies {
					if !q.done && types.Chase(q.ap.Type()) == m {
						host = q.me
						break
					}
				}
			}
			if host != nil {
				// `f ()` keeps the Gom reading of the unit literal
				// (alwaysGom); other arg lists replay by whatever
				// kind the member settles to.
				lit, isLit := e.Args[0].Value.(*syntax.Lit)
				pa := &pendingApply{me: host, calleeT: m, alwaysGom: isLit && lit.Kind == syntax.LitUnit}
				out.Base = baseOf(e.Sp, c.newMeta(e.Sp, "the deferred call's result"))
				pa.ap = out
				for _, a := range e.Args {
					label := ""
					var labelSp diag.Span
					if a.Label != nil {
						label = a.Label.Name
						labelSp = a.Label.Sp
					}
					out.Args = append(out.Args, Arg{
						Label: label, LabelSp: labelSp,
						Value: c.provisionalArg(a.Value, pa),
					})
				}
				out.Call = CallInfo{Kind: CallUnknown, Applied: len(out.Args), HoleIndex: -1}
				c.pendingApplies = append(c.pendingApplies, pa)
				return out
			}
		}
	}

	// A Go zero-argument function *value* — `let f = e.Error`,
	// `let g = time.Now` — types as `-> T` (the Go signature's
	// shape), not `Unit -> T`. It applies the way the Go source
	// does: `f ()`, where the unit literal is the call marker and
	// not an argument (checkGoApply drops it the same way), so a
	// bare `f ()` saturates into a zero-argument application.
	// Anything else over-applies the `-> T` result like any other
	// type.
	if flat == 0 && len(e.Args) == 1 && e.Args[0].Label == nil {
		if fty, ok := types.Chase(fn.Type()).(*types.Func); ok && len(fty.Params) == 0 {
			if lit, ok := e.Args[0].Value.(*syntax.Lit); ok && lit.Kind == syntax.LitUnit {
				out.Base = baseOf(e.Sp, ret)
				out.Call = CallInfo{
					Kind:      CallUnknown,
					Applied:   0,
					Arity:     flat,
					FlatArity: flat,
					HoleIndex: -1,
					TypeArgs:  targsOf(fn),
				}
				if err := out.Validate(); err != nil {
					panic(fmt.Sprintf("check: invalid CallInfo for %T: %v", e.Fn, err))
				}
				return out
			}
		}
	}

	for _, a := range e.Args {
		if a.Spread {
			c.spreadRecordArgs(a, fn, params, used, out, &argToParam, &unmapped)
			continue
		}
		label := ""
		var labelSp diag.Span
		if a.Label != nil {
			label = a.Label.Name
			labelSp = a.Label.Sp
		}
		pi := -1
		if label != "" {
			pi = c.paramByLabel(params, label)
			if pi < 0 {
				if _, unresolved := types.Chase(fn.Type()).(*types.Meta); unresolved || flat == 0 {
					// A labeled call needs the callee's parameter
					// list: on an unresolved function type that is
					// E0408, not "unknown label".
					c.diags.Add(diag.Error("E0408",
						"a labeled call requires the callee's type to be known").
						At(labelSpan(labelSp, a.Sp)).Set("label", label))
				} else {
					c.diags.Add(diag.Error("E0406",
						fmt.Sprintf("the callee has no parameter labeled `~%s`", label)).
						At(labelSpan(labelSp, a.Sp)).Set("label", label))
				}
			} else if used[pi] {
				c.diags.Add(diag.Error("E0405",
					fmt.Sprintf("the label `~%s` is supplied twice", label)).
					At(labelSpan(labelSp, a.Sp)).Set("label", label))
				pi = -1
			}
		} else {
			for posIdx < flat && (used[posIdx] || params[posIdx].Label != "") {
				posIdx++
			}
			if posIdx < flat {
				pi = posIdx
				posIdx++
			}
		}
		var pt types.Type
		contextual := false
		switch {
		case pi >= 0:
			used[pi] = true
			pt = params[pi].Type
			if xp, ok := expectations.at(pi, pt); ok {
				pt = xp
				contextual = true
			}
		case label == "":
			// A positional argument beyond the callee's parameters
			// over-applies into the returned function value.
			pt = c.newMeta(a.Sp, "an over-applied argument's parameter")
		default:
			// The label already reported E0406/E0408 — the
			// argument's parameter slot is recovery.
			pt = c.quietMeta(a.Sp, fmt.Sprintf("the argument `~%s`", label))
		}
		var argExpr Expr
		switch av := a.Value.(type) {
		case nil:
			// A missing argument value (recovery hole in the
			// surface tree) — same handling as the Go path.
			argExpr = c.checkExpr(nil, pt)
		case *syntax.Hole:
			c.diags.Add(diag.Error("E0504",
				"`_` is only allowed as an argument of a Go call").At(av.Sp))
			// A Hole in Args would fail Apply.Validate on a non-Go
			// call — recover with a unit literal of the slot type.
			argExpr = &Lit{Base: baseOf(av.Sp, pt), Kind: syntax.LitUnit, Text: "()"}
		case *syntax.Spread:
			x := c.checkExpr(av.X, nil)
			c.diags.Add(diag.Error("E0505",
				"`..e` is only allowed as the last argument of a variadic Go call").At(av.Sp))
			argExpr = &Spread{Base: baseOf(av.Sp, x.Type()), X: x}
		case *syntax.Ident, *syntax.Lit, *syntax.StrInterp,
			*syntax.Apply, *syntax.Select, *syntax.Unary,
			*syntax.Binary, *syntax.Lambda, *syntax.If, *syntax.Match,
			*syntax.Block, *syntax.Let, *syntax.Ignore, *syntax.Try,
			*syntax.GoExpr, *syntax.For, *syntax.SelectExpr,
			*syntax.Assign, *syntax.ListLit, *syntax.TupleLit, *syntax.Ascribe,
			*syntax.RecordLit:
			argExpr = c.checkExpr(a.Value, pt)
		default:
			panic("unreachable")
		}
		if contextual && pi >= 0 && argExpr != nil {
			argExpr = c.conformExpr(argExpr, params[pi].Type, a.Value.Span())
		}
		out.Args = append(out.Args, Arg{Label: label, LabelSp: labelSp, Value: argExpr})
		argToParam = append(argToParam, pi)
		if pi < 0 {
			if label == "" {
				extras = append(extras, argExpr)
				extraIdx = append(extraIdx, len(out.Args)-1)
			}
			unmapped = true
		}
	}

	if callee != nil && callee.Kind == resolve.KindBuiltin && callee.Name == "assert_eq" && len(params) > 0 {
		c.constrain(params[0].Type, e.Sp, "the operands of `assert_eq`", func(r *metaRec) { r.eq = true })
	}

	// Positional arguments that outlive the callee's parameters are
	// applied to the returned function; the result type is whatever
	// remains.
	var res types.Type
	if len(extras) > 0 {
		res = c.overApply(ret, extras, extraIdx, out.Args, e.Sp)
	} else {
		res = applyResidual(fn.Type(), used, 0)
	}
	out.Base = baseOf(e.Sp, res)

	// CallInfo: the mapping domain is the *flattened* parameter
	// list — a `~label:` may name a returned-function level's
	// parameter, and ArgToParam records the flat position it
	// landed on. Classification reads the filled prefix: the call
	// is CallDirect when the filled flat positions cover the
	// declared level contiguously (deeper filled slots chain onto
	// the returned function), CallPartial when an earlier flat
	// slot stays open — the residual is a function awaiting it.
	// Arguments no flat position answers (a positional extra
	// applied onto the result, or a label the call already
	// reported) leave ArgToParam nil and classify positionally.
	out.Call = CallInfo{
		Callee:    callee,
		Applied:   len(out.Args),
		Arity:     declArity,
		FlatArity: flat,
		HoleIndex: -1,
		TypeArgs:  targsOf(fn),
	}
	allMapped := !unmapped
	switch {
	case callee == nil:
		out.Call.Kind = CallUnknown
		out.Call.Arity = flat
		if allMapped {
			out.Call.ArgToParam = normalizeMapping(argToParam)
		}
	case allMapped:
		out.Call.ArgToParam = normalizeMapping(argToParam)
		out.Call.Kind, out.Call.Unapplied = ClassMappedCall(argToParam, flat, declArity)
	case out.Call.Applied >= declArity:
		out.Call.Kind = CallDirect
	default:
		out.Call.Kind = CallPartial
		out.Call.Unapplied = flat - out.Call.Applied
	}
	if err := out.Validate(); err != nil {
		panic(fmt.Sprintf("check: invalid CallInfo for %T: %v", e.Fn, err))
	}
	return out
}

// ClassMappedCall classifies a declared-callee call whose every
// argument resolved to a flat parameter position: CallDirect when
// the filled positions form a prefix {0..k} covering the declared
// level — the callee applies directly and any deeper filled slots
// chain onto the returned function — CallPartial otherwise. The
// second result is the count of still-unfilled flat positions —
// the closure partialCall emits binds them in declaration order.
// Lowering re-runs the same rule when a pipe-synthesized argument
// joins an application (expr.appendArg), so the classification
// stays in one place.
func ClassMappedCall(mapping []int, flat, declArity int) (CallKind, int) {
	highest := -1
	for _, p := range mapping {
		if p > highest {
			highest = p
		}
	}
	if len(mapping) == highest+1 && highest >= declArity-1 {
		return CallDirect, 0
	}
	return CallPartial, flat - len(mapping)
}

// applyResidual rebuilds an application's result type from the
// callee's own — possibly nested — arrow structure. `used` marks the
// flattened parameters (in flattenFunc order) the call filled; the
// residual keeps the declared split level by level: `add3 40` is
// `Int -> (Int -> Int)`, so a later `g 2 0` chunks at immediate
// arity 1 the way the emitted Go value demands. types.Fn would
// flatten that to `Int -> Int -> Int`, and rebuilding from the
// flattened parameter list loses splits declared inside Ret.
func applyResidual(t types.Type, used []bool, off int) types.Type {
	fs, ok := types.Chase(t).(*types.Func)
	if !ok {
		return t
	}
	var rem []types.FuncParam
	for i, p := range fs.Params {
		if off+i >= len(used) || !used[off+i] {
			rem = append(rem, p)
		}
	}
	r := applyResidual(fs.Ret, used, off+len(fs.Params))
	if len(rem) == 0 {
		return r
	}
	return &types.Func{Params: rem, Ret: r}
}

// fillUsed simulates the argument loop's slot assignment without
// checking anything, and reports the fill set when it is decided
// statically — every argument either an ordinary positional or a
// label that resolves. Anything that depends on the arguments' own
// types (a record spread, a `..` tail) or that errors (unknown or
// repeated label, more positionals than slots) fails, and the
// caller keeps the positional-prefix residual.
func (c *checker) fillUsed(args []syntax.Arg, params []types.FuncParam) ([]bool, bool) {
	used := make([]bool, len(params))
	posIdx := 0
	for _, a := range args {
		if a.Spread {
			return nil, false
		}
		switch a.Value.(type) {
		case *syntax.Hole, *syntax.Spread:
			return nil, false
		case *syntax.Ident, *syntax.Lit, *syntax.StrInterp,
			*syntax.Apply, *syntax.Select, *syntax.Unary,
			*syntax.Binary, *syntax.Lambda, *syntax.If, *syntax.Match,
			*syntax.Block, *syntax.Let, *syntax.Ignore, *syntax.Try,
			*syntax.GoExpr, *syntax.For, *syntax.SelectExpr,
			*syntax.Assign, *syntax.ListLit, *syntax.TupleLit,
			*syntax.Ascribe, *syntax.RecordLit:
			// An ordinary expression occupies its slot.
		case nil:
			// A missing argument value still occupies the slot.
		default:
			panic("unreachable")
		}
		label := ""
		if a.Label != nil {
			label = a.Label.Name
		}
		pi := -1
		if label != "" {
			pi = c.paramByLabel(params, label)
			if pi < 0 || used[pi] {
				return nil, false
			}
		} else {
			for posIdx < len(params) && (used[posIdx] || params[posIdx].Label != "") {
				posIdx++
			}
			if posIdx >= len(params) {
				return nil, false
			}
			pi = posIdx
			posIdx++
		}
		used[pi] = true
	}
	return used, true
}

// labelSpan returns a label's own span, falling back to the arg's
// when the label carries no span (hand-built input).
func labelSpan(labelSp, argSp diag.Span) diag.Span {
	if labelSp.File == "" && labelSp.Start == 0 && labelSp.End == 0 {
		return argSp
	}
	return labelSp
}

// overApply applies extra arguments to the function value a saturated
// call returns and yields the remaining result type.
//
// Each extra extends the result with a fresh curried step, unified
// against the callee's return type as it stands: `Int -> Int -> Int`
// with one extra solves `next` to `Int -> Int`, so unfilled parameters
// (labeled or not) stay in the result. A positional extra can never
// fill a labeled parameter — unifyFunc's labeled-run rule reports the
// mismatch, enforcing spec section 2 rule 3 even when the returned
// function's type became known only through a bound meta (`id mk 3`
// used to slip through; docs/problems/2026-11-03-overapply-labeled-params.md).
func (c *checker) overApply(ret types.Type, extras []Expr, extraIdx []int, args []Arg, sp diag.Span) types.Type {
	rret := ret
	for i, ex := range extras {
		// A failed unify leaves next unsolved — the mismatch
		// already reported it.
		next := c.newMeta(ex.Span(), "the over-applied result")
		if !c.conformCallee(rret, ex.Type(), next, ex.Span(), true, ex) {
			c.markQuiet(next)
		} else {
			// The extra conforms to the residual's first
			// parameter like a direct call's argument — the
			// `Go.Any` box and boundary conversions a type-only
			// unify never wrapped (conformCallee).
			args[extraIdx[i]].Value = c.conformFirstArg(rret, ex)
		}
		rret = next
	}
	return rret
}

// spreadRecordArgs expands a `~..e` argument: the record's fields
// become labeled arguments matched to the callee's labeled
// parameters by name (spec section 2). The record expression checks
// once, so its effects evaluate once — each field reads it through
// a MemberExpr. Positional parameters are out of reach for a field
// name (paramByLabel only sees labeled slots); mixing a spread with
// positional arguments is fine because each fill an independent
// slot.
func (c *checker) spreadRecordArgs(a syntax.Arg, fn Expr, params []types.FuncParam,
	used []bool, out *Apply, argToParam *[]int, unmapped *bool) {
	x := c.checkExpr(a.Value, nil)
	c.spreadApply(x, a.Sp, fn, params, used, out, argToParam, unmapped)
}

// spreadApply expands a checked `~..e` record value — the
// convention-decided half of spreadRecordArgs, shared by the
// deferred-apply replay (which carries an already-checked Value).
func (c *checker) spreadApply(x Expr, sp diag.Span, fn Expr, params []types.FuncParam,
	used []bool, out *Apply, argToParam *[]int, unmapped *bool) {
	con, ok := types.Chase(x.Type()).(*types.Con)
	var ti *typeInfo
	if ok {
		ti = c.tyCons[conKey{qual: con.Qual, name: con.Name}]
	}
	if ti == nil || ti.kind != kindRecord {
		c.diags.Add(diag.Error("E0418",
			"a `~..` argument must be a record value").At(sp))
		*unmapped = true
		return
	}
	sub := types.Subst{}
	for i, v := range ti.params {
		if i < len(con.Args) {
			sub[v.ID] = con.Args[i]
		}
	}
	// A spread can only fill labeled slots: on an unresolved
	// function type, or a callee with no flat parameters, there is
	// nothing to match — E0408 once, not per field.
	if _, unresolved := types.Chase(fn.Type()).(*types.Meta); unresolved || len(params) == 0 {
		c.diags.Add(diag.Error("E0408",
			"a labeled call requires the callee's type to be known").At(sp))
		*unmapped = true
		return
	}
	for _, fi := range ti.fields {
		sel := &syntax.Ident{Sp: sp, Name: fi.name}
		argExpr := &MemberExpr{
			Base:  baseOf(sp, sub.Apply(fi.typ)),
			Recv:  x,
			Sel:   sel,
			Sym:   fi.sym,
			Index: fi.index,
		}
		var arg Expr = argExpr
		pi := c.paramByLabel(params, fi.name)
		switch {
		case pi < 0:
			c.diags.Add(diag.Error("E0406",
				fmt.Sprintf("the callee has no parameter labeled `~%s`", fi.name)).
				At(sp).Set("label", fi.name))
			*argToParam = append(*argToParam, -1)
			*unmapped = true
		case used[pi]:
			c.diags.Add(diag.Error("E0405",
				fmt.Sprintf("the label `~%s` is supplied twice", fi.name)).
				At(sp).Set("label", fi.name))
			*argToParam = append(*argToParam, -1)
			*unmapped = true
		default:
			used[pi] = true
			// conformExpr, not bare unify — a spread field needs
			// the same implicit conversions as a written argument
			// (`List` → `Go.Slice`, interface boxing, ...).
			arg = c.conformExpr(argExpr, params[pi].Type, sp)
			*argToParam = append(*argToParam, pi)
		}
		out.Args = append(out.Args, Arg{Label: fi.name, LabelSp: sp, Value: arg})
	}
}

// goSpreadParams returns the parameter list a `~..e` expansion
// matches against at a Go call: the variadic tail reads as its
// element type (each spread field supplies one element, matching
// the lowered goParamTypeAt shape), and every parameter's want is
// registered so chanDirConv sees the boundary during the
// expansion. The returned func lifts the registrations.
func (c *checker) goSpreadParams(goParams []types.FuncParam, variadic bool, arity int, sp diag.Span) ([]types.FuncParam, func()) {
	ps := goParams
	if variadic && arity > 0 {
		ps = slices.Clone(goParams)
		ps[arity-1].Type = sliceElemOf(goParams[arity-1].Type, sp, c)
	}
	undos := make([]func(), 0, len(ps))
	for _, p := range ps {
		undos = append(undos, c.goWant(p.Type))
	}
	return ps, func() {
		for _, u := range undos {
			u()
		}
	}
}

// paramByLabel finds the index of a labeled parameter, or -1.
func (c *checker) paramByLabel(params []types.FuncParam, label string) int {
	for i, p := range params {
		if p.Label == label {
			return i
		}
	}
	return -1
}

// normalizeMapping returns the ArgToParam slice, or nil when the
// mapping is the identity or cannot be represented (a -1 entry: an
// argument with no parameter). Validity (distinct, in-range entries)
// is the caller's; ArgToParam is only consulted in call shapes where
// every entry resolved to a declared parameter.
func normalizeMapping(m []int) []int {
	out := make([]int, len(m))
	identity := true
	for i, v := range m {
		out[i] = v
		if v != i {
			identity = false
		}
	}
	if identity {
		return nil
	}
	return out
}

// checkGoApply types a call to a Go function or method — the tuple
// argument convention of spec section 8.1. The surface argument list
// `f(a, b)` (one TupleLit argument) unpacks into the individual Go
// arguments; `f()` passes none. Go calls never curry: every declared
// parameter must be supplied — a labeled one left unfilled is E0404 —
// except a `_` placeholder, which turns the call into a one-argument
// function (the "cute" partial application of spec section 8.2).
func (c *checker) checkGoApply(e *syntax.Apply, fn Expr, callee *resolve.Symbol, params []types.FuncParam, ret types.Type, variadic bool, declArity int) Expr {
	if declArity > len(params) {
		declArity = len(params)
	}
	goParams := params[:declArity]
	arity := declArity
	variadic = variadic && arity > 0

	// Unpack the surface argument form into the Go argument list;
	// labels may arrive on the top-level arguments.
	type garg struct {
		label   string
		labelSp diag.Span
		sp      diag.Span
		x       syntax.Expr
		spread  bool // `~..e` — record fields feed labeled Go params
	}
	var gargs []garg
	one := len(e.Args) == 1
	for _, a := range e.Args {
		var label string
		var lsp diag.Span
		if a.Label != nil {
			label, lsp = a.Label.Name, a.Label.Sp
		}
		v := a.Value
		if one && label == "" && v != nil {
			switch v := v.(type) {
			case *syntax.TupleLit:
				for _, el := range v.Elems {
					gargs = append(gargs, garg{sp: el.Span(), x: el})
				}
				continue
			case *syntax.Lit:
				if v.Kind == syntax.LitUnit {
					continue // `f()` — no Go arguments
				}
			case *syntax.Ident, *syntax.StrInterp, *syntax.Apply,
				*syntax.Select, *syntax.Unary, *syntax.Binary,
				*syntax.Hole, *syntax.Lambda, *syntax.If,
				*syntax.Match, *syntax.Block, *syntax.Let,
				*syntax.Ignore, *syntax.Try, *syntax.GoExpr,
				*syntax.For, *syntax.SelectExpr, *syntax.ListLit,
				*syntax.Assign, *syntax.Spread, *syntax.Ascribe, *syntax.RecordLit:
				// An ordinary argument.
			default:
				panic("unreachable")
			}
		}
		gargs = append(gargs, garg{label: label, labelSp: lsp, sp: a.Sp, x: v, spread: a.Spread})
	}

	used := make([]bool, arity)
	posIdx := 0
	tooMany := 0
	posArgs := 0
	hole := -1
	var holeT types.Type
	mapping := make([]int, 0, len(gargs))
	out := &Apply{Base: baseOf(e.Sp, nil), Fn: fn}

	for gi, g := range gargs {
		last := gi == len(gargs)-1
		if g.spread {
			// `~..e` maps record fields onto the Go callee's labeled
			// params by name — same expansion as a Gom call.
			x := c.checkExpr(g.x, nil)
			tmp := &Apply{}
			var spMap []int
			var unm bool
			sp, undo := c.goSpreadParams(goParams, variadic, arity, g.sp)
			c.spreadApply(x, g.sp, fn, sp, used, tmp, &spMap, &unm)
			undo()
			out.Args = append(out.Args, tmp.Args...)
			mapping = append(mapping, spMap...)
			continue
		}
		pi := -1
		if g.label != "" {
			pi = c.paramByLabel(goParams, g.label)
			if pi < 0 {
				c.diags.Add(diag.Error("E0406",
					fmt.Sprintf("the Go callee has no parameter labeled `~%s`", g.label)).
					At(labelSpan(g.labelSp, g.sp)).Set("label", g.label))
			} else if used[pi] {
				c.diags.Add(diag.Error("E0405",
					fmt.Sprintf("the label `~%s` is supplied twice", g.label)).
					At(labelSpan(g.labelSp, g.sp)).Set("label", g.label))
				pi = -1
			}
		} else {
			posArgs++
			for posIdx < arity && (used[posIdx] || goParams[posIdx].Label != "") {
				posIdx++
			}
			if posIdx < arity {
				pi = posIdx
				posIdx++
			} else if variadic {
				pi = arity - 1
			} else {
				tooMany++
			}
		}
		var pt types.Type
		claimed := false
		if pi >= 0 {
			claimed = used[pi]
			used[pi] = true
			pt = goParams[pi].Type
			if variadic && pi == arity-1 {
				// Arguments of the variadic tail have the element
				// type of the declared last (slice) parameter.
				pt = sliceElemOf(pt, g.sp, c)
			}
		} else {
			// Unresolvable slot (unknown label, over-arity,
			// duplicate): E0405/E0406/the arity diagnostic already
			// reported the cause.
			pt = c.quietMeta(g.sp, "the Go argument's parameter type")
		}
		// The argument conforms at a Go boundary — the channel
		// direction conversion of spec section 10.2 may apply.
		undo := c.goWant(pt)
		var argExpr Expr
		switch av := g.x.(type) {
		case nil:
			argExpr = c.checkExpr(nil, pt)
		case *syntax.Hole:
			if hole >= 0 {
				c.diags.Add(diag.Error("E0504",
					"at most one `_` placeholder is allowed in a call").At(av.Sp))
				// A second Hole node in Args would fail
				// Apply.Validate (exactly one hole at
				// HoleIndex); recover as an ordinary argument.
				argExpr = &Lit{Base: baseOf(av.Sp, pt), Kind: syntax.LitUnit, Text: "()"}
			} else {
				hole = len(out.Args)
				holeT = pt
				argExpr = &Hole{Base: baseOf(av.Sp, pt)}
			}
		case *syntax.Spread:
			x := c.checkExpr(av.X, nil)
			switch {
			case !variadic || !last || g.label != "":
				c.diags.Add(diag.Error("E0505",
					"`..e` is only allowed as the last argument of a variadic Go call").At(av.Sp))
			case pi != arity-1 || claimed:
				// `fmt.Println("tag:", ..xs)`: Go emits `f(xs...)`
				// only when the slice supplies the whole variadic
				// tail — sharing the parameter with a written
				// argument (or missing a fixed parameter so the
				// spread never reaches it) cannot compile to Go.
				c.diags.Add(diag.Error("E0505",
					"`..e` must supply the variadic arguments alone").At(av.Sp))
			default:
				c.spreadElem(x.Type(), pt, av.Sp)
			}
			argExpr = &Spread{Base: baseOf(av.Sp, x.Type()), X: x}
		case *syntax.Ident, *syntax.Lit, *syntax.StrInterp,
			*syntax.Apply, *syntax.Select, *syntax.Unary,
			*syntax.Binary, *syntax.Lambda, *syntax.If, *syntax.Match,
			*syntax.Block, *syntax.Let, *syntax.Ignore, *syntax.Try,
			*syntax.GoExpr, *syntax.For, *syntax.SelectExpr,
			*syntax.Assign, *syntax.ListLit, *syntax.TupleLit, *syntax.Ascribe,
			*syntax.RecordLit:
			argExpr = c.checkExpr(av, pt)
		default:
			panic("unreachable")
		}
		undo()
		out.Args = append(out.Args, Arg{Label: g.label, LabelSp: g.labelSp, Value: argExpr})
		mapping = append(mapping, pi)
	}

	// Saturation check: unfilled unlabeled params join a single arity
	// diagnostic; unfilled labeled params report E0404 each — a Go
	// call cannot be partial.
	missing := 0
	posParams := 0
	for i, p := range goParams {
		if p.Label == "" {
			posParams++
		}
		if used[i] || (variadic && i == arity-1) {
			continue
		}
		if p.Label != "" {
			c.diags.Add(diag.Error("E0404",
				fmt.Sprintf("the Go callee requires `~%s:`, which the call does not supply", p.Label)).
				At(e.Sp).Set("label", p.Label))
		} else {
			missing++
		}
	}
	if missing > 0 || tooMany > 0 {
		if _, unknown := types.Chase(fn.Type()).(*types.Meta); !unknown {
			// A callee whose signature never resolved (a hidden or
			// unknown Go symbol) has no arity to argue with — its
			// own diagnostic already reported the cause, and "the
			// Go callee takes 0 argument(s)" would only mislead.
			// The counts compare positional arguments against
			// positional (unlabeled) parameters: labeled arguments
			// and unfilled labeled parameters carry their own
			// per-label diagnostics (E0404–E0406), and counting
			// them on either side yields self-contradicting
			// messages like "takes 1 argument(s), got 1".
			required := posParams
			if variadic {
				// The variadic tail is optional; only the
				// fixed prefix must be supplied.
				required--
				if required < 0 {
					required = 0
				}
			}
			want := fmt.Sprintf("%d", required)
			if variadic {
				want = fmt.Sprintf("at least %d", required)
			}
			c.diags.Add(diag.Error("E0401",
				fmt.Sprintf("the Go callee takes %s argument(s), got %d", want, posArgs)).
				At(e.Sp).Set("expected", want).Set("actual", fmt.Sprintf("%d", posArgs)))
		}
	}

	// ArgToParam is representable only when every argument resolved
	// to a distinct declared parameter — a repeated variadic slot
	// cannot be expressed and identity mappings stay nil.
	var argToParam []int
	seen := make([]bool, arity)
	ok := true
	for _, pi := range mapping {
		if pi < 0 || seen[pi] {
			ok = false
			break
		}
		seen[pi] = true
	}
	if ok {
		argToParam = normalizeMapping(mapping)
	}

	res := ret
	if hole >= 0 {
		// The cute placeholder: the call is a one-argument function.
		// A literal Func (not types.Fn) keeps a function-typed ret
		// nested — the emitted closure really is `func(hole) ret`.
		res = &types.Func{Params: []types.FuncParam{types.P(holeT)}, Ret: ret}
	}
	out.Base = baseOf(e.Sp, res)
	out.Call = CallInfo{
		Kind:       CallGo,
		Callee:     callee,
		Arity:      arity,
		Applied:    len(out.Args),
		ArgToParam: argToParam,
		HoleIndex:  hole,
		Variadic:   variadic,
		TypeArgs:   targsOf(fn),
	}
	if err := out.Validate(); err != nil {
		panic(fmt.Sprintf("check: invalid CallInfo for %T: %v", e.Fn, err))
	}
	return out
}

// sliceElemOf returns the element type of the declared variadic
// parameter — a GoSlice — or a fresh meta when the shape is unusual.
// A file-local `type List` is not the builtin the rule names.
func sliceElemOf(t types.Type, sp diag.Span, c *checker) types.Type {
	if con, ok := types.Chase(t).(*types.Con); ok && len(con.Args) == 1 &&
		((con.Qual == "Go" && con.Name == "Slice") ||
			(con.Qual == "" && con.Name == "List" && !c.shadowedBuiltin("List"))) {
		return con.Args[0]
	}
	return c.newMeta(sp, "the variadic element type")
}

// spreadElem checks the collection a `..e` spread expands: a List or
// a GoSlice whose element type unifies with the variadic parameter's
// element type. A file-local `type List` shadows the builtin the
// spread needs (E0415).
func (c *checker) spreadElem(t types.Type, elem types.Type, sp diag.Span) {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if len(t.Args) == 1 && t.Qual == "Go" && t.Name == "Slice" {
			c.unify(elem, t.Args[0], sp)
			return
		}
		if len(t.Args) == 1 && t.Qual == "" && t.Name == "List" {
			if c.shadowedBuiltin("List") {
				c.shadowedErr(sp, "the spread `..e`", "List")
				return
			}
			c.unify(elem, t.Args[0], sp)
			return
		}
	case *types.Meta:
		c.bind(t, types.GoSlice(elem), sp)
		return
	case *types.Var, *types.Func, *types.Tuple, *types.Go:
		// Not a spreadable collection.
	default:
		panic("unreachable")
	}
	c.mismatch(sp, "a List or Go.Slice", t)
}

// --- deferred applications ----------------------------------------

// pendingApply is an application whose callee is a member selection
// deferred on an unresolved receiver meta (pendingSels). The surface
// call `x.M(args)` cannot pick a calling convention while `.M`'s
// owner is unknown: resolving to a Gom field means curried
// application, resolving to a Go method means the tuple convention
// of spec section 8.1. checkDeferredApply checks the arguments
// provisionally — labels kept, `_` and `..e` recorded without their
// convention diagnostics — and replayApply rewrites Args and Call
// once revisitSelects resolves the member, or at the end of finalize
// under the Gom rule when the receiver never does.
type pendingApply struct {
	ap *Apply      // the provisional node
	me *MemberExpr // the deferred member the callee's type waits on
	// calleeT is the type the replay consumes arguments against:
	// me's standing meta when the callee is the member itself
	// (`x.M(args)`), the aliased standing meta when the callee is a
	// variable bound to the member (`let f = x.M; f args`), or the
	// result meta of an earlier pending apply (`x.M(a) (b)`). At
	// replay time it chases to the resolved signature.
	calleeT types.Type
	done    bool // replayed — guards chained members
	// dropped marks an apply abandoned as dead work when its member
	// failed — distinct from done, which replay also sets on success.
	dropped bool
	// failed marks an apply whose replay emitted diagnostics — its
	// result meta is error debris, not a live type.
	failed bool

	// alwaysGom forces the Gom replay even when the member settles
	// to a Go method: the apply's callee is a plain value (a Var
	// standing on the member's standing meta — `let m = x.Error`),
	// not the member expression itself, so no CallGo can name the
	// receiver. `m ()` applies the method *value*'s `-> T` shape.
	alwaysGom bool

	// holes and spreads record the `_` and `..e` markers the
	// provisional pass minted (only direct arguments and elements
	// of the surface arg tuple carry them — every other context
	// reported the marker eagerly). The replay consumes the ones
	// the settled convention admits and flagStrayMarkers reports
	// the rest.
	holes   []*Hole
	spreads []*Spread
	used    map[Expr]bool
}

// useMarker marks a provisional marker as consumed — it reached a
// legal position under the settled convention or was already
// reported by the replay's own switch.
func (pa *pendingApply) useMarker(e Expr) {
	if pa.used == nil {
		pa.used = map[Expr]bool{}
	}
	pa.used[e] = true
}

// checkDeferredApply types `x.M(args)` while `x`'s type — and hence
// `.M`'s owner — is still a meta. Arguments check with no parameter
// expectation: their own inference still runs (a `42` literal still
// defaults), only the conform step waits for the convention. The
// node's standing result meta is what context constraints see; the
// replay unifies it with the convention-correct result.
func (c *checker) checkDeferredApply(e *syntax.Apply, me *MemberExpr) Expr {
	pa := &pendingApply{me: me, calleeT: me.Type()}
	out := &Apply{Base: baseOf(e.Sp, c.newMeta(e.Sp, "the deferred call's result")), Fn: me}
	pa.ap = out
	for _, a := range e.Args {
		label := ""
		var labelSp diag.Span
		if a.Label != nil {
			label = a.Label.Name
			labelSp = a.Label.Sp
		}
		out.Args = append(out.Args, Arg{
			Label: label, LabelSp: labelSp,
			Value:  c.provisionalArg(a.Value, pa),
			Spread: a.Spread,
		})
	}
	// The provisional CallInfo claims nothing: a CallUnknown with no
	// callee — valid for walkers that inspect it before finalize —
	// and replayApply replaces it with a validated record.
	out.Call = CallInfo{Kind: CallUnknown, Applied: len(out.Args), HoleIndex: -1}
	c.pendingApplies = append(c.pendingApplies, pa)
	return out
}

// provisionalArg checks one argument of a deferred application
// without the convention a parameter type would supply: `_` and
// `..e` keep their markers (the replay's convention decides whether
// they were legal — E0504/E0505 withheld until then), a `(a, b, ...)`
// argument tuple keeps its surface unpackability by checking the
// elements the same way, and everything else checks normally.
func (c *checker) provisionalArg(e syntax.Expr, pa *pendingApply) Expr {
	switch av := e.(type) {
	case nil:
		// A recovery hole in the surface tree.
		return c.checkExpr(nil, nil)
	case *syntax.Hole:
		// `_` is legal only under the Go convention — which the
		// replay may yet select — so the Hole is kept and E0504
		// waits for the convention decision.
		h := &Hole{Base: baseOf(av.Sp, c.newMeta(av.Sp, "the placeholder's type"))}
		pa.holes = append(pa.holes, h)
		return h
	case *syntax.Spread:
		x := c.checkExpr(av.X, nil)
		sp := &Spread{Base: baseOf(av.Sp, x.Type()), X: x}
		pa.spreads = append(pa.spreads, sp)
		return sp
	case *syntax.TupleLit:
		// `f(_, b)` packs the `_` inside the argument tuple — the
		// eager Go path unpacks the *surface* tuple, so the
		// provisional one must keep its elements' markers too.
		out := &TupleLit{Base: baseOf(av.Sp, nil), Elems: make([]Expr, len(av.Elems))}
		ts := make([]types.Type, len(av.Elems))
		for i, el := range av.Elems {
			out.Elems[i] = c.provisionalArg(el, pa)
			ts[i] = out.Elems[i].Type()
		}
		out.Base = baseOf(av.Sp, types.Tup(ts...))
		return out
	case *syntax.Ident, *syntax.Lit, *syntax.StrInterp,
		*syntax.Apply, *syntax.Select, *syntax.Unary,
		*syntax.Binary, *syntax.Lambda, *syntax.If, *syntax.Match,
		*syntax.Block, *syntax.Let, *syntax.Ignore, *syntax.Try,
		*syntax.GoExpr, *syntax.For, *syntax.SelectExpr,
		*syntax.Assign, *syntax.ListLit, *syntax.Ascribe, *syntax.RecordLit:
		return c.checkExpr(e, nil)
	default:
		panic("unreachable")
	}
}

// replayAppliesOf runs the convention replay of every deferred
// application whose callee member is me — called by revisitSelects
// right after it resolves me, so a member chained off the call's
// result (`x.M(1).N(2)`) sees the bound result type when its own
// turn comes.
func (c *checker) replayAppliesOf(me *MemberExpr) {
	for _, pa := range c.pendingApplies {
		if pa.me == me {
			c.replayApply(pa)
		}
	}
}

// replayResolvedApplies replays the deferred applications whose
// member already settled — replayAppliesOf covers members resolved
// inside revisitSelects; this sweep catches applies a re-checked
// fill registered on an already-settled member. A replay's
// unifications can pin the receiver another apply or selection
// waits on, so callers drive it inside a fixpoint.
func (c *checker) replayResolvedApplies() int {
	n := 0
	for _, pa := range c.pendingApplies {
		if pa.done || pa.me.Sym == nil {
			continue
		}
		c.replayApply(pa)
		n++
	}
	return n
}

// replayUnresolvedApplies commits the still-undecided applications
// to the Gom convention — the convention diagnostics the first
// pass withheld (a `_`, a label on an unknown callee) fire and
// lowering sees a valid record. Call it only once nothing can
// bind receivers any more: an early commit pins the member's
// standing meta to a Gom shape (`Unit -> ?r`), so a member that
// resolves later both loses its Go convention and mis-unifies.
func (c *checker) replayUnresolvedApplies() int {
	n := 0
	for _, pa := range c.pendingApplies {
		if pa.done || pa.me.Sym != nil {
			continue
		}
		c.replayApply(pa)
		n++
	}
	return n
}

// replayApply gives a deferred application its final Args and
// CallInfo. A member that resolved to a Go method replays the Go
// tuple convention; anything else — a record or Go struct field
// (whose func-typed value curries), an unresolved receiver, a failed
// lookup — replays the Gom rule.
func (c *checker) replayApply(pa *pendingApply) {
	if pa.done {
		return
	}
	pa.done = true
	diagMark := len(c.diags)
	// The Go method replay builds a CallGo naming the member's
	// receiver — it applies only when the callee *is* the member
	// (`x.M(args)`). A variable or call result standing on the
	// member's type carries the receiver inside its own value, so
	// it replays the Gom rule even when the member settled to a
	// Go method (the callee's signature already holds the arity).
	if me := pa.me; !pa.alwaysGom && pa.ap.Fn == Expr(me) && me.Sym != nil && me.Sym.IsGo && me.Sym.Kind == resolve.KindMethod {
		c.replayGoApply(pa)
	} else {
		c.replayGomApply(pa)
	}
	pa.failed = len(c.diags) > diagMark
}

// replayGoApply is checkGoApply's second pass over an already-typed
// argument list: the surface argument tuple unpacks the same way
// (the typed TupleLit of a single argument carries the individual Go
// arguments), parameter matching and all of E0404–E0406 and the
// arity diagnostic run identically, and the conform step unifies
// each typed argument's solved type with its parameter — no
// re-checking, so inference the provisional pass seeded is not
// replayed. The node's standing result meta unifies with the
// method's result (wrapped in the hole's one-parameter function for
// a cute call).
func (c *checker) replayGoApply(pa *pendingApply) {
	e := pa.ap
	me := pa.me
	params, ret := flattenFunc(me.Type())
	sig, _ := c.sigOf(me.Sym, me.Sel.Sp)
	declArity := min(c.declArity(sig, len(params)), len(params))
	goParams := params[:declArity]
	arity := declArity
	variadic := sig.Variadic && arity > 0

	// Unpack the surface argument form into the Go argument list;
	// labels may arrive on the top-level arguments.
	type garg struct {
		label   string
		labelSp diag.Span
		sp      diag.Span
		x       Expr
		spread  bool // `~..e` — expand to labeled args at replay
	}
	var gargs []garg
	one := len(e.Args) == 1
	for _, a := range e.Args {
		// Partial probes stay as if-assertions: check.Expr is a
		// sumtype and the checker requires exhaustive switches.
		if one && a.Label == "" {
			if v, ok := a.Value.(*TupleLit); ok {
				for _, el := range v.Elems {
					gargs = append(gargs, garg{sp: el.Span(), x: el})
				}
				continue
			}
			if v, ok := a.Value.(*Lit); ok && v.Kind == syntax.LitUnit {
				continue // `f()` — no Go arguments
			}
		}
		gargs = append(gargs, garg{label: a.Label, labelSp: a.LabelSp, sp: a.Span(), x: a.Value, spread: a.Spread})
	}

	used := make([]bool, arity)
	posIdx := 0
	tooMany := 0
	posArgs := 0
	hole := -1
	var holeT types.Type
	mapping := make([]int, 0, len(gargs))
	newArgs := make([]Arg, 0, len(gargs))

	for gi, g := range gargs {
		last := gi == len(gargs)-1
		if g.spread {
			tmp := &Apply{}
			var spMap []int
			var unm bool
			sp, undo := c.goSpreadParams(goParams, variadic, arity, g.sp)
			c.spreadApply(g.x, g.sp, me, sp, used, tmp, &spMap, &unm)
			undo()
			newArgs = append(newArgs, tmp.Args...)
			mapping = append(mapping, spMap...)
			continue
		}
		pi := -1
		if g.label != "" {
			pi = c.paramByLabel(goParams, g.label)
			if pi < 0 {
				c.diags.Add(diag.Error("E0406",
					fmt.Sprintf("the Go callee has no parameter labeled `~%s`", g.label)).
					At(labelSpan(g.labelSp, g.sp)).Set("label", g.label))
			} else if used[pi] {
				c.diags.Add(diag.Error("E0405",
					fmt.Sprintf("the label `~%s` is supplied twice", g.label)).
					At(labelSpan(g.labelSp, g.sp)).Set("label", g.label))
				pi = -1
			}
		} else {
			posArgs++
			for posIdx < arity && (used[posIdx] || goParams[posIdx].Label != "") {
				posIdx++
			}
			if posIdx < arity {
				pi = posIdx
				posIdx++
			} else if variadic {
				pi = arity - 1
			} else {
				tooMany++
			}
		}
		var pt types.Type
		claimed := false
		if pi >= 0 {
			claimed = used[pi]
			used[pi] = true
			pt = goParams[pi].Type
			if variadic && pi == arity-1 {
				// Arguments of the variadic tail have the element
				// type of the declared last (slice) parameter.
				pt = sliceElemOf(pt, g.sp, c)
			}
		} else {
			pt = c.quietMeta(g.sp, "the Go argument's parameter type")
		}
		// The argument conforms at a Go boundary — the channel
		// direction conversion of spec section 10.2 may apply.
		undo := c.goWant(pt)
		var argExpr Expr
		switch v := g.x.(type) {
		case *Hole:
			pa.useMarker(v)
			if hole >= 0 {
				c.diags.Add(diag.Error("E0504",
					"at most one `_` placeholder is allowed in a call").At(v.Sp))
				// A second Hole node in Args would fail
				// Apply.Validate (exactly one hole at
				// HoleIndex); recover as an ordinary argument.
				c.markQuiet(v.Type())
				argExpr = &Lit{Base: baseOf(v.Sp, pt), Kind: syntax.LitUnit, Text: "()"}
			} else {
				hole = len(newArgs)
				holeT = pt
				// The hole's own meta must take the parameter type
				// even when it is `any`: unify's want-side Go.Any
				// acceptance deliberately never binds the *got*
				// meta, so unifying (pt, hole) leaves the
				// placeholder unsolved (E0407). The hole's type
				// genuinely is pt — bind in this direction.
				c.unify(v.Type(), pt, v.Sp)
				argExpr = v
			}
		case *Spread:
			pa.useMarker(v)
			switch {
			case !variadic || !last || g.label != "":
				c.diags.Add(diag.Error("E0505",
					"`..e` is only allowed as the last argument of a variadic Go call").At(v.Sp))
			case pi != arity-1 || claimed:
				// Same constraint as checkGoApply: `x.M("t:", ..xs)`
				// shares the variadic parameter with a written
				// argument and has no Go spelling.
				c.diags.Add(diag.Error("E0505",
					"`..e` must supply the variadic arguments alone").At(v.Sp))
			default:
				c.spreadElem(v.X.Type(), pt, v.Sp)
			}
			argExpr = v
		case *Var, *Lit, *StrInterp, *Apply, *MemberExpr, *Unary,
			*Binary, *Lambda, *If, *Match, *Block, *Let, *Ignore,
			*Try, *FieldAssign, *GoExpr, *For, *SelectExpr, *ListLit, *TupleLit,
			*Ascribe, *RecordLit:
			argExpr = c.conformExpr(v, pt, g.sp)
		default:
			panic("unreachable")
		}
		undo()
		newArgs = append(newArgs, Arg{Label: g.label, LabelSp: g.labelSp, Value: argExpr})
		mapping = append(mapping, pi)
	}

	// Saturation check: unfilled unlabeled params join a single arity
	// diagnostic; unfilled labeled params report E0404 each — a Go
	// call cannot be partial.
	missing := 0
	posParams := 0
	for i, p := range goParams {
		if p.Label == "" {
			posParams++
		}
		if used[i] || (variadic && i == arity-1) {
			continue
		}
		if p.Label != "" {
			c.diags.Add(diag.Error("E0404",
				fmt.Sprintf("the Go callee requires `~%s:`, which the call does not supply", p.Label)).
				At(e.Sp).Set("label", p.Label))
		} else {
			missing++
		}
	}
	if missing > 0 || tooMany > 0 {
		if _, unknown := types.Chase(me.Type()).(*types.Meta); !unknown {
			required := posParams
			if variadic {
				required--
				if required < 0 {
					required = 0
				}
			}
			want := fmt.Sprintf("%d", required)
			if variadic {
				want = fmt.Sprintf("at least %d", required)
			}
			c.diags.Add(diag.Error("E0401",
				fmt.Sprintf("the Go callee takes %s argument(s), got %d", want, posArgs)).
				At(e.Sp).Set("expected", want).Set("actual", fmt.Sprintf("%d", posArgs)))
		}
	}

	var argToParam []int
	seen := make([]bool, arity)
	ok := true
	for _, pi := range mapping {
		if pi < 0 || seen[pi] {
			ok = false
			break
		}
		seen[pi] = true
	}
	if ok {
		argToParam = normalizeMapping(mapping)
	}

	res := ret
	if hole >= 0 {
		// The cute placeholder: the call is a one-argument function.
		// A literal Func (not types.Fn) keeps a function-typed ret
		// nested — the emitted closure really is `func(hole) ret`.
		res = &types.Func{Params: []types.FuncParam{types.P(holeT)}, Ret: ret}
	}
	c.unifyDeferredResult(e, res, hole >= 0)
	e.Args = newArgs
	e.Call = CallInfo{
		Kind:       CallGo,
		Callee:     me.Sym,
		Arity:      arity,
		Applied:    len(newArgs),
		ArgToParam: argToParam,
		HoleIndex:  hole,
		Variadic:   variadic,
		TypeArgs:   me.TypeArgs,
	}
	c.flagStrayMarkers(pa)
	if err := e.Validate(); err != nil {
		panic(fmt.Sprintf("check: invalid CallInfo for %T: %v", e.Fn, err))
	}
}

// calleeTail returns the residual type after consuming the first
// positional parameter of callee. The bool is false for a callee
// shape conformCallee handles through the synthesized function-type
// fallback instead.
func calleeTail(callee types.Type) (types.Type, bool) {
	fs, ok := types.Chase(callee).(*types.Func)
	if !ok || len(fs.Params) == 0 || fs.Params[0].Label != "" {
		return nil, false
	}
	if len(fs.Params) > 1 {
		return &types.Func{Params: fs.Params[1:], Ret: fs.Ret}, true
	}
	return fs.Ret, true
}

// conformCalleeAfterParamFailure performs only the result-side half
// of conformCallee when the caller already knows the first parameter
// conformance failed and reported that failure. It keeps the ordinary
// recovery type of the application without re-reporting the parameter
// mismatch, and returns false because the application itself is invalid.
func (c *checker) conformCalleeAfterParamFailure(callee, res types.Type, sp diag.Span) bool {
	if tail, ok := calleeTail(callee); ok {
		c.unify(res, tail, sp)
	}
	return false
}

// conformCallee checks a one-argument application of `callee`: its
// first positional parameter accepts `argT` under the conformance a
// direct call applies — a `Go.Any` parameter takes every argument
// (spec 8.1.7), a `Go.Slice`/`List` or interface pair takes its
// boundary conversion, and other pairs unify — and the callee's
// residual (the remaining curried parameters, or the bare result)
// unifies with `res`, the caller's synthesized expectation.
//
// Synthesizing `argT -> res` and unifying whole function types is
// *not* equivalent: unifyFunc's parameter pairs are structural now
// (two function types are equal only parameter-for-parameter —
// `func(any) int64` is not `func(string) int64`), so a `Go.Any`
// parameter would reject the argument's concrete type. Application
// is not equality — the pipe, overApply and `|?>` go through here
// instead of types.Fn synthesis. A callee that is still a meta or
// one whose first parameter is labeled falls back to the
// synthesized `argT -> res` unify, which reports the shape
// mismatch (or binds the meta) the same way it did before. A
// `Go.Any` callee never reaches that fallback: `unify`'s early
// accept reads "an `any` slot takes every argument", which answers
// the wrong question here — Go cannot call an `interface{}`, so the
// structural judgment reports the mismatch instead of admitting an
// `a("s")` the emitter then writes verbatim.
// conv reports whether the caller can carry the slice-boundary
// conversions conformParam admits on a real argument expression:
// `|>` and overApply wrap their argument in the Ascribe
// conformFirstArg emits, so they pass true; `>>`/`<<` and `|?>`
// apply a value that exists only inside the lowering (the middle
// application result, the unwrapped payload), so they pass false
// and the List↔Go.Slice boundary stays an honest unify mismatch —
// admitting it would emit the raw List where the callee takes a
// Go.Slice. (Interface assignability materializes no conversion,
// so conv=false still admits it — see conformParam.)
func (c *checker) conformCallee(callee, argT, res types.Type, sp diag.Span, conv bool, arg Expr) bool {
	if tail, ok := calleeTail(callee); ok {
		fs := types.Chase(callee).(*types.Func)
		// A literal argument can still materialize into a defined
		// first parameter — the type-only conformParam cannot see
		// the literalness, so scalarLitConv decides first (it
		// reports nothing); conformFirstArg re-emits the same
		// Ascribe for the wrap when the call is admitted.
		if !(conv && arg != nil && c.scalarLitConv(fs.Params[0].Type, arg, arg.Span()) != nil) &&
			!c.conformParam(fs.Params[0].Type, argT, sp, conv) {
			return false
		}
		return c.unify(res, tail, sp)
	}
	// The synthesized `argT -> res` unify asks "does this
	// application type-check", not "are these types equal": a
	// labeled callee parameter must not lend its label to the
	// positional argument the synthesize side stands for — that
	// would let `id mk 3` fill `~x` positionally (spec section 2
	// rule 3). Introduction is a type-equality rule, so it stays
	// off for this unify.
	c.noLabelIntro = true
	defer func() { c.noLabelIntro = false }()
	if isGoAny(types.Chase(callee)) {
		return c.unifySub(callee, types.Fn(res, types.P(argT)), sp)
	}
	return c.unify(callee, types.Fn(res, types.P(argT)), sp)
}

// conformParam is the type-level half of argument conformance —
// what `fn arg` admits against the callee's first parameter — for
// the type-only application check (conformCallee has no argument
// expression to wrap; conformFirstArg emits the matching Ascribe
// once the check passes — conv=false callers admit only what unify
// admits since no Ascribe can be hung). The admitted pairs mirror conformExpr:
// ordinary unification first, then the boundary conversions a
// settled argument type can take — `List`/`Go.Slice` in either
// direction (element equality or an `any` element box) and a
// Go-origin value assignable to a Go interface parameter. The
// channel-direction narrowing stays gated to live Go parameters
// (goParamWants), which a Gom callee's parameter is not.
func (c *checker) conformParam(want, got types.Type, sp diag.Span, conv bool) bool {
	if conv && c.slicePair(want, got, sp) {
		return true
	}
	w, g := types.Chase(want), types.Chase(got)
	if !types.Equal(w, g) && c.goOriginType(w) && !isGoAny(w) {
		if c.goOriginType(g) {
			if src, ok := c.src.(InterfaceSource); ok && src.InterfaceAssignable(g, w) {
				// Interface assignability materializes nothing —
				// the emitted atom is already assignable at the Go
				// call, so conv=false callers admit it too.
				return true
			}
		} else if c.implementsIface(w, g) {
			// A Gom type whose @method set covers the interface —
			// assignable as is, like the Go-to-Go case above.
			return true
		}
	}
	return c.unify(want, got, sp)
}

// slicePair reports whether `got` conforms to `want` through the
// slice/list boundary conversion — the type-level decision of
// sliceConv without an expression to wrap. `List T` copies into a
// `Go.Slice T` and back (`Go.Slice T` materializing into `List T`),
// and an `any`-element `Go.Slice` takes a concrete-element list or
// slice by boxing each element. Element metas bind through the
// ordinary unify so `Go.Slice a -> R` applied to a `List String`
// pins `a := String`.
func (c *checker) slicePair(want, got types.Type, sp diag.Span) bool {
	wc, _ := types.Chase(want).(*types.Con)
	gc, _ := types.Chase(got).(*types.Con)
	if wc == nil || gc == nil || len(wc.Args) != 1 || len(gc.Args) != 1 {
		return false
	}
	isGoSlice := func(con *types.Con) bool { return con.Qual == "Go" && con.Name == "Slice" }
	isList := func(con *types.Con) bool {
		return con.Qual == "" && con.Name == "List" && !c.shadowedBuiltin("List")
	}
	var we, ge types.Type
	switch {
	case isGoSlice(wc) && (isList(gc) || isGoSlice(gc)):
		we, ge = wc.Args[0], gc.Args[0]
		if isGoAny(types.Chase(we)) && !isGoAny(types.Chase(ge)) {
			// `[]any` accepts any element — the box materializes
			// element-wise at the emit.
			return true
		}
	case isList(wc) && isGoSlice(gc):
		we, ge = wc.Args[0], gc.Args[0]
	default:
		return false
	}
	if types.Equal(we, ge) {
		return true
	}
	_, wmeta := types.Chase(we).(*types.Meta)
	_, gmeta := types.Chase(ge).(*types.Meta)
	if !wmeta && !gmeta {
		return false
	}
	return c.unify(we, ge, sp)
}

// conformFirstArg wraps `arg` for the callee's first positional
// parameter — the same conformance a direct call applies to its
// argument: a `Go.Any` parameter boxes (materializing a numeric
// first, so `42 |> ga` emits `ga(any(int64(42)))` like `ga 42`),
// an interface or `Go.Slice`/`Chan` parameter takes its boundary
// conversion. When the callee's first parameter is not statically
// known (a meta, a non-function, a labeled parameter) the argument
// stands as checked — the synthesized `argT -> res` unify in
// conformCallee covers the shape.
func (c *checker) conformFirstArg(callee types.Type, arg Expr) Expr {
	if fs, ok := types.Chase(callee).(*types.Func); ok &&
		len(fs.Params) > 0 && fs.Params[0].Label == "" {
		return c.conformExpr(arg, fs.Params[0].Type, arg.Span())
	}
	return arg
}

// unifyDeferredResult settles the deferred application's standing
// result meta against the replayed convention's result type. The
// meta is usually still open and simply binds; but a consumer that
// ran before the member resolved may have pinned it already — `f
// "a"` binds `String -> ?r` where the settled call is `any ->
// Unit`. The hole's `any` parameter accepts every argument, so the
// pin is a valid specialization the eager path accepts directly
// (problems/2026-11-05-any-hole-deferred-result): conform the
// pinned first parameter to `any` on the want side — which accepts
// a concrete type and leaves a still-open meta free — and unify
// the tails. A bound meta pinned to a narrower type stays the
// narrowing (monomorphic: `f` is seen at `String -> Unit`).
func (c *checker) unifyDeferredResult(e *Apply, res types.Type, hasHole bool) {
	if hasHole {
		bf, bok := types.Chase(e.Type()).(*types.Func)
		rf, rok := types.Chase(res).(*types.Func)
		if bok && rok && len(rf.Params) == 1 && len(bf.Params) > 0 &&
			isGoAny(rf.Params[0].Type) {
			c.unify(rf.Params[0].Type, bf.Params[0].Type, e.Sp)
			var tail types.Type = bf.Ret
			if len(bf.Params) > 1 {
				tail = &types.Func{Params: bf.Params[1:], Ret: bf.Ret}
			}
			c.unify(tail, rf.Ret, e.Sp)
			return
		}
	}
	switch c.unifySlicePair(e.Type(), res, e.Sp) {
	case 0:
		c.unify(e.Type(), res, e.Sp)
	case 2:
		// A consumer pinned the result to `Go.Slice` before the
		// callee's `List` arrived — the lowering still owes the
		// boundary copy.
		e.CopyFrom = res
	}
}

// replayGomApply is checkGomApply's second pass: the member resolved
// to a func-typed value (or never resolved — the standing meta
// becomes the curried chain, as the first-pass over-application path
// would have produced). Convention diagnostics the provisional pass
// withheld fire here: E0504 for a `_`, E0505 for a `..e`, the label
// errors E0405/E0406/E0408.
func (c *checker) replayGomApply(pa *pendingApply) {
	e := pa.ap
	params, ret := flattenFunc(pa.calleeT)
	flat := len(params)
	used := make([]bool, flat)
	posIdx := 0
	argToParam := make([]int, 0, len(e.Args))
	var extras []Expr
	var extraIdx []int // each extra's index in newArgs
	unmapped := false  // an argument no flat parameter position answers
	newArgs := make([]Arg, 0, len(e.Args))

	// The `-> T` value rule of checkGomApply, second pass: a Go
	// zero-argument function *value* — a member that settled to a
	// `-> T` — applied as `x.M ()` saturates; the unit literal is
	// the call marker, not an argument.
	if flat == 0 && len(e.Args) == 1 && e.Args[0].Label == "" {
		if fty, ok := types.Chase(pa.calleeT).(*types.Func); ok && len(fty.Params) == 0 {
			if lit, ok := e.Args[0].Value.(*Lit); ok && lit.Kind == syntax.LitUnit {
				c.unify(e.Type(), ret, e.Sp)
				e.Args = nil
				e.Call = CallInfo{
					Kind:      CallUnknown,
					Arity:     flat,
					Applied:   0,
					HoleIndex: -1,
				}
				c.flagStrayMarkers(pa)
				if err := e.Validate(); err != nil {
					panic(fmt.Sprintf("check: invalid CallInfo for %T: %v", e.Fn, err))
				}
				return
			}
		}
	}

	for _, a := range e.Args {
		if a.Spread {
			// `~..e` survived the provisional pass on Arg.Spread —
			// expand it now that the callee's convention is decided.
			tmp := &Apply{}
			var spMap []int
			c.spreadApply(a.Value, a.Span(), e.Fn, params, used, tmp, &spMap, &unmapped)
			newArgs = append(newArgs, tmp.Args...)
			argToParam = append(argToParam, spMap...)
			continue
		}
		label := a.Label
		labelSp := a.LabelSp
		pi := -1
		if label != "" {
			pi = c.paramByLabel(params, label)
			if pi < 0 {
				if _, unresolved := types.Chase(pa.calleeT).(*types.Meta); unresolved || flat == 0 {
					c.diags.Add(diag.Error("E0408",
						"a labeled call requires the callee's type to be known").
						At(labelSpan(labelSp, a.Span())).Set("label", label))
				} else {
					c.diags.Add(diag.Error("E0406",
						fmt.Sprintf("the callee has no parameter labeled `~%s`", label)).
						At(labelSpan(labelSp, a.Span())).Set("label", label))
				}
			} else if used[pi] {
				c.diags.Add(diag.Error("E0405",
					fmt.Sprintf("the label `~%s` is supplied twice", label)).
					At(labelSpan(labelSp, a.Span())).Set("label", label))
				pi = -1
			}
		} else {
			for posIdx < flat && (used[posIdx] || params[posIdx].Label != "") {
				posIdx++
			}
			if posIdx < flat {
				pi = posIdx
				posIdx++
			}
		}
		var pt types.Type
		switch {
		case pi >= 0:
			used[pi] = true
			pt = params[pi].Type
		case label == "":
			// A positional argument beyond the callee's parameters
			// over-applies into the returned function value.
			pt = c.newMeta(a.Span(), "an over-applied argument's parameter")
		default:
			// The label already reported E0406/E0408 — the
			// argument's parameter slot is recovery.
			pt = c.quietMeta(a.Span(), fmt.Sprintf("the argument `~%s`", label))
		}
		var argExpr Expr
		switch v := a.Value.(type) {
		case *Hole:
			pa.useMarker(v)
			c.diags.Add(diag.Error("E0504",
				"`_` is only allowed as an argument of a Go call").At(v.Sp))
			// A Hole in Args would fail Apply.Validate on a non-Go
			// call — recover with a unit literal of the slot type.
			c.markQuiet(v.Type())
			argExpr = &Lit{Base: baseOf(v.Sp, pt), Kind: syntax.LitUnit, Text: "()"}
		case *Spread:
			pa.useMarker(v)
			c.diags.Add(diag.Error("E0505",
				"`..e` is only allowed as the last argument of a variadic Go call").At(v.Sp))
			argExpr = v
		case *Var, *Lit, *StrInterp, *Apply, *MemberExpr, *Unary,
			*Binary, *Lambda, *If, *Match, *Block, *Let, *Ignore,
			*Try, *FieldAssign, *GoExpr, *For, *SelectExpr, *ListLit, *TupleLit,
			*Ascribe, *RecordLit:
			argExpr = c.conformExpr(v, pt, a.Span())
		default:
			panic("unreachable")
		}
		newArgs = append(newArgs, Arg{Label: label, LabelSp: labelSp, Value: argExpr})
		argToParam = append(argToParam, pi)
		if pi < 0 {
			if label == "" {
				extras = append(extras, argExpr)
				extraIdx = append(extraIdx, len(newArgs)-1)
			}
			unmapped = true
		}
	}

	var res types.Type
	if len(extras) > 0 {
		res = c.overApply(ret, extras, extraIdx, newArgs, e.Sp)
	} else {
		res = applyResidual(pa.calleeT, used, 0)
	}
	c.unify(e.Type(), res, e.Sp)
	e.Args = newArgs
	e.Call = CallInfo{
		Kind:      CallUnknown,
		Arity:     flat,
		Applied:   len(newArgs),
		HoleIndex: -1,
	}
	// A member value's arity is its whole signature, so every
	// resolved argument already maps inside the flat list; the
	// mapping drops only for arguments no position answers.
	if !unmapped {
		e.Call.ArgToParam = normalizeMapping(argToParam)
	}
	c.flagStrayMarkers(pa)
	if err := e.Validate(); err != nil {
		panic(fmt.Sprintf("check: invalid CallInfo for %T: %v", e.Fn, err))
	}
}

// flagStrayMarkers reports the convention diagnostics for the
// provisional `_`/`..e` markers the replay did not consume — a `_`
// under a Gom convention, or either marker nested inside an argument
// tuple below the Go argument list's own level. The marker node stays
// in the tree (the eager pass leaves the same recovery shape — a
// quiet-meta Hole lowering's stray-placeholder guard also covers).
func (c *checker) flagStrayMarkers(pa *pendingApply) {
	for _, h := range pa.holes {
		if pa.used[h] {
			continue
		}
		c.diags.Add(diag.Error("E0504",
			"`_` is only allowed as an argument of a Go call").At(h.Sp))
		c.markQuiet(h.Type())
	}
	for _, sp := range pa.spreads {
		if pa.used[sp] {
			continue
		}
		c.diags.Add(diag.Error("E0505",
			"`..e` is only allowed as the last element of a list literal or the last argument of a variadic Go call").At(sp.Sp))
	}
}
