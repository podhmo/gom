package check

import (
	"fmt"
	"strings"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// checkListLit types `[e1, e2, ..rest]`: one element type, a spread
// tail (a List or GoSlice of it), and `List a` as the node type. An
// expected `List T` pins the element before the elements check —
// under `List Go.Any` each element conforms into the `any` slot and
// the literal builds `[]any`, not `[]<concrete>` (Go generics are
// invariant; a `List[int64]` cannot serve a `List[any]` parameter).
func (c *checker) checkListLit(e *syntax.ListLit, want types.Type) Expr {
	elem := c.newMeta(e.Sp, "the list element type")
	// The literal produces the builtin `List a` — a file-local `type
	// List` shadows that reading, so `[e1, e2]` cannot produce the
	// declared type at all (E0415).
	if c.shadowedBuiltin("List") {
		c.shadowedErr(e.Sp, "a list literal `[...]`", "List")
	}
	if wc, ok := types.Chase(want).(*types.Con); ok && wc.Qual == "" &&
		wc.Name == "List" && len(wc.Args) == 1 && !c.shadowedBuiltin("List") {
		c.unifySub(wc.Args[0], elem, e.Sp)
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
					c.unifySub(elem, t.Args[0], sp.Sp)
				} else if len(t.Args) == 1 && t.Qual == "" && t.Name == "List" &&
					!c.shadowedBuiltin("List") {
					c.unifySub(elem, t.Args[0], sp.Sp)
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

// checkTupleLit types `(e1, e2, ...)`. An expected tuple of the same
// arity supplies each element's want — under `(Int, Go.Any)` the
// second element conforms into the `any` slot and the literal builds
// `rt.Tuple2[int64, any]` rather than the element's concrete type.
func (c *checker) checkTupleLit(e *syntax.TupleLit, want types.Type) Expr {
	out := &TupleLit{Base: baseOf(e.Sp, nil), Elems: make([]Expr, len(e.Elems))}
	ts := make([]types.Type, len(e.Elems))
	var wt *types.Tuple
	if w, ok := types.Chase(want).(*types.Tuple); ok && len(w.Elems) == len(e.Elems) {
		wt = w
	}
	for i, el := range e.Elems {
		var ew types.Type
		if wt != nil {
			ew = wt.Elems[i]
		}
		out.Elems[i] = c.checkExpr(el, ew)
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
		// every written name. A quiet base is already recovery owned
		// by an earlier literal, so do not report the same ambiguity
		// again at the update.
		if !(base != nil && c.isQuiet(wantT)) {
			ti = c.recordByFields(e)
		}
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
			// Resolution already reported, or an earlier quiet recovery
			// owns the failure. Keep the recovery type quiet rather than
			// manufacturing a Unit mismatch at later uses.
			if m, ok := types.Chase(wantT).(*types.Meta); ok {
				c.markQuiet(m)
			} else if wantT == nil {
				wantT = c.quietMeta(e.Sp, "the record's type")
			}
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
	pool := c.records
	// Foreign records join only when the literal's own file
	// imports their package — fileVis is nil for unscoped
	// sources, where every foreign record stays a candidate.
	for key, ti := range c.foreignRecs {
		if c.fileVis == nil || c.fileVis[e.Sp.File][key] {
			pool = append(pool, ti)
		}
	}
	for _, ti := range pool {
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
		return c.isBuiltinCon(t, "Int") || c.isBuiltinCon(t, "Int32") || c.isBuiltinCon(t, "Uint") || c.isBuiltinCon(t, "Float") || c.isBuiltinCon(t, "Bool") || c.isBuiltinCon(t, "String") || c.isBuiltinCon(t, "Char") || c.isBuiltinCon(t, "Byte") || c.isBuiltinCon(t, "GoError")
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
		if c.implementsIface(conv.Type(), from) {
			continue
		}
		src, ok := c.src.(InterfaceSource)
		if !ok || !c.goOriginType(from) || !src.InterfaceAssignable(from, conv.Type()) {
			if expectedFrom, ok := c.pendingIfaceFrom[conv]; ok {
				prev := c.expectedFrom
				c.expectedFrom = expectedFrom
				c.mismatch(conv.Sp, conv.Type(), from)
				c.expectedFrom = prev
			} else {
				c.mismatch(conv.Sp, conv.Type(), from)
			}
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

// typeTestKey is the Go `case` type an arm's target emits — the
// duplicate-detection key of a type-test match. A declared type
// whose name shadows a builtin (`type Int = ...`) emits its own
// nominal name, not the builtin repr, including when it appears
// inside a container type argument.
func (c *checker) typeTestKey(t types.Type) string {
	key := types.CanonGoRepr(types.GoRepr(c.typeTestNominal(t)))
	return strings.ReplaceAll(key, "\x00", "")
}

// typeTestNominal rewrites file-local declarations that shadow a
// builtin to NUL-delimited marker constructors types.GoRepr cannot
// mistake for the builtin. Nested arguments are rewritten with them;
// the impossible-in-Go NUL markers are stripped from the final key.
func (c *checker) typeTestNominal(t types.Type) types.Type {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		args := make([]types.Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = c.typeTestNominal(a)
		}
		name := t.Name
		if t.Qual == "" && c.shadowedBuiltin(name) {
			name = "\x00" + name + "\x00"
		}
		return &types.Con{Qual: t.Qual, Name: name, Args: args}
	case *types.Func:
		params := make([]types.FuncParam, len(t.Params))
		for i, p := range t.Params {
			params[i] = p
			params[i].Type = c.typeTestNominal(p.Type)
		}
		return &types.Func{Params: params, Ret: c.typeTestNominal(t.Ret)}
	case *types.Tuple:
		elems := make([]types.Type, len(t.Elems))
		for i, el := range t.Elems {
			elems[i] = c.typeTestNominal(el)
		}
		return &types.Tuple{Elems: elems}
	case *types.Go, *types.Meta, *types.Var:
		return t
	default:
		panic("unreachable")
	}
}
