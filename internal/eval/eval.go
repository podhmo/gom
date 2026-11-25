package eval

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/staticalias"
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
	ctx   *ctx
	lines *diag.LineIndex
	// fileLines holds a per-file line index for step labels of a
	// multi-file evaluation (Evaluator.SetLineIndexFor): a |?> step
	// in b.gom must name its own file:line, not the primary file's.
	fileLines map[string]*diag.LineIndex
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
	// localT names the unit's declared types — an unqualified
	// *types.Con carrying one of these names denotes a package-local
	// declaration, never the builtin of the same spelling. It is
	// the same guard check's shadowedBuiltin, lower's declaredT and
	// codegen's localT apply: a `type Result` declared in any file
	// of the unit makes IsResult answer false for its variants
	// unit-wide.
	localT map[string]bool
	// floatWidthOracle answers the machine width of a float type
	// the interpreter cannot see through — a named Go type whose
	// underlying is float32 (`type SmallRatio float32`). Only the
	// go/types oracle of the loader knows it; nil means every float
	// position judges at width 64. Evaluator.SetFloatWidthOracle
	// installs it.
	floatWidthOracle func(types.Type) (int, bool)
}

// letInit is the lazy initializer of one top-level `let`
// declaration: the compiled form is a Go package variable, whose
// initialization order follows dependencies rather than source order
// (`let y = z + 1; let z = 40` initializes z first and y = 41). The
// evaluator matches that by computing the value on first demand —
// when Run's ready-frontier reaches the declaration, when a ready
// dependency's initializer reads it, or when evalVar reads one of
// its bound names. busy marks an in-progress initializer so a
// re-entrant read reports an initialization cycle instead of
// diverging; done means the pattern has been bound into the global
// environment already.
type letInit struct {
	decl *check.LetDecl
	done bool
	busy bool
}

// --- public API ---

// Evaluator evaluates the typed files of one package unit: it
// registers type declarations and binds top-level functions into a
// global environment at New/NewFiles; Run then initializes the value
// declarations in dependency order, runs the tests, and applies
// `main`.
type Evaluator struct {
	env    *Env
	files  []*check.File
	interp *interp
	// initOrder is the unit's top-level lets in declaration order —
	// lexical file order across files, source order within one —
	// each carrying its package-variable dependency set (spec 5.1).
	// Run consumes it; lazy forcing through evalVar can mark a node
	// done before the frontier reaches it.
	initOrder []*initNode
}

// New prepares evaluation of f — the one-file unit.
func New(f *check.File) *Evaluator {
	return NewFiles([]*check.File{f})
}

// NewFiles prepares evaluation of one package unit — the typed files
// check.Files returns and build.CompileFiles hands out through
// Result.Typed. Every file's declarations share one package scope:
// type declarations register their constructors and fields into one
// metadata set (a package-local `type Result` shadows the builtin in
// every member file), every top-level function symbol binds its
// (recursion-ready) closure, and every top-level let registers a lazy
// initializer read on first demand.
//
// File order is the lexical file-name order spec 5.1 fixes as the
// cross-file declaration order — the base name, the key the
// generated package's .go files are written and compiled under — so
// the order callers hand the slice in does not matter (and neither
// do the paths' directory prefixes: a unit's files live in one
// directory). Within a file, declarations keep source order.
func NewFiles(files []*check.File) *Evaluator {
	env := NewEnv()
	ev := &Evaluator{env: env, interp: &interp{ctx: env.ctx, maxDepth: defaultMaxDepth}}
	for _, f := range files {
		if f != nil {
			ev.files = append(ev.files, f)
		}
	}
	sort.SliceStable(ev.files, func(i, j int) bool {
		return filepath.Base(ev.files[i].Sp.File) < filepath.Base(ev.files[j].Sp.File)
	})
	for _, f := range ev.files {
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
	}
	for _, f := range ev.files {
		for _, d := range f.Decls {
			fd, ok := d.(*check.FnDecl)
			if !ok || fd.Body == nil {
				// A bodiless declaration (an interface-level
				// signature that slipped into a .gom file) stays
				// unbound — a use will report "unbound symbol".
				continue
			}
			env.Bind(fd.Sym, ev.fnValue(fd))
		}
	}
	// The static-alias classification is file-local like lowering's:
	// the unit's declarations index the local functions, but only a
	// file's own `let`s are alias candidates — a `let h = f` whose
	// `f` is a sibling file's binding stays a plain variable in both
	// the emitted Go and the graph buildInitGraph builds.
	var unit []check.Decl
	for _, f := range ev.files {
		unit = append(unit, f.Decls...)
	}
	aliases := map[*check.File]*staticalias.Index{}
	for _, f := range ev.files {
		aliases[f] = staticalias.Classify(f.Decls, unit)
	}
	// A static-alias binding lowers to a ValueAlias FuncDecl — a
	// function, not a package var — so it binds eagerly like the
	// FnDecls above: applying the wrapper evaluates the binding's
	// peeled element and never forces the hidden variable an
	// effectful alias initializer still emits.
	for _, f := range ev.files {
		idx := aliases[f]
		for _, d := range f.Decls {
			ld, ok := d.(*check.LetDecl)
			if !ok {
				continue
			}
			for _, s := range boundSyms(ld.Pat) {
				if s != nil && idx.Aliases[s] {
					env.Bind(s, ev.aliasValue(s, idx.Elems[s]))
				}
			}
		}
	}
	// Top-level lets stay unbound until Run (or a first read through
	// evalVar) initializes them — the compiled form is a package var,
	// whose initialization follows dependencies, not source order.
	for _, f := range ev.files {
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
	}
	ev.initOrder = buildInitGraph(ev.files, env.ctx.topLets, aliases)
	return ev
}

// Env returns the evaluator's global environment — bind host
// functions and stub module members here before Run or Expr.
func (e *Evaluator) Env() *Env { return e.env }

// SetLineIndex supplies source lines for nameless pipeline step labels.
// Without an index, hand-built AST evaluation uses byte offsets.
func (e *Evaluator) SetLineIndex(lines *diag.LineIndex) { e.interp.lines = lines }

// SetLineIndexFor registers the line index of one file of the unit.
// A multi-file evaluation resolves each |?> step label against the
// index of the file the step's span names — a step in b.gom must not
// read its line through a.gom's bytes.
func (e *Evaluator) SetLineIndexFor(path string, lines *diag.LineIndex) {
	if e.interp.fileLines == nil {
		e.interp.fileLines = map[string]*diag.LineIndex{}
	}
	e.interp.fileLines[path] = lines
}

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

// SetFloatWidthOracle installs the width oracle for float types the
// interpreter cannot see through — named Go types whose underlying
// is a float kind. The contract has three outcomes: (32, true) and
// (64, true) for float32/float64 underlyings respectively, and
// (_, false) when the type is not float-underlying OR NOT KNOWN —
// int-spelled literals at a position depend on the float/unknown
// distinction to materialize as floating-point values, so an oracle
// that answers (_, false) for a float64-underlying named type turns
// its arithmetic integer. Without an oracle everything judges at
// width 64.
func (e *Evaluator) SetFloatWidthOracle(f func(types.Type) (int, bool)) {
	e.interp.floatWidthOracle = f
}

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

// Run initializes the unit's top-level LetDecls the way the compiled
// package does (spec 5.1): each step initializes the earliest-declared
// let whose dependencies have all initialized, so an independent var
// declared between a var and its dependency runs before either — the
// ready-in-declaration-order pick pure lazy forcing could not
// reproduce. Tests then run in declaration order — the compiled
// binary's test entry points start after package init completes — and
// `main` is applied to `()` last when a function named main exists.
// The first failure stops the run.
func (e *Evaluator) Run() Result {
	var diags diag.List
	force := func(n *initNode) bool {
		_, ds := runEval(func() Value {
			e.interp.ensureTopLet(n.decl, e.env)
			return nil
		})
		diags = append(diags, ds...)
		return !ds.HasError()
	}
	// The ready frontier: repeatedly take the first pending let in
	// declaration order whose vardep set has fully initialized.
	// Forcing one can mark others done through lazy reads, so each
	// pick rescans rather than carrying a ready set.
	for {
		var next *initNode
		for _, n := range e.initOrder {
			if n.forced() || !n.ready() {
				continue
			}
			next = n
			break
		}
		if next == nil {
			break
		}
		if !force(next) {
			return Result{Diags: diags}
		}
		next.done = true
	}
	// Initializers still pending sit on (or downstream of) a
	// dependency cycle the compiled unit rejects as E0907 — only
	// hand-built units reach here. Forcing them in declaration order
	// keeps the lazy re-entrant-read diagnostic (E9003) for them.
	for _, n := range e.initOrder {
		if n.forced() {
			continue
		}
		if !force(n) {
			return Result{Diags: diags}
		}
		n.done = true
	}
	for _, f := range e.files {
		for _, d := range f.Decls {
			td, ok := d.(*check.TestDecl)
			if !ok {
				continue
			}
			v, ds := runEval(func() Value {
				return runBody(func() Value { return e.interp.eval(td.Body, e.env.Child()) })
			})
			if !ds.HasError() && v != nil {
				if isRes, isOk := e.interp.resultTag(v); isRes && !isOk {
					ds = append(ds, diag.Error(CodeTestFail, "test returned Err").
						At(td.Sp).Set("test", td.Name).Set("value", v.String()))
				}
			}
			diags = append(diags, ds...)
			if ds.HasError() {
				return Result{Diags: diags}
			}
		}
	}
	if g := e.Global("main"); g != nil {
		v, ds := runEval(func() Value {
			return e.interp.applyOne(g, &VUnit{}, e.mainSpan())
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

// ProgramFiles evaluates a package unit — NewFiles(files).Run().
func ProgramFiles(files []*check.File) Result {
	return NewFiles(files).Run()
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

// sp returns the first file's span for diagnostics, or the zero span
// when the evaluator was built with no files.
func (e *Evaluator) sp() diag.Span {
	if len(e.files) == 0 {
		return diag.Span{}
	}
	return e.files[0].Sp
}

// mainSpan returns the span of the FnDecl that declared `main` —
// the declaration boundary the "applying main" diagnostics belong
// to — and falls back to sp() when `main` is a non-declared binding
// (a host stub bound by name through Env).
func (e *Evaluator) mainSpan() diag.Span {
	for _, f := range e.files {
		for _, d := range f.Decls {
			if fd, ok := d.(*check.FnDecl); ok && fd.Sym != nil && fd.Sym.Name == "main" {
				return fd.Sp
			}
		}
	}
	return e.sp()
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
	// Declared >=2-arrow parameters carry the flat calling
	// convention in generated code — mark the same slots so
	// applyArgs can defer partial applications the way the
	// emitted closure does (decision 2026-11-19-flat-partial-eval).
	for i, p := range sig.Params {
		if fs := evalFlatSig(p.Type); fs != nil && len(fs.Params) >= 2 {
			if f.flatArity == nil {
				f.flatArity = make([]int, len(sig.Params))
			}
			f.flatArity[i] = len(fs.Params)
		}
	}
	return f
}

// aliasValue builds the ValueAlias wrapper the compiled unit emits
// for a static function alias (the eval analogue of lower's
// aliasWrapper): a function value that evaluates the binding's peeled
// element expression — a pure reference to a local function — and
// applies the result, so calling through the alias never forces the
// hidden package variable an effectful `let` initializer still
// lowers to. The element is resolved per call, like the emitted
// wrapper's own body reading its target.
func (e *Evaluator) aliasValue(sym *resolve.Symbol, elem check.Expr) *VFunc {
	sig, _ := types.Chase(elem.Type()).(*types.Func)
	f := funcOfType(sym.Name, fnBuiltin, sig)
	f.builtin = func(args []Value) (Value, error) {
		v := e.interp.eval(elem, e.env)
		for _, a := range args {
			v = e.interp.applyOne(v, a, elem.Span())
		}
		return v, nil
	}
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
				fields[i] = fieldInfo{name: tf.Name, label: tf.Label, sym: tf.Sym, index: tf.Index, typ: tf.Type}
			}
			e.interp.registerCtor(&ctorInfo{
				sym: variant.Sym, name: variant.Sym.Name, typ: con, fields: fields, declared: true,
			})
		}
	case *check.RecordType:
		fields := make([]fieldInfo, len(body.Fields))
		for i, tf := range body.Fields {
			fields[i] = fieldInfo{name: tf.Name, label: tf.Label, sym: tf.Sym, index: tf.Index, typ: tf.Type}
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
		return e.litValueTyped(x.Kind, x.Text, x.Type(), x.Span())
	case *check.Var:
		return e.evalVar(x, env)
	case *check.Apply:
		return e.evalApply(x, env)
	case *check.Hole:
		failData(CodeInternal, "misplaced _ placeholder", x.Span(), "kind", "bare")
	case *check.MemberExpr:
		return e.evalMember(x, env)
	case *check.FieldAssign:
		return e.evalFieldAssign(x, env)
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
				sb.WriteString(e.displayTyped(e.eval(part.X, env), part.X.Type()))
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
				switch tail := v.(type) {
				case *VList:
					elems = append(elems, tail.Elems...)
				case *VHost:
					// A Go slice through a host binding spreads
					// element-wise — unboxed per the element type
					// so arithmetic and patterns see real values,
					// the same conversion unboxAny applies at a
					// composite's slots.
					if con, ok := types.Chase(sp.X.Type()).(*types.Con); ok && len(con.Args) == 1 {
						if rv := reflect.ValueOf(tail.V); rv.Kind() == reflect.Slice {
							for i := 0; i < rv.Len(); i++ {
								elems = append(elems, e.unboxAny(&VHost{V: rv.Index(i).Interface()}, con.Args[0]))
							}
							continue
						}
					}
					failData(CodeInternal, "spread of a non-list", el.Span(), "value", v.String())
				default:
					failData(CodeInternal, "spread of a non-list", el.Span(), "value", v.String())
				}
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
		if x.TryAs {
			// `Go.try_as x : Option T` — the comma-ok assertion
			// `x.(T)` is a dynamic-shape check on the evaluated
			// value; Some on a match, None otherwise.
			v := e.eval(x.X, env)
			con, _ := types.Chase(x.Type()).(*types.Con)
			if con != nil && len(con.Args) == 1 && e.inhabits(v, con.Args[0]) {
				return &VVariant{Type: con, Name: "Some", Fields: []VField{{Index: 0, V: e.unboxAny(v, con.Args[0])}}}
			}
			return &VVariant{Type: con, Name: "None"}
		}
		if x.Conv {
			// A literal materialization into a defined type is a
			// no-op — the eval value is the field scalar itself
			// (a one-field record evaluates to its field value).
			if x.Mat {
				return e.eval(x.X, env)
			}
			// The implicit `any` box (checker's `Go.Any`
			// conformance materialization) is representation-only
			// here: eval values are dynamically typed already.
			// Other Conv ascriptions keep the existing verdict.
			if !x.Iface && !x.Slice && isGoAnyCon(x.Type()) {
				return e.anyBox(e.eval(x.X, env), x.X.Type(), env)
			}
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
	return e.applyArgs(f, nil, nil, nil, sp)
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
		if info != nil && !info.declared {
			// A lazily-derived info is only valid for the
			// instantiation it was built from — the first apply
			// site's concrete types would stick to every later site
			// (Some 'a' recasting a later Some 42's payload type).
			// Derive this site's shape fresh; the shared cache entry
			// still serves name/label lookups.
			info = deriveCtor(x.Sym, x.Sym.Name, x.Type())
		}
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
// Run drives declarations through the ready frontier while evalVar
// forces them on first read, so a declaration may already be done
// when reached here. A pattern binding no symbols (`let _ = e`) can
// never be referenced and is evaluated straight away for effect.
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

// evalFieldAssign performs `ptr.Field = v` on a host-bound Go struct
// pointer: the receiver must be a VHost carrying *T, the field is
// set by reflection — the same mutation compiled code emits.
func (e *interp) evalFieldAssign(x *check.FieldAssign, env *Env) Value {
	recv := e.eval(x.Recv, env)
	v := e.eval(x.Value, env)
	h, ok := recv.(*VHost)
	if !ok {
		failData(CodeUnsupported,
			"field assignment on a non-Go value", x.Recv.Span(),
			"value", recv.String())
	}
	rv := reflect.ValueOf(h.V)
	if rv.Kind() != reflect.Ptr || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		failData(CodeInternal,
			"field assignment receiver is not a struct pointer", x.Recv.Span(),
			"value", recv.String())
	}
	f := rv.Elem().FieldByName(x.Sym.Name)
	if !f.IsValid() || !f.CanSet() {
		failData(CodeInternal, "Go field is not settable", x.Sel.Sp,
			"field", x.Sym.Name)
	}
	gv, ok := goFieldValue(v, f.Type())
	if !ok {
		failData(CodeUnsupported,
			"field assignment value cannot convert to the Go field type", x.Value.Span(),
			"field", x.Sym.Name)
	}
	f.Set(gv)
	return &VUnit{}
}

// goFieldValue unboxes an eval value into the Go field type of a
// host-bound struct — the write-side mirror of hostElem's Go→Value
// read. Scalars and opaque host values convert; gom aggregates do
// not.
func goFieldValue(v Value, t reflect.Type) (reflect.Value, bool) {
	var x any
	switch v := v.(type) {
	case *VHost:
		x = v.V
	case *VInt:
		x = v.N
	case *VChar:
		x = v.R
	case *VFloat:
		x = v.F
	case *VBool:
		x = v.B
	case *VStr:
		x = v.S
	default:
		return reflect.Value{}, false
	}
	rv := reflect.ValueOf(x)
	if !rv.IsValid() || !rv.Type().AssignableTo(t) {
		return reflect.Value{}, false
	}
	return rv, true
}

// evalMember handles `recv.sel`: record/variant field access, module
// member selection (resolved through the environment so the harness
// can stub it), or a Go method selection — a bound method whose host
// stub receives the receiver as its first argument.
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
			// A named payload read through the sum type asserts the
			// value holds the owning variant (spec 3.2, decision
			// 2026-11-05-variant-field-assert-owning-ctor): a wrong-
			// variant value is a contract violation and panics, like
			// generated code's `s.(Shape_Circle).R` assertion and the
			// match decision tree's invalid-variant arm. The identity
			// comparison mirrors matchPat's — a bare-name value
			// (Ctor == nil, hand-built) compares by name.
			if x.Ctor != nil &&
				r.Ctor != x.Ctor &&
				!(r.Ctor == nil && r.Name == x.Ctor.Name) {
				panic(failure{d: diag.Error(CodePanic,
					"field access on the wrong variant").
					At(x.Sel.Sp).
					Set("field", x.Sel.Name).
					Set("ctor", x.Ctor.Name).
					Set("value", r.String())})
			}
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
			// A one-field record evaluates to its field value when
			// the type is a Go defined type (`type UserId int64`) —
			// `let x : UserId = 0` binds the scalar 0, and `x.value`
			// unwraps like generated code's `int64(x)`. The scalar
			// still carries the literal's own kind: a Float-backed
			// field reads its value as a float (`let s: Score = 1`
			// projects `1.0`, matching generated `Score(1)`).
			if x.Index == 0 {
				if e.floatBackedField(x.Type()) {
					if i, ok := recv.(*VInt); ok {
						return &VFloat{F: float64(i.N)}
					}
				}
				return recv
			}
			failData(CodeInternal, "field access on a non-record value", x.Recv.Span(),
				"value", recv.String())
		}
	case resolve.KindMethod:
		// A Go method selection evaluates to the bound method: the
		// receiver evaluates now, and the host stub — bound into
		// the environment like any other Go member — takes it as
		// its first argument when the method is finally applied.
		recv := e.eval(x.Recv, env)
		if v, ok := env.Lookup(x.Sym); ok {
			f, ok := v.(*VFunc)
			if !ok || f.kind != fnBuiltin {
				failData(CodeInternal, "Go method is not bound to a host function", x.Sel.Sp,
					"method", x.Sym.Name)
			}
			g := funcOfType(x.Sym.Name, fnBuiltin, methodValueSig(x.Type()))
			g.builtin = func(args []Value) (Value, error) {
				return f.builtin(append([]Value{recv}, args...))
			}
			return g
		}
		failData(CodeUnsupported,
			"Go method not bound — provide a host value via Env.Bind", x.Sel.Sp,
			"method", x.Sym.Name)
	default:
		// Module member selection (`pkg.Fn`): the member symbol's
		// value comes from the environment so tests can stub it.
		if v, ok := env.Lookup(x.Sym); ok {
			return e.forceThunk(v, x.Sel.Sp)
		}
		if x.Sym.Kind == resolve.KindCtor {
			info := e.ctorOf(x.Sym, x.Sym.Name, x.Type())
			if info != nil && !info.declared {
				info = deriveCtor(x.Sym, x.Sym.Name, x.Type())
			}
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

// intOperandWidth returns the machine width of an integer operand
// type: 32 for the builtin Int32, 64 for everything else (Int and
// Int64 are both int64 at run time; an unresolved meta defaults to
// Int). A file-local `type Int32` shadows the builtin the same way
// the checker's num constraint rejects it — its Con is not an
// integer type here.
func (e *interp) intOperandWidth(t types.Type) int {
	c, ok := types.Chase(t).(*types.Con)
	if ok && c.Qual == "" && c.Name == "Int32" && !e.localT["Int32"] {
		return 32
	}
	return 64
}

// floatOperandWidth returns the machine width a float operand type
// computes in: 32 when the type peels — through declared one-field
// records, the way codegen's defined-type chain emits — to a named
// Go type whose underlying is float32, 64 otherwise. Peelable
// records need no oracle: a user `type Float` shadows nothing here
// because only the emitted scalar shape decides the width. Values
// at width 32 carry the canonical float64 image of their float32
// value — like the VInt invariant, host-supplied or hand-built
// values are repaired at the operation instead of trusting
// construction.
func (e *interp) floatOperandWidth(t types.Type) int {
	w, _ := e.floatKindOf(t)
	return w
}

// roundFloat narrows f to the float32 image width 32 carries —
// the canonical form every width-32 VFloat keeps.
func roundFloat(width int, f float64) float64 {
	if width == 32 {
		return float64(float32(f))
	}
	return f
}

// wrapTo narrows n to width bits and sign-extends back to int64 —
// the per-operation two's-complement wrap of spec 3.3. Width 64 is
// the identity: int64 arithmetic already wraps.
func wrapTo(width int, n int64) int64 {
	if width == 32 {
		return int64(int32(n))
	}
	return n
}

// evalUnary handles `-x` and `!x`.
func (e *interp) evalUnary(x *check.Unary, env *Env) Value {
	v := e.eval(x.X, env)
	switch x.Op {
	case syntax.UnNeg:
		switch v := v.(type) {
		case *VInt:
			return &VInt{N: wrapTo(e.intOperandWidth(x.X.Type()), -v.N)}
		case *VFloat:
			// Negating a zero LITERAL folds like a Go constant
			// expression: untyped constants carry no signed zero,
			// so `-0`/`-0.0` emit as positive zero while runtime
			// negation of a non-literal +0.0 stays IEEE -0.
			if _, lit := x.X.(*check.Lit); lit && v.F == 0 {
				return &VFloat{F: 0, W: v.W}
			}
			return &VFloat{F: -v.F, W: v.W}
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
		// Per-operation machine semantics (spec 3.3): the operands
		// are normalized to their static width — every VInt a
		// checked program produces is already canonical, so this
		// only repairs host-supplied or hand-built values — then
		// the operation runs in int64 and arithmetic results wrap
		// back. MinInt32 / -1 lands on -2147483648 exactly like
		// Go's int32 division, and normalized operands compare
		// identically to int32 operands.
		w := e.intOperandWidth(x.X.Type())
		a, b := wrapTo(w, l.N), wrapTo(w, r.N)
		switch x.Op {
		case syntax.BinAdd:
			return &VInt{N: wrapTo(w, a+b)}
		case syntax.BinSub:
			return &VInt{N: wrapTo(w, a-b)}
		case syntax.BinMul:
			return &VInt{N: wrapTo(w, a*b)}
		case syntax.BinDiv:
			if b == 0 {
				failData(CodePanic, "integer division by zero", x.Span(), "op", "/")
			}
			return &VInt{N: wrapTo(w, a/b)}
		case syntax.BinMod:
			if b == 0 {
				failData(CodePanic, "integer modulo by zero", x.Span(), "op", "%")
			}
			return &VInt{N: wrapTo(w, a%b)}
		case syntax.BinBitAnd:
			return &VInt{N: wrapTo(w, a&b)}
		case syntax.BinBitAndNot:
			return &VInt{N: wrapTo(w, a&^b)}
		case syntax.BinBitOr:
			return &VInt{N: wrapTo(w, a|b)}
		case syntax.BinBitXor:
			return &VInt{N: wrapTo(w, a^b)}
		case syntax.BinBitShl:
			if b < 0 {
				failData(CodePanic, "negative shift amount", x.Span(), "op", "<<<")
			}
			return &VInt{N: wrapTo(w, a<<uint(b))}
		case syntax.BinBitShr:
			if b < 0 {
				failData(CodePanic, "negative shift amount", x.Span(), "op", ">>>")
			}
			return &VInt{N: wrapTo(w, a>>uint(b))}
		case syntax.BinLt:
			return &VBool{B: a < b}
		case syntax.BinLe:
			return &VBool{B: a <= b}
		case syntax.BinGt:
			return &VBool{B: a > b}
		case syntax.BinGe:
			return &VBool{B: a >= b}
		}
	case *VFloat:
		r, ok := r.(*VFloat)
		if !ok {
			e.binMismatch(x, l, r)
		}
		// Per-operation machine semantics, mirroring the integer
		// path: at width 32 the operands are canonical float32
		// images, so converting down is lossless and the operation
		// runs in float32 precision exactly like the emitted Go —
		// `16777217.0` rounds at construction and compares equal
		// to `16777216.0` where compiled float32 does.
		w := e.floatOperandWidth(x.X.Type())
		if w == 32 {
			a, b := float32(l.F), float32(r.F)
			switch x.Op {
			case syntax.BinAdd:
				return &VFloat{F: float64(a + b), W: 32}
			case syntax.BinSub:
				return &VFloat{F: float64(a - b), W: 32}
			case syntax.BinMul:
				return &VFloat{F: float64(a * b), W: 32}
			case syntax.BinDiv:
				return &VFloat{F: float64(a / b), W: 32}
			case syntax.BinLt:
				return &VBool{B: a < b}
			case syntax.BinLe:
				return &VBool{B: a <= b}
			case syntax.BinGt:
				return &VBool{B: a > b}
			case syntax.BinGe:
				return &VBool{B: a >= b}
			}
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
	wrapped := &VGoErr{Text: e.stepName(x.Y) + ": " + errorText(errV), Wrapped: errV}
	fields := append([]VField(nil), variant.Fields...)
	fields[0].V = wrapped
	return &VVariant{Type: variant.Type, Ctor: variant.Ctor, Name: variant.Name, Fields: fields}
}

// anyBox wraps a value crossing into `Go.Any` — the `any(v)` the
// compiled boundary emits. The VHost records the value's gom type so
// `Go.try_as` can assert at the emitted-Go-type grain (`x.(T)` on
// int64 succeeds for both Int and Int64). A value already boxed —
// re-entering `any` — keeps its inner box, matching Go.
func (e *interp) anyBox(v Value, t types.Type, env *Env) Value {
	if _, ok := v.(*VHost); ok {
		return v
	}
	if ct := substTy(types.Chase(t), env); types.GoRepr(ct) != "" {
		return &VHost{V: boxRepr(v, ct), T: ct}
	}
	// The box's static type is not concretely nameable — a type
	// parameter under a generic helper — so the slot holds a
	// value-driven Go repr and asserts by reflect name instead.
	return &VHost{V: boxValue(v)}
}

// substTy resolves type parameters through the call-frame type
// bindings — the instantiation a generic helper's `a` carried at
// apply time. An unbound parameter stays a Var (reprName then
// reports "unnameable" and the box falls back to value shape).
func substTy(t types.Type, env *Env) types.Type {
	switch t := types.Chase(t).(type) {
	case *types.Var:
		if bt, ok := env.LookupTy(t); ok {
			return types.Chase(bt)
		}
		return t
	case *types.Con:
		args := make([]types.Type, len(t.Args))
		changed := false
		for i, a := range t.Args {
			args[i] = substTy(a, env)
			changed = changed || args[i] != types.Chase(a)
		}
		if !changed {
			return t
		}
		cp := *t
		cp.Args = args
		return &cp
	case *types.Func:
		ps := make([]types.FuncParam, len(t.Params))
		changed := false
		for i, p := range t.Params {
			ps[i] = p
			ps[i].Type = substTy(p.Type, env)
			changed = changed || ps[i].Type != types.Chase(p.Type)
		}
		ret := substTy(t.Ret, env)
		if !changed && ret == types.Chase(t.Ret) {
			return t
		}
		return &types.Func{Params: ps, Ret: ret}
	case *types.Tuple:
		elems := make([]types.Type, len(t.Elems))
		changed := false
		for i, el := range t.Elems {
			elems[i] = substTy(el, env)
			changed = changed || elems[i] != types.Chase(el)
		}
		if !changed {
			return t
		}
		return &types.Tuple{Elems: elems}
	case *types.Go, *types.Meta:
		// Host types and unsolved metas name nothing to
		// substitute — leave them be.
		return t
	default:
		panic("unreachable")
	}
}

// boxRepr puts the Go representation of a scalar into the `any`
// slot — the same materialization emit applies (`any(int64(42))`,
// int32 for Int32, byte for Byte). Aggregates keep their Value in
// the slot: the recorded gom type carries the assertion's answer.
func boxRepr(v Value, t types.Type) any {
	if c, ok := types.Chase(t).(*types.Con); ok && c.Qual == "" {
		switch c.Name {
		case "Int", "Int64":
			if i, ok := v.(*VInt); ok {
				return i.N
			}
		case "Int32":
			if i, ok := v.(*VInt); ok {
				return int32(i.N)
			}
		case "Byte":
			if i, ok := v.(*VInt); ok {
				return byte(i.N)
			}
		case "Float", "Float64":
			if f, ok := v.(*VFloat); ok {
				return f.F
			}
		case "Char":
			if ch, ok := v.(*VChar); ok {
				return ch.R
			}
		case "String":
			if s, ok := v.(*VStr); ok {
				return s.S
			}
		case "Bool":
			if b, ok := v.(*VBool); ok {
				return b.B
			}
		}
	}
	return v
}

// boxValue materializes a Go repr without static type information —
// the value-driven counterpart of boxRepr for `any` slots whose gom
// type is a type parameter. Only scalars materialize (a generic
// `Int32` still boxes int64 — eval keeps one int repr); aggregates
// keep their Value, which inhabits names by valueReprName.
func boxValue(v Value) any {
	switch v := v.(type) {
	case *VInt:
		return v.N
	case *VFloat:
		return v.F
	case *VChar:
		return v.R
	case *VStr:
		return v.S
	case *VBool:
		return v.B
	default:
		return v
	}
}

// unboxAny converts a successful `x.(T)` payload back to an eval
// value: a gom value that survived the box (aggregates, and any
// value an unknown-type slot held) comes out as itself; a
// materialized Go repr is rebuilt at the target's grain; opaque
// host payloads — named Go types like `sort.IntSlice` — stay host
// values.
func (e *interp) unboxAny(v Value, t types.Type) Value {
	h, ok := v.(*VHost)
	if !ok {
		return v
	}
	if types.GoRepr(t) == "any" {
		// The catch-all `p : Go.Any` binds at the box's own
		// grain — keep the recorded element type so a later
		// match on the bound name still sees `List Int`.
		return h
	}
	if inner, ok := h.V.(Value); ok {
		return e.reshape(inner, t)
	}
	if tup, ok := types.Chase(t).(*types.Tuple); ok {
		// A host tuple arrives as the emitted struct — `rt.TupleN`
		// up to 8 fields, the anonymous `struct{ V0..Vn }` past it.
		// Both expose V-fields, so reflect the elements out.
		if rv := reflect.ValueOf(h.V); rv.Kind() == reflect.Struct {
			elems := make([]Value, len(tup.Elems))
			for i, et := range tup.Elems {
				fv := rv.FieldByName(fmt.Sprintf("V%d", i))
				if !fv.IsValid() {
					return h
				}
				elems[i] = e.unboxAny(&VHost{V: fv.Interface()}, et)
			}
			return &VTuple{Elems: elems}
		}
	}
	if con, ok := types.Chase(t).(*types.Con); ok && len(con.Args) == 1 &&
		con.Qual == "" && con.Name == "List" {
		// A host slice becomes a VList so list patterns and
		// indexing see its elements — same conversion inside
		// composite targets (a tuple field, a record slot). Only
		// Gom's immutable List converts: a Go.Slice target keeps
		// its VHost so writes through Go stay observable.
		if rv := reflect.ValueOf(h.V); rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			elems := make([]Value, rv.Len())
			for i := range elems {
				elems[i] = e.unboxAny(&VHost{V: rv.Index(i).Interface()}, con.Args[0])
			}
			return &VList{Elems: elems}
		}
	}
	switch types.GoRepr(t) {
	case "error":
		// A typed-nil error payload (a nil *T that satisfies
		// error) must not be Error()'d — the call would panic
		// even when the arm never touches the binding. Keep
		// the host value; an actual method call on it panics
		// the same as Go's.
		if err, ok := h.V.(error); ok {
			rv := reflect.ValueOf(err)
			switch rv.Kind() {
			case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
				if rv.IsNil() {
					return h
				}
			default:
			}
			return &VGoErr{Text: err.Error()}
		}
	case "int64", "int32", "byte", "int":
		switch i := h.V.(type) {
		case int64:
			return &VInt{N: i}
		case int32:
			return &VInt{N: int64(i)}
		case byte:
			return &VInt{N: int64(i)}
		case int:
			return &VInt{N: int64(i)}
		}
	case "float64":
		if f, ok := h.V.(float64); ok {
			return &VFloat{F: f}
		}
	case "rune":
		if r, ok := h.V.(rune); ok {
			return &VChar{R: r}
		}
	case "string":
		if s, ok := h.V.(string); ok {
			return &VStr{S: s}
		}
	case "bool":
		if b, ok := h.V.(bool); ok {
			return &VBool{B: b}
		}
	}
	return h
}

// inhabits reports whether v has the dynamic shape type t asks
// `Go.try_as` to assert — the comma-ok `x.(T)` check on eval's
// values. Both sides compare at emitted-Go-type-name grain: a boxed
// gom value asserts by its recorded type's repr name (Int and Int64
// both assert as int64, `List Int` and `List String` differ), an
// opaque host value by its reflect-rendered name, and a named Go
// type target by `pkgbase.Name`. A nil interface value asserts as
// nothing; an `error` target accepts any non-nil error payload.
func (e *interp) inhabits(v Value, t types.Type) bool {
	want := types.CanonGoRepr(types.GoRepr(t))
	if want == "" {
		return false
	}
	match := func(dyn string) bool {
		dyn = types.CanonGoRepr(dyn)
		return dyn == want || strings.HasSuffix(dyn, "."+want)
	}
	if h, ok := v.(*VHost); ok {
		if h.T != nil {
			return match(types.GoRepr(h.T))
		}
		if inner, isVal := h.V.(Value); isVal {
			if want == "error" {
				_, isErr := inner.(*VGoErr)
				return isErr
			}
			return match(valueReprName(inner))
		}
		if h.V == nil {
			return false
		}
		if want == "error" {
			_, isErr := h.V.(error)
			return isErr
		}
		return match(reflect.TypeOf(h.V).String())
	}
	// Defensive — an `any` slot should only ever hold a box; a
	// native value reaching here answers for scalar reprs.
	if want == "error" {
		_, isErr := v.(*VGoErr)
		return isErr
	}
	return match(valueReprName(v))
}

// implements reports whether v carries every method name the
// type-test arm's named Go interface target requires — the eval
// side of `x.(fmt.Stringer)`. A boxed gom value cannot implement a
// Go interface (gom methods are functions, not a Go method set);
// only an opaque host value's reflect method set can satisfy it.
// A nil interface value implements nothing; an empty requirement
// list matches every non-nil-in-spirit value, like `case any`.
func (e *interp) implements(v Value, names []string, sigs []string) bool {
	h, ok := v.(*VHost)
	if !ok || h.V == nil {
		return false
	}
	if _, isVal := h.V.(Value); isVal {
		// The box holds a gom Value, not a Go object — no method set.
		return false
	}
	rv := reflect.ValueOf(h.V)
	for i, name := range names {
		m := rv.MethodByName(name)
		if !m.IsValid() {
			return false
		}
		// The bound method's type excludes the receiver — compare
		// the flat signature spelling when check could render one.
		if i < len(sigs) && sigs[i] != "" && m.Type().String() != sigs[i] {
			return false
		}
	}
	return true
}

// reshape rewrites a boxed value to the asserted type's Value
// shape when canon-equal instantiations spell scalars differently
// — x.(Tuple[Int32]) on a tuple holding a Char succeeds in Go
// (rune ≡ int32) but eval arithmetic wants a VInt, not a VChar.
// Recurses through lists, tuples, records and variants; anything
// else (and any field type the checker could not name) comes back
// unchanged.
func (e *interp) reshape(v Value, t types.Type) Value {
	ct := types.Chase(t)
	switch v := v.(type) {
	case *VChar:
		if types.GoRepr(ct) == "int32" {
			return &VInt{N: int64(v.R)}
		}
	case *VInt:
		if types.GoRepr(ct) == "rune" {
			return &VChar{R: rune(v.N)}
		}
	case *VList:
		if con, ok := ct.(*types.Con); ok && con.Name == "List" && len(con.Args) == 1 {
			elems := make([]Value, len(v.Elems))
			for i, el := range v.Elems {
				elems[i] = e.reshape(el, con.Args[0])
			}
			return &VList{Elems: elems}
		}
	case *VTuple:
		if tup, ok := ct.(*types.Tuple); ok && len(tup.Elems) == len(v.Elems) {
			elems := make([]Value, len(v.Elems))
			for i, el := range v.Elems {
				elems[i] = e.reshape(el, tup.Elems[i])
			}
			return &VTuple{Elems: elems}
		}
	case *VRecord:
		if con, ok := ct.(*types.Con); ok {
			if ri := e.recordOf(v.Type); ri != nil && len(ri.fields) == len(v.Fields) {
				sub := instSubst(ri.typ, con)
				fs := make([]VField, len(v.Fields))
				for i, f := range v.Fields {
					fs[i] = f
					if ft := ri.fields[i].typ; ft != nil {
						fs[i].V = e.reshape(f.V, applySubst(ft, sub))
					}
				}
				return &VRecord{Type: con, Fields: fs}
			}
		}
	case *VVariant:
		if con, ok := ct.(*types.Con); ok {
			ci := e.ctorOf(v.Ctor, v.Name, v.Type)
			if ci != nil && !ci.declared {
				// A site-derived info's field types are concrete for
				// the instantiation it was built at; substituting
				// them against the target's args recasts payloads to
				// the wrong shape (Some 42's Int becoming Char after
				// an earlier Some 'a'). The builtin Option/Result
				// ctors have a declared shape to fall back to; any
				// other derived info leaves the payload unchanged.
				ci = builtinDeclCtor(v.Ctor)
			}
			if ci != nil && ci.declared && len(ci.fields) == len(v.Fields) {
				sub := instSubst(ci.typ, con)
				fs := make([]VField, len(v.Fields))
				for i, f := range v.Fields {
					fs[i] = f
					if ft := ci.fields[i].typ; ft != nil {
						fs[i].V = e.reshape(f.V, applySubst(ft, sub))
					}
				}
				return &VVariant{Type: con, Ctor: v.Ctor, Name: v.Name, Fields: fs}
			}
		}
	}
	return v
}

// instSubst maps a declared type's parameter vars to an
// instantiation's arguments (`Box a` against `Box Int32` → a↦Int32).
func instSubst(decl, inst *types.Con) map[*types.Var]types.Type {
	if len(decl.Args) == 0 || len(decl.Args) != len(inst.Args) {
		return nil
	}
	m := make(map[*types.Var]types.Type, len(decl.Args))
	for i, a := range decl.Args {
		if v, ok := types.Chase(a).(*types.Var); ok {
			m[v] = inst.Args[i]
		}
	}
	return m
}

// applySubst substitutes vars through a fixed map — the field-type
// counterpart of substTy's env lookup.
func applySubst(t types.Type, m map[*types.Var]types.Type) types.Type {
	if t == nil || len(m) == 0 {
		return t
	}
	switch t := types.Chase(t).(type) {
	case *types.Var:
		if bt, ok := m[t]; ok {
			return types.Chase(bt)
		}
		return t
	case *types.Con:
		args := make([]types.Type, len(t.Args))
		for i, a := range t.Args {
			args[i] = applySubst(a, m)
		}
		return &types.Con{Qual: t.Qual, Name: t.Name, Args: args}
	case *types.Func:
		ps := make([]types.FuncParam, len(t.Params))
		for i, p := range t.Params {
			ps[i] = p
			ps[i].Type = applySubst(p.Type, m)
		}
		return &types.Func{Params: ps, Ret: applySubst(t.Ret, m)}
	case *types.Tuple:
		elems := make([]types.Type, len(t.Elems))
		for i, el := range t.Elems {
			elems[i] = applySubst(el, m)
		}
		return &types.Tuple{Elems: elems}
	case *types.Go, *types.Meta:
		return t
	default:
		panic("unreachable")
	}
}

// valueReprName gives the emitted-Go-type name a native eval value
// would assert as — the counterpart of reprName for a value that
// reached an `any` slot unboxed or under an unnameable type (a
// type parameter). Aggregate names come from the value's own shape.
func valueReprName(v Value) string {
	switch v := v.(type) {
	case *VInt:
		return "int64"
	case *VFloat:
		return "float64"
	case *VChar:
		return "rune"
	case *VStr:
		return "string"
	case *VBool:
		return "bool"
	case *VUnit:
		return "struct{}"
	case *VList:
		elem := "any"
		if len(v.Elems) > 0 {
			if e := valueReprName(v.Elems[0]); e != "" {
				elem = e
			}
		}
		return "list.List[" + elem + "]"
	case *VTuple:
		parts := make([]string, len(v.Elems))
		for i, el := range v.Elems {
			parts[i] = valueReprName(el)
		}
		return fmt.Sprintf("rt.Tuple%d[%s]", len(v.Elems), strings.Join(parts, ", "))
	case *VRecord:
		if v.Type != nil {
			return types.GoRepr(v.Type)
		}
		return ""
	case *VVariant:
		if v.Type != nil {
			return types.GoRepr(v.Type)
		}
		return ""
	case *VGoErr:
		return "error"
	case *VFunc, *VHost:
		return ""
	default:
		panic("unreachable")
	}
}

// isGoAnyCon reports whether t is `Go.Any` — the empty interface the
// checker's implicit boxing ascriptions target.
func isGoAnyCon(t types.Type) bool {
	con, ok := types.Chase(t).(*types.Con)
	return ok && con.Qual == "Go" && con.Name == "Any"
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
// the source line index supplied by the caller; hand-built ASTs without
// an index retain the byte-offset fallback.
func (e *interp) stepName(x check.Expr) string {
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
		return e.stepName(x.Fn)
	}
	sp := x.Span()
	if idx := e.lineIndexOf(sp.File); idx != nil {
		return fmt.Sprintf("%s:%d", sp.File, idx.Pos(sp.Start).Line)
	}
	return fmt.Sprintf("%s:%d", sp.File, sp.Start)
}

// lineIndexOf returns the line index registered for file — the
// per-file index of SetLineIndexFor when present, else the primary
// SetLineIndex one (or nil, which the caller renders as a byte
// offset).
func (e *interp) lineIndexOf(file string) *diag.LineIndex {
	if idx := e.fileLines[file]; idx != nil {
		return idx
	}
	return e.lines
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
	argTys := make([]types.Type, len(x.Args))
	holes := 0
	holeAt := -1
	for i, a := range x.Args {
		if _, isHole := a.Value.(*check.Hole); isHole {
			holes++
			holeAt = i
			continue
		}
		if sp, isSpread := a.Value.(*check.Spread); isSpread {
			// `..e` evaluates to the collection itself; evalGoCall
			// splices it into the flat argument tail, mirroring
			// lower's atom + CallGo Spread flag → `f(xs...)`.
			vals[i] = e.eval(sp.X, env)
			continue
		}
		vals[i] = e.eval(a.Value, env)
		argTys[i] = substTy(a.Value.Type(), env)
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
	return e.applyArgs(f, x.Call.ArgToParam, vals, argTys, x.Span())
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
	ordered := declOrderVals(x.Call, vals)
	if n := len(x.Args); n > 0 {
		if sp, isSpread := x.Args[n-1].Value.(*check.Spread); isSpread {
			// `..e`: the trailing slot holds the evaluated
			// collection — `f(xs...)` splices its elements into
			// the flat tail. A saturated Go call fills every
			// declared parameter, so the variadic spread always
			// lands at the last declaration-order position.
			ordered = e.spliceGoSpread(ordered, sp.Span())
		}
	}
	if x.Call.HoleIndex < 0 {
		return e.invokeGo(f, ordered, x.Span())
	}
	// cute partial call: capture evaluated args, one hole remains.
	// kept and holeAt are in *declaration* order — the order the
	// host callee's parameter list expects (declOrderVals is the
	// eval counterpart of lowering's declOrderArgs), and a hole
	// supplied under a label keeps its declaration slot. The hole
	// position precedes the spliced tail, so the flat indexes stay
	// aligned.
	holePos := x.Call.HoleIndex
	if x.Call.HoleIndex < len(x.Call.ArgToParam) {
		holePos = x.Call.ArgToParam[x.Call.HoleIndex]
	}
	kept := make([]Value, 0, len(ordered)-1)
	for i, v := range ordered {
		if i != holePos {
			kept = append(kept, v)
		}
	}
	g := funcOfType("", fnGoPartial, &types.Func{
		Params: []types.FuncParam{types.P(types.Unit)}, Ret: types.Unit})
	g.gocall = &goCall{sp: x.Span(), callee: f, args: kept, holeAt: holePos}
	return g
}

// declOrderVals reorders source-order argument values into
// declaration order per CallInfo.ArgToParam — the eval counterpart
// of lowering's declOrderArgs. An unrepresentable mapping
// (ArgToParam nil, e.g. a repeated variadic slot) keeps source
// order, which is also what the compiled path emits.
func declOrderVals(ci check.CallInfo, vals []Value) []Value {
	if ci.ArgToParam == nil {
		return vals
	}
	out := make([]Value, len(vals))
	for i, v := range vals {
		pos := i
		if i < len(ci.ArgToParam) {
			pos = ci.ArgToParam[i]
		}
		if pos >= 0 && pos < len(out) {
			out[pos] = v
		}
	}
	return out
}

// spliceGoSpread expands the collection the trailing `..e` argument
// evaluated to — the eval counterpart of the CallGo Spread flag,
// which codegen turns into `f(xs...)`. A List value contributes its
// elements directly; a Go slice that arrived through a host binding
// (a VHost) splices element-wise as further host values, the way
// `xs...` would hand the variadic parameter the slice's elements.
// Anything else is a checker-side contract break — spreadElem
// guarantees a List or Go.Slice.
func (e *interp) spliceGoSpread(ordered []Value, sp diag.Span) []Value {
	if len(ordered) == 0 {
		return ordered
	}
	var elems []Value
	ok := true
	switch c := ordered[len(ordered)-1].(type) {
	case *VList:
		elems = c.Elems
	case *VHost:
		if rv := reflect.ValueOf(c.V); rv.Kind() == reflect.Slice {
			elems = make([]Value, rv.Len())
			for i := range elems {
				elems[i] = &VHost{V: rv.Index(i).Interface()}
			}
		} else {
			ok = false
		}
	default:
		ok = false
	}
	if !ok {
		v := ordered[len(ordered)-1]
		var vs string
		if v != nil {
			vs = v.String()
		}
		failData(CodeInternal, "spread of a non-collection", sp, "value", vs)
	}
	out := make([]Value, 0, len(ordered)-1+len(elems))
	out = append(out, ordered[:len(ordered)-1]...)
	return append(out, elems...)
}

// evalFor implements `for pat in iter { body }` over List values —
// and over Go.Slice values, which arrive as *VHost wrapping the
// Go slice (the compiled path emits `for _, x := range s`, so eval
// must iterate the same elements; the reflect shape is the spread's).
func (e *interp) evalFor(x *check.For, env *Env) Value {
	it := e.eval(x.Iter, env)
	var elems []Value
	ok := true
	switch it := it.(type) {
	case *VList:
		elems = it.Elems
	case *VHost:
		if rv := reflect.ValueOf(it.V); rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			elems = make([]Value, rv.Len())
			for i := range elems {
				// Scalar elements unbox to their Value spelling so
				// the bound variable feeds arithmetic and match
				// like the compiled element does; anything else
				// stays an opaque host value.
				elems[i] = hostElem(x.Pat.Type(), rv.Index(i))
			}
		} else {
			ok = false
		}
	default:
		ok = false
	}
	if !ok {
		failData(CodeUnsupported, "for over a non-List iterable", x.Iter.Span(),
			"value", it.String())
	}
	for _, el := range elems {
		child := env.Child()
		if !e.matchPat(x.Pat, el, child) {
			failData(CodeInternal, "irrefutable for pattern failed", x.Pat.Span(),
				"value", el.String())
		}
		e.eval(x.Body, child)
	}
	return &VUnit{}
}

// hostElem boxes one element a host slice/array yields to `for`,
// keyed by the pattern's static element type: the scalar kinds
// become their Value spelling — an int64 element feeding `x + 1`
// must be a VInt like a Go call's result, not an opaque VHost —
// and everything else stays opaque (matching how host values move
// through the evaluator elsewhere).
func hostElem(t types.Type, rv reflect.Value) Value {
	if con, ok := types.Chase(t).(*types.Con); ok && con.Qual == "" {
		switch con.Name {
		case "Int", "Int64", "Int32", "Char":
			var n int64
			switch rv.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				n = rv.Int()
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
				n = int64(rv.Uint())
			default:
				return &VHost{V: rv.Interface()}
			}
			if con.Name == "Char" {
				return &VChar{R: rune(n)}
			}
			return &VInt{N: n}
		case "Float":
			switch rv.Kind() {
			case reflect.Float32:
				return &VFloat{F: rv.Float(), W: 32}
			case reflect.Float64:
				return &VFloat{F: rv.Float()}
			}
		case "Bool":
			if rv.Kind() == reflect.Bool {
				return &VBool{B: rv.Bool()}
			}
		case "String":
			if rv.Kind() == reflect.String {
				return &VStr{S: rv.String()}
			}
		}
	}
	return &VHost{V: rv.Interface()}
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

// displayTyped renders a value inside string interpolation with the
// splice's static type: a float at a float32-width position prints
// at float32 precision — `0.1`, not the float64 expansion of its
// canonical image — matching the compiled interpolation's
// float32-typed operand. Everything else renders through the
// type-driven record shape typedDisplayValue computes.
func (e *interp) displayTyped(v Value, t types.Type) string {
	if f, ok := v.(*VFloat); ok {
		if w := e.floatOperandWidth(t); w == 32 {
			return strconv.FormatFloat(f.F, 'g', -1, 32)
		}
	}
	return display(e.typedDisplayValue(v, t))
}

// typedDisplayValue wraps a scalar value into the record shape its
// static type declares, so interpolation renders declared-type
// values the way compiled gomShow does: type-driven, never
// construction-driven. A scalar bound through the literal rule
// (`let f : Flag = true`) is the same value as the record literal
// (`{ on = true }`) in generated code — `type Flag bool` carries no
// record at run time — and gomShow renders both as `{ on = true }`;
// the oracle's splice now matches instead of splitting on how the
// value was constructed (wave45 lane C). The wrap recurses through
// peel chains (`type M = {u: UserId}` renders `{ u = ({ v = 8 }) }`)
// and skips what does not peel: generic one-field records emit as
// structs, and values already composite carry their own shape.
func (e *interp) typedDisplayValue(v Value, t types.Type) Value {
	switch v := v.(type) {
	case *VUnit, *VBool, *VInt, *VFloat, *VChar, *VStr:
		c, ok := types.Chase(t).(*types.Con)
		if !ok || len(c.Args) != 0 {
			return v
		}
		ri := e.recordOf(c)
		if ri == nil || len(ri.fields) != 1 {
			return v
		}
		inner := e.typedDisplayValue(v, ri.fields[0].typ)
		return &VRecord{Type: c, Fields: []VField{{
			Index: ri.fields[0].index, Name: ri.fields[0].name,
			Sym: ri.fields[0].sym, V: inner,
		}}}
	case *VRecord:
		// Fields literal-conformed into declared types are scalars
		// here exactly like top-level bindings — wrap them by the
		// declared field type the way gomShow's field recursion
		// does.
		ri := e.recordOf(v.Type)
		if ri == nil {
			return v
		}
		// Declared field types are in terms of the record's type
		// parameters — instantiate them at the value's own type the
		// way reshape does, or a generic record passes the bare
		// parameter `a` down and the payload never wraps.
		sub := instSubst(ri.typ, v.Type)
		out := &VRecord{Type: v.Type}
		for _, f := range v.Fields {
			nf := f
			for _, fi := range ri.fields {
				if fi.index == f.Index {
					nf.V = e.typedDisplayValue(f.V, applySubst(fi.typ, sub))
					break
				}
			}
			out.Fields = append(out.Fields, nf)
		}
		return out
	case *VVariant:
		// Constructor payloads literal-conformed into declared
		// types are scalars exactly like record fields — wrap them
		// by the ctor's DECLARED field types instantiated at the
		// splice's static type. Site-derived infos are concrete for
		// whichever instantiation built them first, so they must
		// not reshape a different instantiation (`Some` at Flag
		// recasting a later `Some` at Bool) — the same discipline
		// reshape follows.
		if v.Ctor == nil {
			return v
		}
		con, ok := types.Chase(t).(*types.Con)
		if !ok {
			return v
		}
		info := e.ctorOf(v.Ctor, v.Name, v.Type)
		if info != nil && !info.declared {
			info = builtinDeclCtor(v.Ctor)
		}
		if info == nil || !info.declared {
			return v
		}
		sub := instSubst(info.typ, con)
		out := &VVariant{Type: v.Type, Ctor: v.Ctor, Name: v.Name}
		for _, f := range v.Fields {
			nf := f
			for _, fi := range info.fields {
				if fi.index == f.Index && fi.typ != nil {
					nf.V = e.typedDisplayValue(f.V, applySubst(fi.typ, sub))
					break
				}
			}
			out.Fields = append(out.Fields, nf)
		}
		return out
	case *VList:
		el := listElemType(t)
		if el == nil {
			return v
		}
		out := &VList{Elems: make([]Value, len(v.Elems))}
		for i, x := range v.Elems {
			out.Elems[i] = e.typedDisplayValue(x, el)
		}
		return out
	case *VTuple:
		tp, ok := types.Chase(t).(*types.Tuple)
		if !ok || len(tp.Elems) != len(v.Elems) {
			return v
		}
		out := &VTuple{Elems: make([]Value, len(v.Elems))}
		for i, x := range v.Elems {
			out.Elems[i] = e.typedDisplayValue(x, tp.Elems[i])
		}
		return out
	default:
		return v
	}
}

// listElemType extracts the element type of a List static type —
// nil for anything else.
func listElemType(t types.Type) types.Type {
	c, ok := types.Chase(t).(*types.Con)
	if ok && c.Qual == "" && c.Name == "List" && len(c.Args) == 1 {
		return c.Args[0]
	}
	return nil
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
	case *VTuple, *VList, *VRecord, *VVariant, *VGoErr, *VHost, *VFunc:
		return v.String()
	default:
		panic("unreachable")
	}
}
