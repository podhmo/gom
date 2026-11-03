package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// conKey is the nominal identity of a declared or builtin type
// constructor: (declaring package's import path, name) — the same pair
// types.Con stores in Qual and Name.
type conKey struct {
	qual string
	name string
}

// typeInfo is the checker's record of one type declaration (or of a
// builtin-scope type like Option and Result whose declaration lives in
// the standard library the resolver does not load).
type typeInfo struct {
	sym    *resolve.Symbol // the declared type (KindType); nil for builtins
	con    *types.Con      // the constructor applied to its own parameters
	params []*types.Var    // declared parameters in source order
	scope  *tscope         // the declaration's rigid-variable scope
	sp     diag.Span

	// Exactly one of these describes the body; kind picks which.
	kind     typeKind
	variants []*variantInfo // kindSum
	fields   []*fieldInfo   // kindRecord
	alias    types.Type     // kindAlias — the aliased type with params unsubstituted
	aliasRes bool           // the alias body has been resolved
	aliasIn  bool           // alias resolution in progress (cycle guard)
}

type typeKind int

const (
	kindSum typeKind = iota + 1
	kindRecord
	kindAlias
	kindAbstract
)

// fieldInfo is one payload slot of a variant or one record field.
// name is what patterns and selections see ("" when positional);
// label is what call sites must write with `~` ("" when positional).
type fieldInfo struct {
	name  string
	label string
	index int
	sym   *resolve.Symbol // KindField; nil for unnamed positional payload
	typ   types.Type      // may contain the declaring type's param vars
	sp    diag.Span
}

// variantInfo is one constructor of a sum type: its symbol, its
// payload fields, and its parent type. name/index mirror the
// declaration so the pattern checker can enumerate a type's
// constructors even for builtin types whose symbols arrive lazily.
type variantInfo struct {
	sym    *resolve.Symbol // KindCtor; nil until a builtin ctor is seen
	name   string
	index  int
	parent *typeInfo
	fields []*fieldInfo
	sp     diag.Span
}

// tscope is the rigid-variable scope of one type-level context: a type
// declaration's parameter list, or the implicit quantification of a
// signature. Type variables are looked up by symbol when the resolver
// bound them (KindTypeVar) and by name otherwise — signatures like
// `fn map (f: a -> b) ...` bind their variables implicitly.
//
// A scope opened while a body is being checked chains to the
// enclosing signature's scope (outer): `fn f (xs: List a) -> List a =
// { let y : List a = xs; y }` resolves the annotation's `a` to the
// function's own variable rather than minting a fresh one. owned
// records the vars the scope itself minted — unboundVars reports only
// those, since an inherited var is bound by the enclosing decl.
type tscope struct {
	bySym  map[*resolve.Symbol]*types.Var
	byName map[string]*types.Var
	owned  map[*types.Var]bool
	outer  *tscope
	c      *checker
}

func (c *checker) newTScope() *tscope {
	return &tscope{
		bySym:  map[*resolve.Symbol]*types.Var{},
		byName: map[string]*types.Var{},
		owned:  map[*types.Var]bool{},
		outer:  c.curTScope,
		c:      c,
	}
}

// bind registers a resolver-provided type variable (a `type` decl's
// parameter) so TypeVar nodes resolve to it.
func (s *tscope) bind(sym *resolve.Symbol, v *types.Var) {
	s.bySym[sym] = v
	s.owned[v] = true
}

// find returns the variable name already denotes in this scope or an
// enclosing one — nil when unbound (unlike named it never mints).
func (s *tscope) find(name string) *types.Var {
	if v, ok := s.byName[name]; ok {
		return v
	}
	if s.outer != nil {
		return s.outer.find(name)
	}
	return nil
}

// named returns the variable for a type parameter declared by name:
// an enclosing scope's variable when one exists, otherwise a fresh
// one owned by this scope.
func (s *tscope) named(name string) *types.Var {
	if v := s.find(name); v != nil {
		return v
	}
	v := &types.Var{ID: s.c.nextVar, Name: name}
	s.c.nextVar++
	s.byName[name] = v
	s.owned[v] = true
	return v
}

// lookup resolves a *syntax.TypeVar occurrence: first the symbol the
// resolver bound, then the implicit same-name variable of the scope.
func (s *tscope) lookup(tv *syntax.TypeVar) *types.Var {
	if sym := s.c.res.UseOf(tv.Name); sym != nil {
		if v, ok := s.bySym[sym]; ok {
			return v
		}
	}
	return s.named(tv.Name.Name)
}

// registerBuiltinTypes pre-populates the type-constructor table with
// the compiler-owned ADTs Option and Result, whose variants are fixed
// (Some/None, Ok/Err) even though their constructor symbols are not
// known until the resolver provides them. The variant entries carry
// field shapes but no symbols; ctorOf/builtinVariant attach the
// resolved symbol the first time a constructor is seen. The pattern
// checker (P1-6) enumerates variants by name for exhaustiveness.
func (c *checker) registerBuiltinTypes(v *builtinTVar) {
	a, e := v.named("a"), v.named("e")
	option := &typeInfo{
		params: []*types.Var{a},
		con:    &types.Con{Name: "Option", Args: []types.Type{a}},
		kind:   kindSum,
		variants: []*variantInfo{
			{name: "Some", index: 0, fields: []*fieldInfo{{index: 0, typ: a}}},
			{name: "None", index: 1},
		},
	}
	for _, vi := range option.variants {
		vi.parent = option
	}
	result := &typeInfo{
		params: []*types.Var{a, e},
		con:    &types.Con{Name: "Result", Args: []types.Type{a, e}},
		kind:   kindSum,
		variants: []*variantInfo{
			{name: "Ok", index: 0, fields: []*fieldInfo{{index: 0, typ: a}}},
			{name: "Err", index: 1, fields: []*fieldInfo{{index: 0, typ: e}}},
		},
	}
	for _, vi := range result.variants {
		vi.parent = result
	}
	c.tyCons[conKey{"", "Option"}] = option
	c.tyCons[conKey{"", "Result"}] = result
}

// registerTypeDecls performs the first type-declaration pass: every
// `type` name is registered with its parameter list so bodies may be
// recursive. Bodies are resolved by a second pass (resolveTypeBodies).
func (c *checker) registerTypeDecls(decls []syntax.Decl) {
	for _, d := range decls {
		td, ok := d.(*syntax.TypeDecl)
		if !ok {
			continue
		}
		sym := c.res.DefOf(td.Name)
		if sym == nil {
			sym = &resolve.Symbol{Name: td.Name.Name, Kind: resolve.KindType, Def: td.Name}
		}
		ti := &typeInfo{sym: sym, sp: td.Sp}
		scope := c.newTScope()
		params := make([]*types.Var, len(td.Params))
		for i, p := range td.Params {
			v := scope.named(p.Name)
			if psym := c.res.DefOf(p); psym != nil {
				scope.bind(psym, v)
			}
			params[i] = v
		}
		ti.params = params
		args := make([]types.Type, len(params))
		for i, v := range params {
			args[i] = v
		}
		ti.con = &types.Con{Qual: sym.Package, Name: sym.Name, Args: args}
		ti.scope = scope
		c.tyCons[conKey{qual: sym.Package, name: sym.Name}] = ti
		c.tyBySym[sym] = ti
	}
}

// typeDeclInfo returns the typeInfo registered for a `type` decl —
// looked up by symbol, then by name for inputs whose Defs table is
// incomplete (the registration pass always creates one).
func (c *checker) typeDeclInfo(td *syntax.TypeDecl) *typeInfo {
	if sym := c.res.DefOf(td.Name); sym != nil {
		if ti, ok := c.tyBySym[sym]; ok {
			return ti
		}
	}
	return c.tyCons[conKey{qual: "", name: td.Name.Name}]
}

// resolveTypeBodies performs the second pass over the type
// declarations, resolving bodies now that every constructor name is
// registered. It appends the typed *TypeDecl to c.decls.
func (c *checker) resolveTypeBodies(decls []syntax.Decl) {
	for _, d := range decls {
		td, ok := d.(*syntax.TypeDecl)
		if !ok {
			continue
		}
		ti := c.typeDeclInfo(td)
		if ti == nil {
			// registerTypeDecls always records every `type` decl;
			// this only fires for inputs that skipped it.
			ti = &typeInfo{
				sym:   c.declSym(td.Name, resolve.KindType),
				con:   &types.Con{Name: td.Name.Name},
				scope: c.newTScope(),
			}
		}
		var body TypeBody
		switch b := td.Body.(type) {
		case *syntax.SumBody:
			variants := make([]Variant, len(b.Variants))
			vs := make([]*variantInfo, len(b.Variants))
			if ti != nil {
				ti.kind = kindSum
			}
			for i, sv := range b.Variants {
				sv := sv
				vs[i] = c.variantInfoOf(&sv, i, ti)
				variants[i] = c.variantOf(vs[i])
			}
			if ti != nil {
				ti.variants = vs
			}
			body = &SumType{Sp: b.Sp, Variants: variants}
		case *syntax.RecordBody:
			tf := make([]TField, len(b.Fields))
			fs := make([]*fieldInfo, len(b.Fields))
			for i, f := range b.Fields {
				fs[i] = c.fieldInfoOf(f, i, ti)
				tf[i] = c.tfieldOf(fs[i])
			}
			if ti != nil {
				ti.kind = kindRecord
				ti.fields = fs
				c.records = append(c.records, ti)
			}
			body = &RecordType{Sp: b.Sp, Fields: tf}
		case *syntax.AliasBody:
			if ti != nil {
				ti.kind = kindAlias
				// The alias body is resolved lazily on first
				// reference — mutually recursive uses resolve
				// through the cycle guard in resolveAlias.
				c.resolveAlias(ti, b.Type)
			}
			body = &AliasType{Sp: b.Sp, Type: ti.alias}
		case *syntax.AbstractBody:
			if ti != nil {
				ti.kind = kindAbstract
			}
			body = &AbstractType{Sp: b.Sp}
		case nil:
			if ti != nil {
				ti.kind = kindAbstract
			}
			body = &AbstractType{Sp: td.Sp}
		default:
			panic("unreachable")
		}
		c.decls = append(c.decls, &TypeDecl{
			Sp:         td.Sp,
			Sym:        ti.sym,
			TypeParams: ti.params,
			Body:       body,
		})
	}
}

// variantInfoOf resolves one variant's payload fields.
func (c *checker) variantInfoOf(sv *syntax.Variant, index int, parent *typeInfo) *variantInfo {
	vi := &variantInfo{sp: sv.Sp, parent: parent, index: index}
	vi.sym = c.declSym(sv.Name, resolve.KindCtor)
	vi.name = sv.Name.Name
	fields := make([]*fieldInfo, len(sv.Params))
	for i, f := range sv.Params {
		fields[i] = c.fieldInfoOf(f, i, parent)
	}
	vi.fields = fields
	c.ctors[vi.sym] = vi
	// The constructor's type: `fields... -> Parent` — Unit-free; a
	// parameterless variant is a value of the parent type, not a
	// function.
	var sig types.Type = parent.con
	if len(fields) > 0 {
		params := make([]types.FuncParam, len(fields))
		for i, f := range fields {
			params[i] = types.FuncParam{Label: f.label, Type: f.typ}
		}
		sig = types.Fn(parent.con, params...)
	}
	c.vals[vi.sym] = sig
	return vi
}

// variantOf produces the typed Variant of a resolved variantInfo.
func (c *checker) variantOf(vi *variantInfo) Variant {
	fields := make([]TField, len(vi.fields))
	for i, fi := range vi.fields {
		fields[i] = c.tfieldOf(fi)
	}
	return Variant{Sp: vi.sp, Sym: vi.sym, Fields: fields}
}

// fieldInfoOf resolves one payload slot / record field.
func (c *checker) fieldInfoOf(f syntax.Field, index int, parent *typeInfo) *fieldInfo {
	fi := &fieldInfo{index: index, sp: f.Sp}
	switch {
	case f.Label != nil:
		fi.name = f.Label.Name
		fi.label = f.Label.Name
		fi.sym = c.defSymAny(f.Label, f.Name, resolve.KindField)
	case f.Name != nil:
		fi.name = f.Name.Name
		fi.sym = c.declSym(f.Name, resolve.KindField)
	}
	if f.Type != nil && parent != nil {
		fi.typ = c.typeExpr(f.Type, parent.scope)
	} else if f.Type != nil {
		fi.typ = c.typeExpr(f.Type, c.newTScope())
	} else {
		fi.typ = c.newMeta(f.Sp, fmt.Sprintf("the type of field `%s`", fi.name))
	}
	if fi.sym != nil {
		c.fieldSyms[fi.sym] = fi
	}
	return fi
}

// tfieldOf produces the typed TField of a resolved fieldInfo.
func (c *checker) tfieldOf(fi *fieldInfo) TField {
	return TField{
		Sp:    fi.sp,
		Name:  fi.name,
		Label: fi.label,
		Index: fi.index,
		Sym:   fi.sym,
		Type:  fi.typ,
	}
}

// resolveAlias resolves an alias body, guarding against cycles.
func (c *checker) resolveAlias(ti *typeInfo, body syntax.TypeExpr) {
	if ti.aliasRes {
		return
	}
	if ti.aliasIn {
		d := diag.Error("E0401",
			fmt.Sprintf("cyclic type alias `%s`", ti.sym.Name)).At(ti.sp)
		c.diags.Add(d)
		ti.alias = c.newMeta(ti.sp, "the cyclic alias")
		ti.aliasRes = true
		return
	}
	ti.aliasIn = true
	ti.alias = c.typeExpr(body, ti.scope)
	ti.aliasIn = false
	ti.aliasRes = true
}

// typeCtor is what a TypeName resolves to: either a nominal
// constructor to apply, or an alias to expand.
type typeCtor struct {
	con    *types.Con // applied to the decl's own param vars; nil for aliases
	alias  *typeInfo  // non-nil for alias references
	exact  types.Type // non-nil for imported names whose signature is the type itself (Go named types)
	arity  int        // required argument count; -1 = unknown (imported opaque)
	params []*types.Var
}

// builtinTypeArity is the arity of the builtin-scope type constructors
// (spec section 3.1 and 8.3). `Go.Slice` and `Go.Map` are not here —
// they are reached through the reserved `Go` qualifier.
var builtinTypeArity = map[string]int{
	"Unit": 0, "Bool": 0, "Int": 0, "Int32": 0, "Int64": 0,
	"Float": 0, "Char": 0, "String": 0, "GoError": 0,
	"List": 1, "Option": 1, "Ref": 1, "Array": 1, "Ptr": 1,
	"Chan": 1, "RecvChan": 1, "SendChan": 1, "Task": 1,
	"Result": 2,
}

// goTypeArity is the arity of the reserved-qualifier Go types.
var goTypeArity = map[string]int{
	"Slice": 1,
	"Map":   2,
}

// typeExpr resolves a surface TypeExpr to a types.Type inside the
// rigid-variable scope sc.
func (c *checker) typeExpr(te syntax.TypeExpr, sc *tscope) types.Type {
	switch te := te.(type) {
	case *syntax.TypeVar:
		return sc.lookup(te)
	case *syntax.TypeName:
		ctor := c.typeCtorOf(te)
		t, _ := c.applyTypeCtor(ctor, nil, te.Sp)
		return t
	case *syntax.TypeApp:
		ctor := c.typeCtorOf(te.Ctor)
		args := make([]types.Type, len(te.Args))
		for i, a := range te.Args {
			args[i] = c.typeExpr(a, sc)
		}
		t, _ := c.applyTypeCtor(ctor, args, te.Sp)
		return t
	case *syntax.TypeFn:
		params := make([]types.FuncParam, len(te.Params))
		for i, p := range te.Params {
			label := ""
			if p.Label != nil {
				label = p.Label.Name
			}
			params[i] = types.FuncParam{Label: label, Type: c.typeExpr(p.Type, sc)}
		}
		return types.Fn(c.typeExpr(te.Ret, sc), params...)
	case *syntax.TypeTuple:
		elems := make([]types.Type, len(te.Elems))
		for i, e := range te.Elems {
			elems[i] = c.typeExpr(e, sc)
		}
		return &types.Tuple{Elems: elems}
	case *syntax.TypeRecord:
		// An anonymous record type has no types.Type form — types are
		// nominal (spec section 3). The grammar admits the shape; the
		// checker asks for a named declaration instead.
		d := diag.Error("E0401",
			"anonymous record types are not supported: declare a named record type").
			At(te.Sp)
		c.diags.Add(d)
		return c.newMeta(te.Sp, "an anonymous record type")
	default:
		panic("unreachable")
	}
}

// typeCtorOf resolves a TypeName (or the head of a TypeApp) to a
// typeCtor. Non-name heads (TypeFn inside a TypeApp, ...) produce a
// mismatch diagnostic and a recovery meta.
func (c *checker) typeCtorOf(te syntax.TypeExpr) typeCtor {
	tn, ok := te.(*syntax.TypeName)
	if !ok {
		d := diag.Error("E0401", "the head of a type application must be a type name").At(te.Span())
		c.diags.Add(d)
		return typeCtor{arity: -1}
	}
	if tn.Qual != nil {
		// The reserved `Go` namespace covers Go.Slice / Go.Map.
		if tn.Qual.Name == "Go" {
			if arity, ok := goTypeArity[tn.Name.Name]; ok {
				params := make([]*types.Var, arity)
				args := make([]types.Type, arity)
				for i := range params {
					params[i] = &types.Var{ID: c.nextVar, Name: fmt.Sprintf("g%d", i)}
					c.nextVar++
					args[i] = params[i]
				}
				return typeCtor{
					con:    &types.Con{Qual: "Go", Name: tn.Name.Name, Args: args},
					arity:  arity,
					params: params,
				}
			}
		}
		// A package-qualified type name: the member sym's signature
		// supplies its constructor (arity included) when the source
		// knows it; otherwise it is an opaque imported constructor.
		sym := c.res.UseOf(tn.Name)
		qual := ""
		if qsym := c.res.UseOf(tn.Qual); qsym != nil {
			qual = qsym.Package
		}
		if sym != nil {
			if c.hiddenDiag(sym, tn.Sp) {
				return typeCtor{arity: -1}
			}
			if sig, ok := c.sigOf(sym, tn.Sp); ok {
				return c.symTypeCtor(sym, sig, tn.Sp)
			}
			return typeCtor{con: &types.Con{Qual: qual, Name: sym.Name}, arity: -1}
		}
		d := diag.Error("E0401",
			fmt.Sprintf("unknown type `%s.%s`", tn.Qual.Name, tn.Name.Name)).At(tn.Sp)
		c.diags.Add(d)
		return typeCtor{arity: -1}
	}

	// Unqualified: resolver-bound symbol first (local declarations
	// and shadowing), then the builtin table by name.
	if sym := c.res.UseOf(tn.Name); sym != nil {
		if c.hiddenDiag(sym, tn.Sp) {
			return typeCtor{arity: -1}
		}
		if ti, ok := c.tyBySym[sym]; ok {
			return c.ctorOfInfo(ti)
		}
		if sig, ok := c.sigOf(sym, tn.Sp); ok {
			return c.symTypeCtor(sym, sig, tn.Sp)
		}
	}
	if arity, ok := builtinTypeArity[tn.Name.Name]; ok {
		if ti, ok := c.tyCons[conKey{qual: "", name: tn.Name.Name}]; ok {
			return c.ctorOfInfo(ti)
		}
		params := make([]*types.Var, arity)
		args := make([]types.Type, arity)
		for i := range params {
			params[i] = &types.Var{ID: c.nextVar, Name: fmt.Sprintf("b%d", i)}
			c.nextVar++
			args[i] = params[i]
		}
		return typeCtor{
			con:    &types.Con{Name: tn.Name.Name, Args: args},
			arity:  arity,
			params: params,
		}
	}
	// An undeclared name the resolver did not report (hand-built
	// inputs or an upstream gap): a fresh meta keeps the check going.
	return typeCtor{
		con:   &types.Con{Name: tn.Name.Name},
		arity: -1,
	}
}

// symTypeCtor reads a signature-bearing symbol in type position.
// Only symbols that name a type may denote one: KindType (an
// imported or declared type) and KindCtor (a constructor name spells
// its parent ADT — `Some Int` is `Option Int`). Every other signature
// — a Go function or method, a Go var, a Gom function, a builtin
// value — is a term misused as a type. Reading such a signature as a
// constructor would blame the *function's* parameter count for the
// name's kind (`Ptr` resolved to a three-parameter Go func reported
// "Ptr takes 3 type argument(s)" — P3-5).
func (c *checker) symTypeCtor(sym *resolve.Symbol, sig Signature, sp diag.Span) typeCtor {
	switch sym.Kind {
	case resolve.KindType, resolve.KindCtor:
	default:
		c.notAType(sp, sym)
		return typeCtor{arity: -1}
	}
	if con, ok := types.Chase(sig.Type).(*types.Con); ok {
		return typeCtor{con: con, arity: len(con.Args), params: varsOf(con.Args)}
	}
	if sym.Kind == resolve.KindCtor {
		if fn, ok := types.Chase(sig.Type).(*types.Func); ok {
			if con, ok := types.Chase(fn.Ret).(*types.Con); ok {
				// A constructor's signature returns the ADT — the
				// type its name spells is that ADT, whose arity is
				// the ADT's own parameter count, not the payload's
				// (`Ok` declares one payload but spells `Result`,
				// arity 2).
				return typeCtor{con: con, arity: len(con.Args), params: varsOf(con.Args)}
			}
		}
	}
	// The signature's type is the type the name denotes — a Go
	// named type, a tuple, an opaque abstract type.
	return typeCtor{exact: sig.Type, arity: -1}
}

// notAType reports a term-level symbol used where a type was
// expected — the type-position mirror of the resolver's "is a %s,
// not a value" (E0201).
func (c *checker) notAType(sp diag.Span, sym *resolve.Symbol) {
	c.diags.Add(diag.Error("E0201",
		fmt.Sprintf("`%s` is a %s, not a type", sym.Name, sym.Kind)).
		At(sp).
		Set("name", sym.Name).
		Set("namespace", "type").
		Set("resolved_kind", sym.Kind.String()))
}

// ctorOfInfo returns the typeCtor for a declared type.
func (c *checker) ctorOfInfo(ti *typeInfo) typeCtor {
	if ti.kind == kindAlias {
		return typeCtor{alias: ti, arity: len(ti.params), params: ti.params}
	}
	return typeCtor{con: ti.con, arity: len(ti.params), params: ti.params}
}

// applyTypeCtor applies a resolved constructor to type arguments,
// checking the arity and expanding aliases. Returns the type and a
// substitution for further use (ctor field types) when needed.
func (c *checker) applyTypeCtor(tc typeCtor, args []types.Type, sp diag.Span) (types.Type, types.Subst) {
	if tc.exact != nil {
		// The name denotes the signature type directly; any type
		// arguments instantiate its rigid variables in order.
		sub := types.Subst{}
		vars := rigidVars(tc.exact)
		for i, v := range vars {
			if i < len(args) {
				sub[v.ID] = args[i]
			} else {
				sub[v.ID] = c.newMeta(sp, fmt.Sprintf("the type argument for `%s`", v))
			}
		}
		return sub.Apply(tc.exact), nil
	}
	if tc.alias != nil {
		if tc.arity >= 0 && len(args) != tc.arity {
			name := "the type alias"
			if tc.alias.sym != nil {
				name = fmt.Sprintf("`%s`", tc.alias.sym.Name)
			}
			c.arityMismatch(sp, name, tc.arity, len(args))
		}
		sub := types.Subst{}
		for i, v := range tc.params {
			if i < len(args) {
				sub[v.ID] = args[i]
			}
		}
		var target types.Type
		if tc.alias.aliasRes {
			target = tc.alias.alias
		} else {
			target = c.newMeta(sp, fmt.Sprintf("the alias `%s`", tc.alias.sym.Name))
		}
		return sub.Apply(target), nil
	}
	if tc.con == nil {
		// Reached only after the unknown/hidden-type diagnostic —
		// the meta is recovery, not an inference question.
		return c.quietMeta(sp, "an unknown type"), nil
	}
	if tc.arity >= 0 && len(args) != tc.arity {
		c.arityMismatch(sp, tc.con.Name, tc.arity, len(args))
	}
	sub := types.Subst{}
	for i, v := range tc.params {
		if i < len(args) {
			sub[v.ID] = args[i]
		}
	}
	applied := sub.Apply(tc.con)
	return applied, sub
}

// arityMismatch reports a type constructor applied to the wrong number
// of arguments.
func (c *checker) arityMismatch(sp diag.Span, name string, want, got int) {
	if name == "" {
		name = "the type constructor"
	} else {
		name = fmt.Sprintf("`%s`", name)
	}
	d := diag.Error("E0401",
		fmt.Sprintf("type mismatch: %s takes %d type argument(s), got %d", name, want, got)).
		At(sp).Set("expected", fmt.Sprintf("%d", want)).Set("actual", fmt.Sprintf("%d", got))
	c.diags.Add(d)
}

// varsOf collects the *types.Var inside a type-argument list — used to
// name the parameters of an imported constructor's signature.
func varsOf(ts []types.Type) []*types.Var {
	var out []*types.Var
	for _, t := range ts {
		out = append(out, rigidVars(t)...)
	}
	return out
}

// recordCycleLimit bounds the number of distinct instantiated
// records one field walk may visit. An expansion that grows past the
// limit describes an infinitely large value — `{x: B (B A)}` style
// nesting peels one layer per step — so exceeding it reports the
// same E0413 an outright cycle does.
const recordCycleLimit = 256

// checkRecordCycles rejects records that contain themselves by
// value: the Go representation of a record is a struct whose fields
// are stored inline, so `type Point = {x: Point}` — or a mutual
// cycle `type A = {b: B}; type B = {a: A}` — would emit a struct
// that contains itself, which Go forbids as an invalid recursive
// type. The check runs after resolveTypeBodies so every local
// record's fields are known; each offending field reports E0413 at
// its own span.
func (c *checker) checkRecordCycles() {
	for _, ti := range c.records {
		for _, fi := range ti.fields {
			w := &recordCycle{origin: ti, c: c}
			if w.holds(fi.typ) {
				name := ""
				if ti.sym != nil {
					name = ti.sym.Name
				}
				d := diag.Error("E0413",
					fmt.Sprintf("invalid recursive type: `%s` contains itself by value through field `%s`", name, fi.name)).
					At(fi.sp).Set("type", name).Set("field", fi.name)
				if fi.typ != nil {
					d = d.Set("field_type", fi.typ.String())
				}
				c.diags.Add(d)
			}
		}
	}
}

// recordCycle walks a record field's type looking for a by-value
// path back to the record it is declared in. The walk stops at
// representations that box or reference their payload — sum types
// (sealed interfaces), function types, and the reference-shaped
// builtins (List, Ref, Ptr, Array, Go.Slice, Go.Map, the channels,
// scalars) — and descends through the ones that carry their payload
// inline: records themselves, tuples, the struct-shaped builtins
// Result and Task, and instantiated Go named types, whose layout is
// opaque and is therefore treated as by-value.
type recordCycle struct {
	origin *typeInfo
	c      *checker
	steps  int
	// seen memoizes the instantiated records (typeInfo + type
	// arguments) already fully expanded: re-entering one means the
	// expansion repeats without reaching the origin, so it cannot
	// complete a cycle. Linear scan is fine — record graphs are
	// small.
	seen []recordVisit
}

// recordVisit is one expanded instantiated record: the type
// declaration plus the arguments its parameters took on this path.
type recordVisit struct {
	ti   *typeInfo
	args []types.Type
}

// holds reports whether t contains the origin record somewhere
// inside a by-value expansion.
func (w *recordCycle) holds(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		return w.con(t)
	case *types.Tuple:
		return w.args(t.Elems)
	case *types.Func:
		// A function field stores a closure — the record appears
		// only inside the code pointer's environment, not inline.
		return false
	case *types.Go:
		// A Go named type instantiated with the record (`pkg.Box
		// A`) may store its arguments inside a struct; the layout
		// is opaque to Gom, so the walk is conservative.
		return w.args(t.Args)
	case *types.Var, *types.Meta:
		return false
	default:
		panic("unreachable")
	}
}

func (w *recordCycle) args(ts []types.Type) bool {
	for _, t := range ts {
		if w.holds(t) {
			return true
		}
	}
	return false
}

// con handles a constructor application: a local record expands its
// fields under the instantiation, Result and Task keep the walk
// going through their inline payloads, and every other constructor
// — boxed sum types, references, scalars, opaque imported types —
// ends it.
func (w *recordCycle) con(t *types.Con) bool {
	if ti, ok := w.c.tyCons[conKey{qual: t.Qual, name: t.Name}]; ok {
		switch ti.kind {
		case kindRecord:
			if ti == w.origin {
				return true
			}
			return w.fields(ti, t.Args)
		case kindSum:
			// Sum types emit as sealed interfaces; the builtin
			// Result is the exception — its representation is a
			// struct holding both payloads inline.
			if ti.sym == nil && t.Qual == "" && t.Name == "Result" {
				return w.args(t.Args)
			}
			return false
		case kindAlias:
			// Alias references expand at use sites, so no field
			// type arrives as an alias Con; recurse defensively in
			// case a hand-built input produces one.
			if ti.aliasRes {
				sub := types.Subst{}
				for i, p := range ti.params {
					if i < len(t.Args) {
						sub[p.ID] = t.Args[i]
					}
				}
				return w.holds(sub.Apply(ti.alias))
			}
			return false
		case kindAbstract:
			// An abstract type has no visible body — nothing it
			// could contain is knowable here.
			return false
		default:
			panic("unreachable")
		}
	}
	// Not a declared (or builtin ADT) type: Result and Task still
	// carry their arguments inside a struct; the remaining builtin
	// and Go-namespace constructors are reference-shaped or opaque.
	if t.Qual == "" {
		switch t.Name {
		case "Result", "Task":
			return w.args(t.Args)
		default:
			return false
		}
	}
	return false
}

// fields expands ti's field types under the instantiation args and
// keeps walking any of them that still lead toward the origin.
func (w *recordCycle) fields(ti *typeInfo, args []types.Type) bool {
	for _, v := range w.seen {
		if v.ti == ti && sameTypes(v.args, args) {
			return false
		}
	}
	w.seen = append(w.seen, recordVisit{ti: ti, args: args})
	w.steps++
	if w.steps > recordCycleLimit {
		return true
	}
	var sub types.Subst
	if len(args) > 0 {
		sub = types.Subst{}
		for i, p := range ti.params {
			if i < len(args) {
				sub[p.ID] = args[i]
			}
		}
	}
	for _, fi := range ti.fields {
		typ := fi.typ
		if sub != nil {
			typ = sub.Apply(typ)
		}
		if w.holds(typ) {
			return true
		}
	}
	return false
}

// sameTypes reports whether two type-argument lists are pairwise
// Equal.
func sameTypes(a, b []types.Type) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !types.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
