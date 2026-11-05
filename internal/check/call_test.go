package check

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

var sp = diag.SpanIn("test.gom", 10, 20)

// transfer : Account -> ~to: Account -> ~amount: Money -> Result Unit TxError
var transferSig = types.Fn(types.Result(types.Unit, &types.Con{Name: "TxError"}),
	types.P(&types.Con{Name: "Account"}),
	types.LP("to", &types.Con{Name: "Account"}),
	types.LP("amount", &types.Con{Name: "Money"}))

var transferSym = &resolve.Symbol{Name: "transfer", Kind: resolve.KindFunc}

func v(name string, k resolve.Kind, t types.Type) *Var {
	return &Var{Sp: sp, Typ: t, Sym: &resolve.Symbol{Name: name, Kind: k}}
}

func arg(v Expr) Arg { return Arg{Value: v} }

func TestCallInfoScenarios(t *testing.T) {
	account := &types.Con{Name: "Account"}
	money := &types.Con{Name: "Money"}
	alice := v("alice", resolve.KindVar, account)
	bob := v("bob", resolve.KindVar, account)
	m := v("m", resolve.KindVar, money)

	t.Run("saturated call", func(t *testing.T) {
		// transfer alice ~to: bob ~amount: 100
		app := &Apply{
			Sp: sp, Typ: types.Result(types.Unit, &types.Con{Name: "TxError"}),
			Fn:   &Var{Sp: sp, Typ: transferSig, Sym: transferSym},
			Args: []Arg{arg(alice), {Label: "to", Value: bob}, {Label: "amount", Value: m}},
			Call: CallInfo{
				Kind:       CallDirect,
				Callee:     transferSym,
				Arity:      3,
				Applied:    3,
				Unapplied:  0,
				ArgToParam: []int{0, 1, 2},
				HoleIndex:  -1,
			},
		}
		if err := app.Call.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if app.Call.Callee.Name != "transfer" {
			t.Errorf("callee = %v", app.Call.Callee)
		}
	})

	t.Run("labeled arguments reordered", func(t *testing.T) {
		// transfer ~to: bob ~amount: m alice — evaluated in source
		// order, supplied to parameters 1, 2, 0.
		app := &Apply{
			Sp:   sp,
			Fn:   &Var{Sp: sp, Typ: transferSig, Sym: transferSym},
			Args: []Arg{{Label: "to", Value: bob}, {Label: "amount", Value: m}, arg(alice)},
			Call: CallInfo{
				Kind:       CallDirect,
				Callee:     transferSym,
				Arity:      3,
				Applied:    3,
				ArgToParam: []int{1, 2, 0},
				HoleIndex:  -1,
			},
		}
		if err := app.Call.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("partial application", func(t *testing.T) {
		// transfer alice ~to: bob — ~amount remains.
		// type: ~amount: Money -> Result Unit TxError
		rest := types.Fn(types.Result(types.Unit, &types.Con{Name: "TxError"}),
			types.LP("amount", money))
		app := &Apply{
			Sp: sp, Typ: rest,
			Fn:   &Var{Sp: sp, Typ: transferSig, Sym: transferSym},
			Args: []Arg{arg(alice), {Label: "to", Value: bob}},
			Call: CallInfo{
				Kind:       CallPartial,
				Callee:     transferSym,
				Arity:      3,
				Applied:    2,
				Unapplied:  1,
				ArgToParam: []int{0, 1},
				HoleIndex:  -1,
			},
		}
		if err := app.Call.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if got := app.Type(); !types.Equal(got, rest) {
			t.Errorf("partial application type = %s, want %s", got, rest)
		}
	})

	t.Run("go call", func(t *testing.T) {
		// fmt.Println(a, b) — variadic Go function.
		printlnSym := &resolve.Symbol{Name: "Println", Kind: resolve.KindFunc, IsGo: true, Package: "fmt"}
		app := &Apply{
			Sp: sp, Typ: types.Unit,
			Fn:   &Var{Sp: sp, Sym: printlnSym},
			Args: []Arg{arg(alice), arg(bob)},
			Call: CallInfo{
				Kind:      CallGo,
				Callee:    printlnSym,
				Arity:     1, // one variadic parameter
				Applied:   2,
				Unapplied: 0,
				Variadic:  true,
				HoleIndex: -1,
			},
		}
		if err := app.Call.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if !app.Call.Callee.IsGo {
			t.Error("go callee not marked IsGo")
		}
	})

	t.Run("cute placeholder", func(t *testing.T) {
		// strings.Repeat(_, 3) — a function value waiting for arg 0.
		repeatSym := &resolve.Symbol{Name: "Repeat", Kind: resolve.KindFunc, IsGo: true, Package: "strings"}
		app := &Apply{
			Sp: sp, Typ: types.Fn(types.String, types.P(types.String)),
			Fn: &Var{Sp: sp, Sym: repeatSym},
			Args: []Arg{
				{Value: &Hole{Sp: sp, Typ: types.String}},
				{Value: &Lit{Sp: sp, Typ: types.Int, Kind: syntax.LitInt, Text: "3"}},
			},
			Call: CallInfo{
				Kind:      CallGo,
				Callee:    repeatSym,
				Arity:     2,
				Applied:   2,
				HoleIndex: 0,
			},
		}
		if err := app.Call.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("unknown function value", func(t *testing.T) {
		// f x y where f : Int -> Int -> Int is a let-bound value.
		fs := &resolve.Symbol{Name: "f", Kind: resolve.KindVar}
		app := &Apply{
			Sp: sp, Typ: types.Int,
			Fn:   &Var{Sp: sp, Typ: types.Fn(types.Int, types.P(types.Int), types.P(types.Int)), Sym: fs},
			Args: []Arg{arg(&Lit{Sp: sp, Typ: types.Int, Kind: syntax.LitInt, Text: "1"})},
			Call: CallInfo{
				Kind:      CallUnknown,
				Callee:    nil,
				Arity:     2,
				Applied:   1,
				Unapplied: 0,
				HoleIndex: -1,
			},
		}
		if err := app.Call.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
}

func TestCallInfoValidateFailures(t *testing.T) {
	cases := []struct {
		name string
		call CallInfo
	}{
		{"zero kind", CallInfo{}},
		{"unknown with callee", CallInfo{Kind: CallUnknown, Callee: transferSym, HoleIndex: -1}},
		{"direct without callee", CallInfo{Kind: CallDirect, HoleIndex: -1}},
		{"partial with nothing unapplied", CallInfo{Kind: CallPartial, Callee: transferSym, Arity: 3, Applied: 3, Unapplied: 0, HoleIndex: -1}},
		{"partial arity mismatch", CallInfo{Kind: CallPartial, Callee: transferSym, Arity: 3, Applied: 1, Unapplied: 1, HoleIndex: -1}},
		{"direct with unapplied", CallInfo{Kind: CallDirect, Callee: transferSym, Arity: 3, Applied: 3, Unapplied: 1, HoleIndex: -1}},
		{"direct underapplied", CallInfo{Kind: CallDirect, Callee: transferSym, Arity: 3, Applied: 2, Unapplied: 0, HoleIndex: -1}},
		{"hole on non-go call", CallInfo{Kind: CallDirect, Callee: transferSym, Arity: 1, Applied: 1, HoleIndex: 0}},
		{"hole out of range", CallInfo{Kind: CallGo, Callee: transferSym, Arity: 2, Applied: 2, HoleIndex: 5}},
		{"argtoparam length", CallInfo{Kind: CallDirect, Callee: transferSym, Arity: 1, Applied: 1, ArgToParam: []int{0, 1}, HoleIndex: -1}},
		{"negative arity", CallInfo{Kind: CallDirect, Callee: transferSym, Arity: -1, Applied: 0, HoleIndex: -1}},
		{"argtoparam out of range", CallInfo{Kind: CallDirect, Callee: transferSym, Arity: 2, Applied: 2, ArgToParam: []int{0, 2}, HoleIndex: -1}},
		{"argtoparam negative", CallInfo{Kind: CallDirect, Callee: transferSym, Arity: 2, Applied: 2, ArgToParam: []int{0, -1}, HoleIndex: -1}},
		{"argtoparam fills param twice", CallInfo{Kind: CallDirect, Callee: transferSym, Arity: 2, Applied: 2, ArgToParam: []int{1, 1}, HoleIndex: -1}},
		{"over-applied with argtoparam", CallInfo{Kind: CallDirect, Callee: transferSym, Arity: 1, Applied: 2, ArgToParam: []int{0, 0}, HoleIndex: -1}},
	}
	for _, tc := range cases {
		if err := tc.call.Validate(); err == nil {
			t.Errorf("%s: expected validation error", tc.name)
		}
	}
}

func TestApplyValidate(t *testing.T) {
	account := &types.Con{Name: "Account"}
	alice := v("alice", resolve.KindVar, account)
	fn := &Var{Sp: sp, Typ: transferSig, Sym: transferSym}
	goSym := &resolve.Symbol{Name: "Repeat", Kind: resolve.KindFunc, IsGo: true, Package: "strings"}

	t.Run("valid", func(t *testing.T) {
		app := &Apply{
			Sp: sp, Typ: account,
			Fn:   fn,
			Args: []Arg{arg(alice)},
			Call: CallInfo{Kind: CallPartial, Callee: transferSym,
				Arity: 3, Applied: 1, Unapplied: 2, HoleIndex: -1},
		}
		if err := app.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("valid cute call", func(t *testing.T) {
		app := &Apply{
			Sp: sp, Typ: account,
			Fn: &Var{Sp: sp, Sym: goSym},
			Args: []Arg{
				{Value: &Hole{Sp: sp, Typ: account}},
				{Value: &Lit{Sp: sp, Typ: types.Int, Kind: syntax.LitInt, Text: "3"}},
			},
			Call: CallInfo{Kind: CallGo, Callee: goSym, Arity: 2, Applied: 2, HoleIndex: 0},
		}
		if err := app.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	cases := []struct {
		name string
		app  *Apply
	}{
		{"invalid call info", &Apply{
			Fn:   fn,
			Args: []Arg{arg(alice)},
			Call: CallInfo{Kind: CallDirect, Callee: transferSym, Arity: 3, Applied: 2, HoleIndex: -1},
		}},
		{"args/applied mismatch", &Apply{
			Fn:   fn,
			Args: []Arg{arg(alice)},
			Call: CallInfo{Kind: CallPartial, Callee: transferSym, Arity: 3, Applied: 2, Unapplied: 1, HoleIndex: -1},
		}},
		{"hole index on non-hole arg", &Apply{
			Fn:   &Var{Sp: sp, Sym: goSym},
			Args: []Arg{arg(alice), arg(alice)},
			Call: CallInfo{Kind: CallGo, Callee: goSym, Arity: 2, Applied: 2, HoleIndex: 0},
		}},
		{"hole without hole index", &Apply{
			Fn:   &Var{Sp: sp, Sym: goSym},
			Args: []Arg{{Value: &Hole{Sp: sp}}, arg(alice)},
			Call: CallInfo{Kind: CallGo, Callee: goSym, Arity: 2, Applied: 2, HoleIndex: -1},
		}},
		{"second stray hole", &Apply{
			Fn:   &Var{Sp: sp, Sym: goSym},
			Args: []Arg{{Value: &Hole{Sp: sp}}, {Value: &Hole{Sp: sp}}},
			Call: CallInfo{Kind: CallGo, Callee: goSym, Arity: 2, Applied: 2, HoleIndex: 0},
		}},
	}
	for _, tc := range cases {
		if err := tc.app.Validate(); err == nil {
			t.Errorf("%s: expected validation error", tc.name)
		}
	}
}

func TestCallKindString(t *testing.T) {
	for k, want := range map[CallKind]string{
		CallUnknown: "unknown", CallDirect: "direct", CallPartial: "partial", CallGo: "go",
	} {
		if got := k.String(); got != want {
			t.Errorf("CallKind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
}
