package core

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Violation is one broken Core IR invariant, reported by Validate.
// Violations are compiler-bug evidence — a malformed tree from
// internal/lower or from a hand-built test fixture — not user-facing
// diagnostics, so they are plain errors carrying the offending span
// rather than diag.Diagnostic values (the diagnostic code table has
// no internal-invariant category yet).
type Violation struct {
	// Span is the offending node's source span, when known.
	Span diag.Span
	// Node names the IR node type, e.g. "CallDirect".
	Node string
	// Msg is a one-line description of the broken invariant.
	Msg string
}

// Error implements error: "core: CallDirect: arity 3 but 2 args".
func (v Violation) Error() string {
	return "core: " + v.Node + ": " + v.Msg
}

// String renders the violation with its span for test output:
// "file.gom:10-20 core: CallDirect: ...".
func (v Violation) String() string {
	if v.Span.IsZero() {
		return v.Error()
	}
	return fmt.Sprintf("%s:%d-%d %s", v.Span.File, v.Span.Start, v.Span.End, v.Error())
}

// Validate checks a Core IR file against the invariants of
// docs/design.md section 2.1 — the contract internal/lower must
// satisfy and internal/codegen relies on:
//
//   - evaluation order: complex expressions appear only at Let.X and
//     Body.Tail — operands are atoms — and every Bind introduces a
//     fresh symbol used in scope (ANF shape is structural; scoping
//     and uniqueness are checked)
//   - call classification: CallDirect/CtorApp are saturated
//     (len(Args) == Arity), CallFunc applies a function value at
//     exactly one argument — the canonical one-func-per-arrow value
//     representation — CallGo arity/variadic/spread agree with
//     the callee, and no node re-classifies a known callee as an
//     unknown value
//   - interop conversions are explicit: non-plain Go calls appear
//     only inside their consumer nodes (ResultFromGo / OptionFromGo /
//     TupleFromGo / ErrorFromGo / EscapeCall) and Conv kinds agree
//     with the operand/result type shapes
//   - match is a decision tree: no patterns exist at this level,
//     SwitchType/SwitchTag defaults are DPanic arms for invalid
//     Go-side values, and sugared binary operators are absent
//   - escapes and self-tail calls point at the innermost enclosing
//     function boundary — escapes require the boundary's *residual*
//     result (Func.ResidualType — Sig.Ret for a full-arity function,
//     the remaining arrow for an arity-split one) to be Result _ e;
//     TailCall only at tail position; neither may cross a Spawn
//     (go{}) boundary
//   - every node carries a valid span and a non-nil type
//
// It returns the violations found; a nil or empty result means the
// file satisfies the mechanically checkable subset of the contract.
func Validate(f *File) []Violation {
	v := &validator{
		top:     make(map[*resolve.Symbol]bool),
		bound:   make(map[*resolve.Symbol]diag.Span),
		tparams: make(map[*resolve.Symbol]int),
		localT:  make(map[string]bool),
	}
	v.file(f)
	return v.errs
}

type validator struct {
	errs  []Violation
	top   map[*resolve.Symbol]bool      // file-scope value symbols (fn and let decls)
	bound map[*resolve.Symbol]diag.Span // every symbol bound anywhere — uniqueness check
	// localT names the file's declared types. An unqualified
	// *types.Con carrying one of these names denotes the file-local
	// declaration, never the builtin of the same spelling — so a
	// `type Result`/`type List`/`type Unit` in the file makes every
	// "operand is builtin Result/List/Unit" shape test answer false
	// (checker E0415 keeps those constructs from reaching here;
	// the validator still refuses the reading on hand-built IR).
	localT map[string]bool
	// tparams is the declared type-parameter count of file-local
	// function and constructor symbols — the count a reference's
	// TypeArgs must instantiate.
	tparams map[*resolve.Symbol]int
	scope   []map[*resolve.Symbol]bool // lexical scope stack of Var-visible binds
	fn      *Func                      // innermost enclosing function (nil outside one)
	fnTop   bool                       // fn belongs to a FuncDecl (TailCall may target it)
	spawn   int                        // >0 inside a go{} block — escapes are illegal
	pkg     string                     // the file's package clause — entry-point consistency
}

func (v *validator) err(sp diag.Span, node, format string, args ...any) {
	v.errs = append(v.errs, Violation{Span: sp, Node: node, Msg: fmt.Sprintf(format, args...)})
}

// nodeName renders a node for violation reports ("CallDirect").
func nodeName(n any) string {
	return strings.TrimPrefix(fmt.Sprintf("%T", n), "*core.")
}

func (v *validator) base(n any, b Base) {
	if !b.Sp.Valid() {
		v.err(b.Sp, nodeName(n), "node has no valid source span")
	}
	if b.Typ == nil {
		v.err(b.Sp, nodeName(n), "node has nil type")
	}
}

// --- scopes ---

func (v *validator) pushScope() { v.scope = append(v.scope, map[*resolve.Symbol]bool{}) }

func (v *validator) popScope() { v.scope = v.scope[:len(v.scope)-1] }

// bind registers a binder: the symbol must be fresh for the whole
// file (each binder owns its *resolve.Symbol) and becomes visible
// until the enclosing body ends.
func (v *validator) bind(b *Bind, what string) {
	if b == nil {
		v.err(diag.Span{}, what, "missing binder")
		return
	}
	if !b.Sp.Valid() {
		v.err(b.Sp, what, "binder has no valid source span")
	}
	if b.Typ == nil {
		v.err(b.Sp, what, "binder has nil type")
	}
	if b.Discard {
		return // `_` introduces no variable
	}
	if b.Sym == nil {
		v.err(b.Sp, what, "non-discard binder has nil symbol")
		return
	}
	if b.Sym.Kind != resolve.KindVar {
		v.err(b.Sp, what, "binder symbol %q has kind %s, want var", b.Sym.Name, b.Sym.Kind)
	}
	if prev, dup := v.bound[b.Sym]; dup {
		v.err(b.Sp, what, "symbol %q bound twice (first at %s:%d)",
			b.Sym.Name, prev.File, prev.Start)
	} else {
		v.bound[b.Sym] = b.Sp
	}
	v.scope[len(v.scope)-1][b.Sym] = true
}

func (v *validator) inScope(sym *resolve.Symbol) bool {
	for _, v0 := range slices.Backward(v.scope) {
		if v0[sym] {
			return true
		}
	}
	return v.top[sym]
}

// --- files and declarations ---

func (v *validator) file(f *File) {
	if f == nil {
		v.err(diag.Span{}, "File", "nil file")
		return
	}
	if !f.Sp.Valid() {
		v.err(f.Sp, "File", "file has no valid source span")
	}
	if f.Package == "" {
		v.err(f.PkgSp, "File", "package name is empty")
	}
	v.pkg = f.Package
	if !f.PkgSp.Valid() {
		v.err(f.PkgSp, "File", "package clause has no valid source span")
	}
	// File scope first: order-free references between decls.
	for _, d := range f.Decls {
		if d == nil || nilDecl(d) {
			continue // reported in the second pass
		}
		switch d := d.(type) {
		case *FuncDecl:
			if d.Sym != nil {
				v.top[d.Sym] = true
				v.tparams[d.Sym] = len(d.TypeParams)
			}
		case *VarDecl:
			if d.Sym != nil {
				v.top[d.Sym] = true
			}
		case *TypeDecl:
			// Type decls bind no referencable value symbol, but a
			// sum type's constructors carry TypeArgs instantiating
			// the ADT's parameters.
			if d.Sym != nil {
				v.localT[d.Sym.Name] = true
			}
			if s, ok := d.Body.(*TSum); ok {
				for _, vr := range s.Variants {
					if vr.Sym != nil {
						v.tparams[vr.Sym] = len(d.TypeParams)
					}
				}
			}
		case *TestDecl:
		default:
			panic("unreachable")
		}
	}
	// The unit's sibling files share the package scope: their
	// package-level vars, declared type names, and fn/ctor type
	// parameter arities count as established facts here, exactly as
	// if they had been declared in this file.
	if u := f.Unit; u != nil {
		for sym := range u.Vars {
			v.top[sym] = true
		}
		maps.Copy(v.tparams, u.TParams)
		for name := range u.Types {
			v.localT[name] = true
		}
	}
	for _, d := range f.Decls {
		v.decl(d)
	}
	for _, d := range f.EqDecls {
		v.eqDecl(d)
	}
	for _, d := range f.ShowDecls {
		v.showDecl(d)
	}
}

// showDecl checks a generated display function: valid span, a fresh
// KindFunc symbol, a non-nil displayed type, and a well-formed plan.
// Composite plans are legal only here — at a ShowDecl's root.
func (v *validator) showDecl(d *ShowDecl) {
	if d == nil {
		v.err(diag.Span{}, "ShowDecl", "nil display declaration")
		return
	}
	if !d.Sp.Valid() {
		v.err(d.Sp, "ShowDecl", "declaration has no valid source span")
	}
	v.declSym(d.Sp, d.Sym, resolve.KindFunc, "ShowDecl")
	if d.Typ == nil {
		v.err(d.Sp, "ShowDecl", "nil displayed type")
	}
	v.showPlan(d.Plan, true, "ShowDecl.Plan")
}

// showPlan checks a display recipe's shape — the same root/leaf
// invariant eqPlan enforces: composite kinds are legal only at a
// ShowDecl's root so generated display code outside the decls stays
// expression-shaped.
func (v *validator) showPlan(p *ShowPlan, root bool, what string) {
	if p == nil {
		v.err(diag.Span{}, what, "missing display recipe")
		return
	}
	if root {
		if p.Kind < ShowScalar || p.Kind > ShowResult {
			v.err(diag.Span{}, what, "invalid recipe kind %d", int(p.Kind))
			return
		}
	} else {
		switch p.Kind {
		case ShowScalar, ShowRuntime, ShowError, ShowCall:
		default:
			v.err(diag.Span{}, what, "composite recipe kind %s is legal only at a display decl's root", p.Kind)
			return
		}
	}
	switch p.Kind {
	case ShowCall:
		if p.Fn == nil {
			v.err(diag.Span{}, what, "call recipe without a display decl")
		}
	case ShowSlice:
		v.showPlan(p.Elem, false, what+".Elem")
	case ShowTuple:
		for i := range p.Elems {
			v.showPlan(p.Elems[i], false, what+".Elems")
		}
	case ShowRecord:
		for i := range p.Fields {
			v.showField(&p.Fields[i], what)
		}
	case ShowSum, ShowResult:
		for i := range p.Variants {
			vr := &p.Variants[i]
			if vr.Sym == nil || vr.Sym.Kind != resolve.KindCtor {
				v.err(diag.Span{}, what, "variant %d symbol is not a ctor", i)
			}
			for j := range vr.Fields {
				v.showField(&vr.Fields[j], what)
			}
		}
	case ShowScalar, ShowRuntime, ShowError:
	default:
		panic("unreachable")
	}
}

// showField checks one projected operand: a way to select the field
// (Sel, Sym, or Name — Index alone suffices when none names it) and
// a nested leaf recipe.
func (v *validator) showField(f *ShowField, what string) {
	if f.Sel == "" && f.Sym == nil && f.Name == "" && f.Index < 0 {
		v.err(diag.Span{}, what, "field with no selector")
	}
	if f.Sym != nil && f.Sym.Kind != resolve.KindField {
		v.err(diag.Span{}, what, "field symbol %q has kind %s, want field", f.Sym.Name, f.Sym.Kind)
	}
	v.showPlan(f.Plan, false, what+".Plan")
}

// eqDecl checks a generated comparator: valid span, a fresh
// KindFunc symbol, a non-nil compared type, and a well-formed plan.
// Composite plans are legal only here — at an EqDecl's root.
func (v *validator) eqDecl(d *EqDecl) {
	if d == nil {
		v.err(diag.Span{}, "EqDecl", "nil comparator declaration")
		return
	}
	if !d.Sp.Valid() {
		v.err(d.Sp, "EqDecl", "declaration has no valid source span")
	}
	v.declSym(d.Sp, d.Sym, resolve.KindFunc, "EqDecl")
	if d.Typ == nil {
		v.err(d.Sp, "EqDecl", "nil compared type")
	}
	v.eqPlan(d.Plan, true, "EqDecl.Plan")
}

// eqPlan checks a comparison recipe's shape. At an EqDecl's root
// (root=true) every kind is legal; nested plans — an Equal's How and
// the children inside composite plans — must be leaf kinds, which is
// the invariant that keeps generated comparison code expression-
// shaped outside comparator bodies.
func (v *validator) eqPlan(p *EqPlan, root bool, what string) {
	if p == nil {
		v.err(diag.Span{}, what, "missing comparison recipe")
		return
	}
	if root {
		if p.Kind < EqNative || p.Kind > EqResult {
			v.err(diag.Span{}, what, "invalid recipe kind %d", int(p.Kind))
			return
		}
	} else if !eqLeafKind(p.Kind) {
		v.err(diag.Span{}, what, "composite recipe kind %s is legal only at a comparator's root", p.Kind)
		return
	}
	switch p.Kind {
	case EqCall:
		if p.Fn == nil {
			v.err(diag.Span{}, what, "call recipe without a comparator")
		}
	case EqSlice, EqMap:
		v.eqPlan(p.Elem, false, what+".Elem")
	case EqTuple, EqRecord:
		for i := range p.Fields {
			v.eqField(&p.Fields[i], what)
		}
	case EqSum, EqResult:
		for i := range p.Variants {
			vr := &p.Variants[i]
			if vr.Sym == nil || vr.Sym.Kind != resolve.KindCtor {
				v.err(diag.Span{}, what, "variant %d symbol is not a ctor", i)
			}
			for j := range vr.Fields {
				v.eqField(&vr.Fields[j], what)
			}
		}
	case EqNative, EqRuntime, EqError:
	default:
		panic("unreachable")
	}
}

// eqField checks one projected operand pair: a way to select the
// field on both sides (Sel, Sym, or Name — Index alone suffices when
// none names it) and a nested leaf recipe.
func (v *validator) eqField(f *EqField, what string) {
	if f.Sel == "" && f.Sym == nil && f.Name == "" && f.Index < 0 {
		v.err(diag.Span{}, what, "field with no selector")
	}
	if f.Sym != nil && f.Sym.Kind != resolve.KindField {
		v.err(diag.Span{}, what, "field symbol %q has kind %s, want field", f.Sym.Name, f.Sym.Kind)
	}
	v.eqPlan(f.Plan, false, what+".Plan")
}

// declSym checks and registers a declaration's symbol.
func (v *validator) declSym(sp diag.Span, sym *resolve.Symbol, want resolve.Kind, what string) {
	if sym == nil {
		v.err(sp, what, "declaration has nil symbol")
		return
	}
	if sym.Kind != want {
		v.err(sp, what, "symbol %q has kind %s, want %s", sym.Name, sym.Kind, want)
	}
	if prev, dup := v.bound[sym]; dup {
		v.err(sp, what, "symbol %q bound twice (first at %s:%d)", sym.Name, prev.File, prev.Start)
	} else {
		v.bound[sym] = sp
	}
}

func (v *validator) decl(d Decl) {
	if d == nil || nilDecl(d) {
		v.err(diag.Span{}, "Decl", "nil declaration")
		return
	}
	if !d.Span().Valid() {
		v.err(d.Span(), nodeName(d), "declaration has no valid source span")
	}
	switch d := d.(type) {
	case *FuncDecl:
		kind := resolve.KindFunc
		if d.ValueAlias {
			kind = resolve.KindVar
			if d.IsEntry || len(d.TypeParams) != 0 || d.Fn == nil || len(d.Fn.Params) != 1 {
				v.err(d.Sp, "FuncDecl", "value alias must be a monomorphic arity-one non-entry function")
			}
		}
		v.declSym(d.Sp, d.Sym, kind, "FuncDecl")
		if d.Sym != nil && d.Sym.IsGo {
			v.err(d.Sp, "FuncDecl", "function declaration symbol %q is a Go symbol", d.Sym.Name)
		}
		for _, tp := range d.TypeParams {
			if tp == nil {
				v.err(d.Sp, "FuncDecl", "nil type parameter")
			}
		}
		if d.Fn != nil && d.Fn.Sig != nil {
			if n := len(types.FreeVars(d.Fn.Sig)); n != len(d.TypeParams) {
				v.err(d.Sp, "FuncDecl", "signature has %d type variable(s) for %d declared type parameter(s)",
					n, len(d.TypeParams))
			}
		}
		if d.IsEntry {
			v.entryDecl(d)
		}
		v.fnDecl(d.Fn, true, "FuncDecl")
	case *VarDecl:
		v.declSym(d.Sp, d.Sym, resolve.KindVar, "VarDecl")
		if d.Body == nil {
			v.err(d.Sp, "VarDecl", "missing body")
			break
		}
		v.withFunc(nil, false, func() { v.body(d.Body, false) })
	case *TypeDecl:
		v.declSym(d.Sp, d.Sym, resolve.KindType, "TypeDecl")
		for _, tp := range d.TypeParams {
			if tp == nil {
				v.err(d.Sp, "TypeDecl", "nil type parameter")
			}
		}
		// A body's rigid variables must be declared parameters —
		// undeclared variables would emit as undeclared names in
		// generated Go. Unused (phantom) parameters are legal.
		if n := len(typeBodyVars(d.Body)); n > len(d.TypeParams) {
			v.err(d.Sp, "TypeDecl", "body has %d type variable(s) for %d declared type parameter(s)",
				n, len(d.TypeParams))
		}
		v.typeBody(d.Body)
	case *TestDecl:
		if d.Name == "" {
			v.err(d.Sp, "TestDecl", "empty test name")
		}
		if d.Fn != nil && len(d.Fn.Params) != 0 {
			v.err(d.Sp, "TestDecl", "test function has %d params, want 0", len(d.Fn.Params))
		}
		v.fnDecl(d.Fn, false, "TestDecl")
	default:
		panic("unreachable")
	}
}

// entryDecl checks the IsEntry contract: the emitted `func main()`
// drops every parameter, so each bind must be a Unit-typed discard
// (a named bind would dangle in the generated code) and the residual
// must be Unit (a `func main()` result is not allowed).
func (v *validator) entryDecl(d *FuncDecl) {
	if d.Sym == nil || d.Sym.Name != "main" || v.pkg != "main" {
		v.err(d.Sp, "FuncDecl", "IsEntry requires `fn main` in package main")
	}
	if d.Fn == nil {
		return // reported by fnDecl
	}
	for i, p := range d.Fn.Params {
		if !p.Discard {
			v.err(p.Sp, "FuncDecl", "entry parameter %d binds a name — `func main()` binds nothing", i)
		}
		if !v.conIs(p.Typ, "", "Unit", 0) {
			v.err(p.Sp, "FuncDecl", "entry parameter %d is %s, want Unit", i, p.Typ)
		}
	}
	if rt := d.Fn.ResidualType(); !v.conIs(rt, "", "Unit", 0) {
		v.err(d.Sp, "FuncDecl", "entry result is %s, want Unit", rt)
	}
}

// typeBodyVars collects the rigid type variables occurring in a
// type declaration's body — variant payload and record field types,
// or the alias target.
func typeBodyVars(b TypeBody) map[int]*types.Var {
	out := map[int]*types.Var{}
	if b == nil || nilTypeBody(b) {
		return out
	}
	switch b := b.(type) {
	case *TSum:
		for _, vr := range b.Variants {
			for _, f := range vr.Fields {
				maps.Copy(out, types.FreeVars(f.Type))
			}
		}
	case *TRecord:
		for _, f := range b.Fields {
			maps.Copy(out, types.FreeVars(f.Type))
		}
	case *TAlias:
		return types.FreeVars(b.Type)
	case *TAbstract:
	default:
		panic("unreachable")
	}
	return out
}

// targs checks a TypeArgs instantiation against the target's
// declared type-parameter count — when the target is declared in
// this file and the count is known (imported symbols carry no decl
// to compare with).
func (v *validator) targs(sp diag.Span, what string, sym *resolve.Symbol, targs []types.Type) {
	if sym == nil {
		return
	}
	n, ok := v.tparams[sym]
	if !ok {
		return
	}
	if len(targs) != n {
		v.err(sp, what, "%d type argument(s) for %d declared type parameter(s)", len(targs), n)
	}
}

func (v *validator) typeBody(b TypeBody) {
	if b == nil || nilTypeBody(b) {
		v.err(diag.Span{}, "TypeDecl", "missing type body")
		return
	}
	switch b := b.(type) {
	case *TSum:
		for i, vr := range b.Variants {
			if vr.Sym == nil || vr.Sym.Kind != resolve.KindCtor {
				v.err(vr.Sp, "TSum", "variant %d symbol is not a ctor", i)
			} else if prev, dup := v.bound[vr.Sym]; dup {
				v.err(vr.Sp, "TSum", "variant symbol %q bound twice (first at %s:%d)",
					vr.Sym.Name, prev.File, prev.Start)
			} else {
				v.bound[vr.Sym] = vr.Sp
			}
			v.tfields(vr.Fields, "TSum")
		}
	case *TRecord:
		v.tfields(b.Fields, "TRecord")
	case *TAlias:
		if b.Type == nil {
			v.err(b.Sp, "TAlias", "alias has nil type")
		}
	case *TAbstract:
		// name only — nothing to check
	default:
		panic("unreachable")
	}
}

func (v *validator) tfields(fs []TField, what string) {
	for i, f := range fs {
		if f.Type == nil {
			v.err(f.Sp, what, "field %d has nil type", i)
		}
		if f.Sym != nil && f.Sym.Kind != resolve.KindField {
			v.err(f.Sp, what, "field %d symbol %q has kind %s, want field", i, f.Sym.Name, f.Sym.Kind)
		}
	}
}

// fnDecl checks a Func's shape, binds its params, and walks the body
// with it as the innermost enclosing function. isDecl marks FuncDecl
// functions — the only TailCall targets.
func (v *validator) fnDecl(fn *Func, isDecl bool, what string) {
	if fn == nil {
		v.err(diag.Span{}, what, "missing function")
		return
	}
	if !fn.Sp.Valid() {
		v.err(fn.Sp, what, "function has no valid source span")
	}
	if fn.Sig == nil {
		v.err(fn.Sp, what, "function has nil signature")
	} else {
		// Declared arity may split the flat arrow chain — a function
		// returning a function keeps fewer binders than Sig.Params
		// (P0-2 rule 2). Exceeding the arrow length is malformed.
		if len(fn.Params) > len(fn.Sig.Params) {
			v.err(fn.Sp, what, "declared arity %d exceeds signature's flat arity %d",
				len(fn.Params), len(fn.Sig.Params))
		}
		if _, nested := types.Chase(fn.Sig.Ret).(*types.Func); nested {
			v.err(fn.Sp, what, "signature return type is a nested *types.Func (not canonical flat form)")
		}
	}
	v.pushScope()
	defer v.popScope()
	for i, p := range fn.Params {
		v.bind(p, fmt.Sprintf("%s.Params[%d]", what, i))
		if fn.Sig != nil && i < len(fn.Sig.Params) && p != nil && p.Typ != nil &&
			!types.Equal(p.Typ, fn.Sig.Params[i].Type) {
			v.err(p.Sp, what, "param %d type %s differs from signature %s",
				i, p.Typ, fn.Sig.Params[i].Type)
		}
	}
	if fn.Body == nil {
		v.err(fn.Sp, what, "missing body")
		return
	}
	if rt := fn.ResidualType(); rt != nil {
		if bt := tailType(fn.Body); bt != nil && !types.Equal(bt, rt) {
			v.err(fn.Sp, what, "body type %s differs from residual type %s", bt, rt)
		}
	}
	v.withFunc(fn, isDecl, func() { v.body(fn.Body, true) })
}

// withFunc runs f with fn as the innermost enclosing function. A
// Func is a real function boundary, so the go{} depth resets: a
// `try` inside a lambda nested in a `go {}` block legally escapes
// through the lambda (spec section 10.3 — go{} is not a boundary,
// but a lambda inside it is). Escapes in the raw go{} body stay
// rejected — their To pins the *outer* Func, which still sees the
// outer spawn depth.
func (v *validator) withFunc(fn *Func, isDecl bool, f func()) {
	prev, prevTop, prevSpawn := v.fn, v.fnTop, v.spawn
	v.fn, v.fnTop, v.spawn = fn, isDecl, 0
	defer func() { v.fn, v.fnTop, v.spawn = prev, prevTop, prevSpawn }()
	f()
}

// --- bodies ---

// body walks a Body in evaluation order: each Let's expression, then
// its binder, then the tail. inTail marks a body whose Tail is the
// enclosing function's tail position.
func (v *validator) body(b *Body, inTail bool) {
	if b == nil {
		v.err(diag.Span{}, "Body", "missing body")
		return
	}
	v.pushScope()
	defer v.popScope()
	for i, l := range b.Lets {
		if l == nil {
			v.err(diag.Span{}, "Body", "nil let at index %d", i)
			continue
		}
		if !l.Sp.Valid() {
			v.err(l.Sp, "Let", "binding has no valid source span")
		}
		if l.X == nil {
			v.err(l.Sp, "Let", "binding has nil expression")
		} else if call, ok := l.X.(*CallGo); ok && call.Results == GoResultsNone &&
			l.Name != nil && l.Name.Discard {
			v.base(call, call.BaseOf())
			v.callGo(call)
		} else {
			v.expr(l.X, false)
		}
		if l.Name != nil && l.Name.Typ != nil && exprType(l.X) != nil &&
			!types.Equal(l.Name.Typ, exprType(l.X)) {
			v.err(l.Sp, "Let", "binder type %s differs from bound expression type %s",
				l.Name.Typ, exprType(l.X))
		}
		v.bind(l.Name, "Let")
	}
	if b.Tail == nil {
		v.err(diag.Span{}, "Body", "missing tail expression")
		return
	}
	v.expr(b.Tail, inTail)
}

// --- atoms ---

func (v *validator) atom(a Atom, what string) {
	if a == nil || nilAtom(a) {
		v.err(diag.Span{}, what, "missing atom")
		return
	}
	v.base(a, a.BaseOf())
	switch a := a.(type) {
	case *Var:
		if a.Sym == nil {
			v.err(a.Sp, "Var", "nil symbol")
			return
		}
		if a.Sym.Kind != resolve.KindVar {
			v.err(a.Sp, "Var", "symbol %q has kind %s, want var (functions use FuncRef, ctors CtorRef)",
				a.Sym.Name, a.Sym.Kind)
		}
		// A package member is referenced by qualification, not by an
		// enclosing binder — it needs no scope entry (Var's doc: a
		// package-level value). This covers Go members (`os.Args`,
		// `math.Pi`) and imported Gom `let` members (`pkg.x`) alike;
		// a package-less symbol is always a local binding that must
		// live in scope — sibling-package vars arrive via File.Unit.
		if a.Sym.Package == "" && !v.inScope(a.Sym) {
			v.err(a.Sp, "Var", "symbol %q is not in scope — not bound by an enclosing let/param/case",
				a.Sym.Name)
		}
	case *Lit:
		switch a.Kind {
		case syntax.LitInt, syntax.LitFloat, syntax.LitStr, syntax.LitChar, syntax.LitBool, syntax.LitUnit:
		default:
			v.err(a.Sp, "Lit", "invalid literal kind %d", int(a.Kind))
		}
	case *FuncRef:
		if a.Sym == nil || a.Sym.Kind != resolve.KindFunc || a.Sym.IsGo {
			v.err(a.Sp, "FuncRef", "not a Gom function symbol")
		}
		v.targs(a.Sp, "FuncRef", a.Sym, a.TypeArgs)
		if a.Arity < 1 {
			v.err(a.Sp, "FuncRef", "declared arity %d < 1", a.Arity)
		}
		// The emitted declaration takes its declared arity —
		// `func f_(p0, p1, ...)` — while a function *value* is the
		// canonical one-func-per-arrow chain. The two coincide only
		// at declared arity 1; any larger arity needs an adapter
		// Closure instead of a bare reference.
		if a.Arity != 1 {
			v.err(a.Sp, "FuncRef", "declared arity %d does not emit the canonical one-argument value — use an adapter Closure",
				a.Arity)
		}
		if _, ok := types.Chase(a.Typ).(*types.Func); !ok && a.Typ != nil {
			v.err(a.Sp, "FuncRef", "type %s is not a function type", a.Typ)
		}
	case *CtorRef:
		if a.Sym == nil || a.Sym.Kind != resolve.KindCtor {
			v.err(a.Sp, "CtorRef", "not a constructor symbol")
		}
		v.targs(a.Sp, "CtorRef", a.Sym, a.TypeArgs)
		if a.Typ != nil {
			if _, ok := types.Chase(a.Typ).(*types.Func); !ok {
				v.err(a.Sp, "CtorRef", "type %s is not a function type", a.Typ)
			}
		}
	case *GoFuncRef:
		if a.Sym == nil || !a.Sym.IsGo {
			v.err(a.Sp, "GoFuncRef", "not a Go symbol")
		}
		if a.Sym != nil {
			switch a.Sym.Kind {
			case resolve.KindMethod:
				if a.Recv == nil {
					v.err(a.Sp, "GoFuncRef", "method value %q without receiver", a.Sym.Name)
				}
			case resolve.KindFunc:
				if a.Recv != nil {
					v.err(a.Sp, "GoFuncRef", "package function %q with receiver", a.Sym.Name)
				}
			default:
				v.err(a.Sp, "GoFuncRef", "symbol %q has kind %s, want func or method",
					a.Sym.Name, a.Sym.Kind)
			}
		}
		if a.Recv != nil {
			v.atom(a.Recv, "GoFuncRef.Recv")
		}
		// Direct use is legal only when the Go signature matches the
		// Gom value shape exactly (see GoFuncRef's doc).
		if a.Arity != 1 || a.Results != GoResultsOne {
			v.err(a.Sp, "GoFuncRef", "Go signature (arity %d, results %s) needs an adapter Closure — only arity 1 single-result refs are direct",
				a.Arity, a.Results)
		}
		if a.Typ != nil {
			if _, ok := types.Chase(a.Typ).(*types.Func); !ok {
				v.err(a.Sp, "GoFuncRef", "type %s is not a function type", a.Typ)
			}
		}
	case *FieldGet:
		if a.Index < 0 {
			v.err(a.Sp, "FieldGet", "negative index %d", a.Index)
		}
		v.atom(a.Recv, "FieldGet.Recv")
		if a.Ctor != nil && a.Ctor.Kind != resolve.KindCtor {
			v.err(a.Sp, "FieldGet", "owner symbol %q has kind %s, want ctor",
				a.Ctor.Name, a.Ctor.Kind)
		}
		if a.Field != nil {
			if a.Field.Kind != resolve.KindField {
				v.err(a.Sp, "FieldGet", "field symbol %q has kind %s, want field",
					a.Field.Name, a.Field.Kind)
			}
		} else if a.Recv != nil {
			// A symbol-free projection is a tuple element or a
			// positional payload slot. Only the tuple index is
			// checkable: payload arity is the type decl's business.
			if tup, ok := atomType(a.Recv).(*types.Tuple); ok {
				if a.Index < 0 || a.Index >= len(tup.Elems) {
					v.err(a.Sp, "FieldGet", "tuple index %d out of range [0, %d)", a.Index, len(tup.Elems))
				}
			}
		}
	default:
		panic("unreachable")
	}
}

// atomType returns an atom's recorded type, or nil.
func atomType(a Atom) types.Type {
	if a == nil || nilAtom(a) {
		return nil
	}
	// Atom and expression types may still carry metas the checker
	// bound late (a let-bound function value's `?n` to `Int ->
	// String`), so validate chases before inspecting shapes.
	return types.Chase(a.Type())
}

// exprType returns an expression's recorded type, or nil.
func exprType(e Expr) types.Type {
	if e == nil || nilExpr(e) {
		return nil
	}
	return e.Type()
}

// dtreeType returns a decision tree's result type, or nil.
func dtreeType(t DTree) types.Type {
	if t == nil || nilDTree(t) {
		return nil
	}
	return t.Type()
}

// --- expressions ---

func (v *validator) expr(e Expr, inTail bool) {
	if e == nil || nilExpr(e) {
		v.err(diag.Span{}, "Expr", "missing expression")
		return
	}
	v.base(e, e.BaseOf())
	switch e := e.(type) {
	case *Var, *Lit, *FuncRef, *CtorRef, *GoFuncRef, *FieldGet:
		v.atom(e.(Atom), nodeName(e))
	case *Closure:
		v.closure(e)
	case *CallDirect:
		if e.Callee == nil || e.Callee.Kind != resolve.KindFunc || e.Callee.IsGo {
			v.err(e.Sp, "CallDirect", "callee is not a Gom function symbol")
		}
		v.targs(e.Sp, "CallDirect", e.Callee, e.TypeArgs)
		if len(e.Args) != e.Arity {
			v.err(e.Sp, "CallDirect", "call has %d args for declared arity %d — not saturated",
				len(e.Args), e.Arity)
		}
		v.atoms(e.Args, "CallDirect.Args")
	case *CallFunc:
		v.atom(e.Fn, "CallFunc.Fn")
		if _, bad := e.Fn.(*FuncRef); bad {
			v.err(e.Sp, "CallFunc", "Fn is a FuncRef — a known callee must be CallDirect")
		}
		if _, bad := e.Fn.(*CtorRef); bad {
			v.err(e.Sp, "CallFunc", "Fn is a CtorRef — a known callee must be CtorApp")
		}
		if fn, ok := atomType(e.Fn).(*types.Func); ok {
			if len(fn.Params) == 0 {
				v.err(e.Sp, "CallFunc", "Fn type %s has no parameters", atomType(e.Fn))
			}
		} else if atomType(e.Fn) != nil {
			v.err(e.Sp, "CallFunc", "Fn type %s is not a function type", atomType(e.Fn))
		}
		// The canonical value representation is one Go func per
		// arrow, so every CallFunc supplies exactly one argument.
		if len(e.Args) != 1 {
			v.err(e.Sp, "CallFunc", "applying at %d args — the canonical function value takes exactly one",
				len(e.Args))
		}
		v.atoms(e.Args, "CallFunc.Args")
	case *CallGo:
		v.callGo(e)
		if e.Results == GoResultsNone {
			v.err(e.Sp, "CallGo", "zero-result Go call must be a discarded binding")
		} else if e.Results != GoResultsOne {
			v.err(e.Sp, "CallGo", "non-plain Go call (%s) must be wrapped by ResultFromGo/OptionFromGo/TupleFromGo/ErrorFromGo/EscapeCall",
				e.Results)
		}
	case *CallBuiltin:
		if e.Sym == nil || e.Sym.Kind != resolve.KindBuiltin {
			v.err(e.Sp, "CallBuiltin", "not a builtin symbol")
		}
		v.atoms(e.Args, "CallBuiltin.Args")
		if e.Eq != nil {
			if e.Sym == nil || e.Sym.Name != "assert_eq" {
				v.err(e.Sp, "CallBuiltin", "Eq plan on a non-assert_eq builtin")
			}
			v.eqPlan(e.Eq, false, "CallBuiltin.Eq")
		} else if e.Sym != nil && e.Sym.Name == "assert_eq" {
			v.err(e.Sp, "CallBuiltin", "assert_eq without an Eq plan")
		}
		if e.Show != nil {
			if e.Sym == nil || e.Sym.Name != "assert_eq" {
				v.err(e.Sp, "CallBuiltin", "Show plan on a non-assert_eq builtin")
			}
			v.showPlan(e.Show, false, "CallBuiltin.Show")
		}
	case *CtorApp:
		if e.Ctor == nil || e.Ctor.Kind != resolve.KindCtor {
			v.err(e.Sp, "CtorApp", "callee is not a constructor symbol")
		}
		v.targs(e.Sp, "CtorApp", e.Ctor, e.TypeArgs)
		if len(e.Args) != e.Arity {
			v.err(e.Sp, "CtorApp", "constructor takes %d args for declared arity %d",
				len(e.Args), e.Arity)
		}
		v.atoms(e.Args, "CtorApp.Args")
	case *Unary:
		switch e.Op {
		case syntax.UnNeg, syntax.UnNot:
		default:
			v.err(e.Sp, "Unary", "invalid unary operator %d", int(e.Op))
		}
		v.atom(e.X, "Unary.X")
	case *Binary:
		if e.Op.Sugared() {
			v.err(e.Sp, "Binary", "sugared operator %s survived lowering", e.Op)
		}
		switch e.Op {
		case syntax.BinAdd, syntax.BinSub, syntax.BinMul, syntax.BinDiv, syntax.BinMod,
			syntax.BinEq, syntax.BinNe, syntax.BinLt, syntax.BinLe, syntax.BinGt, syntax.BinGe,
			syntax.BinAnd, syntax.BinOr:
		default:
			if !e.Op.Sugared() {
				v.err(e.Sp, "Binary", "invalid binary operator %d", int(e.Op))
			}
		}
		v.atom(e.X, "Binary.X")
		v.atom(e.Y, "Binary.Y")
	case *Equal:
		v.atom(e.X, "Equal.X")
		v.atom(e.Y, "Equal.Y")
		v.eqPlan(e.How, false, "Equal.How")
		if e.Typ != nil && !v.conIs(e.Typ, "", "Bool", 0) {
			v.err(e.Sp, "Equal", "result type %s is not Bool", e.Typ)
		}
		if xt, yt := atomType(e.X), atomType(e.Y); xt != nil && yt != nil && !types.Equal(xt, yt) {
			v.err(e.Sp, "Equal", "operand types differ: %s vs %s", xt, yt)
		}
		if e.How != nil && e.How.Kind == EqCall && e.How.Fn != nil && e.How.Fn.Typ != nil {
			if xt := atomType(e.X); xt != nil && !types.Equal(xt, e.How.Fn.Typ) {
				v.err(e.Sp, "Equal", "operand type %s differs from comparator's %s",
					xt, e.How.Fn.Typ)
			}
		}
	case *If:
		v.atom(e.Cond, "If.Cond")
		if t := atomType(e.Cond); t != nil && !v.conIs(t, "", "Bool", 0) {
			v.err(e.Sp, "If", "condition type %s is not Bool", t)
		}
		v.body(e.Then, inTail)
		if e.Else == nil {
			if t := tailType(e.Then); t != nil && !v.conIs(t, "", "Unit", 0) {
				v.err(e.Sp, "If", "else-less if has non-Unit then type %s", t)
			}
		} else {
			v.body(e.Else, inTail)
			if t, u := tailType(e.Then), tailType(e.Else); t != nil && u != nil && !types.Equal(t, u) {
				v.err(e.Sp, "If", "branch types differ: %s vs %s", t, u)
			}
		}
		if e.Typ != nil {
			if t := tailType(e.Then); t != nil && !types.Equal(e.Typ, t) {
				v.err(e.Sp, "If", "node type %s differs from branch type %s", e.Typ, t)
			}
		}
	case *Match:
		if tt := dtreeType(e.Tree); e.Typ != nil && tt != nil && !types.Equal(e.Typ, tt) {
			v.err(e.Sp, "Match", "node type %s differs from decision-tree type %s", e.Typ, tt)
		}
		v.dtree(e.Tree, inTail)
	case *SelectExpr:
		v.selectExpr(e, inTail)
	case *Spawn:
		if e.Typ != nil && !v.conIs(e.Typ, "", "Unit", 0) {
			v.err(e.Sp, "Spawn", "node type %s is not Unit", e.Typ)
		}
		if e.Body == nil {
			v.err(e.Sp, "Spawn", "missing body")
			break
		}
		if t := tailType(e.Body); t != nil && !v.conIs(t, "", "Unit", 0) {
			v.err(e.Sp, "Spawn", "go block's type is %s, not Unit", t)
		}
		v.spawn++
		v.body(e.Body, false)
		v.spawn--
	case *ForRange:
		v.forRange(e)
	case *TupleNew:
		if len(e.Elems) < 2 {
			v.err(e.Sp, "TupleNew", "tuple has %d elements", len(e.Elems))
		}
		if tup, ok := types.Chase(e.Typ).(*types.Tuple); ok {
			if len(tup.Elems) != len(e.Elems) {
				v.err(e.Sp, "TupleNew", "tuple type has %d elements for %d values",
					len(tup.Elems), len(e.Elems))
			}
		} else if e.Typ != nil {
			v.err(e.Sp, "TupleNew", "type %s is not a tuple type", e.Typ)
		}
		v.atoms(e.Elems, "TupleNew.Elems")
	case *ListNew:
		if !v.conIs(e.Typ, "", "List", 1) && e.Typ != nil {
			v.err(e.Sp, "ListNew", "type %s is not a List", e.Typ)
		}
		v.atoms(e.Elems, "ListNew.Elems")
		if e.Rest != nil {
			v.atom(e.Rest, "ListNew.Rest")
			if t := atomType(e.Rest); t != nil && !v.conIs(t, "", "List", 1) {
				v.err(e.Sp, "ListNew", "rest type %s is not a List", t)
			}
		}
	case *RecordNew:
		v.fieldInits(e.Fields, "RecordNew")
	case *RecordUpdate:
		v.atom(e.Rec, "RecordUpdate.Rec")
		v.fieldInits(e.Fields, "RecordUpdate")
	case *TailCall:
		v.tailCall(e, inTail)
	case *EscapeResult:
		v.escapeResult(e)
	case *EscapeCall:
		v.escapeCall(e)
	case *ResultFromGo:
		v.wrapCall(e, e.Call, "ResultFromGo", GoResultsErr, GoResultsErrOnly)
		if !v.conIs(e.Typ, "", "Result", 2) && e.Typ != nil {
			v.err(e.Sp, "ResultFromGo", "type %s is not a Result", e.Typ)
		}
		if e.NilGuard && (e.Call == nil || e.Call.Results != GoResultsErr) {
			v.err(e.Sp, "ResultFromGo", "NilGuard requires a (T, error) call — GoResultsErrOnly has no payload to guard")
		}
	case *OptionFromGo:
		v.wrapCall(e, e.Call, "OptionFromGo", GoResultsOK, GoResultsNil)
		if !v.conIs(e.Typ, "", "Option", 1) && e.Typ != nil {
			v.err(e.Sp, "OptionFromGo", "type %s is not an Option", e.Typ)
		}
	case *TupleFromGo:
		v.wrapCall(e, e.Call, "TupleFromGo", GoResultsOK, GoResultsPair)
		if _, ok := types.Chase(e.Typ).(*types.Tuple); !ok && e.Typ != nil {
			v.err(e.Sp, "TupleFromGo", "type %s is not a tuple", e.Typ)
		}
	case *ErrorFromGo:
		v.wrapCall(e, e.Call, "ErrorFromGo", GoResultsErrValue)
		if !v.conIs(e.Typ, "", "GoError", 0) && e.Typ != nil {
			v.err(e.Sp, "ErrorFromGo", "type %s is not GoError", e.Typ)
		}
	case *Conv:
		v.conv(e)
	default:
		panic("unreachable")
	}
}

func (v *validator) atoms(as []Atom, what string) {
	for i, a := range as {
		v.atom(a, fmt.Sprintf("%s[%d]", what, i))
	}
}

func (v *validator) closure(e *Closure) {
	fn := e.Fn
	if fn == nil {
		v.err(e.Sp, "Closure", "missing function")
		return
	}
	if fn.Sig != nil && e.Typ != nil && !types.Equal(e.Typ, fn.Sig) {
		v.err(e.Sp, "Closure", "node type %s differs from function signature %s", e.Typ, fn.Sig)
	}
	// A Closure is a function *value*, so its Func is one canonical
	// level: at most one parameter. `func() R` is the zero-argument
	// value and `func(p) R'` one arrow; more parameters would emit
	// the flat direct-call shape, which is not a value of the gom
	// function type — a multi-arrow value must nest closures.
	if len(fn.Params) > 1 {
		v.err(e.Sp, "Closure", "binds %d parameters — the canonical function value binds at most one per arrow",
			len(fn.Params))
	}
	v.fnDecl(fn, false, "Closure")
}

// callGo checks the CallGo shape shared by the bare call site and
// the consumer nodes' Call operand.
func (v *validator) callGo(c *CallGo) {
	if c.Callee == nil || !c.Callee.IsGo {
		v.err(c.Sp, "CallGo", "callee is not a Go symbol")
	} else {
		switch c.Callee.Kind {
		case resolve.KindFunc:
			if c.Recv != nil {
				v.err(c.Sp, "CallGo", "package function %q with receiver", c.Callee.Name)
			}
		case resolve.KindMethod:
			if c.Recv == nil {
				v.err(c.Sp, "CallGo", "method %q without receiver", c.Callee.Name)
			}
		default:
			v.err(c.Sp, "CallGo", "callee %q has kind %s, want func or method",
				c.Callee.Name, c.Callee.Kind)
		}
	}
	if c.Recv != nil {
		v.atom(c.Recv, "CallGo.Recv")
	}
	v.atoms(c.Args, "CallGo.Args")
	if c.Arity < 0 {
		v.err(c.Sp, "CallGo", "negative arity %d", c.Arity)
	}
	if c.Variadic {
		if len(c.Args) < c.Arity-1 {
			v.err(c.Sp, "CallGo", "variadic call has %d args, needs at least %d",
				len(c.Args), c.Arity-1)
		}
	} else if len(c.Args) != c.Arity {
		v.err(c.Sp, "CallGo", "call has %d args for Go arity %d", len(c.Args), c.Arity)
	}
	if c.Spread && (!c.Variadic || len(c.Args) == 0) {
		v.err(c.Sp, "CallGo", "spread call requires a variadic callee with a splice argument")
	}
	switch c.Results {
	case GoResultsNone:
		if !v.conIs(c.Type(), "", "Unit", 0) {
			v.err(c.Sp, "CallGo", "zero-result Go call must have Unit type")
		}
	case GoResultsOne, GoResultsErr, GoResultsErrOnly, GoResultsOK, GoResultsPair, GoResultsNil, GoResultsErrValue:
	default:
		v.err(c.Sp, "CallGo", "invalid result shape %d", int(c.Results))
	}
}

// wrapCall checks a consumer node's embedded multi-result call: the
// CallGo itself plus the shape it must have.
func (v *validator) wrapCall(e Expr, c *CallGo, what string, want ...GoResults) {
	if c == nil {
		v.err(e.Span(), what, "missing call")
		return
	}
	v.callGo(c)
	if slices.Contains(want, c.Results) {
		return
	}
	v.err(c.Sp, what, "result shape %s not usable here", c.Results)
}

// escapeTarget checks that To is the innermost enclosing Func and
// returns the boundary's Result error type (ok=false when the
// boundary's result is not a Result type).
func (v *validator) escapeTarget(sp diag.Span, to *Func, what string) (errT types.Type, ok bool) {
	if v.fn == nil {
		v.err(sp, what, "escape outside any function")
		return nil, false
	}
	if to != v.fn {
		v.err(sp, what, "escape target is not the innermost enclosing function")
		return nil, false
	}
	if v.spawn > 0 {
		v.err(sp, what, "escape crosses a go{} boundary")
		return nil, false
	}
	if v.fn.Sig == nil {
		return nil, false // nil signature already reported
	}
	// The escape leaves through the boundary's *emitted* result — the
	// residual type, not Sig.Ret: for an arity-split boundary such as
	// `fn f (a) -> (Int -> Result _ e)` the outer body's result is the
	// remaining arrow, which is not a Result and must be rejected.
	rt := v.fn.ResidualType()
	c, isRes := v.resultType(rt)
	if !isRes {
		v.err(sp, what, "boundary result type %s is not a Result", rt)
		return nil, false
	}
	return c.Args[1], true
}

func (v *validator) escapeResult(e *EscapeResult) {
	v.atom(e.X, "EscapeResult.X")
	xt := atomType(e.X)
	c, isRes := v.resultType(xt)
	if xt != nil && !isRes {
		v.err(e.Sp, "EscapeResult", "operand type %s is not a Result", xt)
	}
	if isRes && e.Typ != nil && !types.Equal(e.Typ, c.Args[0]) {
		v.err(e.Sp, "EscapeResult", "node type %s differs from operand's Ok payload %s", e.Typ, c.Args[0])
	}
	be, ok := v.escapeTarget(e.Sp, e.To, "EscapeResult")
	if isRes && ok && !types.Equal(c.Args[1], be) {
		v.err(e.Sp, "EscapeResult", "error type %s does not match boundary's %s", c.Args[1], be)
	}
	if e.Wrap != "" && isRes && !v.conIs(c.Args[1], "", "GoError", 0) {
		v.err(e.Sp, "EscapeResult", "|?> wraps only GoError steps; error type is %s", c.Args[1])
	}
}

func (v *validator) escapeCall(e *EscapeCall) {
	v.wrapCall(e, e.Call, "EscapeCall", GoResultsErr, GoResultsErrOnly)
	if e.Call != nil && e.Call.Typ != nil && e.Typ != nil && !types.Equal(e.Typ, e.Call.Typ) {
		v.err(e.Sp, "EscapeCall", "node type %s differs from call's first-result type %s", e.Typ, e.Call.Typ)
	}
	if e.NilGuard && (e.Call == nil || e.Call.Results != GoResultsErr) {
		v.err(e.Sp, "EscapeCall", "NilGuard requires a (T, error) call — GoResultsErrOnly has no payload to guard")
	}
	be, ok := v.escapeTarget(e.Sp, e.To, "EscapeCall")
	if ok && !v.conIs(be, "", "GoError", 0) {
		v.err(e.Sp, "EscapeCall", "boundary error type %s is not GoError — the (T, error) escape needs Result _ GoError", be)
	}
}

func (v *validator) tailCall(e *TailCall, inTail bool) {
	if !inTail {
		v.err(e.Sp, "TailCall", "self-tail call not in tail position")
	}
	if v.fn == nil || !v.fnTop {
		v.err(e.Sp, "TailCall", "self-tail call outside a function declaration")
		return
	}
	if e.To != v.fn {
		v.err(e.Sp, "TailCall", "self-tail call does not target the innermost enclosing function")
		return
	}
	if !e.To.TailLoop {
		v.err(e.Sp, "TailCall", "enclosing function lacks TailLoop")
	}
	if len(e.Args) != len(e.To.Params) {
		v.err(e.Sp, "TailCall", "tail call has %d args for %d params",
			len(e.Args), len(e.To.Params))
	}
	v.atoms(e.Args, "TailCall.Args")
	if v.spawn > 0 {
		v.err(e.Sp, "TailCall", "self-tail call across a go{} boundary")
	}
}

func (v *validator) conv(e *Conv) {
	switch e.Kind {
	case ConvNum, ConvNamed, ConvBoxAny, ConvIface:
		// operand/target legality is Go's conversion and
		// assignability rules — settled at check time.
	case ConvChanDir:
		if t := atomType(e.X); t != nil && !v.conIs(t, "", "Chan", 1) {
			v.err(e.Sp, "Conv", "chan-dir operand type %s is not a Chan", t)
		}
		if e.Typ != nil && !v.conIs(e.Typ, "", "SendChan", 1) && !v.conIs(e.Typ, "", "RecvChan", 1) {
			v.err(e.Sp, "Conv", "chan-dir result type %s is not a SendChan or RecvChan", e.Typ)
		}
	case ConvSliceToGo:
		if t := atomType(e.X); t != nil && !v.conIs(t, "", "List", 1) {
			v.err(e.Sp, "Conv", "slice-to-go operand type %s is not a List", t)
		}
		if e.Typ != nil && !v.conIs(e.Typ, "Go", "Slice", 1) {
			v.err(e.Sp, "Conv", "slice-to-go result type %s is not Go.Slice", e.Typ)
		}
	case ConvSliceFromGo:
		if t := atomType(e.X); t != nil && !v.conIs(t, "Go", "Slice", 1) {
			v.err(e.Sp, "Conv", "slice-from-go operand type %s is not Go.Slice", t)
		}
		if e.Typ != nil && !v.conIs(e.Typ, "", "List", 1) {
			v.err(e.Sp, "Conv", "slice-from-go result type %s is not a List", e.Typ)
		}
	case ConvSliceBox:
		if t := atomType(e.X); t != nil &&
			!v.conIs(t, "", "List", 1) && !v.conIs(t, "Go", "Slice", 1) {
			v.err(e.Sp, "Conv", "slice-box operand type %s is not a List or Go.Slice", t)
		}
		if e.Typ != nil && !v.conIs(e.Typ, "Go", "Slice", 1) {
			v.err(e.Sp, "Conv", "slice-box result type %s is not Go.Slice", e.Typ)
		}
	case ConvOptNil:
		// `option.FromGo(v, v != nil)` — the result must be
		// `Option` over the operand's own type.
		var arg types.Type
		if c, ok := types.Chase(e.Typ).(*types.Con); ok && c.Qual == "" && c.Name == "Option" && len(c.Args) == 1 {
			arg = c.Args[0]
		} else if e.Typ != nil {
			v.err(e.Sp, "Conv", "opt-nil result type %s is not an Option", e.Typ)
		}
		if t := atomType(e.X); t != nil && arg != nil && !types.Equal(t, arg) {
			v.err(e.Sp, "Conv", "opt-nil operand type %s is not the Option payload %s", t, arg)
		}
	default:
		v.err(e.Sp, "Conv", "invalid conversion kind %d", int(e.Kind))
	}
	v.atom(e.X, "Conv.X")
}

func (v *validator) selectExpr(e *SelectExpr, inTail bool) {
	if len(e.Arms) == 0 {
		v.err(e.Sp, "SelectExpr", "select has no arms")
	}
	defaults := 0
	var want types.Type
	for i, arm := range e.Arms {
		if arm == nil || nilSelArm(arm) {
			v.err(e.Sp, "SelectExpr", "nil arm at index %d", i)
			continue
		}
		var body *Body
		switch a := arm.(type) {
		case *SelRecv:
			v.atom(a.Chan, "SelRecv.Chan")
			if ct := atomType(a.Chan); ct != nil &&
				!v.conIs(ct, "", "Chan", 1) && !v.conIs(ct, "", "RecvChan", 1) {
				v.err(a.Sp, "SelRecv", "channel type %s is not Chan/RecvChan", ct)
			}
			if a.Bind == nil {
				v.err(a.Sp, "SelRecv", "missing bind")
			} else {
				if !a.Bind.Discard && !v.conIs(a.Bind.Typ, "", "Option", 1) {
					v.err(a.Sp, "SelRecv", "receive bind type %s is not an Option", a.Bind.Typ)
				}
				v.pushScope()
				v.bind(a.Bind, "SelRecv.Bind")
				v.body(a.Body, inTail) // nil body reported by body
				v.popScope()
			}
		case *SelSend:
			v.atom(a.Chan, "SelSend.Chan")
			if ct := atomType(a.Chan); ct != nil &&
				!v.conIs(ct, "", "Chan", 1) && !v.conIs(ct, "", "SendChan", 1) {
				v.err(a.Sp, "SelSend", "channel type %s is not Chan/SendChan", ct)
			}
			v.atom(a.Value, "SelSend.Value")
			if a.Body == nil {
				v.err(a.Sp, "SelSend", "missing body")
			}
			body = a.Body
		case *SelDefault:
			defaults++
			if a.Body == nil {
				v.err(a.Sp, "SelDefault", "missing body")
			}
			body = a.Body
		default:
			panic("unreachable")
		}
		if body != nil {
			v.body(body, inTail)
		}
		if t := armBodyType(arm); t != nil {
			if want == nil {
				want = t
			} else if !types.Equal(t, want) {
				v.err(arm.Span(), "SelectExpr", "arm %d's type %s differs from earlier %s", i, t, want)
			}
		}
	}
	if defaults > 1 {
		v.err(e.Sp, "SelectExpr", "select has %d default arms", defaults)
	}
	if e.Typ != nil && want != nil && !types.Equal(e.Typ, want) {
		v.err(e.Sp, "SelectExpr", "node type %s differs from arm type %s", e.Typ, want)
	}
}

// armBodyType returns the tail type of an arm's body.
func armBodyType(a SelArm) types.Type {
	var b *Body
	switch a := a.(type) {
	case *SelRecv:
		b = a.Body
	case *SelSend:
		b = a.Body
	case *SelDefault:
		b = a.Body
	default:
		panic("unreachable")
	}
	return tailType(b)
}

func (v *validator) forRange(e *ForRange) {
	switch e.Kind {
	case ForList:
		if t := atomType(e.Iter); t != nil &&
			!v.conIs(t, "", "List", 1) && !v.conIs(t, "", "Array", 1) && !v.conIs(t, "Go", "Slice", 1) {
			v.err(e.Sp, "ForRange", "list-loop iterable type %s is not List/Array/Go.Slice", t)
		}
	case ForChan:
		if t := atomType(e.Iter); t != nil &&
			!v.conIs(t, "", "Chan", 1) && !v.conIs(t, "", "RecvChan", 1) {
			v.err(e.Sp, "ForRange", "chan-loop iterable type %s is not Chan/RecvChan", t)
		}
	default:
		v.err(e.Sp, "ForRange", "invalid for kind %d", int(e.Kind))
	}
	v.atom(e.Iter, "ForRange.Iter")
	if e.Elem == nil {
		v.err(e.Sp, "ForRange", "missing element binder")
	}
	if e.Body == nil {
		v.err(e.Sp, "ForRange", "missing body")
	} else if t := tailType(e.Body); t != nil && !v.conIs(t, "", "Unit", 0) {
		v.err(e.Sp, "ForRange", "loop body's type is %s, not Unit", t)
	}
	v.pushScope()
	defer v.popScope()
	if e.Elem != nil {
		v.bind(e.Elem, "ForRange.Elem")
	}
	if e.Body != nil {
		v.body(e.Body, false)
	}
	if e.Typ != nil && !v.conIs(e.Typ, "", "Unit", 0) {
		v.err(e.Sp, "ForRange", "node type %s is not Unit", e.Typ)
	}
}

func (v *validator) fieldInits(fs []FieldInit, what string) {
	seen := map[*resolve.Symbol]bool{}
	for i, f := range fs {
		if f.Sym == nil || f.Sym.Kind != resolve.KindField {
			v.err(f.Sp, what, "field %d is not a field symbol", i)
		} else if seen[f.Sym] {
			v.err(f.Sp, what, "field %q initialized twice", f.Sym.Name)
		} else {
			seen[f.Sym] = true
		}
		v.atom(f.Value, fmt.Sprintf("%s.Fields[%d]", what, i))
	}
}

// --- decision trees ---

func (v *validator) dtree(t DTree, inTail bool) {
	if t == nil || nilDTree(t) {
		v.err(diag.Span{}, "Match", "nil decision tree")
		return
	}
	v.base(t, dtreeBase(t))
	switch t := t.(type) {
	case *DSwitch:
		v.atom(t.Subj, "DSwitch.Subj")
		switch t.Kind {
		case SwitchType, SwitchTag, SwitchValue, SwitchList:
		default:
			v.err(t.Sp, "DSwitch", "invalid switch kind %d", int(t.Kind))
		}
		if t.Kind == SwitchList {
			if st := atomType(t.Subj); st != nil && !v.conIs(st, "", "List", 1) {
				v.err(t.Sp, "DSwitch", "list switch's subject type %s is not a List", st)
			}
		}
		if len(t.Cases) == 0 {
			v.err(t.Sp, "DSwitch", "switch has no cases")
		}
		for i := range t.Cases {
			v.dcase(&t.Cases[i], t, inTail)
		}
		if t.Default == nil || nilDTree(t.Default) {
			v.err(t.Sp, "DSwitch", "switch has no default")
		} else {
			switch t.Kind {
			case SwitchType, SwitchTag:
				if _, ok := t.Default.(*DPanic); !ok {
					v.err(t.Sp, "DSwitch",
						"%s switch's default is %s, want DPanic — invalid Go-side values must panic (expand wildcards into cases)",
						t.Kind, nodeName(t.Default))
				}
			case SwitchValue, SwitchList:
				// any tree continues the match
			}
			v.dtreeChild(t.Default, t.Typ, "DSwitch.Default", inTail)
		}
	case *DLeaf:
		if t.Body == nil {
			v.err(t.Sp, "DLeaf", "missing body")
			break
		}
		v.body(t.Body, inTail)
		if bt := tailType(t.Body); bt != nil && t.Typ != nil && !types.Equal(bt, t.Typ) {
			v.err(t.Sp, "DLeaf", "body type %s differs from node type %s", bt, t.Typ)
		}
	case *DPanic:
		v.atom(t.Subj, "DPanic.Subj")
		if t.TypeName == "" {
			v.err(t.Sp, "DPanic", "empty type name for invalid-value panic")
		}
	case *DGuard:
		v.atom(t.Cond, "DGuard.Cond")
		if c := atomType(t.Cond); c != nil && !v.conIs(c, "", "Bool", 0) {
			v.err(t.Sp, "DGuard", "guard type %s is not Bool", c)
		}
		v.dtreeChild(t.Then, t.Typ, "DGuard.Then", inTail)
		v.dtreeChild(t.Else, t.Typ, "DGuard.Else", inTail)
	default:
		panic("unreachable")
	}
}

// dtreeChild checks that a subtree's result type agrees with its
// parent's before recursing — a decision tree produces one result
// type, so a nested switch or guard branch must carry the same Typ.
func (v *validator) dtreeChild(sub DTree, parent types.Type, what string, inTail bool) {
	if sub == nil || nilDTree(sub) {
		v.err(diag.Span{}, what, "missing subtree")
		return
	}
	if parent != nil && sub.Type() != nil && !types.Equal(sub.Type(), parent) {
		v.err(sub.Span(), what, "subtree type %s differs from parent tree type %s",
			sub.Type(), parent)
	}
	v.dtree(sub, inTail)
}

func (v *validator) dcase(c *DCase, sw *DSwitch, inTail bool) {
	kind := sw.Kind
	if c.Test == nil || nilDTest(c.Test) {
		v.err(c.Sp, "DCase", "missing test")
	} else {
		v.dtest(c.Test, kind)
	}
	nb := len(c.Binds)
	switch kind {
	case SwitchType:
		if nb > 1 {
			v.err(c.Sp, "DCase", "type-switch case binds %d vars, want at most 1 (narrowed subject)", nb)
		}
	case SwitchTag:
		if nb > 1 {
			v.err(c.Sp, "DCase", "tag-switch case binds %d vars, want at most 1 (payload)", nb)
		}
	case SwitchValue:
		if nb != 0 {
			v.err(c.Sp, "DCase", "value-switch case binds %d vars, want 0", nb)
		}
	case SwitchList:
		if lt, ok := c.Test.(*DListTest); ok && lt != nil {
			if lt.Cons && nb > 2 {
				v.err(c.Sp, "DCase", "cons case binds %d vars, want at most 2 (head, tail)", nb)
			}
			if !lt.Cons && nb != 0 {
				v.err(c.Sp, "DCase", "nil case binds %d vars, want 0", nb)
			}
		}
	}
	v.pushScope()
	defer v.popScope()
	for i, b := range c.Binds {
		v.bind(b, fmt.Sprintf("DCase.Binds[%d]", i))
	}
	if c.Body == nil {
		v.err(c.Sp, "DCase", "missing body")
		return
	}
	v.body(c.Body, inTail)
	if bt := tailType(c.Body); bt != nil && sw.Typ != nil && !types.Equal(bt, sw.Typ) {
		v.err(c.Sp, "DCase", "case body type %s differs from switch type %s", bt, sw.Typ)
	}
}

func (v *validator) dtest(t DTest, kind SwitchKind) {
	switch t := t.(type) {
	case *DCtorTest:
		if kind != SwitchType && kind != SwitchTag {
			v.err(t.Sp, "DCtorTest", "constructor test in a %s switch", kind)
		}
		if t.Ctor == nil || t.Ctor.Kind != resolve.KindCtor {
			v.err(t.Sp, "DCtorTest", "not a constructor symbol")
		}
	case *DLitTest:
		if kind != SwitchValue {
			v.err(t.Sp, "DLitTest", "literal test in a %s switch", kind)
		}
		switch t.Kind {
		case syntax.LitInt, syntax.LitFloat, syntax.LitStr, syntax.LitChar, syntax.LitBool:
		default:
			v.err(t.Sp, "DLitTest", "invalid literal test kind %d", int(t.Kind))
		}
	case *DListTest:
		if kind != SwitchList {
			v.err(t.Sp, "DListTest", "list test in a %s switch", kind)
		}
	default:
		panic("unreachable")
	}
}

// --- helpers ---

// The nilX helpers report whether a sealed-interface value is a
// typed-nil pointer — malformed IR that a plain `x == nil` misses
// and that must produce a Violation rather than panic the walk.
// Each is a sumtype-checked exhaustive switch over its family.

func nilExpr(e Expr) bool {
	switch e := e.(type) {
	case *Var:
		return e == nil
	case *Lit:
		return e == nil
	case *FuncRef:
		return e == nil
	case *CtorRef:
		return e == nil
	case *GoFuncRef:
		return e == nil
	case *FieldGet:
		return e == nil
	case *Closure:
		return e == nil
	case *CallDirect:
		return e == nil
	case *CallFunc:
		return e == nil
	case *CallGo:
		return e == nil
	case *CallBuiltin:
		return e == nil
	case *CtorApp:
		return e == nil
	case *Unary:
		return e == nil
	case *Binary:
		return e == nil
	case *Equal:
		return e == nil
	case *If:
		return e == nil
	case *Match:
		return e == nil
	case *SelectExpr:
		return e == nil
	case *Spawn:
		return e == nil
	case *ForRange:
		return e == nil
	case *TupleNew:
		return e == nil
	case *ListNew:
		return e == nil
	case *RecordNew:
		return e == nil
	case *RecordUpdate:
		return e == nil
	case *TailCall:
		return e == nil
	case *EscapeResult:
		return e == nil
	case *EscapeCall:
		return e == nil
	case *ResultFromGo:
		return e == nil
	case *OptionFromGo:
		return e == nil
	case *TupleFromGo:
		return e == nil
	case *ErrorFromGo:
		return e == nil
	case *Conv:
		return e == nil
	default:
		panic("unreachable")
	}
}

func nilAtom(a Atom) bool {
	switch a := a.(type) {
	case *Var:
		return a == nil
	case *Lit:
		return a == nil
	case *FuncRef:
		return a == nil
	case *CtorRef:
		return a == nil
	case *GoFuncRef:
		return a == nil
	case *FieldGet:
		return a == nil
	default:
		panic("unreachable")
	}
}

func nilDecl(d Decl) bool {
	switch d := d.(type) {
	case *FuncDecl:
		return d == nil
	case *VarDecl:
		return d == nil
	case *TypeDecl:
		return d == nil
	case *TestDecl:
		return d == nil
	default:
		panic("unreachable")
	}
}

func nilTypeBody(b TypeBody) bool {
	switch b := b.(type) {
	case *TSum:
		return b == nil
	case *TRecord:
		return b == nil
	case *TAlias:
		return b == nil
	case *TAbstract:
		return b == nil
	default:
		panic("unreachable")
	}
}

func nilDTree(t DTree) bool {
	switch t := t.(type) {
	case *DSwitch:
		return t == nil
	case *DLeaf:
		return t == nil
	case *DPanic:
		return t == nil
	case *DGuard:
		return t == nil
	default:
		panic("unreachable")
	}
}

func nilDTest(t DTest) bool {
	switch t := t.(type) {
	case *DCtorTest:
		return t == nil
	case *DLitTest:
		return t == nil
	case *DListTest:
		return t == nil
	default:
		panic("unreachable")
	}
}

func nilSelArm(a SelArm) bool {
	switch a := a.(type) {
	case *SelRecv:
		return a == nil
	case *SelSend:
		return a == nil
	case *SelDefault:
		return a == nil
	default:
		panic("unreachable")
	}
}

// dtreeBase extracts the Base of a DTree node for span/type checks.
func dtreeBase(t DTree) Base {
	switch t := t.(type) {
	case *DSwitch:
		return t.Base
	case *DLeaf:
		return t.Base
	case *DPanic:
		return t.Base
	case *DGuard:
		return t.Base
	default:
		panic("unreachable")
	}
}

// tailType returns the type of a body's tail, nil when absent.
func tailType(b *Body) types.Type {
	if b == nil || b.Tail == nil {
		return nil
	}
	return exprType(b.Tail)
}

// conIs reports whether t is a *types.Con with the given qualifier,
// name and argument count — and, for an unqualified name, that the
// file does not declare a same-named type of its own. Bound metas
// are chased first — inferred types (a lambda boundary's Sig.Ret,
// for one) may still carry them.
func (v *validator) conIs(t types.Type, qual, name string, nargs int) bool {
	if qual == "" && v.localT[name] {
		return false
	}
	c, ok := types.Chase(t).(*types.Con)
	return ok && c.Qual == qual && c.Name == name && len(c.Args) == nargs
}

// resultType unpacks the builtin `Result a e` — not a file-local
// `type Result`, which spells the same Con but denotes an ADT.
func (v *validator) resultType(t types.Type) (*types.Con, bool) {
	c, ok := types.Chase(t).(*types.Con)
	if !ok || c.Qual != "" || c.Name != "Result" || len(c.Args) != 2 || v.localT["Result"] {
		return nil, false
	}
	return c, true
}
