package goimport

// N-ary (3+) Go results — the wave20 folding: a plain multi-result
// maps to a tuple, a trailing-error multi-result folds the payload
// into `Result (Tuple ...) GoError`, and the representability cap
// is 8 results (docs/decisions/2026-11-18-nary-go-results.md).

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	gt "github.com/podhmo/gom/internal/types"
)

// The derived views: `Triple` is a plain 3-tuple, `DivMod`'s
// trailing error folds to `Result (Int, Int) GoError`, and a
// method-bearing interface element inside a Result payload is
// Option-wrapped — the same rule `(T, error)` applies.
func TestNaryDerivedViews(t *testing.T) {
	t.Parallel()

	l := loaderFor(t)
	pkg := membersOf(t, l, "github.com/podhmo/gom/testdata/goiface")

	triple := pkg.Members["Triple"]
	if triple == nil || triple.Hidden != "" {
		t.Fatalf("Triple missing or hidden: %+v", triple)
	}
	sig, ok := l.SignatureOf(triple)
	if !ok {
		t.Fatal("SignatureOf(Triple) not found")
	}
	if got := sig.Type.String(); got != "Int -> (Int, String, Bool)" {
		t.Errorf("Triple signature = %s, want `Int -> (Int, String, Bool)`", got)
	}
	gs, _ := l.GoSigOf(triple)
	if gs.Results != core.GoResultsNary || len(gs.ResTypes) != 3 {
		t.Errorf("Triple GoSig = %+v, want 3-result nary", gs)
	}

	divmod := pkg.Members["DivMod"]
	sig, ok = l.SignatureOf(divmod)
	if !ok {
		t.Fatal("SignatureOf(DivMod) not found")
	}
	if got := sig.Type.String(); got != "Int -> Int -> Result (Int, Int) GoError" {
		t.Errorf("DivMod signature = %s, want `Int -> Int -> Result (Int, Int) GoError`", got)
	}
	gs, _ = l.GoSigOf(divmod)
	if gs.Results != core.GoResultsTupErr || len(gs.ResTypes) != 3 {
		t.Errorf("DivMod GoSig = %+v, want 3-result tup-err", gs)
	}

	ne := pkg.Members["NetErrTriple"]
	sig, ok = l.SignatureOf(ne)
	if !ok {
		t.Fatal("SignatureOf(NetErrTriple) not found")
	}
	want := "Int -> Result (Option net.Error, Int) GoError"
	if got := sig.Type.String(); got != want {
		t.Errorf("NetErrTriple signature = %s, want `%s`", got, want)
	}
	gs, _ = l.GoSigOf(ne)
	if gs.Results != core.GoResultsTupErr {
		t.Errorf("NetErrTriple GoSig = %+v, want tup-err", gs)
	}
}

// The representability cap: 8 results map, 9 hide.
func TestNaryResultCap(t *testing.T) {
	t.Parallel()

	l := loaderFor(t)
	pkg := membersOf(t, l, "github.com/podhmo/gom/testdata/goiface")

	eight := pkg.Members["Eight"]
	if eight == nil || eight.Hidden != "" {
		t.Fatalf("Eight missing or hidden: %+v", eight)
	}
	if _, ok := l.SignatureOf(eight); !ok {
		t.Error("SignatureOf(Eight) not found")
	}

	nine := pkg.Members["Nine"]
	if nine == nil {
		t.Fatal("Nine missing")
	}
	if nine.Hidden == "" || !strings.Contains(nine.Hidden, "at most 8") {
		t.Errorf("Nine should be Hidden with the cap reason, got %+v", nine)
	}
	if _, ok := l.SignatureOf(nine); ok {
		t.Error("SignatureOf must not answer a hidden member")
	}
}

// An extern entry declares the N-ary views directly: the plain
// tuple and the `Result (Tuple) GoError` fold, including the
// Option-wrapped interface element.
func TestExternNaryViews(t *testing.T) {
	t.Parallel()

	l := loaderFor(t)
	for _, decl := range []string{
		"fn Triple (n: Int) -> (Int, String, Bool)",
		"fn DivMod (a: Int) (b: Int) -> Result (Int, Int) GoError",
		"fn NetErrTriple (n: Int) -> Result (Option NetError, Int) GoError",
		"fn Eight (n: Int) -> (Int, Int, Int, Int, Int, Int, Int, Int)",
	} {
		ds := compileDiags(t, l, `package main

import go "github.com/podhmo/gom/testdata/goiface"

extern go "github.com/podhmo/gom/testdata/goiface" {
  `+decl+`
}
`)
		for _, d := range ds {
			if d.Severity == diag.SeverityError {
				t.Fatalf("%s: unexpected diagnostic %s: %s", decl, d.Code, d.Message)
			}
		}
	}
}

// A verbatim interface element inside a Result-payload tuple is
// rejected — the tuple carries no never-nil boundary check the
// `(T, error)` pair has, so the declared view must spell Option.
func TestExternNaryVerbatimIfaceRejected(t *testing.T) {
	t.Parallel()

	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "github.com/podhmo/gom/testdata/goiface"

extern go "github.com/podhmo/gom/testdata/goiface" {
  fn NetErrTriple (n: Int) -> Result (NetError, Int) GoError
}
`)
	wantDiag(t, ds, "E0509", "declare `Option (net.Error)`")
}

// A declared tuple that drops the trailing error is rejected with
// the folded-Result hint.
func TestExternNaryResultShape(t *testing.T) {
	t.Parallel()

	l := loaderFor(t)
	ds := compileDiags(t, l, `package main

import go "github.com/podhmo/gom/testdata/goiface"

extern go "github.com/podhmo/gom/testdata/goiface" {
  fn DivMod (a: Int) (b: Int) -> (Int, Int)
}
`)
	wantDiag(t, ds, "E0509", "declare `Result (int64, int64) GoError`")
}

// `Go.FuncN` type arguments lift to n+8 — a func type with
// `(T, U, error)` results carries them as raw trailing arguments
// (`Go.Func2 Int Int Int Int GoError`).
func TestNaryFuncNType(t *testing.T) {
	t.Parallel()

	l := loaderFor(t)
	pkg := membersOf(t, l, "github.com/podhmo/gom/testdata/goiface")

	f := pkg.Members["DivModFn"]
	sig, ok := l.SignatureOf(f)
	if !ok {
		t.Fatal("SignatureOf(DivModFn) not found")
	}
	// The FuncN con spells its result arguments in the raw Go view
	// — the folding happens at `Go.fnN` read-back, not in the type.
	want := " -> Go.Func2 Int Int Int Int GoError"
	if got := sig.Type.String(); got != want {
		t.Errorf("DivModFn signature = %s, want `%s`", got, want)
	}
	fn := pkg.Members["TripleFn"]
	sig, ok = l.SignatureOf(fn)
	if !ok {
		t.Fatal("SignatureOf(TripleFn) not found")
	}
	want = " -> Go.Func1 Int Int String Bool"
	if got := sig.Type.String(); got != want {
		t.Errorf("TripleFn signature = %s, want `%s`", got, want)
	}
	fnTy, isFn := sig.Type.(*gt.Func)
	if !isFn {
		t.Fatalf("TripleFn type = %v, want a Func", sig.Type)
	}
	if c, isCon := gt.Chase(fnTy.Ret).(*gt.Con); !isCon || c.Name != "Func1" || len(c.Args) != 4 {
		t.Errorf("TripleFn return = %v, want Go.Func1 con with 4 args", fnTy.Ret)
	}
}
