package codegen

import (
	"fmt"
	"go/ast"
	"go/printer"
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
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
		if !d.Extern {
			// An `@extern fn` signature claims its name (indexDecls)
			// but emits no code — the body is the hand-written Go
			// function of the same name in this package.
			g.funcDecl(d)
		}
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
	g.paramsFlat(d.Fn.Params)
	g.raw(") ")
	g.resultSig(d.Fn.ResidualType(), d.Fn.Body)
	g.open()
	g.funcBody(d.Fn, ret)
	g.close()
	g.nl()
	if d.Method {
		g.methodDecl(d)
	}
}

// methodDecl emits the Go method half of an @method fn — the
// receiver-takes-first forwarding wrapper that makes the record
// satisfy a Go interface (decisions/2026-11-21-go-iface-impl). Unit
// parameters drop out: no interface method takes one.
func (g *gen) methodDecl(d *core.FuncDecl) {
	fn := d.Fn
	recv := fn.Params[0]
	// Discarded params still need a name in the wrapper — the
	// forwarded value is what the inner function ignores, not the
	// method signature.
	nameOf := func(b *core.Bind) string {
		if b.Discard {
			return g.fresh("arg")
		}
		return g.bindName(b)
	}
	var rest []*core.Bind
	for _, b := range fn.Params[1:] {
		if types.Equal(b.Typ, types.Unit) {
			continue
		}
		rest = append(rest, b)
	}
	names := map[*core.Bind]string{recv: nameOf(recv)}
	for _, b := range rest {
		names[b] = nameOf(b)
	}
	g.line(d.Sp)
	g.raw("func (" + names[recv] + " ")
	g.typ(g.goType(recv.Typ))
	mname := d.MethodName
	if mname == "" {
		mname = core.ExportName(d.Sym.Name)
	}
	g.raw(") " + mname + "(")
	for i, b := range rest {
		if i > 0 {
			g.raw(", ")
		}
		g.raw(names[b] + " ")
		if flatArity(b.Typ) >= 2 {
			g.typ(g.flatFuncType(b.Typ))
		} else {
			g.typ(g.methodType(b.Typ))
		}
	}
	g.raw(") ")
	res := d.Fn.ResidualType()
	// `Result T GoError` unwraps to the method's (T, error) pair —
	// `Result Unit GoError` to a bare error (spec §8.7).
	resT, resErr := methodResult(res)
	// outs is the flat boundary result list before the trailing
	// error: a Tuple residual spreads into individual results like
	// Go's own (A, B) form; opt marks a `Result (Option T) GoError`
	// Ok payload, which unwraps Some→v / None→nil.
	var outs []types.Type
	var resOpt bool
	payload := res
	if resErr {
		payload = resT
		if o := optionElem(payload); o != nil {
			payload, resOpt = o, true
		}
	}
	switch {
	case types.Equal(payload, types.Unit):
		// `Result Unit GoError` → bare `error`; plain Unit → no result.
	case resErr || !types.Equal(res, types.Unit):
		if tup, ok := types.Chase(payload).(*types.Tuple); ok {
			outs = tup.Elems
		} else {
			outs = []types.Type{payload}
		}
	}
	if resErr || len(outs) > 1 {
		g.raw("(")
		for i, o := range outs {
			if i > 0 {
				g.raw(", ")
			}
			g.typ(g.methodType(o))
		}
		if resErr {
			if len(outs) > 0 {
				g.raw(", ")
			}
			g.raw("error")
		}
		g.raw(")")
	} else if len(outs) == 1 {
		g.typ(g.methodType(outs[0]))
	}
	g.open()
	call := g.symName(d.Sym) + "(" + names[recv]
	for _, b := range fn.Params[1:] {
		if types.Equal(b.Typ, types.Unit) {
			call += ", struct{}{}"
			continue
		}
		call += ", " + g.methodArg(names[b], b.Typ)
	}
	call += ")"
	switch {
	case resErr && len(outs) == 0:
		g.raw("_, err := " + g.pkgQual(resultPath) + "ToGo(" + call + ")")
		g.nl()
		g.raw("return err")
	case resErr && len(outs) == 1 && !resOpt && !methodConv(outs[0]):
		g.raw("return " + g.pkgQual(resultPath) + "ToGo(" + call + ")")
	case resErr:
		g.raw("v, err := " + g.pkgQual(resultPath) + "ToGo(" + call + ")")
		g.nl()
		g.raw("return " + g.boundaryResults(outs, resOpt) + ", err")
	case len(outs) == 1:
		o := outs[0]
		switch {
		case listElem(o) != nil:
			g.raw("return []" + g.exprString(g.goType(listElem(o))) + "(" + call + ")")
		case types.Equal(types.Chase(o), types.Int):
			g.raw("return int(" + call + ")")
		default:
			g.raw("return " + call)
		}
	case len(outs) > 1:
		g.raw("v := " + call)
		g.nl()
		g.raw("return " + g.boundaryResults(outs, false))
	default:
		g.raw(call)
	}
	g.nl()
	g.close()
	g.nl()
}

// methodResult reports the method's Result unwrap shape: resErr is
// true when the residual is `Result T GoError` and resT is its Ok
// payload (Unit for an error-only signature).
func methodResult(res types.Type) (resT types.Type, resErr bool) {
	rc, ok := types.Chase(res).(*types.Con)
	if !ok || rc.Qual != "" || rc.Name != "Result" || len(rc.Args) != 2 {
		return nil, false
	}
	if !types.Equal(types.Chase(rc.Args[1]), types.GoError) {
		return nil, false
	}
	return rc.Args[0], true
}

// methodConv reports whether a type needs a value conversion between
// the method's boundary spelling and the emitted inner shape —
// `List T` (`[]T` vs `list.List[T]`) and `Int` (`int` vs `int64`).
func methodConv(t types.Type) bool {
	return listElem(t) != nil || types.Equal(types.Chase(t), types.Int)
}

// listElem reports the element type when t is the gom `List T`
// view — the one builtin whose emitted name (`list.List[T]`) differs
// from the boundary's raw slice `[]T` an interface method signature
// must spell.
func listElem(t types.Type) types.Type {
	if c, ok := types.Chase(t).(*types.Con); ok && c.Qual == "" && c.Name == "List" && len(c.Args) == 1 {
		return c.Args[0]
	}
	return nil
}

// optionElem reports the payload when t is `Option T` — the
// nilable-interface view a `(method-bearing interface, error)`
// result maps to (spec §8.3).
func optionElem(t types.Type) types.Type {
	if c, ok := types.Chase(t).(*types.Con); ok && c.Qual == "" && c.Name == "Option" && len(c.Args) == 1 {
		return c.Args[0]
	}
	return nil
}

// boundaryResults renders the result values a method returns from
// an already-bound `v` — a single payload uses `v`, a Tuple payload
// spreads into `v.V0, v.V1, ...`; `List T` converts to the raw
// `[]T`, an Option payload unwraps Some→v / None→nil.
func (g *gen) boundaryResults(outs []types.Type, opt bool) string {
	conv := func(o types.Type, acc string) string {
		if el := listElem(o); el != nil {
			return "[]" + g.exprString(g.goType(el)) + "(" + acc + ")"
		}
		if types.Equal(types.Chase(o), types.Int) {
			return "int(" + acc + ")"
		}
		return acc
	}
	if len(outs) == 1 {
		if opt {
			nilLit := g.exprString(&ast.CallExpr{
				Fun:  g.goType(outs[0]),
				Args: []ast.Expr{ast.NewIdent("nil")},
			})
			return g.pkgQual(optionPath) + "GetOrElse(" + nilLit + ", v)"
		}
		return conv(outs[0], "v")
	}
	parts := make([]string, len(outs))
	for i, o := range outs {
		parts[i] = conv(o, "v.V"+strconv.Itoa(i))
	}
	return strings.Join(parts, ", ")
}

// exprString renders an ast expression as source — the method
// boundary helpers compose small conversions as text.
func (g *gen) exprString(e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, g.fset, e); err != nil {
		panic(fmt.Sprintf("codegen: expr render failed: %v", err))
	}
	return b.String()
}

// methodType renders a type the way an interface method's signature
// must spell it — the boundary's raw form: `List T` is `[]T` and
// `Int` is `int` (the spelling `int`/`int64` collapse into one gom
// view; `int` is the interface-typical choice —
// docs/decisions/2026-11-21-go-iface-impl). Everything else keeps
// the emitted shape.
func (g *gen) methodType(t types.Type) ast.Expr {
	if el := listElem(t); el != nil {
		return &ast.ArrayType{Elt: g.goType(el)}
	}
	if types.Equal(types.Chase(t), types.Int) {
		return ast.NewIdent("int")
	}
	return g.goType(t)
}

// methodArg wraps an inner-call argument for the boundary spelling:
// a `List T` parameter takes `[]T` at the method edge and converts
// back to `list.List[T]`; an `Int` parameter takes `int` and widens
// to the inner `int64`.
func (g *gen) methodArg(name string, t types.Type) string {
	if el := listElem(t); el != nil {
		return g.pkgQual(listPath) + "List[" + g.exprString(g.goType(el)) + "](" + name + ")"
	}
	if types.Equal(types.Chase(t), types.Int) {
		return "int64(" + name + ")"
	}
	return name
}

// varDecl emits `var name T = init`. A statement-level initializer
// is wrapped in the single sanctioned IIFE
// `func() T { ... }()` (decl.go VarDecl).
func (g *gen) varDecl(d *core.VarDecl) {
	if d.Extern {
		// `@extern let` — the Go variable is hand-written, so
		// nothing emits, unless //gom:embed patterns ask codegen
		// to generate it (spec section 8.10).
		if len(d.Embed) == 0 {
			return
		}
		g.line(d.Sp)
		for _, p := range d.Embed {
			g.raw("//go:embed " + p)
			g.nl()
		}
		g.needBlankEmbed = true
		g.raw("var " + g.symName(d.Sym) + " ")
		g.typ(g.goType(d.Typ))
		g.nl()
		g.nl()
		return
	}
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
	g.resultSig(t, d.Body)
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
	name := g.testName(d.Name, d.Sp)
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
// note). A test name a sibling file of the package unit also
// declares (f.Unit.Tests names the same base) takes the file's
// fnv32a tag instead — `TestX_a1b2c3d4` — the same convention the
// lowering temps use, so two files' `test "x"` can never emit the
// same `func TestX` into the shared Go package.
func (g *gen) testName(name string, sp diag.Span) string {
	base := testFuncName(name)
	// A sibling's deduplicated suffix can land on this file's base
	// too: `test "x2"` emits TestX2, the same name a sibling's
	// repeated `test "x"` produces. Strip the numeric tail and
	// treat it as a sibling collision — the file tag dedups it.
	if strip := strings.TrimRight(base, "0123456789"); g.testCollide[base] ||
		(strip != base && g.testCollide[strip]) {
		base = base + "_" + g.testTag
	}
	// Find the first name in the dedup chain a non-test claimant
	// owns — a decl stealing the base OR a suffixed slot the test
	// would have landed on.
	taken := ""
	var owner emitRecord
	scan := func(n string) {
		if taken != "" {
			return
		}
		if o, ok := g.emitOwner[n]; ok && o.kind != emitTest {
			taken, owner = n, o
		}
	}
	scan(base)
	cand := base
	for i := 2; g.used[cand]; i++ {
		scan(cand)
		cand = base + fmt.Sprint(i)
	}
	// A numeric suffix is expected when a same-named test claimed
	// the base first (the renaming rule above); it is a collision
	// worth warning about only when a non-test claimant owns the
	// taken name — a decl stealing TestX moves `test "x"` to
	// TestX2, a decl stealing TestX2 moves it to TestX3, both
	// names the user cannot predict from the declaration.
	if taken != "" {
		g.emitDiags = append(g.emitDiags,
			diag.Warning(codeEmitNameEvict,
				"generated Go name "+q(taken)+" is taken by "+
					q(owner.gomName)+" — test "+q(name)+" emits "+q(cand)).
				At(sp).
				Set("emitted", cand).
				Set("base", base).
				Set("taken_by", owner.gomName))
	}
	g.recordEmit(emitRecord{kind: emitTest, gomName: name,
		name: cand, base: base, sp: sp})
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
// representation (P0-1's plain struct). A one-field record emits as
// the Go defined type `type Name[A] F` instead — the Gom-side newtype
// spelling, so `x.value` unwraps as `F(x)` and `{value = v}`
// constructs as `Name(v)`.
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
	if b.Defined {
		// One-field record — the Gom-side newtype spelling: the Go
		// defined type `type X F`. Lower set Defined via
		// check.DefinedField; the bare-parameter fallback (`type
		// X[A any] A` is illegal Go) stays a struct.
		g.raw(" ")
		g.typ(g.goType(b.Fields[0].Type))
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
		if f.Tag != "" {
			// Verbatim struct tag — `Name T `json:"name"``.
			g.raw(" `" + f.Tag + "`")
		}
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
