package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// checkPat checks a surface pattern against the expected type and
// returns its typed form. Every VarPat and AsPat binder registers its
// symbol in c.vals; or-patterns merge the bindings of all
// alternatives. The pattern checker (phase 5) judges refutability and
// or-pattern consistency — this function only types.
func (c *checker) checkPat(p syntax.Pat, want types.Type) Pat {
	if p == nil {
		// A missing pattern (incomplete hand-built input) degrades
		// to a wildcard rather than a panic.
		return &WildPat{Base: baseOf(diag.Span{}, want)}
	}
	switch p := p.(type) {
	case *syntax.WildPat:
		return &WildPat{Base: baseOf(p.Sp, want)}
	case *syntax.VarPat:
		sym := c.declSym(p.Name, resolve.KindVar)
		c.bindSym(sym, want)
		return &VarPat{Base: baseOf(p.Sp, want), Sym: sym}
	case *syntax.LitPat:
		lt := c.litType(p.Lit)
		if !c.unify(want, lt, p.Sp) {
			c.markQuiet(lt)
		}
		out := &LitPat{Base: baseOf(p.Sp, want), Kind: p.Lit.Kind, Text: p.Lit.Text}
		c.watchPatLit(out)
		return out
	case *syntax.CtorPat:
		return c.checkCtorPat(p, want)
	case *syntax.TuplePat:
		elems := c.tupleElems(want, len(p.Elems), p.Sp)
		out := &TuplePat{Base: baseOf(p.Sp, want), Elems: make([]Pat, len(p.Elems))}
		for i, e := range p.Elems {
			out.Elems[i] = c.checkPat(e, elems[i])
		}
		return out
	case *syntax.RecordPat:
		return c.checkRecordPat(p, want)
	case *syntax.ListPat:
		elem := c.listElem(want, p.Sp)
		out := &ListPat{Base: baseOf(p.Sp, want)}
		for _, e := range p.Elems {
			out.Elems = append(out.Elems, c.checkPat(e, elem))
		}
		if p.Rest != nil {
			out.Rest = c.checkPat(p.Rest, want)
		}
		return out
	case *syntax.OrPat:
		out := &OrPat{Base: baseOf(p.Sp, want), Alts: make([]Pat, len(p.Alts))}
		for i, alt := range p.Alts {
			out.Alts[i] = c.checkPat(alt, want)
		}
		return out
	case *syntax.AsPat:
		inner := c.checkPat(p.Pat, want)
		sym := c.declSym(p.Name, resolve.KindVar)
		c.bindSym(sym, want)
		return &AsPat{Base: baseOf(p.Sp, want), Pat: inner, Sym: sym}
	default:
		panic("unreachable")
	}
}

// bindSym records the type of a pattern-bound variable. When the
// symbol already carries a type — top-level lets are pre-registered
// so earlier references resolve — the binding unifies with it instead
// of overwriting.
func (c *checker) bindSym(sym *resolve.Symbol, t types.Type) {
	if sym == nil {
		return
	}
	if prev, ok := c.vals[sym]; ok {
		c.unify(prev, t, sym.DefSpan())
		return
	}
	c.vals[sym] = t
}

// checkCtorPat resolves the constructor, instantiates its signature
// with fresh metas, and checks the payload patterns — positional,
// `~label:` arguments, or the `{ name = pat }` field form.
func (c *checker) checkCtorPat(p *syntax.CtorPat, want types.Type) Pat {
	vi, _ := c.ctorOf(p.Ctor)
	out := &CtorPat{Base: baseOf(p.Sp, want), Name: p.Ctor}
	if vi == nil {
		// Unresolved constructor (the resolver reported it); check
		// the payload against Unit for recovery — a dangling meta
		// would only add an unsolvable E0407 to the resolver's
		// diagnostic (the same convention checkRecordPat uses).
		for _, a := range p.Args {
			var label string
			if a.Label != nil {
				label = a.Label.Name
			}
			out.Args = append(out.Args, CtorArg{
				Label: label,
				Pat:   c.checkPat(a.Pat, types.Unit),
			})
		}
		for _, f := range p.Fields {
			out.Fields = append(out.Fields, c.fieldPatOf(f, types.Unit, nil, -1))
		}
		out.Rest = p.Rest
		return out
	}
	out.Ctor = vi.sym
	params, ret, targs := c.ctorParams(vi, p.Sp)
	if !c.unify(want, ret, p.Sp) {
		// The constructor's instantiation arguments can no longer
		// solve — a `Some v` pattern against a Tuple subject says
		// nothing about `a`; silence them under the mismatch.
		for _, t := range targs {
			c.markQuiet(t)
		}
	} else {
		// `unify` never binds a meta to a `Go.Any` want — the
		// accept-everything slot deliberately stays unpinned
		// (unify.go). Here the want is the scrutinee's own type,
		// so its `any` argument *is* the ctor instantiation's
		// answer: `Result Go.Any GoError` pins `Ok`'s `a` to
		// `Go.Any` — pin it, or the argument dangles into E0407
		// (docs/decisions/
		// 2026-11-06-nilable-iface-pair-ok-payload.md).
		if wc, ok := types.Chase(want).(*types.Con); ok {
			if rc, ok := types.Chase(ret).(*types.Con); ok &&
				len(rc.Args) == len(wc.Args) {
				for i := range rc.Args {
					m, ok := types.Chase(rc.Args[i]).(*types.Meta)
					if ok && isGoAny(types.Chase(wc.Args[i])) {
						c.bind(m, wc.Args[i], p.Sp)
					}
				}
			}
		}
	}
	out.Rest = p.Rest

	if len(p.Fields) != 0 {
		for _, f := range p.Fields {
			fname := ""
			if f.Name != nil {
				fname = f.Name.Name
			}
			fi := c.fieldByName(vi.fields, fname)
			if fi == nil {
				c.diags.Add(diag.Error("E0401",
					fmt.Sprintf("`%s` has no field named `%s`", vi.sym.Name, fname)).
					At(f.Sp).Set("ctor", vi.sym.Name).Set("field", fname))
				out.Fields = append(out.Fields, c.fieldPatOf(f, c.quietMeta(f.Sp, "the field's type"), nil, -1))
				continue
			}
			ft := params[fi.index].Type
			out.Fields = append(out.Fields, c.fieldPatOf(f, ft, fi.sym, fi.index))
		}
		return out
	}

	// Positional and `~label:` arguments. Positional args fill the
	// unlabeled payload slots in order; labeled args match by name.
	posIdx := 0
	used := make([]bool, len(vi.fields))
	for _, a := range p.Args {
		var label string
		var fi *fieldInfo
		if a.Label != nil {
			label = a.Label.Name
			fi = c.fieldByLabel(vi.fields, label)
			if fi == nil {
				c.diags.Add(diag.Error("E0406",
					fmt.Sprintf("`%s` has no parameter labeled `~%s`", vi.sym.Name, label)).
					At(a.Sp).Set("label", label).Set("callee", vi.sym.Name))
			} else if used[fi.index] {
				c.diags.Add(diag.Error("E0405",
					fmt.Sprintf("the label `~%s` is supplied twice", label)).
					At(a.Sp).Set("label", label))
				fi = nil
			}
		} else {
			for posIdx < len(vi.fields) && (used[posIdx] || vi.fields[posIdx].label != "") {
				posIdx++
			}
			if posIdx < len(vi.fields) {
				fi = vi.fields[posIdx]
				posIdx++
			} else {
				// More arguments than payload slots.
				c.mismatch(a.Sp, fmt.Sprintf("at most %d argument(s)", len(vi.fields)), "another argument")
			}
		}
		var pt types.Type
		if fi != nil {
			used[fi.index] = true
			pt = params[fi.index].Type
		} else {
			// The argument's own diagnostic (E0405/E0406, or the
			// too-many mismatch) already ran.
			pt = c.quietMeta(a.Sp, "the constructor argument's type")
		}
		out.Args = append(out.Args, CtorArg{Label: label, Pat: c.checkPat(a.Pat, pt)})
	}
	return out
}

// checkRecordPat types a `{ name = pat, .. }` record pattern. The
// expected type must name a record declaration; each entry resolves
// its field by name.
func (c *checker) checkRecordPat(p *syntax.RecordPat, want types.Type) Pat {
	out := &RecordPat{Base: baseOf(p.Sp, want), Rest: p.Rest}
	ti, sub := c.recordOf(want, p.Sp)
	for _, f := range p.Fields {
		var fi *fieldInfo
		var ft types.Type
		fname := ""
		if f.Name != nil {
			fname = f.Name.Name
		}
		if ti != nil {
			fi = c.fieldByName(ti.fields, fname)
			if fi == nil {
				c.diags.Add(diag.Error("E0401",
					fmt.Sprintf("record `%s` has no field named `%s`", ti.con.Name, fname)).
					At(f.Sp).Set("type", ti.con.Name).Set("field", fname))
			} else {
				if sub != nil {
					ft = sub.Apply(fi.typ)
				} else {
					ft = fi.typ
				}
			}
		}
		if ft == nil {
			// No record resolved this name — the inner pattern
			// still needs a type. Bind it to Unit so the recovery
			// does not dangle an unsolved variable on top of the
			// E0401 already reported.
			ft = types.Unit
		}
		var fsym *resolve.Symbol
		idx := -1
		if fi != nil {
			fsym = fi.sym
			idx = fi.index
		}
		out.Fields = append(out.Fields, c.fieldPatOf(f, ft, fsym, idx))
	}
	return out
}

// fieldPatOf types one `name = pat` (or punned `name`) field entry.
// Punned fields expand to `name = <var>` in the typed form.
func (c *checker) fieldPatOf(f syntax.FieldPat, ft types.Type, fsym *resolve.Symbol, idx int) FieldPat {
	name := ""
	var inner Pat
	if f.Name != nil {
		name = f.Name.Name
	}
	if f.Punned() {
		// `{ name }` binds a variable of the same name.
		sym := c.declSym(f.Name, resolve.KindVar)
		c.bindSym(sym, ft)
		sp := f.Sp
		if f.Name != nil {
			sp = f.Name.Sp
		}
		inner = &VarPat{Base: baseOf(sp, ft), Sym: sym}
	} else {
		inner = c.checkPat(f.Pat, ft)
	}
	return FieldPat{Sp: f.Sp, Name: name, Sym: fsym, Index: idx, Pat: inner}
}

// ctorOf resolves a constructor expression (a bare `Name` or a
// `pkg.Name` select) to its variantInfo.
func (c *checker) ctorOf(e syntax.Expr) (*variantInfo, *syntax.Ident) {
	var sym *resolve.Symbol
	var name *syntax.Ident
	switch e := e.(type) {
	case nil:
		// A missing constructor name in hand-built input.
		return nil, nil
	case *syntax.Ident:
		name = e
		sym = c.res.UseOf(e)
	case *syntax.Select:
		name = e.Sel
		sym = c.res.UseOf(e.Sel)
	case *syntax.Lit, *syntax.StrInterp, *syntax.Apply, *syntax.Unary,
		*syntax.Binary, *syntax.Hole, *syntax.Lambda, *syntax.If,
		*syntax.Match, *syntax.Block, *syntax.Let, *syntax.Ignore,
		*syntax.Try, *syntax.GoExpr, *syntax.For, *syntax.SelectExpr,
		*syntax.ListLit, *syntax.Spread, *syntax.TupleLit,
		*syntax.Ascribe, *syntax.RecordLit:
		// A constructor name is always an Ident or a Select.
		panic("unreachable")
	default:
		panic("unreachable")
	}
	if sym == nil {
		return nil, name
	}
	if vi, ok := c.ctors[sym]; ok {
		return vi, name
	}
	// A builtin-scope constructor (Some, None, Ok, Err) that is not
	// declared in this file.
	sp := diag.Span{}
	if name != nil {
		sp = name.Sp
	}
	if sig, ok := c.sigOf(sym, sp); ok {
		return c.builtinVariant(sym, sig), name
	}
	return nil, name
}

// builtinVariant attaches a builtin-scope constructor's symbol to the
// variant entry registerBuiltinTypes prepared under its parent type —
// keeping the parent's variant list complete for P1-6. For a ctor the
// checker does not know (an imported one), it fabricates a detached
// entry from the symbol's signature.
func (c *checker) builtinVariant(sym *resolve.Symbol, sig Signature) *variantInfo {
	if vi, ok := c.ctors[sym]; ok {
		return vi
	}
	// The constructor's parent is the signature's result type.
	con, _ := types.Chase(sig.Type).(*types.Con)
	if fn, ok := types.Chase(sig.Type).(*types.Func); ok {
		con, _ = types.Chase(fn.Ret).(*types.Con)
	}
	if con != nil {
		if ti, ok := c.tyCons[conKey{qual: con.Qual, name: con.Name}]; ok && ti.kind == kindSum {
			for _, vi := range ti.variants {
				if vi.name == sym.Name || (vi.name == "" && len(ti.variants) == 1) {
					vi.sym = sym
					c.ctors[sym] = vi
					return vi
				}
			}
		}
	}
	// Unknown/imported ctor: fabricate an entry from the signature.
	vi := &variantInfo{sym: sym, name: sym.Name, sp: sym.DefSpan()}
	if fn, ok := types.Chase(sig.Type).(*types.Func); ok {
		fields := make([]*fieldInfo, len(fn.Params))
		for i, p := range fn.Params {
			fields[i] = &fieldInfo{label: p.Label, index: i, typ: p.Type}
		}
		vi.fields = fields
	}
	vi.parent = &typeInfo{con: con}
	c.ctors[sym] = vi
	return vi
}

// ctorParams instantiates a variant's signature at the use site sp:
// it returns the payload parameter list, the constructed (parent)
// type, and the instantiation's type arguments — the metas a failed
// subject unify may need to silence.
func (c *checker) ctorParams(vi *variantInfo, sp diag.Span) ([]types.FuncParam, types.Type, []types.Type) {
	var sig types.Type
	if s, ok := c.sigOf(vi.sym, sp); ok {
		sig = s.Type
	} else if vi.parent != nil && len(vi.fields) > 0 {
		// A builtin-variant entry whose signature never reached the
		// symbol table: rebuild it from the recorded field types.
		params := make([]types.FuncParam, len(vi.fields))
		for i, f := range vi.fields {
			params[i] = types.FuncParam{Label: f.label, Type: f.typ}
		}
		sig = types.Fn(vi.parent.con, params...)
	} else if vi.parent != nil {
		sig = vi.parent.con
	}
	t, targs := c.instantiate(sig, sp)
	params, ret := flattenFunc(t)
	return params, ret, targs
}

// fieldByName finds a payload slot by pattern name.
func (c *checker) fieldByName(fields []*fieldInfo, name string) *fieldInfo {
	for _, f := range fields {
		if f.name == name {
			return f
		}
	}
	return nil
}

// fieldByLabel finds a payload slot by call-site label.
func (c *checker) fieldByLabel(fields []*fieldInfo, label string) *fieldInfo {
	for _, f := range fields {
		if f.label == label {
			return f
		}
	}
	return nil
}

// recordOf expects want to be a declared record type and returns its
// info together with a substitution instantiating its parameters.
// A concrete non-record reports a mismatch; a meta is bound to a
// record constructor only when the type is already known — otherwise
// the meta stays unsolved and the caller's field types stay metas.
func (c *checker) recordOf(want types.Type, sp diag.Span) (*typeInfo, types.Subst) {
	con, ok := types.Chase(want).(*types.Con)
	if !ok {
		if _, isMeta := types.Chase(want).(*types.Meta); isMeta {
			// The expected type is unknown: fields stay metas and
			// a later binding may still name a record.
			return nil, nil
		}
		c.mismatch(sp, "a record type", want)
		return nil, nil
	}
	ti, ok := c.tyCons[conKey{qual: con.Qual, name: con.Name}]
	if !ok || ti == nil || ti.kind != kindRecord {
		c.mismatch(sp, "a record type", want)
		return nil, nil
	}
	sub := types.Subst{}
	for i, v := range ti.params {
		if i < len(con.Args) {
			sub[v.ID] = con.Args[i]
		}
	}
	return ti, sub
}

// tupleElems returns the element types want must have as an n-tuple,
// extending a meta into a fresh tuple type.
func (c *checker) tupleElems(want types.Type, n int, sp diag.Span) []types.Type {
	switch t := types.Chase(want).(type) {
	case *types.Tuple:
		if len(t.Elems) != n {
			c.mismatch(sp, fmt.Sprintf("a %d-tuple", n), want)
			ms := make([]types.Type, n)
			for i := range ms {
				ms[i] = c.quietMeta(sp, "a tuple element")
			}
			return ms
		}
		return t.Elems
	case *types.Meta:
		ms := make([]types.Type, n)
		elems := make([]types.Type, n)
		for i := range ms {
			ms[i] = c.newMeta(sp, "a tuple element")
			elems[i] = ms[i]
		}
		if !c.bind(t, types.Tup(elems...), sp) {
			for i := range ms {
				c.markQuiet(ms[i])
			}
		}
		return ms
	case *types.Var, *types.Con, *types.Func, *types.Go:
		c.mismatch(sp, fmt.Sprintf("a %d-tuple", n), want)
		ms := make([]types.Type, n)
		for i := range ms {
			ms[i] = c.quietMeta(sp, "a tuple element")
		}
		return ms
	default:
		panic("unreachable")
	}
}

// listElem returns the element type want must have as `List _`,
// extending a meta into a fresh list type. A file-local `type List`
// is not the builtin: a `[x, ..rest]` pattern against it is E0415.
func (c *checker) listElem(want types.Type, sp diag.Span) types.Type {
	switch t := types.Chase(want).(type) {
	case *types.Con:
		if t.Qual == "" && t.Name == "List" && len(t.Args) == 1 {
			if c.shadowedBuiltin("List") {
				c.shadowedErr(sp, "a list pattern", "List")
			} else {
				return t.Args[0]
			}
		} else {
			c.mismatch(sp, "List", want)
		}
		return c.quietMeta(sp, "the element type")
	case *types.Meta:
		elem := c.newMeta(sp, "the element type")
		if !c.bind(t, types.List(elem), sp) {
			c.markQuiet(elem)
		}
		return elem
	case *types.Var, *types.Func, *types.Tuple, *types.Go:
		c.mismatch(sp, "List", want)
		return c.quietMeta(sp, "the element type")
	default:
		panic("unreachable")
	}
}

// resultArgs decomposes want as `Result a e` — used by `try` and
// `|?>`. An unbound meta becomes Result of fresh metas. A file-local
// `type Result` is not the builtin — the bound meta below spells the
// local ADT, so callers reject it (E0415) via isShadowedCon. This
// helper itself never reports: every caller names its own construct
// in the complaint, and a second report at the same span would only
// duplicate it.
func (c *checker) resultArgs(want types.Type, sp diag.Span) (val, err types.Type, ok bool) {
	switch t := types.Chase(want).(type) {
	case *types.Con:
		if t.Qual == "" && t.Name == "Result" && len(t.Args) == 2 &&
			!c.shadowedBuiltin("Result") {
			return t.Args[0], t.Args[1], true
		}
		return nil, nil, false
	case *types.Meta:
		a := c.newMeta(sp, "the Result's value type")
		e := c.newMeta(sp, "the Result's error type")
		// Under a shadowed `Result` the bind below poisons the
		// operand for good — quiet it before binding so the metas
		// minted inside `Result ?a ?e` do not cascade into E0407s
		// on top of the E0415 the caller reports.
		if c.shadowedBuiltin("Result") {
			c.markQuiet(t)
		}
		if !c.bind(t, types.Result(a, e), sp) {
			// The constraint on t rejected Result and already
			// reported (e.g. `try 1`, whose numeric literal meta
			// can never be a Result). Bind the fresh metas so they
			// do not dangle into E0407s, and report failure.
			c.bind(a, types.Unit, sp)
			c.bind(e, types.Unit, sp)
			return nil, nil, false
		}
		// The bound meta now spells `Result` — which a file-local
		// `type Result` declares: the `try`/`|?>` would unwrap the
		// local ADT through the builtin's tag machinery (E0415,
		// reported by the caller).
		if c.shadowedBuiltin("Result") {
			return nil, nil, false
		}
		return a, e, true
	case *types.Var, *types.Func, *types.Tuple, *types.Go:
		return nil, nil, false
	default:
		panic("unreachable")
	}
}

// chanElem decomposes want as `Chan a`, `RecvChan a` or `SendChan a`,
// reporting the direction actually found. A file-local `type Chan`
// (or `RecvChan`/`SendChan`) is not a builtin channel — select and
// `for` cannot receive from it.
func (c *checker) chanElem(t types.Type) (elem types.Type, dir string, ok bool) {
	con, ok := types.Chase(t).(*types.Con)
	if !ok || con.Qual != "" || len(con.Args) != 1 || c.shadowedBuiltin(con.Name) {
		return nil, "", false
	}
	switch con.Name {
	case "Chan":
		return con.Args[0], "both", true
	case "RecvChan":
		return con.Args[0], "recv", true
	case "SendChan":
		return con.Args[0], "send", true
	default:
		return nil, "", false
	}
}

// litType gives the type of a literal: concrete for strings, chars,
// booleans and unit; a constrained meta for numbers — Int literals
// may be any numeric type and default to Int, Float literals to Float.
func (c *checker) litType(l *syntax.Lit) types.Type {
	switch l.Kind {
	case syntax.LitInt:
		m := c.newMeta(l.Sp, "an integer literal")
		c.constrain(m, l.Sp, "a numeric type", func(r *metaRec) {
			r.num = true
			r.ord = true
			r.eq = true
			r.dflt = types.Int
		})
		return m
	case syntax.LitFloat:
		m := c.newMeta(l.Sp, "a float literal")
		c.constrain(m, l.Sp, "a floating-point type", func(r *metaRec) {
			r.flo = true
			r.ord = true
			r.eq = true
			r.dflt = types.Float
		})
		return m
	case syntax.LitStr:
		// A literal produces the builtin — a file-local `type
		// String` shadows that reading (E0415).
		if c.shadowedBuiltin("String") {
			c.shadowedErr(l.Sp, "a string literal", "String")
		}
		return types.String
	case syntax.LitChar:
		if c.shadowedBuiltin("Char") {
			c.shadowedErr(l.Sp, "a char literal", "Char")
		}
		return types.Char
	case syntax.LitBool:
		if c.shadowedBuiltin("Bool") {
			c.shadowedErr(l.Sp, "a boolean literal", "Bool")
		}
		return types.Bool
	case syntax.LitUnit:
		if c.shadowedBuiltin("Unit") {
			c.shadowedErr(l.Sp, "the unit literal `()`", "Unit")
		}
		return types.Unit
	default:
		panic("unreachable")
	}
}
