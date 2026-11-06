package lower

import (
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// errPairView is one declared view of a Go `(T, error)` call's
// payload — the distinction payloadNilGuard and the ConvOptNil
// selection hinge on.
type errPairView struct {
	name      string
	viewT     types.Type    // the callee's declared result type — a builtin Result
	payloadT  types.Type    // viewT's Ok payload — the try / |?> payload's type
	rawT      types.Type    // GoSig.ResTypes[0] — the raw first result
	wantGuard bool          // the never-nil claim — NilGuard on the boundary node
	wantConv  core.ConvKind // the payload's Gom-facing conversion; 0 = none
}

var errPairViews = []errPairView{
	{
		// The explicit `Result GoError GoError` claim: the raw
		// `(error, error)` payload is declared never nil, so
		// every boundary verifies `v != nil` (spec section 8.6).
		name:      "strict Result GoError GoError",
		viewT:     types.Result(types.GoError, types.GoError),
		payloadT:  types.GoError,
		rawT:      types.GoError,
		wantGuard: true,
	},
	{
		// The derived `(error, error)` view: the payload
		// observes nil as `None` — ConvOptNil, never a guard.
		name:     "derived Result (Option GoError) GoError",
		viewT:    types.Result(types.Option(types.GoError), types.GoError),
		payloadT: types.Option(types.GoError),
		rawT:     types.GoError,
		wantConv: core.ConvOptNil,
	},
	{
		// A non-error payload (`(int, error)` → `Result Int
		// GoError`): no nil contract applies, so neither the
		// guard nor the Option conversion appears.
		name:     "non-error (int, error)",
		viewT:    types.Result(types.Int, types.GoError),
		payloadT: types.Int,
		rawT:     types.GoInt,
		wantConv: core.ConvNum,
	},
}

// errPairEnv is the Env.GoSig oracle for a `(rawT, error)` Go
// callee of the given arity — the facts the import loader records,
// including ResNilable (raw result 0 is a method-bearing interface;
// `error` qualifies, `int` does not).
func errPairEnv(mk *resolve.Symbol, arity int, rawT types.Type) *Env {
	return &Env{GoSig: func(sym *resolve.Symbol) (GoSig, bool) {
		if sym != mk {
			return GoSig{}, false
		}
		return GoSig{Arity: arity, Results: core.GoResultsErr,
			ResTypes:   []types.Type{rawT, types.GoError},
			ResNilable: rawT == types.GoError}, true
	}}
}

// errPairCall builds `mk "x"` — a saturated arity-1 `(rawT, error)`
// Go call the Gom view declares as viewT.
func errPairCall(mk *resolve.Symbol, v errPairView) *check.Apply {
	return app(vr(mk, types.Fn(v.viewT, types.P(types.String))),
		[]check.Expr{strLit("x")},
		check.CallInfo{Kind: check.CallGo, Callee: mk,
			Arity: 1, Applied: 1, HoleIndex: -1}, v.viewT)
}

// boundaryNodes collects the Core nodes a `(T, error)` Go call's
// boundary can produce wherever they sit — lets, tails, closure and
// segment bodies, match arms. EscapeCall and ResultFromGo carry the
// strict-view NilGuard claim; Conv records the payload conversion
// (ConvOptNil / ConvNum) the raw value takes on its way in.
type boundaryNodes struct {
	escapes []*core.EscapeCall
	results []*core.ResultFromGo
	convs   []*core.Conv
}

func collectBoundary(f *core.File) boundaryNodes {
	var out boundaryNodes
	var walkBody func(b *core.Body)
	var walkTree func(t core.DTree)
	var walkExpr func(x core.Expr)

	walkTree = func(t core.DTree) {
		switch t := t.(type) {
		case *core.DSwitch:
			for _, c := range t.Cases {
				walkBody(c.Body)
			}
			walkTree(t.Default)
		case *core.DLeaf:
			walkBody(t.Body)
		case *core.DGuard:
			walkTree(t.Then)
			walkTree(t.Else)
		case *core.DPanic:
		default:
			panic("unreachable")
		}
	}
	walkBody = func(b *core.Body) {
		if b == nil {
			return
		}
		for _, l := range b.Lets {
			walkExpr(l.X)
		}
		walkExpr(b.Tail)
	}
	walkExpr = func(x core.Expr) {
		switch x := x.(type) {
		case *core.EscapeCall:
			out.escapes = append(out.escapes, x)
		case *core.ResultFromGo:
			out.results = append(out.results, x)
		case *core.Conv:
			out.convs = append(out.convs, x)
		case *core.Closure:
			walkBody(x.Fn.Body)
		case *core.If:
			walkBody(x.Then)
			walkBody(x.Else)
		case *core.Match:
			walkTree(x.Tree)
		case *core.SelectExpr:
			for _, a := range x.Arms {
				switch a := a.(type) {
				case *core.SelRecv:
					walkBody(a.Body)
				case *core.SelSend:
					walkBody(a.Body)
				case *core.SelDefault:
					walkBody(a.Body)
				default:
					panic("unreachable")
				}
			}
		case *core.Spawn:
			walkBody(x.Body)
		case *core.ForRange:
			walkBody(x.Body)
		case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
			*core.GoFuncRef, *core.FieldGet, *core.Unary, *core.Binary,
			*core.TupleNew, *core.ListNew, *core.RecordNew,
			*core.RecordUpdate, *core.Equal,
			*core.CallDirect, *core.CallFunc, *core.CallGo,
			*core.CallBuiltin, *core.CtorApp, *core.TailCall,
			*core.EscapeResult, *core.OptionFromGo, *core.ErrorFromGo,
			*core.TupleFromGo:
			// Atom operands only — nothing to descend into.
		default:
			panic("unreachable")
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *core.FuncDecl:
			walkBody(d.Fn.Body)
		case *core.VarDecl:
			walkBody(d.Body)
		case *core.TestDecl:
			walkBody(d.Fn.Body)
		case *core.TypeDecl:
		default:
			panic("unreachable")
		}
	}
	return out
}

// TestGoErrPairNilGuardPins: a `(T, error)` Go call crosses into
// Gom at exactly the boundary nodes below — ResultFromGo when the
// Result materializes (wrapGoResult) and EscapeCall wherever `try`
// or `|?>` fuses the call instead (tryExpr, pipeInflowAtom,
// escapeStepPayload, pipeRest's unguarded-step emission). Every
// site takes the never-nil claim's NilGuard only under the strict
// view — payloadNilGuard requires a nil-able raw result
// (ResNilable: a method-bearing interface) whose declared payload
// equals the raw result type — while the Option view's
// payload arrives through ConvOptNil and a non-error payload
// through neither.
func TestGoErrPairNilGuardPins(t *testing.T) {
	sites := []struct {
		name string
		// build returns the fn decl exercising the site plus the
		// Env the callee's GoSig oracle answers from.
		build func(mk *resolve.Symbol, v errPairView) (*check.FnDecl, *Env)
		node  string // the boundary node the site emits: "EscapeCall" | "ResultFromGo"
	}{
		{
			// `fn f () -> R = mk "x"` — the bare call
			// materializes its Result at the tail.
			name: "bare call -> ResultFromGo (wrapGoResult)",
			node: "ResultFromGo",
			build: func(mk *resolve.Symbol, v errPairView) (*check.FnDecl, *Env) {
				return decl("f", nil, errPairCall(mk, v), types.Fn(v.viewT)),
					errPairEnv(mk, 1, v.rawT)
			},
		},
		{
			// `fn f () -> R = { let v = try (mk "x"); Ok v }`
			// — try fuses the call into EscapeCall at the
			// user boundary (a try cannot be the body tail:
			// it yields the payload, not the Result).
			name: "try -> EscapeCall (tryExpr)",
			node: "EscapeCall",
			build: func(mk *resolve.Symbol, v errPairView) (*check.FnDecl, *Env) {
				ok := csym("Ok")
				out := vsym("v", v.payloadT)
				body := &check.Block{Base: b(v.viewT), Stmts: []check.Expr{
					&check.Let{Base: b(types.Unit), Pat: vpat(out, v.payloadT),
						Value: &check.Try{Base: b(v.payloadT), X: errPairCall(mk, v)}},
					app(vr(ok, types.Fn(v.viewT, types.P(v.payloadT))),
						[]check.Expr{vr(out, v.payloadT)},
						check.CallInfo{Kind: check.CallDirect, Callee: ok,
							Arity: 1, Applied: 1, HoleIndex: -1}, v.viewT),
				}}
				return decl("f", nil, body, types.Fn(v.viewT)),
					errPairEnv(mk, 1, v.rawT)
			},
		},
		{
			// `fn f () -> R = mk "x" |?> step` — the inflow
			// Go call fuses into EscapeCall inside the
			// synthesized boundary closure.
			name: "|?> inflow -> EscapeCall (pipeInflowAtom)",
			node: "EscapeCall",
			build: func(mk *resolve.Symbol, v errPairView) (*check.FnDecl, *Env) {
				step := fsym("step")
				body := &check.Binary{Base: b(v.viewT), Op: syntax.BinPipeTry,
					X: errPairCall(mk, v),
					Y: vr(step, types.Fn(v.viewT, types.P(v.payloadT)))}
				return decl("f", nil, body, types.Fn(v.viewT)),
					errPairEnv(mk, 1, v.rawT)
			},
		},
		{
			// `fn f (r) -> R = r |?> mk` — the step is a
			// unary Go call fused through
			// escapeStepPayload; the stored-Result inflow
			// unwraps by EscapeResult alone.
			name: "|?> step -> EscapeCall (escapeStepPayload)",
			node: "EscapeCall",
			build: func(mk *resolve.Symbol, v errPairView) (*check.FnDecl, *Env) {
				r := vsym("r", v.viewT)
				body := &check.Binary{Base: b(v.viewT), Op: syntax.BinPipeTry,
					X: vr(r, v.viewT),
					Y: vr(mk, types.Fn(v.viewT, types.P(v.payloadT)))}
				return decl("f", []check.Param{param(vpat(r, v.viewT))},
						body, types.Fn(v.viewT, types.P(v.viewT))),
					errPairEnv(mk, 1, v.rawT)
			},
		},
		{
			// `fn f (r, r2) -> R = r |?> mk (try r2)` —
			// the unguarded try forces pipeTryMixed; the
			// Go step's call proper joins the next
			// segment as a fused EscapeCall via
			// pipeRest's wrap branch.
			name: "|?> unguarded step -> EscapeCall (pipeRest)",
			node: "EscapeCall",
			build: func(mk *resolve.Symbol, v errPairView) (*check.FnDecl, *Env) {
				r := vsym("r", v.viewT)
				r2 := vsym("r2", v.viewT)
				tryArg := &check.Try{Base: b(v.payloadT), X: vr(r2, v.viewT)}
				step := app(vr(mk, types.Fn(v.viewT,
					types.P(v.payloadT), types.P(v.payloadT))),
					[]check.Expr{tryArg},
					check.CallInfo{Kind: check.CallGo, Callee: mk,
						Arity: 2, Applied: 1, HoleIndex: -1},
					types.Fn(v.viewT, types.P(v.payloadT)))
				body := &check.Binary{Base: b(v.viewT), Op: syntax.BinPipeTry,
					X: vr(r, v.viewT), Y: step}
				return decl("f", []check.Param{
						param(vpat(r, v.viewT)), param(vpat(r2, v.viewT)),
					}, body, types.Fn(v.viewT, types.P(v.viewT), types.P(v.viewT))),
					errPairEnv(mk, 2, v.rawT)
			},
		},
	}
	for _, site := range sites {
		for _, v := range errPairViews {
			t.Run(site.name+" / "+v.name, func(t *testing.T) {
				mk := gsym("mk")
				d, env := site.build(mk, v)
				f := &check.File{Sp: sp, Package: "p", PkgSp: sp,
					Decls: []check.Decl{d}}
				out, diags := File(f, env)
				for _, dg := range diags {
					t.Errorf("unexpected diagnostic: %v", dg)
				}
				ns := collectBoundary(out)
				switch site.node {
				case "ResultFromGo":
					if len(ns.escapes) != 0 {
						t.Fatalf("EscapeCall count = %d, want 0", len(ns.escapes))
					}
					if len(ns.results) != 1 {
						t.Fatalf("ResultFromGo count = %d, want 1", len(ns.results))
					}
					rg := ns.results[0]
					if rg.NilGuard != v.wantGuard {
						t.Errorf("ResultFromGo.NilGuard = %v, want %v",
							rg.NilGuard, v.wantGuard)
					}
					if rg.ValConv != v.wantConv {
						t.Errorf("ResultFromGo.ValConv = %v, want %v",
							rg.ValConv, v.wantConv)
					}
				case "EscapeCall":
					if len(ns.results) != 0 {
						t.Fatalf("ResultFromGo count = %d, want 0 — "+
							"the fused call keeps its raw pair",
							len(ns.results))
					}
					if len(ns.escapes) != 1 {
						t.Fatalf("EscapeCall count = %d, want 1", len(ns.escapes))
					}
					if ns.escapes[0].NilGuard != v.wantGuard {
						t.Errorf("EscapeCall.NilGuard = %v, want %v",
							ns.escapes[0].NilGuard, v.wantGuard)
					}
					got := map[core.ConvKind]int{}
					for _, c := range ns.convs {
						got[c.Kind]++
					}
					if v.wantConv == 0 {
						if len(ns.convs) != 0 {
							t.Errorf("Conv nodes = %v, want none — the payload "+
								"already spells the declared type", got)
						}
					} else if got[v.wantConv] != 1 || len(ns.convs) != 1 {
						t.Errorf("Conv nodes = %v, want exactly one %v",
							got, v.wantConv)
					}
				default:
					panic("unreachable")
				}
			})
		}
	}
}
