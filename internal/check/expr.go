package check

import (
	"fmt"
	"go/constant"
	"go/token"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// checkExpr checks a surface expression and returns the typed node.
// want is the type the context expects — a concrete type or a meta —
// or nil when nothing is expected. The result unifies with want
// before returning.
func (c *checker) checkExpr(e syntax.Expr, want types.Type) Expr {
	if e == nil {
		// A missing expression (a .gomi signature body, a recovery
		// hole in the surface tree) is a meta-typed unit. Quiet:
		// the hole's own diagnostic already ran.
		m := c.quietMeta(diag.Span{}, "a missing expression")
		if want != nil {
			c.unify(want, m, diag.Span{})
		}
		return &Lit{Base: baseOf(diag.Span{}, m), Kind: syntax.LitUnit, Text: "()"}
	}
	var out Expr
	// The branching checkers (if/match/select) stamp `want` on the
	// node itself and thread it into every arm's body — under a
	// Go `any` expectation (a `...any` Go-call argument) that would
	// pin Go.Any on the node while the branches keep their natural
	// types, which lower's node-type invariants then reject (E0901).
	// `any` bounds without seeding: infer the branch type, and the
	// trailing unify below conforms it — an `any` slot accepts
	// every type (spec 8.1.7).
	bw := want
	if w := types.Chase(bw); isGoAny(w) {
		bw = nil
	}
	switch e := e.(type) {
	case *syntax.Ident:
		out = c.checkIdent(e)
	case *syntax.Lit:
		out = &Lit{Base: baseOf(e.Sp, c.litType(e)), Kind: e.Kind, Text: e.Text}
	case *syntax.StrInterp:
		out = c.checkStrInterp(e)
	case *syntax.Apply:
		out = c.checkApply(e)
	case *syntax.Select:
		out = c.checkSelect(e)
	case *syntax.Unary:
		out = c.checkUnary(e)
	case *syntax.Binary:
		out = c.checkBinary(e)
	case *syntax.Hole:
		// `_` is only legal as a Go-call argument; the apply checker
		// never calls checkExpr on one, so reaching here is misplaced.
		c.diags.Add(diag.Error("E0504",
			"`_` is only allowed as an argument of a Go call").At(e.Sp))
		out = &Hole{Base: baseOf(e.Sp, c.quietMeta(e.Sp, "the placeholder's type"))}
	case *syntax.Lambda:
		out = c.checkLambda(e)
	case *syntax.If:
		out = c.checkIf(e, bw)
	case *syntax.Match:
		out = c.checkMatch(e, bw)
	case *syntax.Block:
		out = c.checkBlock(e, want)
	case *syntax.Let:
		out = c.checkLet(e)
	case *syntax.Ignore:
		x := c.checkExpr(e.X, nil)
		out = &Ignore{Base: baseOf(e.Sp, types.Unit), X: x}
	case *syntax.Try:
		out = c.checkTry(e)
	case *syntax.GoExpr:
		out = c.checkGoExpr(e)
	case *syntax.For:
		out = c.checkFor(e)
	case *syntax.SelectExpr:
		out = c.checkSelectExpr(e, bw)
	case *syntax.ListLit:
		out = c.checkListLit(e, want)
	case *syntax.Spread:
		// A `..e` outside a list literal or a variadic Go argument.
		x := c.checkExpr(e.X, nil)
		c.diags.Add(diag.Error("E0505",
			"`..e` is only allowed as the last element of a list literal or the last argument of a variadic Go call").
			At(e.Sp))
		out = &Spread{Base: baseOf(e.Sp, x.Type()), X: x}
	case *syntax.TupleLit:
		out = c.checkTupleLit(e)
	case *syntax.RecordLit:
		out = c.checkRecordLit(e, bw)
	case *syntax.Ascribe:
		out = c.checkAscribe(e)
	default:
		panic("unreachable")
	}
	return c.conformExpr(out, want, e.Span())
}

// conformExpr applies the want-context conformance to an already
// typed expression — the trailing half of checkExpr, shared with the
// deferred-apply replay whose arguments checked provisionally. An
// interface-typed Go want may route through pendingIfaces or an
// Ascribe conversion; a mismatch reports at sp and quiets want so no
// E0407 piles on.
func (c *checker) conformExpr(out Expr, want types.Type, sp diag.Span) Expr {
	if want == nil {
		return out
	}
	// Go.Any keeps its dedicated all-Gom-values boxing path.
	ifaceWant := c.goOriginType(want) && !isGoAny(types.Chase(want))
	if m, ok := types.Chase(out.Type()).(*types.Meta); ok && c.deferInterfaceSource(m) && ifaceWant {
		if src, ok := c.src.(InterfaceSource); ok && src.InterfaceAssignable(want, want) {
			conv := &Ascribe{Base: baseOf(sp, want), X: out, Conv: true, Iface: true}
			c.pendingIfaces = append(c.pendingIfaces, conv)
			return conv
		}
	}
	if !types.Equal(want, out.Type()) {
		if src, ok := c.src.(InterfaceSource); ok && c.goOriginType(out.Type()) && ifaceWant && src.InterfaceAssignable(out.Type(), want) {
			return &Ascribe{Base: baseOf(sp, want), X: out, Conv: true, Iface: true}
		}
		if conv := c.sliceConv(want, out.Type(), out, sp); conv != nil {
			return conv
		}
		if conv := c.chanDirConv(want, out, sp); conv != nil {
			return conv
		}
	}
	// A failed expectation left want unsolved: silence it — the
	// mismatch diagnostic at sp already tells the user what to fix.
	// The silence reaches every meta inside want, not just a top
	// one: `Chan.close (Chan.recvOnly ch)` reports "expected
	// SendChan ?3, got RecvChan ?4" and the callee's still-unbound
	// type argument ?3 must not pile on an E0407 of its own.
	// Trade-off: quiet propagates through later bindings, so a
	// shared unsolved want can also silence a genuine mismatch at
	// a sibling if/match/select arm — accepted while want sharing
	// stays narrow (docs/implementation-gaps.md).
	if !c.unify(want, out.Type(), sp) {
		metasIn(want, func(m *types.Meta) { c.markQuiet(m) })
	}
	return out
}

// goWant marks want as a live Go-call parameter type for the extent
// of the returned function — chanDirConv consults it for the
// implicit channel direction conversion spec section 10.2 grants Go
// boundaries. checkGoApply and replayGoApply register each
// parameter's type around the argument check that conforms to it.
func (c *checker) goWant(want types.Type) func() {
	if want == nil {
		return func() {}
	}
	if c.goParamWants == nil {
		c.goParamWants = map[types.Type]bool{}
	}
	c.goParamWants[want] = true
	return func() { delete(c.goParamWants, want) }
}

// chanDirConv converts a `Chan a` argument feeding a `SendChan a` or
// `RecvChan a` Go-call parameter into an Ascribe — the implicit
// direction narrowing of spec section 10.2 (`Chan a` may go anywhere
// a directional channel is wanted, matching Go's assignability). The
// conversion only fires when want is a live Go parameter type
// (goParamWants): a Gom `SendChan` parameter or a type annotation
// keeps the explicit `Chan.sendOnly`/`Chan.recvOnly` rule.
func (c *checker) chanDirConv(want types.Type, out Expr, sp diag.Span) Expr {
	if !c.goParamWants[want] {
		return nil
	}
	we, wdir, wok := c.chanElem(want)
	ge, gdir, gok := c.chanElem(out.Type())
	if !wok || !gok || gdir != "both" || wdir == "both" {
		return nil
	}
	if !types.Equal(we, ge) {
		// The elements must agree. An unsolved element meta — a
		// generic callee's type argument — binds through ordinary
		// unification; a concrete mismatch is reported better by
		// the outer unify ("expected SendChan String, got Chan Int").
		if !hasUnboundMeta(we) && !hasUnboundMeta(ge) {
			return nil
		}
		if !c.unify(we, ge, sp) {
			metasIn(want, func(m *types.Meta) { c.markQuiet(m) })
			return out
		}
	}
	return &Ascribe{Base: baseOf(sp, want), X: out, Conv: true}
}

// hasUnboundMeta reports whether t contains a meta still awaiting a
// binding — the case element agreement needs real unification rather
// than a type-equality check.
func hasUnboundMeta(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Meta:
		return true
	case *types.Con:
		for _, a := range t.Args {
			if hasUnboundMeta(a) {
				return true
			}
		}
	case *types.Func:
		for _, p := range t.Params {
			if hasUnboundMeta(p.Type) {
				return true
			}
		}
		return hasUnboundMeta(t.Ret)
	case *types.Tuple:
		for _, e := range t.Elems {
			if hasUnboundMeta(e) {
				return true
			}
		}
	case *types.Go:
		// An instantiated Go named type — `gopkg.Box ?m` — can
		// carry metas in its arguments like Con does.
		for _, a := range t.Args {
			if hasUnboundMeta(a) {
				return true
			}
		}
	case *types.Var:
		return false
	default:
		panic("unreachable")
	}
	return false
}

// checkIdent resolves a value-position identifier.
func (c *checker) checkIdent(e *syntax.Ident) Expr {
	sym := c.useSym(e)
	typ, targs := c.valOf(sym, e.Sp)
	if sym != nil && sym.Kind == resolve.KindCtor {
		// A constructor's TypeArgs instantiate its ADT — the
		// result's own argument order, not the signature's.
		targs = ctorADTArgs(typ)
	}
	return &Var{Base: baseOf(e.Sp, typ), Sym: sym, TypeArgs: targs}
}

// checkStrInterp types an interpolated string. Each splice is checked
// for its own type — the spec leaves the splice's allowed types open,
// so any value is accepted and lowering renders it.
func (c *checker) checkStrInterp(e *syntax.StrInterp) Expr {
	parts := make([]StrPart, len(e.Parts))
	for i, p := range e.Parts {
		switch p := p.(type) {
		case *syntax.StrText:
			parts[i] = &StrText{Sp: p.Sp, Text: p.Text}
		case *syntax.StrExpr:
			parts[i] = &StrExpr{Sp: p.Sp, X: c.checkExpr(p.X, nil)}
		default:
			panic("unreachable")
		}
	}
	return &StrInterp{Base: baseOf(e.Sp, types.String), Parts: parts}
}

// checkSelect types member access: module member selection, record
// and variant field access, constructor selection (`pkg.V`), and Go
// method and field selection.
func (c *checker) checkSelect(e *syntax.Select) Expr {
	// `pkg.Name` — the receiver resolves to a module.
	if id, ok := e.Recv.(*syntax.Ident); ok {
		if msym := c.res.UseOf(id); msym != nil && msym.Kind == resolve.KindModule {
			sel := c.useSym(e.Sel)
			if sel == nil {
				c.diags.Add(diag.Error("E0401",
					fmt.Sprintf("module `%s` has no member `%s`", id.Name, nameOfSel(sel, e.Sel))).
					At(e.Sel.Sp).Set("module", id.Name).Set("member", nameOfSel(sel, e.Sel)))
			}
			recv := &Var{Base: baseOf(id.Sp, c.untypedMeta()), Sym: msym}
			var typ types.Type
			var targs []types.Type
			switch {
			case sel != nil && isConv(sel):
				// `Go.conv` alone is not a value; the conversion form
				// is `Go.conv e : T`, which the ascription checker
				// reads straight from the surface apply without ever
				// evaluating this node. Reaching here means the
				// selection sits in expression position — a bare
				// `{Go.conv}` or the callee of a malformed call —
				// so report E0507. Its type is not representable:
				// keep it off the meta table so a stray `Go.conv`
				// never asks for an annotation.
				c.diags.Add(diag.Error("E0507",
					"`Go.conv` is only valid as a conversion: `Go.conv e : T`").At(e.Sp))
				typ = c.untypedMeta()
			case sel != nil && !valueMember(sel.Kind):
				// A namespace member (a type, or a builtin that is
				// not a function) is not an expression. The resolver
				// reports this as E0201 already; the checker stays
				// total over inputs that skipped it — recover with a
				// quiet meta so no E0407 follows.
				c.diags.Add(diag.Error("E0201",
					fmt.Sprintf("`%s` is a %s, not a value", nameOfSel(sel, e.Sel), sel.Kind)).
					At(e.Sel.Sp).
					Set("name", nameOfSel(sel, e.Sel)).
					Set("namespace", "value").
					Set("resolved_kind", sel.Kind.String()))
				typ = c.quietMeta(e.Sp, fmt.Sprintf("the type of `%s`", nameOfSel(sel, e.Sel)))
			default:
				typ, targs = c.valOf(sel, e.Sel.Sp)
				if sel.Kind == resolve.KindCtor {
					targs = ctorADTArgs(typ)
				}
			}
			return &MemberExpr{Base: baseOf(e.Sp, typ), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1, TypeArgs: targs}
		}
	}

	// A type name in receiver position (`Option.Some`, `Shape.Circle`)
	// is a namespace, not a value: checking it as an expression would
	// mint a meta that can never solve. The same is true of a
	// constructor or module bound the resolver already produced, and
	// of the qualified `pkg.Type` hop under `pkg.Type.Ctor` — its
	// selector resolved to an imported type member.
	var recv Expr
	if inner, ok := e.Recv.(*syntax.Select); ok && inner.Sel != nil {
		if tsym := c.res.UseOf(inner.Sel); tsym != nil && tsym.Kind == resolve.KindType {
			if id, ok2 := inner.Recv.(*syntax.Ident); ok2 {
				if msym := c.res.UseOf(id); msym != nil && msym.Kind == resolve.KindModule {
					mrecv := &Var{Base: baseOf(id.Sp, c.untypedMeta()), Sym: msym}
					recv = &MemberExpr{Base: baseOf(inner.Sp, c.untypedMeta()),
						Recv: mrecv, Sel: inner.Sel, Sym: tsym, Index: -1}
				}
			}
		}
	}
	if recv == nil {
		if id, ok := e.Recv.(*syntax.Ident); ok {
			if rs := c.res.UseOf(id); rs != nil {
				switch rs.Kind {
				case resolve.KindType, resolve.KindCtor, resolve.KindModule, resolve.KindBuiltin:
					recv = &Var{Base: baseOf(id.Sp, c.untypedMeta()), Sym: rs}
				}
			}
		}
	}
	if recv == nil {
		recv = c.checkExpr(e.Recv, nil)
	}
	sel := c.useSym(e.Sel)
	selName := nameOfSel(sel, e.Sel)

	// A method selected on a Go value.
	if sel != nil && sel.Kind == resolve.KindMethod {
		if c.gomValue(recv.Type()) {
			c.diags.Add(diag.Error("E0502",
				fmt.Sprintf("method calls are only valid on Go values; `%s` has Gom type %s", selName, recv.Type())).
				At(e.Sel.Sp).Set("type", recv.Type().String()))
		}
		mtyp, mtargs := c.valOf(sel, e.Sel.Sp)
		return &MemberExpr{Base: baseOf(e.Sp, mtyp), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1, TypeArgs: mtargs}
	}
	// A constructor selected through a type or package (`Option.Some`
	// with the receiver already resolved); the callee checker picks
	// it up by symbol.
	if sel != nil && sel.Kind == resolve.KindCtor {
		ctyp, _ := c.valOf(sel, e.Sel.Sp)
		return &MemberExpr{Base: baseOf(e.Sp, ctyp), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1, TypeArgs: ctorADTArgs(ctyp)}
	}
	// Field access on a declared record or a variant's uniquely named
	// payload slot. The owning variant of a sum selection goes on
	// the node: the receiver is the sealed interface, so emitting
	// the read needs to know which concrete variant struct asserts
	// it (design.md 2.1 — codegen does not re-derive this).
	if fi, owner, typ, ok := c.fieldOf(recv.Type(), selName); ok {
		var ctor *resolve.Symbol
		if owner != nil {
			ctor = owner.sym
		}
		return &MemberExpr{Base: baseOf(e.Sp, typ), Recv: recv, Sel: e.Sel, Sym: fi.sym, Index: fi.index, Ctor: ctor}
	}
	// A field symbol the resolver bound but the declared-type tables
	// cannot place — a Go struct field or an imported record field
	// (both carry signatures in the SignatureSource).
	if sel != nil && sel.Kind == resolve.KindField {
		ftyp, ftargs := c.valOf(sel, e.Sel.Sp)
		return &MemberExpr{Base: baseOf(e.Sp, ftyp), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1, TypeArgs: ftargs}
	}
	// A method selected on a Go-typed receiver that the resolver
	// could not see: the SignatureSource's optional MethodSource
	// looks the name up in the named type's method set (P1-9).
	if sel == nil {
		if field, typ, raw, ok := c.goFieldOf(recv.Type(), selName, e.Sel.Sp); ok {
			return &MemberExpr{Base: baseOf(e.Sp, typ), Recv: recv, Sel: e.Sel, Sym: field, Index: 0, GoFieldType: raw}
		}
		if msym, typ, mtargs, ok := c.methodOf(recv.Type(), selName, e.Sel.Sp); ok {
			return &MemberExpr{Base: baseOf(e.Sp, typ), Recv: recv, Sel: e.Sel, Sym: msym, Index: -1, TypeArgs: mtargs}
		}
	}
	if _, ok := types.Chase(recv.Type()).(*types.Meta); ok {
		// The receiver's type is still unknown. The node keeps a
		// meta type; Index stays -1 — and the selection is deferred:
		// finalize retries the lookup once the receiver's meta has
		// bound (`0.f` waits for the literal's Int default). When the
		// receiver's own meta is quiet (an unresolved name), the
		// member's is too.
		mt := c.newMeta(e.Sp, fmt.Sprintf("the type of `.%s`", selName))
		if c.isQuiet(recv.Type()) {
			c.markQuiet(mt)
		}
		me := &MemberExpr{Base: baseOf(e.Sp, mt), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1}
		c.pendingSels = append(c.pendingSels, me)
		return me
	}
	c.diags.Add(diag.Error("E0401",
		fmt.Sprintf("%s has no field or method `%s`", recv.Type(), selName)).
		At(e.Sel.Sp).Set("type", recv.Type().String()).Set("member", selName))
	return &MemberExpr{Base: baseOf(e.Sp, c.quietMeta(e.Sp, fmt.Sprintf("the type of `.%s`", selName))), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1}
}

// valueMember reports whether a module member kind denotes a term —
// functions, constructors, variables, fields, and methods are
// values; types and modules are namespaces. (KindBuiltin members
// with a signature are values too; `Go.conv` is diverted before this
// test ever sees it.)
func valueMember(k resolve.Kind) bool {
	switch k {
	case resolve.KindFunc, resolve.KindCtor, resolve.KindVar,
		resolve.KindField, resolve.KindMethod, resolve.KindBuiltin:
		return true
	default:
		return false
	}
}

// revisitSelects retries the member selections checkSelect deferred
// on an unresolved receiver meta — see pendingSels. finalize calls
// it once the defaulting pass has run, so `0.f` sees the literal's
// Int and `x.f` whatever the use site pinned, and it loops to a
// fixpoint because resolving one member (and replaying its deferred
// application) can bind the receiver another entry waits on. A
// retry that finds the member patches Sym/Index into place and
// unifies the node's standing meta with the member type — a meta
// already bound by the use site reports the conflict through the
// ordinary unification diagnostic. A receiver still unresolved is
// left alone: its meta's own E0407 (or quiet recovery) covers the
// selection. A resolved receiver that carries no such member earns
// the E0401 the first pass withheld.
func (c *checker) revisitSelects() {
	// Resolved members are marked done: a receiver bound by a later
	// replay — e.g. `g (x.AddDate(1, 0, 0))` whose apply meta feeds
	// an earlier `r.Year` receiver — must still get its pass, so the
	// loop repeats while any member newly resolved. Registration
	// order decides only the diagnostic order, never the outcome.
	done := map[*MemberExpr]bool{}
	for progress := true; progress; {
		progress = false
		for _, me := range c.pendingSels {
			if done[me] {
				continue
			}
			rt := types.Chase(me.Recv.Type())
			if _, ok := rt.(*types.Meta); ok {
				continue
			}
			done[me] = true
			progress = true
			selName := nameOfSel(me.Sym, me.Sel)
			resolved := false
			if fi, owner, typ, ok := c.fieldOf(rt, selName); ok {
				me.Sym, me.Index = fi.sym, fi.index
				if owner != nil {
					me.Ctor = owner.sym
				}
				c.unify(me.Type(), typ, me.Sel.Sp)
				resolved = true
			} else if me.Sym == nil {
				if field, typ, raw, ok := c.goFieldOf(rt, selName, me.Sel.Sp); ok {
					me.Sym, me.Index, me.GoFieldType = field, 0, raw
					c.unify(me.Type(), typ, me.Sel.Sp)
					resolved = true
				} else if msym, typ, targs, ok := c.methodOf(rt, selName, me.Sel.Sp); ok {
					me.Sym = msym
					me.TypeArgs = targs
					c.unify(me.Type(), typ, me.Sel.Sp)
					resolved = true
				}
			}
			if !resolved {
				c.diags.Add(diag.Error("E0401",
					fmt.Sprintf("%s has no field or method `%s`", rt, selName)).
					At(me.Sel.Sp).Set("type", rt.String()).Set("member", selName))
				// The selection failed; the member's standing meta — nothing
				// bound it — must not pile an E0407 on the E0401.
				c.markQuiet(me.Type())
			}
			// Applications sitting on this member (pendingApplies)
			// now know their calling convention — a Go method
			// settles the tuple form; anything else the Gom rule.
			// Replaying here, in pendingSels order, lets a member
			// chained off the call's result (`x.M(1).N(2)`) see the
			// bound result type when its own turn comes — and lets
			// an earlier member whose receiver this result binds
			// resolve on the next pass.
			c.replayAppliesOf(me)
		}
	}
	// Applications whose member never resolved still need a final
	// CallInfo: they replay under the Gom convention so the
	// diagnostics the first pass withheld (a `_`, a label on an
	// unknown callee) fire and lowering sees a valid record.
	c.replayDeferredApplies()
}

// nameOfSel returns the selector name a resolved member symbol
// denotes, falling back to the written name.
func nameOfSel(sym *resolve.Symbol, sel *syntax.Ident) string {
	if sym != nil {
		return sym.Name
	}
	if sel != nil {
		return sel.Name
	}
	return "?"
}

// gomValue reports whether t is provably a Gom-defined value — a
// type this file declares, an imported Gom nominal type, or a
// structural Tuple/Func — on which Go methods cannot exist (E0502).
// Builtin constructors (Int, List, Ptr, Go.Slice, ...), Go handles,
// and still-unknown metas are interop-shaped and may carry methods.
//
// Note: unqualified builtin Cons like Int or List fall through to
// `t.Qual != ""` and count as Go-side values here, so `x.M(...)` on a
// List receiver is allowed. Spec section 8.1.3 says method calls are
// for Go values; whether a Gom builtin type may carry methods is left
// open (List has none today, but interop mapping may change that).
func (c *checker) gomValue(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if t.Qual == "Go" {
			return false
		}
		if _, ok := c.tyCons[conKey{qual: t.Qual, name: t.Name}]; ok {
			return true // declared here (record/sum/abstract/alias)
		}
		return t.Qual != "" // imported Gom nominal type
	case *types.Tuple, *types.Func:
		return true
	case *types.Var, *types.Meta, *types.Go:
		return false
	default:
		panic("unreachable")
	}
}

// fieldOf resolves a field selection on a receiver of known type:
// a record field by name, or a variant's uniquely named payload slot.
// The variant result is the constructor info owning a sum selection —
// nil for record fields — because the receiver's representation is
// the sealed interface and the read only exists on one variant's
// concrete struct.
func (c *checker) fieldOf(t types.Type, name string) (*fieldInfo, *variantInfo, types.Type, bool) {
	con, ok := types.Chase(t).(*types.Con)
	if !ok {
		return nil, nil, nil, false
	}
	ti, ok := c.tyCons[conKey{qual: con.Qual, name: con.Name}]
	if !ok || ti == nil {
		return nil, nil, nil, false
	}
	sub := types.Subst{}
	for i, v := range ti.params {
		if i < len(con.Args) {
			sub[v.ID] = con.Args[i]
		}
	}
	switch ti.kind {
	case kindRecord:
		if fi := c.fieldByName(ti.fields, name); fi != nil {
			return fi, nil, sub.Apply(fi.typ), true
		}
	case kindSum:
		// A named payload slot of a sum type is selectable when
		// exactly one variant declares it.
		var found *fieldInfo
		var owner *variantInfo
		for _, v := range ti.variants {
			for _, f := range v.fields {
				if f.name == name {
					if found != nil && found.sym != f.sym {
						return nil, nil, nil, false
					}
					found = f
					owner = v
				}
			}
		}
		if found != nil {
			return found, owner, sub.Apply(found.typ), true
		}
	}
	return nil, nil, nil, false
}

// methodOf resolves a method name on a Go-typed receiver (a
// types.Go, the builtin GoError, or a Ptr to one) through the
// optional MethodSource of the signature source. The returned type
// is the instantiated method signature — the receiver itself is not
// a parameter of it — and the targs are the metas the instantiation
// substituted for the signature's type variables.
func (c *checker) methodOf(recvT types.Type, name string, sp diag.Span) (*resolve.Symbol, types.Type, []types.Type, bool) {
	t := types.Chase(recvT)
	if con, ok := t.(*types.Con); ok && con.Qual == "" && con.Name == "Ptr" && len(con.Args) == 1 &&
		!c.shadowedBuiltin("Ptr") {
		t = types.Chase(con.Args[0])
	}
	// `types.Equal(t, types.GoError)` cannot tell a file-local `type
	// GoError` from the builtin — its Con spells the same name — so
	// the builtin receiver test goes through isBuiltinCon.
	if _, ok := t.(*types.Go); !ok && !c.isBuiltinCon(types.Chase(t), "GoError") {
		return nil, nil, nil, false
	}
	// The signature source wins when it can answer — a loader-provided
	// symbol keeps its origin, its hidden-symbol check, and whatever
	// method set the Go type really carries.
	if ms, ok := c.src.(MethodSource); ok {
		if sym, sig, ok := ms.MethodOf(recvT, name); ok {
			if c.hiddenDiag(sym, sp) {
				return sym, c.quietMeta(sp, fmt.Sprintf("the type of method `%s`", name)), nil, true
			}
			typ, targs := c.instantiateSig(sig, sp)
			return sym, typ, targs, true
		}
	}
	// `GoError` is a builtin scalar — not a types.Go — so a
	// MethodSource that only knows named Go types cannot reach its
	// `Error() string`. The checker owns the builtin and answers the
	// one method the `error` interface carries, so checkers built
	// without a signature source still type `e.Error()`.
	if c.isBuiltinCon(t, "GoError") && name == "Error" {
		sym := c.goErrMethodSym()
		typ, targs := c.instantiate(c.vals[sym], sp)
		return sym, typ, targs, true
	}
	return nil, nil, nil, false
}

// checkAscribe types `e : T` — and `Go.conv e : T`, the explicit Go
// conversion. The ascribed type is authoritative: the expression must
// have it. For Go.conv the checker validates the pair against Go's
// own conversion rules — the local shape rules first, then the
// ConvertSource oracle's go/types judgment (E0507). An operand whose
// type is still an unsolved meta defers to revisitConvs.
//
// Ascribing an arbitrary expression `e : T` is rejected: the
// ascription form is reserved for `Go.conv` until the open question
// (docs/problems/ascription.md) resolves — the typed node still
// records the target for recovery.
func (c *checker) checkAscribe(e *syntax.Ascribe) Expr {
	target := c.typeExpr(e.Type, c.newTScope())
	// `Go.conv e : T` — the surface wraps the argument in an
	// application of the conv builtin.
	if app, ok := e.X.(*syntax.Apply); ok {
		if sym := c.calleeSym(app.Fn); sym != nil && isConv(sym) {
			if len(app.Args) == 1 && app.Args[0].Label == nil {
				x := c.checkExpr(app.Args[0].Value, nil)
				if _, unsolved := types.Chase(x.Type()).(*types.Meta); unsolved {
					c.pendingConvs = append(c.pendingConvs,
						&pendingConv{x: x, to: target, sp: e.Sp})
				} else {
					c.checkConv(x.Type(), target, e.Sp)
				}
				return &Ascribe{Base: baseOf(e.Sp, target), X: x, Conv: true}
			}
			// Wrong shape: recover on the first argument (or a
			// missing one) without double-reporting the callee.
			var arg syntax.Expr
			for _, a := range app.Args {
				if a.Value != nil {
					arg = a.Value
					break
				}
			}
			c.diags.Add(diag.Error("E0507",
				"`Go.conv` takes exactly one argument").At(e.Sp))
			return &Ascribe{Base: baseOf(e.Sp, target), X: c.checkExpr(arg, nil), Conv: true}
		}
	}
	x := c.checkExpr(e.X, target)
	c.diags.Add(diag.Error("E0401",
		"`e : T` is only allowed as `Go.conv e : T`; there is no general ascription").
		At(e.Sp).Set("type", target.String()))
	return &Ascribe{Base: baseOf(e.Sp, target), X: x}
}

// calleeSym resolves the symbol a callee position names without
// checking the expression — used where the call's shape is handled
// specially (Go.conv) and the ordinary apply path must not run.
func (c *checker) calleeSym(e syntax.Expr) *resolve.Symbol {
	switch e := e.(type) {
	case *syntax.Ident:
		return c.useSym(e)
	case *syntax.Select:
		return c.useSym(e.Sel)
	case *syntax.Lit, *syntax.StrInterp, *syntax.Apply, *syntax.Unary,
		*syntax.Binary, *syntax.Hole, *syntax.Lambda, *syntax.If,
		*syntax.Match, *syntax.Block, *syntax.Let, *syntax.Ignore,
		*syntax.Try, *syntax.GoExpr, *syntax.For, *syntax.SelectExpr,
		*syntax.ListLit, *syntax.Spread, *syntax.TupleLit,
		*syntax.Ascribe, *syntax.RecordLit:
		return nil
	default:
		panic("unreachable")
	}
}

// revisitConvs rechecks the `Go.conv e : T` sites whose operand meta
// was unsolved at ascription time — a `1` literal before its Int
// default binds. A meta still unsolved passes convOK again, and its
// own E0407 covers the position.
func (c *checker) revisitConvs() {
	for _, pc := range c.pendingConvs {
		c.checkConv(pc.x.Type(), pc.to, pc.sp)
	}
}

// checkConv reports E0507 for a conversion Go itself would reject:
// conversions are allowed between numeric types, between a named Go
// type and its underlying type (a builtin with the same shape), and
// from a Go named type to another type it may convert to. The
// ConvertSource oracle carries the go/types judgment for pairs the
// shape rules cannot decide (spec section 8.4).
func (c *checker) checkConv(from, to types.Type, sp diag.Span) {
	if !c.convOK(types.Chase(from), types.Chase(to)) {
		c.diags.Add(diag.Error("E0507",
			fmt.Sprintf("cannot convert %s to %s", from, to)).
			At(sp).Set("from", from.String()).Set("to", to.String()))
	}
}

// convOK approximates Go's assignability-by-conversion rule at the
// type-shape level. Both sides concrete; metas defer (the conversion
// is rechecked implicitly when the meta solves, since the ascription
// keeps the target type). After the shape rules, a ConvertSource
// decides Go-boundary pairs by go/types convertibility; pairs it
// cannot judge keep the earlier acceptance only when a named Go
// type participates.
func (c *checker) convOK(from, to types.Type) bool {
	if types.Equal(from, to) {
		return true
	}
	if _, ok := from.(*types.Meta); ok {
		return true
	}
	if _, ok := to.(*types.Meta); ok {
		return true
	}
	numeric := func(t types.Type) bool {
		con, ok := t.(*types.Con)
		if !ok {
			return false
		}
		// A file-local `type Int` is not the builtin numeric the
		// conversion rule means.
		if con.Qual == "" && !c.shadowedBuiltin(con.Name) {
			switch con.Name {
			case "Int", "Int32", "Int64", "Float", "Char":
				return true
			}
		}
		return false
	}
	if numeric(from) && numeric(to) {
		return true
	}
	// A file-local `type` declaration is invisible to the oracle,
	// and it can hide at any depth: boundaryType maps every
	// unqualified Con by name alone, so a `type Int` ADT nested
	// inside `List`/`Ptr`/`Go.Slice`/etc. args would convert to
	// the builtin's Go representation and emit uncompilable code.
	// Walk both sides for shadowed Cons before trusting it; a hit
	// sends the pair to the fallback the way the old outermost
	// check did.
	var containsShadowed func(t types.Type) bool
	containsShadowed = func(t types.Type) bool {
		switch t := types.Chase(t).(type) {
		case *types.Con:
			if t.Qual == "" && c.shadowedBuiltin(t.Name) {
				return true
			}
			for _, a := range t.Args {
				if containsShadowed(a) {
					return true
				}
			}
		case *types.Func:
			for _, p := range t.Params {
				if containsShadowed(p.Type) {
					return true
				}
			}
			return containsShadowed(t.Ret)
		case *types.Tuple:
			for _, e := range t.Elems {
				if containsShadowed(e) {
					return true
				}
			}
		case *types.Go:
			for _, a := range t.Args {
				if containsShadowed(a) {
					return true
				}
			}
		case *types.Var, *types.Meta:
		default:
			panic("unreachable")
		}
		return false
	}
	if !containsShadowed(from) && !containsShadowed(to) {
		if src, ok := c.src.(ConvertSource); ok {
			if decided, ok := src.GoConvertible(from, to); decided {
				return ok
			}
		}
	}
	// Unproven pairs — an unmappable Go view or no oracle at all —
	// keep the historical acceptance when a named Go handle is
	// involved; two Gom-side shapes the rules did not pass fail.
	if _, ok := from.(*types.Go); ok {
		return true
	}
	if _, ok := to.(*types.Go); ok {
		return true
	}
	return false
}

// checkUnary types `-x` (numeric) and `!x` (Bool).
func (c *checker) checkUnary(e *syntax.Unary) Expr {
	switch e.Op {
	case syntax.UnNeg:
		x := c.checkExpr(e.X, nil)
		c.constrain(x.Type(), e.X.Span(), "the operand of `-`", func(r *metaRec) {
			r.num = true
		})
		return &Unary{Base: baseOf(e.Sp, x.Type()), Op: e.Op, X: x}
	case syntax.UnNot:
		x := c.checkExpr(e.X, types.Bool)
		return &Unary{Base: baseOf(e.Sp, c.boolResult(e.Sp, "`!`")), Op: e.Op, X: x}
	default:
		panic("unreachable")
	}
}

// checkBinary types the binary operators, including the sugared
// pipeline and composition forms, which stay Binary nodes until
// lowering.
func (c *checker) checkBinary(e *syntax.Binary) Expr {
	op := e.Op
	switch op {
	case syntax.BinAdd, syntax.BinSub, syntax.BinMul, syntax.BinDiv:
		return c.checkArith(e, func(r *metaRec) { r.num = true })
	case syntax.BinMod:
		return c.checkArith(e, func(r *metaRec) { r.intg = true })
	case syntax.BinEq, syntax.BinNe:
		x := c.checkExpr(e.X, nil)
		y := c.checkExpr(e.Y, nil)
		// The constraint check runs only when the operands agreed —
		// a failed unify already reported; probing its type again
		// would restate the same mismatch.
		if c.unify(x.Type(), y.Type(), e.Sp) {
			c.constrain(x.Type(), e.Sp, "the operands of `==`", func(r *metaRec) { r.eq = true })
		}
		return &Binary{Base: baseOf(e.Sp, c.boolResult(e.Sp, "`==`")), Op: op, X: x, Y: y}
	case syntax.BinLt, syntax.BinLe, syntax.BinGt, syntax.BinGe:
		x := c.checkExpr(e.X, nil)
		y := c.checkExpr(e.Y, nil)
		if c.unify(x.Type(), y.Type(), e.Sp) {
			c.constrain(x.Type(), e.Sp, "the operands of a comparison", func(r *metaRec) { r.ord = true })
		}
		return &Binary{Base: baseOf(e.Sp, c.boolResult(e.Sp, "a comparison")), Op: op, X: x, Y: y}
	case syntax.BinAnd, syntax.BinOr:
		x := c.checkExpr(e.X, types.Bool)
		y := c.checkExpr(e.Y, types.Bool)
		return &Binary{Base: baseOf(e.Sp, c.boolResult(e.Sp, fmt.Sprintf("`%s`", e.Op))), Op: op, X: x, Y: y}
	case syntax.BinPipeFwd:
		// `x |> f` is `f x`.
		x := c.checkExpr(e.X, nil)
		f := c.checkExpr(e.Y, nil)
		return c.pipeCall(e, x, f)
	case syntax.BinPipeBack:
		// `f PIPE_BACK x` applies f to x — operands in source order.
		f := c.checkExpr(e.X, nil)
		x := c.checkExpr(e.Y, nil)
		return c.pipeCall(e, x, f)
	case syntax.BinPipeTry:
		return c.checkPipeTry(e)
	case syntax.BinComposeFwd:
		// f >> g is x -> g (f x).
		x := c.checkExpr(e.X, nil)
		g := c.checkExpr(e.Y, nil)
		a := c.newMeta(e.Sp, "the composed function's input")
		b := c.newMeta(e.Sp, "the composed function's middle type")
		r := c.newMeta(e.Sp, "the composed function's result")
		ok1 := c.unify(x.Type(), types.Fn(b, types.P(a)), e.X.Span())
		ok2 := c.unify(g.Type(), types.Fn(r, types.P(b)), e.Y.Span())
		if !ok1 || !ok2 {
			c.markQuiet(a)
			c.markQuiet(b)
			c.markQuiet(r)
		}
		return &Binary{Base: baseOf(e.Sp, types.Fn(r, types.P(a))), Op: op, X: x, Y: g}
	case syntax.BinComposeBack:
		// g << f is x -> g (f x).
		g := c.checkExpr(e.X, nil)
		f := c.checkExpr(e.Y, nil)
		a := c.newMeta(e.Sp, "the composed function's input")
		b := c.newMeta(e.Sp, "the composed function's middle type")
		r := c.newMeta(e.Sp, "the composed function's result")
		ok1 := c.unify(f.Type(), types.Fn(b, types.P(a)), e.Y.Span())
		ok2 := c.unify(g.Type(), types.Fn(r, types.P(b)), e.X.Span())
		if !ok1 || !ok2 {
			c.markQuiet(a)
			c.markQuiet(b)
			c.markQuiet(r)
		}
		return &Binary{Base: baseOf(e.Sp, types.Fn(r, types.P(a))), Op: op, X: g, Y: f}
	default:
		panic("unreachable")
	}
}

// goCalleeSym returns the Go symbol a bare pipe callee denotes —
// a Var or MemberExpr bound to a Go function or a method — or nil.
// It is the bare-reference counterpart of calleeOf's classification.
func goCalleeSym(e Expr) *resolve.Symbol {
	var sym *resolve.Symbol
	switch e := e.(type) {
	case *Var:
		sym = e.Sym
	case *MemberExpr:
		sym = e.Sym
	case *Apply, *Ascribe, *Binary, *Block, *For, *GoExpr, *Hole,
		*If, *Ignore, *Lambda, *Let, *ListLit, *Lit, *Match,
		*RecordLit, *SelectExpr, *Spread, *StrInterp, *Try,
		*TupleLit, *Unary:
		return nil
	default:
		panic("unreachable")
	}
	if sym != nil && (sym.Kind == resolve.KindMethod || sym.IsGo) {
		return sym
	}
	return nil
}

// pipeCall types `x |> f` and `f BACKPIPE x`: f applied to x.
// arg/fn are the checked operands in application roles (for BACKPIPE
// they arrive swapped relative to source order).
//
// An ordinary application would make x the first argument of a
// fresh call; two Go-callee shapes bend that rule so the spelling
// matches the calling convention lowering emits:
//
//   - A bare variadic callee of arity 1 (`fmt.Println`) receives x
//     as one variadic element — checked against the element type of
//     the declared slice parameter, never the slice itself — and the
//     pipe has the callee's return type. `Println BACKPIPE "hi"` is
//     `Println("hi")`, not `Println("hi"...)`.
//   - A partially applied Go call (`Println "a" BACKPIPE x`)
//     appends x as the next argument — again as one variadic
//     element when the open slot is the variadic parameter. A
//     saturated non-variadic call cannot take another argument —
//     the same arity diagnostic an over-applied Go call reports.
//
// Everything else goes through the generic function-type rule:
// f must be `x's type -> ret` for a fresh ret meta.
func (c *checker) pipeCall(e *syntax.Binary, arg, fn Expr) Expr {
	out := &Binary{Base: baseOf(e.Sp, nil), Op: e.Op, X: arg, Y: fn}
	if e.Op == syntax.BinPipeBack {
		out.X, out.Y = fn, arg
	}
	if sym := goCalleeSym(fn); sym != nil {
		if sig, ok := c.sigOf(sym, e.Sp); ok {
			params, ret := flattenFunc(fn.Type())
			arity := c.declArity(sig, len(params))
			switch {
			case sig.Variadic && arity == 1 && len(params) == 1:
				c.unify(sliceElemOf(params[0].Type, arg.Span(), c), arg.Type(), arg.Span())
				out.Base = baseOf(e.Sp, ret)
				return out
			case arity == 0 && !sig.Variadic:
				// `x |> f` for a Go callee taking no argument is
				// the one-argument over-application — the same
				// diagnostic the application form reports.
				c.diags.Add(diag.Error("E0401",
					"the Go callee takes 0 argument(s), got 1").
					At(e.Sp).Set("expected", "0").Set("actual", "1"))
				out.Base = baseOf(e.Sp, ret)
				return out
			case arity == 1 && len(params) == 1:
				// `x |> f` where f is a one-parameter Go callee:
				// the piped value conforms to the declared
				// parameter as a Go-boundary argument — `Chan a`
				// narrows to `chan<-`/`<-chan` (chanDirConv), a
				// `List` copies to `[]T`, Go interfaces assign —
				// exactly what `f x` admits (spec 8.5/10.2). The
				// generic path below applies the callee by plain
				// unify and would bypass every conversion.
				undo := c.goWant(params[0].Type)
				conv := c.conformExpr(arg, params[0].Type, arg.Span())
				undo()
				if e.Op == syntax.BinPipeBack {
					out.Y = conv
				} else {
					out.X = conv
				}
				out.Base = baseOf(e.Sp, ret)
				return out
			}
		}
	}
	// A call carrying a `_` placeholder keeps a function residual
	// (`holeT -> ret`), so it takes the generic path like any other
	// function value — the pipe fills the hole at lowering. The
	// special cases below are the *saturated* Go calls: a variadic
	// one can absorb x as another element; a non-variadic one is
	// over-applied — the same diagnostic the application form gives.
	if ap, ok := fn.(*Apply); ok && ap.Call.Kind == CallGo && ap.Call.HoleIndex < 0 {
		params, _ := flattenFunc(ap.Fn.Type())
		switch {
		case ap.Call.Variadic && ap.Call.Arity > 0 && len(params) == ap.Call.Arity:
			// The open slot is the variadic parameter: x is one
			// element of it.
			c.unify(sliceElemOf(params[ap.Call.Arity-1].Type, arg.Span(), c), arg.Type(), arg.Span())
		default:
			// A saturated non-variadic Go call — over-applied. A
			// partial one (Applied < Arity) already carries its own
			// arity diagnostic; another would only echo it with a
			// self-contradicting "got" count.
			if ap.Call.Applied >= ap.Call.Arity {
				c.diags.Add(diag.Error("E0401",
					fmt.Sprintf("the Go callee takes %d argument(s), got %d", ap.Call.Arity, ap.Call.Applied+1)).
					At(e.Sp).Set("expected", fmt.Sprintf("%d", ap.Call.Arity)).Set("actual", fmt.Sprintf("%d", ap.Call.Applied+1)))
			}
		}
		out.Base = baseOf(e.Sp, ap.Type())
		return out
	}
	ret := c.newMeta(e.Sp, "the pipe's result type")
	c.unify(fn.Type(), types.Fn(ret, types.P(arg.Type())), e.Sp)
	out.Base = baseOf(e.Sp, ret)
	return out
}

// checkArith types +,-,*,/ and %: both operands share one numeric
// type (integral for %), which is also the result.
func (c *checker) checkArith(e *syntax.Binary, constrain func(*metaRec)) Expr {
	x := c.checkExpr(e.X, nil)
	y := c.checkExpr(e.Y, nil)
	out := &Binary{Base: baseOf(e.Sp, x.Type()), Op: e.Op, X: x, Y: y}
	if c.unify(x.Type(), y.Type(), e.Sp) {
		c.constrain(x.Type(), e.Sp, "the operands of an arithmetic operator", constrain)
		// A constant-zero divisor is always wrong — for an integer
		// division Go itself rejects `x / 0` (there is no integer
		// infinity to produce) and a constant division like `10 / 0`
		// is not even compilable Go. Whether `x / 0.0` is legal IEEE
		// arithmetic depends on the operand type, so the decision is
		// deferred to finalize — see pendingDivs. A quiet operand
		// (an unresolved name, say) already reports elsewhere.
		if (e.Op == syntax.BinDiv || e.Op == syntax.BinMod) &&
			constZero(y) && !c.isQuiet(x.Type()) {
			c.pendingDivs = append(c.pendingDivs, out)
		}
	}
	return out
}

// revisitDivs reports E0414 for each `/` or `%` whose right operand
// folded to a constant zero — the rule Go applies to the emitted
// code. It runs from finalize, after the defaulting pass, so an
// operand meta is judged by its solved type:
//
//   - integral operand (Int, Int32, Int64 — including metas that
//     defaulted there): the division is reported, dividend or not —
//     `x / 0` emits a division Go itself rejects, and for `x / (1-1)`
//     folding the divisor is what Go does too;
//   - Float operand: reported only when the dividend is also a
//     constant expression (`10.0 / 0.0` is an invalid Go constant;
//     `x / 0.0` is the IEEE division to +Inf that Go allows);
//   - still-unbound operand meta: left alone — the meta's own E0407
//     (or quiet recovery) covers the expression. A non-numeric
//     operand (`true / 0`) already reports the num constraint —
//     piling E0414 on it would cascade.
func (c *checker) revisitDivs() {
	for _, b := range c.pendingDivs {
		t := types.Chase(b.X.Type())
		switch {
		case isIntegral(t):
			// Always invalid — see above.
		case isFloat(t):
			if _, ok := constFold(b.X); !ok {
				continue
			}
		default:
			continue
		}
		c.diags.Add(diag.Error("E0414", "division by a constant zero").
			At(b.Y.Span()).Set("op", b.Op.String()))
	}
}

// constZero reports whether e is a constant expression that
// evaluates to numeric zero — see constFold for the folded shapes.
func constZero(e Expr) bool {
	v, ok := constFold(e)
	return ok && constant.Sign(v) == 0
}

// constFold evaluates e as a Go constant expression and returns its
// value, or false when e is not constant. The folded shapes are the
// ones that emit Go constants — numeric literals, unary `-`, the
// arithmetic binary operators over constants, and type ascriptions —
// evaluated with go/constant's exact arithmetic (arbitrary precision
// integers and rationals, so the fold agrees with the compiler's
// judgment of the emitted code, not float64 arithmetic). Everything
// else is not a constant: variables and named constants (a `let`
// binding emits a Go variable, not a Go constant — `let z = 0`
// leaves `x / z` a runtime panic, matching Go), function calls,
// blocks and branching expressions. One consequence of treating
// references as opaque: a *Go* constant used as a divisor
// (`x / pkg.Zero`) still emits a division Go rejects — folding
// imported constants is out of scope for this check.
func constFold(e Expr) (constant.Value, bool) {
	switch e := e.(type) {
	case *Lit:
		var tok token.Token
		switch e.Kind {
		case syntax.LitInt:
			tok = token.INT
		case syntax.LitFloat:
			tok = token.FLOAT
		default:
			return nil, false
		}
		v := constant.MakeFromLiteral(e.Text, tok, 0)
		if v.Kind() == constant.Unknown {
			return nil, false
		}
		return v, true
	case *Unary:
		if e.Op != syntax.UnNeg {
			return nil, false
		}
		v, ok := constFold(e.X)
		if !ok {
			return nil, false
		}
		return constant.UnaryOp(token.SUB, v, 0), true
	case *Binary:
		x, ok := constFold(e.X)
		if !ok {
			return nil, false
		}
		y, ok := constFold(e.Y)
		if !ok {
			return nil, false
		}
		var op token.Token
		switch e.Op {
		case syntax.BinAdd:
			op = token.ADD
		case syntax.BinSub:
			op = token.SUB
		case syntax.BinMul:
			op = token.MUL
		case syntax.BinDiv:
			op = token.QUO
		case syntax.BinMod:
			op = token.REM
		default:
			return nil, false
		}
		// constant.BinaryOp panics on a division by zero — and the
		// nested node that actually divides by zero reports its own
		// E0414, so this expression simply has no value to fold —
		// and on `%` applied to non-integers (a malformed operand
		// already diagnosed against the intg constraint).
		if (op == token.QUO || op == token.REM) && constant.Sign(y) == 0 {
			return nil, false
		}
		if op == token.REM &&
			(x.Kind() != constant.Int || y.Kind() != constant.Int) {
			return nil, false
		}
		return constant.BinaryOp(x, op, y), true
	case *Ascribe:
		// `e : T` emits e (possibly under a conversion); converting
		// a constant keeps it constant, so the ascription is
		// transparent to folding.
		return constFold(e.X)
	case *Var, *Apply, *Hole, *MemberExpr, *StrInterp, *Lambda, *If,
		*Match, *Block, *Let, *Ignore, *Try, *GoExpr, *For,
		*SelectExpr, *ListLit, *Spread, *TupleLit, *RecordLit:
		return nil, false
	default:
		panic("unreachable")
	}
}

// checkPipeTry types the Result-bind pipeline `x |?> f`: when x is
// `Result a e` it unwraps to a, f must be `a -> Result b e`, and the
// whole expression is `Result b e`. When x is not a Result it is
// wrapped in Ok first, per spec 9.2.7.
func (c *checker) checkPipeTry(e *syntax.Binary) Expr {
	x := c.checkExpr(e.X, nil)
	f := c.checkExpr(e.Y, nil)
	// `|?>` wraps the step in the builtin Result and unwraps the
	// operand through its tag machinery — both meanings are the
	// file-local `type Result` when one shadows it, so the whole
	// pipeline is E0415.
	if c.shadowedBuiltin("Result") {
		c.shadowedErr(e.Sp, "`|?>`", "Result")
	}
	var argT, errT types.Type
	// Numeric/ordered metas cannot be Result. Unlike try, a pipeline
	// accepts these as its initial unwrapped value (spec 9.2.7).
	canBeResult := true
	if m, ok := types.Chase(x.Type()).(*types.Meta); ok {
		r := c.recOf(m)
		canBeResult = !r.num && !r.ord && !r.flo && !r.intg
	}
	var a, er types.Type
	var isResult bool
	if canBeResult {
		a, er, isResult = c.resultArgs(x.Type(), e.X.Span())
	}
	if isResult {
		argT, errT = a, er
	} else {
		// Non-Result left side: the value is used as-is (Ok-wrapped
		// at evaluation). The step still returns a Result.
		argT = x.Type()
		errT = c.newMeta(e.Sp, "the pipeline error type")
	}
	resT := c.newMeta(e.Sp, "the |?> result type")
	if !c.unify(f.Type(), types.Fn(types.Result(resT, errT), types.P(argT)), e.Y.Span()) {
		c.markQuiet(resT)
		c.markQuiet(errT)
	}
	return &Binary{Base: baseOf(e.Sp, types.Result(resT, errT)), Op: e.Op, X: x, Y: f}
}

// checkLambda types an anonymous function `fn x y -> body`.
// Unannotated parameters get metas; the boundary stack grows so a
// `try` inside escapes to this lambda's Result — which therefore
// becomes `p1 ... -> Result a e` when it contains one.
func (c *checker) checkLambda(e *syntax.Lambda) Expr {
	sc := c.newTScope()
	params := make([]Param, len(e.Params))
	fparams := make([]types.FuncParam, len(e.Params))
	for i, p := range e.Params {
		var pt types.Type
		if p.Type != nil {
			pt = c.typeExpr(p.Type, sc)
		} else {
			pt = c.newMeta(p.Sp, "the lambda parameter's type")
		}
		label := ""
		if p.Label != nil {
			label = p.Label.Name
		}
		params[i] = Param{Label: label, Pat: c.checkPat(p.Pat, pt)}
		fparams[i] = types.FuncParam{Label: label, Type: pt}
	}
	ret := c.newMeta(e.Sp, "the lambda's return type")
	// The lambda's own annotation variables stay visible in its
	// body — `fn (x: a) -> { let y : a = x; y }`.
	prevScope := c.curTScope
	c.curTScope = sc
	c.bounds = append(c.bounds, boundary{ret: ret, sp: e.Sp, goDepth: c.goDepth, kind: "lambda"})
	body := c.checkExpr(e.Body, ret)
	c.bounds = c.bounds[:len(c.bounds)-1]
	c.curTScope = prevScope
	return &Lambda{
		Base:   baseOf(e.Sp, types.Fn(ret, fparams...)),
		Params: params,
		Body:   body,
	}
}

// checkIf types `if c { a } else { b }`. Without else the then-branch
// must be Unit — a value-producing then without else is E0403.
func (c *checker) checkIf(e *syntax.If, want types.Type) Expr {
	cond := c.checkExpr(e.Cond, types.Bool)
	// The condition must be the builtin Bool — which a file-local
	// `type Bool` shadows away entirely.
	if c.shadowedBuiltin("Bool") {
		c.shadowedErr(e.Sp, "the `if` condition", "Bool")
	}
	if e.Else == nil {
		then := c.checkExpr(e.Then, nil)
		switch t := types.Chase(then.Type()).(type) {
		case *types.Meta:
			// An unconstrained unknown may still be Unit; a
			// constrained one (numeric, ordered, ...) can never
			// be, so this is the missing-else shape.
			if r := c.recOf(t); r != nil && (r.num || r.flo || r.intg || r.ord) {
				c.diags.Add(diag.Error("E0403",
					fmt.Sprintf("`if` without `else` requires a Unit then-branch, got %s", then.Type())).
					At(e.Sp).Set("type", then.Type().String()))
			} else {
				c.bind(t, types.Unit, e.Then.Span())
			}
		case *types.Con:
			if !isCon(t, "Unit") {
				c.diags.Add(diag.Error("E0403",
					fmt.Sprintf("`if` without `else` requires a Unit then-branch, got %s", then.Type())).
					At(e.Sp).Set("type", then.Type().String()))
			}
		case *types.Var, *types.Func, *types.Tuple, *types.Go:
			c.diags.Add(diag.Error("E0403",
				fmt.Sprintf("`if` without `else` requires a Unit then-branch, got %s", then.Type())).
				At(e.Sp).Set("type", then.Type().String()))
		default:
			panic("unreachable")
		}
		return &If{Base: baseOf(e.Sp, c.unitResult(e.Sp, "`if` without `else`")), Cond: cond, Then: then}
	}
	bt := want
	if bt == nil {
		bt = c.newMeta(e.Sp, "the if branches' type")
	}
	then := c.checkExpr(e.Then, bt)
	els := c.checkExpr(e.Else, bt)
	return &If{Base: baseOf(e.Sp, bt), Cond: cond, Then: then, Else: els}
}

// checkMatch types `match subj { arms }`. Arm patterns check against
// the subject type; bodies unify on one result type. Exhaustiveness
// and reachability are P1-6's, not this phase's.
func (c *checker) checkMatch(e *syntax.Match, want types.Type) Expr {
	subj := c.checkExpr(e.Subj, nil)
	bt := want
	if bt == nil {
		bt = c.newMeta(e.Sp, "the match arms' type")
	}
	arms := make([]MatchArm, len(e.Arms))
	for i, a := range e.Arms {
		// The pattern binds before the guard runs: `a == b` in a
		// guard must see the pattern variables' own types, not
		// fresh metas minted against an empty binding table.
		pat := c.checkPat(a.Pat, subj.Type())
		var guard Expr
		if a.Guard != nil {
			guard = c.checkExpr(a.Guard, types.Bool)
			if c.shadowedBuiltin("Bool") {
				c.shadowedErr(a.Guard.Span(), "a match guard", "Bool")
			}
		}
		arms[i] = MatchArm{
			Sp:    a.Sp,
			Pat:   pat,
			Guard: guard,
			Body:  c.checkExpr(a.Body, bt),
		}
	}
	return &Match{Base: baseOf(e.Sp, bt), Subj: subj, Arms: arms}
}

// checkBlock types `{ stmts }`. Every statement but the last must
// have type Unit — a mid-block value is E0402 — and the block's type
// is the last statement's (Unit when empty). When the context passes
// an expected type the node takes it directly: the last statement
// was already checked against it, so reporting the block itself
// against it again would double every mismatch.
func (c *checker) checkBlock(e *syntax.Block, want types.Type) Expr {
	out := &Block{Base: baseOf(e.Sp, types.Unit), Stmts: make([]Expr, 0, len(e.Stmts))}
	for i, s := range e.Stmts {
		var st Expr
		if i == len(e.Stmts)-1 {
			st = c.checkExpr(s, want)
		} else {
			st = c.checkExpr(s, nil)
			switch t := types.Chase(st.Type()).(type) {
			case *types.Meta:
				// An unknown mid-block result is discarded: it must
				// be Unit. A constrained meta (numeric, ordered...)
				// can never be — that is the E0402 shape too.
				if r := c.recOf(t); r != nil && (r.num || r.flo || r.intg || r.ord) {
					c.diags.Add(midBlockUnitDiag(s, st.Type()))
				} else {
					c.bind(t, types.Unit, s.Span())
				}
			case *types.Con:
				if t.Qual == "" && t.Name == "Unit" {
					// The mid-block rule names the builtin Unit —
					// a file-local `type Unit` is an ADT, not it.
					if c.shadowedBuiltin("Unit") {
						c.shadowedErr(s.Span(), "a mid-block expression", "Unit")
					}
				} else {
					c.diags.Add(midBlockUnitDiag(s, st.Type()))
				}
			case *types.Var, *types.Func, *types.Tuple, *types.Go:
				c.diags.Add(midBlockUnitDiag(s, st.Type()))
			default:
				panic("unreachable")
			}
		}
		out.Stmts = append(out.Stmts, st)
	}
	if n := len(out.Stmts); n > 0 && want != nil {
		out.Base = baseOf(e.Sp, want)
	} else if n := len(out.Stmts); n > 0 {
		out.Base = baseOf(e.Sp, out.Stmts[n-1].Type())
	}
	// An empty block stays Unit — the context's want unifies against
	// it in checkExpr's trailing step (an `fn f () -> a = {}` is a
	// mismatch, not a silently ascribed `a`).
	return out
}

// midBlockUnitDiag builds the E0402 diagnostic of a non-Unit
// mid-block statement s. It carries the "discard the value with
// `ignore`" fix: wrapping the statement in `ignore (...)` makes the
// discard the code already performs explicit, so the fix is
// ApplicabilitySafe and `gom fix` applies it (spec section 1.1.2,
// section 12 item 5). The parens are needed — `ignore` binds at
// unary level, so `ignore a + b` would parse as `(ignore a) + b`.
func midBlockUnitDiag(s syntax.Expr, ty types.Type) diag.Diagnostic {
	sp := s.Span()
	return diag.Error("E0402",
		fmt.Sprintf("a mid-block expression must have type Unit, got %s", ty)).
		At(sp).
		Set("type", ty.String()).
		AddFix(diag.Fix{
			Title:         "discard the value with `ignore`",
			Applicability: diag.ApplicabilitySafe,
			Edits: []diag.Edit{
				{Span: diag.Point(sp.File, sp.Start), Text: "ignore ("},
				{Span: diag.Point(sp.File, sp.End), Text: ")"},
			},
		})
}

// checkLet types a block-level `let pat = e`: the binding's type is
// Unit and the bound names go into scope for the rest of the block.
// Local lets are monomorphic (spec 3.3): an annotation's type
// variables have no binder — a fresh `a` the enclosing signature
// never declared is E0412 (one declared there resolves to it).
func (c *checker) checkLet(e *syntax.Let) Expr {
	var want types.Type
	var annScope *tscope
	if e.Type != nil {
		annScope = c.newTScope()
		want = c.typeExpr(e.Type, annScope)
	} else {
		want = c.newMeta(e.Sp, "the let binding's type")
	}
	// The annotation's variables stay visible inside the value —
	// `let y : List a = xs` under `fn f (xs: List a)` resolves `a`
	// to f's variable, and a nested annotation can name it too.
	prevScope := c.curTScope
	if annScope != nil {
		c.curTScope = annScope
	}
	val := c.checkExpr(e.Value, want)
	c.curTScope = prevScope
	pat := c.checkPat(e.Pat, want)
	if e.Type != nil {
		c.unboundVars(want, e.Type.Span(), annScope)
	}
	return &Let{Base: baseOf(e.Sp, c.unitResult(e.Sp, "`let`")), Pat: pat, Value: val}
}

// checkTry types `try e`: e must be `Result a err`; the expression
// is `a`, and `err` must agree with the error channel of the
// innermost enclosing function boundary — which must itself return
// `Result _ err` (E0410, E0411). A boundary on the far side of a
// `go {}` is not an escape target (E0702).
func (c *checker) checkTry(e *syntax.Try) Expr {
	x := c.checkExpr(e.X, nil)
	a, errT, ok := c.resultArgs(x.Type(), e.X.Span())
	if !ok {
		if c.isShadowedCon(x.Type(), "Result") {
			c.shadowedErr(e.X.Span(), "`try`", "Result")
		} else {
			c.mismatch(e.X.Span(), "Result a e", x.Type())
		}
		// Recovery: the tried value is the operand itself. The
		// boundary shape checks below still run (they are separate
		// rules), but the error-channel unification is skipped —
		// binding the boundary's return to `Result ?a ?e` here
		// would only mint unsolvable metas on top of the mismatch
		// already reported.
		a = x.Type()
	}
	if len(c.bounds) == 0 {
		c.diags.Add(diag.Error("E0410",
			"`try` escapes to the enclosing function, but there is no enclosing function").
			At(e.Sp))
		return &Try{Base: baseOf(e.Sp, a), X: x}
	}
	b := c.bounds[len(c.bounds)-1]
	if b.goDepth != c.goDepth {
		c.diags.Add(diag.Error("E0702",
			"`try` cannot escape across a `go` boundary; the spawned task cannot return a Result").
			At(e.Sp).Set("boundary", b.kind))
		return &Try{Base: baseOf(e.Sp, a), X: x}
	}
	if ok {
		bv, berr, bok := c.resultArgs(b.ret, e.Sp)
		if !bok {
			if c.isShadowedCon(b.ret, "Result") {
				c.shadowedErr(e.Sp,
					fmt.Sprintf("the enclosing %s's declared return type", b.kind), "Result")
			} else {
				c.diags.Add(diag.Error("E0410",
					fmt.Sprintf("`try` requires the enclosing %s to return Result _ e, but it returns %s", b.kind, b.ret)).
					At(e.Sp).Set("return", b.ret.String()).Set("boundary", b.kind))
			}
			return &Try{Base: baseOf(e.Sp, a), X: x}
		}
		_ = bv
		if !c.unifyErr(berr, errT, e.X.Span()) {
			c.diags.Add(diag.Error("E0411",
				fmt.Sprintf("the error type of the tried Result (%s) does not match the %s's error type (%s)", errT, b.kind, berr)).
				At(e.Sp).Set("tried", errT.String()).Set("expected", berr.String()))
		}
	}
	return &Try{Base: baseOf(e.Sp, a), X: x}
}

// checkGoExpr types `go { ... }` — a spawned task. The body must be
// Unit (E0701) and the expression is Unit. Inside it, `try` no longer
// escapes to an outer function (E0702).
func (c *checker) checkGoExpr(e *syntax.GoExpr) Expr {
	c.goDepth++
	body := c.checkExpr(e.Body, nil)
	c.goDepth--
	switch t := types.Chase(body.Type()).(type) {
	case *types.Meta:
		// An unconstrained unknown may still be Unit; a
		// constrained one can never be — that is E0701. A literal
		// meta reports its default rather than the raw `?n`.
		if r := c.recOf(t); r != nil && (r.num || r.flo || r.intg || r.ord) {
			shown := body.Type()
			if r.dflt != nil {
				shown = r.dflt
			}
			c.diags.Add(diag.Error("E0701",
				fmt.Sprintf("a `go` body's type must be Unit, got %s", shown)).
				At(body.Span()).Set("type", shown.String()))
		} else {
			c.bind(t, types.Unit, body.Span())
		}
	case *types.Con:
		if t.Qual == "" && t.Name == "Unit" {
			// The `go` body's rule names the builtin Unit — a
			// file-local `type Unit` is an ADT, not it.
			if c.shadowedBuiltin("Unit") {
				c.shadowedErr(body.Span(), "a `go` body", "Unit")
			}
		} else {
			c.diags.Add(diag.Error("E0701",
				fmt.Sprintf("a `go` body's type must be Unit, got %s", body.Type())).
				At(body.Span()).Set("type", body.Type().String()))
		}
	case *types.Var, *types.Func, *types.Tuple, *types.Go:
		c.diags.Add(diag.Error("E0701",
			fmt.Sprintf("a `go` body's type must be Unit, got %s", body.Type())).
			At(body.Span()).Set("type", body.Type().String()))
	default:
		panic("unreachable")
	}
	return &GoExpr{Base: baseOf(e.Sp, c.unitResult(e.Sp, "`go {}`")), Body: body}
}

// checkFor types `for pat in iter { body }`: iter is a List or a
// receivable channel; the pattern binds each element; the body is Unit.
func (c *checker) checkFor(e *syntax.For) Expr {
	iter := c.checkExpr(e.Iter, nil)
	var elem types.Type
	switch t := types.Chase(iter.Type()).(type) {
	case *types.Con:
		if t.Qual == "" && t.Name == "List" && len(t.Args) == 1 &&
			!c.shadowedBuiltin("List") {
			elem = t.Args[0]
		} else if el, dir, ok := c.chanElem(t); ok {
			if dir == "send" {
				c.diags.Add(diag.Error("E0401",
					"cannot iterate a send-only channel").At(e.Iter.Span()))
			}
			elem = el
		} else {
			if t.Qual == "" && c.shadowedBuiltin(t.Name) &&
				(t.Name == "List" || t.Name == "Chan" || t.Name == "RecvChan" || t.Name == "SendChan") {
				c.shadowedErr(e.Iter.Span(),
					"`for` iteration", t.Name)
			} else {
				c.mismatch(e.Iter.Span(), "a List or a channel", iter.Type())
			}
			// Recovery: pretend the value iterates itself so the
			// pattern still has a concrete type to bind.
			elem = iter.Type()
		}
	case *types.Meta:
		elem = c.newMeta(e.Iter.Span(), "the iterated element's type")
		if !c.bind(t, types.List(elem), e.Iter.Span()) {
			c.markQuiet(elem)
		}
		// The bound meta spells `List` — under a file-local `type
		// List` that is the local ADT, which `for` cannot range
		// over: the element type needs an explicit channel or
		// builtin-List annotation. Quiet the minted element meta so
		// the E0415 does not grow a dangling-type cascade.
		if c.shadowedBuiltin("List") {
			c.shadowedErr(e.Iter.Span(), "`for` iteration", "List")
			c.markQuiet(elem)
		}
	case *types.Var, *types.Func, *types.Tuple, *types.Go:
		c.mismatch(e.Iter.Span(), "a List or a channel", iter.Type())
		elem = iter.Type()
	default:
		panic("unreachable")
	}
	pat := c.checkPat(e.Pat, elem)
	body := c.checkExpr(e.Body, types.Unit)
	return &For{Base: baseOf(e.Sp, c.unitResult(e.Sp, "`for`")), Pat: pat, Iter: iter, Body: body}
}

// checkSelectExpr types a channel `select`. recv arms bind
// `Option a`; send arms require a send-capable channel and a value of
// the element type; at least one arm is required (E0705) and at most
// one default arm is allowed (E0703).
func (c *checker) checkSelectExpr(e *syntax.SelectExpr, want types.Type) Expr {
	bt := want
	if len(e.Arms) == 0 {
		// `select {}` cannot evaluate — Go's empty select blocks
		// forever, and the Core validator would only report an
		// internal invariant. The user-facing rule lives here.
		c.diags.Add(diag.Error("E0705",
			"a `select` requires at least one arm").At(e.Sp))
		if bt == nil {
			bt = c.quietMeta(e.Sp, "the select arms' type")
		} else {
			c.markQuiet(bt)
		}
	}
	if bt == nil {
		bt = c.newMeta(e.Sp, "the select arms' type")
	}
	out := &SelectExpr{Base: baseOf(e.Sp, bt)}
	defaults := 0
	for _, a := range e.Arms {
		switch a := a.(type) {
		case *syntax.RecvArm:
			ch := c.checkExpr(a.Chan, nil)
			elem, dir, ok := c.chanElem(ch.Type())
			if !ok {
				_, isMeta := types.Chase(ch.Type()).(*types.Meta)
				if !isMeta {
					if con, isCon := types.Chase(ch.Type()).(*types.Con); isCon &&
						con.Qual == "" && c.shadowedBuiltin(con.Name) &&
						(con.Name == "Chan" || con.Name == "RecvChan" || con.Name == "SendChan") {
						c.shadowedErr(a.Chan.Span(),
							"a select receive arm's channel", con.Name)
					} else {
						c.mismatch(a.Chan.Span(), "a receivable channel", ch.Type())
					}
				}
				elem = c.newMeta(a.Chan.Span(), "the received element's type")
				if !isMeta || c.isQuiet(ch.Type()) {
					c.markQuiet(elem)
				}
			} else if dir == "send" {
				c.diags.Add(diag.Error("E0401",
					"cannot receive on a send-only channel").At(a.Chan.Span()))
			}
			// The bind side takes only a variable or `_` (spec
			// section 10.5): each arm is one Go select case, so a
			// destructuring pattern has nowhere to run. Anything
			// else is E0704; the pattern still checks for recovery
			// (its names stay visible to the body) but the typed
			// arm keeps the VarPat/WildPat invariant with a
			// wildcard.
			var bind Pat
			switch a.Bind.(type) {
			case nil, *syntax.VarPat, *syntax.WildPat:
				bind = c.checkPat(a.Bind, types.Option(elem))
				// The bound name's declared type is the builtin
				// `Option a` — under a file-local `type Option`
				// that reading is the local ADT, which the
				// emitted `option.FromGo` is not (E0415). An
				// unused bind never materializes the Option, so
				// only a bind the arm's body consumes is an error.
				if vp, isVar := bind.(*VarPat); isVar && vp.Sym != nil &&
					c.shadowedBuiltin("Option") {
					// referenced is marked by the arm-body check
					// below; defer the diagnostic until then.
					defer func() {
						if c.referenced[vp.Sym] {
							c.shadowedErr(vp.Sp,
								"a select receive arm's variable bind (it receives the builtin `Option a`)", "Option")
						}
					}()
				}
			case *syntax.LitPat, *syntax.CtorPat, *syntax.TuplePat,
				*syntax.RecordPat, *syntax.ListPat, *syntax.OrPat,
				*syntax.AsPat:
				c.diags.Add(diag.Error("E0704",
					"a select receive arm must bind a variable or `_`, not a destructuring pattern").
					At(a.Bind.Span()))
				_ = c.checkPat(a.Bind, types.Option(elem))
				bind = &WildPat{Base: baseOf(a.Bind.Span(), types.Option(elem))}
			default:
				panic("unreachable")
			}
			out.Arms = append(out.Arms, &RecvArm{
				Bind: bind,
				Chan: ch,
				Body: c.checkExpr(a.Body, bt),
			})
		case *syntax.SendArm:
			ch := c.checkExpr(a.Chan, nil)
			elem, dir, ok := c.chanElem(ch.Type())
			if !ok {
				_, isMeta := types.Chase(ch.Type()).(*types.Meta)
				if !isMeta {
					if con, isCon := types.Chase(ch.Type()).(*types.Con); isCon &&
						con.Qual == "" && c.shadowedBuiltin(con.Name) &&
						(con.Name == "Chan" || con.Name == "RecvChan" || con.Name == "SendChan") {
						c.shadowedErr(a.Chan.Span(),
							"a select send arm's channel", con.Name)
					} else {
						c.mismatch(a.Chan.Span(), "a sendable channel", ch.Type())
					}
				}
				elem = c.newMeta(a.Chan.Span(), "the sent element's type")
				if !isMeta || c.isQuiet(ch.Type()) {
					c.markQuiet(elem)
				}
			} else if dir == "recv" {
				c.diags.Add(diag.Error("E0401",
					"cannot send on a receive-only channel").At(a.Chan.Span()))
			}
			out.Arms = append(out.Arms, &SendArm{
				Chan:  ch,
				Value: c.checkExpr(a.Value, elem),
				Body:  c.checkExpr(a.Body, bt),
			})
		case *syntax.DefaultArm:
			defaults++
			if defaults > 1 {
				c.diags.Add(diag.Error("E0703",
					"a select may have at most one default arm").At(a.Sp))
			}
			out.Arms = append(out.Arms, &DefaultArm{
				Sp:   a.Sp,
				Body: c.checkExpr(a.Body, bt),
			})
		default:
			panic("unreachable")
		}
	}
	return out
}

// checkListLit types `[e1, e2, ..rest]`: one element type, a spread
// tail (a List or GoSlice of it), and `List a` as the node type.
func (c *checker) checkListLit(e *syntax.ListLit, want types.Type) Expr {
	elem := c.newMeta(e.Sp, "the list element type")
	// The literal produces the builtin `List a` — a file-local `type
	// List` shadows that reading, so `[e1, e2]` cannot produce the
	// declared type at all (E0415).
	if c.shadowedBuiltin("List") {
		c.shadowedErr(e.Sp, "a list literal `[...]`", "List")
	}
	out := &ListLit{Base: baseOf(e.Sp, types.List(elem))}
	for i, el := range e.Elems {
		if sp, ok := el.(*syntax.Spread); ok {
			if i != len(e.Elems)-1 {
				c.diags.Add(diag.Error("E0505",
					"`..e` is only allowed as the last element of a list literal").At(sp.Sp))
			}
			x := c.checkExpr(sp.X, nil)
			switch t := types.Chase(x.Type()).(type) {
			case *types.Con:
				if len(t.Args) == 1 && t.Qual == "Go" && t.Name == "Slice" {
					c.unify(elem, t.Args[0], sp.Sp)
				} else if len(t.Args) == 1 && t.Qual == "" && t.Name == "List" &&
					!c.shadowedBuiltin("List") {
					c.unify(elem, t.Args[0], sp.Sp)
				} else if t.Qual == "" && c.shadowedBuiltin(t.Name) {
					c.shadowedErr(sp.Sp, "the spread's element type", t.Name)
				} else {
					c.mismatch(sp.Sp, "a List or Go.Slice", x.Type())
				}
			case *types.Meta:
				if !c.bind(t, types.List(elem), sp.Sp) {
					c.markQuiet(elem)
				}
			case *types.Var, *types.Func, *types.Tuple, *types.Go:
				c.mismatch(sp.Sp, "a List or Go.Slice", x.Type())
			default:
				panic("unreachable")
			}
			out.Elems = append(out.Elems, &Spread{Base: baseOf(sp.Sp, x.Type()), X: x})
			continue
		}
		out.Elems = append(out.Elems, c.checkExpr(el, elem))
	}
	return out
}

// checkTupleLit types `(e1, e2, ...)`.
func (c *checker) checkTupleLit(e *syntax.TupleLit) Expr {
	out := &TupleLit{Base: baseOf(e.Sp, nil), Elems: make([]Expr, len(e.Elems))}
	ts := make([]types.Type, len(e.Elems))
	for i, el := range e.Elems {
		out.Elems[i] = c.checkExpr(el, nil)
		ts[i] = out.Elems[i].Type()
	}
	out.Base = baseOf(e.Sp, types.Tup(ts...))
	return out
}

// checkRecordLit types `{ name = e, ... }` and `{ base with name = e }`.
// The record type comes from the context (want or the base) or is
// resolved by field set — exactly one declared record may own all the
// written field names. Missing fields in a literal are an error;
// `with` inherits them from the base.
func (c *checker) checkRecordLit(e *syntax.RecordLit, want types.Type) Expr {
	var base Expr
	if e.Base != nil {
		base = c.checkExpr(e.Base, nil)
	}

	// Resolve the record type.
	var ti *typeInfo
	var sub types.Subst
	var wantT types.Type
	if base != nil {
		wantT = base.Type()
	} else {
		wantT = want
	}
	if con, ok := types.Chase(wantT).(*types.Con); ok {
		if t, ok2 := c.tyCons[conKey{qual: con.Qual, name: con.Name}]; ok2 && t.kind == kindRecord {
			ti = t
			sub = types.Subst{}
			for i, v := range ti.params {
				if i < len(con.Args) {
					sub[v.ID] = con.Args[i]
				}
			}
		} else if !ok2 || t.kind != kindRecord {
			c.mismatch(e.Sp, "a record type", wantT)
		}
	} else if _, isMeta := types.Chase(wantT).(*types.Meta); isMeta || wantT == nil {
		// Field-set resolution: exactly one declared record may own
		// every written name.
		ti = c.recordByFields(e)
		if ti != nil {
			// Instantiate the record's parameters with metas.
			args := make([]types.Type, len(ti.params))
			for i := range args {
				args[i] = c.newMeta(e.Sp, "the record type's argument")
			}
			sub = types.Subst{}
			for i, v := range ti.params {
				sub[v.ID] = args[i]
			}
			wantT = &types.Con{Qual: ti.con.Qual, Name: ti.con.Name, Args: args}
			if m, ok := types.Chase(want).(*types.Meta); ok {
				c.bind(m, wantT, e.Sp)
			} else if want == nil && base == nil {
				// nothing else to tie to
			}
		} else {
			// Resolution already reported (no candidate or an
			// ambiguous set). Bind the expected meta so the failure
			// does not leave an unsolved variable behind, and use
			// Unit as the recovery type.
			if m, ok := types.Chase(wantT).(*types.Meta); ok {
				c.bind(m, types.Unit, e.Sp)
			}
			wantT = types.Unit
		}
	} else {
		// The context expects a definite non-record type — a record
		// literal cannot have it (Var, Tuple, Func, Go all reach
		// here); reporting keeps the node from claiming a type it
		// does not have.
		c.mismatch(e.Sp, "a record type", wantT)
	}

	// Check each field init.
	out := &RecordLit{Base: baseOf(e.Sp, nil), With: base}
	seen := map[string]bool{}
	for _, f := range e.Fields {
		var fname string
		if f.Name != nil {
			fname = f.Name.Name
		}
		var ft types.Type
		var fsym *resolve.Symbol
		if ti != nil {
			if fi := c.fieldByName(ti.fields, fname); fi != nil {
				fsym = fi.sym
				if sub != nil {
					ft = sub.Apply(fi.typ)
				} else {
					ft = fi.typ
				}
			} else {
				c.diags.Add(diag.Error("E0401",
					fmt.Sprintf("record `%s` has no field named `%s`", ti.con.Name, fname)).
					At(f.Sp).Set("type", ti.con.Name).Set("field", fname))
			}
		}
		// When no record resolved this name, ft stays nil and the
		// value is checked without expectation — no extra mismatch
		// over the E0401 already reported.
		if seen[fname] {
			c.diags.Add(diag.Error("E0405",
				fmt.Sprintf("the field `%s` is supplied twice", fname)).
				At(f.Sp).Set("field", fname))
		}
		seen[fname] = true
		out.Fields = append(out.Fields, FieldInit{
			Sp:    f.Sp,
			Name:  fname,
			Sym:   fsym,
			Value: c.checkExpr(f.Value, ft),
		})
	}

	// A literal must supply every field; `with` inherits the rest.
	if ti != nil && base == nil {
		for _, fi := range ti.fields {
			if !seen[fi.name] {
				c.diags.Add(diag.Error("E0401",
					fmt.Sprintf("the record literal of `%s` is missing field `%s`", ti.con.Name, fi.name)).
					At(e.Sp).Set("type", ti.con.Name).Set("field", fi.name))
			}
		}
	}

	// The node's type is the record constructor applied to the
	// instantiation arguments.
	var rt types.Type
	if ti != nil {
		args := make([]types.Type, len(ti.params))
		for i, v := range ti.params {
			if s, ok := sub[v.ID]; ok {
				args[i] = s
			} else {
				args[i] = c.newMeta(e.Sp, "the record type's argument")
			}
		}
		rt = &types.Con{Qual: ti.con.Qual, Name: ti.con.Name, Args: args}
	} else if wantT != nil {
		rt = wantT
	} else {
		// ti == nil means resolution or the expectation already
		// reported — the fallback type is recovery.
		rt = c.quietMeta(e.Sp, "the record's type")
	}
	out.Base = baseOf(e.Sp, rt)
	return out
}

// recordByFields resolves a record literal's type when no expected
// type pins it: exactly one declared record may own every written
// field name. Ambiguity or no match reports and returns nil.
func (c *checker) recordByFields(e *syntax.RecordLit) *typeInfo {
	var cand *typeInfo
	count := 0
	names := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		if f.Name != nil {
			names = append(names, f.Name.Name)
		}
	}
	for _, ti := range c.records {
		ok := true
		for _, n := range names {
			if c.fieldByName(ti.fields, n) == nil {
				ok = false
				break
			}
		}
		if ok {
			cand = ti
			count++
		}
	}
	switch {
	case count == 0:
		c.diags.Add(diag.Error("E0401",
			fmt.Sprintf("no record type has fields %s", fmt.Sprint(names))).At(e.Sp))
		return nil
	case count > 1:
		c.diags.Add(diag.Error("E0407",
			fmt.Sprintf("the record literal is ambiguous: %d record types own these fields; annotate the type", count)).
			At(e.Sp))
		return nil
	default:
		return cand
	}
}

// goOriginType excludes file-local nominal types and shadowed builtins from
// the loader's Go type relation.
func (c *checker) goOriginType(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Go:
		return true
	case *types.Con:
		if t.Qual == "Go" {
			return true
		}
		if t.Qual != "" || c.shadowedBuiltin(t.Name) {
			return false
		}
		if (t.Name == "Ptr" || t.Name == "Chan" || t.Name == "RecvChan" || t.Name == "SendChan") && len(t.Args) == 1 {
			return c.goOriginType(t.Args[0])
		}
		return c.isBuiltinCon(t, "Int") || c.isBuiltinCon(t, "Int32") || c.isBuiltinCon(t, "Float") || c.isBuiltinCon(t, "Bool") || c.isBuiltinCon(t, "String") || c.isBuiltinCon(t, "Char") || c.isBuiltinCon(t, "GoError")
	case *types.Func, *types.Var, *types.Tuple, *types.Meta:
		return false
	default:
		panic("unreachable")
	}
}

// sliceConv checks the implicit List ↔ Go.Slice boundary conversion
// (spec section 8.5): an annotated position whose expected and actual
// types are the builtin List and `Go.Slice` in either order converts
// by copy. The Conv+Slice Ascribe records the choice — lowering reads
// the two types to pick ConvSliceToGo or ConvSliceFromGo. Element
// types must agree: equal solves, an unbound meta binds, and a
// `Go.Slice Go.Any` target accepts any element through boxing
// (ConvSliceBox). Anything else falls back to the ordinary mismatch.
func (c *checker) sliceConv(want, got types.Type, x Expr, sp diag.Span) Expr {
	wc, _ := types.Chase(want).(*types.Con)
	gc, _ := types.Chase(got).(*types.Con)
	if wc == nil || gc == nil || len(wc.Args) != 1 || len(gc.Args) != 1 {
		return nil
	}
	isGoSlice := func(con *types.Con) bool { return con.Qual == "Go" && con.Name == "Slice" }
	isList := func(con *types.Con) bool {
		return con.Qual == "" && con.Name == "List" && !c.shadowedBuiltin("List")
	}
	var we, ge types.Type
	box := false
	switch {
	case isGoSlice(wc) && isList(gc):
		we, ge = wc.Args[0], gc.Args[0]
		box = isGoAny(types.Chase(we)) && !isGoAny(types.Chase(ge))
	case isGoSlice(wc) && isGoSlice(gc):
		we, ge = wc.Args[0], gc.Args[0]
		box = isGoAny(types.Chase(we)) && !isGoAny(types.Chase(ge))
	case isList(wc) && isGoSlice(gc):
		we, ge = wc.Args[0], gc.Args[0]
	default:
		return nil
	}
	if !box && !types.Equal(we, ge) {
		_, wmeta := types.Chase(we).(*types.Meta)
		_, gmeta := types.Chase(ge).(*types.Meta)
		if !wmeta && !gmeta {
			return nil
		}
		if !c.unify(we, ge, sp) {
			return nil
		}
	}
	return &Ascribe{Base: baseOf(sp, want), X: x, Conv: true, Slice: true}
}

func (c *checker) goFieldOf(recv types.Type, name string, sp diag.Span) (*resolve.Symbol, types.Type, types.Type, bool) {
	if !c.goOriginType(recv) {
		return nil, nil, nil, false
	}
	if src, ok := c.src.(FieldSource); ok {
		if sym, sig, raw, found := src.FieldOf(recv, name); found {
			if c.hiddenDiag(sym, sp) {
				return sym, c.quietMeta(sp, "the type of Go field `"+name+"`"), raw, true
			}
			return sym, sig.Type, raw, true
		}
	}
	return nil, nil, nil, false
}

func (c *checker) revisitInterfaces() {
	for _, conv := range c.pendingIfaces {
		from := types.Chase(conv.X.Type())
		if _, ok := from.(*types.Meta); ok {
			continue
		}
		src, ok := c.src.(InterfaceSource)
		if !ok || !c.goOriginType(from) || !src.InterfaceAssignable(from, conv.Type()) {
			c.mismatch(conv.Sp, conv.Type(), from)
		}
	}
}

// Numeric inference and unresolved selectors have independent constraints that
// settle during finalize. Bare unconstrained parameters still infer an interface
// from their use, rather than becoming unconstrained polymorphic parameters.
func (c *checker) deferInterfaceSource(m *types.Meta) bool {
	if defaultOf(c.recOf(m)) != nil {
		return true
	}
	for _, member := range c.pendingSels {
		if types.Chase(member.Type()) == m {
			return true
		}
	}
	// A deferred application's result meta is equally unknowable at
	// conformance time: the replay's unify binds the real Go result
	// during revisitSelects, right before revisitInterfaces re-checks
	// the pending Ascribe — the same window the member metas use.
	for _, pa := range c.pendingApplies {
		if types.Chase(pa.ap.Type()) == m {
			return true
		}
	}
	return false
}
