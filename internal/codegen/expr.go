package codegen

import (
	"fmt"
	"go/ast"
	"go/token"
	"slices"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// This file renders Core IR atoms and single-expression nodes to
// go/ast expressions. Everything that compiles to statements lives in
// emit.go instead — the split is mechanical: operand positions hold
// only atoms, so a nested complex expression cannot occur inside one
// of these (docs/design.md section 2.1, item 1).

// atom renders an operand: Var, Lit, FuncRef, CtorRef, GoFuncRef,
// FieldGet.
func (g *gen) atom(a core.Atom) ast.Expr {
	switch a := a.(type) {
	case *core.Var:
		return g.varRef(a.Sym)
	case *core.Lit:
		return g.lit(a)
	case *core.FuncRef:
		return g.qualRef(a.Sym, a.TypeArgs)
	case *core.CtorRef:
		return g.ctorRef(a)
	case *core.GoFuncRef:
		return g.goFuncRef(a)
	case *core.FieldGet:
		return g.fieldGet(a)
	default:
		panic("unreachable")
	}
}

// selName renders the `Name` half of a `pkg.Name` reference: a Go
// member keeps its exported Go identifier verbatim (`O_RDONLY`
// stays `O_RDONLY`), a Gom package member follows the capitalize
// convention.
func selName(sym *resolve.Symbol) string {
	if sym.IsGo {
		return sym.Name
	}
	return exportName(sym.Name)
}

// varRef renders a variable reference: a bare name for file-local
// symbols, pkg.Name for symbols of another package (Gom or Go).
func (g *gen) varRef(sym *resolve.Symbol) ast.Expr {
	if sym.Package == "" {
		return ast.NewIdent(g.symName(sym))
	}
	return g.qualIdent(sym.Package, selName(sym))
}

// qualRef renders a reference to a top-level function or other
// package-level symbol, with optional type instantiation.
func (g *gen) qualRef(sym *resolve.Symbol, targs []types.Type) ast.Expr {
	var x ast.Expr
	if sym.Package == "" {
		x = ast.NewIdent(g.symName(sym))
	} else {
		x = g.qualIdent(sym.Package, selName(sym))
	}
	args := g.goTypeArgs(targs)
	switch len(args) {
	case 0:
		return x
	case 1:
		return &ast.IndexExpr{X: x, Index: args[0]}
	default:
		return &ast.IndexListExpr{X: x, Indices: args}
	}
}

// lit renders a literal. Int/Float/Str/Char keep the source lexeme —
// Gom and Go spell them alike. Unit is the zero-size struct value.
// Two spelling/type mismatches materialize the checked type around
// the lexeme — Go's default type for a bare literal would be wrong:
//
//   - an integer literal whose checked type is Float (`let m = 1`
//     with `m` solving to Float, or `1/2` under a Float binder)
//     would emit Go's `int` default — `m := 1` binds `int` and
//     `float64(1/2)` is `0` — so the literal emits `float64(1)`
//     (docs/problems/2026-11-02-let-int-literal-go-type.md);
//   - a literal checked at a named Go type (`time.Duration`) —
//     same default-type trap, fixed below.
func (g *gen) lit(l *core.Lit) ast.Expr {
	var x ast.Expr
	switch l.Kind {
	case syntax.LitInt:
		x = &ast.BasicLit{Kind: token.INT, Value: l.Text}
		if g.floatTyped(l.Type()) {
			return &ast.CallExpr{Fun: g.goType(l.Type()), Args: []ast.Expr{x}}
		}
	case syntax.LitFloat:
		x = &ast.BasicLit{Kind: token.FLOAT, Value: l.Text}
	case syntax.LitStr:
		x = &ast.BasicLit{Kind: token.STRING, Value: l.Text}
	case syntax.LitChar:
		x = &ast.BasicLit{Kind: token.CHAR, Value: l.Text}
	case syntax.LitBool:
		x = ast.NewIdent(l.Text)
	case syntax.LitUnit:
		// `struct{}{}` — spelled as an identifier because an empty
		// positional struct type does not print cleanly (types.go).
		return ast.NewIdent("struct{}{}")
	default:
		panic("unreachable")
	}
	// A literal checked at a named Go type materializes it —
	// `d := 1 + 2` under a `time.Duration` binder would otherwise
	// bind Go's default `int`, and `1 / 2` under a named float
	// would be integer division. `T(lit)` keeps both the type and
	// the arithmetic semantics (decision
	// 2026-11-17-named-int-arith).
	if _, ok := types.Chase(l.Type()).(*types.Go); ok {
		return &ast.CallExpr{Fun: g.goType(l.Type()), Args: []ast.Expr{x}}
	}
	return x
}

// goFuncRef renders a Go function or bound method used as a value —
// legal directly only when the signature needs no adaptation (the
// validator enforces arity 1 + single result).
func (g *gen) goFuncRef(a *core.GoFuncRef) ast.Expr {
	if a.Recv != nil {
		return &ast.SelectorExpr{X: g.atom(a.Recv), Sel: ast.NewIdent(a.Sym.Name)}
	}
	return g.qualRef(a.Sym, a.TypeArgs)
}

// fieldGet renders `recv.Name` for named fields and `recv.Vn` for
// positional payload slots and tuple elements. A named payload read
// through the sealed sum interface (Ctor set) first asserts the
// receiver to the owning variant's concrete struct —
// `recv.(Shape_Circle).R` — which panics when the value holds a
// different variant (spec 3.2, decision
// 2026-11-05-variant-field-assert-owning-ctor). The evaluator
// mirrors the assertion: evalMember checks the ctor identity first
// and reports E9003 on a wrong-variant value.
func (g *gen) fieldGet(a *core.FieldGet) ast.Expr {
	var recv ast.Expr = g.atom(a.Recv)
	if a.Ctor != nil {
		recv = &ast.TypeAssertExpr{
			X:    recv,
			Type: g.ctorGoType(a.Ctor, a.Recv.Type()),
		}
	}
	if a.Unwrap {
		// One-field record → `type X F`: `r.f` unwraps as `F(r)`.
		return &ast.CallExpr{
			Fun:  g.goType(a.Type()),
			Args: []ast.Expr{recv},
		}
	}
	sel := fmt.Sprintf("V%d", a.Index)
	if a.Field != nil {
		if a.Field.IsGo {
			sel = a.Field.Name
		} else {
			sel = exportName(a.Field.Name)
		}
	}
	return &ast.SelectorExpr{X: recv, Sel: ast.NewIdent(sel)}
}

// ctorRef materializes a constructor used as a value: a function
// literal `func(v0, ...) T { return T_C{...} }` wrapping the variant
// construction. The builtin Result ctors are the exception — Ok and
// Err exist as real generic functions in stdlib/result (the Result
// representation keeps its tag private, so no `Result_Ok` struct is
// ever emitted), and both take exactly one payload argument, so the
// instantiated function value `result.Ok[T, E]` serves directly.
func (g *gen) ctorRef(a *core.CtorRef) ast.Expr {
	fn, ok := types.Chase(a.Type()).(*types.Func)
	if !ok {
		panic("codegen: CtorRef type is not a function type")
	}
	if isResultCon(fn.Ret) && !g.localT["Result"] {
		x := ast.Expr(ast.NewIdent(g.pkgQual(resultPath) + exportName(a.Sym.Name)))
		switch args := g.goTypeArgs(ctorTArgs(fn.Ret)); len(args) {
		case 0:
			// No instantiation info — the bare generic name still
			// resolves where the target type guides inference.
			return x
		case 1:
			return &ast.IndexExpr{X: x, Index: args[0]}
		default:
			return &ast.IndexListExpr{X: x, Indices: args}
		}
	}
	params, args := g.variantParams(fn)
	lit := g.ctorLit(a.Sym, fn.Ret, args, a.Fields)
	// Canonical value representation: one func literal per
	// parameter — `func(v0) func(v1) ... T { return T_C{...} }` —
	// so a constructor reference has the same Go shape as every
	// other function value of its type.
	var x ast.Expr = lit
	for i := range slices.Backward(fn.Params) {
		resTyp := ast.Expr(g.goType(fn.Ret))
		if i+1 < len(fn.Params) {
			resTyp = g.goType(&types.Func{Params: fn.Params[i+1:], Ret: fn.Ret})
		}
		x = &ast.FuncLit{
			Type: &ast.FuncType{
				Params:  &ast.FieldList{List: []*ast.Field{params[i]}},
				Results: &ast.FieldList{List: []*ast.Field{{Type: resTyp}}},
			},
			Body: &ast.BlockStmt{List: []ast.Stmt{
				&ast.ReturnStmt{Results: []ast.Expr{x}},
			}},
		}
	}
	return x
}

// variantParams builds `v0 T0, v1 T1` parameter fields and the
// matching argument list for a constructor's curried signature. The
// instantiated types come from the type's own parameter list — a
// ctor's TypeArgs instantiate the ADT, and the params already carry
// the substituted types when the resolver produced them.
func (g *gen) variantParams(fn *types.Func) (params []*ast.Field, args []ast.Expr) {
	params = make([]*ast.Field, 0, len(fn.Params))
	args = make([]ast.Expr, 0, len(fn.Params))
	for i, p := range fn.Params {
		v := ast.NewIdent(fmt.Sprintf("v%d", i))
		params = append(params, &ast.Field{Names: []*ast.Ident{v}, Type: g.goType(p.Type)})
		args = append(args, v)
	}
	return params, args
}

// simpleExpr renders an expression that fits in a single Go
// expression — everything that is not a statement-level node. The
// set is exactly the non-statement nodes of emit.go's switch.
func (g *gen) simpleExpr(e core.Expr) ast.Expr {
	switch e := e.(type) {
	case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
		*core.GoFuncRef, *core.FieldGet:
		return g.atom(e.(core.Atom))
	case *core.Unary:
		// A negated int literal at a named Go type puts the sign
		// inside the conversion — `-Duration(9223372036854775808)`
		// overflows the positive magnitude before negating, while
		// `Duration(-9223372036854775808)` is a legal constant.
		if e.Op == syntax.UnNeg {
			if l, ok := e.X.(*core.Lit); ok && l.Kind == syntax.LitInt {
				if _, named := types.Chase(l.Type()).(*types.Go); named {
					return &ast.CallExpr{Fun: g.goType(l.Type()), Args: []ast.Expr{
						&ast.UnaryExpr{Op: token.SUB, X: &ast.BasicLit{Kind: token.INT, Value: l.Text}},
					}}
				}
			}
		}
		return &ast.UnaryExpr{Op: unOp(e.Op), X: g.atom(e.X)}
	case *core.Binary:
		return &ast.BinaryExpr{X: g.atom(e.X), Op: binOp(e.Op), Y: g.atom(e.Y)}
	case *core.Equal:
		return g.equalExpr(e)
	case *core.CallDirect:
		return &ast.CallExpr{Fun: g.qualRef(e.Callee, e.TypeArgs), Args: g.atoms(e.Args)}
	case *core.CallFunc:
		return &ast.CallExpr{Fun: g.atom(e.Fn), Args: g.atoms(e.Args)}
	case *core.CallGo:
		return g.callGo(e)
	case *core.CallBuiltin:
		return g.callBuiltin(e)
	case *core.CtorApp:
		return g.ctorApp(e)
	case *core.TupleNew:
		return g.tupleNew(e)
	case *core.ListNew:
		return g.listNew(e)
	case *core.RecordNew:
		return g.recordNew(e)
	case *core.Conv:
		return g.conv(e)
	case *core.Closure, *core.GoFunc, *core.Uncurry, *core.If, *core.Match, *core.SelectExpr,
		*core.Spawn, *core.ForRange, *core.TailCall, *core.FieldSet,
		*core.EscapeResult, *core.EscapeCall,
		*core.ResultFromGo, *core.OptionFromGo, *core.TupleFromGo,
		*core.ErrorFromGo, *core.RecordUpdate, *core.Defer, *core.TryAs:
		// statement-level nodes (emit.go) — reached only if
		// isSimpleExpr's split is wrong
		panic(fmt.Sprintf("codegen: %T is not a single-expression node", e))
	default:
		panic("unreachable")
	}
}

// isSimpleExpr reports whether e renders as a single expression —
// the simpleExpr/emit split point.
func isSimpleExpr(e core.Expr) bool {
	switch e.(type) {
	case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
		*core.GoFuncRef, *core.FieldGet,
		*core.Unary, *core.Binary, *core.Equal,
		*core.CallDirect, *core.CallFunc, *core.CallGo, *core.CallBuiltin,
		*core.CtorApp, *core.TupleNew, *core.ListNew, *core.RecordNew,
		*core.Conv, *core.Closure, *core.GoFunc, *core.Uncurry:
		return true
	case *core.If, *core.Match, *core.SelectExpr,
		*core.Spawn, *core.ForRange, *core.TailCall, *core.FieldSet,
		*core.EscapeResult, *core.EscapeCall,
		*core.ResultFromGo, *core.OptionFromGo, *core.TupleFromGo,
		*core.ErrorFromGo, *core.RecordUpdate, *core.Defer, *core.TryAs:
		return false
	default:
		panic("unreachable")
	}
}

func (g *gen) atoms(as []core.Atom) []ast.Expr {
	out := make([]ast.Expr, len(as))
	for i, a := range as {
		out[i] = g.atom(a)
	}
	return out
}

// callGo renders `pkg.F(a, b)`, `recv.M(a)`, the func-value call
// `f(a, b)` (`Go.fnN`'s adapter), or the spread `f(a...)`.
func (g *gen) callGo(c *core.CallGo) ast.Expr {
	// `Chan.make` lowers to a bare `make(chan T, int(n))` rather than
	// the rt.MakeChan helper: vet's sigchanyzer cannot see the
	// capacity through the generic call and flags
	// `MakeChan[os.Signal](1)` as an unbuffered channel misuse.
	// The direct builtin is identical — rt.MakeChan's own body is
	// `make(chan T, int(n))`, including the native-width narrowing
	// on 32-bit targets and the negative-capacity panic. The
	// convention arrives on Core: CallGo.ChanMake is set by lowering.
	if c.ChanMake {
		return &ast.CallExpr{Fun: ast.NewIdent("make"),
			Args: []ast.Expr{g.goType(c.Type()),
				&ast.CallExpr{Fun: ast.NewIdent("int"), Args: []ast.Expr{g.atom(c.Args[0])}}}}
	}
	var fun ast.Expr
	switch {
	case c.Fn != nil:
		fun = g.atom(c.Fn)
	case c.Recv != nil:
		fun = &ast.SelectorExpr{X: g.atom(c.Recv), Sel: ast.NewIdent(c.Callee.Name)}
	default:
		fun = g.qualRef(c.Callee, c.TypeArgs)
	}
	call := &ast.CallExpr{Fun: fun, Args: g.atoms(c.Args)}
	if c.Spread {
		call.Ellipsis = token.Pos(1)
	}
	return call
}

// callBuiltin renders a compiler builtin from a fixed table
// dispatched on the symbol's name (docs/design.md — no semantic
// choice here; the table is the node's own spec).
func (g *gen) callBuiltin(c *core.CallBuiltin) ast.Expr {
	switch c.Sym.Name {
	case "assert_eq":
		// testutil.AssertEq(t, eq, actual, expected) — eq is the verdict
		// under the operands' own recipe (core.CallBuiltin.Eq),
		// matching what `actual == expected` evaluates to; the value
		// operands go through the operand type's display recipe
		// (core.CallBuiltin.Show, docs/design.md section 4.8) so the
		// failure reports Gom-shaped values.
		var eq ast.Expr
		if c.Eq != nil && len(c.Args) >= 2 {
			eq = g.eqExpr(c.Eq, g.atom(c.Args[0]), g.atom(c.Args[1]))
		} else {
			// A plan-less assert_eq is a contract gap — degrade to
			// the runtime fallback rather than miscompile.
			eq = &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "Equal"),
				Args: g.atoms(c.Args)}
		}
		args := []ast.Expr{ast.NewIdent("t"), eq}
		for _, a := range c.Args {
			args = append(args, g.showExpr(c.Show, g.atom(a)))
		}
		// Raw operands ride along for go-cmp's structural failure
		// diff; equality still uses the Gom recipe verdict above.
		for _, a := range c.Args {
			args = append(args, g.atom(a))
		}
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(testutilPath) + "AssertEq"),
			Args: args}
	case "assert":
		// rt.Assert(t, cond)
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "Assert"),
			Args: append([]ast.Expr{ast.NewIdent("t")}, g.atoms(c.Args)...)}
	case "snapshot":
		// testutil.Snapshot(t, key, rendered value)
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(testutilPath) + "Snapshot"),
			Args: []ast.Expr{ast.NewIdent("t"), strLit(g.testDeclName), g.atom(c.Args[1]), g.showExpr(c.Show, g.atom(c.Args[0]))}}
	case "todo":
		// rt.Todo[T]() panics at the hole.
		fun := &ast.IndexExpr{X: ast.NewIdent(g.pkgQual(rtPath) + "Todo"),
			Index: g.goType(c.Type())}
		return &ast.CallExpr{Fun: fun}
	case "nil":
		// `Go.nil ()` — rt.Nil[T]() yields the nil value of the
		// nil-able type the checker proved (decision
		// 2026-11-23-nil-spelling).
		fun := &ast.IndexExpr{X: ast.NewIdent(g.pkgQual(rtPath) + "Nil"),
			Index: g.goType(c.Type())}
		return &ast.CallExpr{Fun: fun}
	case "not":
		// `not x` is the bool negation `!x`.
		return &ast.UnaryExpr{Op: token.NOT, X: g.atom(c.Args[0])}
	case "str_concat":
		// The parts of an interpolated string: `a + b + ...`.
		return g.strConcat(c)
	case "display":
		// A splice's value rendered per its static type.
		return g.display(c)
	default:
		panic(fmt.Sprintf("codegen: unknown builtin %q", c.Sym.Name))
	}
}

// strConcat renders `str_concat a b ...` as the `a + b + ...` chain —
// lowering only ever feeds it strings. A single part needs no
// operator; zero parts degrade to the empty literal.
func (g *gen) strConcat(c *core.CallBuiltin) ast.Expr {
	if len(c.Args) == 0 {
		return &ast.BasicLit{Kind: token.STRING, Value: `""`}
	}
	x := g.atom(c.Args[0])
	for _, a := range c.Args[1:] {
		x = &ast.BinaryExpr{X: x, Op: token.ADD, Y: g.atom(a)}
	}
	return x
}

// display renders `display x`: the value a `"${...}"` splice shows.
// Scalars go through strconv so generated output matches the
// evaluator's display format exactly (internal/eval's display: Int
// in decimal, Float in 'g' shortest form, Char as the code point
// itself, String unquoted). Composite values (ADTs, records, lists,
// tuples, function values, Go types) reuse the gomShow machinery:
// lowering fixes a Show plan on the call (design.md section 4.8),
// so a composite splice emits the same gomShowN the test half's
// assert_eq uses — matching the evaluator's comparison format.
// Anything the plan leaves to the runtime still uses the
// representation-driven runtime fallback for composite recipes; a
// top-level type-erased splice keeps fmt's %v.
func (g *gen) display(c *core.CallBuiltin) ast.Expr {
	a := c.Args[0]
	x := g.atom(a)
	if con, ok := types.Chase(a.Type()).(*types.Con); ok && con.Qual == "" &&
		!g.localT[exportName(con.Name)] {
		// The name switch only fires on builtin spellings — a
		// file-local `type Bool`/`type Int` is an ADT and takes the
		// fmt fallback like every other composite.
		switch con.Name {
		case "String":
			return x
		case "Unit":
			return &ast.BasicLit{Kind: token.STRING, Value: `"()"`}
		case "Bool":
			return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual("strconv") + "FormatBool"), Args: []ast.Expr{x}}
		case "Int", "Int64":
			return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual("strconv") + "FormatInt"),
				Args: []ast.Expr{x, &ast.BasicLit{Kind: token.INT, Value: "10"}}}
		case "Int32":
			return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual("strconv") + "FormatInt"),
				Args: []ast.Expr{
					&ast.CallExpr{Fun: ast.NewIdent("int64"), Args: []ast.Expr{x}},
					&ast.BasicLit{Kind: token.INT, Value: "10"}}}
		case "Float":
			return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual("strconv") + "FormatFloat"),
				Args: []ast.Expr{x,
					&ast.BasicLit{Kind: token.CHAR, Value: `'g'`},
					&ast.BasicLit{Kind: token.INT, Value: "-1"},
					&ast.BasicLit{Kind: token.INT, Value: "64"}}}
		case "Char":
			return &ast.CallExpr{Fun: ast.NewIdent("string"), Args: []ast.Expr{x}}
		}
	}
	if p := c.Show; p != nil {
		switch p.Kind {
		case core.ShowScalar, core.ShowGoScalar:
			return g.displayScalar(p.Scalar, x)
		case core.ShowCall:
			// A composite recipe — the minted gomShowN renders
			// the evaluator's comparison format.
			return &ast.CallExpr{Fun: ast.NewIdent(g.symName(p.Fn.Sym)), Args: []ast.Expr{x}}
		case core.ShowError:
			return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "ShowError"), Args: []ast.Expr{x}}
		case core.ShowRuntime:
			return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "ShowDisplay"), Args: []ast.Expr{x}}
		}
	}
	return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual("fmt") + "Sprint"), Args: []ast.Expr{x}}
}

// displayScalar renders a scalar for string interpolation — unlike
// the comparison display, strings stay unquoted and Char prints the
// code point itself. Named Go scalars convert through their
// underlying basic type first.
func (g *gen) displayScalar(name string, x ast.Expr) ast.Expr {
	if name == "go:string" {
		return &ast.CallExpr{Fun: ast.NewIdent("string"), Args: []ast.Expr{x}}
	}
	switch name {
	case "String":
		return x
	case "Char":
		return &ast.CallExpr{Fun: ast.NewIdent("string"), Args: []ast.Expr{x}}
	default:
		return g.showScalar(name, x)
	}
}

// ctorApp renders a saturated constructor application: a variant
// struct literal `T_C{F0: a, ...}` — or `result.Ok/Err[T, E](a)` for
// the builtin Result, whose representation keeps the tag private.
func (g *gen) ctorApp(e *core.CtorApp) ast.Expr {
	if isResultCon(e.Type()) && !g.localT["Result"] {
		name := g.pkgQual(resultPath) + exportName(e.Ctor.Name) // result.Ok / result.Err
		fun := ast.Expr(ast.NewIdent(name))
		args := g.goTypeArgs(ctorTArgs(e.Type()))
		switch len(args) {
		case 0:
			// No instantiation anywhere — the bare name is better
			// than `result.Ok[]` even though a missing [T, E] is a
			// malformed node the validator should have caught.
		case 1:
			fun = &ast.IndexExpr{X: fun, Index: args[0]}
		default:
			fun = &ast.IndexListExpr{X: fun, Indices: args}
		}
		return &ast.CallExpr{Fun: fun, Args: g.atoms(e.Args)}
	}
	return g.ctorLit(e.Ctor, e.Type(), g.atoms(e.Args), e.Fields)
}

// ctorTArgs returns a constructor's ADT instantiation: the result
// type's own arguments — a saturated application always lands on a
// Con. CtorRef's function type resolves to the same Con through
// resultCon's chase.
func ctorTArgs(ret types.Type) []types.Type {
	if c, ok := types.Chase(ret).(*types.Con); ok {
		return c.Args
	}
	return nil
}

// ctorLit renders `Type_Ctor{F0: a0, ...}` — the sealed-variant
// struct literal. Named payload fields use their names; positional
// slots use V0, V1, ... The node-carried fields win — they cover
// constructors no file-local declaration table describes (sibling-
// file and imported ADTs) — with g.ctors as the fallback.
func (g *gen) ctorLit(sym *resolve.Symbol, typ types.Type, args []ast.Expr, fields []core.TField) ast.Expr {
	t := g.ctorGoType(sym, typ)
	elts := make([]ast.Expr, 0, len(args))
	if fields == nil {
		if d, ok := g.ctors[sym]; ok {
			fields = d.fields
		}
	}
	for i, a := range args {
		name := fmt.Sprintf("V%d", i)
		if i < len(fields) {
			name = g.tfieldName(&fields[i])
		}
		elts = append(elts, &ast.KeyValueExpr{Key: ast.NewIdent(name), Value: a})
	}
	return &ast.CompositeLit{Type: t, Elts: elts}
}

// ctorGoType renders the variant struct's type name —
// `Shape_Circle`, `Shape_Circle[A]`, `option.Option_Some[A]` —
// deriving the owning ADT from the node's result type.
func (g *gen) ctorGoType(sym *resolve.Symbol, typ types.Type) ast.Expr {
	typeName := ""
	if d, ok := g.ctors[sym]; ok {
		typeName = d.typeName
	} else if c, ok := types.Chase(typ).(*types.Con); ok {
		typeName = c.Name
	}
	variant := exportName(typeName) + "_" + exportName(sym.Name)

	var x ast.Expr
	switch {
	case g.ctors[sym] != nil:
		// local variant
		x = ast.NewIdent(variant)
	case typeName == "Option" && !g.localT["Option"]:
		x = ast.NewIdent(g.pkgQual(optionPath) + variant)
	default:
		if c, ok := types.Chase(typ).(*types.Con); ok && c.Qual != "" {
			x = ast.NewIdent(g.pkgQual(c.Qual) + variant)
		} else {
			x = ast.NewIdent(variant)
		}
	}
	args := g.goTypeArgs(ctorTArgs(typ))
	switch len(args) {
	case 0:
		return x
	case 1:
		return &ast.IndexExpr{X: x, Index: args[0]}
	default:
		return &ast.IndexListExpr{X: x, Indices: args}
	}
}

// tfieldName renders a declared field name — `radius` → `Radius`,
// `~label` → `Label` — or `V<Index>` for positional payload.
func (g *gen) tfieldName(f *core.TField) string {
	if f.Name != "" {
		return exportName(f.Name)
	}
	if f.Label != "" {
		return exportName(f.Label)
	}
	return fmt.Sprintf("V%d", f.Index)
}

// tupleNew renders `(a, b, ...)` as an rt.TupleN literal.
func (g *gen) tupleNew(e *core.TupleNew) ast.Expr {
	tup, ok := types.Chase(e.Type()).(*types.Tuple)
	if !ok {
		panic("codegen: TupleNew type is not a tuple")
	}
	elts := make([]ast.Expr, len(e.Elems))
	for i, a := range e.Elems {
		elts[i] = &ast.KeyValueExpr{Key: ast.NewIdent(fmt.Sprintf("V%d", i)), Value: g.atom(a)}
	}
	return &ast.CompositeLit{Type: g.tupleType(tup), Elts: elts}
}

// listNew renders `[e, ...]` as a list.List literal and
// `[e, ..rest]` as `append(list.List[T]{e...}, rest...)` — the O(n)
// construction of the slice representation (P0-3, spec section
// 3.4's `[x, ..rest]`).
func (g *gen) listNew(e *core.ListNew) ast.Expr {
	lit := &ast.CompositeLit{Type: g.goType(e.Type()), Elts: g.atoms(e.Elems)}
	if e.Rest == nil {
		return lit
	}
	return &ast.CallExpr{Fun: ast.NewIdent("append"),
		Args:     []ast.Expr{lit, g.atom(e.Rest)},
		Ellipsis: token.Pos(1)}
}

// recordNew renders `{ name = e, ... }` as a keyed struct literal.
// One-field records never reach here: they emit as `type X F` and
// lower already turned the literal into a `core.Conv`.
func (g *gen) recordNew(e *core.RecordNew) ast.Expr {
	elts := make([]ast.Expr, len(e.Fields))
	for i, f := range e.Fields {
		elts[i] = &ast.KeyValueExpr{
			Key:   ast.NewIdent(exportName(f.Sym.Name)),
			Value: g.atom(f.Value),
		}
	}
	return &ast.CompositeLit{Type: g.goType(e.Type()), Elts: elts}
}

// conv renders the explicit Go-interop conversions: `T(x)` for the
// numeric and named conversions and interface assignment, `any(x)`
// for boxing, and the boundary slice copies.
func (g *gen) conv(e *core.Conv) ast.Expr {
	x := g.atom(e.X)
	switch e.Kind {
	case core.ConvNum, core.ConvNamed, core.ConvIface:
		fun := g.goType(e.Type())
		if _, isStar := fun.(*ast.StarExpr); isStar {
			// `*T(x)` parses as a dereference — pointer targets
			// (`type P = {p: Ptr Int}` peels to `Ptr Int`) need
			// parens in call position.
			fun = &ast.ParenExpr{X: fun}
		}
		return &ast.CallExpr{Fun: fun, Args: []ast.Expr{x}}
	case core.ConvChanDir:
		// The target spells `chan<- T` or `<-chan T` — both need
		// parens in call position (`<-chan T(x)` would read as a
		// receive from the conversion `chan T(x)`). Call-argument
		// narrowing never reaches here: convGoArg skips the Conv
		// node outright, both because `chan T` is already
		// assignable and because the conversion shape defeats
		// `go vet`'s sigchanyzer tracking (a buffered `make`
		// behind `(chan<- os.Signal)(c)` reports as unbuffered).
		return &ast.CallExpr{Fun: &ast.ParenExpr{X: g.goType(e.Type())}, Args: []ast.Expr{x}}
	case core.ConvBoxAny:
		return &ast.CallExpr{Fun: ast.NewIdent("any"), Args: []ast.Expr{x}}
	case core.ConvSliceToGo:
		return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: ast.NewIdent("ToSlice")}}
	case core.ConvSliceFromGo:
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(listPath) + "FromSlice"), Args: []ast.Expr{x}}
	case core.ConvSliceBox:
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "BoxSlice"), Args: []ast.Expr{x}}
	case core.ConvOptNil:
		// `option.FromGo(v, v != nil)` — the nil-able payload
		// read of a `Result (Option T) GoError` boundary (the
		// `(error, error)` derived view among them, spec 8.4).
		return &ast.CallExpr{
			Fun:  ast.NewIdent(g.pkgQual(optionPath) + "FromGo"),
			Args: []ast.Expr{x, &ast.BinaryExpr{X: x, Op: token.NEQ, Y: ast.NewIdent("nil")}},
		}
	case core.ConvOptToNil:
		// `option.NilOr(v)` — the write side of the `(T, error)`
		// boundary: `Some` is the payload, `None` the nil.
		return &ast.CallExpr{
			Fun:  ast.NewIdent(g.pkgQual(optionPath) + "NilOr"),
			Args: []ast.Expr{x},
		}
	default:
		panic("unreachable")
	}
}

func unOp(op syntax.UnOp) token.Token {
	switch op {
	case syntax.UnNeg:
		return token.SUB
	case syntax.UnNot:
		return token.NOT
	default:
		panic("unreachable")
	}
}

func binOp(op syntax.BinOp) token.Token {
	switch op {
	case syntax.BinAdd:
		return token.ADD
	case syntax.BinSub:
		return token.SUB
	case syntax.BinMul:
		return token.MUL
	case syntax.BinDiv:
		return token.QUO
	case syntax.BinMod:
		return token.REM
	case syntax.BinEq:
		return token.EQL
	case syntax.BinNe:
		return token.NEQ
	case syntax.BinLt:
		return token.LSS
	case syntax.BinLe:
		return token.LEQ
	case syntax.BinGt:
		return token.GTR
	case syntax.BinGe:
		return token.GEQ
	case syntax.BinAnd:
		return token.LAND
	case syntax.BinOr:
		return token.LOR
	case syntax.BinBitAnd:
		return token.AND
	case syntax.BinBitAndNot:
		return token.AND_NOT
	case syntax.BinBitOr:
		return token.OR
	case syntax.BinBitXor:
		return token.XOR
	case syntax.BinBitShl:
		return token.SHL
	case syntax.BinBitShr:
		return token.SHR
	default:
		panic("unreachable")
	}
}

// isResultCon reports whether t is the builtin `Result` type — the
// one constructor family whose Go representation is functions, not a
// struct literal.
func isResultCon(t types.Type) bool {
	c, ok := types.Chase(t).(*types.Con)
	return ok && c.Qual == "" && c.Name == "Result"
}
