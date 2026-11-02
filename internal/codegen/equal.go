package codegen

import (
	"fmt"
	"go/ast"
	"go/token"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/types"
)

// This file renders the comparison recipes lowering fixed on
// core.Equal nodes and File.EqDecls comparators (docs/decisions/
// 2026-11-05-generated-equality-comparators.md): the plan is the
// semantic decision — codegen only translates it, so no type or
// builtin name is re-inspected here.

// equalExpr renders a core.Equal — `x == y` / `x != y` under the
// recipe its How plan carries.
func (g *gen) equalExpr(e *core.Equal) ast.Expr {
	x := g.eqExpr(e.How, g.atom(e.X), g.atom(e.Y))
	if !e.Neg {
		return x
	}
	// `!=` prefers the operator where the recipe is the native one —
	// `x != y` reads better than `!(x == y)`. Every other recipe is
	// a call or a compound, negated wholesale.
	if b, ok := x.(*ast.BinaryExpr); ok && b.Op == token.EQL {
		return &ast.BinaryExpr{X: b.X, Op: token.NEQ, Y: b.Y}
	}
	if _, ok := x.(*ast.BinaryExpr); ok {
		x = &ast.ParenExpr{X: x}
	}
	return &ast.UnaryExpr{Op: token.NOT, X: x}
}

// eqExpr renders one operand pair's comparison under a leaf recipe —
// the position every plan except an EqDecl's root occupies: inside
// `if !`, inside `&&` chains, inside the assert_eq operand.
// Composite kinds are unreachable here (the validator restricts them
// to comparator roots).
func (g *gen) eqExpr(p *core.EqPlan, x, y ast.Expr) ast.Expr {
	switch p.Kind {
	case core.EqNative:
		return &ast.BinaryExpr{X: x, Op: token.EQL, Y: y}
	case core.EqRuntime:
		return &ast.CallExpr{
			Fun:  ast.NewIdent(g.pkgQual(rtPath) + "Equal"),
			Args: []ast.Expr{x, y},
		}
	case core.EqError:
		// nil-aware message comparison — two nils are equal, a nil
		// and an error are not, two errors compare by text (the
		// evaluator's VGoErr reading).
		xnil := &ast.BinaryExpr{X: x, Op: token.EQL, Y: ast.NewIdent("nil")}
		ynil := &ast.BinaryExpr{X: y, Op: token.EQL, Y: ast.NewIdent("nil")}
		return &ast.BinaryExpr{
			X:  &ast.ParenExpr{X: &ast.BinaryExpr{X: &ast.ParenExpr{X: xnil}, Op: token.EQL, Y: &ast.ParenExpr{X: ynil}}},
			Op: token.LAND,
			Y: &ast.ParenExpr{X: &ast.BinaryExpr{
				X:  &ast.ParenExpr{X: xnil},
				Op: token.LOR,
				Y: &ast.BinaryExpr{
					X:  &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: ast.NewIdent("Error")}},
					Op: token.EQL,
					Y:  &ast.CallExpr{Fun: &ast.SelectorExpr{X: y, Sel: ast.NewIdent("Error")}},
				},
			}},
		}
	case core.EqCall:
		return &ast.CallExpr{
			Fun:  ast.NewIdent(g.symName(p.Fn.Sym)),
			Args: []ast.Expr{x, y},
		}
	default:
		panic(fmt.Sprintf("codegen: eq plan kind %s is not expression-renderable", p.Kind))
	}
}

// eqDecl emits one generated comparator —
// `func gomEqN(a, b T) bool { ... }` — after the file's own
// declarations. The body's shape comes from the root plan kind;
// every nested pair is a leaf recipe rendered inline.
func (g *gen) eqDecl(d *core.EqDecl) {
	g.line(d.Sp)
	g.raw("func " + g.symName(d.Sym) + "(a, b ")
	g.typ(g.goType(d.Typ))
	g.raw(") bool")
	g.open()
	g.eqBody(d)
	g.close()
	g.nl()
}

// eqBody emits a comparator body for its root plan.
func (g *gen) eqBody(d *core.EqDecl) {
	p := d.Plan
	switch p.Kind {
	case core.EqSlice:
		// Elementwise: nil and empty both have length 0 — spec's
		// nil==empty rule — and identical backing stores still
		// compare element by element (no DeepEqual shortcut).
		g.raw("if len(a) != len(b)")
		g.open()
		g.raw("return false")
		g.nl()
		g.close()
		g.raw("for i := range a")
		g.open()
		g.raw("if !(")
		g.expr(g.eqExpr(p.Elem, g.eqIndex(ast.NewIdent("a")), g.eqIndex(ast.NewIdent("b"))))
		g.raw(")")
		g.open()
		g.raw("return false")
		g.nl()
		g.close()
		g.close()
		g.raw("return true")
		g.nl()
	case core.EqMap:
		// Key sets equal plus every shared key's values Eq-ing —
		// keys are Go-comparable by construction.
		g.raw("if len(a) != len(b)")
		g.open()
		g.raw("return false")
		g.nl()
		g.close()
		g.raw("for k, av := range a")
		g.open()
		g.raw("bv, ok := b[k]")
		g.nl()
		g.raw("if !ok || !(")
		g.expr(g.eqExpr(p.Elem, ast.NewIdent("av"), ast.NewIdent("bv")))
		g.raw(")")
		g.open()
		g.raw("return false")
		g.nl()
		g.close()
		g.close()
		g.raw("return true")
		g.nl()
	case core.EqSum:
		g.eqSum(d)
	case core.EqResult:
		g.eqResult(d)
	case core.EqTuple, core.EqRecord:
		// The fieldwise conjunction — `a.V0 == b.V0 && ...`.
		g.raw("return ")
		for i := range p.Fields {
			f := &p.Fields[i]
			if i > 0 {
				g.raw(" && ")
			}
			g.expr(g.eqExpr(f.Plan, g.eqField(f, "a"), g.eqField(f, "b")))
		}
		g.nl()
	case core.EqNative, core.EqRuntime, core.EqError, core.EqCall:
		// A leaf recipe at a comparator's root — the defensive
		// plans (e.g. an alias that slipped through).
		g.raw("return ")
		g.expr(g.eqExpr(p, ast.NewIdent("a"), ast.NewIdent("b")))
		g.nl()
	default:
		panic("unreachable")
	}
}

// eqIndex renders `x[i]` inside a slice comparator's loop.
func (g *gen) eqIndex(x ast.Expr) ast.Expr {
	return &ast.IndexExpr{X: x, Index: ast.NewIdent("i")}
}

// eqField renders `x.<sel>` — the projected operand of a fieldwise
// comparison.
func (g *gen) eqField(f *core.EqField, base string) ast.Expr {
	sel := fmt.Sprintf("V%d", f.Index)
	switch {
	case f.Sel != "":
		sel = f.Sel
	case f.Name != "":
		sel = exportName(f.Name)
	case f.Sym != nil:
		sel = exportName(f.Sym.Name)
	}
	return &ast.SelectorExpr{X: ast.NewIdent(base), Sel: ast.NewIdent(sel)}
}

// eqSum emits the sealed-ADT comparison: nil-aware, then a variant
// type switch whose arms compare payloads — the dynamic reading the
// evaluator's VVariant case performs.
func (g *gen) eqSum(d *core.EqDecl) {
	g.raw("if a == nil || b == nil")
	g.open()
	g.raw("return a == nil && b == nil")
	g.nl()
	g.close()
	g.raw("switch av := a.(type)")
	g.open()
	var targs []types.Type
	if c, ok := types.Chase(d.Typ).(*types.Con); ok {
		targs = c.Args
	}
	for i := range d.Plan.Variants {
		v := &d.Plan.Variants[i]
		vt := g.ctorGoType(v.Sym, d.Typ, targs)
		g.caseLabel(func() {
			g.raw("case ")
			g.typ(vt)
		})
		if len(v.Fields) == 0 {
			g.raw("_, ok := b.(")
			g.typ(vt)
			g.raw(")")
			g.nl()
			g.raw("return ok")
			g.nl()
			continue
		}
		g.raw("bv, ok := b.(")
		g.typ(vt)
		g.raw(")")
		g.nl()
		g.raw("return ok")
		for j := range v.Fields {
			f := &v.Fields[j]
			g.raw(" && ")
			g.expr(g.eqExpr(f.Plan, g.eqVariantField(f, "av"), g.eqVariantField(f, "bv")))
		}
		g.nl()
	}
	// The sealed set is exhaustive by construction; an unexpected
	// dynamic type is a contract violation — answer false rather
	// than panic so a `!=` cannot panic either.
	g.caseLabel(func() { g.raw("default") })
	g.raw("return false")
	g.nl()
	g.close()
}

// eqResult emits the flat-Result comparison — same tag, then the
// payload the tag selects (Value for Ok, Error for Err).
func (g *gen) eqResult(d *core.EqDecl) {
	g.raw("return a.Tag() == b.Tag()")
	for i := range d.Plan.Variants {
		v := &d.Plan.Variants[i]
		tag := g.pkgQual(resultPath) + "ResultTag" + exportName(v.Sym.Name)
		for j := range v.Fields {
			f := &v.Fields[j]
			g.raw(" && (a.Tag() != " + tag + " || ")
			g.expr(g.eqExpr(f.Plan, g.eqField(f, "a"), g.eqField(f, "b")))
			g.raw(")")
		}
	}
	g.nl()
}

// eqVariantField renders `av.<sel>` — the projected payload slot of
// a narrowed variant operand.
func (g *gen) eqVariantField(f *core.EqField, base string) ast.Expr {
	return g.eqField(f, base)
}
