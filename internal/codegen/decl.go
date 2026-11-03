package codegen

import (
	"fmt"
	"strings"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/types"
)

// This file emits the Go declarations a Core IR file lowers to
// (docs/design.md section 4): function and value declarations, test
// functions for the _test.go half, and the type declarations — the
// sealed-interface ADT of P0-1, the struct of TRecord, the alias of
// TAlias. TAbstract produces nothing (its representation is settled
// by its consumers, decl.go).

// decl emits one top-level declaration into the generator's buffer.
func (g *gen) decl(d core.Decl) {
	switch d := d.(type) {
	case *core.FuncDecl:
		g.funcDecl(d)
	case *core.VarDecl:
		g.varDecl(d)
	case *core.TypeDecl:
		g.typeDecl(d)
	case *core.TestDecl:
		panic("codegen: TestDecl must be emitted to the test half")
	default:
		panic("unreachable")
	}
}

// funcDecl emits `func name[A any](p T, ...) Res { ... }` — the
// declared arity in, the residual type out (P0-2). The program
// entry — `fn main () -> Unit` in `package main` — is marked by
// lowering on FuncDecl.IsEntry (design.md section 2.1: codegen does
// not re-derive language rules); it emits as `func main()` with the
// body discarded.
func (g *gen) funcDecl(d *core.FuncDecl) {
	g.line(d.Sp)
	if d.IsEntry {
		g.raw("func main()")
		g.open()
		g.funcBody(d.Fn, discard)
		g.close()
		g.nl()
		return
	}
	g.raw("func " + g.symName(d.Sym))
	g.typeParams(d.TypeParams)
	g.raw("(")
	g.params(d.Fn.Params)
	g.raw(") ")
	g.typ(g.goType(d.Fn.ResidualType()))
	g.open()
	g.funcBody(d.Fn, ret)
	g.close()
	g.nl()
}

// varDecl emits `var name T = init`. A statement-level initializer
// is wrapped in the single sanctioned IIFE
// `func() T { ... }()` (decl.go VarDecl).
func (g *gen) varDecl(d *core.VarDecl) {
	g.line(d.Sp)
	t := d.Body.Tail.Type()
	g.raw("var " + g.symName(d.Sym) + " ")
	g.typ(g.goType(t))
	g.raw(" = ")
	if isSimpleExpr(d.Body.Tail) && len(d.Body.Lets) == 0 {
		g.exprValue(d.Body.Tail)
		g.nl()
		return
	}
	g.raw("func() ")
	g.typ(g.goType(t))
	g.open()
	g.body(d.Body, ret)
	g.closeInline()
	g.raw("()")
	g.nl()
	g.nl()
}

// testDecl emits `func TestX(t *testing.T) { ... }` into the test
// half and returns the emitted name — File records it on
// Output.Tests so `gom test` can map `go test -json` events back to
// the `test "Name"` declaration (docs/design.md section 4.8). A
// `Result Unit e` body binds its result and asserts Ok; a Unit body
// is discarded (decl.go TestDecl, spec section 12).
func (g *gen) testDecl(d *core.TestDecl) string {
	name := g.testName(d.Name)
	g.line(d.Sp)
	g.raw("func " + name + "(t *" + g.pkgQual("testing") + "T)")
	g.open()
	savedT := g.testFn
	g.testFn = d.Fn
	res := d.Fn.ResidualType()
	if isResultCon(res) && !g.localT["Result"] {
		r := g.fresh("r")
		g.raw("var " + r + " ")
		g.typ(g.goType(res))
		g.nl()
		// g.body, not g.funcBody: TailLoop only matters when a fn
		// is a TailCall target, and a self-tail call must target the
		// innermost enclosing *FuncDecl* fn — a test body never is
		// one (validator's tailCall rule), so d.Fn.TailLoop cannot
		// be set here.
		g.body(d.Fn.Body, assign(r))
		g.line(d.Sp)
		g.raw(g.pkgQual(resultPath) + "AssertOk(t, " + r + ")")
		g.nl()
	} else {
		g.funcBody(d.Fn, discard)
	}
	g.testFn = savedT
	g.close()
	g.nl()
	return name
}

// testName deduplicates generated test function names — repeated or
// empty test names get a number suffix (spec section 12's renaming
// note).
func (g *gen) testName(name string) string {
	base := testFuncName(name)
	cand := base
	for i := 2; g.used[cand]; i++ {
		cand = base + fmt.Sprint(i)
	}
	g.used[cand] = true
	return cand
}

// typeDecl emits the Go declaration(s) of a type body. TAbstract
// emits nothing.
func (g *gen) typeDecl(d *core.TypeDecl) {
	switch b := d.Body.(type) {
	case *core.TSum:
		g.tsum(d, b)
	case *core.TRecord:
		g.trecord(d, b)
	case *core.TAlias:
		g.talias(d, b)
	case *core.TAbstract:
		// no Go representation of its own
	default:
		panic("unreachable")
	}
}

// typeParams renders `[A any, B any]` — or nothing when the
// declaration is monomorphic — and returns the parameter names for
// use inside the declaration.
func (g *gen) typeParams(tparams []*types.Var) []string {
	if len(tparams) == 0 {
		return nil
	}
	names := make([]string, len(tparams))
	for i, v := range tparams {
		names[i] = typeParamName(v.Name, v.ID)
	}
	g.raw("[")
	g.raw(strings.Join(names, ", "))
	g.raw(" any]")
	return names
}

// tsum emits the P0-1 sealed-interface ADT: the interface with the
// unexported marker, one struct per variant, and each variant's
// marker method. Generic ADTs keep the type parameters on every
// variant and the marker signature mentions them — the fix for
// phantom-parameter inference (P0-1's `isOption() A` form).
func (g *gen) tsum(d *core.TypeDecl, b *core.TSum) {
	g.line(d.Sp)
	tname := g.typeName(d.Sym)
	names := g.tparamNames(d.TypeParams)

	// type Name[A, B] interface{ isName() (A, B) }
	g.raw("type " + tname)
	g.typeParams(d.TypeParams)
	g.raw(" interface{ " + markerName(tname) + "()")
	if len(names) > 0 {
		if len(names) == 1 {
			g.raw(" " + names[0])
		} else {
			g.raw(" (" + strings.Join(names, ", ") + ")")
		}
	}
	g.raw(" }")
	g.nl()
	g.nl()

	for i := range b.Variants {
		v := &b.Variants[i]
		if v.Sym == nil {
			continue
		}
		vname := tname + "_" + exportName(v.Sym.Name)
		g.line(v.Sp)

		// type Name_Ctor[A] struct { fields }
		g.raw("type " + vname)
		g.typeParams(d.TypeParams)
		if len(v.Fields) == 0 {
			g.raw(" struct{}")
			g.nl()
		} else {
			g.raw(" struct")
			g.open()
			for j := range v.Fields {
				f := &v.Fields[j]
				g.line(f.Sp)
				g.raw(g.tfieldName(f) + " ")
				g.typ(g.goType(f.Type))
				g.nl()
			}
			g.close()
		}

		// func (Name_Ctor[A]) isName() (A, B) { return zero vals }
		g.raw("func (" + vname)
		if len(names) > 0 {
			g.raw("[" + strings.Join(names, ", ") + "]")
		}
		g.raw(") " + markerName(tname) + "()")
		if len(names) == 0 {
			g.raw(" {}")
			g.nl()
			g.nl()
			continue
		}
		if len(names) == 1 {
			g.raw(" " + names[0])
		} else {
			g.raw(" (" + strings.Join(names, ", ") + ")")
		}
		g.open()
		zeros := make([]string, len(names))
		for j, n := range names {
			zeros[j] = "z" + itoa(j)
			g.raw("var " + zeros[j] + " " + n)
			g.nl()
		}
		g.raw("return " + strings.Join(zeros, ", "))
		g.nl()
		g.close()
		g.nl()
	}
}

// tparamNames renders the declared type parameters' names without
// emitting — the shared helper of tsum's interface, structs and
// marker methods.
func (g *gen) tparamNames(tparams []*types.Var) []string {
	names := make([]string, len(tparams))
	for i, v := range tparams {
		names[i] = typeParamName(v.Name, v.ID)
	}
	return names
}

// markerName is the sealing method name: `is` + the Go type name.
func markerName(tname string) string { return "is" + tname }

// trecord emits `type Name[A any] struct { F T; ... }` — the record
// representation (P0-1's plain struct).
func (g *gen) trecord(d *core.TypeDecl, b *core.TRecord) {
	g.line(d.Sp)
	g.raw("type " + g.typeName(d.Sym))
	g.typeParams(d.TypeParams)
	if len(b.Fields) == 0 {
		g.raw(" struct{}")
		g.nl()
		g.nl()
		return
	}
	g.raw(" struct")
	g.open()
	for i := range b.Fields {
		f := &b.Fields[i]
		g.line(f.Sp)
		g.raw(g.tfieldName(f) + " ")
		g.typ(g.goType(f.Type))
		g.nl()
	}
	g.close()
	g.nl()
}

// talias emits `type Name[A] = T` — the transparent alias: a Gom
// alias introduces a new name, not a new type, matching Go's `=`.
func (g *gen) talias(d *core.TypeDecl, b *core.TAlias) {
	g.line(d.Sp)
	g.raw("type " + g.typeName(d.Sym))
	g.typeParams(d.TypeParams)
	g.raw(" = ")
	g.typ(g.goType(b.Type))
	g.nl()
	g.nl()
}
