package check

import (
	"fmt"

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
		// hole in the surface tree) is a meta-typed unit.
		m := c.newMeta(diag.Span{}, "a missing expression")
		if want != nil {
			c.unify(want, m, diag.Span{})
		}
		return &Lit{Base: baseOf(diag.Span{}, m), Kind: syntax.LitUnit, Text: "()"}
	}
	var out Expr
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
		out = &Hole{Base: baseOf(e.Sp, c.newMeta(e.Sp, "the placeholder's type"))}
	case *syntax.Lambda:
		out = c.checkLambda(e)
	case *syntax.If:
		out = c.checkIf(e, want)
	case *syntax.Match:
		out = c.checkMatch(e, want)
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
		out = c.checkSelectExpr(e, want)
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
		out = c.checkRecordLit(e, want)
	case *syntax.Ascribe:
		out = c.checkAscribe(e)
	default:
		panic("unreachable")
	}
	if want != nil {
		c.unify(want, out.Type(), e.Span())
	}
	return out
}

// checkIdent resolves a value-position identifier.
func (c *checker) checkIdent(e *syntax.Ident) Expr {
	sym := c.useSym(e)
	return &Var{Base: baseOf(e.Sp, c.valOf(sym, e.Sp)), Sym: sym}
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
			if sel != nil && isConv(sel) {
				// `Go.conv` alone is not a value; the conversion form
				// is `Go.conv e : T` (the apply/ascription checker
				// reads it without evaluating this node). Its type is
				// not representable — keep it off the meta table so a
				// stray `Go.conv` never asks for an annotation.
				typ = c.untypedMeta()
			} else {
				typ = c.valOf(sel, e.Sel.Sp)
			}
			return &MemberExpr{Base: baseOf(e.Sp, typ), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1}
		}
	}

	// A type name in receiver position (`Option.Some`, `Shape.Circle`)
	// is a namespace, not a value: checking it as an expression would
	// mint a meta that can never solve. The same is true of a
	// constructor or module bound the resolver already produced.
	var recv Expr
	if id, ok := e.Recv.(*syntax.Ident); ok {
		if rs := c.res.UseOf(id); rs != nil {
			switch rs.Kind {
			case resolve.KindType, resolve.KindCtor, resolve.KindModule, resolve.KindBuiltin:
				recv = &Var{Base: baseOf(id.Sp, c.untypedMeta()), Sym: rs}
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
		return &MemberExpr{Base: baseOf(e.Sp, c.valOf(sel, e.Sel.Sp)), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1}
	}
	// A constructor selected through a type or package (`Option.Some`
	// with the receiver already resolved); the callee checker picks
	// it up by symbol.
	if sel != nil && sel.Kind == resolve.KindCtor {
		return &MemberExpr{Base: baseOf(e.Sp, c.valOf(sel, e.Sel.Sp)), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1}
	}
	// Field access on a declared record or a variant's uniquely named
	// payload slot.
	if fi, typ, ok := c.fieldOf(recv.Type(), selName); ok {
		return &MemberExpr{Base: baseOf(e.Sp, typ), Recv: recv, Sel: e.Sel, Sym: fi.sym, Index: fi.index}
	}
	// A field symbol the resolver bound but the declared-type tables
	// cannot place — a Go struct field or an imported record field
	// (both carry signatures in the SignatureSource).
	if sel != nil && sel.Kind == resolve.KindField {
		return &MemberExpr{Base: baseOf(e.Sp, c.valOf(sel, e.Sel.Sp)), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1}
	}
	// A method selected on a Go-typed receiver that the resolver
	// could not see: the SignatureSource's optional MethodSource
	// looks the name up in the named type's method set (P1-9).
	if sel == nil {
		if msym, typ, ok := c.methodOf(recv.Type(), selName, e.Sel.Sp); ok {
			return &MemberExpr{Base: baseOf(e.Sp, typ), Recv: recv, Sel: e.Sel, Sym: msym, Index: -1}
		}
	}
	if _, ok := types.Chase(recv.Type()).(*types.Meta); ok {
		// The receiver's type is still unknown. The node keeps a
		// meta type; Index stays -1 — field-dependent lowering sees
		// an unresolved selector.
		return &MemberExpr{Base: baseOf(e.Sp, c.newMeta(e.Sp, fmt.Sprintf("the type of `.%s`", selName))), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1}
	}
	c.diags.Add(diag.Error("E0401",
		fmt.Sprintf("%s has no field or method `%s`", recv.Type(), selName)).
		At(e.Sel.Sp).Set("type", recv.Type().String()).Set("member", selName))
	return &MemberExpr{Base: baseOf(e.Sp, c.newMeta(e.Sp, fmt.Sprintf("the type of `.%s`", selName))), Recv: recv, Sel: e.Sel, Sym: sel, Index: -1}
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
func (c *checker) fieldOf(t types.Type, name string) (*fieldInfo, types.Type, bool) {
	con, ok := types.Chase(t).(*types.Con)
	if !ok {
		return nil, nil, false
	}
	ti, ok := c.tyCons[conKey{qual: con.Qual, name: con.Name}]
	if !ok || ti == nil {
		return nil, nil, false
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
			return fi, sub.Apply(fi.typ), true
		}
	case kindSum:
		// A named payload slot of a sum type is selectable when
		// exactly one variant declares it.
		var found *fieldInfo
		for _, v := range ti.variants {
			for _, f := range v.fields {
				if f.name == name {
					if found != nil && found.sym != f.sym {
						return nil, nil, false
					}
					found = f
				}
			}
		}
		if found != nil {
			return found, sub.Apply(found.typ), true
		}
	}
	return nil, nil, false
}

// methodOf resolves a method name on a Go-typed receiver (a
// types.Go, the builtin GoError, or a Ptr to one) through the
// optional MethodSource of the signature source. The returned type
// is the instantiated method signature — the receiver itself is not
// a parameter of it.
func (c *checker) methodOf(recvT types.Type, name string, sp diag.Span) (*resolve.Symbol, types.Type, bool) {
	t := types.Chase(recvT)
	if con, ok := t.(*types.Con); ok && con.Qual == "" && con.Name == "Ptr" && len(con.Args) == 1 {
		t = types.Chase(con.Args[0])
	}
	if _, ok := t.(*types.Go); !ok && !types.Equal(t, types.GoError) {
		return nil, nil, false
	}
	ms, ok := c.src.(MethodSource)
	if !ok {
		return nil, nil, false
	}
	sym, sig, ok := ms.MethodOf(recvT, name)
	if !ok {
		return nil, nil, false
	}
	if c.hiddenDiag(sym, sp) {
		return sym, c.newMeta(sp, fmt.Sprintf("the type of method `%s`", name)), true
	}
	typ, _ := c.instantiate(sig.Type, sp)
	return sym, typ, true
}

// checkAscribe types `e : T` — and `Go.conv e : T`, the explicit Go
// conversion. The ascribed type is authoritative: the expression must
// have it. For Go.conv the checker also validates that Go's own
// conversion rules could apply — between numeric types or involving
// a Go handle (E0507). The full Go convertibility check belongs to
// goimport (P1-9); here the rule is the conservative shape check.
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
				c.checkConv(x.Type(), target, e.Sp)
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

// checkConv reports E0507 for a conversion Go itself would reject:
// conversions are allowed between numeric types, between a named Go
// type and its underlying type (a builtin with the same shape), and
// from a Go named type to another type it may convert to. Without
// go/types the checker accepts anything that is not a statically
// impossible pair — functions to scalars, records to numbers, etc.
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
// keeps the target type).
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
		if con.Qual == "" {
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
	// Named Go types convert freely for now — the underlying-type
	// check needs go/types data (P1-9).
	if _, ok := from.(*types.Go); ok {
		return true
	}
	if _, ok := to.(*types.Go); ok {
		return true
	}
	// String and Char convert in Go's rules (rune slice, etc.) only
	// in specific directions the checker does not model; accept the
	// same-type cases already handled and reject the rest.
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
		return &Unary{Base: baseOf(e.Sp, types.Bool), Op: e.Op, X: x}
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
		c.unify(x.Type(), y.Type(), e.Sp)
		c.constrain(x.Type(), e.Sp, "the operands of `==`", func(r *metaRec) { r.eq = true })
		return &Binary{Base: baseOf(e.Sp, types.Bool), Op: op, X: x, Y: y}
	case syntax.BinLt, syntax.BinLe, syntax.BinGt, syntax.BinGe:
		x := c.checkExpr(e.X, nil)
		y := c.checkExpr(e.Y, nil)
		c.unify(x.Type(), y.Type(), e.Sp)
		c.constrain(x.Type(), e.Sp, "the operands of a comparison", func(r *metaRec) { r.ord = true })
		return &Binary{Base: baseOf(e.Sp, types.Bool), Op: op, X: x, Y: y}
	case syntax.BinAnd, syntax.BinOr:
		x := c.checkExpr(e.X, types.Bool)
		y := c.checkExpr(e.Y, types.Bool)
		return &Binary{Base: baseOf(e.Sp, types.Bool), Op: op, X: x, Y: y}
	case syntax.BinPipeFwd:
		// `x |> f` is `f x`.
		x := c.checkExpr(e.X, nil)
		f := c.checkExpr(e.Y, nil)
		ret := c.newMeta(e.Sp, "the result of `|>`")
		c.unify(f.Type(), types.Fn(ret, types.P(x.Type())), e.Sp)
		return &Binary{Base: baseOf(e.Sp, ret), Op: op, X: x, Y: f}
	case syntax.BinPipeBack:
		// f left-pipe x applies f to x.
		f := c.checkExpr(e.X, nil)
		x := c.checkExpr(e.Y, nil)
		ret := c.newMeta(e.Sp, "the result of left-pipe")
		c.unify(f.Type(), types.Fn(ret, types.P(x.Type())), e.Sp)
		return &Binary{Base: baseOf(e.Sp, ret), Op: op, X: f, Y: x}
	case syntax.BinPipeTry:
		return c.checkPipeTry(e)
	case syntax.BinComposeFwd:
		// f >> g is x -> g (f x).
		x := c.checkExpr(e.X, nil)
		g := c.checkExpr(e.Y, nil)
		a := c.newMeta(e.Sp, "the composed function's input")
		b := c.newMeta(e.Sp, "the composed function's middle type")
		r := c.newMeta(e.Sp, "the composed function's result")
		c.unify(x.Type(), types.Fn(b, types.P(a)), e.X.Span())
		c.unify(g.Type(), types.Fn(r, types.P(b)), e.Y.Span())
		return &Binary{Base: baseOf(e.Sp, types.Fn(r, types.P(a))), Op: op, X: x, Y: g}
	case syntax.BinComposeBack:
		// g << f is x -> g (f x).
		g := c.checkExpr(e.X, nil)
		f := c.checkExpr(e.Y, nil)
		a := c.newMeta(e.Sp, "the composed function's input")
		b := c.newMeta(e.Sp, "the composed function's middle type")
		r := c.newMeta(e.Sp, "the composed function's result")
		c.unify(f.Type(), types.Fn(b, types.P(a)), e.Y.Span())
		c.unify(g.Type(), types.Fn(r, types.P(b)), e.X.Span())
		return &Binary{Base: baseOf(e.Sp, types.Fn(r, types.P(a))), Op: op, X: g, Y: f}
	default:
		panic("unreachable")
	}
}

// checkArith types +,-,*,/ and %: both operands share one numeric
// type (integral for %), which is also the result.
func (c *checker) checkArith(e *syntax.Binary, constrain func(*metaRec)) Expr {
	x := c.checkExpr(e.X, nil)
	y := c.checkExpr(e.Y, nil)
	c.unify(x.Type(), y.Type(), e.Sp)
	c.constrain(x.Type(), e.Sp, "the operands of an arithmetic operator", constrain)
	return &Binary{Base: baseOf(e.Sp, x.Type()), Op: e.Op, X: x, Y: y}
}

// checkPipeTry types the Result-bind pipeline `x |?> f`: when x is
// `Result a e` it unwraps to a, f must be `a -> Result b e`, and the
// whole expression is `Result b e`. When x is not a Result it is
// wrapped in Ok first, per spec 9.2.7.
func (c *checker) checkPipeTry(e *syntax.Binary) Expr {
	x := c.checkExpr(e.X, nil)
	f := c.checkExpr(e.Y, nil)
	var argT, errT types.Type
	if a, er, ok := c.resultArgs(x.Type(), e.X.Span()); ok {
		argT, errT = a, er
	} else {
		// Non-Result left side: the value is used as-is (Ok-wrapped
		// at evaluation). The step still returns a Result.
		argT = x.Type()
		errT = c.newMeta(e.Sp, "the pipeline error type")
	}
	resT := c.newMeta(e.Sp, "the |?> result type")
	c.unify(f.Type(), types.Fn(types.Result(resT, errT), types.P(argT)), e.Y.Span())
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
	c.bounds = append(c.bounds, boundary{ret: ret, sp: e.Sp, goDepth: c.goDepth, kind: "lambda"})
	body := c.checkExpr(e.Body, ret)
	c.bounds = c.bounds[:len(c.bounds)-1]
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
		return &If{Base: baseOf(e.Sp, types.Unit), Cond: cond, Then: then}
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
		var guard Expr
		if a.Guard != nil {
			guard = c.checkExpr(a.Guard, types.Bool)
		}
		arms[i] = MatchArm{
			Sp:    a.Sp,
			Pat:   c.checkPat(a.Pat, subj.Type()),
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
					c.diags.Add(diag.Error("E0402",
						fmt.Sprintf("a mid-block expression must have type Unit, got %s", st.Type())).
						At(s.Span()).Set("type", st.Type().String()))
				} else {
					c.bind(t, types.Unit, s.Span())
				}
			case *types.Con:
				if !(t.Qual == "" && t.Name == "Unit") {
					c.diags.Add(diag.Error("E0402",
						fmt.Sprintf("a mid-block expression must have type Unit, got %s", st.Type())).
						At(s.Span()).Set("type", st.Type().String()))
				}
			case *types.Var, *types.Func, *types.Tuple, *types.Go:
				c.diags.Add(diag.Error("E0402",
					fmt.Sprintf("a mid-block expression must have type Unit, got %s", st.Type())).
					At(s.Span()).Set("type", st.Type().String()))
			default:
				panic("unreachable")
			}
		}
		out.Stmts = append(out.Stmts, st)
	}
	if want != nil {
		out.Base = baseOf(e.Sp, want)
	} else if n := len(out.Stmts); n > 0 {
		out.Base = baseOf(e.Sp, out.Stmts[n-1].Type())
	}
	return out
}

// checkLet types a block-level `let pat = e`: the binding's type is
// Unit and the bound names go into scope for the rest of the block.
func (c *checker) checkLet(e *syntax.Let) Expr {
	var want types.Type
	if e.Type != nil {
		want = c.typeExpr(e.Type, c.newTScope())
	} else {
		want = c.newMeta(e.Sp, "the let binding's type")
	}
	val := c.checkExpr(e.Value, want)
	pat := c.checkPat(e.Pat, want)
	return &Let{Base: baseOf(e.Sp, types.Unit), Pat: pat, Value: val}
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
		c.mismatch(e.X.Span(), "Result a e", x.Type())
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
			c.diags.Add(diag.Error("E0410",
				fmt.Sprintf("`try` requires the enclosing %s to return Result _ e, but it returns %s", b.kind, b.ret)).
				At(e.Sp).Set("return", b.ret.String()).Set("boundary", b.kind))
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
		// constrained one can never be — that is E0701.
		if r := c.recOf(t); r != nil && (r.num || r.flo || r.intg || r.ord) {
			c.diags.Add(diag.Error("E0701",
				fmt.Sprintf("a `go` body's type must be Unit, got %s", body.Type())).
				At(body.Span()).Set("type", body.Type().String()))
		} else {
			c.bind(t, types.Unit, body.Span())
		}
	case *types.Con:
		if !(t.Qual == "" && t.Name == "Unit") {
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
	return &GoExpr{Base: baseOf(e.Sp, types.Unit), Body: body}
}

// checkFor types `for pat in iter { body }`: iter is a List or a
// receivable channel; the pattern binds each element; the body is Unit.
func (c *checker) checkFor(e *syntax.For) Expr {
	iter := c.checkExpr(e.Iter, nil)
	var elem types.Type
	switch t := types.Chase(iter.Type()).(type) {
	case *types.Con:
		if t.Qual == "" && t.Name == "List" && len(t.Args) == 1 {
			elem = t.Args[0]
		} else if el, dir, ok := chanElem(t); ok {
			if dir == "send" {
				c.diags.Add(diag.Error("E0401",
					"cannot iterate a send-only channel").At(e.Iter.Span()))
			}
			elem = el
		} else {
			c.mismatch(e.Iter.Span(), "a List or a channel", iter.Type())
			// Recovery: pretend the value iterates itself so the
			// pattern still has a concrete type to bind.
			elem = iter.Type()
		}
	case *types.Meta:
		elem = c.newMeta(e.Iter.Span(), "the iterated element's type")
		c.bind(t, types.List(elem), e.Iter.Span())
	case *types.Var, *types.Func, *types.Tuple, *types.Go:
		c.mismatch(e.Iter.Span(), "a List or a channel", iter.Type())
		elem = iter.Type()
	default:
		panic("unreachable")
	}
	pat := c.checkPat(e.Pat, elem)
	body := c.checkExpr(e.Body, types.Unit)
	return &For{Base: baseOf(e.Sp, types.Unit), Pat: pat, Iter: iter, Body: body}
}

// checkSelectExpr types a channel `select`. recv arms bind
// `Option a`; send arms require a send-capable channel and a value of
// the element type; at most one default arm is allowed (E0703).
func (c *checker) checkSelectExpr(e *syntax.SelectExpr, want types.Type) Expr {
	bt := want
	if bt == nil {
		bt = c.newMeta(e.Sp, "the select arms' type")
	}
	out := &SelectExpr{Base: baseOf(e.Sp, bt)}
	defaults := 0
	for _, a := range e.Arms {
		switch a := a.(type) {
		case *syntax.RecvArm:
			ch := c.checkExpr(a.Chan, nil)
			elem, dir, ok := chanElem(ch.Type())
			if !ok {
				if _, isMeta := types.Chase(ch.Type()).(*types.Meta); !isMeta {
					c.mismatch(a.Chan.Span(), "a receivable channel", ch.Type())
				}
				elem = c.newMeta(a.Chan.Span(), "the received element's type")
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
			elem, dir, ok := chanElem(ch.Type())
			if !ok {
				if _, isMeta := types.Chase(ch.Type()).(*types.Meta); !isMeta {
					c.mismatch(a.Chan.Span(), "a sendable channel", ch.Type())
				}
				elem = c.newMeta(a.Chan.Span(), "the sent element's type")
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
				if len(t.Args) == 1 &&
					((t.Qual == "" && t.Name == "List") || (t.Qual == "Go" && t.Name == "Slice")) {
					c.unify(elem, t.Args[0], sp.Sp)
				} else {
					c.mismatch(sp.Sp, "a List or Go.Slice", x.Type())
				}
			case *types.Meta:
				c.bind(t, types.List(elem), sp.Sp)
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
		rt = c.newMeta(e.Sp, "the record's type")
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
