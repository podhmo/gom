package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

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
			case sel != nil && isTryAs(sel):
				// `Go.try_as` alone is not a value; the extraction form
				// is `Go.try_as x : Option T`, which the ascription
				// checker reads straight from the surface apply. A bare
				// reference or a callee of a malformed call reports
				// E0514 — like `Go.conv`, keep it off the meta table.
				c.diags.Add(diag.Error("E0514",
					"`Go.try_as` is only valid as an extraction: `Go.try_as x : Option T`").At(e.Sp))
				typ = c.untypedMeta()
			case sel != nil && isNil(sel):
				// `Go.nil` is a value only through `Go.nil ()`; the
				// apply path (checkNilCall) mints the real result
				// meta and quiets this one. A bare reference keeps
				// `Unit -> ?a` — registering the meta makes an
				// indirect call (`{let f = Go.nil; f ()}`) answer
				// for the type it solves to, so `Unit -> Int32`
				// reports E0513 instead of emitting
				// `rt.Nil[int32]()` — which is 0, not a nil at all.
				m := c.newMeta(e.Sel.Sp, "the nil value's type")
				c.pendingNils = append(c.pendingNils, &pendingNil{sp: e.Sel.Sp, m: m})
				typ = &types.Func{Params: []types.FuncParam{types.P(types.Unit)}, Ret: m}
			case sel != nil && funcCallMember(sel):
				// `Go.funcN` is a conversion whose result type the
				// apply itself computes — like `Go.conv` the bare
				// member names no type. In callee position the
				// apply path (checkGoFuncCall) does the real work
				// and this meta is never inspected; a bare use
				// surfaces as the unsolved-meta E0407.
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
			rt := types.Display(recv.Type(), metaNamer())
			c.diags.Add(diag.Error("E0502",
				fmt.Sprintf("method calls are only valid on Go values; `%s` has Gom type %s", selName, rt)).
				At(e.Sel.Sp).Set("type", rt))
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
	// A member of a `Go`-namespace type's operation table
	// (`Go.Map.make`): resolve reached the symbol through the type's
	// member-table path, so the receiver is a namespace hop — an
	// untyped meta, never a value — and the selection types straight
	// from the member's signature like a constructor would.
	if sel != nil && namespaceRecv(recv) {
		typ, targs := c.valOf(sel, e.Sel.Sp)
		return &MemberExpr{Base: baseOf(e.Sp, typ), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1, TypeArgs: targs}
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
	if hasUnsolvedMeta(recv.Type()) {
		// The receiver's type is still unknown — either a bare meta
		// or a shape like `Ptr ?m` whose member set cannot be read
		// until the inner meta binds (a `let x = Ptr.new ()` handle
		// later pinned by its use). The node keeps a meta type;
		// Index stays -1 — and the selection is deferred: finalize
		// retries the lookup once the receiver's metas have bound
		// (`0.f` waits for the literal's Int default). When an
		// unsolved meta inside the receiver is quiet (a recovery
		// artifact), the member's is too.
		mt := c.newMeta(e.Sp, fmt.Sprintf("the type of `.%s`", selName))
		metasIn(recv.Type(), func(m *types.Meta) {
			if c.recOf(m).quiet {
				c.markQuiet(mt)
			}
		})
		me := &MemberExpr{Base: baseOf(e.Sp, mt), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1}
		c.pendingSels = append(c.pendingSels, me)
		return me
	}
	rtd := types.Display(recv.Type(), metaNamer())
	c.diags.Add(diag.Error("E0401",
		fmt.Sprintf("%s has no field or method `%s`", rtd, selName)).
		At(e.Sel.Sp).Set("type", rtd).Set("member", selName))
	return &MemberExpr{Base: baseOf(e.Sp, c.quietMeta(e.Sp, fmt.Sprintf("the type of `.%s`", selName))), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1}
}

// namespaceRecv reports whether a select's receiver stands for a
// namespace rather than a value — a module, type, constructor or
// builtin name — recognized by the untyped marker meta the
// namespace-hop builders above mint (untypedMeta). A genuine
// unsolved receiver types at a real meta (ID >= 0) and takes the
// deferred-selection path instead.
func namespaceRecv(recv Expr) bool {
	if m, ok := types.Chase(recv.Type()).(*types.Meta); ok && m.ID == -1 {
		return true
	}
	return false
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
	// The done marks live on the checker: finalize may call this once
	// per defaults round, and an entry processed in an earlier round
	// must not re-resolve (duplicate E0401) or re-check its RHS.
	if c.selDone == nil {
		c.selDone = map[*MemberExpr]bool{}
	}
	done := c.selDone
	// Field assignments deferred on a pending selection run inside
	// the same fixpoint: completing one checks its RHS, which may
	// itself register deferred selections for the next pass.
	if c.assignDone == nil {
		c.assignDone = map[*assignTarget]bool{}
	}
	doneAssigns := c.assignDone
	for progress := true; progress; {
		progress = false
		// Index-driven: selections a revisited RHS registers mid-pass
		// are visible to the next iteration.
		for i := 0; i < len(c.pendingSels); i++ {
			me := c.pendingSels[i]
			if done[me] {
				continue
			}
			rt := types.Chase(me.Recv.Type())
			if hasUnsolvedMeta(rt) {
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
				c.unifyMember(me, typ)
				resolved = true
			} else if me.Sym == nil {
				if field, typ, raw, ok := c.goFieldOf(rt, selName, me.Sel.Sp); ok {
					me.Sym, me.Index, me.GoFieldType = field, 0, raw
					c.unifyMember(me, typ)
					resolved = true
				} else if msym, typ, targs, ok := c.methodOf(rt, selName, me.Sel.Sp); ok {
					me.Sym = msym
					me.TypeArgs = targs
					c.unifyMember(me, typ)
					resolved = true
				}
			}
			if !resolved {
				rtd := types.Display(rt, metaNamer())
				c.diags.Add(diag.Error("E0401",
					fmt.Sprintf("%s has no field or method `%s`", rtd, selName)).
					At(me.Sel.Sp).Set("type", rtd).Set("member", selName))
				// The selection failed; the member's standing meta — nothing
				// bound it — must not pile an E0407 on the E0401.
				c.markQuiet(me.Type())
				// Deferred writes and calls waiting on this member are
				// dead too: their provisional checks ran with no want,
				// so their diagnostics and minted metas are debris that
				// would only pile spurious E0407/E0510 on the E0401.
				c.dropDeferredOn(me, doneAssigns)
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
		// Deferred field assignments validate their resolved shape
		// and check the RHS under the member's real signature — a
		// `Go.funcN` conversion or inline lambda sees the field's
		// `Go.FuncN` want exactly as an eagerly checked write would.
		for _, pa := range c.pendingAssigns {
			if doneAssigns[pa] || hasUnsolvedMeta(pa.Member.Recv.Type()) {
				continue
			}
			doneAssigns[pa] = true
			progress = true
			me := pa.Member
			if pa.diagMark >= 0 {
				// A provisional fill already checked this RHS with no
				// want — its diagnostics are superseded by the real
				// signature's check below. Drop exactly that range:
				// diagnostics emitted later (an unrelated deferred
				// select diagnosed in this same round) belong to
				// entries already marked done and would be lost.
				c.rollbackFill(pa)
			}
			if d := c.assignShapeDiag(pa.Sel, me); d != nil {
				c.diags.Add(*d)
				pa.Assign.Value = c.checkAssignRHS(pa, nil)
				continue
			}
			pa.Assign.Sym = me.Sym
			pa.Assign.GoFieldType = me.GoFieldType
			pa.Assign.Value = c.checkAssignRHS(pa, me.Type())
		}
	}
}

// rollbackFill drops the diagnostics a provisional nil-want fill
// emitted and shifts every later fill's marks by the removed count —
// removing the range moves every index after it.
func (c *checker) rollbackFill(pa *assignTarget) {
	removed := pa.diagEnd - pa.diagMark
	c.diags = append(c.diags[:pa.diagMark], c.diags[pa.diagEnd:]...)
	mark := pa.diagMark
	pa.diagMark = -1
	for _, q := range c.pendingAssigns {
		if q.diagMark > mark {
			q.diagMark -= removed
			q.diagEnd -= removed
		}
	}
}

// dropDeferredOn abandons the deferred field writes and applications
// waiting on a member that just failed to resolve: each provisional
// fill's diagnostic range rolls back, and every unbound meta its
// check minted (spans inside the dead node) goes quiet — a dead
// `x.F = Go.funcN (fn _ -> …)` reports the member's E0401 alone
// instead of a stack of annotation errors for a callback that never
// had a signature to check against.
func (c *checker) dropDeferredOn(me *MemberExpr, doneAssigns map[*assignTarget]bool) {
	for _, pa := range c.pendingAssigns {
		if pa.Member != me {
			continue
		}
		doneAssigns[pa] = true
		// The target's shape is validated independently of member
		// resolution — a non-`Ptr T` receiver still earns its E0511.
		if d := c.assignShapeDiag(pa.Sel, me); d != nil {
			c.diags.Add(*d)
		}
		if pa.diagMark >= 0 {
			c.rollbackFill(pa)
		}
		if pa.Assign.Value == nil {
			// The fill never ran — lower still needs a checked
			// node, so produce one and drop its debris at once.
			pa.diagMark = len(c.diags)
			pa.Assign.Value = c.checkAssignRHS(pa, nil)
			pa.diagEnd = len(c.diags)
			c.rollbackFill(pa)
		}
		c.quietMetasIn(pa.Assign.Value.Span())
	}
	for _, pa := range c.pendingApplies {
		if pa.me != me || pa.done {
			continue
		}
		pa.done = true
		pa.dropped = true
		c.quietMetasIn(pa.ap.Span())
	}
}

// finishAssigns completes the field assignments whose receiver never
// settled — finalize runs it once after the last defaults round.
// The member's own E0401/E0407 already covers the failure; the RHS
// still needs a checked node for lowering, and the dead member meta
// is no want: feeding it would only leak an E0407 onto an innocent
// literal.
func (c *checker) finishAssigns() bool {
	filled := false
	for _, pa := range c.pendingAssigns {
		if c.assignDone[pa] || pa.Assign.Value != nil {
			continue
		}
		filled = true
		pa.diagMark = len(c.diags)
		pa.Assign.Value = c.checkAssignRHS(pa, nil)
		pa.diagEnd = len(c.diags)
	}
	return filled
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
