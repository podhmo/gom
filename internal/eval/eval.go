package eval

import (
	"fmt"
	"sort"
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Result is the outcome of an evaluation: the value produced, plus
// the diagnostics raised. A nil Value with non-empty Diags means a
// runtime failure — Go panics are never used for Gom-level errors.
type Result struct {
	Value Value
	Diags []diag.Diagnostic
}

// OK reports whether the evaluation produced a value without errors.
func (r Result) OK() bool { return r.Value != nil && !diag.List(r.Diags).HasError() }

// defaultMaxDepth is the built-in call-depth bound: eval recursion
// is Go-stack recursion, and the goroutine stack ceiling (~1 GB) is
// a fatal, unrecoverable death — `f x = f x` reaches it around
// ~300K nested calls, well before a throughput-sized step budget
// would trip. 50K frames stays an order of magnitude below the
// measured ceiling while leaving generous headroom for legitimate
// recursion (stdlib list ops are Go-side and do not consume this
// budget at all). Evaluator.SetMaxDepth overrides it.
const defaultMaxDepth = 50_000

// interp carries one evaluation's mutable state.
type interp struct {
	ctx *ctx
	// steps/maxSteps implement the evaluator's fuel budget
	// (Evaluator.SetMaxSteps): every expression node evaluated costs
	// one step, so a diverging program — unbounded recursion, a long
	// for loop — is cut off with a diagnostic instead of growing the
	// Go stack without limit. 0 means unlimited, the default.
	steps    int64
	maxSteps int64
	// depth/maxDepth bound nested function calls — the other half of
	// the resource story. A step budget alone cannot catch unbounded
	// recursion early enough: the Go stack dies first. Each
	// saturated invoke counts one frame against maxDepth; crossing
	// it reports the same E9006 class as fuel exhaustion.
	// defaultMaxDepth applies when unset; <= 0 means unlimited.
	depth    int64
	maxDepth int64
	// localT names the file's declared types — an unqualified
	// *types.Con carrying one of these names denotes the file-local
	// declaration, never the builtin of the same spelling. It is
	// the same guard check's shadowedBuiltin, lower's declaredT and
	// codegen's localT apply: a file-local `type Result` makes
	// IsResult answer false for its variants.
	localT map[string]bool
}

// letInit is the lazy initializer of one top-level `let`
// declaration: the compiled form is a Go package variable, whose
// initialization order follows dependencies rather than source order
// (`let y = z + 1; let z = 40` initializes z first and y = 41). The
// evaluator matches that by computing the value on first demand —
// either when Run reaches the declaration or when evalVar reads one
// of its bound names. busy marks an in-progress initializer so a
// re-entrant read reports an initialization cycle instead of
// diverging; done means the pattern has been bound into the global
// environment already.
type letInit struct {
	decl *check.LetDecl
	done bool
	busy bool
}

// --- public API ---

// Evaluator evaluates one typed file: it registers type declarations
// and binds top-level functions into a global environment at New;
// Run then evaluates the value declarations, tests, and `main`.
type Evaluator struct {
	env    *Env
	file   *check.File
	interp *interp
}

// New prepares evaluation of f: registers type declarations and binds
// every top-level function symbol to its (recursion-ready) closure.
// LetDecl values are not evaluated until Run.
func New(f *check.File) *Evaluator {
	env := NewEnv()
	ev := &Evaluator{env: env, file: f, interp: &interp{ctx: env.ctx, maxDepth: defaultMaxDepth}}
	if f == nil {
		return ev
	}
	for _, d := range f.Decls {
		td, ok := d.(*check.TypeDecl)
		if !ok {
			continue
		}
		ev.registerTypeDecl(td)
		if td.Sym != nil && td.Sym.Package == "" {
			if ev.interp.localT == nil {
				ev.interp.localT = map[string]bool{}
			}
			ev.interp.localT[td.Sym.Name] = true
		}
	}
	for _, d := range f.Decls {
		fd, ok := d.(*check.FnDecl)
		if !ok || fd.Body == nil {
			// A bodiless declaration (an interface-level signature
			// that slipped into a .gom file) stays unbound — a use
			// will report "unbound symbol".
			continue
		}
		env.Bind(fd.Sym, ev.fnValue(fd))
	}
	// Top-level lets stay unbound until Run (or a first read through
	// evalVar) initializes them — the compiled form is a package var,
	// whose initialization follows dependencies, not source order.
	for _, d := range f.Decls {
		ld, ok := d.(*check.LetDecl)
		if !ok {
			continue
		}
		init := &letInit{decl: ld}
		for _, sym := range boundSyms(ld.Pat) {
			env.ctx.topLets[sym] = init
		}
	}
	return ev
}

// Env returns the evaluator's global environment — bind host
// functions and stub module members here before Run or Expr.
func (e *Evaluator) Env() *Env { return e.env }

// SetMaxSteps bounds how many expression nodes one evaluation may
// reduce before it stops with an E9006 diagnostic — the fuel budget
// the differential harness uses to keep generated programs from
// running forever (a compiled binary gets a wall-clock timeout; a
// tree-walking evaluator needs a structural bound instead, since a
// leaked goroutine cannot be killed). n <= 0 restores the default,
// unlimited.
func (e *Evaluator) SetMaxSteps(n int64) { e.interp.maxSteps = n }

// SetMaxDepth bounds how deeply function calls may nest before the
// evaluation stops with an E9006 diagnostic — the depth analogue of
// SetMaxSteps. Eval recursion is Go-stack recursion, so an
// unbounded recursive program hits the fatal goroutine stack
// ceiling long before a step budget sized for throughput trips;
// every Evaluator starts at defaultMaxDepth so that death is
// unreachable. n <= 0 removes the bound — only for callers that can
// prove the program terminates.
func (e *Evaluator) SetMaxDepth(n int64) { e.interp.maxDepth = n }

// Global returns the value bound to the top-level symbol named name,
// or nil.
func (e *Evaluator) Global(name string) Value {
	for sym, v := range e.env.vars {
		if sym.Name == name {
			return v
		}
	}
	return nil
}

// Run evaluates the file's top-level LetDecls in order, runs each
// `test` block as a function boundary, and finally applies `main` to
// `()` when a function named main exists. The first failure stops the
// run.
func (e *Evaluator) Run() Result {
	var diags diag.List
	for _, d := range e.fileDecls() {
		switch d := d.(type) {
		case *check.LetDecl:
			_, ds := runEval(func() Value {
				e.interp.ensureTopLet(d, e.env)
				return nil
			})
			diags = append(diags, ds...)
			if ds.HasError() {
				return Result{Diags: diags}
			}
		case *check.TestDecl:
			v, ds := runEval(func() Value {
				return runBody(func() Value { return e.interp.eval(d.Body, e.env.Child()) })
			})
			if !ds.HasError() && v != nil {
				if isRes, isOk := e.interp.resultTag(v); isRes && !isOk {
					ds = append(ds, diag.Error(CodeTestFail, "test returned Err").
						At(d.Sp).Set("test", d.Name).Set("value", v.String()))
				}
			}
			diags = append(diags, ds...)
			if ds.HasError() {
				return Result{Diags: diags}
			}
		case *check.FnDecl, *check.TypeDecl:
			// Handled by New.
		default:
			panic("unreachable")
		}
	}
	if g := e.Global("main"); g != nil {
		v, ds := runEval(func() Value {
			return e.interp.applyOne(g, &VUnit{}, e.sp())
		})
		diags = append(diags, ds...)
		return Result{Value: v, Diags: diags}
	}
	return Result{Value: &VUnit{}, Diags: diags}
}

// Expr evaluates x in the evaluator's global environment.
func (e *Evaluator) Expr(x check.Expr) Result {
	v, ds := runEval(func() Value { return e.interp.eval(x, e.env) })
	return Result{Value: v, Diags: ds}
}

// Apply invokes a function value with positional arguments —
// curried: extra calls return partial VFuncs.
func (e *Evaluator) Apply(fn Value, args ...Value) Result {
	v, ds := runEval(func() Value {
		for _, a := range args {
			fn = e.interp.applyOne(fn, a, e.sp())
		}
		return fn
	})
	return Result{Value: v, Diags: ds}
}

// Program evaluates a whole typed file — New(f).Run().
func Program(f *check.File) Result {
	return New(f).Run()
}

// Expr evaluates a standalone expression against env (NewEnv() gives
// an empty global scope; use Env.Bind to provide values).
func Expr(env *Env, x check.Expr) Result {
	if env == nil {
		env = NewEnv()
	}
	it := &interp{ctx: env.ctx, maxDepth: defaultMaxDepth}
	v, ds := runEval(func() Value { return it.eval(x, env) })
	return Result{Value: v, Diags: ds}
}

// fileDecls returns f.Decls or nil for a nil file.
func (e *Evaluator) fileDecls() []check.Decl {
	if e.file == nil {
		return nil
	}
	return e.file.Decls
}

// sp returns the file's span for diagnostics, or the zero span when
// the evaluator was built with New(nil).
func (e *Evaluator) sp() diag.Span {
	if e.file == nil {
		return diag.Span{}
	}
	return e.file.Sp
}

// fnValue builds the function value of a top-level FnDecl.
func (e *Evaluator) fnValue(d *check.FnDecl) *VFunc {
	sig := d.Sig
	if sig == nil {
		sig = &types.Func{Ret: d.Body.Type()}
		for range d.Params {
			sig.Params = append(sig.Params, types.P(types.Unit))
		}
	}
	if len(sig.Params) != len(d.Params) {
		// A signature arity that disagrees with the parameter list is
		// a broken checker invariant.
		sig = &types.Func{Ret: sig.Ret, Params: make([]types.FuncParam, len(d.Params))}
		for i, p := range d.Params {
			sig.Params[i] = types.FuncParam{Label: p.Label, Type: types.Unit}
		}
	}
	f := funcOfType(d.Sym.Name, fnLambda, sig)
	f.params = d.Params
	f.body = d.Body
	f.env = e.env
	f.thunk = d.Thunk
	return f
}

// registerTypeDecl records the constructor/field metadata of one
// type declaration.
func (e *Evaluator) registerTypeDecl(d *check.TypeDecl) {
	con := &types.Con{Qual: d.Sym.Package, Name: d.Sym.Name}
	for _, tp := range d.TypeParams {
		con.Args = append(con.Args, tp)
	}
	switch body := d.Body.(type) {
	case *check.SumType:
		for _, variant := range body.Variants {
			fields := make([]fieldInfo, len(variant.Fields))
			for i, tf := range variant.Fields {
				fields[i] = fieldInfo{name: tf.Name, label: tf.Label, sym: tf.Sym, index: tf.Index}
			}
			e.interp.registerCtor(&ctorInfo{
				sym: variant.Sym, name: variant.Sym.Name, typ: con, fields: fields,
			})
		}
	case *check.RecordType:
		fields := make([]fieldInfo, len(body.Fields))
		for i, tf := range body.Fields {
			fields[i] = fieldInfo{name: tf.Name, label: tf.Label, sym: tf.Sym, index: tf.Index}
		}
		e.interp.ctx.records[d.Sym] = &recordInfo{sym: d.Sym, typ: con, fields: fields}
	case *check.AliasType, *check.AbstractType:
		// no runtime footprint
	default:
		panic("unreachable")
	}
}

// resultTag decomposes a Result value: (isResult, isOk).
func (e *interp) resultTag(v Value) (isResult, isOk bool) {
	variant, ok := v.(*VVariant)
	if !ok {
		return false, false
	}
	return variant.IsResult(e.localT)
}

// --- expression dispatch ---

func (e *interp) eval(x check.Expr, env *Env) Value {
	if e.maxSteps > 0 {
		e.steps++
		if e.steps > e.maxSteps {
			failData(CodeBudget, "evaluation step budget exhausted", x.Span(),
				"budget", e.maxSteps)
		}
	}
	switch x := x.(type) {
	case *check.Lit:
		return litValue(x.Kind, x.Text, x.Span())
	case *check.Var:
		return e.evalVar(x, env)
	case *check.Apply:
		return e.evalApply(x, env)
	case *check.Hole:
		failData(CodeInternal, "misplaced _ placeholder", x.Span(), "kind", "bare")
	case *check.MemberExpr:
		return e.evalMember(x, env)
	case *check.Unary:
		return e.evalUnary(x, env)
	case *check.Binary:
		return e.evalBinary(x, env)
	case *check.StrInterp:
		var sb strings.Builder
		for _, part := range x.Parts {
			switch part := part.(type) {
			case *check.StrText:
				sb.WriteString(unescape(part.Text, part.Sp))
			case *check.StrExpr:
				sb.WriteString(display(e.eval(part.X, env)))
			default:
				panic("unreachable")
			}
		}
		return &VStr{S: sb.String()}
	case *check.Lambda:
		sig, _ := x.Type().(*types.Func)
		f := funcOfType("", fnLambda, sig)
		f.params = x.Params
		f.body = x.Body
		f.env = env
		return f
	case *check.If:
		c := e.eval(x.Cond, env)
		b, ok := c.(*VBool)
		if !ok {
			failData(CodeInternal, "if condition is not Bool", x.Cond.Span(), "value", c.String())
		}
		if b.B {
			return e.eval(x.Then, env)
		}
		if x.Else == nil {
			return &VUnit{}
		}
		return e.eval(x.Else, env)
	case *check.Match:
		return e.evalMatch(x, env)
	case *check.Block:
		child := env.Child()
		var v Value = &VUnit{}
		for _, s := range x.Stmts {
			v = e.eval(s, child)
		}
		return v
	case *check.Let:
		v := e.eval(x.Value, env)
		if !e.matchPat(x.Pat, v, env) {
			failData(CodeInternal, "irrefutable let pattern failed", x.Pat.Span(), "value", v.String())
		}
		return &VUnit{}
	case *check.Ignore:
		e.eval(x.X, env)
		return &VUnit{}
	case *check.Try:
		v := e.eval(x.X, env)
		variant, ok := v.(*VVariant)
		if !ok {
			failData(CodeInternal, "try applied to a non-Result value", x.Span(), "value", v.String())
		}
		isRes, isOk := variant.IsResult(e.localT)
		if !isRes {
			failData(CodeInternal, "try applied to a non-Result value", x.Span(), "value", v.String())
		}
		if !isOk {
			panic(escape{v: v, sp: x.Span()})
		}
		if len(variant.Fields) != 1 {
			failData(CodeInternal, "malformed Ok value", x.Span(), "value", v.String())
		}
		return variant.Fields[0].V
	case *check.GoExpr:
		failData(CodeUnsupported, "go expressions are not supported by the evaluator", x.Span(), "construct", "go")
	case *check.For:
		return e.evalFor(x, env)
	case *check.SelectExpr:
		failData(CodeUnsupported, "select expressions are not supported by the evaluator", x.Span(), "construct", "select")
	case *check.ListLit:
		var elems []Value
		for _, el := range x.Elems {
			if sp, isSpread := el.(*check.Spread); isSpread {
				v := e.eval(sp.X, env)
				tail, ok := v.(*VList)
				if !ok {
					failData(CodeInternal, "spread of a non-list", el.Span(), "value", v.String())
				}
				elems = append(elems, tail.Elems...)
				continue
			}
			elems = append(elems, e.eval(el, env))
		}
		return &VList{Elems: elems}
	case *check.Spread:
		failData(CodeUnsupported, "spread outside a list literal or Go call", x.Span(), "construct", "spread")
	case *check.TupleLit:
		elems := make([]Value, len(x.Elems))
		for i, el := range x.Elems {
			elems[i] = e.eval(el, env)
		}
		return &VTuple{Elems: elems}
	case *check.RecordLit:
		return e.evalRecord(x, env)
	case *check.Ascribe:
		if x.Conv {
			failData(CodeUnsupported, "Go.conv conversions are not supported by the evaluator", x.Span(), "construct", "Go.conv")
		}
		return e.eval(x.X, env)
	default:
		panic("unreachable")
	}
	panic("unreachable")
}

// forceThunk applies a generalized value-let — a top-level `let x =
// <value>` the checker emitted as a parameterless `() -> T` FnDecl
// (P2-1) — yielding the bound value; generated code makes the same
// shape a thunk call `x[T]()`. Any other value passes through —
// including a declared `fn f () -> T`, whose value is the function.
func (e *interp) forceThunk(v Value, sp diag.Span) Value {
	f, ok := v.(*VFunc)
	if !ok || !f.thunk {
		return v
	}
	return e.applyArgs(f, nil, nil, sp)
}

// evalVar resolves a symbol reference: environment first (locals,
// globals, harness stubs), then the lazy initializer of a top-level
// let, then constructors and builtins.
func (e *interp) evalVar(x *check.Var, env *Env) Value {
	if v, ok := env.Lookup(x.Sym); ok {
		return e.forceThunk(v, x.Span())
	}
	if init := e.ctx.topLets[x.Sym]; init != nil {
		// A top-level let not yet bound: force its initializer —
		// the package var it compiles to is initialized before its
		// first dependent's use, not in source order.
		e.forceTopLet(init, env)
		if v, ok := env.Lookup(x.Sym); ok {
			return v
		}
	}
	switch x.Sym.Kind {
	case resolve.KindCtor:
		info := e.ctorOf(x.Sym, x.Sym.Name, x.Type())
		if info == nil {
			failData(CodeInternal, "cannot determine constructor type", x.Span(), "ctor", x.Sym.Name)
		}
		if len(info.fields) == 0 {
			return &VVariant{Type: info.typ, Ctor: info.sym, Name: info.name}
		}
		f := funcOfType(info.name, fnCtor, ctorSig(info))
		f.ctor = info
		return f
	case resolve.KindBuiltin:
		if f := builtinByName(x.Sym.Name, x.Type()); f != nil {
			return f
		}
		failData(CodeUnsupported, "unknown builtin", x.Span(), "name", x.Sym.Name)
	case resolve.KindModule:
		failData(CodeUnsupported, "module in expression position", x.Span(), "name", x.Sym.Name)
	default:
		if x.Sym.IsGo || x.Sym.Package != "" {
			failData(CodeUnsupported,
				"Go or module symbol not bound — provide a host function via Env.Bind", x.Span(),
				"name", x.Sym.Name)
		}
		failData(CodeInternal, "unbound symbol", x.Span(), "name", x.Sym.Name)
	}
	panic("unreachable")
}

// ensureTopLet initializes a top-level let declaration exactly once.
// Run drives declarations in source order while evalVar forces them
// on first read, so a declaration may already be done when reached
// here. A pattern binding no symbols (`let _ = e`) can never be
// referenced and is evaluated straight away for effect.
func (e *interp) ensureTopLet(d *check.LetDecl, env *Env) {
	for _, sym := range boundSyms(d.Pat) {
		if init := e.ctx.topLets[sym]; init != nil {
			e.forceTopLet(init, env)
			return
		}
	}
	v := e.eval(d.Value, env)
	if !e.matchPat(d.Pat, v, env) {
		failData(CodeInternal, "irrefutable let pattern failed", d.Pat.Span(), "value", v.String())
	}
}

// forceTopLet evaluates a top-level let's initializer once and binds
// its pattern into the global (root) environment — the frame the
// package variable it compiles to would live in. Dependencies on
// other pending lets resolve by the same lazy read, matching Go's
// dependency-ordered package initialization; a re-entrant read is an
// initialization cycle, which a compiled program could not contain
// either (Go rejects `var a = b; var b = a` at compile time).
func (e *interp) forceTopLet(init *letInit, env *Env) {
	if init.done {
		return
	}
	if init.busy {
		name := ""
		if syms := boundSyms(init.decl.Pat); len(syms) > 0 {
			name = syms[0].Name
		}
		failData(CodePanic, "initialization cycle through top-level let", init.decl.Sp,
			"name", name)
	}
	root := rootEnv(env)
	init.busy = true
	defer func() { init.busy = false }()
	v := e.eval(init.decl.Value, root)
	if !e.matchPat(init.decl.Pat, v, root) {
		failData(CodeInternal, "irrefutable let pattern failed", init.decl.Pat.Span(), "value", v.String())
	}
	init.done = true
}

// rootEnv returns the outermost frame of env — the global scope
// where top-level declarations live.
func rootEnv(env *Env) *Env {
	for env.parent != nil {
		env = env.parent
	}
	return env
}

// evalMember handles `recv.sel`: record/variant field access, module
// member selection (resolved through the environment so the harness
// can stub it), or a Go method selection (unsupported).
func (e *interp) evalMember(x *check.MemberExpr, env *Env) Value {
	switch x.Sym.Kind {
	case resolve.KindField:
		recv := e.eval(x.Recv, env)
		switch r := recv.(type) {
		case *VRecord:
			if x.Index >= 0 {
				if f, ok := r.FieldByIndex(x.Index); ok {
					return f.V
				}
			}
			if f, ok := r.FieldBySym(x.Sym, x.Sel.Name); ok {
				return f.V
			}
			failData(CodeInternal, "record has no such field", x.Sel.Sp,
				"field", x.Sel.Name)
		case *VVariant:
			if x.Index >= 0 {
				if f, ok := r.FieldByIndex(x.Index); ok {
					return f.V
				}
			}
			for _, f := range r.Fields {
				if f.Sym != nil && f.Sym == x.Sym {
					return f.V
				}
			}
			for _, f := range r.Fields {
				if f.Name == x.Sel.Name {
					return f.V
				}
			}
			failData(CodeInternal, "variant has no such field", x.Sel.Sp,
				"field", x.Sel.Name)
		default:
			failData(CodeInternal, "field access on a non-record value", x.Recv.Span(),
				"value", recv.String())
		}
	case resolve.KindMethod:
		failData(CodeUnsupported, "Go method calls are not supported by the evaluator",
			x.Span(), "method", x.Sym.Name)
	default:
		// Module member selection (`pkg.Fn`): the member symbol's
		// value comes from the environment so tests can stub it.
		if v, ok := env.Lookup(x.Sym); ok {
			return e.forceThunk(v, x.Sel.Sp)
		}
		if x.Sym.Kind == resolve.KindCtor {
			info := e.ctorOf(x.Sym, x.Sym.Name, x.Type())
			if info != nil {
				if len(info.fields) == 0 {
					return &VVariant{Type: info.typ, Ctor: info.sym, Name: info.name}
				}
				f := funcOfType(info.name, fnCtor, ctorSig(info))
				f.ctor = info
				return f
			}
		}
		failData(CodeUnsupported,
			"module member not bound — provide a host value via Env.Bind", x.Sel.Sp,
			"name", x.Sym.Name)
	}
	panic("unreachable")
}

// evalUnary handles `-x` and `!x`.
func (e *interp) evalUnary(x *check.Unary, env *Env) Value {
	v := e.eval(x.X, env)
	switch x.Op {
	case syntax.UnNeg:
		switch v := v.(type) {
		case *VInt:
			return &VInt{N: -v.N}
		case *VFloat:
			return &VFloat{F: -v.F}
		default:
			failData(CodeInternal, "negation of a non-number", x.Span(), "value", v.String())
		}
	case syntax.UnNot:
		if b, ok := v.(*VBool); ok {
			return &VBool{B: !b.B}
		}
		failData(CodeInternal, "! applied to a non-Bool", x.Span(), "value", v.String())
	default:
		failData(CodeInternal, "unknown unary operator", x.Span(), "op", int(x.Op))
	}
	panic("unreachable")
}

// evalBinary handles primitive and sugared binary operators.
func (e *interp) evalBinary(x *check.Binary, env *Env) Value {
	switch x.Op {
	case syntax.BinAnd:
		l := e.eval(x.X, env)
		if b, ok := l.(*VBool); !ok || !b.B {
			if !ok {
				failData(CodeInternal, "&& on a non-Bool", x.X.Span(), "value", l.String())
			}
			return &VBool{}
		}
		return e.boolOf(e.eval(x.Y, env), x.Y.Span())
	case syntax.BinOr:
		l := e.eval(x.X, env)
		if b, ok := l.(*VBool); !ok || b.B {
			if !ok {
				failData(CodeInternal, "|| on a non-Bool", x.X.Span(), "value", l.String())
			}
			return &VBool{B: true}
		}
		return e.boolOf(e.eval(x.Y, env), x.Y.Span())
	case syntax.BinPipeFwd:
		arg := e.eval(x.X, env)
		f := e.eval(x.Y, env)
		return e.applyOne(f, arg, x.Span())
	case syntax.BinPipeBack:
		f := e.eval(x.X, env)
		arg := e.eval(x.Y, env)
		return e.applyOne(f, arg, x.Span())
	case syntax.BinComposeFwd, syntax.BinComposeBack:
		first := e.eval(x.X, env)
		second := e.eval(x.Y, env)
		sig, _ := x.Type().(*types.Func)
		f := funcOfType("", fnCompose, sig)
		if x.Op == syntax.BinComposeFwd {
			f.compose = [2]Value{first, second}
		} else {
			f.compose = [2]Value{second, first}
		}
		return f
	case syntax.BinPipeTry:
		return e.evalPipeTry(x, env)
	default:
		return e.evalPrim(x, env)
	}
}

// evalPrim evaluates arithmetic, comparison and equality operators.
// Both operands evaluate left-to-right before the operator runs.
func (e *interp) evalPrim(x *check.Binary, env *Env) Value {
	l := e.eval(x.X, env)
	r := e.eval(x.Y, env)
	switch x.Op {
	case syntax.BinEq:
		return &VBool{B: e.eqValue(l, r, x.Span())}
	case syntax.BinNe:
		return &VBool{B: !e.eqValue(l, r, x.Span())}
	}
	switch l := l.(type) {
	case *VInt:
		r, ok := r.(*VInt)
		if !ok {
			e.binMismatch(x, l, r)
		}
		switch x.Op {
		case syntax.BinAdd:
			return &VInt{N: l.N + r.N}
		case syntax.BinSub:
			return &VInt{N: l.N - r.N}
		case syntax.BinMul:
			return &VInt{N: l.N * r.N}
		case syntax.BinDiv:
			if r.N == 0 {
				failData(CodePanic, "integer division by zero", x.Span(), "op", "/")
			}
			return &VInt{N: l.N / r.N}
		case syntax.BinMod:
			if r.N == 0 {
				failData(CodePanic, "integer modulo by zero", x.Span(), "op", "%")
			}
			return &VInt{N: l.N % r.N}
		case syntax.BinLt:
			return &VBool{B: l.N < r.N}
		case syntax.BinLe:
			return &VBool{B: l.N <= r.N}
		case syntax.BinGt:
			return &VBool{B: l.N > r.N}
		case syntax.BinGe:
			return &VBool{B: l.N >= r.N}
		}
	case *VFloat:
		r, ok := r.(*VFloat)
		if !ok {
			e.binMismatch(x, l, r)
		}
		switch x.Op {
		case syntax.BinAdd:
			return &VFloat{F: l.F + r.F}
		case syntax.BinSub:
			return &VFloat{F: l.F - r.F}
		case syntax.BinMul:
			return &VFloat{F: l.F * r.F}
		case syntax.BinDiv:
			return &VFloat{F: l.F / r.F}
		case syntax.BinLt:
			return &VBool{B: l.F < r.F}
		case syntax.BinLe:
			return &VBool{B: l.F <= r.F}
		case syntax.BinGt:
			return &VBool{B: l.F > r.F}
		case syntax.BinGe:
			return &VBool{B: l.F >= r.F}
		}
	case *VChar:
		r, ok := r.(*VChar)
		if !ok {
			e.binMismatch(x, l, r)
		}
		switch x.Op {
		case syntax.BinLt:
			return &VBool{B: l.R < r.R}
		case syntax.BinLe:
			return &VBool{B: l.R <= r.R}
		case syntax.BinGt:
			return &VBool{B: l.R > r.R}
		case syntax.BinGe:
			return &VBool{B: l.R >= r.R}
		}
	case *VStr:
		r, ok := r.(*VStr)
		if !ok {
			e.binMismatch(x, l, r)
		}
		switch x.Op {
		case syntax.BinLt:
			return &VBool{B: l.S < r.S}
		case syntax.BinLe:
			return &VBool{B: l.S <= r.S}
		case syntax.BinGt:
			return &VBool{B: l.S > r.S}
		case syntax.BinGe:
			return &VBool{B: l.S >= r.S}
		}
	}
	failData(CodeInternal, "operator not defined on these operands", x.Span(),
		"op", x.Op.String())
	panic("unreachable")
}

func (e *interp) binMismatch(x *check.Binary, l, r Value) {
	failData(CodeInternal, "binary operands have mismatched value kinds", x.Span(),
		"op", x.Op.String())
}

// boolOf asserts a Bool value.
func (e *interp) boolOf(v Value, sp diag.Span) Value {
	if _, ok := v.(*VBool); ok {
		return v
	}
	failData(CodeInternal, "operand is not Bool", sp, "value", v.String())
	panic("unreachable")
}

// evalMatch runs arms in order: pattern bindings then the optional
// guard, source order throughout.
func (e *interp) evalMatch(x *check.Match, env *Env) Value {
	subj := e.eval(x.Subj, env)
	for _, arm := range x.Arms {
		child := env.Child()
		if !e.matchPat(arm.Pat, subj, child) {
			continue
		}
		if arm.Guard != nil {
			g := e.eval(arm.Guard, child)
			b, ok := g.(*VBool)
			if !ok {
				failData(CodeInternal, "match guard is not Bool", arm.Guard.Span(),
					"value", g.String())
			}
			if !b.B {
				continue
			}
		}
		return e.eval(arm.Body, child)
	}
	failData(CodeMatchFail, "no match arm accepted the value", x.Span(),
		"value", subj.String())
	panic("unreachable")
}

// evalPipeTry implements `r |?> f` (spec section 9.2): an incoming
// Err passes through untouched and the step is never evaluated; an Ok
// (or a non-Result first value, which spec 9.2 item 7 treats as Ok)
// feeds the step — and when the pipe's own error type is GoError the
// step's failure is wrapped once with the step name.
func (e *interp) evalPipeTry(x *check.Binary, env *Env) Value {
	l := e.eval(x.X, env)
	isRes, isOk := e.resultTag(l)
	if isRes && !isOk {
		// Err e |?> f: pass through untouched; f does not evaluate
		// (spec 9.2 items 1 and 5; the evaluation-order decision has
		// the step run only inside the Ok arm).
		return l
	}
	var arg Value
	if isRes {
		variant := l.(*VVariant)
		if len(variant.Fields) != 1 {
			failData(CodeInternal, "malformed Ok value", x.Span(), "value", l.String())
		}
		arg = variant.Fields[0].V
	} else {
		arg = l
	}
	f := e.eval(x.Y, env)
	r := e.applyOne(f, arg, x.Span())
	// The GoError auto-wrap applies at the step that produced the
	// Err, keyed on the pipe node's own Result type (spec 9.2 items
	// 3 and 5) — so `5 |?> f` wraps too, while a later `|?> g` sees
	// an already-wrapped Err as plain inflow and passes it through.
	if !errIsGoError(x.Type(), e.localT) {
		return r
	}
	isRes, isOk = e.resultTag(r)
	if !isRes || isOk {
		return r
	}
	variant := r.(*VVariant)
	if len(variant.Fields) != 1 {
		failData(CodeInternal, "malformed Err value", x.Span(), "value", r.String())
	}
	errV := variant.Fields[0].V
	wrapped := &VGoErr{Text: stepName(x.Y) + ": " + errorText(errV), Wrapped: errV}
	fields := append([]VField(nil), variant.Fields...)
	fields[0].V = wrapped
	return &VVariant{Type: variant.Type, Ctor: variant.Ctor, Name: variant.Name, Fields: fields}
}

// errIsGoError reports whether t is `Result _ GoError` — the
// builtin Result only: a file-local `type Result` (localT) spells
// the same Con but is an ADT the pipe-wrap rule does not apply to.
func errIsGoError(t types.Type, localT map[string]bool) bool {
	con, ok := t.(*types.Con)
	if !ok || con.Qual != "" || con.Name != "Result" || len(con.Args) != 2 || localT["Result"] {
		return false
	}
	// A file-local `type GoError` spells the same Con as the builtin;
	// the pipe-wrap rule only applies to the builtin error channel.
	if localT["GoError"] {
		return false
	}
	return types.Equal(con.Args[1], types.GoError)
}

// stepName derives the `|?>` step name from the right-hand
// expression's head (spec 9.2 item 4): the written name, qualified
// for module members; otherwise the source position. Position uses
// the byte offset because the evaluator does not hold sources for a
// line index — see the reported gap.
func stepName(x check.Expr) string {
	// Partial probes stay as if-assertions: check.Expr is a sumtype
	// and the checker requires exhaustive switches.
	if x, ok := x.(*check.Var); ok {
		return x.Sym.Name
	}
	if x, ok := x.(*check.MemberExpr); ok {
		prefix := ""
		if v, ok := x.Recv.(*check.Var); ok {
			prefix = v.Sym.Name + "."
		}
		return prefix + x.Sel.Name
	}
	if x, ok := x.(*check.Apply); ok {
		return stepName(x.Fn)
	}
	sp := x.Span()
	return fmt.Sprintf("%s:%d", sp.File, sp.Start)
}

// errorText renders the message of an error-ish value.
func errorText(v Value) string {
	if g, ok := v.(*VGoErr); ok {
		return g.Text
	}
	return v.String()
}

// evalApply evaluates a function application per its CallInfo.
// Left-to-right: the function expression evaluates first, then the
// arguments in source order (a Hole never evaluates).
func (e *interp) evalApply(x *check.Apply, env *Env) Value {
	fv := e.eval(x.Fn, env)
	vals := make([]Value, len(x.Args))
	holes := 0
	holeAt := -1
	for i, a := range x.Args {
		if _, isHole := a.Value.(*check.Hole); isHole {
			holes++
			holeAt = i
			continue
		}
		vals[i] = e.eval(a.Value, env)
	}
	// The CallInfo contract: at most one hole, and HoleIndex records
	// exactly where it is (any negative value claims "no hole").
	claimed := x.Call.HoleIndex
	if claimed < 0 {
		claimed = -1
	}
	if holes > 1 || holeAt != claimed {
		failData(CodeInternal, "misplaced _ placeholder", x.Span(), "kind", x.Call.Kind.String())
	}

	if x.Call.Kind == check.CallGo {
		return e.evalGoCall(x, fv, vals)
	}
	if x.Call.HoleIndex >= 0 {
		failData(CodeInternal, "hole in a non-Go call", x.Span(), "index", x.Call.HoleIndex)
	}
	f, ok := fv.(*VFunc)
	if !ok {
		failData(CodeInternal, "applied a non-function value", x.Span(),
			"value", fv.String())
	}
	return e.applyArgs(f, x.Call.ArgToParam, vals, x.Span())
}

// evalGoCall evaluates a CallGo application: the callee must be
// bound in the environment as a host function; `_` placeholders
// produce a one-argument function capturing the evaluated args
// (cute semantics — spec section 8.2).
func (e *interp) evalGoCall(x *check.Apply, fv Value, vals []Value) Value {
	f, ok := fv.(*VFunc)
	if !ok {
		failData(CodeInternal, "Go callee is not a host function", x.Span(),
			"value", fv.String())
	}
	if x.Call.HoleIndex < 0 {
		return e.invokeGo(f, vals, x.Span())
	}
	// cute partial call: capture evaluated args, one hole remains.
	var kept []Value
	for i, v := range vals {
		if i == x.Call.HoleIndex {
			continue
		}
		kept = append(kept, v)
	}
	g := funcOfType("", fnGoPartial, &types.Func{
		Params: []types.FuncParam{types.P(types.Unit)}, Ret: types.Unit})
	g.gocall = &goCall{sp: x.Span(), callee: f, args: kept, holeAt: x.Call.HoleIndex}
	return g
}

// evalFor implements `for pat in iter { body }` over List values.
func (e *interp) evalFor(x *check.For, env *Env) Value {
	it := e.eval(x.Iter, env)
	l, ok := it.(*VList)
	if !ok {
		failData(CodeUnsupported, "for over a non-List iterable", x.Iter.Span(),
			"value", it.String())
	}
	for _, el := range l.Elems {
		child := env.Child()
		if !e.matchPat(x.Pat, el, child) {
			failData(CodeInternal, "irrefutable for pattern failed", x.Pat.Span(),
				"value", el.String())
		}
		e.eval(x.Body, child)
	}
	return &VUnit{}
}

// evalRecord builds or updates a record. With a declared record type
// the fields come out in declaration order.
func (e *interp) evalRecord(x *check.RecordLit, env *Env) Value {
	var fields []VField
	var con *types.Con
	if c, ok := x.Type().(*types.Con); ok {
		con = c
	}
	if x.With != nil {
		base := e.eval(x.With, env)
		r, ok := base.(*VRecord)
		if !ok {
			failData(CodeInternal, "record update of a non-record", x.With.Span(),
				"value", base.String())
		}
		fields = append(fields, r.Fields...)
		con = r.Type
	} else if info := e.recordOf(con); info != nil {
		fields = make([]VField, len(info.fields))
		for i, fi := range info.fields {
			fields[i] = VField{Index: fi.index, Name: fi.name, Sym: fi.sym}
		}
	}
	for _, fi := range x.Fields {
		v := e.eval(fi.Value, env)
		idx := e.recordFieldIndex(con, fi.Sym, fi.Name)
		placed := false
		for j := range fields {
			if (fields[j].Sym != nil && fields[j].Sym == fi.Sym) ||
				(idx >= 0 && fields[j].Index == idx) ||
				(fields[j].Sym == nil && fields[j].Name == fi.Name && idx < 0) {
				fields[j].V = v
				if fields[j].Name == "" {
					fields[j].Name = fi.Name
				}
				if fields[j].Sym == nil {
					fields[j].Sym = fi.Sym
				}
				placed = true
				break
			}
		}
		if !placed {
			fields = append(fields, VField{Index: idx, Name: fi.Name, Sym: fi.Sym, V: v})
		}
	}
	for _, f := range fields {
		if f.V == nil {
			// A declared-layout field the literal never initialized:
			// the checker rejects missing fields, so this is a
			// broken invariant — not a value String() can render.
			failData(CodeInternal, "record field is not initialized", x.Span(),
				"field", f.Name)
		}
	}
	if info := e.recordOf(con); info != nil {
		sort.SliceStable(fields, func(i, j int) bool { return fields[i].Index < fields[j].Index })
	}
	return &VRecord{Type: con, Fields: fields}
}

// recordOf returns the declared layout of a record type. Structural
// equality is tried first; a generic record's literal (`Box Int`
// against a declared `Box a`) falls back to the type's qualified
// name.
func (e *interp) recordOf(con *types.Con) *recordInfo {
	if con == nil {
		return nil
	}
	for _, info := range e.ctx.records {
		if types.Equal(info.typ, con) {
			return info
		}
	}
	for _, info := range e.ctx.records {
		if info.typ.Qual == con.Qual && info.typ.Name == con.Name {
			return info
		}
	}
	return nil
}

// recordFieldIndex finds the declared index of a record field.
func (e *interp) recordFieldIndex(con *types.Con, sym *resolve.Symbol, name string) int {
	info := e.recordOf(con)
	if info == nil {
		return -1
	}
	for _, f := range info.fields {
		if f.sym != nil && f.sym == sym {
			return f.index
		}
	}
	for _, f := range info.fields {
		if f.name == name {
			return f.index
		}
	}
	return -1
}

// ctorSig synthesizes the constructor's function signature from its
// field metadata (all positional; labels are an apply-time concern
// resolved through CallInfo).
func ctorSig(info *ctorInfo) *types.Func {
	sig := &types.Func{Ret: info.typ}
	for _, f := range info.fields {
		sig.Params = append(sig.Params, types.FuncParam{Label: f.label, Type: types.Unit})
	}
	return sig
}

// display renders a value inside string interpolation: scalar values
// in their natural form, composites in the comparison format.
func display(v Value) string {
	switch v := v.(type) {
	case *VUnit:
		return "()"
	case *VBool:
		return v.String()
	case *VInt:
		return v.String()
	case *VFloat:
		return v.String()
	case *VChar:
		return string(v.R)
	case *VStr:
		return v.S
	case *VTuple, *VList, *VRecord, *VVariant, *VGoErr, *VFunc:
		return v.String()
	default:
		panic("unreachable")
	}
}
