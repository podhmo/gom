package resolve

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
)

// astb builds surface-AST literals with distinct sequential spans so
// tests can assert the exact span a diagnostic attaches to.
type astb struct{ off int }

func newAstb() *astb { return &astb{} }

func (b *astb) sp() diag.Span {
	b.off += 4
	return diag.SpanIn("t.gom", b.off-4, b.off)
}

func (b *astb) id(name string) *syntax.Ident {
	return &syntax.Ident{Sp: b.sp(), Name: name}
}
func (b *astb) lit() *syntax.Lit {
	return &syntax.Lit{Sp: b.sp(), Kind: syntax.LitInt, Text: "1"}
}
func (b *astb) vp(name string) *syntax.VarPat {
	return &syntax.VarPat{Sp: b.sp(), Name: b.id(name)}
}
func (b *astb) wild() *syntax.WildPat { return &syntax.WildPat{Sp: b.sp()} }
func (b *astb) named(name string) *syntax.TypeName {
	return &syntax.TypeName{Sp: b.sp(), Name: b.id(name)}
}
func (b *astb) tvar(name string) *syntax.TypeVar {
	return &syntax.TypeVar{Sp: b.sp(), Name: b.id(name)}
}
func (b *astb) qnamed(qual, name string) *syntax.TypeName {
	return &syntax.TypeName{Sp: b.sp(), Qual: b.id(qual), Name: b.id(name)}
}
func (b *astb) sel(recv syntax.Expr, name string) *syntax.Select {
	return &syntax.Select{Sp: b.sp(), Recv: recv, Sel: b.id(name)}
}
func (b *astb) param(pat syntax.Pat, ty syntax.TypeExpr) syntax.Param {
	return syntax.Param{Sp: b.sp(), Pat: pat, Type: ty}
}

// fn decl: `fn name params -> ret = body`
func (b *astb) fn(name string, params []syntax.Param, ret syntax.TypeExpr, body syntax.Expr) *syntax.FnDecl {
	return &syntax.FnDecl{Sp: b.sp(), Name: b.id(name), Params: params, Ret: ret, Body: body}
}

// sum type: `type name = | V fields...`
func (b *astb) sumType(name string, variants ...*syntax.Variant) *syntax.TypeDecl {
	return &syntax.TypeDecl{Sp: b.sp(), Name: b.id(name),
		Body: &syntax.SumBody{Sp: b.sp(), Variants: vars(variants)}}
}
func vars(vs []*syntax.Variant) []syntax.Variant {
	out := make([]syntax.Variant, len(vs))
	for i, v := range vs {
		out[i] = *v
	}
	return out
}
func (b *astb) variant(name string, fields ...syntax.Field) *syntax.Variant {
	return &syntax.Variant{Sp: b.sp(), Name: b.id(name), Params: fields}
}
func (b *astb) namedField(name string, ty syntax.TypeExpr) syntax.Field {
	return syntax.Field{Sp: b.sp(), Name: b.id(name), Type: ty}
}
func (b *astb) posField(ty syntax.TypeExpr) syntax.Field {
	return syntax.Field{Sp: b.sp(), Type: ty}
}
func (b *astb) ctorpat(ctor syntax.Expr, args ...syntax.Pat) *syntax.CtorPat {
	ca := make([]syntax.CtorArg, len(args))
	for i, p := range args {
		ca[i] = syntax.CtorArg{Sp: b.sp(), Pat: p}
	}
	return &syntax.CtorPat{Sp: b.sp(), Ctor: ctor, Args: ca}
}
func (b *astb) match(subj syntax.Expr, arms ...syntax.MatchArm) *syntax.Match {
	return &syntax.Match{Sp: b.sp(), Subj: subj, Arms: arms}
}
func (b *astb) arm(pat syntax.Pat, body syntax.Expr) syntax.MatchArm {
	return syntax.MatchArm{Sp: b.sp(), Pat: pat, Body: body}
}
func (b *astb) block(stmts ...syntax.Expr) *syntax.Block {
	return &syntax.Block{Sp: b.sp(), Stmts: stmts}
}
func (b *astb) let(pat syntax.Pat, value syntax.Expr) *syntax.Let {
	return &syntax.Let{Sp: b.sp(), Pat: pat, Value: value}
}
func (b *astb) letdecl(pat syntax.Pat, value syntax.Expr) *syntax.LetDecl {
	return &syntax.LetDecl{Sp: b.sp(), Pat: pat, Value: value}
}
func (b *astb) file(decls ...syntax.Decl) *syntax.File {
	return &syntax.File{Sp: diag.SpanIn("t.gom", 0, 1), Decls: decls}
}

func useAt(t *testing.T, r *Resolved, id *syntax.Ident) *Symbol {
	t.Helper()
	sym, ok := r.Uses[id]
	if !ok {
		t.Fatalf("ident %q@%v has no Uses entry", id.Name, id.Sp)
	}
	return sym
}

func defAt(t *testing.T, r *Resolved, id *syntax.Ident) *Symbol {
	t.Helper()
	sym, ok := r.Defs[id]
	if !ok {
		t.Fatalf("ident %q@%v has no Defs entry", id.Name, id.Sp)
	}
	return sym
}

func codes(ds diag.List) []diag.Code {
	out := make([]diag.Code, len(ds))
	for i, d := range ds {
		out[i] = d.Code
	}
	return out
}

func hasCode(ds diag.List, code diag.Code) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestFnScopeAndShadowing(t *testing.T) {
	b := newAstb()
	// let x = 1; fn f x = { let x = 2; x } — the body's `x` is the
	// block-let binder, shadowing the parameter, which shadows the
	// file-scope let.
	topX := b.vp("x")
	paramX := b.vp("x")
	innerX := b.vp("x")
	useX := b.id("x")
	f := b.file(
		b.letdecl(topX, b.lit()),
		b.fn("f", []syntax.Param{b.param(paramX, nil)}, nil,
			b.block(b.let(innerX, b.lit()), useX)),
	)
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	top := defAt(t, res, topX.Name)
	param := defAt(t, res, paramX.Name)
	inner := defAt(t, res, innerX.Name)
	if top == param || param == inner || top == inner {
		t.Fatal("shadowed bindings share a symbol")
	}
	if got := useAt(t, res, useX); got != inner {
		t.Error("body `x` did not resolve to the innermost binding")
	}
	if got := useAt(t, res, useX); got.Kind != KindVar {
		t.Error("inner `x` is not a var")
	}
}

func TestSequentialLet(t *testing.T) {
	b := newAstb()
	// { let x = y; let z = x; z } — the rhs `y` cannot see `x` (not
	// bound yet); the second let's rhs sees the first binding.
	yUse := b.id("y")
	xBind := b.vp("x")
	xUse := b.id("x")
	zBind := b.vp("z")
	zUse := b.id("z")
	f := b.file(b.fn("f", nil, nil,
		b.block(b.let(xBind, yUse), b.let(zBind, xUse), zUse)))
	res, diags := File(f, nil)
	if !hasCode(diags, "E0201") {
		t.Fatal("expected undefined `y`, got no E0201")
	}
	// recovery: the second let still bound z, and x's use resolved.
	xsym := defAt(t, res, xBind.Name)
	if got := useAt(t, res, xUse); got != xsym {
		t.Error("sequential let did not make `x` visible")
	}
	if got := useAt(t, res, zUse); got != defAt(t, res, zBind.Name) {
		t.Error("`z` did not resolve")
	}
	// the diagnostic points at the `y` occurrence
	if got := diags[0].Span; got != yUse.Sp {
		t.Errorf("E0201 span = %v, want %v (the `y` ident)", got, yUse.Sp)
	}
}

func TestMatchArmScopes(t *testing.T) {
	b := newAstb()
	// fn f s = match s { Some x -> x, _ -> x } — the first arm's use
	// is the arm binder; the second arm's `x` is undefined because
	// arm scopes are siblings.
	someUse := b.id("x")
	badUse := b.id("x")
	armX := b.vp("x")
	f := b.file(b.fn("f", []syntax.Param{b.param(b.vp("s"), nil)}, nil,
		b.match(b.id("s"),
			b.arm(b.ctorpat(b.id("Some"), armX), someUse),
			b.arm(b.wild(), badUse))))
	res, diags := File(f, nil)
	if !hasCode(diags, "E0201") {
		t.Fatal("expected the second arm's `x` to be undefined")
	}
	if got := useAt(t, res, someUse); got != defAt(t, res, armX.Name) {
		t.Error("arm `x` did not resolve to its binder")
	}
}

func TestCtorResolution(t *testing.T) {
	b := newAstb()
	// type Shape = | Circle (r: Float) | Empty
	// match s { Circle r -> r, Shape.Empty -> 0 }
	circle := b.variant("Circle", b.namedField("r", b.named("Float")))
	empty := b.variant("Empty")
	shape := b.sumType("Shape", circle, empty)

	ctorUse := b.id("Circle")
	fieldR := b.vp("r")
	qualCtor := b.sel(b.id("Shape"), "Empty")
	f := b.file(
		shape,
		b.fn("f", []syntax.Param{b.param(b.vp("s"), nil)}, nil,
			b.match(b.id("s"),
				b.arm(b.ctorpat(ctorUse, fieldR), b.id("r")),
				b.arm(b.ctorpat(qualCtor), b.lit()))),
	)
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	circleSym := defAt(t, res, circle.Name)
	if circleSym.Kind != KindCtor {
		t.Fatalf("Circle kind = %v, want ctor", circleSym.Kind)
	}
	if got := useAt(t, res, ctorUse); got != circleSym {
		t.Error("unqualified `Circle` did not resolve to the ctor")
	}
	if got := useAt(t, res, qualCtor.Sel); got != defAt(t, res, empty.Name) {
		t.Error("`Shape.Empty` did not resolve to the Empty ctor")
	}
	// the type qualifier ident resolves to the type symbol
	if got := useAt(t, res, qualCtor.Recv.(*syntax.Ident)); got != defAt(t, res, shape.Name) {
		t.Error("`Shape` qualifier did not resolve to the type")
	}
	// named payload field gets a KindField symbol
	rSym := defAt(t, res, circle.Params[0].Name)
	if rSym.Kind != KindField {
		t.Fatalf("payload field kind = %v, want field", rSym.Kind)
	}
}

func TestCtorPatternFieldNames(t *testing.T) {
	b := newAstb()
	// type Shape = | Circle (r: Float)
	// match s { Circle { r = x } -> x }
	circle := b.variant("Circle", b.namedField("r", b.named("Float")))
	shape := b.sumType("Shape", circle)

	fieldName := syntax.FieldPat{Sp: b.sp(), Name: b.id("r"), Pat: b.vp("x")}
	pat := &syntax.CtorPat{Sp: b.sp(), Ctor: b.id("Circle"),
		Fields: []syntax.FieldPat{fieldName}}
	f := b.file(shape, b.fn("f", []syntax.Param{b.param(b.vp("s"), nil)}, nil,
		b.match(b.id("s"), b.arm(pat, b.id("x")))))
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	fieldSym := defAt(t, res, circle.Params[0].Name)
	if got := useAt(t, res, fieldName.Name); got != fieldSym {
		t.Error("`r` field name did not resolve to the field symbol")
	}
	// a misspelled field reports E0201
	b2 := newAstb()
	bad := syntax.FieldPat{Sp: b2.sp(), Name: b2.id("zz"), Pat: b2.vp("x")}
	pat2 := &syntax.CtorPat{Sp: b2.sp(), Ctor: b2.id("Circle"),
		Fields: []syntax.FieldPat{bad}}
	f2 := b2.file(
		b2.sumType("Shape", b2.variant("Circle", b2.namedField("r", b2.named("Float")))),
		b2.fn("f", []syntax.Param{b2.param(b2.vp("s"), nil)}, nil,
			b2.match(b2.id("s"), b2.arm(pat2, b2.id("x")))))
	_, diags2 := File(f2, nil)
	if !hasCode(diags2, "E0201") {
		t.Fatal("expected `Circle` has no field `zz`")
	}
	for _, d := range diags2 {
		if d.Code == "E0201" && d.Span != bad.Name.Sp {
			t.Fatalf("E0201 span = %v, want the `zz` ident", d.Span)
		}
	}
}

func TestOrPatternSharedBinders(t *testing.T) {
	b := newAstb()
	// match s { A x | B x -> x } — both `x` binders share one symbol.
	ax := b.vp("x")
	bx := b.vp("x")
	useX := b.id("x")
	f := b.file(
		b.sumType("T", b.variant("A", b.posField(b.named("Int"))),
			b.variant("B", b.posField(b.named("Int")))),
		b.fn("f", []syntax.Param{b.param(b.vp("s"), nil)}, nil,
			b.match(b.id("s"),
				b.arm(&syntax.OrPat{Sp: b.sp(), Alts: []syntax.Pat{
					b.ctorpat(b.id("A"), ax),
					b.ctorpat(b.id("B"), bx),
				}}, useX))))
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	asym := defAt(t, res, ax.Name)
	bsym := defAt(t, res, bx.Name)
	if asym != bsym {
		t.Fatal("or-pattern binders of the same name did not share a symbol")
	}
	if got := useAt(t, res, useX); got != asym {
		t.Error("arm-body `x` did not resolve to the shared binder")
	}
}

func TestTypeVariables(t *testing.T) {
	b := newAstb()
	// fn id (x: a) -> a = x — `a` binds at its first occurrence.
	pTy := b.tvar("a")
	rTy := b.tvar("a")
	fn := b.fn("id", []syntax.Param{b.param(b.vp("x"), pTy)}, rTy, b.id("x"))
	f := b.file(fn)
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	av := defAt(t, res, pTy.Name)
	if av.Kind != KindTypeVar {
		t.Fatalf("`a` kind = %v, want typevar", av.Kind)
	}
	if got := useAt(t, res, rTy.Name); got != av {
		t.Error("return `a` did not resolve to the parameter's `a`")
	}
}

func TestTypeDeclParams(t *testing.T) {
	b := newAstb()
	// type Wrap a = | Wrap a — declared param binds; a second `a` in
	// the body uses it; undeclared `b` is an error.
	pa := b.id("a")
	wrap := b.variant("Wrap", b.posField(b.tvar("a")), b.posField(b.tvar("b")))
	td := &syntax.TypeDecl{Sp: b.sp(), Name: b.id("WrapT"), Params: []*syntax.Ident{pa},
		Body: &syntax.SumBody{Sp: b.sp(), Variants: vars([]*syntax.Variant{wrap})}}
	res, diags := File(b.file(td), nil)
	if !hasCode(diags, "E0201") {
		t.Fatal("expected undeclared `b` to be E0201")
	}
	aSym := defAt(t, res, pa)
	if aSym.Kind != KindTypeVar {
		t.Fatalf("type param kind = %v", aSym.Kind)
	}
	// the first body `a` uses the declared param
	useA := wrap.Params[0].Type.(*syntax.TypeVar).Name
	if got := useAt(t, res, useA); got != aSym {
		t.Error("body `a` did not resolve to the declared param")
	}
	// the diagnostic names `b` as a typevar
	for _, d := range diags {
		if d.Code == "E0201" && d.Data["namespace"] != "typevar" {
			t.Fatalf("E0201 namespace = %v, want typevar", d.Data["namespace"])
		}
	}
}

func TestDuplicateDefinitions(t *testing.T) {
	b := newAstb()
	// fn f x x = x — two `x` params in one scope.
	f := b.file(b.fn("f",
		[]syntax.Param{b.param(b.vp("x"), nil), b.param(b.vp("x"), nil)},
		nil, b.lit()))
	_, diags := File(f, nil)
	if !hasCode(diags, "E0202") {
		t.Fatal("expected duplicate `x` to be E0202")
	}

	// let x = ...; fn x = ... — file-scope duplicate.
	b2 := newAstb()
	f2 := b2.file(
		b2.letdecl(b2.vp("x"), b2.lit()),
		b2.fn("x", nil, nil, b2.lit()))
	_, diags2 := File(f2, nil)
	if !hasCode(diags2, "E0202") {
		t.Fatal("expected file-scope duplicate to be E0202")
	}

	// inner shadowing is NOT a duplicate.
	b3 := newAstb()
	f3 := b3.file(b3.fn("f", []syntax.Param{b3.param(b3.vp("x"), nil)}, nil,
		b3.block(b3.let(b3.vp("x"), b3.lit()), b3.lit())))
	_, diags3 := File(f3, nil)
	if hasCode(diags3, "E0202") {
		t.Fatal("inner shadowing reported as duplicate")
	}
}

func TestImportAndPackageMembers(t *testing.T) {
	b := newAstb()
	// import go "fmt" — fmt.Println resolves to the member symbol.
	println := &Symbol{Name: "Println", Kind: KindFunc, IsGo: true, Package: "fmt"}
	loader := MapLoader{
		"go:fmt": {Path: "fmt", Members: map[string]*Symbol{"Println": println}},
	}
	imp := &syntax.ImportDecl{Sp: b.sp(), Go: true, Path: "fmt",
		PathSp: b.sp(), Name: b.id("fmt"), Alias: false}
	sel := b.sel(b.id("fmt"), "Println")
	f := b.file(imp, b.fn("f", nil, nil, sel))
	res, diags := File(f, &Config{Loader: loader})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	modSym := defAt(t, res, imp.Name)
	if modSym.Kind != KindModule || !modSym.IsGo || modSym.Package != "fmt" {
		t.Fatalf("import alias symbol = %+v", modSym)
	}
	if got := useAt(t, res, sel.Sel); got != println {
		t.Error("`fmt.Println` did not resolve to the loaded member")
	}
	if got := useAt(t, res, sel.Recv.(*syntax.Ident)); got != modSym {
		t.Error("`fmt` qualifier did not resolve to the module")
	}
}

func TestImportFailures(t *testing.T) {
	// no loader configured: E0203 at the path literal.
	b := newAstb()
	imp := &syntax.ImportDecl{Sp: b.sp(), Path: "foo", PathSp: b.sp(), Name: b.id("foo")}
	f := b.file(imp)
	_, diags := File(f, nil)
	if !hasCode(diags, "E0203") {
		t.Fatal("expected E0203 without a loader")
	}
	if diags[0].Span != imp.PathSp {
		t.Fatalf("E0203 span = %v, want the path literal", diags[0].Span)
	}
	// the alias still binds so member selects don't cascade errors
	res, _ := File(f, nil)
	if defAt(t, res, imp.Name).Kind != KindModule {
		t.Fatal("failed import did not bind its alias")
	}

	// loader miss: E0203 with the reason recorded.
	b2 := newAstb()
	imp2 := &syntax.ImportDecl{Sp: b2.sp(), Path: "missing", PathSp: b2.sp(), Name: b2.id("missing")}
	_, diags2 := File(b2.file(imp2), &Config{Loader: MapLoader{}})
	if !hasCode(diags2, "E0203") {
		t.Fatal("expected E0203 for unknown package")
	}
}

func TestUndefinedMember(t *testing.T) {
	b := newAstb()
	loader := MapLoader{"mypkg": {Path: "mypkg", Members: map[string]*Symbol{
		"F": {Name: "F", Kind: KindFunc, Package: "mypkg"},
	}}}
	imp := &syntax.ImportDecl{Sp: b.sp(), Path: "mypkg", PathSp: b.sp(), Name: b.id("mypkg")}
	sel := b.sel(b.id("mypkg"), "G")
	f := b.file(imp, b.fn("f", nil, nil, sel))
	_, diags := File(f, &Config{Loader: loader})
	if !hasCode(diags, "E0201") {
		t.Fatal("expected undefined member")
	}
	found := false
	for _, d := range diags {
		if d.Code == "E0201" && d.Span == sel.Sel.Sp {
			found = true
			if d.Data["namespace"] != "member" {
				t.Fatalf("namespace = %v, want member", d.Data["namespace"])
			}
		}
	}
	if !found {
		t.Fatal("no E0201 at the member ident")
	}
}

func TestUndefinedBuiltinMemberNotes(t *testing.T) {
	b := newAstb()
	// A member miss on a builtin-scope module with no injected
	// table splits two ways: List/Option/Result are implemented by
	// internal/stdlib — the miss means this compile was not wired
	// for them — while Ref/Array/Chan/Task have no members
	// anywhere yet. The E0201 note must say which.
	cases := []struct {
		name     string
		mod      string
		wantNote string
	}{
		{"stdlib not wired", "List", "not given its member table"},
		{"stdlib not implemented", "Task", "exposes no members yet"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sel := b.sel(b.id(tt.mod), "mapp")
			f := b.file(b.fn("f", nil, nil, sel))
			_, diags := File(f, &Config{})
			found := false
			for _, d := range diags {
				if d.Code != "E0201" || d.Data["namespace"] != "member" {
					continue
				}
				found = true
				var note string
				for _, n := range d.Notes {
					note += n
				}
				if !strings.Contains(note, tt.wantNote) {
					t.Fatalf("note %q does not contain %q", note, tt.wantNote)
				}
				if strings.Contains(note, "P4-8") {
					t.Fatalf("note %q still names the closed task", note)
				}
			}
			if !found {
				t.Fatalf("no E0201 member diagnostic in %+v", diags)
			}
		})
	}
}

func TestBuiltinCtorQualified(t *testing.T) {
	b := newAstb()
	// `Option.Some` in expression position: `Option` resolves to the
	// builtin module; `Some` falls back to the Option type's ctor.
	sel := b.sel(b.id("Option"), "Some")
	f := b.file(b.fn("f", nil, nil, sel))
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	sym := useAt(t, res, sel.Sel)
	if sym.Kind != KindCtor || sym.Name != "Some" {
		t.Fatalf("`Option.Some` resolved to %+v", sym)
	}
	// bare `Some` also resolves (builtin scope).
	b2 := newAstb()
	bare := b2.id("Some")
	f2 := b2.file(b2.fn("f", nil, nil, bare))
	res2, diags2 := File(f2, nil)
	if diags2.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags2))
	}
	if got := useAt(t, res2, bare); got.Kind != KindCtor {
		t.Error("bare `Some` is not a ctor")
	}
}

func TestTestOnlyVisibility(t *testing.T) {
	b := newAstb()
	// @test fn helper ... ; fn f = helper — non-test code cannot
	// reference it; inside `test` it can.
	helper := b.fn("helper", nil, nil, b.lit())
	helper.TestOnly = true
	badUse := b.id("helper")
	goodUse := b.id("helper")
	f := b.file(
		helper,
		b.fn("f", nil, nil, badUse),
		&syntax.TestDecl{Sp: b.sp(), Name: "t", Body: b.block(goodUse)},
	)
	res, diags := File(f, nil)
	if !hasCode(diags, "E0802") {
		t.Fatal("expected E0802 for non-test use of @test")
	}
	for _, d := range diags {
		if d.Code == "E0802" && d.Span != badUse.Sp {
			t.Fatalf("E0802 span = %v, want the non-test `helper`", d.Span)
		}
	}
	// the test-block use still resolved (recovery), to the same sym
	if got := useAt(t, res, goodUse); got != defAt(t, res, helper.Name) {
		t.Error("test-block `helper` did not resolve")
	}
	if got := useAt(t, res, badUse); got.TestOnly != true {
		t.Error("bad use did not resolve to the test-only sym")
	}
}

func TestSelectArmScopes(t *testing.T) {
	b := newAstb()
	// select { x <- ch -> x, default -> () } — the arm binds x.
	bind := b.vp("x")
	use := b.id("x")
	f := b.file(b.fn("f", []syntax.Param{b.param(b.vp("ch"), nil)}, nil,
		&syntax.SelectExpr{Sp: b.sp(), Arms: []syntax.SelectArm{
			&syntax.RecvArm{Sp: b.sp(), Bind: bind, Chan: b.id("ch"), Body: use},
			&syntax.DefaultArm{Sp: b.sp(), Body: b.lit()},
		}}))
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, use); got != defAt(t, res, bind.Name) {
		t.Error("recv-arm binder did not scope its body")
	}
}

func TestForScope(t *testing.T) {
	b := newAstb()
	bind := b.vp("x")
	use := b.id("x")
	f := b.file(b.fn("f", []syntax.Param{b.param(b.vp("xs"), nil)}, nil,
		&syntax.For{Sp: b.sp(), Pat: bind, Iter: b.id("xs"),
			Body: b.block(use)}))
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, use); got != defAt(t, res, bind.Name) {
		t.Error("for-loop binder did not scope its body")
	}
}

func TestLambdaScope(t *testing.T) {
	b := newAstb()
	p := b.vp("x")
	use := b.id("x")
	f := b.file(b.fn("f", nil, nil,
		&syntax.Lambda{Sp: b.sp(), Params: []syntax.Param{b.param(p, nil)}, Body: use}))
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, use); got != defAt(t, res, p.Name) {
		t.Error("lambda param did not scope its body")
	}
}

func TestExternSignature(t *testing.T) {
	b := newAstb()
	writer := &Symbol{Name: "Writer", Kind: KindType, IsGo: true, Package: "io"}
	loader := MapLoader{
		"go:io": {Path: "io", Members: map[string]*Symbol{"Writer": writer}},
	}
	// extern go "io" { fn Copy (~dst: Writer) -> Int64 } — the
	// unqualified Writer resolves to the package member first.
	ty := b.named("Writer")
	ext := &syntax.ExternDecl{Sp: b.sp(), Path: "io", PathSp: b.sp(),
		Entries: []syntax.ExternEntry{&syntax.ExternFn{Sp: b.sp(), Name: b.id("Copy"),
			Params: []syntax.Param{{
				Sp:    b.sp(),
				Label: b.id("dst"),
				Pat:   b.vp("dst"),
				Type:  ty,
			}},
			Ret: b.named("Int64")}}}
	f := b.file(ext)
	res, diags := File(f, &Config{Loader: loader})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, ty.Name); got != writer {
		t.Error("extern `Writer` did not resolve to the io member")
	}
	// the entry's name binds a Go function symbol
	copySym := defAt(t, res, ext.Entries[0].(*syntax.ExternFn).Name)
	if !copySym.IsGo || copySym.Package != "io" {
		t.Fatalf("extern entry symbol = %+v", copySym)
	}
}

// ---------------------------------------------------------------------
// Completeness oracle: after File, every identifier occurrence must be
// either in Defs (binder), in Uses (reference), exempt (a positional
// or type-directed ident the resolver deliberately leaves for check),
// or covered by a diagnostic at its own span (undefined name).
// ---------------------------------------------------------------------

type identWalker struct {
	occurrences []*syntax.Ident
	exempt      []*syntax.Ident
}

func (w *identWalker) occ(id *syntax.Ident) {
	if id != nil {
		w.occurrences = append(w.occurrences, id)
	}
}
func (w *identWalker) exc(id *syntax.Ident) {
	if id != nil {
		w.exempt = append(w.exempt, id)
	}
}

func (w *identWalker) file(f *syntax.File) {
	w.exc(f.Package) // the package name is not a symbol occurrence
	for _, d := range f.Decls {
		w.decl(d)
	}
}

func (w *identWalker) decl(d syntax.Decl) {
	switch n := d.(type) {
	case *syntax.FnDecl:
		w.occ(n.Name)
		for i := range n.Params {
			w.param(&n.Params[i])
		}
		w.typeExpr(n.Ret)
		w.expr(n.Body)
	case *syntax.LetDecl:
		w.pat(n.Pat)
		w.typeExpr(n.Type)
		w.expr(n.Value)
	case *syntax.TypeDecl:
		w.occ(n.Name)
		for _, p := range n.Params {
			w.occ(p)
		}
		w.typeBody(n.Body)
	case *syntax.ImportDecl:
		w.occ(n.Name)
	case *syntax.ExternDecl:
		for _, e := range n.Entries {
			if fn, ok := e.(*syntax.ExternFn); ok {
				w.occ(fn.Name)
				for i := range fn.Params {
					w.param(&fn.Params[i])
				}
				w.typeExpr(fn.Ret)
			}
		}
	case *syntax.TestDecl:
		w.expr(n.Body)
	default:
		panic("unreachable")
	}
}

func (w *identWalker) param(p *syntax.Param) {
	w.occ(p.Label) // param labels get Defs entries
	w.pat(p.Pat)
	w.typeExpr(p.Type)
}

func (w *identWalker) typeBody(b syntax.TypeBody) {
	switch n := b.(type) {
	case nil:
	case *syntax.SumBody:
		for i := range n.Variants {
			v := &n.Variants[i]
			w.occ(v.Name)
			for j := range v.Params {
				w.field(&v.Params[j])
			}
		}
	case *syntax.RecordBody:
		for i := range n.Fields {
			w.field(&n.Fields[i])
		}
	case *syntax.AliasBody:
		w.typeExpr(n.Type)
	case *syntax.AbstractBody:
	default:
		panic("unreachable")
	}
}

// field handles a declaration-position Field: its name and label are
// Defs (bindFields), its type is an occurrence context.
func (w *identWalker) field(f *syntax.Field) {
	w.occ(f.Name)
	w.occ(f.Label)
	w.typeExpr(f.Type)
}

func (w *identWalker) pat(p syntax.Pat) {
	switch n := p.(type) {
	case nil:
	case *syntax.WildPat, *syntax.LitPat:
	case *syntax.VarPat:
		w.occ(n.Name)
	case *syntax.CtorPat:
		w.expr(n.Ctor) // ctor heads resolve like expression idents
		for i := range n.Args {
			a := &n.Args[i]
			w.exc(a.Label) // label position — resolved only for local ctors
			w.pat(a.Pat)
		}
		for i := range n.Fields {
			f := &n.Fields[i]
			if f.Punned() {
				w.occ(f.Name) // binder
			} else {
				w.exc(f.Name) // field position — resolved only for local ctors
				w.pat(f.Pat)
			}
		}
	case *syntax.TuplePat:
		for _, e := range n.Elems {
			w.pat(e)
		}
	case *syntax.RecordPat:
		for i := range n.Fields {
			f := &n.Fields[i]
			if f.Punned() {
				w.occ(f.Name)
			} else {
				w.exc(f.Name)
				w.pat(f.Pat)
			}
		}
	case *syntax.ListPat:
		for _, e := range n.Elems {
			w.pat(e)
		}
		w.pat(n.Rest)
	case *syntax.OrPat:
		for _, e := range n.Alts {
			w.pat(e)
		}
	case *syntax.AsPat:
		w.pat(n.Pat)
		w.occ(n.Name)
	default:
		panic("unreachable")
	}
}

func (w *identWalker) expr(e syntax.Expr) {
	switch n := e.(type) {
	case nil:
	case *syntax.Ident:
		w.occ(n)
	case *syntax.Lit:
	case *syntax.StrInterp:
		for _, p := range n.Parts {
			switch part := p.(type) {
			case *syntax.StrText:
			case *syntax.StrExpr:
				w.expr(part.X)
			default:
				panic("unreachable")
			}
		}
	case *syntax.Apply:
		w.expr(n.Fn)
		for i := range n.Args {
			a := &n.Args[i]
			w.exc(a.Label) // call-site label — type-directed
			w.expr(a.Value)
		}
	case *syntax.Select:
		w.expr(n.Recv)
		w.exc(n.Sel) // member selection — resolved for module/type qualifiers only
	case *syntax.Unary:
		w.expr(n.X)
	case *syntax.Binary:
		w.expr(n.X)
		w.expr(n.Y)
	case *syntax.Hole:
	case *syntax.Lambda:
		for i := range n.Params {
			w.param(&n.Params[i])
		}
		w.expr(n.Body)
	case *syntax.If:
		w.expr(n.Cond)
		w.expr(n.Then)
		w.expr(n.Else)
	case *syntax.Match:
		w.expr(n.Subj)
		for i := range n.Arms {
			a := &n.Arms[i]
			w.pat(a.Pat)
			w.expr(a.Guard)
			w.expr(a.Body)
		}
	case *syntax.Block:
		for _, s := range n.Stmts {
			w.expr(s)
		}
	case *syntax.Let:
		w.pat(n.Pat)
		w.typeExpr(n.Type)
		w.expr(n.Value)
	case *syntax.Ignore:
		w.expr(n.X)
	case *syntax.Try:
		w.expr(n.X)
	case *syntax.GoExpr:
		w.expr(n.Body)
	case *syntax.For:
		w.pat(n.Pat)
		w.expr(n.Iter)
		w.expr(n.Body)
	case *syntax.SelectExpr:
		for _, a := range n.Arms {
			switch arm := a.(type) {
			case *syntax.RecvArm:
				w.pat(arm.Bind)
				w.expr(arm.Chan)
				w.expr(arm.Body)
			case *syntax.SendArm:
				w.expr(arm.Chan)
				w.expr(arm.Value)
				w.expr(arm.Body)
			case *syntax.DefaultArm:
				w.expr(arm.Body)
			default:
				panic("unreachable")
			}
		}
	case *syntax.ListLit:
		for _, e := range n.Elems {
			w.expr(e)
		}
	case *syntax.Spread:
		w.expr(n.X)
	case *syntax.TupleLit:
		for _, e := range n.Elems {
			w.expr(e)
		}
	case *syntax.Ascribe:
		w.expr(n.X)
		w.typeExpr(n.Type)
	case *syntax.RecordLit:
		w.expr(n.Base)
		for i := range n.Fields {
			w.exc(n.Fields[i].Name) // record field — type-directed
			w.expr(n.Fields[i].Value)
		}
	default:
		panic("unreachable")
	}
}

func (w *identWalker) typeExpr(e syntax.TypeExpr) {
	switch n := e.(type) {
	case nil:
	case *syntax.TypeVar:
		w.occ(n.Name)
	case *syntax.TypeName:
		w.occ(n.Qual)
		w.occ(n.Name)
	case *syntax.TypeApp:
		w.typeExpr(n.Ctor)
		for _, a := range n.Args {
			w.typeExpr(a)
		}
	case *syntax.TypeFn:
		for i := range n.Params {
			p := &n.Params[i]
			w.exc(p.Label) // external parameter name — not a symbol
			w.typeExpr(p.Type)
		}
		w.typeExpr(n.Ret)
	case *syntax.TypeTuple:
		for _, e := range n.Elems {
			w.typeExpr(e)
		}
	case *syntax.TypeRecord:
		for i := range n.Fields {
			w.field(&n.Fields[i])
		}
	default:
		panic("unreachable")
	}
}

// checkComplete asserts the oracle: every occurrence-position ident
// is in Defs or Uses, or is covered by a diagnostic at its span.
func checkComplete(t *testing.T, res *Resolved, ds diag.List) {
	t.Helper()
	var w identWalker
	w.file(res.File)
	in := func(m map[*syntax.Ident]*Symbol, id *syntax.Ident) bool {
		_, ok := m[id]
		return ok
	}
	exempt := func(id *syntax.Ident) bool {
		for _, e := range w.exempt {
			if e == id {
				return true
			}
		}
		return false
	}
	for _, id := range w.occurrences {
		if in(res.Defs, id) || in(res.Uses, id) || exempt(id) {
			continue
		}
		covered := false
		for _, d := range ds {
			if d.Span == id.Sp {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("ident %q@%v in neither Defs nor Uses and no diagnostic covers it", id.Name, id.Sp)
		}
	}
	// no ident is in both tables at once unless it rebinds (never)
	for id := range res.Uses {
		if in(res.Defs, id) {
			t.Errorf("ident %q@%v is in both Defs and Uses", id.Name, id.Sp)
		}
	}
}

func TestCompletenessOracle(t *testing.T) {
	b := newAstb()
	// a kitchen-sink file exercising every binder and reference kind.
	shape := b.sumType("Shape",
		b.variant("Circle", b.namedField("r", b.named("Float"))),
		b.variant("Empty"))
	imp := &syntax.ImportDecl{Sp: b.sp(), Path: "mypkg", PathSp: b.sp(), Name: b.id("mypkg")}
	loader := MapLoader{"mypkg": {Path: "mypkg", Members: map[string]*Symbol{
		"F": {Name: "F", Kind: KindFunc, Package: "mypkg"},
	}}}
	pS := b.vp("s")
	pX := b.vp("x")
	ann := b.tvar("a")
	f := b.file(
		shape,
		imp,
		b.letdecl(b.vp("top"), b.lit()),
		b.fn("f",
			[]syntax.Param{b.param(pS, b.named("Shape")), b.param(pX, b.tvar("a"))},
			b.tvar("a"),
			b.block(
				b.let(b.vp("y"), b.sel(b.id("mypkg"), "F")),
				b.match(b.id("s"),
					b.arm(b.ctorpat(b.id("Circle"), b.vp("r")),
						&syntax.Apply{Sp: b.sp(), Fn: b.id("y"),
							Args: []syntax.Arg{{Sp: b.sp(), Value: b.id("r")}}}),
					b.arm(b.ctorpat(b.sel(b.id("Shape"), "Empty")), b.id("x"))),
			)),
		&syntax.TypeDecl{Sp: b.sp(), Name: b.id("Wrap"), Params: []*syntax.Ident{ann.Name},
			Body: &syntax.AliasBody{Sp: b.sp(), Type: b.tvar("a")}},
	)
	res, diags := File(f, &Config{Loader: loader})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	checkComplete(t, res, diags)
	// the implicit `a` in the fn signature bound at its first site
	if _, ok := res.Defs[ann.Name]; !ok {
		// ann.Name is the TYPE DECL param — check it separately below
		t.Error("type decl param `a` not in Defs")
	}
}

func TestCompletenessWithErrors(t *testing.T) {
	b := newAstb()
	// fn f s = match s { Unknown x -> x, _ -> nosuch } — undefined
	// ctor and var still leave a complete-or-diagnosed file.
	f := b.file(b.fn("f", []syntax.Param{b.param(b.vp("s"), nil)}, nil,
		b.match(b.id("s"),
			b.arm(b.ctorpat(b.id("Unknown"), b.vp("x")), b.id("x")),
			b.arm(b.wild(), b.id("nosuch")))))
	res, diags := File(f, nil)
	if !diags.HasError() {
		t.Fatal("expected diagnostics")
	}
	checkComplete(t, res, diags)
}

func TestAmbiguousCtorIsDuplicate(t *testing.T) {
	b := newAstb()
	// two variants named Leaf in one file — a duplicate definition in
	// the file's term namespace (qualified `A.Leaf`/`B.Leaf` is the
	// disambiguation).
	f := b.file(
		b.sumType("A", b.variant("Leaf")),
		b.sumType("B", b.variant("Leaf")),
	)
	res, diags := File(f, nil)
	if !hasCode(diags, "E0202") {
		t.Fatal("expected E0202 for the colliding ctor name")
	}
	// recovery: both idents still got Defs entries; a bare `Leaf`
	// reference resolves to the last-defined symbol.
	_ = res
}

func TestQualifiedTypeName(t *testing.T) {
	b := newAstb()
	shape := &Symbol{Name: "Shape", Kind: KindType, Package: "geom"}
	loader := MapLoader{"geom": {Path: "geom", Members: map[string]*Symbol{
		"Shape": shape,
	}}}
	imp := &syntax.ImportDecl{Sp: b.sp(), Path: "geom", PathSp: b.sp(), Name: b.id("geom")}
	ty := b.qnamed("geom", "Shape")
	f := b.file(imp, b.fn("f", []syntax.Param{b.param(b.vp("s"), ty)}, nil, b.lit()))
	res, diags := File(f, &Config{Loader: loader})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, ty.Name); got != shape {
		t.Error("`geom.Shape` did not resolve to the imported type")
	}
	if got := useAt(t, res, ty.Qual); got != defAt(t, res, imp.Name) {
		t.Error("`geom` qualifier did not resolve to the module")
	}
}

func TestRecordTypeFields(t *testing.T) {
	b := newAstb()
	// fn f (x: { name: String }) = ... — the record-type field gets a
	// KindField Defs entry.
	rt := &syntax.TypeRecord{Sp: b.sp(), Fields: []syntax.Field{
		b.namedField("name", b.named("String")),
	}}
	f := b.file(b.fn("f", []syntax.Param{b.param(b.vp("x"), rt)}, nil, b.lit()))
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := defAt(t, res, rt.Fields[0].Name); got.Kind != KindField {
		t.Fatalf("record-type field kind = %v, want field", got.Kind)
	}
}

func TestCtorShadowing(t *testing.T) {
	b := newAstb()
	// a file-declared `Some` ctor shadows the builtin Some — the
	// reference resolves to the file's ctor.
	some := b.variant("Some", b.posField(b.named("Int")))
	use := b.id("Some")
	f := b.file(
		b.sumType("MyOpt", some),
		b.fn("f", []syntax.Param{b.param(b.vp("s"), nil)}, nil,
			b.match(b.id("s"), b.arm(b.ctorpat(use, b.wild()), b.lit()))),
	)
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, use); got != defAt(t, res, some.Name) {
		t.Error("file `Some` did not shadow the builtin")
	}
}

func TestSignatureOnlyFnBindsParams(t *testing.T) {
	// .gomi shape: `fn f (~x: Int) (~out: y) -> Int` with no body —
	// parameter binders still get Defs entries, like extern entries.
	b := newAstb()
	label := b.id("x")
	pat := b.vp("x")
	otherLabel := b.id("out")
	otherPat := b.vp("y")
	sig := b.fn("f",
		[]syntax.Param{
			{Sp: b.sp(), Label: label, Pat: pat, Type: b.named("Int")},
			{Sp: b.sp(), Label: otherLabel, Pat: otherPat},
		},
		b.named("Int"), nil) // Body nil — a signature, not a definition
	f := b.file(sig)
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	checkComplete(t, res, diags)
	sym := defAt(t, res, pat.Name)
	if sym.Kind != KindVar {
		t.Fatalf("param kind = %v, want var", sym.Kind)
	}
	// a `~name` label on a same-named VarPat shares the variable's
	// symbol; on a differently named pattern it gets its own.
	if got := defAt(t, res, label); got != sym {
		t.Error("~x label on a same-named VarPat did not share its symbol")
	}
	other := defAt(t, res, otherLabel)
	if other.Kind != KindVar {
		t.Fatalf("~out label kind = %v, want var", other.Kind)
	}
	if other == defAt(t, res, otherPat.Name) {
		t.Error("~out label on a different VarPat shared the variable's symbol")
	}
}

func TestQualCtorTermWinsTypeFallback(t *testing.T) {
	// `fn Shape` (a term) + `type Shape = | Circle`: `Shape.Circle`
	// in pattern position reads as type-qualified — Uses[Shape] is
	// the TYPE symbol so qualifier and ctor agree.
	b := newAstb()
	circle := b.variant("Circle")
	shape := b.sumType("Shape", circle)
	shapeFn := b.fn("Shape", nil, nil, b.lit())
	sel := b.sel(b.id("Shape"), "Circle")
	f := b.file(
		shape, shapeFn,
		b.fn("f", []syntax.Param{b.param(b.vp("s"), nil)}, nil,
			b.match(b.id("s"), b.arm(b.ctorpat(sel), b.lit()))))
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, sel.Recv.(*syntax.Ident)); got != defAt(t, res, shape.Name) {
		t.Error("`Shape` qualifier did not resolve to the type")
	}
	if got := useAt(t, res, sel.Sel); got != defAt(t, res, circle.Name) {
		t.Error("`Circle` did not resolve to the type's ctor")
	}
}

func TestQualCtorTermWinsTypeFallbackTestOnly(t *testing.T) {
	// Same fallback, but the shadowing term is @test-only: the
	// qualifier resolves to the type, so non-test code must not
	// report E0802 for the hidden term.
	b := newAstb()
	circle := b.variant("Circle")
	shape := b.sumType("Shape", circle)
	shapeFn := b.fn("Shape", nil, nil, b.lit())
	shapeFn.TestOnly = true
	sel := b.sel(b.id("Shape"), "Circle")
	f := b.file(
		shape, shapeFn,
		b.fn("f", []syntax.Param{b.param(b.vp("s"), nil)}, nil,
			b.match(b.id("s"), b.arm(b.ctorpat(sel), b.lit()))))
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, sel.Recv.(*syntax.Ident)); got != defAt(t, res, shape.Name) {
		t.Error("`Shape` qualifier did not resolve to the type")
	}
	if got := useAt(t, res, sel.Sel); got != defAt(t, res, circle.Name) {
		t.Error("`Circle` did not resolve to the type's ctor")
	}
}

func TestQualCtorQualifierIsTerm(t *testing.T) {
	// `v.C` in pattern position where v is a variable (no such type):
	// the qualifier still resolves to the term found, then reports
	// "is not a package".
	b := newAstb()
	pv := b.vp("v")
	sel := b.sel(b.id("v"), "C")
	f := b.file(b.fn("f", []syntax.Param{b.param(pv, nil)}, nil,
		b.match(b.id("v"), b.arm(b.ctorpat(sel), b.lit()))))
	res, diags := File(f, nil)
	if !hasCode(diags, "E0201") {
		t.Fatal("expected `v is not a package`")
	}
	if got := useAt(t, res, sel.Recv.(*syntax.Ident)); got != defAt(t, res, pv.Name) {
		t.Error("qualifier did not resolve to the variable")
	}
}

func TestMemberCtorNotCtor(t *testing.T) {
	// `m.F` in pattern position where m.F is a function member: the
	// member still resolves (Uses) and reports "is not a constructor".
	b := newAstb()
	member := &Symbol{Name: "F", Kind: KindFunc, Package: "m"}
	loader := MapLoader{"m": {Path: "m", Members: map[string]*Symbol{"F": member}}}
	imp := &syntax.ImportDecl{Sp: b.sp(), Path: "m", PathSp: b.sp(), Name: b.id("m")}
	sel := b.sel(b.id("m"), "F")
	f := b.file(imp, b.fn("f", []syntax.Param{b.param(b.vp("s"), nil)}, nil,
		b.match(b.id("s"), b.arm(b.ctorpat(sel), b.lit()))))
	res, diags := File(f, &Config{Loader: loader})
	if !hasCode(diags, "E0201") {
		t.Fatal("expected `F is not a constructor`")
	}
	if got := useAt(t, res, sel.Sel); got != member {
		t.Error("`m.F` did not resolve to the member")
	}
}

func TestTopLevelMutualVisibility(t *testing.T) {
	// fn f x = g x; fn g x = f x — bodies see top-level names bound
	// later in the file (mutual recursion).
	b := newAstb()
	fCall := b.id("g")
	gCall := b.id("f")
	fdecl := b.fn("f", []syntax.Param{b.param(b.vp("x"), nil)}, nil,
		&syntax.Apply{Sp: b.sp(), Fn: fCall, Args: []syntax.Arg{{Sp: b.sp(), Value: b.id("x")}}})
	gdecl := b.fn("g", []syntax.Param{b.param(b.vp("x"), nil)}, nil,
		&syntax.Apply{Sp: b.sp(), Fn: gCall, Args: []syntax.Arg{{Sp: b.sp(), Value: b.id("x")}}})
	res, diags := File(b.file(fdecl, gdecl), nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, fCall); got != defAt(t, res, gdecl.Name) {
		t.Error("f's body did not resolve the later `g`")
	}
	if got := useAt(t, res, gCall); got != defAt(t, res, fdecl.Name) {
		t.Error("g's body did not resolve `f`")
	}
}

func TestTestOnlyLetBinding(t *testing.T) {
	// @test let helper = 1 — the binder is marked TestOnly; a
	// non-test use reports E0802 and still resolves.
	b := newAstb()
	bind := b.vp("helper")
	ld := b.letdecl(bind, b.lit())
	ld.TestOnly = true
	bad := b.id("helper")
	f := b.file(ld, b.fn("f", nil, nil, bad))
	res, diags := File(f, nil)
	if !hasCode(diags, "E0802") {
		t.Fatal("expected E0802 for non-test use of @test let")
	}
	sym := defAt(t, res, bind.Name)
	if !sym.TestOnly {
		t.Error("@test let binder is not marked TestOnly")
	}
	if got := useAt(t, res, bad); got != sym {
		t.Error("non-test use did not resolve to the @test sym")
	}
}

func TestNestedQualificationResolvesFirstHop(t *testing.T) {
	// `pkg.Type.Ctor`: the first hop `pkg.Type` resolves; the second
	// selector is left for check (flat member tables in P1-4).
	b := newAstb()
	ty := &Symbol{Name: "Shape", Kind: KindType, Package: "geom"}
	loader := MapLoader{"geom": {Path: "geom", Members: map[string]*Symbol{"Shape": ty}}}
	imp := &syntax.ImportDecl{Sp: b.sp(), Path: "geom", PathSp: b.sp(), Name: b.id("geom")}
	inner := b.sel(b.id("geom"), "Shape")
	outer := b.sel(inner, "Circle")
	f := b.file(imp, b.fn("f", nil, nil, outer))
	res, diags := File(f, &Config{Loader: loader})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, inner.Sel); got != ty {
		t.Error("`geom.Shape` did not resolve to the member")
	}
	if _, ok := res.Uses[outer.Sel]; ok {
		t.Error("second-hop selector got a Uses entry — should be left for check")
	}
}

func TestFailedImportMemberSelectsAreSuppressed(t *testing.T) {
	// `missing.F` where the import failed to load: the import site
	// reports E0203 and member lookups stay unresolved silently.
	b := newAstb()
	imp := &syntax.ImportDecl{Sp: b.sp(), Path: "missing", PathSp: b.sp(), Name: b.id("missing")}
	sel := b.sel(b.id("missing"), "F")
	f := b.file(imp, b.fn("f", nil, nil, sel))
	res, diags := File(f, &Config{Loader: MapLoader{}})
	if len(diags) != 1 || diags[0].Code != "E0203" {
		t.Fatalf("diags = %v, want exactly E0203", codes(diags))
	}
	if got := useAt(t, res, sel.Recv.(*syntax.Ident)); got != defAt(t, res, imp.Name) {
		t.Error("`missing` qualifier did not resolve to the module")
	}
	if _, ok := res.Uses[sel.Sel]; ok {
		t.Error("member of a failed package got a Uses entry")
	}
}

func TestImportAliasDuplicate(t *testing.T) {
	// two imports binding the same name — one file scope, E0202.
	b := newAstb()
	imp1 := &syntax.ImportDecl{Sp: b.sp(), Path: "a", PathSp: b.sp(), Name: b.id("m"), Alias: true}
	imp2 := &syntax.ImportDecl{Sp: b.sp(), Path: "b", PathSp: b.sp(), Name: b.id("m"), Alias: true}
	_, diags := File(b.file(imp1, imp2), &Config{Loader: MapLoader{}})
	if !hasCode(diags, "E0202") {
		t.Fatal("expected duplicate import alias to be E0202")
	}
}

func TestImportAndImportGoDoNotShareCache(t *testing.T) {
	// `import "x"` and `import go "x"` are different packages —
	// pkgCache keys on (path, goPkg).
	b := newAstb()
	gomF := &Symbol{Name: "F", Kind: KindFunc, Package: "x"}
	goG := &Symbol{Name: "G", Kind: KindFunc, IsGo: true, Package: "x"}
	loader := MapLoader{
		"x":    {Path: "x", Members: map[string]*Symbol{"F": gomF}},
		"go:x": {Path: "x", Members: map[string]*Symbol{"G": goG}},
	}
	gomImp := &syntax.ImportDecl{Sp: b.sp(), Path: "x", PathSp: b.sp(), Name: b.id("x")}
	goImp := &syntax.ImportDecl{Sp: b.sp(), Go: true, Path: "x", PathSp: b.sp(), Name: b.id("gx"), Alias: true}
	selGom := b.sel(b.id("x"), "F")
	selGo := b.sel(b.id("gx"), "G")
	f := b.file(gomImp, goImp, b.fn("f", nil, nil, b.block(selGom, selGo)))
	res, diags := File(f, &Config{Loader: loader})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, selGom.Sel); got != gomF {
		t.Error("`x.F` did not resolve to the Gom member")
	}
	if got := useAt(t, res, selGo.Sel); got != goG {
		t.Error("`gx.G` did not resolve to the Go member")
	}
	if defAt(t, res, goImp.Name) != useAt(t, res, selGo.Recv.(*syntax.Ident)) {
		t.Error("`gx` qualifier did not resolve to its import alias")
	}
}

func TestExternOverlayDoesNotLeakToGomImport(t *testing.T) {
	// `extern go "io"` must not overlay a Gom `import "io"` — the
	// extern table is keyed per (path, goPkg) like pkgCache.
	b := newAstb()
	gomMember := &Symbol{Name: "GomFn", Kind: KindFunc, Package: "io"}
	loader := MapLoader{
		"io":    {Path: "io", Members: map[string]*Symbol{"GomFn": gomMember}},
		"go:io": {Path: "io", Members: map[string]*Symbol{}},
	}
	ext := &syntax.ExternDecl{Sp: b.sp(), Path: "io", PathSp: b.sp(),
		Entries: []syntax.ExternEntry{&syntax.ExternFn{Sp: b.sp(), Name: b.id("Extra")}}}
	imp := &syntax.ImportDecl{Sp: b.sp(), Path: "io", PathSp: b.sp(), Name: b.id("io")}
	good := b.sel(b.id("io"), "GomFn")
	bad := b.sel(b.id("io"), "Extra")
	f := b.file(ext, imp, b.fn("f", nil, nil, b.block(good, bad)))
	res, diags := File(f, &Config{Loader: loader})
	if got := useAt(t, res, good.Sel); got != gomMember {
		t.Error("`io.GomFn` did not resolve to the Gom member")
	}
	if _, ok := res.Uses[bad.Sel]; ok {
		t.Error("`io.Extra` resolved — the extern table leaked into the Gom import")
	}
	if !hasCode(diags, "E0201") {
		t.Fatal("expected undefined member for `io.Extra`")
	}
}

func TestExternDuplicateEntries(t *testing.T) {
	// two extern entries of one name — same table, E0202.
	b := newAstb()
	e1 := &syntax.ExternFn{Sp: b.sp(), Name: b.id("Copy")}
	e2 := &syntax.ExternFn{Sp: b.sp(), Name: b.id("Copy")}
	ext := &syntax.ExternDecl{Sp: b.sp(), Path: "io", PathSp: b.sp(),
		Entries: []syntax.ExternEntry{e1, e2}}
	res, diags := File(b.file(ext), nil)
	if !hasCode(diags, "E0202") {
		t.Fatal("expected duplicate extern entry to be E0202")
	}
	// recovery: both binder idents still got Defs entries
	defAt(t, res, e1.Name)
	defAt(t, res, e2.Name)
}

func TestBlockLetAnnotationSeesSignatureTVars(t *testing.T) {
	// fn f (p: a) = { let x : a = p; x } — the let's annotation sees
	// the signature's type variable through the tvScope chain.
	b := newAstb()
	pTy := b.tvar("a")
	ann := b.tvar("a")
	f := b.file(b.fn("f",
		[]syntax.Param{b.param(b.vp("p"), pTy)}, nil,
		b.block(
			&syntax.Let{Sp: b.sp(), Pat: b.vp("x"), Type: ann, Value: b.id("p")},
			b.id("x"))))
	res, diags := File(f, nil)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
	if got := useAt(t, res, ann.Name); got != defAt(t, res, pTy.Name) {
		t.Error("let annotation `a` did not resolve to the signature's `a`")
	}
	checkComplete(t, res, diags)
}
