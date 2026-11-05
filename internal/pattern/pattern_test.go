package pattern

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

var sp = diag.SpanIn("test.gom", 0, 1)

func base(t types.Type) check.Base { return check.Base{Sp: sp, Typ: t} }

func vsym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindCtor}
}

// --- type declaration builders ---

// typeDecl builds a `type Name params = | V1 f... | V2 f...`
// declaration for the env.
func typeDecl(name string, params []*types.Var, variants ...*check.Variant) *check.TypeDecl {
	vs := make([]check.Variant, len(variants))
	for i, v := range variants {
		vs[i] = *v
	}
	return &check.TypeDecl{
		Sp:         sp,
		Sym:        &resolve.Symbol{Name: name, Kind: resolve.KindType},
		TypeParams: params,
		Body:       &check.SumType{Sp: sp, Variants: vs},
	}
}

func variant(name string, fields ...types.Type) *check.Variant {
	v := &check.Variant{Sp: sp, Sym: vsym(name)}
	for i, t := range fields {
		v.Fields = append(v.Fields, check.TField{Sp: sp, Index: i, Type: t})
	}
	return v
}

func namedVariant(name string, fields ...check.TField) *check.Variant {
	return &check.Variant{Sp: sp, Sym: vsym(name), Fields: fields}
}

func tfield(name string, i int, t types.Type) check.TField {
	return check.TField{
		Sp:    sp,
		Name:  name,
		Index: i,
		Sym:   &resolve.Symbol{Name: name, Kind: resolve.KindField},
		Type:  t,
	}
}

// exprDecl returns the spec's own `type Expr` (testdata/spec's eval
// example): Num Int | Add Expr Expr | Neg Expr | Let String Expr Expr.
func exprDecl() *check.TypeDecl {
	expr := &types.Con{Name: "Expr"}
	return typeDecl("Expr", nil,
		variant("Num", types.Int),
		variant("Add", expr, expr),
		variant("Neg", expr),
		namedVariant("Let",
			tfield("name", 0, types.String),
			tfield("value", 1, expr),
			tfield("body", 2, expr)),
	)
}

// --- pattern builders ---

func wildp(t types.Type) check.Pat { return &check.WildPat{Base: base(t)} }
func varp(t types.Type) check.Pat {
	return &check.VarPat{Base: base(t), Sym: &resolve.Symbol{Name: "x", Kind: resolve.KindVar}}
}
func litp(k syntax.LitKind, text string, t types.Type) check.Pat {
	return &check.LitPat{Base: base(t), Kind: k, Text: text}
}
func ctor(t types.Type, name string, args ...check.Pat) check.Pat {
	ca := make([]check.CtorArg, len(args))
	for i, a := range args {
		ca[i] = check.CtorArg{Pat: a}
	}
	return &check.CtorPat{Base: base(t), Ctor: vsym(name), Args: ca}
}
func orp(t types.Type, alts ...check.Pat) check.Pat {
	return &check.OrPat{Base: base(t), Alts: alts}
}
func asp(p check.Pat) check.Pat {
	return &check.AsPat{Base: base(p.Type()), Pat: p, Sym: &resolve.Symbol{Name: "w", Kind: resolve.KindVar}}
}
func listp(t types.Type, rest check.Pat, elems ...check.Pat) check.Pat {
	return &check.ListPat{Base: base(t), Elems: elems, Rest: rest}
}
func tupp(t types.Type, elems ...check.Pat) check.Pat {
	return &check.TuplePat{Base: base(t), Elems: elems}
}
func recp(t types.Type, names []string, pats []check.Pat) check.Pat {
	fs := make([]check.FieldPat, len(names))
	for i, n := range names {
		fs[i] = check.FieldPat{
			Sp:    sp,
			Name:  n,
			Sym:   &resolve.Symbol{Name: n, Kind: resolve.KindField},
			Index: i,
			Pat:   pats[i],
		}
	}
	return &check.RecordPat{Base: base(t), Fields: fs, Rest: len(names) > 0}
}

func arm(p check.Pat, guarded bool) check.MatchArm {
	var g check.Expr
	if guarded {
		g = &check.Lit{Base: base(types.Bool), Kind: syntax.LitBool, Text: "true"}
	}
	return check.MatchArm{Sp: p.Span(), Pat: p, Guard: g, Body: &check.Lit{Base: base(types.Int), Kind: syntax.LitInt, Text: "0"}}
}

func matchExpr(subjTy types.Type, arms ...check.MatchArm) *check.Match {
	subj := &check.Var{Base: base(subjTy), Sym: &resolve.Symbol{Name: "e", Kind: resolve.KindVar}}
	return &check.Match{Base: base(types.Int), Subj: subj, Arms: arms}
}

// --- test helpers ---

// missing runs CheckMatch and returns the E0301 missing_patterns,
// or nil when no E0301 fired.
func missing(t *testing.T, m *check.Match, env *Env) []string {
	t.Helper()
	for _, d := range CheckMatch(m, env, Options{}) {
		if d.Code == "E0301" {
			v, _ := d.Data["missing_patterns"].([]string)
			return v
		}
	}
	return nil
}

// codes returns the diagnostic codes produced, in emission order.
func codes(diags []diag.Diagnostic) []string {
	out := make([]string, len(diags))
	for i, d := range diags {
		out[i] = string(d.Code)
	}
	return out
}

func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestExhaustiveBool(t *testing.T) {
	env := NewEnv(nil)
	m := matchExpr(types.Bool,
		arm(litp(syntax.LitBool, "true", types.Bool), false),
		arm(litp(syntax.LitBool, "false", types.Bool), false))
	if diags := CheckMatch(m, env, Options{}); len(diags) != 0 {
		t.Fatalf("exhaustive Bool match produced %v", codes(diags))
	}
}

func TestBoolMissingFalse(t *testing.T) {
	env := NewEnv(nil)
	m := matchExpr(types.Bool,
		arm(litp(syntax.LitBool, "true", types.Bool), false))
	got := missing(t, m, env)
	if !eqStrs(got, []string{"false"}) {
		t.Fatalf("missing = %v, want [false]", got)
	}
}

func TestOptionSomeMissingNone(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	m := matchExpr(o, arm(ctor(o, "Some", varp(types.Int)), false))
	if got := missing(t, m, env); !eqStrs(got, []string{"None"}) {
		t.Fatalf("missing = %v, want [None]", got)
	}
}

func TestOptionExhaustive(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	m := matchExpr(o,
		arm(ctor(o, "Some", varp(types.Int)), false),
		arm(ctor(o, "None"), false))
	if diags := CheckMatch(m, env, Options{}); len(diags) != 0 {
		t.Fatalf("exhaustive Option match produced %v", codes(diags))
	}
}

func TestSpecExampleOrder(t *testing.T) {
	// The exact scenario of docs/spec.md section 4 and
	// testdata/spec/E0301_nonexhaustive_match: arms covering Num,
	// Add, Neg-of-Neg, plus a guarded Neg arm. Missing patterns must
	// enumerate depth-first in declaration order.
	expr := &types.Con{Name: "Expr"}
	env := NewEnv([]check.Decl{exprDecl()})
	m := matchExpr(expr,
		arm(ctor(expr, "Num", varp(types.Int)), false),
		arm(ctor(expr, "Add",
			ctor(expr, "Num", litp(syntax.LitInt, "0", types.Int)),
			varp(expr)), false),
		arm(ctor(expr, "Add", varp(expr), varp(expr)), false),
		arm(ctor(expr, "Neg", ctor(expr, "Neg", varp(expr))), false),
		arm(ctor(expr, "Neg", varp(expr)), true))
	want := []string{"Neg (Num _)", "Neg (Add _ _)", "Neg (Let _ _ _)", "Let _ _ _"}
	if got := missing(t, m, env); !eqStrs(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
	// The guard note must be present.
	for _, d := range CheckMatch(m, env, Options{}) {
		if d.Code == "E0301" {
			found := false
			for _, n := range d.Notes {
				if n == "arms with guards do not count toward exhaustiveness" {
					found = true
				}
			}
			if !found {
				t.Fatalf("E0301 lacks the guard note: %v", d.Notes)
			}
		}
	}
}

func TestOrPatCovers(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	m := matchExpr(o,
		arm(orp(o,
			ctor(o, "Some", wildp(types.Int)),
			ctor(o, "None")), false))
	if diags := CheckMatch(m, env, Options{}); len(diags) != 0 {
		t.Fatalf("or-pattern covering all ctors produced %v", codes(diags))
	}
}

func TestOrPatPartialCover(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	m := matchExpr(o,
		arm(orp(o,
			ctor(o, "Some", varp(types.Int)),
			litp(syntax.LitInt, "0", types.Int)), // nonsense alt, still useful
			false))
	// `0` is not a member of Option's signature; the row still
	// covers only `Some`, so `None` is missing.
	if got := missing(t, m, env); !eqStrs(got, []string{"None"}) {
		t.Fatalf("missing = %v, want [None]", got)
	}
}

func TestListPatterns(t *testing.T) {
	env := NewEnv(nil)
	l := types.List(types.Int)

	// [] + [x, ..rest] is exhaustive: any list is empty or non-empty.
	m := matchExpr(l,
		arm(listp(l, nil), false),
		arm(listp(l, varp(l), varp(types.Int)), false))
	if diags := CheckMatch(m, env, Options{}); len(diags) != 0 {
		t.Fatalf("[] + [x, ..rest] produced %v", codes(diags))
	}

	// [x] alone: empty, length>=2 and the tail-1 cases are missing.
	m = matchExpr(l, arm(listp(l, nil, varp(types.Int)), false))
	want := []string{"[]", "[_, _, .._]"}
	if got := missing(t, m, env); !eqStrs(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}

	// [x, y] alone misses [], [_], [_ , _ , _ , .._] (length >= 3).
	m = matchExpr(l, arm(listp(l, nil, varp(types.Int), varp(types.Int)), false))
	want = []string{"[]", "[_]", "[_, _, _, .._]"}
	if got := missing(t, m, env); !eqStrs(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}

	// [] + [_] + [_, _, ..rest] is exhaustive.
	m = matchExpr(l,
		arm(listp(l, nil), false),
		arm(listp(l, nil, varp(types.Int)), false),
		arm(listp(l, varp(l), varp(types.Int), varp(types.Int)), false))
	if diags := CheckMatch(m, env, Options{}); len(diags) != 0 {
		t.Fatalf("three-arm list produced %v", codes(diags))
	}
}

// checkWithin runs CheckMatch on a separate goroutine and fails the
// test when it does not return within d. It guards the Σ₁
// complete/incomplete split of matrix.go: specializing every
// signature constructor at a wildcard column (the pre-split code)
// recursed on a recursive type's tail into the identical matrix
// state forever — tys cycling 2→3→2 — so the wrong split shows up
// as a hang (or stack overflow) rather than a wrong answer.
func checkWithin(t *testing.T, m *check.Match, env *Env, d time.Duration) []diag.Diagnostic {
	t.Helper()
	done := make(chan []diag.Diagnostic, 1)
	go func() { done <- CheckMatch(m, env, Options{}) }()
	select {
	case ds := <-done:
		return ds
	case <-time.After(d):
		t.Fatalf("CheckMatch did not terminate within %s", d)
		return nil
	}
}

// missingWithin is missing over checkWithin.
func missingWithin(t *testing.T, m *check.Match, env *Env, d time.Duration) []string {
	t.Helper()
	for _, d0 := range checkWithin(t, m, env, d) {
		if d0.Code == "E0301" {
			v, _ := d0.Data["missing_patterns"].([]string)
			return v
		}
	}
	return nil
}

// TestRecursiveColumnTerminates is the multi-column recursive-type
// regression: with scrutinee (List Int, X) and a wildcard first
// column, Σ₁ is empty — incomplete — so the check must take the
// default matrix. Specializing all of List's constructors instead
// put the List column back and looped forever.
func TestRecursiveColumnTerminates(t *testing.T) {
	env := NewEnv(nil)
	l := types.List(types.Int)

	// match (l, u) { (_, ()) -> ... } on (List Int, Unit):
	// exhaustive, since Unit's only value is (). A second,
	// unreachable Cons arm exercises `useful` on the same split.
	t.Run("list/unit", func(t *testing.T) {
		tup := types.Tup(l, types.Unit)
		m := matchExpr(tup,
			arm(tupp(tup, wildp(l), litp(syntax.LitUnit, "()", types.Unit)), false))
		if diags := checkWithin(t, m, env, 5*time.Second); len(diags) != 0 {
			t.Fatalf("(_, ()) on (List Int, Unit) produced %v", codes(diags))
		}
		m = matchExpr(tup,
			arm(tupp(tup, wildp(l), litp(syntax.LitUnit, "()", types.Unit)), false),
			arm(tupp(tup,
				listp(l, varp(l), varp(types.Int)),
				litp(syntax.LitUnit, "()", types.Unit)), false))
		if got := codes(checkWithin(t, m, env, 5*time.Second)); !eqStrs(got, []string{"E0302"}) {
			t.Fatalf("codes = %v, want [E0302]", got)
		}
	})

	// match (l, b) { (_, true) -> ... } on (List Int, Bool):
	// the false column is uncovered under each list head.
	t.Run("list/bool", func(t *testing.T) {
		tp := types.Tup(l, types.Bool)
		m := matchExpr(tp,
			arm(tupp(tp, wildp(l), litp(syntax.LitBool, "true", types.Bool)), false))
		got := missingWithin(t, m, env, 5*time.Second)
		want := []string{"([], false)", "([_, .._], false)"}
		if !eqStrs(got, want) {
			t.Fatalf("missing = %v, want %v", got, want)
		}
	})

	// match (l, r) { (_, { name = "x" }) -> ... } on
	// (List Int, User): the record's single constructor keeps the
	// second column finite; only name = "x" records are covered.
	t.Run("list/record", func(t *testing.T) {
		user := &types.Con{Name: "User"}
		env := NewEnv([]check.Decl{&check.TypeDecl{
			Sp:  sp,
			Sym: &resolve.Symbol{Name: "User", Kind: resolve.KindType},
			Body: &check.RecordType{Sp: sp, Fields: []check.TField{
				tfield("name", 0, types.String),
				tfield("age", 1, types.Int),
			}},
		}})
		tp := types.Tup(l, user)
		m := matchExpr(tp,
			arm(tupp(tp, wildp(l),
				recp(user, []string{"name"},
					[]check.Pat{litp(syntax.LitStr, `"x"`, types.String)})), false))
		got := missingWithin(t, m, env, 5*time.Second)
		want := []string{
			`([], { name = _, age = _ })`,
			`([_, .._], { name = _, age = _ })`,
		}
		if !eqStrs(got, want) {
			t.Fatalf("missing = %v, want %v", got, want)
		}
	})
}

func TestGuardedArmCannotProveExhaustion(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	// `Some x if g` does not count: `Some _` is still missing.
	m := matchExpr(o,
		arm(ctor(o, "Some", varp(types.Int)), true),
		arm(ctor(o, "None"), false))
	if got := missing(t, m, env); !eqStrs(got, []string{"Some _"}) {
		t.Fatalf("missing = %v, want [Some _]", got)
	}
	// And the arm after the guard is NOT redundant.
	for _, d := range CheckMatch(m, env, Options{}) {
		if d.Code == "E0302" {
			t.Fatalf("guarded arm shadowed a following arm: %v", d)
		}
	}
}

func TestGuardedArmExemptFromE0302(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	// The guarded `Some x if g` would be unreachable — but guards
	// may fail, so it stays exempt even though `_` precedes it.
	m := matchExpr(o,
		arm(wildp(o), false),
		arm(ctor(o, "Some", varp(types.Int)), true))
	for _, d := range CheckMatch(m, env, Options{}) {
		if d.Code == "E0302" {
			t.Fatalf("guarded arm reported unreachable: %v", d)
		}
	}
	// `_` makes it exhaustive, so no E0301 either.
	if diags := CheckMatch(m, env, Options{}); len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", codes(diags))
	}
}

func TestUnreachableAfterWildcard(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	m := matchExpr(o,
		arm(wildp(o), false),
		arm(ctor(o, "Some", varp(types.Int)), false))
	diags := CheckMatch(m, env, Options{})
	if got := codes(diags); !eqStrs(got, []string{"E0302"}) {
		t.Fatalf("codes = %v, want [E0302]", got)
	}
}

func TestUnreachableOrPatArm(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	// Every alternative is covered by the earlier wildcard: the
	// whole or-pattern arm is unreachable.
	m := matchExpr(o,
		arm(wildp(o), false),
		arm(orp(o, ctor(o, "Some", wildp(types.Int)), ctor(o, "None")), false))
	diags := CheckMatch(m, env, Options{})
	if got := codes(diags); !eqStrs(got, []string{"E0302"}) {
		t.Fatalf("codes = %v, want [E0302]", got)
	}
	// Partially covered alternatives keep the arm reachable.
	m = matchExpr(o,
		arm(ctor(o, "None"), false),
		arm(orp(o, ctor(o, "Some", wildp(types.Int)), ctor(o, "None")), false))
	for _, d := range CheckMatch(m, env, Options{}) {
		if d.Code == "E0302" {
			t.Fatalf("partially covered or-pattern flagged unreachable: %v", d)
		}
	}
}

func TestUnreachableDuplicateArm(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	m := matchExpr(o,
		arm(ctor(o, "Some", varp(types.Int)), false),
		arm(ctor(o, "None"), false),
		arm(ctor(o, "Some", litp(syntax.LitInt, "0", types.Int)), false))
	diags := CheckMatch(m, env, Options{})
	if got := codes(diags); !eqStrs(got, []string{"E0302"}) {
		t.Fatalf("codes = %v, want [E0302]", got)
	}
}

func TestIntNeedsWildcard(t *testing.T) {
	env := NewEnv(nil)
	m := matchExpr(types.Int,
		arm(litp(syntax.LitInt, "0", types.Int), false),
		arm(litp(syntax.LitInt, "1", types.Int), false))
	if got := missing(t, m, env); !eqStrs(got, []string{"_"}) {
		t.Fatalf("missing = %v, want [_]", got)
	}
	// With a wildcard arm the same literal arms stay reachable.
	m = matchExpr(types.Int,
		arm(litp(syntax.LitInt, "0", types.Int), false),
		arm(wildp(types.Int), false))
	if diags := CheckMatch(m, env, Options{}); len(diags) != 0 {
		t.Fatalf("int literal + wildcard produced %v", codes(diags))
	}
}

func TestLiteralNormalizedAliases(t *testing.T) {
	env := NewEnv(nil)
	// 0x1 and 1 are the same literal: the second arm is unreachable.
	m := matchExpr(types.Int,
		arm(litp(syntax.LitInt, "0x1", types.Int), false),
		arm(litp(syntax.LitInt, "1", types.Int), false),
		arm(wildp(types.Int), false))
	diags := CheckMatch(m, env, Options{})
	if got := codes(diags); !eqStrs(got, []string{"E0302"}) {
		t.Fatalf("codes = %v, want [E0302]", got)
	}
}

func TestTuplePattern(t *testing.T) {
	env := NewEnv(nil)
	tp := types.Tup(types.Int, types.Int)
	m := matchExpr(tp, arm(tupp(tp, litp(syntax.LitInt, "0", types.Int), wildp(types.Int)), false))
	if got := missing(t, m, env); !eqStrs(got, []string{"(_, _)"}) {
		t.Fatalf("missing = %v, want [(_, _)]", got)
	}
	// Full coverage through a var arm.
	m = matchExpr(tp,
		arm(tupp(tp, litp(syntax.LitInt, "0", types.Int), wildp(types.Int)), false),
		arm(varp(tp), false))
	if diags := CheckMatch(m, env, Options{}); len(diags) != 0 {
		t.Fatalf("tuple + var produced %v", codes(diags))
	}
}

func TestNestedOrInsideCtor(t *testing.T) {
	expr := &types.Con{Name: "Expr"}
	env := NewEnv([]check.Decl{exprDecl()})
	m := matchExpr(expr,
		arm(ctor(expr, "Add",
			orp(expr, ctor(expr, "Num", varp(types.Int)), varp(expr)),
			varp(expr)), false))
	// Add _ _ covered via the or's variable alternative; Num, Neg,
	// Let missing.
	want := []string{"Num _", "Neg _", "Let _ _ _"}
	if got := missing(t, m, env); !eqStrs(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
}

func TestMoreThanFiveMissing(t *testing.T) {
	// A six-variant enum with no arms yields exactly five
	// counterexamples (design.md section 4.2's cap).
	six := &types.Con{Name: "Six"}
	decl := typeDecl("Six", nil,
		variant("A"), variant("B"), variant("C"),
		variant("D"), variant("E"), variant("F"))
	env := NewEnv([]check.Decl{decl})
	m := matchExpr(six, arm(ctor(six, "F"), false))
	want := []string{"A", "B", "C", "D", "E"}
	if got := missing(t, m, env); !eqStrs(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
}

func TestMissingCappedAtFiveNested(t *testing.T) {
	expr := &types.Con{Name: "Expr"}
	env := NewEnv([]check.Decl{exprDecl()})
	// Nothing covered at all: Num _, Add _ _, Neg _, Let _ _ _ —
	// 4 top-level, under the cap.
	m := matchExpr(expr)
	want := []string{"Num _", "Add _ _", "Neg _", "Let _ _ _"}
	if got := missing(t, m, env); !eqStrs(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
}

func TestLetPattern(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	diags := CheckPattern(ctor(o, "Some", varp(types.Int)), CtxLet, env, Options{})
	if got := codes(diags); !eqStrs(got, []string{"E0303"}) {
		t.Fatalf("codes = %v, want [E0303]", got)
	}
	for _, d := range diags {
		if v, _ := d.Data["missing_patterns"].([]string); !eqStrs(v, []string{"None"}) {
			t.Fatalf("missing_patterns = %v, want [None]", d.Data["missing_patterns"])
		}
		if v, _ := d.Data["context"].(string); v != "let" {
			t.Fatalf("context = %v, want let", d.Data["context"])
		}
	}
	// Irrefutable forms pass silently.
	for _, p := range []check.Pat{
		varp(o), wildp(o),
		tupp(types.Tup(types.Int, types.Int), varp(types.Int), varp(types.Int)),
		listp(types.List(types.Int), varp(types.List(types.Int))), // [..rest]
	} {
		if diags := CheckPattern(p, CtxLet, env, Options{}); len(diags) != 0 {
			t.Fatalf("irrefutable %T produced %v", p, codes(diags))
		}
	}
	// A non-empty list pattern is refutable.
	diags = CheckPattern(listp(types.List(types.Int), nil, varp(types.Int)), CtxFor, env, Options{})
	if got := codes(diags); !eqStrs(got, []string{"E0303"}) {
		t.Fatalf("codes = %v, want [E0303]", got)
	}
}

func TestFnParamPattern(t *testing.T) {
	env := NewEnv(nil)
	diags := CheckPattern(litp(syntax.LitInt, "0", types.Int), CtxParam, env, Options{})
	if got := codes(diags); !eqStrs(got, []string{"E0303"}) {
		t.Fatalf("codes = %v, want [E0303]", got)
	}
	for _, d := range diags {
		if v, _ := d.Data["context"].(string); v != "parameter" {
			t.Fatalf("context = %v, want parameter", d.Data["context"])
		}
	}
}

func TestOrPatternBindings(t *testing.T) {
	shape := &types.Con{Name: "Shape"}
	decl := typeDecl("Shape", nil,
		namedVariant("Circle", tfield("radius", 0, types.Float)),
		namedVariant("Rect", tfield("w", 0, types.Float), tfield("h", 1, types.Float)))
	env := NewEnv([]check.Decl{decl})

	bind := func(t types.Type, name string) check.Pat {
		return &check.VarPat{Base: base(t), Sym: &resolve.Symbol{Name: name, Kind: resolve.KindVar}}
	}
	// Same bindings: fine.
	m := matchExpr(shape,
		arm(orp(shape,
			ctor(shape, "Circle", bind(types.Float, "x")),
			ctor(shape, "Rect", bind(types.Float, "x"), wildp(types.Float))), false))
	for _, d := range CheckMatch(m, env, Options{}) {
		if d.Code == "E0304" {
			t.Fatalf("same-binding or-pattern flagged: %v", d.Data)
		}
	}
	// Different bindings: E0304.
	m = matchExpr(shape,
		arm(orp(shape,
			ctor(shape, "Circle", bind(types.Float, "r")),
			ctor(shape, "Rect", bind(types.Float, "w"), bind(types.Float, "h"))), false))
	found := false
	for _, d := range CheckMatch(m, env, Options{}) {
		if d.Code == "E0304" {
			found = true
		}
	}
	if !found {
		t.Fatal("differing bindings did not produce E0304")
	}
}

func TestRecordPattern(t *testing.T) {
	user := &types.Con{Name: "User"}
	decl := &check.TypeDecl{
		Sp:  sp,
		Sym: &resolve.Symbol{Name: "User", Kind: resolve.KindType},
		Body: &check.RecordType{Sp: sp, Fields: []check.TField{
			tfield("name", 0, types.String),
			tfield("age", 1, types.Int),
		}},
	}
	env := NewEnv([]check.Decl{decl})

	// Matching one field's literal leaves the rest uncovered.
	m := matchExpr(user,
		arm(recp(user, []string{"name"}, []check.Pat{
			litp(syntax.LitStr, `"x"`, types.String)}), false))
	got := missing(t, m, env)
	want := []string{`{ name = _, age = _ }`}
	if !eqStrs(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
	// Wildcards everywhere cover the record.
	m = matchExpr(user,
		arm(recp(user, []string{"name", "age"}, []check.Pat{
			wildp(types.String), wildp(types.Int)}), false))
	if diags := CheckMatch(m, env, Options{}); len(diags) != 0 {
		t.Fatalf("record with wild fields produced %v", codes(diags))
	}
}

func TestUnitPattern(t *testing.T) {
	env := NewEnv(nil)
	m := matchExpr(types.Unit,
		arm(litp(syntax.LitUnit, "()", types.Unit), false))
	if diags := CheckMatch(m, env, Options{}); len(diags) != 0 {
		t.Fatalf("() arm produced %v", codes(diags))
	}
}

func TestAsPatPassthrough(t *testing.T) {
	env := NewEnv(nil)
	o := types.Option(types.Int)
	m := matchExpr(o,
		arm(asp(ctor(o, "Some", varp(types.Int))), false))
	if got := missing(t, m, env); !eqStrs(got, []string{"None"}) {
		t.Fatalf("missing = %v, want [None]", got)
	}
}

func TestResultType(t *testing.T) {
	env := NewEnv(nil)
	r := types.Result(types.Int, types.String)
	m := matchExpr(r,
		arm(ctor(r, "Ok", varp(types.Int)), false))
	if got := missing(t, m, env); !eqStrs(got, []string{"Err _"}) {
		t.Fatalf("missing = %v, want [Err _]", got)
	}
}

func TestQualifiedCtorVariant(t *testing.T) {
	// Instantiated generics keep their field types: `Some _` on
	// Option (List Int) sees through to the list inside.
	env := NewEnv(nil)
	o := types.Option(types.List(types.Int))
	m := matchExpr(o,
		arm(ctor(o, "Some", listp(types.List(types.Int), nil)), false),
		arm(ctor(o, "None"), false))
	if got := missing(t, m, env); !eqStrs(got, []string{"Some ([_, .._])"}) {
		t.Fatalf("missing = %v, want [Some ([_, .._])]", got)
	}
}

func TestAbstractTypeNeedsWildcard(t *testing.T) {
	// An abstract type is inhabited but opaque: its constructors
	// cannot be enumerated, so only a wildcard arm covers it. An
	// empty match must report `_`, not pass vacuously.
	abs := &types.Con{Name: "Abs"}
	env := NewEnv([]check.Decl{&check.TypeDecl{
		Sp:   sp,
		Sym:  &resolve.Symbol{Name: "Abs", Kind: resolve.KindType},
		Body: &check.AbstractType{Sp: sp},
	}})
	if got := missing(t, matchExpr(abs), env); !eqStrs(got, []string{"_"}) {
		t.Fatalf("missing = %v, want [_]", got)
	}
	if diags := CheckMatch(matchExpr(abs, arm(wildp(abs), false)), env, Options{}); len(diags) != 0 {
		t.Fatalf("wildcard on abstract type produced %v", codes(diags))
	}
}

// TestE0301WireShape renders the spec example through
// diag.MarshalDiagnostic and checks the wire fields against the
// spec section 12 / testdata/spec expectation shape.
func TestE0301WireShape(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "spec", "E0301_nonexhaustive_match.gom")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("spec fixture unreadable: %v", err)
	}
	expr := &types.Con{Name: "Expr"}
	env := NewEnv([]check.Decl{exprDecl()})
	m := &check.Match{
		Base: base(types.Int),
		Subj: &check.Var{Sp: diag.SpanIn("E0301_nonexhaustive_match.gom", 149, 154), Typ: expr,
			Sym: &resolve.Symbol{Name: "e", Kind: resolve.KindVar}},
	}
	m.Typ = types.Int
	m.Sp = diag.SpanIn("E0301_nonexhaustive_match.gom", 149, 290)
	m.Arms = []check.MatchArm{
		arm(ctor(expr, "Num", varp(types.Int)), false),
		arm(ctor(expr, "Add",
			ctor(expr, "Num", litp(syntax.LitInt, "0", types.Int)),
			varp(expr)), false),
		arm(ctor(expr, "Add", varp(expr), varp(expr)), false),
		arm(ctor(expr, "Neg", ctor(expr, "Neg", varp(expr))), false),
		arm(ctor(expr, "Neg", varp(expr)), true),
	}
	res := diag.NewMapResolver(map[string][]byte{
		"E0301_nonexhaustive_match.gom": src,
	})
	var diags []diag.Diagnostic
	for _, d := range CheckMatch(m, env, Options{Resolver: res}) {
		if d.Code == "E0301" {
			diags = append(diags, d)
		}
	}
	if len(diags) != 1 {
		t.Fatalf("got %d E0301s, want 1", len(diags))
	}
	raw, err := diag.MarshalDiagnostic(diags[0], res)
	if err != nil {
		t.Fatalf("MarshalDiagnostic: %v", err)
	}
	var w map[string]any
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	if w["kind"] != "diagnostic" || w["code"] != "E0301" || w["severity"] != "error" {
		t.Fatalf("wire header = %v", w)
	}
	if w["message"] != "non-exhaustive match on `Expr`" {
		t.Fatalf("message = %v", w["message"])
	}
	data, _ := w["data"].(map[string]any)
	if data["scrutinee_type"] != "Expr" {
		t.Fatalf("scrutinee_type = %v", data["scrutinee_type"])
	}
	got, _ := data["missing_patterns"].([]any)
	want := []string{"Neg (Num _)", "Neg (Add _ _)", "Neg (Let _ _ _)", "Let _ _ _"}
	if len(got) != len(want) {
		t.Fatalf("missing_patterns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("missing_patterns[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	notes, _ := w["notes"].([]any)
	if len(notes) != 1 || notes[0] != "arms with guards do not count toward exhaustiveness" {
		t.Fatalf("notes = %v", notes)
	}
	fixes, _ := w["fixes"].([]any)
	if len(fixes) != 1 {
		t.Fatalf("fixes = %v", fixes)
	}
	f, _ := fixes[0].(map[string]any)
	if f["title"] != "add the missing arms" || f["applicability"] != "placeholder" {
		t.Fatalf("fix header = %v", f)
	}
	edits, _ := f["edits"].([]any)
	if len(edits) != 1 {
		t.Fatalf("edits = %v", edits)
	}
	e, _ := edits[0].(map[string]any)
	wantText := "  Neg (Num _) -> todo ()\n    Neg (Add _ _) -> todo ()\n    Neg (Let _ _ _) -> todo ()\n    Let _ _ _ -> todo ()\n  "
	if e["text"] != wantText {
		t.Fatalf("edit text = %q, want %q", e["text"], wantText)
	}
	start, _ := e["start"].(map[string]any)
	if start["offset"] != float64(289) {
		t.Fatalf("edit start = %v, want offset 289", start)
	}
	if start["line"] != float64(16) || start["col"] != float64(3) {
		t.Fatalf("edit start = %v, want line 16 col 3", start)
	}
}

// TestCheckFileWalks exercises the whole-file walker: a match inside
// a fn body plus a refutable let are both reported.
func TestCheckFileWalks(t *testing.T) {
	expr := &types.Con{Name: "Expr"}
	decl := exprDecl()
	o := types.Option(types.Int)
	body := &check.Block{Base: base(types.Int), Stmts: []check.Expr{
		&check.Let{Base: base(types.Unit),
			Pat:   ctor(o, "Some", varp(types.Int)),
			Value: &check.Var{Base: base(o), Sym: &resolve.Symbol{Name: "o", Kind: resolve.KindVar}}},
		matchExpr(expr,
			arm(ctor(expr, "Num", varp(types.Int)), false)),
	}}
	f := &check.File{
		Sp:      sp,
		Package: "test",
		Decls: []check.Decl{
			decl,
			&check.FnDecl{
				Sp:   sp,
				Sym:  &resolve.Symbol{Name: "f", Kind: resolve.KindFunc},
				Sig:  types.Fn(types.Int, types.P(expr)),
				Body: body,
			},
		},
	}
	diags := CheckFile(f, Options{})
	got := codes(diags)
	want := []string{"E0303", "E0301"}
	if !eqStrs(got, want) {
		t.Fatalf("codes = %v, want %v", got, want)
	}
}
