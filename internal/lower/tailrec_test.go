package lower

import (
	"fmt"
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Tests for the @tailrec requirement pass (tailrec.go;
// docs/decisions/2026-11-02-tailrec-requirement.md). Each test builds
// a marked check.FnDecl and asserts the diagnostics File reports.

// tailRecDecl is decl() plus the marker; the returned decl's Sym is
// the one body expressions must reference to be "self".
func tailRecDecl(name string, params []check.Param, body check.Expr, sig *types.Func) *check.FnDecl {
	d := decl(name, params, body, sig)
	d.TailRec = true
	return d
}

// selfApp applies callee sym with the given arg expressions as a
// saturated direct call — the shape `f x` produces in the typed AST.
func selfApp(sym *resolve.Symbol, sig *types.Func, ret types.Type, args ...check.Expr) *check.Apply {
	return app(vr(sym, sig), args, check.CallInfo{
		Kind: check.CallDirect, Callee: sym,
		Arity: len(args), Applied: len(args), HoleIndex: -1,
	}, ret)
}

// diagCodes collects the error codes of a diagnostic list.
func diagCodes(diags []diag.Diagnostic) []string {
	var codes []string
	for _, d := range diags {
		codes = append(codes, string(d.Code))
	}
	return codes
}

// wantCodes asserts the exact set of codes File reported.
func wantCodes(t *testing.T, diags []diag.Diagnostic, want ...string) {
	t.Helper()
	got := diagCodes(diags)
	if len(got) != len(want) {
		t.Fatalf("codes = %v, want %v (diags: %v)", got, want, diags)
	}
	for i, c := range want {
		if got[i] != c {
			t.Fatalf("codes = %v, want %v", got, want)
		}
	}
}

// TestTailRecAccepted: a marked function whose only self-call is a
// tail call lowers cleanly — and still gets the unconditional
// TailCall → for-loop conversion.
func TestTailRecAccepted(t *testing.T) {
	n := vsym("n", types.Int)
	c := vsym("c", types.Bool)
	sig := types.Fn(types.Int, types.P(types.Int), types.P(types.Bool))
	self := fsym("loop")
	d := tailRecDecl("loop",
		[]check.Param{param(vpat(n, types.Int)), param(vpat(c, types.Bool))}, nil, sig)
	d.Sym = self
	d.Body = &check.If{Base: b(types.Int), Cond: vr(c, types.Bool),
		Then: selfApp(self, sig, types.Int, vr(n, types.Int), vr(c, types.Bool)),
		Else: intLit("0")}
	out, diags := lower1(t, d)
	wantCodes(t, diags)
	if !fnDeclOf(t, out, 0).Fn.TailLoop {
		t.Fatal("TailLoop not set on an accepted @tailrec fn")
	}
}

// TestTailRecNonTailCall (R2): a self-call bound to a let stays a
// residual CallDirect → E0902 at the call.
func TestTailRecNonTailCall(t *testing.T) {
	n := vsym("n", types.Int)
	sig := types.Fn(types.Int, types.P(types.Int))
	d := tailRecDecl("f", []check.Param{param(vpat(n, types.Int))}, nil, sig)
	call := selfApp(d.Sym, sig, types.Int, vr(n, types.Int))
	// `f (f n)` — the inner call is an operand, so ANF binds it.
	d.Body = selfApp(d.Sym, sig, types.Int, call)
	_, diags := lower1(t, d)
	wantCodes(t, diags, "E0902")
	if diags[0].Data["fn"] != "f" {
		t.Errorf("diagnostic data[fn] = %v", diags[0].Data["fn"])
	}
}

// TestTailRecFuncRef (R3): binding the marked function as a value —
// `let g = f` — reports E0903 at the reference. The indirect call is
// invisible to CallDirect, so R1 reports E0905 alongside.
func TestTailRecFuncRef(t *testing.T) {
	n := vsym("n", types.Int)
	c := vsym("c", types.Bool)
	sig := types.Fn(types.Int, types.P(types.Int), types.P(types.Bool))
	d := tailRecDecl("f",
		[]check.Param{param(vpat(n, types.Int)), param(vpat(c, types.Bool))}, nil, sig)
	g := vsym("g", sig)
	d.Body = &check.Block{Base: b(types.Int), Stmts: []check.Expr{
		&check.Let{Base: b(sig), Pat: vpat(g, sig), Value: vr(d.Sym, sig)},
		&check.If{Base: b(types.Int), Cond: vr(c, types.Bool),
			// `g (n-1)` — a CallUnknown through the variable.
			Then: app(vr(g, sig), []check.Expr{vr(n, types.Int), vr(c, types.Bool)},
				check.CallInfo{Kind: check.CallUnknown}, types.Int),
			Else: intLit("0")},
	}}
	_, diags := lower1(t, d)
	wantCodes(t, diags, "E0903", "E0905")
	for _, dg := range diags {
		if dg.Code == "E0903" && dg.Data["fn"] != "f" {
			t.Errorf("E0903 data[fn] = %v", dg.Data["fn"])
		}
	}
}

// TestTailRecMutual (R4): f → g → f through direct calls reports
// E0904 naming the partner; g itself is unmarked and unaffected.
func TestTailRecMutual(t *testing.T) {
	ne, no := vsym("ne", types.Int), vsym("no", types.Int)
	sig := types.Fn(types.Int, types.P(types.Int))
	fd := tailRecDecl("even", []check.Param{param(vpat(ne, types.Int))}, nil, sig)
	gd := decl("odd", []check.Param{param(vpat(no, types.Int))}, nil, sig)
	fd.Body = selfApp(gd.Sym, sig, types.Int, vr(ne, types.Int))
	gd.Body = selfApp(fd.Sym, sig, types.Int, vr(no, types.Int))
	_, diags := lower1(t, fd, gd)
	wantCodes(t, diags, "E0905", "E0904")
	var mutual diag.Diagnostic
	for _, dg := range diags {
		if dg.Code == "E0904" {
			mutual = dg
		}
	}
	if mutual.Data["through"] != "odd" {
		t.Errorf("E0904 data[through] = %v", mutual.Data["through"])
	}
	path, _ := mutual.Data["path"].([]string)
	if fmt.Sprint(path) != "[even odd even]" {
		t.Errorf("E0904 data[path] = %v", path)
	}
}

// TestTailRecMutualThroughFuncRef (R4): a FuncRef edge counts the
// same as a direct call — `let h = g` inside f where g calls f is
// still an unprovable cycle.
func TestTailRecMutualThroughFuncRef(t *testing.T) {
	n := vsym("n", types.Int)
	sig := types.Fn(types.Int, types.P(types.Int))
	fd := tailRecDecl("f", []check.Param{param(vpat(n, types.Int))}, nil, sig)
	gd := decl("g", []check.Param{param(vpat(n, types.Int))}, nil, sig)
	h := vsym("h", sig)
	fd.Body = &check.Block{Base: b(types.Int), Stmts: []check.Expr{
		&check.Let{Base: b(sig), Pat: vpat(h, sig), Value: vr(gd.Sym, sig)},
		app(vr(h, sig), []check.Expr{vr(n, types.Int)},
			check.CallInfo{Kind: check.CallUnknown}, types.Int),
	}}
	gd.Body = selfApp(fd.Sym, sig, types.Int, vr(n, types.Int))
	_, diags := lower1(t, fd, gd)
	found := false
	for _, dg := range diags {
		if dg.Code == "E0904" && dg.Data["through"] == "g" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no E0904 through g; diags = %v", diags)
	}
}

// TestTailRecNoCall (R1): no self-reference at all → E0905 at the
// function name.
func TestTailRecNoCall(t *testing.T) {
	d := tailRecDecl("f", nil, intLit("42"), types.Fn(types.Int))
	_, diags := lower1(t, d)
	wantCodes(t, diags, "E0905")
}

// TestTailRecSpawnOnly: a self-call inside `go { ... }` runs on a
// fresh stack — exempt from R2, but it is not convertible, so a
// function whose only self-calls are spawned has no tail call and
// fails R1 (E0905): no loop is emitted for the attribute to certify.
func TestTailRecSpawnOnly(t *testing.T) {
	n := vsym("n", types.Int)
	sig := types.Fn(types.Unit, types.P(types.Int))
	d := tailRecDecl("f", []check.Param{param(vpat(n, types.Int))}, nil, sig)
	d.Body = &check.Block{Base: b(types.Unit), Stmts: []check.Expr{
		&check.GoExpr{Base: b(types.Unit),
			Body: &check.Block{Base: b(types.Unit), Stmts: []check.Expr{
				&check.Ignore{Base: b(types.Unit), X: selfApp(d.Sym, sig, types.Unit, vr(n, types.Int))},
			}}},
		unitLit(),
	}}
	_, diags := lower1(t, d)
	wantCodes(t, diags, "E0905")
}

// TestTailRecSpawnAndTail: a spawned self-call *alongside* a real
// tail call is fine — the spawned call is exempt from R2 and the
// converted TailCall satisfies R1.
func TestTailRecSpawnAndTail(t *testing.T) {
	n := vsym("n", types.Int)
	c := vsym("c", types.Bool)
	sig := types.Fn(types.Int, types.P(types.Int), types.P(types.Bool))
	d := tailRecDecl("f",
		[]check.Param{param(vpat(n, types.Int)), param(vpat(c, types.Bool))}, nil, sig)
	d.Body = &check.Block{Base: b(types.Int), Stmts: []check.Expr{
		&check.GoExpr{Base: b(types.Unit),
			Body: &check.Block{Base: b(types.Unit), Stmts: []check.Expr{
				&check.Ignore{Base: b(types.Unit), X: selfApp(d.Sym, sig, types.Int, vr(n, types.Int), vr(c, types.Bool))},
			}}},
		&check.If{Base: b(types.Int), Cond: vr(c, types.Bool),
			Then: selfApp(d.Sym, sig, types.Int, vr(n, types.Int), vr(c, types.Bool)),
			Else: intLit("0")},
	}}
	out, diags := lower1(t, d)
	wantCodes(t, diags)
	if !fnDeclOf(t, out, 0).Fn.TailLoop {
		t.Fatal("TailLoop not set — tail call should still convert")
	}
}

// TestTailRecNestedClosure: a self-call inside a nested lambda is
// that lambda's body position, not f's tail → E0902. The residual
// CallDirect is what R1 would call missing, but E0902 already names
// the violation, so no E0905 is piled on.
func TestTailRecNestedClosure(t *testing.T) {
	n := vsym("n", types.Int)
	k := vsym("k", types.Int)
	sig := types.Fn(types.Int, types.P(types.Int))
	d := tailRecDecl("f", []check.Param{param(vpat(n, types.Int))}, nil, sig)
	g := vsym("g", sig)
	lam := &check.Lambda{Base: b(sig),
		Params: []check.Param{param(vpat(k, types.Int))},
		Body:   selfApp(d.Sym, sig, types.Int, vr(k, types.Int))}
	d.Body = &check.Block{Base: b(types.Int), Stmts: []check.Expr{
		&check.Let{Base: b(sig), Pat: vpat(g, sig), Value: lam},
		app(vr(g, sig), []check.Expr{vr(n, types.Int)},
			check.CallInfo{Kind: check.CallUnknown}, types.Int),
	}}
	_, diags := lower1(t, d)
	wantCodes(t, diags, "E0902")
}

// TestTailRecUnmarked: the same shape is fine without the marker —
// the pass only looks at marked functions. The outer call is still
// converted unconditionally.
func TestTailRecUnmarked(t *testing.T) {
	n := vsym("n", types.Int)
	sig := types.Fn(types.Int, types.P(types.Int))
	d := decl("f", []check.Param{param(vpat(n, types.Int))}, nil, sig)
	d.Body = selfApp(d.Sym, sig, types.Int, selfApp(d.Sym, sig, types.Int, vr(n, types.Int)))
	out := mustLower(t, d)
	if !fnDeclOf(t, out, 0).Fn.TailLoop {
		t.Fatal("TailLoop not set — the unmarked outer call is still a tail call")
	}
}

// TestTailRecAdapterSpawn: `let g = f` on a *multi-arity* f lowers
// through the curried adapter — nested Closures ending in a CallDirect
// flagged Adapter rather than a FuncRef — but the value escape is the
// same: R3 reports E0903 even under `go`, because the adapted value
// can leave the goroutine (unlike a spawned self-call, which R2
// exempts). The adapter's call is not a call site, so R1 still
// reports E0905 alongside.
func TestTailRecAdapterSpawn(t *testing.T) {
	n := vsym("n", types.Int)
	c := vsym("c", types.Bool)
	sig := types.Fn(types.Unit, types.P(types.Int), types.P(types.Bool))
	d := tailRecDecl("f",
		[]check.Param{param(vpat(n, types.Int)), param(vpat(c, types.Bool))}, nil, sig)
	g := vsym("g", sig)
	d.Body = &check.Block{Base: b(types.Unit), Stmts: []check.Expr{
		&check.GoExpr{Base: b(types.Unit),
			Body: &check.Block{Base: b(types.Unit), Stmts: []check.Expr{
				&check.Let{Base: b(sig), Pat: vpat(g, sig), Value: vr(d.Sym, sig)},
				&check.Ignore{Base: b(types.Unit), X: selfApp(d.Sym, sig, types.Unit, vr(n, types.Int), vr(c, types.Bool))},
			}}},
		unitLit(),
	}}
	_, diags := lower1(t, d)
	wantCodes(t, diags, "E0903", "E0905")
}

// TestTailRecCycleEdge: a self FuncRef inside a spawned body still
// reports E0903 — unlike a spawned call, a function value is not
// bound to the fresh stack; it can leave the goroutine entirely and
// be applied back inside the function's own activation. And since
// the spawned call is the only self-call, R1 reports E0905 too.
func TestTailRecCycleEdge(t *testing.T) {
	n := vsym("n", types.Int)
	sig := types.Fn(types.Unit, types.P(types.Int))
	d := tailRecDecl("f", []check.Param{param(vpat(n, types.Int))}, nil, sig)
	d.Body = &check.Block{Base: b(types.Unit), Stmts: []check.Expr{
		&check.GoExpr{Base: b(types.Unit),
			Body: &check.Block{Base: b(types.Unit), Stmts: []check.Expr{
				&check.Ignore{Base: b(types.Unit), X: selfApp(d.Sym, sig, types.Unit, vr(n, types.Int))},
				// `ignore f` — f as a value inside the go body.
				&check.Ignore{Base: b(types.Unit), X: vr(d.Sym, sig)},
			}}},
		unitLit(),
	}}
	_, diags := lower1(t, d)
	wantCodes(t, diags, "E0903", "E0905")
}

// TestTailRecMatchArm: a self-call at a match arm's result position
// is a tail call — markTailCalls already descends the decision tree,
// so the requirement pass sees the TailCall it made.
func TestTailRecMatchArm(t *testing.T) {
	optT := types.Option(types.Int)
	n := vsym("n", optT)
	x := vsym("x", types.Int)
	sig := types.Fn(types.Int, types.P(optT))
	d := tailRecDecl("f", []check.Param{param(vpat(n, optT))}, nil, sig)
	d.Body = &check.Match{Base: b(types.Int), Subj: vr(n, optT), Arms: []check.MatchArm{
		mArm(&check.CtorPat{Base: b(optT), Ctor: csym("Some"),
			Args: []check.CtorArg{{Pat: vpat(x, types.Int)}}},
			selfApp(d.Sym, sig, types.Int, vr(x, optT))),
		mArm(wpat(optT), intLit("0")),
	}}
	out, diags := lower1(t, d)
	wantCodes(t, diags)
	if !fnDeclOf(t, out, 0).Fn.TailLoop {
		t.Fatal("TailLoop not set on a match-arm tail call")
	}
}

// TestTailRecPkgVarAlias (R4): `let g = f` at package level lowers
// to a VarDecl holding a FuncRef — `g (n-1)` in f is a CallFunc, so
// the FuncDecl-only graph missed the cycle. The Var edges close it:
// f → g (the call's callee), g → f (the initializer's FuncRef).
func TestTailRecPkgVarAlias(t *testing.T) {
	n := vsym("n", types.Int)
	sig := types.Fn(types.Int, types.P(types.Int))
	fd := tailRecDecl("f", []check.Param{param(vpat(n, types.Int))}, nil, sig)
	g := vsym("g", sig)
	ld := &check.LetDecl{Sp: sp, Pat: vpat(g, sig), Value: vr(fd.Sym, sig)}
	fd.Body = &check.If{Base: b(types.Int), Cond: vr(vsym("c", types.Bool), types.Bool),
		Then: app(vr(g, sig), []check.Expr{vr(n, types.Int)},
			check.CallInfo{Kind: check.CallUnknown}, types.Int),
		Else: intLit("0")}
	_, diags := lower1(t, ld, fd)
	found := false
	for _, dg := range diags {
		if dg.Code == "E0904" && dg.Data["through"] == "g" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no E0904 through g; diags = %v", diags)
	}
}

// TestTailRecDestructureVar (R4): `let (g, _) = (fn n -> f (n-1), 0)`
// keeps g a VarDecl — the FnDecl flip needs a VarPat. The cycle
// routes through two VarDecls: f → g (CallFunc callee), g → tmp
// (the FieldGet receiver), tmp → f (the stored closure's call).
func TestTailRecDestructureVar(t *testing.T) {
	n := vsym("n", types.Int)
	k := vsym("k", types.Int)
	sig := types.Fn(types.Int, types.P(types.Int))
	fd := tailRecDecl("f", []check.Param{param(vpat(n, types.Int))}, nil, sig)
	g := vsym("g", sig)
	tupT := types.Tup(sig, types.Int)
	lam := &check.Lambda{Base: b(sig),
		Params: []check.Param{param(vpat(k, types.Int))},
		Body:   selfApp(fd.Sym, sig, types.Int, vr(k, types.Int))}
	ld := &check.LetDecl{Sp: sp,
		Pat: &check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			vpat(g, sig), wpat(types.Int)}},
		Value: &check.TupleLit{Base: b(tupT), Elems: []check.Expr{lam, intLit("0")}}}
	fd.Body = &check.If{Base: b(types.Int), Cond: vr(vsym("c", types.Bool), types.Bool),
		Then: app(vr(g, sig), []check.Expr{vr(n, types.Int)},
			check.CallInfo{Kind: check.CallUnknown}, types.Int),
		Else: intLit("0")}
	_, diags := lower1(t, ld, fd)
	found := false
	for _, dg := range diags {
		if dg.Code == "E0904" && dg.Data["through"] == "g" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no E0904 through g; diags = %v", diags)
	}
}

// TestTailRecPkgVarChain (R4): `let h = g; let g = f` — an alias
// chain through two VarDecls is still one reference cycle.
func TestTailRecPkgVarChain(t *testing.T) {
	n := vsym("n", types.Int)
	sig := types.Fn(types.Int, types.P(types.Int))
	fd := tailRecDecl("f", []check.Param{param(vpat(n, types.Int))}, nil, sig)
	g := vsym("g", sig)
	h := vsym("h", sig)
	hd := &check.LetDecl{Sp: sp, Pat: vpat(h, sig), Value: vr(g, sig)}
	gd := &check.LetDecl{Sp: sp, Pat: vpat(g, sig), Value: vr(fd.Sym, sig)}
	fd.Body = &check.If{Base: b(types.Int), Cond: vr(vsym("c", types.Bool), types.Bool),
		Then: app(vr(h, sig), []check.Expr{vr(n, types.Int)},
			check.CallInfo{Kind: check.CallUnknown}, types.Int),
		Else: intLit("0")}
	_, diags := lower1(t, hd, gd, fd)
	found := false
	for _, dg := range diags {
		if dg.Code == "E0904" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no E0904; diags = %v", diags)
	}
}

// TestTailRecPkgVarSeed (R4, no false cycle): `let r = f 0` calls f
// once at package init — the stored Int cannot call f back, so f
// reading `r` closes no cycle. Init-time calls at a VarDecl body's
// top level do not produce edges.
func TestTailRecPkgVarSeed(t *testing.T) {
	n := vsym("n", types.Int)
	c := vsym("c", types.Bool)
	sig := types.Fn(types.Int, types.P(types.Int), types.P(types.Bool))
	fd := tailRecDecl("f",
		[]check.Param{param(vpat(n, types.Int)), param(vpat(c, types.Bool))}, nil, sig)
	r := vsym("r", types.Int)
	rd := &check.LetDecl{Sp: sp, Pat: vpat(r, types.Int),
		Value: selfApp(fd.Sym, sig, types.Int, intLit("0"),
			lit(syntax.LitBool, "true", types.Bool))}
	fd.Body = &check.If{Base: b(types.Int), Cond: vr(c, types.Bool),
		Then: selfApp(fd.Sym, sig, types.Int, vr(n, types.Int), vr(c, types.Bool)),
		Else: vr(r, types.Int)}
	_, diags := lower1(t, rd, fd)
	wantCodes(t, diags)
}
