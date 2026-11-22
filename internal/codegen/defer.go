package codegen

import (
	"go/ast"
	"go/token"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/types"
)

// deferStmt emits a lowered `Defer` node — a `Scope` registration
// proven to sit in the same Go function as the scope it runs for.
// The shapes mirror rt.Scope's Close order exactly (LIFO at
// function exit), minus the closed-scope panic the escape check
// already ruled out.
func (g *gen) deferStmt(e *core.Defer) {
	g.line(e.Sp)
	switch e.Kind {
	case core.DeferPlain:
		g.raw("defer ")
		g.expr(g.atom(e.F))
		g.raw("(struct{}{})")
		g.nl()
	case core.DeferErr:
		errs := g.bindName(e.Errs)
		g.raw("defer func()")
		g.open()
		g.raw("if r := ")
		g.expr(g.atom(e.F))
		g.raw("(struct{}{}); !r.IsOk()")
		g.open()
		g.raw("_, e := r.Unwrap()")
		g.nl()
		g.raw(errs + " = append(" + errs + ", e)")
		g.nl()
		g.close()
		g.closeInline()
		g.raw("()")
		g.nl()
	case core.DeferErrDrop:
		g.raw("defer func()")
		g.open()
		g.raw("_, _ = ")
		g.expr(g.atom(e.F))
		g.raw("(struct{}{}).Unwrap()")
		g.nl()
		g.closeInline()
		g.raw("()")
		g.nl()
	case core.DeferOnErr:
		g.raw("defer func()")
		g.open()
		g.raw("if " + g.bindName(e.Failed))
		g.open()
		g.expr(g.atom(e.F))
		g.raw("(struct{}{})")
		g.nl()
		g.close()
		g.closeInline()
		g.raw("()")
		g.nl()
	case core.DeferScopeHead:
		// Failure is the default: a panic before the decider
		// registers leaves the flag true, so cleanups already
		// registered still see the body as failed.
		g.raw("var " + g.bindName(e.Failed) + " = true")
		g.nl()
	case core.DeferScopeFailed:
		g.raw("defer func()")
		g.open()
		g.raw("if p := recover(); p != nil")
		g.open()
		g.raw("panic(p)")
		g.close()
		if e.Res != nil {
			g.raw(g.bindName(e.Failed) + " = !" + g.bindName(e.Res) + ".IsOk()")
		} else {
			g.raw(g.bindName(e.Failed) + " = false")
		}
		g.nl()
		g.closeInline()
		g.raw("()")
		g.nl()
	case core.DeferErrFold:
		errs := g.bindName(e.Errs)
		res := g.bindName(e.Res)
		g.raw("var " + errs + " []error")
		g.nl()
		g.raw("defer func()")
		g.open()
		g.raw("if len(" + errs + ") > 0 && " + res + ".IsOk()")
		g.open()
		g.raw(res + " = ")
		g.expr(g.resultErr(e.Res.Typ, errs))
		g.nl()
		g.close()
		g.closeInline()
		g.raw("()")
		g.nl()
	default:
		panic("unreachable")
	}
}

// resultErr renders `result.Err[A, E](errors.Join(errs...))` — the
// cleanup-error fold's rewrite of the named result. t is the
// function's `Result[A, E]` type.
func (g *gen) resultErr(t types.Type, errs string) ast.Expr {
	var idx []ast.Expr
	if con, ok := types.Chase(t).(*types.Con); ok {
		for _, a := range con.Args {
			idx = append(idx, g.goType(a))
		}
	}
	var fun ast.Expr = ast.NewIdent(g.pkgQual(resultPath) + "Err")
	if len(idx) == 1 {
		fun = &ast.IndexExpr{X: fun, Index: idx[0]}
	} else if len(idx) > 1 {
		fun = &ast.IndexListExpr{X: fun, Indices: idx}
	}
	join := &ast.CallExpr{
		Fun:      ast.NewIdent(g.pkgQual("errors") + "Join"),
		Args:     []ast.Expr{ast.NewIdent(errs)},
		Ellipsis: token.Pos(1),
	}
	return &ast.CallExpr{Fun: fun, Args: []ast.Expr{join}}
}
