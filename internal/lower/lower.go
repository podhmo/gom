package lower

import (
	"fmt"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Env supplies the cross-file facts lowering cannot recover from a
// check.File alone. Every field is optional: a nil Env (or a nil
// field) makes lowering fall back to the file's own declarations and
// to heuristics derived from the Gom view of types, which is exact
// whenever the input is a self-contained checked file.
type Env struct {
	// Types answers declaration-level facts about nominal types —
	// the variant set of a sum type (needed to expand wildcard arms
	// of a match over a foreign ADT) and the field order of a record
	// type. The file's own type declarations are consulted first;
	// the oracle covers types declared in other packages.
	Types TypeOracle

	// FuncArity reports a Gom function symbol's *declared* arity —
	// the length of its parameter list — which differs from the flat
	// arity of its type when the function returns a function (an
	// arity-split declaration, P0-2). A negative or unknown answer
	// falls back to the flat arity of the reference's type, which is
	// correct for ordinary functions. File-local FuncDecls are
	// consulted automatically; the hook covers imported functions.
	FuncArity func(sym *resolve.Symbol) int

	// GoSig reports a Go function or method symbol's Go-level
	// signature facts — its Go arity, variadicness and result shape —
	// which check.CallInfo does not carry. It is consulted for bare
	// references (GoFuncRef / adapter Closures) and to sharpen result
	// shapes that the Gom view of the type cannot express. The bool
	// reports whether the signature is known.
	GoSig func(sym *resolve.Symbol) (GoSig, bool)

	// Lines converts byte offsets to source positions for the `|?>`
	// step name of a nameless step (a lambda): the name is rendered
	// as "file:line". Nil makes lowering fall back to a bare offset.
	Lines *diag.LineIndex

	// Tests selects `gom test` lowering (spec 11.6): `test`
	// declarations and `@test`-marked fns and lets stay in the Core
	// IR. The zero value is `gom build` mode — test-only code is
	// still type- and pattern-checked upstream, but dropped here so
	// no test code reaches the generated output.
	Tests bool
}

// GoSig is the Go-level signature of a Go function or method symbol —
// the facts check.CallInfo does not preserve for later phases.
type GoSig struct {
	// Arity is the callee's fixed parameter count; the variadic
	// element counts once, like core.CallGo.Arity.
	Arity int
	// Variadic marks a variadic final parameter.
	Variadic bool
	// Results is the call's result shape (core.GoResults*).
	Results core.GoResults
	// Params is the Go-side view of the parameter types, len == Arity
	// (a variadic last entry keeps its `Go.Slice` shape). It differs
	// from the Gom view of the signature only where the two spell
	// differently — today only `int` → `Go.Int`. Nil means "every
	// parameter is its Gom-view type" (no argument conversions).
	Params []types.Type
	// ResTypes is the Go-side view of the result types — the raw
	// types the call binds at, in result order (the `(T, error)`
	// shape has two). Nil means "each result is its Gom-view type".
	ResTypes []types.Type
}

// Variant is one constructor of a sum type — the answer TypeOracle
// gives for a sum-typed *types.Con.
type Variant struct {
	// Sym is the constructor symbol (KindCtor).
	Sym *resolve.Symbol
	// Fields are the variant's payload slots in declaration order,
	// already instantiated at the queried type's arguments.
	Fields []check.TField
}

// TypeOracle answers the type-declaration facts lowering cannot get
// from a check.File: the constructor set of a sum type — needed to
// expand wildcard match arms over a foreign ADT — and the field order
// of a record type — needed for record literals, updates and record
// patterns. Answers carry instantiated field types.
type TypeOracle interface {
	// Variants returns the sum type's constructors in declaration
	// order, or nil when c is not a known sum type.
	Variants(c *types.Con) []Variant
	// Fields returns the record type's fields in declaration order,
	// or nil when c is not a known record type.
	Fields(c *types.Con) []check.TField
}

// File lowers a fully type- and pattern-checked file into a Core IR
// file. The contract:
//
//   - every check.CallInfo is honoured as given — no re-derivation
//     from names or shapes;
//   - the output is ANF: complex operands are bound by Let in exact
//     left-to-right source order;
//   - patterns, pipes, compositions, `_` placeholders, try and |?>
//     are all gone — only Core IR nodes remain;
//   - the returned file passes core.Validate; a violation is a bug
//     in lowering (the function reports it as a diagnostic rather
//     than silently handing codegen a malformed tree).
func File(f *check.File, env *Env) (*core.File, []diag.Diagnostic) {
	l := &lowerer{
		arity:     map[*resolve.Symbol]int{},
		thunks:    map[*resolve.Symbol]bool{},
		typeD:     map[string]*check.TypeDecl{},
		ctorT:     map[*resolve.Symbol]*check.TypeDecl{},
		boundSyms: map[*resolve.Symbol]bool{},
	}
	if env == nil {
		env = &Env{}
	}
	l.env = env

	// First pass: index declarations for oracle, arity, and thunk
	// lookup.
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *check.FnDecl:
			l.arity[d.Sym] = len(d.Params)
			if d.Thunk {
				l.thunks[d.Sym] = true
			}
		case *check.TypeDecl:
			l.typeD[typeKey(d.Sym)] = d
			if s, ok := d.Body.(*check.SumType); ok {
				for _, v := range s.Variants {
					l.ctorT[v.Sym] = d
				}
			}
		case *check.LetDecl, *check.TestDecl:
		default:
			panic("unreachable")
		}
	}

	out := &core.File{
		Sp:      f.Sp,
		Package: f.Package,
		PkgSp:   f.PkgSp,
	}
	for _, im := range f.Imports {
		out.Imports = append(out.Imports, core.Import{
			Sp: im.Sp, Path: im.Path, IsGo: im.IsGo, Sym: im.Sym,
		})
	}
	for _, d := range f.Decls {
		l.decl(d, out)
	}
	l.checkTailRec(out)

	if errs := core.Validate(out); len(errs) > 0 {
		for _, v := range errs {
			l.diags = append(l.diags, coreViolation(v))
		}
	}
	return out, l.diags
}

// isEntryPoint reports whether sym is the program entry — the rule
// codegen used to infer from the file's package and the symbol's
// name, now settled here so Core IR carries the decision. The shape
// is exactly `() -> Unit`: one parameter whose Unit value the
// pattern discards (the `()` literal or `_`), returning Unit. A
// named `(u: Unit)` binds a name the emitted `func main()` cannot
// provide, so it lowers to an ordinary Unit-parameter function like
// any other — Go then rejects its signature, which is the
// checker-side gap the entry-point problem file already records.
func isEntryPoint(pkg string, sym *resolve.Symbol, fn *core.Func) bool {
	if pkg != "main" || sym == nil || sym.Name != "main" {
		return false
	}
	if len(fn.Params) != 1 || !fn.Params[0].Discard ||
		!types.Equal(fn.Params[0].Typ, types.Unit) {
		return false
	}
	return types.Equal(fn.ResidualType(), types.Unit)
}

// lowerer is the per-file lowering context.
type lowerer struct {
	env *Env

	// arity maps file-local FuncDecl symbols to declared arity.
	arity map[*resolve.Symbol]int
	// thunks marks the generalized value-lets whose `() -> T`
	// declaration is a thunk (check.FnDecl.Thunk) — a reference to
	// one emits the call `x[T]()`, while a declared `fn f () -> T`
	// referenced in value position stays a FuncRef.
	thunks map[*resolve.Symbol]bool
	// typeD indexes the file's TypeDecls by qualified name key; ctorT
	// maps a locally declared variant symbol to its TypeDecl.
	typeD map[string]*check.TypeDecl
	ctorT map[*resolve.Symbol]*check.TypeDecl

	tmpSeq int

	// builtins interns symbols lowering synthesises (builtin
	// constructors for generated patterns, builtin calls) so one
	// lowering run emits a single symbol object per (kind, name).
	builtins map[symKey]*resolve.Symbol

	// boundSyms tracks checker symbols already bound once in this
	// file — a second bind under an active substitution is a
	// duplicated arm body (or-pattern, wildcard expansion, guard
	// continuation) and needs a fresh binder.
	boundSyms map[*resolve.Symbol]bool

	// fnStack is the stack of enclosing function boundaries — the
	// innermost is the escape To target. Entries are *core.Func.
	fnStack []*core.Func

	// inTest marks lowering inside a `test` declaration's body —
	// where the generated function binds the `t` identifier
	// assert/assert_eq emit calls against.
	inTest bool

	// subst is a stack of symbol-substitution maps. Match-arm leaves
	// push the map renaming the arm's pattern variables to the fresh
	// temporaries the earned binds were lowered to, so a duplicated
	// arm body (or-patterns, wildcard expansion) stays well-scoped.
	subst []map[*resolve.Symbol]*resolve.Symbol

	// tailrec records the `@tailrec`-marked FnDecls checkTailRec
	// validates once the file's bodies (and TailCall marks) are
	// complete (tailrec.go).
	tailrec []tailrecMark

	diags []diag.Diagnostic
}

func (l *lowerer) tmp() *resolve.Symbol {
	l.tmpSeq++
	return &resolve.Symbol{Name: fmt.Sprintf("tmp%d", l.tmpSeq), Kind: resolve.KindVar}
}

// tmpBind makes a fresh temporary binder of the given type.
func (l *lowerer) tmpBind(sp diag.Span, typ types.Type) *core.Bind {
	return &core.Bind{Sp: sp, Sym: l.tmp(), Typ: typ}
}

func (l *lowerer) discardBind(sp diag.Span, typ types.Type) *core.Bind {
	return &core.Bind{Sp: sp, Typ: typ, Discard: true}
}

// namedBind binds a checker-named variable. Under an active
// substitution — inside a match arm, whose body may be lowered at
// several leaves (or-pattern alternatives, wildcard-expanded
// variants, guard continuations) — the checked symbol can appear as
// a binder only once file-wide: the second and later occurrences get
// a fresh symbol and the leaf's substitution routes references to it.
// Outside substitution a duplicate is a contract gap left for the
// validator to report.
func (l *lowerer) namedBind(sp diag.Span, sym *resolve.Symbol, typ types.Type) *core.Bind {
	if len(l.subst) > 0 && sym != nil && (l.boundSyms[sym] || l.substSym(sym) != sym) {
		fresh := l.tmp()
		l.subst[len(l.subst)-1][sym] = fresh
		return &core.Bind{Sp: sp, Sym: fresh, Typ: typ}
	}
	l.boundSyms[sym] = true
	return &core.Bind{Sp: sp, Sym: sym, Typ: typ}
}

// varOf makes a reference to a binder's symbol.
func varOf(sp diag.Span, b *core.Bind) *core.Var {
	return &core.Var{Base: core.Base{Sp: sp, Typ: b.Typ}, Sym: b.Sym}
}

// curFn is the innermost enclosing function boundary — the target of
// escape nodes; nil in top-level let initializers.
func (l *lowerer) curFn() *core.Func {
	if len(l.fnStack) == 0 {
		return nil
	}
	return l.fnStack[len(l.fnStack)-1]
}

func (l *lowerer) pushSubst(m map[*resolve.Symbol]*resolve.Symbol) {
	l.subst = append(l.subst, m)
}

func (l *lowerer) popSubst() {
	l.subst = l.subst[:len(l.subst)-1]
}

// substSym resolves a check-side variable symbol to the symbol its
// binder was actually lowered to — the identity outside match arms.
func (l *lowerer) substSym(sym *resolve.Symbol) *resolve.Symbol {
	for i := len(l.subst) - 1; i >= 0; i-- {
		if ns, ok := l.subst[i][sym]; ok {
			return ns
		}
	}
	return sym
}

func (l *lowerer) err(sp diag.Span, msg string) {
	l.diags = append(l.diags,
		diag.Error(codeLowering, msg).At(sp))
}

// funcArity is the declared arity of a Gom function symbol — its
// parameter-list length — falling back through the same-file table,
// the Env hook, and finally the flat arity of the reference type.
func (l *lowerer) funcArity(sym *resolve.Symbol, refTyp types.Type) int {
	if a, ok := l.arity[sym]; ok {
		return a
	}
	if l.env.FuncArity != nil {
		if a := l.env.FuncArity(sym); a >= 0 {
			return a
		}
	}
	return flatArity(refTyp)
}

// goSig returns the Go signature facts for a Go function or method —
// from Env, or derived from the Gom view of the reference type.
func (l *lowerer) goSig(sym *resolve.Symbol, refTyp types.Type) GoSig {
	if l.env.GoSig != nil {
		if s, ok := l.env.GoSig(sym); ok {
			return s
		}
	}
	return GoSig{Arity: flatArity(refTyp), Results: goResultsOf(refTyp)}
}

// flatArity is the *immediate* parameter count of a function type;
// 0 for non-function types. The type is chased first — a let-bound
// function value's type can still be a bound `?n`. A nested Ret is
// not unfolded: `Int -> (Int -> Int)` reports 1, matching the
// declared arity of `fn mk (a) -> (Int -> Int)` and the Go arity of
// `func(a) func(b)` — both are what the count is for. flatSig
// counts the whole chain.
func flatArity(t types.Type) int {
	if fn := sigOf(t); fn != nil {
		return len(fn.Params)
	}
	return 0
}

// goResultsOf derives a Go call's result shape from the Gom view of
// its result type — the contract gaps the Env.GoSig hook overrides:
// Result Unit GoError is read as GoResultsErrOnly, Option as a
// comma-ok (T, bool), a 2-tuple as a two-result call, everything else
// as a single result.
func goResultsOf(t types.Type) core.GoResults {
	if c, ok := types.Chase(t).(*types.Func); ok {
		t = c.Ret
	}
	c, ok := types.Chase(t).(*types.Con)
	if !ok {
		if tup, ok := types.Chase(t).(*types.Tuple); ok && len(tup.Elems) == 2 {
			return core.GoResultsPair
		}
		return core.GoResultsOne
	}
	switch {
	case c.Qual == "" && c.Name == "Result" && len(c.Args) == 2:
		if types.Equal(c.Args[0], types.Unit) {
			return core.GoResultsErrOnly
		}
		return core.GoResultsErr
	case c.Qual == "" && c.Name == "Option" && len(c.Args) == 1:
		return core.GoResultsOK
	}
	if tup, ok := types.Chase(t).(*types.Tuple); ok && len(tup.Elems) == 2 {
		return core.GoResultsPair
	}
	return core.GoResultsOne
}

// codeLowering is the diagnostic code lowering uses for contract
// gaps the checker cannot express — an input the typed AST claims is
// valid but that cannot be represented in Core IR. The "09" lowering
// category is not yet registered in the diag code table; see
// docs/problems/2026-11-02-lower-input-contract-gaps.md.
const codeLowering diag.Code = "E0901"

// coreViolation renders a validator violation as a diagnostic —
// compiler-bug evidence surfaced instead of a malformed file.
func coreViolation(v core.Violation) diag.Diagnostic {
	d := diag.Error(codeLowering, fmt.Sprintf("core invariant violation: %s", v.Msg)).
		Set("node", v.Node)
	if v.Span.Valid() {
		d = d.At(v.Span)
	}
	return d
}

// typeKey is the lookup key for a TypeDecl — the qualified type name
// (Qual is "" for the package being compiled).
func typeKey(sym *resolve.Symbol) string {
	return sym.Package + "/" + sym.Name
}

func conKey(c *types.Con) string {
	return c.Qual + "/" + c.Name
}

// isCon reports whether t is a *types.Con with the given
// qualification and name, chasing bound metas first — expression
// types the checker leaves behind are not always fully zonked.
func isCon(t types.Type, qual, name string) (*types.Con, bool) {
	c, ok := types.Chase(t).(*types.Con)
	if !ok || c.Qual != qual || c.Name != name {
		return nil, false
	}
	return c, true
}

func isResultCon(c *types.Con) bool { return c.Qual == "" && c.Name == "Result" }
func isOptionCon(c *types.Con) bool { return c.Qual == "" && c.Name == "Option" }

// resultArgs unpacks `Result a e`; ok is false for other shapes.
func resultArgs(t types.Type) (a, e types.Type, ok bool) {
	c, is := isCon(t, "", "Result")
	if !is || len(c.Args) != 2 {
		return nil, nil, false
	}
	return c.Args[0], c.Args[1], true
}

// typeName renders a type for a panic message — the qualified type
// name for nominal types, the surface form otherwise.
func typeName(t types.Type) string {
	t = types.Chase(t)
	if t == nil {
		return "<unknown>"
	}
	if c, ok := t.(*types.Con); ok {
		if c.Qual == "" {
			return c.Name
		}
		return c.Qual + "." + c.Name
	}
	return t.String()
}

// symKey keys the builtin-interning table — kind and name, so a
// synthesized constructor and a synthesized builtin call can never
// collide on a shared name.
type symKey struct {
	kind resolve.Kind
	name string
}

func (l *lowerer) builtin(key symKey) *resolve.Symbol {
	if l.builtins == nil {
		l.builtins = map[symKey]*resolve.Symbol{}
	}
	s, ok := l.builtins[key]
	if !ok {
		s = &resolve.Symbol{Name: key.name, Kind: key.kind}
		l.builtins[key] = s
	}
	return s
}

// ctorSym returns a shared symbol for a builtin constructor name —
// used when lowering synthesises a reference to `Ok`/`Some` that no
// pattern carried into this file.
func (l *lowerer) ctorSym(name string) *resolve.Symbol {
	return l.builtin(symKey{kind: resolve.KindCtor, name: name})
}

// builtinSym returns a shared symbol for a builtin call — the small
// runtime hooks lowering synthesises (string display/concat).
func (l *lowerer) builtinSym(name string) *resolve.Symbol {
	return l.builtin(symKey{kind: resolve.KindBuiltin, name: name})
}

// variantsOf returns the constructor set of a nominal type — builtin
// shapes first, then the file's own TypeDecls, then the Env oracle.
// A nil or foreign Con yields nil — the caller reports the gap.
func (l *lowerer) variantsOf(c *types.Con) []Variant {
	if c == nil {
		return nil
	}
	if isResultCon(c) && len(c.Args) == 2 {
		return []Variant{
			{Sym: l.ctorSym("Ok"), Fields: []check.TField{{Index: 0, Type: c.Args[0]}}},
			{Sym: l.ctorSym("Err"), Fields: []check.TField{{Index: 0, Type: c.Args[1]}}},
		}
	}
	if isOptionCon(c) && len(c.Args) == 1 {
		return []Variant{
			{Sym: l.ctorSym("Some"), Fields: []check.TField{{Index: 0, Type: c.Args[0]}}},
			{Sym: l.ctorSym("None")},
		}
	}
	if d, ok := l.typeD[conKey(c)]; ok {
		if s, ok := d.Body.(*check.SumType); ok {
			sub := types.Subst{}
			for i, tp := range d.TypeParams {
				if i < len(c.Args) && tp != nil {
					sub[tp.ID] = c.Args[i]
				}
			}
			out := make([]Variant, len(s.Variants))
			for i, v := range s.Variants {
				fields := make([]check.TField, len(v.Fields))
				for j, f := range v.Fields {
					fields[j] = f
					if len(sub) > 0 {
						fields[j].Type = sub.Apply(f.Type)
					}
				}
				out[i] = Variant{Sym: v.Sym, Fields: fields}
			}
			return out
		}
	}
	if l.env.Types != nil {
		return l.env.Types.Variants(c)
	}
	return nil
}

// fieldIndex resolves a record field's declaration index — from the
// file's own record TypeDecl or the Env oracle. -1 when unknown.
func (l *lowerer) fieldIndex(recTyp types.Type, sym *resolve.Symbol) int {
	if sym == nil {
		return -1
	}
	c, ok := types.Chase(recTyp).(*types.Con)
	if !ok {
		return -1
	}
	var fields []check.TField
	if d, ok := l.typeD[conKey(c)]; ok {
		if r, ok := d.Body.(*check.RecordType); ok {
			fields = r.Fields
		}
	}
	if fields == nil && l.env.Types != nil {
		fields = l.env.Types.Fields(c)
	}
	for i, f := range fields {
		if f.Sym == sym {
			return i
		}
	}
	return -1
}

// --- declarations ---

func (l *lowerer) decl(d check.Decl, out *core.File) {
	switch d := d.(type) {
	case *check.FnDecl:
		if !l.env.Tests && d.Sym != nil && d.Sym.TestOnly {
			// A `@test fn` is test-only code: checked but not
			// lowered under gom build (spec 11.6).
			return
		}
		sig := flatSig(d.Sig)
		if sig == nil {
			var ps []types.FuncParam
			for _, p := range d.Params {
				ps = append(ps, types.FuncParam{Type: p.Pat.Type()})
			}
			sig = flatSig(types.Fn(d.Body.Type(), ps...))
		}
		fn := &core.Func{Sp: d.Sp, Sig: sig}
		fn.Params = l.paramBinds(d.Params, fn)
		l.fnStack = append(l.fnStack, fn)
		s := &seq{}
		l.paramPrelude(d.Params, fn.Params, s)
		// Lower the tail before reading s.lets — composite literal
		// field order is too subtle to rely on here.
		tail := l.expr(s, d.Body)
		fn.Body = &core.Body{Lets: s.lets, Tail: tail}
		l.fnStack = l.fnStack[:len(l.fnStack)-1]
		l.markTailCalls(fn, d.Sym)
		if d.TailRec && d.Sym != nil {
			l.tailrec = append(l.tailrec, tailrecMark{sym: d.Sym, sp: tailrecNameSpan(d)})
		}
		out.Decls = append(out.Decls, &core.FuncDecl{
			Sp: d.Sp, Sym: d.Sym, Fn: fn,
			TypeParams: d.TypeParams,
			IsEntry:    isEntryPoint(out.Package, d.Sym, fn),
		})
	case *check.LetDecl:
		if !l.env.Tests && testOnlyPat(d.Pat) {
			// A `@test let` is test-only code (spec 11.6).
			return
		}
		l.letDecl(d, out)
	case *check.TypeDecl:
		out.Decls = append(out.Decls, &core.TypeDecl{
			Sp: d.Sp, Sym: d.Sym, TypeParams: d.TypeParams,
			Body: l.typeBody(d.Body),
		})
	case *check.TestDecl:
		if !l.env.Tests {
			// spec 11.6: gom build checks tests but emits no test
			// code — that is gom test's output.
			return
		}
		fn := &core.Func{
			Sp:  d.Sp,
			Sig: types.Fn(d.Body.Type()),
		}
		l.fnStack = append(l.fnStack, fn)
		l.inTest = true
		s := &seq{}
		tail := l.expr(s, d.Body)
		fn.Body = &core.Body{Lets: s.lets, Tail: tail}
		l.inTest = false
		l.fnStack = l.fnStack[:len(l.fnStack)-1]
		out.Decls = append(out.Decls, &core.TestDecl{
			Sp: d.Sp, Name: d.Name, NameSp: d.NameSp, Fn: fn,
		})
	default:
		panic("unreachable")
	}
}

// testOnlyPat reports whether a top-level let pattern binds a
// test-only symbol — a `@test let` definition.
func testOnlyPat(p check.Pat) bool {
	for _, v := range boundVars(p) {
		if v.sym != nil && v.sym.TestOnly {
			return true
		}
	}
	return false
}

func (l *lowerer) typeBody(b check.TypeBody) core.TypeBody {
	switch b := b.(type) {
	case *check.SumType:
		out := &core.TSum{Sp: b.Sp}
		for _, v := range b.Variants {
			out.Variants = append(out.Variants, core.TVariant{
				Sp: v.Sp, Sym: v.Sym, Fields: l.tfields(v.Fields),
			})
		}
		return out
	case *check.RecordType:
		return &core.TRecord{Sp: b.Sp, Fields: l.tfields(b.Fields)}
	case *check.AliasType:
		return &core.TAlias{Sp: b.Sp, Type: b.Type}
	case *check.AbstractType:
		return &core.TAbstract{Sp: b.Sp}
	default:
		panic("unreachable")
	}
}

func (l *lowerer) tfields(fs []check.TField) []core.TField {
	out := make([]core.TField, len(fs))
	for i, f := range fs {
		out[i] = core.TField{
			Sp: f.Sp, Name: f.Name, Label: f.Label, Index: f.Index,
			Sym: f.Sym, Type: f.Type,
		}
	}
	return out
}

// letDecl lowers a top-level `let pat = e`.
//
//   - `let x = e` becomes one VarDecl.
//   - a deep-irrefutable pattern (tuple/record/as/`[..rest]`
//     compositions, wildcard) binds a shared hidden temporary VarDecl
//     and one VarDecl per bound variable initialised by pure
//     projection.
//   - a tested pattern — a single-variant constructor pattern is the
//     only one the pattern checker should admit, but refutable
//     patterns are still meaningful — compiles to a match on the
//     shared temporary whose leaf yields the bound variables as a
//     tuple (or the single variable, or Unit when nothing binds).
//     One VarDecl per bound variable then projects the tuple.
//
// This replaces the earlier scheme that projected every pattern with
// fabricated FieldGet nodes; patterns that need a test can only be
// lowered through the decision tree.
func (l *lowerer) letDecl(d *check.LetDecl, out *core.File) {
	if v, ok := d.Pat.(*check.VarPat); ok {
		out.Decls = append(out.Decls, &core.VarDecl{
			Sp: d.Sp, Sym: v.Sym, Body: l.exprBody(d.Value),
		})
		return
	}
	// Every other pattern shares a hidden package-level temporary.
	tmpSym := l.tmp()
	out.Decls = append(out.Decls, &core.VarDecl{
		Sp: d.Sp, Sym: tmpSym, Body: l.exprBody(d.Value),
	})
	tmpVar := func() core.Atom {
		return &core.Var{Base: core.Base{Sp: d.Sp, Typ: d.Value.Type()}, Sym: tmpSym}
	}
	if _, ok := d.Pat.(*check.WildPat); ok {
		return
	}
	if deepPure(d.Pat) {
		for _, b := range l.pureBinds(d.Pat, tmpVar()) {
			out.Decls = append(out.Decls, &core.VarDecl{
				Sp:   b.sp,
				Sym:  b.sym,
				Body: &core.Body{Tail: b.atom},
			})
		}
		return
	}
	// Tested pattern: a match yielding the bound variables.
	vars := boundVars(d.Pat)
	resT := boundVarsType(vars)
	states := []*armState{{
		sp:   d.Pat.Span(),
		todo: []oblig{{subj: tmpVar(), pat: d.Pat}},
		leaf: boundVarsLeaf(vars),
	}}
	s := &seq{}
	tree := l.compile(states, tmpVar(), s, resT)
	body := &core.Body{Lets: s.lets, Tail: &core.Match{
		Base: core.Base{Sp: d.Pat.Span(), Typ: resT}, Tree: tree,
	}}
	if dl, ok := tree.(*core.DLeaf); ok {
		body.Lets = append(body.Lets, dl.Body.Lets...)
		body.Tail = dl.Body.Tail
	}
	switch len(vars) {
	case 0:
		out.Decls = append(out.Decls, &core.VarDecl{
			Sp: d.Sp, Sym: l.tmp(), Body: body,
		})
	case 1:
		out.Decls = append(out.Decls, &core.VarDecl{
			Sp: d.Sp, Sym: vars[0].sym, Body: body,
		})
	default:
		tupSym := l.tmp()
		out.Decls = append(out.Decls, &core.VarDecl{
			Sp: d.Sp, Sym: tupSym, Body: body,
		})
		for i, v := range vars {
			out.Decls = append(out.Decls, &core.VarDecl{
				Sp:  d.Sp,
				Sym: v.sym,
				Body: &core.Body{Tail: &core.FieldGet{
					Base:  core.Base{Sp: d.Sp, Typ: v.typ},
					Recv:  &core.Var{Base: core.Base{Sp: d.Sp, Typ: resT}, Sym: tupSym},
					Index: i,
				}},
			})
		}
	}
}

// paramBinds lowers a parameter list to Core binders. Destructuring
// parameters bind a fresh temporary; the destructure is folded into
// the function body's head by paramPrelude.
func (l *lowerer) paramBinds(params []check.Param, fn *core.Func) []*core.Bind {
	out := make([]*core.Bind, len(params))
	for i, p := range params {
		pt := paramType(fn.Sig, i, p.Pat.Type())
		switch pat := p.Pat.(type) {
		case *check.VarPat:
			out[i] = l.namedBind(pat.Sp, pat.Sym, pt)
		case *check.WildPat:
			out[i] = l.discardBind(pat.Sp, pt)
		case *check.LitPat:
			// `()`-style literal parameter — discards its Unit value.
			out[i] = l.discardBind(pat.Sp, pt)
		case *check.AsPat, *check.CtorPat, *check.ListPat, *check.OrPat, *check.RecordPat, *check.TuplePat:
			out[i] = l.tmpBind(pat.Span(), pt)
		default:
			panic("unreachable")
		}
	}
	return out
}

// paramPrelude emits the destructuring work for non-trivial parameter
// patterns into the head of the function body's sequence.
func (l *lowerer) paramPrelude(params []check.Param, binds []*core.Bind, s *seq) {
	for i, p := range params {
		switch p.Pat.(type) {
		case *check.VarPat, *check.WildPat, *check.LitPat:
		case *check.AsPat, *check.CtorPat, *check.ListPat, *check.OrPat, *check.RecordPat, *check.TuplePat:
			l.destructure(s, p.Pat, varOf(p.Pat.Span(), binds[i]))
		default:
			panic("unreachable")
		}
	}
}

func paramType(sig *types.Func, i int, fallback types.Type) types.Type {
	if sig != nil && i < len(sig.Params) {
		return sig.Params[i].Type
	}
	return fallback
}
