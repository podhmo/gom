package codegen

import (
	"fmt"
	"go/ast"

	"github.com/podhmo/gom/internal/types"
)

// This file renders types.Type values as Go type expressions — the
// mechanical mapping of docs/design.md section 4 and spec section
// 8.3:
//
//	Unit → struct{}   Bool → bool   Int/Int64 → int64   Int32 → int32
//	Float → float64   Char → rune   String → string     GoError → error
//	List a → list.List[A]        Option a → option.Option[A]
//	Result a e → result.Result[T, E]          Task a → rt.Task[A]
//	Ref a / Ptr a → *T           Array a / Go.Slice a → []T
//	Chan a → chan T  RecvChan a → <-chan T   SendChan a → chan<- T
//	Go.Map k v → map[K]V         (a, b) → rt.TupleN[A, B]
//	T1 -> .. -> R → func(T1) func(T2) ... R  Go named → pkg.Name
//	type var → A/E/T0            declared type → Name[args]

// goType renders a types.Type as a Go type expression.
func (g *gen) goType(t types.Type) ast.Expr {
	switch t := t.(type) {
	case *types.Con:
		return g.conType(t)
	case *types.Var:
		return ast.NewIdent(typeParamName(t.Name, t.ID))
	case *types.Meta:
		if t.Bound() {
			if sol := t.Solution(); sol != nil {
				return g.goType(sol)
			}
		}
		panic(fmt.Sprintf("codegen: unsolved meta ?%d reached codegen", t.ID))
	case *types.Func:
		return g.funcType(t)
	case *types.Tuple:
		return g.tupleType(t)
	case *types.Go:
		return g.goRefType(t)
	default:
		panic("unreachable")
	}
}

// goTypeArgs renders `[A, B]` type arguments, or nil when empty.
func (g *gen) goTypeArgs(args []types.Type) []ast.Expr {
	if len(args) == 0 {
		return nil
	}
	out := make([]ast.Expr, len(args))
	for i, a := range args {
		out[i] = g.goType(a)
	}
	return out
}

// conType renders a nominal constructor application.
func (g *gen) conType(c *types.Con) ast.Expr {
	if c.Qual == "Go" {
		switch c.Name {
		case "Slice":
			return &ast.ArrayType{Elt: g.goType(c.Args[0])}
		case "Map":
			return &ast.MapType{Key: g.goType(c.Args[0]), Value: g.goType(c.Args[1])}
		case "Int":
			// The Go-side `int` — distinct from Gom `Int` (int64).
			return ast.NewIdent("int")
		case "Any":
			return ast.NewIdent("any")
		default:
			panic(fmt.Sprintf("codegen: unknown Go namespace type %q", c.Name))
		}
	}

	if c.Qual == "" && !g.localT[exportName(c.Name)] {
		switch builtinConKind(c.Name) {
		case conUnit:
			// A real ast.StructType with an empty FieldList prints
			// `struct {\n}` — the identifier spelling prints the
			// canonical `struct{}`.
			return ast.NewIdent("struct{}")
		case conSimple:
			return ast.NewIdent(simpleBuiltinName(c.Name))
		case conInt64:
			return ast.NewIdent("int64")
		case conError:
			return ast.NewIdent("error")
		case conList:
			return &ast.IndexExpr{X: ast.NewIdent(g.pkgQual(listPath) + "List"),
				Index: g.goType(c.Args[0])}
		case conOption:
			return &ast.IndexExpr{X: ast.NewIdent(g.pkgQual(optionPath) + "Option"),
				Index: g.goType(c.Args[0])}
		case conResult:
			return &ast.IndexListExpr{X: ast.NewIdent(g.pkgQual(resultPath) + "Result"),
				Indices: g.goTypeArgs(c.Args)}
		case conPtr:
			return &ast.StarExpr{X: g.goType(c.Args[0])}
		case conSlice:
			return &ast.ArrayType{Elt: g.goType(c.Args[0])}
		case conChan:
			return &ast.ChanType{Dir: ast.SEND | ast.RECV, Value: g.goType(c.Args[0])}
		case conRecvChan:
			return &ast.ChanType{Dir: ast.RECV, Value: g.goType(c.Args[0])}
		case conSendChan:
			return &ast.ChanType{Dir: ast.SEND, Value: g.goType(c.Args[0])}
		case conTask:
			return &ast.IndexExpr{X: ast.NewIdent(g.pkgQual(rtPath) + "Task"),
				Index: g.goType(c.Args[0])}
		case conOther:
			// a builtin-scope type codegen does not know: fall
			// through to the plain name
		}
	}

	// Local (Qual == "") or package-qualified (Qual == path) nominal
	// type: Name or pkg.Name, with type arguments when applied.
	var x ast.Expr = ast.NewIdent(exportName(c.Name))
	if c.Qual != "" {
		x = g.qualIdent(c.Qual, exportName(c.Name))
	}
	switch len(c.Args) {
	case 0:
		return x
	case 1:
		return &ast.IndexExpr{X: x, Index: g.goType(c.Args[0])}
	default:
		return &ast.IndexListExpr{X: x, Indices: g.goTypeArgs(c.Args)}
	}
}

// goRefType renders a Go-interop type handle (types.Go): the named
// type or interface `path.Name[args]`.
func (g *gen) goRefType(t *types.Go) ast.Expr {
	x := g.qualIdent(t.Ref.Path, t.Ref.Name)
	args := g.goTypeArgs(t.Args)
	switch len(args) {
	case 0:
		return x
	case 1:
		return &ast.IndexExpr{X: x, Index: args[0]}
	default:
		return &ast.IndexListExpr{X: x, Indices: args}
	}
}

// funcType renders a Gom function type in the canonical value
// representation: one Go func per arrow — `func(T1) func(T2) ... R`.
// The representation is a pure function of the *flattened* arrow
// chain, so every spelling types.Equal identifies — `A -> B -> R`,
// `A -> (B -> R)`, `Func{[A,B],R}` — renders to the same Go type and
// no boundary adapter ever stands between them. Labels are erased;
// parameter order is declaration order. Function *declarations* keep
// their declared arity on top of this residual shape — see
// docs/decisions/2026-11-02-function-value-representation.md.
func (g *gen) funcType(f *types.Func) ast.Expr {
	ret := g.goType(f.Ret)
	if len(f.Params) == 0 {
		return &ast.FuncType{
			Params:  &ast.FieldList{},
			Results: &ast.FieldList{List: []*ast.Field{{Type: ret}}},
		}
	}
	for i := len(f.Params) - 1; i >= 0; i-- {
		ret = &ast.FuncType{
			Params:  &ast.FieldList{List: []*ast.Field{{Type: g.goType(f.Params[i].Type)}}},
			Results: &ast.FieldList{List: []*ast.Field{{Type: ret}}},
		}
	}
	return ret
}

// tupleType renders `(A, B, ...)` as rt.TupleN[A, B, ...].
func (g *gen) tupleType(t *types.Tuple) ast.Expr {
	x := ast.NewIdent(fmt.Sprintf("%sTuple%d", g.pkgQual(rtPath), len(t.Elems)))
	args := g.goTypeArgs(t.Elems)
	if len(args) == 1 {
		return &ast.IndexExpr{X: x, Index: args[0]}
	}
	return &ast.IndexListExpr{X: x, Indices: args}
}
