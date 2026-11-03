package codegen

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/types"
)

// This file renders the display recipes lowering fixed on
// CallBuiltin{assert_eq}.Show and File.ShowDecls (docs/design.md
// section 4.8): the plan is the semantic decision — codegen only
// translates it. The format matches the evaluator's comparison
// display (internal/eval/value.go's String): `Rect ~w: 2 ~h: 3`,
// `{ name = "n" }`, `(1, 2)`, `[1, 2]`, `Ok 42` — Go-view values
// show through fmt's %v (spec section 11).

// showExpr renders a leaf plan — `string(x)` where the recipe's
// conversion is known: the operand every plan except a ShowDecl's
// root occupies. Composite kinds are unreachable here (they live at
// decl roots); a nil plan is a contract gap degraded to fmt's %v.
func (g *gen) showExpr(p *core.ShowPlan, x ast.Expr) ast.Expr {
	if p == nil {
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual("fmt") + "Sprint"), Args: []ast.Expr{x}}
	}
	switch p.Kind {
	case core.ShowScalar:
		return g.showScalar(p.Scalar, x)
	case core.ShowRuntime:
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual("fmt") + "Sprint"), Args: []ast.Expr{x}}
	case core.ShowError:
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "ShowError"), Args: []ast.Expr{x}}
	case core.ShowCall:
		return &ast.CallExpr{Fun: ast.NewIdent(g.symName(p.Fn.Sym)), Args: []ast.Expr{x}}
	default:
		panic(fmt.Sprintf("codegen: show plan kind %s is not expression-renderable", p.Kind))
	}
}

// showScalar renders a builtin scalar through strconv — decimal
// ints, 'g' shortest floats, quoted strings and runes, true/false
// bools, `()` for Unit (the evaluator's leaf formats; assert
// operands quote strings and runes so `"6"` and `6` stay distinct).
func (g *gen) showScalar(name string, x ast.Expr) ast.Expr {
	sv := func(fun string, args ...ast.Expr) ast.Expr {
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual("strconv") + fun), Args: args}
	}
	switch name {
	case "String":
		return sv("Quote", x)
	case "Unit":
		return &ast.BasicLit{Kind: token.STRING, Value: `"()"`}
	case "Bool":
		return sv("FormatBool", x)
	case "Int", "Int64":
		return sv("FormatInt", x, &ast.BasicLit{Kind: token.INT, Value: "10"})
	case "Int32":
		return sv("FormatInt",
			&ast.CallExpr{Fun: ast.NewIdent("int64"), Args: []ast.Expr{x}},
			&ast.BasicLit{Kind: token.INT, Value: "10"})
	case "Float":
		return sv("FormatFloat", x,
			&ast.BasicLit{Kind: token.CHAR, Value: `'g'`},
			&ast.BasicLit{Kind: token.INT, Value: "-1"},
			&ast.BasicLit{Kind: token.INT, Value: "64"})
	case "Char":
		return sv("QuoteRune", x)
	default:
		// Go.Int and any spelling the table does not name — %v is
		// the closest scalar rendering.
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual("fmt") + "Sprint"), Args: []ast.Expr{x}}
	}
}

// showOperand renders an operand inside a composite display —
// parenthesizing values whose own display carries unbracketed
// structure so `Add (Num 1) (Num 2)` stays unambiguous (the
// evaluator's writeOperand rule: records, variants, Go errors and
// opaque values parenthesize; scalars, tuples and lists self-
// bracket). The parens are output text — concatenated around the
// recipe's string, not Go syntax around the operand.
func (g *gen) showOperand(p *core.ShowPlan, x ast.Expr) ast.Expr {
	if p == nil {
		return g.showExpr(p, x)
	}
	paren := false
	switch p.Kind {
	case core.ShowError, core.ShowRuntime:
		paren = true
	case core.ShowCall:
		switch p.Fn.Plan.Kind {
		case core.ShowRecord, core.ShowSum, core.ShowResult:
			paren = true
		}
	}
	shown := g.showExpr(p, x)
	if paren {
		return concat(strLit("("), shown, strLit(")"))
	}
	return shown
}

// showField renders `v.<sel>` — the projected operand of a record or
// variant field. It shares eqField's selector rule: Sel first, then
// the declared name, then `V<Index>`.
func (g *gen) showField(f *core.ShowField, base string) ast.Expr {
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

// showDecl emits one generated display function —
// `func gomShowN(v T) string { ... }` — into the test half, after
// the test declarations. The body's shape comes from the root plan
// kind; every nested operand is a leaf recipe rendered inline.
func (g *gen) showDecl(d *core.ShowDecl) {
	g.line(d.Sp)
	g.raw("func " + g.symName(d.Sym) + "(v ")
	g.typ(g.goType(d.Typ))
	g.raw(") string")
	g.open()
	g.showBody(d)
	g.close()
	g.nl()
}

// concat joins string expressions with `+` — the one-line renderers
// of tuples, records and variants.
func concat(xs ...ast.Expr) ast.Expr {
	var out ast.Expr = &ast.BasicLit{Kind: token.STRING, Value: `""`}
	for _, x := range xs {
		out = &ast.BinaryExpr{X: out, Op: token.ADD, Y: x}
	}
	return out
}

// strLit is a quoted Go string literal.
func strLit(s string) ast.Expr {
	return &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(s)}
}

// showBody emits a display function body for its root plan.
func (g *gen) showBody(d *core.ShowDecl) {
	p := d.Plan
	switch p.Kind {
	case core.ShowTuple:
		// `(a, b)` — elements are operands, so composites
		// parenthesize.
		xs := []ast.Expr{strLit("(")}
		for i, e := range p.Elems {
			if i > 0 {
				xs = append(xs, strLit(", "))
			}
			elem := &ast.SelectorExpr{X: ast.NewIdent("v"), Sel: ast.NewIdent(fmt.Sprintf("V%d", i))}
			xs = append(xs, g.showOperand(e, elem))
		}
		xs = append(xs, strLit(")"))
		g.raw("return ")
		g.expr(concat(xs...))
		g.nl()
	case core.ShowSlice:
		// `[a, b]` elementwise through a strings.Builder.
		g.raw("var sb " + g.pkgQual("strings") + "Builder")
		g.nl()
		g.raw("sb.WriteByte('[')")
		g.nl()
		g.raw("for i, e := range v")
		g.open()
		g.raw("if i > 0")
		g.open()
		g.raw("sb.WriteString(\", \")")
		g.nl()
		g.close()
		g.raw("sb.WriteString(")
		g.expr(g.showExpr(p.Elem, ast.NewIdent("e")))
		g.raw(")")
		g.nl()
		g.close()
		g.raw("sb.WriteByte(']')")
		g.nl()
		g.raw("return sb.String()")
		g.nl()
	case core.ShowRecord:
		// `{ name = v, ... }` — field names print in Gom spelling.
		xs := []ast.Expr{strLit("{ ")}
		for i := range p.Fields {
			f := &p.Fields[i]
			if i > 0 {
				xs = append(xs, strLit(", "))
			}
			xs = append(xs, strLit(f.Name+" = "), g.showOperand(f.Plan, g.showField(f, "v")))
		}
		xs = append(xs, strLit(" }"))
		g.raw("return ")
		g.expr(concat(xs...))
		g.nl()
	case core.ShowSum:
		g.showSum(d)
	case core.ShowResult:
		g.showResult(d)
	case core.ShowScalar, core.ShowRuntime, core.ShowError, core.ShowCall:
		// A leaf recipe at a decl's root — the defensive plans.
		g.raw("return ")
		g.expr(g.showExpr(p, ast.NewIdent("v")))
		g.nl()
	default:
		panic("unreachable")
	}
}

// showSum emits the sealed-ADT display: a nil guard, then a variant
// type switch whose arms render `Ctor ~name: v ...`.
func (g *gen) showSum(d *core.ShowDecl) {
	g.raw("if v == nil")
	g.open()
	g.raw("return \"nil\"")
	g.nl()
	g.close()
	g.raw("switch x := v.(type)")
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
		xs := []ast.Expr{strLit(v.Sym.Name)}
		for j := range v.Fields {
			f := &v.Fields[j]
			xs = append(xs, strLit(" "))
			if f.Name != "" {
				// Named payload fields print `~name: v` — the
				// evaluator's VVariant field marker.
				xs = append(xs, strLit("~"+f.Name+": "))
			}
			xs = append(xs, g.showOperand(f.Plan, g.showField(f, "x")))
		}
		g.raw("return ")
		g.expr(concat(xs...))
		g.nl()
	}
	// The sealed set is exhaustive by construction; an unexpected
	// dynamic type is a contract violation.
	g.caseLabel(func() { g.raw("default") })
	g.raw("panic(" + g.pkgQual(rtPath) + "InvalidVariant(")
	g.expr(strLit(d.Typ.String()))
	g.raw(", v))")
	g.nl()
	g.close()
}

// showResult emits the flat-Result display — a Tag() switch
// rendering `Ok v` / `Err e`.
func (g *gen) showResult(d *core.ShowDecl) {
	g.raw("switch v.Tag()")
	g.open()
	for i := range d.Plan.Variants {
		v := &d.Plan.Variants[i]
		tag := g.pkgQual(resultPath) + "ResultTag" + exportName(v.Sym.Name)
		g.caseLabel(func() {
			g.raw("case " + tag)
		})
		xs := []ast.Expr{strLit(v.Sym.Name)}
		for j := range v.Fields {
			f := &v.Fields[j]
			xs = append(xs, strLit(" "), g.showOperand(f.Plan, g.showField(f, "v")))
		}
		g.raw("return ")
		g.expr(concat(xs...))
		g.nl()
	}
	g.caseLabel(func() { g.raw("default") })
	g.raw("panic(" + g.pkgQual(rtPath) + "InvalidVariant(")
	g.expr(strLit("result.Result"))
	g.raw(", v))")
	g.nl()
	g.close()
}
