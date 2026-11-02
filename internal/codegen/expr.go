package codegen

import (
	"fmt"
	"go/ast"
	"go/token"

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
	return &ast.SelectorExpr{X: ast.NewIdent(g.imp(sym.Package)), Sel: ast.NewIdent(selName(sym))}
}

// qualRef renders a reference to a top-level function or other
// package-level symbol, with optional type instantiation.
func (g *gen) qualRef(sym *resolve.Symbol, targs []types.Type) ast.Expr {
	var x ast.Expr
	if sym.Package == "" {
		x = ast.NewIdent(g.symName(sym))
	} else {
		x = &ast.SelectorExpr{X: ast.NewIdent(g.imp(sym.Package)), Sel: ast.NewIdent(selName(sym))}
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
func (g *gen) lit(l *core.Lit) ast.Expr {
	switch l.Kind {
	case syntax.LitInt:
		return &ast.BasicLit{Kind: token.INT, Value: l.Text}
	case syntax.LitFloat:
		return &ast.BasicLit{Kind: token.FLOAT, Value: l.Text}
	case syntax.LitStr:
		return &ast.BasicLit{Kind: token.STRING, Value: l.Text}
	case syntax.LitChar:
		return &ast.BasicLit{Kind: token.CHAR, Value: l.Text}
	case syntax.LitBool:
		return ast.NewIdent(l.Text)
	case syntax.LitUnit:
		// `struct{}{}` — spelled as an identifier because an empty
		// positional struct type does not print cleanly (types.go).
		return ast.NewIdent("struct{}{}")
	default:
		panic("unreachable")
	}
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
// different variant. The spec does not define the wrong-variant
// case; the evaluator answers a fieldless variant with "variant has
// no such field" but reads the wrong variant's same-index slot when
// one exists — the divergence is filed in
// docs/problems/2026-11-02-variant-field-wrong-variant.md.
func (g *gen) fieldGet(a *core.FieldGet) ast.Expr {
	sel := fmt.Sprintf("V%d", a.Index)
	if a.Field != nil {
		if a.Field.IsGo {
			sel = a.Field.Name
		} else {
			sel = exportName(a.Field.Name)
		}
	}
	var recv ast.Expr = g.atom(a.Recv)
	if a.Ctor != nil {
		var targs []types.Type
		if con, ok := types.Chase(a.Recv.Type()).(*types.Con); ok {
			targs = con.Args
		}
		recv = &ast.TypeAssertExpr{
			X:    recv,
			Type: g.ctorGoType(a.Ctor, a.Recv.Type(), targs),
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
		switch args := g.goTypeArgs(ctorTArgs(a.TypeArgs, fn.Ret)); len(args) {
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
	params, args := g.variantParams(fn, a.TypeArgs)
	ret := g.goType(fn.Ret)
	lit := g.ctorLit(a.Sym, fn.Ret, a.TypeArgs, args)
	return &ast.FuncLit{
		Type: &ast.FuncType{
			Params:  &ast.FieldList{List: params},
			Results: &ast.FieldList{List: []*ast.Field{{Type: ret}}},
		},
		Body: &ast.BlockStmt{List: []ast.Stmt{
			&ast.ReturnStmt{Results: []ast.Expr{lit}},
		}},
	}
}

// variantParams builds `v0 T0, v1 T1` parameter fields and the
// matching argument list for a constructor's curried signature. The
// instantiated types come from the type's own parameter list — a
// ctor's TypeArgs instantiate the ADT, and the params already carry
// the substituted types when the resolver produced them.
func (g *gen) variantParams(fn *types.Func, targs []types.Type) (params []*ast.Field, args []ast.Expr) {
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
		return &ast.UnaryExpr{Op: unOp(e.Op), X: g.atom(e.X)}
	case *core.Binary:
		return &ast.BinaryExpr{X: g.atom(e.X), Op: binOp(e.Op), Y: g.atom(e.Y)}
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
	case *core.Closure, *core.If, *core.Match, *core.SelectExpr,
		*core.Spawn, *core.ForRange, *core.TailCall,
		*core.EscapeResult, *core.EscapeCall,
		*core.ResultFromGo, *core.OptionFromGo, *core.TupleFromGo,
		*core.RecordUpdate:
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
		*core.Unary, *core.Binary,
		*core.CallDirect, *core.CallFunc, *core.CallGo, *core.CallBuiltin,
		*core.CtorApp, *core.TupleNew, *core.ListNew, *core.RecordNew,
		*core.Conv, *core.Closure:
		return true
	case *core.If, *core.Match, *core.SelectExpr,
		*core.Spawn, *core.ForRange, *core.TailCall,
		*core.EscapeResult, *core.EscapeCall,
		*core.ResultFromGo, *core.OptionFromGo, *core.TupleFromGo,
		*core.RecordUpdate:
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

// callGo renders `pkg.F(a, b)`, `recv.M(a)`, or the spread `f(a...)`.
func (g *gen) callGo(c *core.CallGo) ast.Expr {
	var fun ast.Expr
	if c.Recv != nil {
		fun = &ast.SelectorExpr{X: g.atom(c.Recv), Sel: ast.NewIdent(c.Callee.Name)}
	} else {
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
		// rt.AssertEq(t, actual, expected)
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "AssertEq"),
			Args: append([]ast.Expr{ast.NewIdent("t")}, g.atoms(c.Args)...)}
	case "assert":
		// rt.Assert(t, cond)
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "Assert"),
			Args: append([]ast.Expr{ast.NewIdent("t")}, g.atoms(c.Args)...)}
	case "todo":
		// rt.Todo[T]() panics at the hole.
		fun := &ast.IndexExpr{X: ast.NewIdent(g.pkgQual(rtPath) + "Todo"),
			Index: g.goType(c.Type())}
		return &ast.CallExpr{Fun: fun}
	case "not":
		// `not x` is the bool negation `!x`.
		return &ast.UnaryExpr{Op: token.NOT, X: g.atom(c.Args[0])}
	case "eq":
		// rt.Equal(x, y) — structural `==` on representations Go's
		// own == cannot compare (spec 3.3; lowering decides which
		// operand types route here).
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "Equal"),
			Args: g.atoms(c.Args)}
	case "ne":
		// `x != y` lowers to `not (eq x y)` — `!rt.Equal(x, y)`.
		return &ast.UnaryExpr{Op: token.NOT,
			X: &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "Equal"),
				Args: g.atoms(c.Args)}}
	case "str_concat":
		// The parts of an interpolated string: `a + b + ...`.
		return g.strConcat(c)
	case "display":
		// A splice's value rendered per its static type.
		return g.display(c.Args[0])
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
// tuples, function values, Go types) have no generated display
// function yet — they fall back to fmt's %v, whose rendering
// diverges from the evaluator's comparison format
// (docs/problems/2026-11-04-interp-composite-display.md).
func (g *gen) display(a core.Atom) ast.Expr {
	x := g.atom(a)
	if con, ok := types.Chase(a.Type()).(*types.Con); ok && con.Qual == "" {
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
	return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual("fmt") + "Sprint"), Args: []ast.Expr{x}}
}

// ctorApp renders a saturated constructor application: a variant
// struct literal `T_C{F0: a, ...}` — or `result.Ok/Err[T, E](a)` for
// the builtin Result, whose representation keeps the tag private.
func (g *gen) ctorApp(e *core.CtorApp) ast.Expr {
	if isResultCon(e.Type()) && !g.localT["Result"] {
		name := g.pkgQual(resultPath) + exportName(e.Ctor.Name) // result.Ok / result.Err
		fun := ast.Expr(ast.NewIdent(name))
		args := g.goTypeArgs(ctorTArgs(e.TypeArgs, e.Type()))
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
	return g.ctorLit(e.Ctor, e.Type(), e.TypeArgs, g.atoms(e.Args))
}

// ctorTArgs returns a constructor's ADT instantiation: the node's
// recorded TypeArgs, else the result type's own arguments — a
// saturated application always lands on a Con.
func ctorTArgs(targs []types.Type, ret types.Type) []types.Type {
	if len(targs) > 0 {
		return targs
	}
	if c, ok := types.Chase(ret).(*types.Con); ok {
		return c.Args
	}
	return nil
}

// ctorLit renders `Type_Ctor{F0: a0, ...}` — the sealed-variant
// struct literal. Named payload fields use their names; positional
// slots use V0, V1, ...
func (g *gen) ctorLit(sym *resolve.Symbol, typ types.Type, targs []types.Type, args []ast.Expr) ast.Expr {
	t := g.ctorGoType(sym, typ, targs)
	elts := make([]ast.Expr, 0, len(args))
	var fields []core.TField
	if d, ok := g.ctors[sym]; ok {
		fields = d.fields
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
func (g *gen) ctorGoType(sym *resolve.Symbol, typ types.Type, targs []types.Type) ast.Expr {
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
	args := g.goTypeArgs(ctorTArgs(targs, typ))
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
		return &ast.CallExpr{Fun: g.goType(e.Type()), Args: []ast.Expr{x}}
	case core.ConvBoxAny:
		return &ast.CallExpr{Fun: ast.NewIdent("any"), Args: []ast.Expr{x}}
	case core.ConvSliceToGo:
		return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: ast.NewIdent("ToSlice")}}
	case core.ConvSliceFromGo:
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(listPath) + "FromSlice"), Args: []ast.Expr{x}}
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
