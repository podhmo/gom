package check

// Builders for resolved-name tables and declarations: symbols for Go
// functions, methods, modules, builtin constructors, and the `type`
// declaration forms.

import (
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// ---- symbol factories -----------------------------------------------

// modSym is an imported package name, e.g. `fmt`.
func (t *tb) modSym(name string, isGo bool) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindModule, IsGo: isGo, Package: name}
}

// gofnSym is a Go package function, e.g. `strings.Repeat`.
func (t *tb) gofnSym(pkg, name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindFunc, IsGo: true, Package: pkg}
}

// methodSym is a Go method symbol.
func (t *tb) methodSym(pkg, name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindMethod, IsGo: true, Package: pkg}
}

// govarSym is a Go package variable or constant, e.g. `os.Args`.
func (t *tb) govarSym(pkg, name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindVar, IsGo: true, Package: pkg}
}

// gotypeSym is a Go named type, e.g. `bytes.Buffer`.
func (t *tb) gotypeSym(pkg, name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindType, IsGo: true, Package: pkg}
}

// ctorSym is an undeclared-in-this-file constructor symbol — used for
// the builtin-scope constructors (Some, None, Ok, Err) the resolver
// binds from the builtin scope.
func (t *tb) ctorSym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindCtor}
}

// builtinSym is a builtin-scope function name (assert, assert_eq,
// todo, Go.conv).
func (t *tb) builtinSym(name, pkg string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindBuiltin, Package: pkg}
}

// member builds `recv.sel` with recv bound to mod and sel bound to
// member.
func (t *tb) member(mod, member *resolve.Symbol, selName string) *syntax.Select {
	recv := t.id(mod.Name)
	t.uses[recv] = mod
	return t.sel(recv, member, selName)
}

// ---- declaration builders -------------------------------------------

// unitParam is the `()` parameter.
func (t *tb) unitParam() syntax.Param {
	l := t.ulit()
	return syntax.Param{Sp: l.Sp, Pat: t.litp(l)}
}

// typeDecl builds `type Name params = body`, binding the name and the
// parameter variables.
func (t *tb) typeDecl(name string, params []string, body syntax.TypeBody) (*syntax.TypeDecl, *resolve.Symbol) {
	nid := t.id(name)
	var ps []*syntax.Ident
	for _, p := range params {
		pid := t.id(p)
		t.def(pid, resolve.KindTypeVar)
		ps = append(ps, pid)
	}
	d := &syntax.TypeDecl{Sp: t.sp(), Name: nid, Params: ps, Body: body}
	return d, t.def(nid, resolve.KindType)
}

// variant builds `| Name fields` binding the constructor and
// returning its symbol for use-site refs.
func (t *tb) variant(name string, fields ...syntax.Field) (syntax.Variant, *resolve.Symbol) {
	vid := t.id(name)
	return syntax.Variant{Sp: t.sp(), Name: vid, Params: fields}, t.def(vid, resolve.KindCtor)
}

// fieldPos is a positional payload slot `T`.
func (t *tb) fieldPos(ty syntax.TypeExpr) syntax.Field {
	return syntax.Field{Sp: t.sp(), Type: ty}
}

// fieldNamed is a named payload/record field `name: T`.
func (t *tb) fieldNamed(name string, ty syntax.TypeExpr) syntax.Field {
	id := t.id(name)
	t.def(id, resolve.KindField)
	return syntax.Field{Sp: t.sp(), Name: id, Type: ty}
}

// fieldLabeled is a labeled payload slot `(~name: T)` — the label is
// the field's name.
func (t *tb) fieldLabeled(label string, ty syntax.TypeExpr) syntax.Field {
	id := t.id(label)
	t.def(id, resolve.KindField)
	return syntax.Field{Sp: t.sp(), Label: id, Type: ty}
}

// sumBody builds `| v1 | v2 | ...`.
func (t *tb) sumBody(variants ...syntax.Variant) *syntax.SumBody {
	return &syntax.SumBody{Sp: t.sp(), Variants: variants}
}

// recBody builds `{ f1, f2, ... }`.
func (t *tb) recBody(fields ...syntax.Field) *syntax.RecordBody {
	return &syntax.RecordBody{Sp: t.sp(), Fields: fields}
}

// aliasBody builds `= T`.
func (t *tb) aliasBody(ty syntax.TypeExpr) *syntax.AliasBody {
	return &syntax.AliasBody{Sp: t.sp(), Type: ty}
}

// ---- more expression builders ----------------------------------------

func (t *tb) letStmt(pat syntax.Pat, val syntax.Expr) *syntax.Let {
	return &syntax.Let{Sp: t.sp(), Pat: pat, Value: val}
}

func (t *tb) goexpr(body syntax.Expr) *syntax.GoExpr {
	return &syntax.GoExpr{Sp: t.sp(), Body: body}
}

func (t *tb) forx(pat syntax.Pat, iter, body syntax.Expr) *syntax.For {
	return &syntax.For{Sp: t.sp(), Pat: pat, Iter: iter, Body: body}
}

func (t *tb) selectx(arms ...syntax.SelectArm) *syntax.SelectExpr {
	return &syntax.SelectExpr{Sp: t.sp(), Arms: arms}
}

func (t *tb) recvArm(bind syntax.Pat, ch, body syntax.Expr) *syntax.RecvArm {
	return &syntax.RecvArm{Sp: t.sp(), Bind: bind, Chan: ch, Body: body}
}

func (t *tb) sendArm(ch, v, body syntax.Expr) *syntax.SendArm {
	return &syntax.SendArm{Sp: t.sp(), Chan: ch, Value: v, Body: body}
}

func (t *tb) defArm(body syntax.Expr) *syntax.DefaultArm {
	return &syntax.DefaultArm{Sp: t.sp(), Body: body}
}

func (t *tb) ascribe(x syntax.Expr, ty syntax.TypeExpr) *syntax.Ascribe {
	return &syntax.Ascribe{Sp: t.sp(), X: x, Type: ty}
}

func (t *tb) testd(name string, body syntax.Expr) *syntax.TestDecl {
	return &syntax.TestDecl{Sp: t.sp(), Name: name, NameSp: t.sp(), Body: body}
}

func (t *tb) strInterp(parts ...syntax.StrPart) *syntax.StrInterp {
	return &syntax.StrInterp{Sp: t.sp(), Parts: parts}
}

func (t *tb) strText(s string) *syntax.StrText {
	return &syntax.StrText{Sp: t.sp(), Text: s}
}

func (t *tb) strExpr(x syntax.Expr) *syntax.StrExpr {
	return &syntax.StrExpr{Sp: t.sp(), X: x}
}

// ---- pattern builders ------------------------------------------------

func (t *tb) tupPat(pats ...syntax.Pat) *syntax.TuplePat {
	return &syntax.TuplePat{Sp: t.sp(), Elems: pats}
}

func (t *tb) recPat(rest bool, fields ...syntax.FieldPat) *syntax.RecordPat {
	return &syntax.RecordPat{Sp: t.sp(), Fields: fields, Rest: rest}
}

func (t *tb) fieldPat(name string, pat syntax.Pat) syntax.FieldPat {
	return syntax.FieldPat{Sp: t.sp(), Name: t.id(name), Pat: pat}
}

func (t *tb) fieldPatPunned(name string) syntax.FieldPat {
	return syntax.FieldPat{Sp: t.sp(), Name: t.id(name)}
}

func (t *tb) listPat(rest syntax.Pat, elems ...syntax.Pat) *syntax.ListPat {
	return &syntax.ListPat{Sp: t.sp(), Elems: elems, Rest: rest}
}

func (t *tb) orPat(alts ...syntax.Pat) *syntax.OrPat {
	return &syntax.OrPat{Sp: t.sp(), Alts: alts}
}

func (t *tb) asPat(pat syntax.Pat, name string) *syntax.AsPat {
	id := t.id(name)
	t.def(id, resolve.KindVar)
	return &syntax.AsPat{Sp: t.sp(), Pat: pat, Name: id}
}

func (t *tb) ctorArg(label string, pat syntax.Pat) syntax.CtorArg {
	var l *syntax.Ident
	if label != "" {
		l = t.id(label)
	}
	return syntax.CtorArg{Sp: t.sp(), Label: l, Pat: pat}
}

// ctorPatBySym builds `Ctor pats` with the ctor resolved to sym.
func (t *tb) ctorPatSym(sym *resolve.Symbol, args ...syntax.CtorArg) *syntax.CtorPat {
	return &syntax.CtorPat{Sp: t.sp(), Ctor: t.ref(sym), Args: args}
}

// ---- Signature helpers ------------------------------------------------

// sig is a Signature for a fixed-arity Go or imported function.
func sig(ret types.Type, params ...types.FuncParam) Signature {
	return Signature{Type: types.Fn(ret, params...)}
}

// vsig is a variadic Signature; the last parameter is the slice.
func vsig(ret types.Type, params ...types.FuncParam) Signature {
	return Signature{Type: types.Fn(ret, params...), Variadic: true}
}
