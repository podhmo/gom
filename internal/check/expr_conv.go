package check

import (
	"fmt"
	"slices"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

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
	c.checkScopeTypeParam(e.Type, target, "ascription target")
	// `Go.conv e : T` — the surface wraps the argument in an
	// application of the conv builtin.
	if app, ok := e.X.(*syntax.Apply); ok {
		if sym := c.calleeSym(app.Fn); sym != nil && isConv(sym) {
			if len(app.Args) == 1 && app.Args[0].Label == nil && !app.Args[0].Spread {
				x := c.checkExpr(app.Args[0].Value, nil)
				// `Go.conv lit : T` emits `T(lit)` — a constant
				// conversion — so the literal's range is judged at
				// T rather than its own type.
				if lit, ok := x.(*Lit); ok {
					c.markLitConv(lit, target)
				}
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
		if sym := c.calleeSym(app.Fn); sym != nil && isTryAs(sym) {
			if len(app.Args) == 1 && app.Args[0].Label == nil && !app.Args[0].Spread {
				x := c.checkExpr(app.Args[0].Value, nil)
				if _, unsolved := types.Chase(x.Type()).(*types.Meta); unsolved {
					c.pendingConvs = append(c.pendingConvs,
						&pendingConv{x: x, to: target, sp: e.Sp, tryAs: true})
				} else {
					c.checkTryAs(x.Type(), target, e.Sp)
				}
				return &Ascribe{Base: baseOf(e.Sp, target), X: x, TryAs: true}
			}
			var arg syntax.Expr
			for _, a := range app.Args {
				if a.Value != nil {
					arg = a.Value
					break
				}
			}
			c.diags.Add(diag.Error("E0514",
				"`Go.try_as` takes exactly one argument").At(e.Sp))
			return &Ascribe{Base: baseOf(e.Sp, target), X: c.checkExpr(arg, nil), TryAs: true}
		}
	}
	x := c.checkExpr(e.X, target)
	td := types.Display(target, metaNamer())
	c.diags.Add(diag.Error("E0401",
		"`e : T` is only allowed as `Go.conv e : T`; there is no general ascription").
		At(e.Sp).Set("type", td))
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
		*syntax.Assign, *syntax.ListLit, *syntax.Spread, *syntax.TupleLit,
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
		if pc.tryAs {
			c.checkTryAs(pc.x.Type(), pc.to, pc.sp)
		} else {
			c.checkConv(pc.x.Type(), pc.to, pc.sp)
		}
	}
}

// checkTryAs validates `Go.try_as x : Option T` (decision
// 2026-11-24-go-any-extraction): the operand must be `Go.Any` and
// the target must be `Option T` over a concrete type — `Go.Any`
// itself and unsolved metas/type params are refused (E0514).
func (c *checker) checkTryAs(from, to types.Type, sp diag.Span) {
	name := metaNamer()
	from = types.Chase(from)
	if !isGoAny(from) {
		fd := types.Display(from, name)
		c.diags.Add(diag.Error("E0514",
			fmt.Sprintf("`Go.try_as` extracts from `Go.Any`, not %s", fd)).
			At(sp).Set("from", fd))
		return
	}
	oc, isOpt := types.Chase(to).(*types.Con)
	if !isOpt || oc.Qual != "" || oc.Name != "Option" || len(oc.Args) != 1 ||
		c.shadowedBuiltin("Option") {
		td := types.Display(to, name)
		c.diags.Add(diag.Error("E0514",
			fmt.Sprintf("`Go.try_as` target must be `Option T`, not %s", td)).
			At(sp).Set("to", td))
		return
	}
	t := oc.Args[0]
	if isGoAny(types.Chase(t)) || typeHasVarOrMeta(t) {
		td := types.Display(t, name)
		c.diags.Add(diag.Error("E0514",
			fmt.Sprintf("`Go.try_as` target must be a concrete type, not %s", td)).
			At(sp).Set("to", td))
	}
}

// typeHasVarOrMeta reports whether t mentions a type parameter or an
// unsolved meta anywhere — `Go.try_as` targets stay concrete.
func typeHasVarOrMeta(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Var, *types.Meta:
		return true
	case *types.Con:
		for _, a := range t.Args {
			if typeHasVarOrMeta(a) {
				return true
			}
		}
		return false
	case *types.Func:
		for _, p := range t.Params {
			if typeHasVarOrMeta(p.Type) {
				return true
			}
		}
		return typeHasVarOrMeta(t.Ret)
	case *types.Tuple:
		for _, e := range t.Elems {
			if typeHasVarOrMeta(e) {
				return true
			}
		}
		return false
	case *types.Go:
		for _, a := range t.Args {
			if typeHasVarOrMeta(a) {
				return true
			}
		}
		return false
	case nil:
		return false
	default:
		return false
	}
}

// checkConv reports E0507 for a conversion Go itself would reject:
// conversions are allowed between numeric types, between a named Go
// type and its underlying type (a builtin with the same shape), and
// from a Go named type to another type it may convert to. The
// ConvertSource oracle carries the go/types judgment for pairs the
// shape rules cannot decide (spec section 8.4).
func (c *checker) checkConv(from, to types.Type, sp diag.Span) {
	from = types.Chase(from)
	to = types.Chase(to)
	if !c.convOK(from, to) {
		name := metaNamer()
		fd, td := types.Display(from, name), types.Display(to, name)
		c.diags.Add(diag.Error("E0507",
			fmt.Sprintf("cannot convert %s to %s", fd, td)).
			At(sp).Set("from", fd).Set("to", td))
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
	// A one-field record emits as the Go defined type `type X F`,
	// so the conversion rules see its field type — `int64(u)` and
	// `UserId(7)` are the `F(x)`/`X(f)` pairs Go admits.
	from0, to0 := from, to
	from = c.definedUnderlying(from)
	to = c.definedUnderlying(to)
	if types.Equal(from, to) {
		return true
	}
	if _, ok := from.(*types.Meta); ok {
		return true
	}
	if _, ok := to.(*types.Meta); ok {
		return true
	}
	if from != from0 || to != to0 {
		// A peeled operand is a NAMED Go type: the emitted `T(x)`
		// stays legal only where the Go conversion rules admit a
		// named operand — identical underlying (the Equal above) or
		// both sides numeric basic kinds (`float64(u)`,
		// `int64(d)` for `type D = {v: time.Duration}`; litRange
		// asks the oracle about named underlyings). Everything else
		// borrows rules an unnamed F would enjoy but X does not:
		// the List↔Go.Slice boundary copy (ConvNamed emits a raw
		// cast that shares the backing), channel direction between
		// two named wrappers, and the oracle's interface
		// satisfaction (a defined type has no methods — X never
		// implements `fmt.Stringer`). Reject them as unconvertible.
		if _, _, _, ok := c.litRange(from); ok {
			_, _, _, ok2 := c.litRange(to)
			return ok2
		}
		// A List↔Go.Slice pair peels to the boundary-copy case —
		// ConvNamed emits a raw `T(x)` cast that shares the backing,
		// so it stays rejected even though the oracle would call the
		// rendered `[]T`↔`[]T` pair convertible.
		if _, _, ok := c.sliceListPair(from, to); ok {
			return false
		}
		// `chan<- T(x)` / `<-chan T(x)` / `S(x)`: a narrowing channel
		// conversion is legal when at least ONE side is unnamed —
		// `from == from0 || to == to0` means one operand was spelled
		// as the builtin constructor rather than peeled off a defined
		// type. `chan<- int64(c)` and `S(ch)` both compile; `S(c)`
		// between two named wrappers does not.
		if fc, ok := types.Chase(from).(*types.Con); ok &&
			c.isBuiltinCon(fc, "Chan") {
			if tc, ok := types.Chase(to).(*types.Con); ok &&
				(c.isBuiltinCon(tc, "SendChan") || c.isBuiltinCon(tc, "RecvChan")) {
				if (from == from0 || to == to0) &&
					c.convGoIdentical(fc.Args[0], tc.Args[0]) {
					return true
				}
			}
		}
		// The last unnamed-rule shape that survives a named operand is
		// identical underlying. The oracle cannot name it directly,
		// but it is the only rule that converts BOTH directions:
		// `string(text)`/`json.Number(s)` both hold for
		// `type Text = {value: String}`, while interface satisfaction
		// (`Duration`→`fmt.Stringer`) and channel direction are
		// one-directional and stay rejected.
		//
		// Pairs shaped like pointer/slice/map/chan/func values ride
		// the identical-underlying rule too — `(*int64)(p)` is legal
		// exactly when the two peelings render the same Go type
		// (`Ptr Int`↔`Ptr Int64`), which convGoIdentical judges
		// element-wise. The other rules such pairs could borrow all
		// require an unnamed operand a defined type never is, so a
		// non-identical shape pair skips the mutual-oracle check
		// (`*Duration`↔`*int64` must not pass).
		if convShapeType(from) || convShapeType(to) {
			return c.convGoIdentical(from, to)
		}
		if src, ok := c.src.(ConvertSource); ok {
			fwd, fok := src.GoConvertible(from, to)
			rev, rok := src.GoConvertible(to, from)
			if fok && rok {
				return fwd && rev
			}
		}
		return false
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
			case "Int", "Int32", "Int64", "Uint", "Uint64", "Float", "Char", "Byte":
				return true
			}
		}
		return false
	}
	if numeric(from) && numeric(to) {
		return true
	}
	// A `Go.conv` between `Go.Slice T` and `List U` goes through the
	// boundary copies of spec 8.5, which require the element types to
	// agree. The oracle cannot see the mismatch — boundaryType maps
	// both sides to their Go spelling, so `Go.Slice Int64` → `List Int`
	// or `Go.Slice Int32` → `List Char` judge `[]int64` → `[]int64` —
	// and the lowering's copy cases gate on the same equality, so an
	// accepted mismatch falls through to `ConvNamed`: a raw named cast
	// that shares backing and lets mutation reach a value Gom treats
	// as immutable (decisions/2026-11-05-goconv-slice-list-element-
	// mismatch). Reject mismatched element pairs here, at either
	// nesting depth of the elements' own spelling.
	if fe, te, ok := c.sliceListPair(from, to); ok && !types.Equal(fe, te) {
		return false
	}
	// A Gom-side declaration is invisible to the oracle, and it can
	// hide at any depth: boundaryType and gomOnly judge a Con by
	// name alone, so a file-local `type Int` ADT reads as the
	// builtin, a `type Foo` nested inside `List`/`Ptr`/`Go.Slice`
	// args reads as an unmapped element, and a qualified
	// `geom.Int` reads as `Int` too — any of them can come back
	// "unproven", falling to the *types.Go acceptance below and
	// emitting a conversion no Go compiler admits (`Int(x)` where
	// Int is the ADT). tyCons is the declaration oracle: local
	// declarations register under `{qual:""}` and foreign ones
	// (registerForeignTypes) under their package path, so a Con
	// naming a declared Gom type — at whatever nesting — rejects
	// the pair directly rather than skipping only the oracle
	// consult. A qualified Con the table does not know is still a
	// declaration's spelling: `Qual` is only ever assigned to a
	// package member or the `Go.` view.
	var containsGomDecl func(t types.Type) bool
	containsGomDecl = func(t types.Type) bool {
		switch t := types.Chase(t).(type) {
		case *types.Con:
			if t.Qual == "Go" {
				// A Go-side view (Go.Slice, Go.Map, Go.Int, ...):
				// mappable itself, but its arguments can still
				// hide a Gom declaration.
				return slices.ContainsFunc(t.Args, containsGomDecl)
			}
			if _, ok := c.tyCons[conKey{qual: t.Qual, name: t.Name}]; ok {
				return true
			}
			if t.Qual != "" {
				return true
			}
			return slices.ContainsFunc(t.Args, containsGomDecl)
		case *types.Func:
			for _, p := range t.Params {
				if containsGomDecl(p.Type) {
					return true
				}
			}
			return containsGomDecl(t.Ret)
		case *types.Tuple:
			return slices.ContainsFunc(t.Elems, containsGomDecl)
		case *types.Go:
			return slices.ContainsFunc(t.Args, containsGomDecl)
		case *types.Var, *types.Meta:
		default:
			panic("unreachable")
		}
		return false
	}
	if containsGomDecl(from) || containsGomDecl(to) {
		return false
	}
	if src, ok := c.src.(ConvertSource); ok {
		if decided, ok := src.GoConvertible(from, to); decided {
			return ok
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

// convShapeType reports whether t is a type whose Go conversion
// rules are shape rules — pointers, slices, maps, channels, funcs.
// For those, a peeled pair converts only via the identical-
// underlying rule (convGoIdentical judges it); the remaining rules
// each require an unnamed operand a defined type never is.
// Residual gap: a *types.Go side with an unnamed-shaped underlying
// (unsafe.Pointer, an unnamed imported pointer) is not detected.
func convShapeType(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Func:
		return true
	case *types.Con:
		if t.Qual == "" {
			switch t.Name {
			case "Ptr", "Chan", "SendChan", "RecvChan":
				return true
			}
			return false
		}
		if t.Qual == "Go" {
			if t.Name == "Slice" || t.Name == "Map" {
				return true
			}
			_, isFn := goFuncTypeArity(t.Name)
			return isFn
		}
		return false
	case *types.Var, *types.Meta, *types.Tuple, *types.Go:
		return false
	default:
		panic("unreachable")
	}
}

// convGoIdentical reports whether two peeled operand types render
// the same Go type — the test behind the identical-underlying
// conversion rule for shape types. `Int` and `Int64` both render
// `int64`, so `Ptr Int`≡`Ptr Int64`; `Ptr Int` vs `Ptr time.Duration`
// differ as `*int64` vs `*time.Duration`. Basic kinds compare by the
// emitted Go basic type; constructors compare name-for-name with
// pairwise arguments; anything else (named Go types, mismatched
// shapes) only counts when types.Equal already ruled it equal.
func (c *checker) convGoIdentical(a, b types.Type) bool {
	if types.Equal(a, b) {
		return true
	}
	ak, aok := c.convBasicKind(a)
	bk, bok := c.convBasicKind(b)
	if aok || bok {
		return aok && bok && ak == bk
	}
	ac, aok := types.Chase(a).(*types.Con)
	bc, bok := types.Chase(b).(*types.Con)
	if !aok || !bok || ac.Qual != bc.Qual || ac.Name != bc.Name ||
		len(ac.Args) != len(bc.Args) {
		return false
	}
	for i := range ac.Args {
		if !c.convGoIdentical(ac.Args[i], bc.Args[i]) {
			return false
		}
	}
	return true
}

// convBasicKind maps a builtin scalar constructor to the Go basic
// type it emits — used by convGoIdentical so the Int/Int64 and
// Char/Int32 spellings compare as the same Go type. Named Go types
// (*types.Go) are not basic: `time.Duration` shares int64's kind but
// is not the int64 type.
func (c *checker) convBasicKind(t types.Type) (string, bool) {
	con, ok := types.Chase(t).(*types.Con)
	if !ok || con.Qual != "" || c.shadowedBuiltin(con.Name) {
		return "", false
	}
	switch con.Name {
	case "Int", "Int64":
		return "int64", true
	case "Int32", "Char":
		return "int32", true
	case "Uint", "Uint64":
		return "uint64", true
	case "Float":
		return "float64", true
	case "Byte":
		return "uint8", true
	case "Bool":
		return "bool", true
	case "String":
		return "string", true
	}
	return "", false
}

// definedUnderlying peels a one-field record that emits as the Go
// defined type `type X F` to its field type — the same criterion
// `check.DefinedField` applies (kindRecord, one field, field not a
// bare type parameter), read off tyCons so both declared and
// imported records peel. Chases chains (`type M = {u: UserId}`)
// and substitutes declared params with the Con's args. A cyclic
// record (`type L = {next: L}` — its own E0413 stands elsewhere)
// is left unpeeled rather than looped on. Anything else returns
// unchanged.
func (c *checker) definedUnderlying(t types.Type) types.Type {
	seen := map[conKey]bool{}
	for {
		con, ok := types.Chase(t).(*types.Con)
		if !ok {
			return t
		}
		key := conKey{qual: con.Qual, name: con.Name}
		if seen[key] {
			return t
		}
		seen[key] = true
		ti, ok := c.tyCons[key]
		if !ok || ti.kind != kindRecord || len(ti.fields) != 1 {
			return t
		}
		f := ti.fields[0].typ
		if _, isVar := types.Chase(f).(*types.Var); isVar {
			return t // `type X a = {f: a}` stays a struct
		}
		if len(ti.params) > 0 {
			sub := types.Subst{}
			for i, p := range ti.params {
				if i < len(con.Args) {
					sub[p.ID] = con.Args[i]
				}
			}
			f = sub.Apply(f)
		}
		t = f
	}
}

// sliceListPair decomposes (from, to) when one side is the builtin
// `List` and the other `Go.Slice`, returning the two element types.
// Either direction matches — the same element-equality gate guards
// `List → Go.Slice` (the copy in) and `Go.Slice → List` (the copy
// out). A file-local `type List` is an ADT, not the builtin — its
// pairs fall through to the oracle like any other declared type.
func (c *checker) sliceListPair(from, to types.Type) (fromElem, toElem types.Type, ok bool) {
	elem := func(t types.Type) (types.Type, bool) {
		con, ok := t.(*types.Con)
		if !ok || len(con.Args) != 1 {
			return nil, false
		}
		switch {
		case con.Qual == "Go" && con.Name == "Slice":
			return con.Args[0], true
		case con.Qual == "" && con.Name == "List" && !c.shadowedBuiltin("List"):
			return con.Args[0], true
		}
		return nil, false
	}
	fe, fok := elem(from)
	te, tok := elem(to)
	if !fok || !tok {
		return nil, nil, false
	}
	fSlice := false
	if con, ok := from.(*types.Con); ok {
		fSlice = con.Qual == "Go" && con.Name == "Slice"
	}
	tSlice := false
	if con, ok := to.(*types.Con); ok {
		tSlice = con.Qual == "Go" && con.Name == "Slice"
	}
	if fSlice == tSlice {
		// Both `Go.Slice` (a verbatim pun — allowed) or both `List`
		// (types.Equal above already answered for equal element
		// types; a `List T` → `List U` mismatch is a plain E0401
		// elsewhere, not a conv question).
		return nil, nil, false
	}
	return fe, te, true
}
