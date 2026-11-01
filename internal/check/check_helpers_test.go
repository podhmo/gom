package check

// Test-time builders for hand-written resolve.Resolved inputs. The
// resolver (P1-4) is not part of this task, so tests construct the
// surface AST plus its Defs/Uses side tables by hand — a tb instance
// mints identifiers, symbols and spans so tables stay consistent.

import (
	"fmt"
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// tb builds one resolved file. Spans are synthesized: every node a
// diagnostic may point at gets a distinct offset so tests can assert
// on positions.
type tb struct {
	defs map[*syntax.Ident]*resolve.Symbol
	uses map[*syntax.Ident]*resolve.Symbol
	pos  int
}

func newTB() *tb {
	return &tb{
		defs: map[*syntax.Ident]*resolve.Symbol{},
		uses: map[*syntax.Ident]*resolve.Symbol{},
	}
}

// sp returns a fresh one-byte span.
func (t *tb) sp() diag.Span {
	t.pos++
	return diag.SpanIn("test.gom", t.pos, t.pos+1)
}

func (t *tb) id(name string) *syntax.Ident {
	return &syntax.Ident{Sp: t.sp(), Name: name}
}

// def binds id to a fresh symbol of kind and returns it.
func (t *tb) def(id *syntax.Ident, kind resolve.Kind) *resolve.Symbol {
	s := &resolve.Symbol{Name: id.Name, Kind: kind, Def: id}
	t.defs[id] = s
	return s
}

// ref mints an identifier bound (as a use) to sym.
func (t *tb) ref(sym *resolve.Symbol) *syntax.Ident {
	id := t.id(sym.Name)
	t.uses[id] = sym
	return id
}

// resolved wraps decls into a File + Resolved.
func (t *tb) resolved(decls ...syntax.Decl) *resolve.Resolved {
	return &resolve.Resolved{
		File: &syntax.File{
			Sp:    t.sp(),
			Decls: decls,
		},
		Defs: t.defs,
		Uses: t.uses,
	}
}

// check runs Check and returns the typed file plus diagnostics.
func (t *tb) check(src SignatureSource, decls ...syntax.Decl) (*File, diag.List) {
	return Check(t.resolved(decls...), src)
}

// ---- expression builders -------------------------------------------

func (t *tb) ilit(v string) *syntax.Lit {
	return &syntax.Lit{Sp: t.sp(), Kind: syntax.LitInt, Text: v}
}
func (t *tb) flit(v string) *syntax.Lit {
	return &syntax.Lit{Sp: t.sp(), Kind: syntax.LitFloat, Text: v}
}
func (t *tb) slit(v string) *syntax.Lit {
	return &syntax.Lit{Sp: t.sp(), Kind: syntax.LitStr, Text: v}
}
func (t *tb) ulit() *syntax.Lit {
	return &syntax.Lit{Sp: t.sp(), Kind: syntax.LitUnit, Text: "()"}
}
func (t *tb) blit(v bool) *syntax.Lit {
	txt := "false"
	if v {
		txt = "true"
	}
	return &syntax.Lit{Sp: t.sp(), Kind: syntax.LitBool, Text: txt}
}

// arg builds a positional argument.
func (t *tb) arg(x syntax.Expr) syntax.Arg {
	return syntax.Arg{Sp: t.sp(), Value: x}
}

// larg builds a labeled `~name: x` argument.
func (t *tb) larg(name string, x syntax.Expr) syntax.Arg {
	return syntax.Arg{Sp: t.sp(), Label: t.id(name), Value: x}
}

// parg builds a punned `~name` argument whose value resolves to sym.
func (t *tb) parg(name string, sym *resolve.Symbol) syntax.Arg {
	id := t.id(name)
	t.uses[id] = sym
	return syntax.Arg{Sp: t.sp(), Label: t.id(name), Value: id, Punned: true}
}

func (t *tb) app(fn syntax.Expr, args ...syntax.Arg) *syntax.Apply {
	return &syntax.Apply{Sp: t.sp(), Fn: fn, Args: args}
}

func (t *tb) bin(op syntax.BinOp, x, y syntax.Expr) *syntax.Binary {
	return &syntax.Binary{Sp: t.sp(), Op: op, X: x, Y: y}
}

func (t *tb) unary(op syntax.UnOp, x syntax.Expr) *syntax.Unary {
	return &syntax.Unary{Sp: t.sp(), Op: op, X: x}
}

func (t *tb) sel(recv syntax.Expr, selSym *resolve.Symbol, selName string) *syntax.Select {
	sel := t.id(selName)
	if selSym != nil {
		t.uses[sel] = selSym
	}
	return &syntax.Select{Sp: t.sp(), Recv: recv, Sel: sel}
}

func (t *tb) block(stmts ...syntax.Expr) *syntax.Block {
	return &syntax.Block{Sp: t.sp(), Stmts: stmts}
}

func (t *tb) hole() *syntax.Hole { return &syntax.Hole{Sp: t.sp()} }

func (t *tb) list(elems ...syntax.Expr) *syntax.ListLit {
	return &syntax.ListLit{Sp: t.sp(), Elems: elems}
}

func (t *tb) spread(x syntax.Expr) *syntax.Spread {
	return &syntax.Spread{Sp: t.sp(), X: x}
}

func (t *tb) tuple(elems ...syntax.Expr) *syntax.TupleLit {
	return &syntax.TupleLit{Sp: t.sp(), Elems: elems}
}

func (t *tb) try(x syntax.Expr) *syntax.Try {
	return &syntax.Try{Sp: t.sp(), X: x}
}

func (t *tb) letx(pat syntax.Pat, ty syntax.TypeExpr, val syntax.Expr) *syntax.Let {
	return &syntax.Let{Sp: t.sp(), Pat: pat, Type: ty, Value: val}
}

func (t *tb) lambda(params []syntax.Param, body syntax.Expr) *syntax.Lambda {
	return &syntax.Lambda{Sp: t.sp(), Params: params, Body: body}
}

func (t *tb) ifx(cond, then, els syntax.Expr) *syntax.If {
	return &syntax.If{Sp: t.sp(), Cond: cond, Then: then, Else: els}
}

func (t *tb) matchx(subj syntax.Expr, arms ...syntax.MatchArm) *syntax.Match {
	return &syntax.Match{Sp: t.sp(), Subj: subj, Arms: arms}
}

func (t *tb) arm(pat syntax.Pat, guard, body syntax.Expr) syntax.MatchArm {
	return syntax.MatchArm{Sp: t.sp(), Pat: pat, Guard: guard, Body: body}
}

func (t *tb) recLit(fields []syntax.FieldInit, base syntax.Expr) *syntax.RecordLit {
	return &syntax.RecordLit{Sp: t.sp(), Base: base, Fields: fields}
}

func (t *tb) fieldInit(name string, val syntax.Expr) syntax.FieldInit {
	return syntax.FieldInit{Sp: t.sp(), Name: t.id(name), Value: val}
}

// ---- type-expression builders --------------------------------------

func (t *tb) tn(name string) *syntax.TypeName {
	return &syntax.TypeName{Sp: t.sp(), Name: t.id(name)}
}

// tnFor binds the type name to a symbol (qualified or declared).
func (t *tb) tnBound(name string, sym *resolve.Symbol) *syntax.TypeName {
	id := t.id(name)
	if sym != nil {
		t.uses[id] = sym
	}
	return &syntax.TypeName{Sp: t.sp(), Name: id}
}

func (t *tb) tapp(ctor syntax.TypeExpr, args ...syntax.TypeExpr) *syntax.TypeApp {
	return &syntax.TypeApp{Sp: t.sp(), Ctor: ctor, Args: args}
}

func (t *tb) tfn(ret syntax.TypeExpr, params ...syntax.TypeParam) *syntax.TypeFn {
	return &syntax.TypeFn{Sp: t.sp(), Params: params, Ret: ret}
}

func (t *tb) tp(ty syntax.TypeExpr) syntax.TypeParam {
	return syntax.TypeParam{Sp: t.sp(), Type: ty}
}

func (t *tb) tpL(label string, ty syntax.TypeExpr) syntax.TypeParam {
	return syntax.TypeParam{Sp: t.sp(), Label: t.id(label), Type: ty}
}

func (t *tb) tvar(name string) *syntax.TypeVar {
	return &syntax.TypeVar{Sp: t.sp(), Name: t.id(name)}
}

func (t *tb) ttup(elems ...syntax.TypeExpr) *syntax.TypeTuple {
	return &syntax.TypeTuple{Sp: t.sp(), Elems: elems}
}

// ---- pattern builders ----------------------------------------------

// vp mints a VarPat binding a fresh variable symbol.
func (t *tb) vp(name string) (*syntax.VarPat, *resolve.Symbol) {
	id := t.id(name)
	return &syntax.VarPat{Sp: id.Sp, Name: id}, t.def(id, resolve.KindVar)
}

func (t *tb) wp() *syntax.WildPat {
	return &syntax.WildPat{Sp: t.sp()}
}

func (t *tb) litp(l *syntax.Lit) *syntax.LitPat {
	return &syntax.LitPat{Sp: l.Sp, Lit: l}
}

// ---- declaration builders ------------------------------------------

// param builds one `(x: T)` or `(~l: T)` parameter binding a fresh
// variable symbol.
func (t *tb) param(name, label string, ty syntax.TypeExpr) (syntax.Param, *resolve.Symbol) {
	var l *syntax.Ident
	if label != "" {
		l = t.id(label)
	}
	pat, vsym := t.vp(name)
	return syntax.Param{Sp: pat.Sp, Label: l, Pat: pat, Type: ty}, vsym
}

// fn builds `fn name params -> ret = body`, binding the name.
func (t *tb) fn(name string, params []syntax.Param, ret syntax.TypeExpr, body syntax.Expr) (*syntax.FnDecl, *resolve.Symbol) {
	nid := t.id(name)
	d := &syntax.FnDecl{Sp: t.sp(), Name: nid, Params: params, Ret: ret, Body: body}
	return d, t.def(nid, resolve.KindFunc)
}

// letd builds a top-level `let pat : T = e`.
func (t *tb) letd(pat syntax.Pat, ty syntax.TypeExpr, val syntax.Expr) *syntax.LetDecl {
	return &syntax.LetDecl{Sp: t.sp(), Pat: pat, Type: ty, Value: val}
}

// ---- assertions ------------------------------------------------------

// diagCodes collects the codes of all diagnostics.
func diagCodes(l diag.List) []string {
	var out []string
	for _, d := range l {
		out = append(out, string(d.Code))
	}
	return out
}

// requireCodes fails the test unless the diagnostics' codes are
// exactly the expected set (in emission order).
func requireCodes(t *testing.T, l diag.List, want ...string) {
	t.Helper()
	got := diagCodes(l)
	if len(got) != len(want) {
		t.Fatalf("diag codes = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("diag codes = %v, want %v", got, want)
		}
	}
}

// findDecl locates the i-th top-level decl of the given kind.
func findDecl[T Decl](f *File, i int) T {
	var zero T
	n := 0
	for _, d := range f.Decls {
		if v, ok := d.(T); ok {
			if n == i {
				return v
			}
			n++
		}
	}
	return zero
}

// applyAt digs an *Apply out of an expression — used to assert
// CallInfo on a specific application nested in blocks/lets.
func applyAt(e Expr) *Apply {
	for e != nil {
		switch n := e.(type) {
		case *Apply:
			return n
		case *Block:
			e = n.Stmts[len(n.Stmts)-1]
		case *Let:
			e = n.Value
		case *Binary:
			e = n.Y
		case *Ascribe, *For, *GoExpr, *Hole, *If, *Ignore, *Lambda,
			*ListLit, *Lit, *Match, *MemberExpr, *RecordLit,
			*SelectExpr, *Spread, *StrInterp, *Try, *TupleLit,
			*Unary, *Var:
			return nil
		default:
			panic("unreachable")
		}
	}
	return nil
}

// firstApply returns the first Apply found walking a declaration's
// body breadth-first.
func firstApply(d *FnDecl) *Apply {
	var found *Apply
	var walk func(e Expr)
	walk = func(e Expr) {
		if found != nil || e == nil {
			return
		}
		if a, ok := e.(*Apply); ok {
			found = a
			return
		}
		switch e := e.(type) {
		case *Block:
			for _, s := range e.Stmts {
				walk(s)
			}
		case *Let:
			walk(e.Value)
		case *Binary:
			walk(e.X)
			walk(e.Y)
		case *If:
			walk(e.Then)
			if e.Else != nil {
				walk(e.Else)
			}
		case *Match:
			walk(e.Subj)
			for _, a := range e.Arms {
				walk(a.Body)
			}
		case *Lambda:
			walk(e.Body)
		case *Try:
			walk(e.X)
		case *Ascribe:
			walk(e.X)
		case *Apply, *For, *GoExpr, *Hole, *Ignore, *ListLit, *Lit,
			*MemberExpr, *RecordLit, *SelectExpr, *Spread,
			*StrInterp, *TupleLit, *Unary, *Var:
			// Already handled, or no nested Apply to find.
		default:
			panic("unreachable")
		}
	}
	walk(d.Body)
	return found
}

// expectCall asserts the CallInfo fields of an apply node.
func expectCall(t *testing.T, a *Apply, kind CallKind, arity, applied, unapplied int, argToParam []int) {
	t.Helper()
	if a == nil {
		t.Fatal("no Apply node found")
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("invalid Apply: %v", err)
	}
	c := a.Call
	if c.Kind != kind {
		t.Errorf("call kind = %s, want %s", c.Kind, kind)
	}
	if c.Arity != arity {
		t.Errorf("arity = %d, want %d", c.Arity, arity)
	}
	if c.Applied != applied {
		t.Errorf("applied = %d, want %d", c.Applied, applied)
	}
	if c.Unapplied != unapplied {
		t.Errorf("unapplied = %d, want %d", c.Unapplied, unapplied)
	}
	if len(argToParam) != len(c.ArgToParam) {
		t.Errorf("argToParam = %v, want %v", c.ArgToParam, argToParam)
	} else {
		for i := range argToParam {
			if c.ArgToParam[i] != argToParam[i] {
				t.Errorf("argToParam = %v, want %v", c.ArgToParam, argToParam)
				break
			}
		}
	}
}

// typeString renders a type for comparisons.
func typeString(ty types.Type) string {
	if ty == nil {
		return "<nil>"
	}
	return ty.String()
}

// errf is a tiny fmt helper for test messages.
var errf = fmt.Sprintf
