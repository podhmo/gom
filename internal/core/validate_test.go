package core

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// fnFile wraps a body in `fn f (u: Unit) -> R`.
func fnFile(ret types.Type, body *Body) *File {
	uB, _ := vbind("u", types.Unit)
	fn := &Func{
		Sp:     sp(),
		Sig:    types.Fn(ret, types.P(types.Unit)),
		Params: []*Bind{uB},
		Body:   body,
	}
	return &File{
		Sp: sp(), Package: "p", PkgSp: sp(),
		Decls: []Decl{&FuncDecl{Sp: sp(), Sym: fsym("f"), Fn: fn}},
	}
}

// fnOf returns the single FuncDecl's function for target wiring.
func fnOf(f *File) *Func { return f.Decls[0].(*FuncDecl).Fn }

// entryFile builds a one-decl file `package pkg; fn name (u: Unit)
// -> ret = ()` with IsEntry set — the shape lower produces for
// `fn main () -> Unit` in `package main` (a discarded Unit binder,
// Unit residual). discard=false keeps the parameter named, the
// dangling-binder bug the tightened rule rejects.
func entryFile(pkg, name string, ret types.Type, discard bool) *File {
	param := &Bind{Sp: sp(), Typ: types.Unit, Discard: discard}
	if !discard {
		param.Sym = vsym("u")
	}
	return &File{
		Sp: sp(), Package: pkg, PkgSp: sp(),
		Decls: []Decl{&FuncDecl{
			Sp: sp(), Sym: fsym(name), IsEntry: true,
			Fn: &Func{
				Sp:     sp(),
				Sig:    types.Fn(ret, types.P(types.Unit)),
				Params: []*Bind{param},
				Body:   &Body{Tail: unitLit()},
			},
		}},
	}
}

// TestValidateAcceptsEntry pins the lowered `fn main () -> Unit`
// shape: package main, a discarded Unit binder, Unit residual.
func TestValidateAcceptsEntry(t *testing.T) {
	if got := Validate(entryFile("main", "main", types.Unit, true)); len(got) != 0 {
		for _, v := range got {
			t.Errorf("unexpected violation: %s", v)
		}
	}
}

func TestValidateAcceptsValidFile(t *testing.T) {
	if got := Validate(okFile); len(got) != 0 {
		for _, v := range got {
			t.Errorf("unexpected violation: %s", v)
		}
	}
}

func TestValidateValueAlias(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*FuncDecl)
		want string
	}{
		{name: "canonical variable wrapper", edit: func(*FuncDecl) {}},
		{name: "function symbol", edit: func(d *FuncDecl) { d.Sym = fsym("alias") }, want: "want var"},
		{name: "entry", edit: func(d *FuncDecl) { d.IsEntry = true }, want: "value alias must be"},
		{name: "nil function", edit: func(d *FuncDecl) { d.Fn = nil }, want: "value alias must be"},
		{name: "zero arity", edit: func(d *FuncDecl) { d.Fn.Params = nil }, want: "value alias must be"},
		{name: "multiple parameters", edit: func(d *FuncDecl) {
			d.Fn.Params = append(d.Fn.Params, bind(vsym("v"), types.Unit))
		}, want: "value alias must be"},
		{name: "type parameters", edit: func(d *FuncDecl) {
			d.TypeParams = []*types.Var{{ID: 1, Name: "a"}}
		}, want: "value alias must be"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fnFile(types.Unit, &Body{Tail: unitLit()})
			d := f.Decls[0].(*FuncDecl)
			d.ValueAlias, d.Sym = true, vsym("alias")
			tc.edit(d)
			got := Validate(f)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("unexpected violations: %v", got)
				}
				return
			}
			for _, v := range got {
				if strings.Contains(v.String(), tc.want) {
					return
				}
			}
			t.Fatalf("violations %v lack %q", got, tc.want)
		})
	}
}

// A Go package member (`os.O_RDONLY`, `math.Pi`) is referenced by
// qualification, not by an enclosing binder — Var's contract admits
// it without a scope entry. A non-Go member still demands the local
// binding (the "var out of scope" violation case below).
func TestValidateAcceptsGoPkgVar(t *testing.T) {
	goVar := &resolve.Symbol{Name: "O_RDONLY", Kind: resolve.KindVar, IsGo: true, Package: "os"}
	// The shape lower emits for `let _ = os.O_RDONLY`: the raw-typed
	// reference inside the int64 conversion.
	f := fnFile(types.Int, &Body{Tail: &Conv{
		Sp: sp(), Typ: types.Int,
		Kind: ConvNum, X: vref(goVar, types.GoInt),
	}})
	if got := Validate(f); len(got) != 0 {
		for _, v := range got {
			t.Errorf("unexpected violation: %s", v)
		}
	}
}

// TestValidateAcceptsAritySplitFunc pins P0-2 rule 2 as a standalone
// regression: a declared arity smaller than the signature's flat
// arrow length is legal — the body returns the residual function
// type — and a `try` inside the innermost full-arity function still
// escapes through it.
func TestValidateAcceptsAritySplitFunc(t *testing.T) {
	// fn mk (r: Result Int GoError) -> (Int -> Result Int GoError)
	//   = fn b -> let n = try r in Ok (n + b)
	rB, rS := vbind("r", resIG)
	bB, bS := vbind("b", types.Int)
	nB, nS := vbind("n", types.Int)
	sumB, sumS := vbind("sum", types.Int)
	inner := &Func{
		Sp:     sp(),
		Sig:    types.Fn(resIG, types.P(types.Int)),
		Params: []*Bind{bB},
		Body: &Body{
			Lets: []*Let{
				{Sp: sp(), Name: nB, X: &EscapeResult{
					Sp: sp(), Typ: types.Int,
					X: vref(rS, resIG),
				}},
				{Sp: sp(), Name: sumB, X: &Binary{
					Sp: sp(), Typ: types.Int,
					Op: syntax.BinAdd, X: vref(nS, types.Int), Y: vref(bS, types.Int),
				}},
			},
			Tail: &CtorApp{
				Sp: sp(), Typ: resIG,
				Ctor: okCtor, Args: []Atom{vref(sumS, types.Int)}, Arity: 1,
			},
		},
	}
	inner.Body.Lets[0].X.(*EscapeResult).To = inner
	f := &File{Sp: sp(), Package: "p", PkgSp: sp(), Decls: []Decl{&FuncDecl{
		Sp: sp(), Sym: fsym("mk"),
		Fn: &Func{
			Sp:     sp(),
			Sig:    types.Fn(resIG, types.P(resIG), types.P(types.Int)),
			Params: []*Bind{rB},
			Body: &Body{Tail: &Closure{
				Sp: sp(), Typ: types.Fn(resIG, types.P(types.Int)),
				Fn: inner,
			}},
		},
	}}}
	if got := Validate(f); len(got) != 0 {
		for _, v := range got {
			t.Errorf("unexpected violation: %s", v)
		}
	}
}

// TestValidateAcceptsGoLambdaEscape pins the sanctioned pattern of
// spec section 10.3: `try` inside a lambda inside `go {}` escapes
// through the lambda — go{} is not a boundary but the lambda is.
func TestValidateAcceptsGoLambdaEscape(t *testing.T) {
	// fn work (src: Result Int GoError) -> Unit =
	//   go { let g = fn (u: Unit) -> Result Int GoError = try src
	//        let _ = g ()
	//        () }
	srcB, srcS := vbind("src", resIG)
	uB, _ := vbind("u", types.Unit)
	nB, nS := vbind("n", types.Int)
	gB, gS := vbind("g", types.Fn(resIG, types.P(types.Unit)))
	gfn := &Func{
		Sp:     sp(),
		Sig:    types.Fn(resIG, types.P(types.Unit)),
		Params: []*Bind{uB},
		Body: &Body{
			Lets: []*Let{{Sp: sp(), Name: nB, X: &EscapeResult{
				Sp: sp(), Typ: types.Int,
				X: vref(srcS, resIG),
			}}},
			Tail: &CtorApp{
				Sp: sp(), Typ: resIG,
				Ctor: okCtor, Args: []Atom{vref(nS, types.Int)}, Arity: 1,
			},
		},
	}
	gfn.Body.Lets[0].X.(*EscapeResult).To = gfn
	f := &File{Sp: sp(), Package: "p", PkgSp: sp(), Decls: []Decl{&FuncDecl{
		Sp: sp(), Sym: fsym("work"),
		Fn: &Func{
			Sp:     sp(),
			Sig:    types.Fn(types.Unit, types.P(resIG)),
			Params: []*Bind{srcB},
			Body: &Body{Tail: &Spawn{
				Sp: sp(), Typ: types.Unit,
				Body: &Body{
					Lets: []*Let{
						{Sp: sp(), Name: gB, X: &Closure{
							Sp: sp(), Typ: types.Fn(resIG, types.P(types.Unit)),
							Fn: gfn,
						}},
						{Sp: sp(), Name: &Bind{Sp: sp(), Typ: resIG, Discard: true}, X: &CallFunc{
							Sp: sp(), Typ: resIG,
							Fn:   vref(gS, types.Fn(resIG, types.P(types.Unit))),
							Args: []Atom{unitLit()},
						}},
					},
					Tail: unitLit(),
				},
			}},
		},
	}}}
	if got := Validate(f); len(got) != 0 {
		for _, v := range got {
			t.Errorf("unexpected violation: %s", v)
		}
	}
}

func TestValidateViolations(t *testing.T) {
	v := vsym("x")
	fnVal := fsym("g")
	goFn := gsym("Itoa", "strconv")
	goPair := gsym("Atoi", "strconv")

	// A small valid body: `let x = 1; x`
	xb, _ := vbind("x", types.Int)
	validBody := func() *Body {
		return &Body{
			Lets: []*Let{{Sp: sp(), Name: xb, X: intLit("1")}},
			Tail: vref(xb.Sym, types.Int),
		}
	}
	_ = validBody

	cases := []struct {
		name string
		file *File
		want string // substring of the expected violation
	}{
		{
			name: "node without span",
			file: fnFile(types.Unit, &Body{Tail: &Lit{
				Typ: types.Unit, Kind: syntax.LitUnit, Text: "()"}}),
			want: "no valid source span",
		},
		{
			name: "node without type",
			file: fnFile(types.Unit, &Body{Tail: &Lit{
				Sp: sp(), Kind: syntax.LitUnit, Text: "()"}}),
			want: "nil type",
		},
		{
			name: "var out of scope",
			file: fnFile(types.Int, &Body{Tail: vref(vsym("missing"), types.Int)}),
			want: "not in scope",
		},
		{
			name: "var bound twice",
			file: fnFile(types.Int, &Body{
				Lets: []*Let{
					{Sp: sp(), Name: bind(v, types.Int), X: intLit("1")},
					{Sp: sp(), Name: bind(v, types.Int), X: intLit("2")},
				},
				Tail: vref(v, types.Int),
			}),
			want: "bound twice",
		},
		{
			name: "var used before binding",
			file: func() *File {
				later, _ := vbind("later", types.Int)
				return fnFile(types.Int, &Body{
					Lets: []*Let{
						{Sp: sp(), Name: bind(v, types.Int), X: vref(later.Sym, types.Int)},
						{Sp: sp(), Name: later, X: intLit("1")},
					},
					Tail: vref(v, types.Int),
				})
			}(),
			want: "not in scope",
		},
		{
			name: "CallDirect not saturated",
			file: fnFile(types.Int, &Body{Tail: &CallDirect{
				Sp: sp(), Typ: types.Int,
				Callee: fnVal, Args: []Atom{intLit("1")}, Arity: 2,
			}}),
			want: "not saturated",
		},
		{
			name: "CallDirect to Go callee",
			file: fnFile(types.Int, &Body{Tail: &CallDirect{
				Sp: sp(), Typ: types.Int,
				Callee: goFn, Args: []Atom{intLit("1")}, Arity: 1,
			}}),
			want: "not a Gom function",
		},
		{
			name: "CallFunc supplies more than one argument",
			file: func() *File {
				fT := types.Fn(types.Int, types.P(types.Int), types.P(types.Int))
				uB, _ := vbind("u", types.Unit)
				fB, fS := vbind("f", fT)
				return &File{Sp: sp(), Package: "p", PkgSp: sp(),
					Decls: []Decl{&FuncDecl{Sp: sp(), Sym: fsym("g"), Fn: &Func{
						Sp:     sp(),
						Sig:    types.Fn(types.Int, types.P(types.Unit), types.P(fT)),
						Params: []*Bind{uB, fB},
						Body: &Body{Tail: &CallFunc{
							Sp: sp(), Typ: types.Int,
							Fn:   vref(fS, fT),
							Args: []Atom{intLit("1"), intLit("2")},
						}},
					}}}}
			}(),
			want: "exactly one",
		},
		{
			name: "CallFunc on a known callee",
			file: fnFile(types.Int, &Body{Tail: &CallFunc{
				Sp: sp(), Typ: types.Int,
				Fn: &FuncRef{
					Sp: sp(), Typ: types.Fn(types.Int, types.P(types.Int)),
					Sym: fnVal, Arity: 1,
				},
				Args: []Atom{intLit("1")},
			}}),
			want: "must be CallDirect",
		},
		{
			name: "bare multi-result Go call",
			file: fnFile(types.Int, &Body{Tail: &CallGo{
				Sp: sp(), Typ: types.Int,
				Callee: goPair, Args: []Atom{intLit("1")}, Arity: 1,
				Results: GoResultsErr,
			}}),
			want: "must be wrapped",
		},
		{
			name: "CallGo wrong arity",
			file: fnFile(types.Int, &Body{Tail: &CallGo{
				Sp: sp(), Typ: types.Int,
				Callee: goFn, Args: nil, Arity: 1,
				Results: GoResultsOne,
			}}),
			want: "Go arity",
		},
		{
			name: "IsEntry outside package main",
			file: entryFile("p", "main", types.Unit, true),
			want: "IsEntry requires `fn main` in package main",
		},
		{
			name: "IsEntry on a non-main name",
			file: entryFile("main", "start", types.Unit, true),
			want: "IsEntry requires `fn main` in package main",
		},
		{
			name: "IsEntry binds a named param",
			file: entryFile("main", "main", types.Unit, false),
			want: "binds a name",
		},
		{
			name: "IsEntry non-Unit result",
			file: entryFile("main", "main", types.Int, true),
			want: "entry result is Int, want Unit",
		},
		{
			name: "CallGo spread on non-variadic",
			file: fnFile(types.Int, &Body{Tail: &CallGo{
				Sp: sp(), Typ: types.Int,
				Callee: goFn, Args: []Atom{intLit("1")}, Arity: 1,
				Spread: true, Results: GoResultsOne,
			}}),
			want: "spread",
		},
		{
			name: "sugared operator survived",
			file: fnFile(types.Int, &Body{Tail: &Binary{
				Sp: sp(), Typ: types.Int,
				Op: syntax.BinPipeFwd,
				X:  intLit("1"), Y: intLit("2"),
			}}),
			want: "survived lowering",
		},
		{
			name: "if condition not Bool",
			file: fnFile(types.Int, &Body{Tail: &If{
				Sp: sp(), Typ: types.Int,
				Cond: intLit("1"),
				Then: &Body{Tail: intLit("1")},
				Else: &Body{Tail: intLit("2")},
			}}),
			want: "not Bool",
		},
		{
			name: "else-less if non-Unit",
			file: fnFile(types.Int, &Body{Tail: &If{
				Sp: sp(), Typ: types.Int,
				Cond: &Lit{Sp: sp(), Typ: types.Bool, Kind: syntax.LitBool, Text: "true"},
				Then: &Body{Tail: intLit("1")},
			}}),
			want: "non-Unit then",
		},
		{
			name: "type switch default is not DPanic",
			file: func() *File {
				sB, sS := vbind("s", shapeT)
				return fnFile(types.Int, &Body{
					Lets: []*Let{{Sp: sp(), Name: sB, X: &Var{
						Sp: sp(), Typ: shapeT, Sym: vsym("w"),
					}}},
					Tail: &Match{
						Sp: sp(), Typ: types.Int,
						Tree: &DSwitch{
							Sp: sp(), Typ: types.Int,
							Subj: vref(sS, shapeT), Kind: SwitchType,
							Cases: []DCase{{
								Sp:   sp(),
								Test: &DCtorTest{Sp: sp(), Ctor: shapeCt},
								Body: &Body{Tail: intLit("0")},
							}},
							Default: &DLeaf{
								Sp: sp(), Typ: types.Int,
								Body: &Body{Tail: intLit("9")},
							},
						},
					},
				})
			}(),
			want: "want DPanic",
		},
		{
			name: "literal test in type switch",
			file: fnFile(types.Int, &Body{Tail: &Match{
				Sp: sp(), Typ: types.Int,
				Tree: &DSwitch{
					Sp: sp(), Typ: types.Int,
					Subj: vref(v, shapeT), Kind: SwitchType,
					Cases: []DCase{{
						Sp:   sp(),
						Test: &DLitTest{Sp: sp(), Kind: syntax.LitInt, Text: "0"},
						Body: &Body{Tail: intLit("0")},
					}},
					Default: &DPanic{
						Sp: sp(), Typ: types.Int,
						Subj: vref(v, shapeT), TypeName: "p.Shape",
					},
				},
			}}),
			want: "literal test in a type switch",
		},
		{
			name: "escape outside a function",
			file: &File{
				Sp: sp(), Package: "p", PkgSp: sp(),
				Decls: []Decl{&VarDecl{
					Sp: sp(), Sym: vsym("top"),
					Body: &Body{Tail: &EscapeResult{
						Sp: sp(), Typ: types.Int,
						X: vref(v, resIG), To: nil,
					}},
				}},
			},
			want: "outside any function",
		},
		{
			name: "escape target not innermost",
			file: func() *File {
				f := fnFile(resIG, &Body{Tail: &If{
					Sp: sp(), Typ: resIG,
					Cond: &Lit{Sp: sp(), Typ: types.Bool, Kind: syntax.LitBool, Text: "true"},
					Then: &Body{Tail: &EscapeResult{
						Sp: sp(), Typ: types.Int,
						X:  vref(v, resIG),
						To: &Func{Sp: sp(), Sig: fnSS}, // some other function
					}},
					Else: &Body{Tail: &CtorApp{
						Sp: sp(), Typ: resIG, Ctor: okCtor,
						Args: []Atom{intLit("0")}, Arity: 1,
					}},
				}})
				return f
			}(),
			want: "not the innermost enclosing function",
		},
		{
			name: "escape boundary not Result",
			file: fnFile(types.Int, &Body{Tail: &EscapeResult{
				Sp: sp(), Typ: types.Int,
				X: vref(v, resIG), To: fnOf(fnFile(types.Int, &Body{Tail: intLit("0")})),
			}}),
			want: "not the innermost",
		},
		{
			name: "escape to a non-Result boundary",
			file: func() *File {
				f := fnFile(types.Int, &Body{Tail: intLit("0")})
				f.Decls[0].(*FuncDecl).Fn.Body.Tail = &EscapeResult{
					Sp: sp(), Typ: types.Int,
					X: vref(v, resIG), To: fnOf(f),
				}
				return f
			}(),
			want: "not a Result",
		},
		{
			name: "wrap with non-GoError step",
			file: func() *File {
				resTx := types.Result(types.Int, &types.Con{Name: "TxError"})
				f := fnFile(resTx, &Body{Tail: &EscapeResult{
					Sp: sp(), Typ: types.Int,
					X: vref(v, resTx),
				}})
				f.Decls[0].(*FuncDecl).Fn.Body.Tail.(*EscapeResult).To = fnOf(f)
				f.Decls[0].(*FuncDecl).Fn.Body.Tail.(*EscapeResult).Wrap = "step"
				return f
			}(),
			want: "only GoError",
		},
		{
			name: "escape inside go block",
			file: func() *File {
				f := fnFile(resIG, &Body{Tail: &Spawn{
					Sp: sp(), Typ: types.Unit,
					Body: &Body{Tail: &EscapeResult{
						Sp: sp(), Typ: types.Int,
						X: vref(v, resIG),
					}},
				}})
				f.Decls[0].(*FuncDecl).Fn.Body.Tail.(*Spawn).Body.Tail.(*EscapeResult).To = fnOf(f)
				return f
			}(),
			want: "go{} boundary",
		},
		{
			name: "escapeCall with single-result call",
			file: func() *File {
				f := fnFile(resIG, &Body{Tail: &EscapeCall{
					Sp: sp(), Typ: types.Int,
					Call: &CallGo{
						Sp: sp(), Typ: types.Int,
						Callee: goFn, Args: []Atom{intLit("1")}, Arity: 1,
						Results: GoResultsOne,
					},
				}})
				f.Decls[0].(*FuncDecl).Fn.Body.Tail.(*EscapeCall).To = fnOf(f)
				return f
			}(),
			want: "not usable here",
		},
		{
			name: "ResultFromGo with ok pair",
			file: fnFile(resIG, &Body{Tail: &ResultFromGo{
				Sp: sp(), Typ: resIG,
				Call: &CallGo{
					Sp: sp(), Typ: types.Int,
					Callee: goPair, Args: []Atom{intLit("1")}, Arity: 1,
					Results: GoResultsOK,
				},
			}}),
			want: "not usable here",
		},
		{
			name: "tail call not in tail position",
			file: func() *File {
				f := fnFile(types.Int, &Body{
					Lets: []*Let{{Sp: sp(), Name: bind(v, types.Int), X: &TailCall{
						Sp: sp(), Typ: types.Int,
						Args: []Atom{unitLit()},
					}}},
					Tail: vref(v, types.Int),
				})
				fn := fnOf(f)
				fn.TailLoop = true
				f.Decls[0].(*FuncDecl).Fn.Body.Lets[0].X.(*TailCall).To = fn
				return f
			}(),
			want: "not in tail position",
		},
		{
			name: "tail call without TailLoop",
			file: func() *File {
				f := fnFile(types.Int, &Body{Tail: &TailCall{
					Sp: sp(), Typ: types.Int,
					Args: []Atom{unitLit()},
				}})
				f.Decls[0].(*FuncDecl).Fn.Body.Tail.(*TailCall).To = fnOf(f)
				return f
			}(),
			want: "lacks TailLoop",
		},
		{
			name: "select with two defaults",
			file: fnFile(types.Int, &Body{Tail: &SelectExpr{
				Sp: sp(), Typ: types.Int,
				Arms: []SelArm{
					&SelDefault{Sp: sp(), Body: &Body{Tail: intLit("0")}},
					&SelDefault{Sp: sp(), Body: &Body{Tail: intLit("1")}},
				},
			}}),
			want: "default arms",
		},
		{
			name: "recv bind not Option",
			file: func() *File {
				ch := vsym("ch")
				uB, _ := vbind("u", types.Unit)
				badB, _ := vbind("r", types.Int) // should be Option Int
				fn := &Func{
					Sp:     sp(),
					Sig:    types.Fn(types.Int, types.P(types.Unit), types.P(types.Chan(types.Int))),
					Params: []*Bind{uB, bind(ch, types.Chan(types.Int))},
					Body: &Body{Tail: &SelectExpr{
						Sp: sp(), Typ: types.Int,
						Arms: []SelArm{&SelRecv{
							Sp:   sp(),
							Chan: vref(ch, types.Chan(types.Int)),
							Bind: badB,
							Body: &Body{Tail: intLit("0")},
						}},
					}},
				}
				return &File{Sp: sp(), Package: "p", PkgSp: sp(),
					Decls: []Decl{&FuncDecl{Sp: sp(), Sym: fsym("f"), Fn: fn}}}
			}(),
			want: "not an Option",
		},
		{
			name: "go block non-Unit",
			file: fnFile(types.Unit, &Body{Tail: &Spawn{
				Sp: sp(), Typ: types.Unit,
				Body: &Body{Tail: intLit("1")},
			}}),
			want: "not Unit",
		},
		{
			name: "for-chan over a list",
			file: func() *File {
				l, _ := vbind("l", types.List(types.Int))
				return fnFile(types.Unit, &Body{
					Lets: []*Let{{Sp: sp(), Name: l, X: &ListNew{
						Sp: sp(), Typ: types.List(types.Int), Elems: []Atom{intLit("1")},
					}}},
					Tail: &ForRange{
						Sp: sp(), Typ: types.Unit,
						Kind: ForChan, Iter: vref(l.Sym, types.List(types.Int)),
						Elem: &Bind{Sp: sp(), Typ: types.Int, Discard: true},
						Body: &Body{Tail: unitLit()},
					},
				})
			}(),
			want: "not Chan/RecvChan",
		},
		{
			name: "conv slice-to-go wrong operand",
			file: fnFile(types.GoSlice(types.Int), &Body{Tail: &Conv{
				Sp: sp(), Typ: types.GoSlice(types.Int),
				Kind: ConvSliceToGo,
				X:    intLit("1"),
			}}),
			want: "not a List",
		},
		{
			name: "declared arity exceeds flat arity",
			file: fnFile(types.Int, &Body{Tail: &Closure{
				Sp: sp(), Typ: fnII,
				Fn: &Func{
					Sp:  sp(),
					Sig: fnII, // 1 signature param
					Params: []*Bind{
						bind(vsym("b"), types.Int),
						bind(vsym("c"), types.Int), // 2 binders — more than the arrow length
					},
					Body: &Body{Tail: intLit("0")},
				},
			}}),
			want: "exceeds",
		},
		{
			name: "GoFuncRef needing adapter used directly",
			file: fnFile(types.Int, &Body{Tail: &CallFunc{
				Sp: sp(), Typ: types.Int,
				Fn: &GoFuncRef{
					Sp: sp(), Typ: types.Fn(types.Int, types.P(types.Int)),
					Sym: goFn, Arity: 2, Results: GoResultsErr, // needs adapter
				},
				Args: []Atom{intLit("1")},
			}}),
			want: "adapter Closure",
		},
		{
			name: "FuncRef arity mismatch",
			file: fnFile(fnII, &Body{Tail: &FuncRef{
				Sp: sp(), Typ: types.Fn(types.Int, types.P(types.Int), types.P(types.Int)),
				Sym: fnVal, Arity: 2, // declared arity 2 — the direct-call shape, not the value shape
			}}),
			want: "adapter Closure",
		},
		{
			name: "record field initialized twice",
			file: func() *File {
				radius := fdsym("radius")
				return fnFile(shapeT, &Body{Tail: &RecordNew{
					Sp: sp(), Typ: shapeT,
					Fields: []FieldInit{
						{Sp: sp(), Sym: radius, Index: 0, Value: intLit("1")},
						{Sp: sp(), Sym: radius, Index: 0, Value: intLit("2")},
					},
				}})
			}(),
			want: "initialized twice",
		},
		{
			name: "binder type differs from bound expression",
			file: fnFile(types.String, &Body{
				Lets: []*Let{{Sp: sp(), Name: bind(vsym("s"), types.Float), X: intLit("1")}},
				Tail: &Lit{Sp: sp(), Typ: types.String, Kind: syntax.LitStr, Text: "\"x\""},
			}),
			want: "binder type Float differs",
		},
		{
			name: "body type differs from residual type",
			file: &File{Sp: sp(), Package: "p", PkgSp: sp(), Decls: []Decl{&FuncDecl{
				Sp: sp(), Sym: fsym("f"),
				Fn: &Func{
					Sp:     sp(),
					Sig:    types.Fn(types.String, types.P(types.Unit)),
					Params: []*Bind{bind(vsym("u"), types.Unit)},
					Body:   &Body{Tail: intLit("1")},
				},
			}}},
			want: "differs from residual type",
		},
		{
			name: "tuple field index out of range",
			file: fnFile(types.Int, &Body{Tail: &CallFunc{
				Sp: sp(), Typ: types.Int,
				Fn: &FieldGet{
					Sp: sp(), Typ: fnII,
					Recv:  vref(v, types.Tup(types.Int, types.Int)),
					Index: 5,
				},
				Args: []Atom{intLit("1")},
			}}),
			want: "out of range",
		},
		{
			name: "escape in outer body of arity-split function",
			file: func() *File {
				// fn f (r: Result Int GoError) -> (Int -> Result Int GoError)
				// with `try r` in the outer body — the emitted result is
				// the residual `Int -> Result _ _`, not a Result.
				rB, rS := vbind("r", resIG)
				fn := &Func{
					Sp:     sp(),
					Sig:    types.Fn(resIG, types.P(resIG), types.P(types.Int)),
					Params: []*Bind{rB},
					Body: &Body{Tail: &EscapeResult{
						Sp: sp(), Typ: types.Int,
						X: vref(rS, resIG),
					}},
				}
				fn.Body.Tail.(*EscapeResult).To = fn
				return &File{Sp: sp(), Package: "p", PkgSp: sp(),
					Decls: []Decl{&FuncDecl{Sp: sp(), Sym: fsym("f"), Fn: fn}}}
			}(),
			want: "not a Result",
		},
		{
			name: "typed-nil atom",
			file: fnFile(types.Int, &Body{Tail: &CallDirect{
				Sp: sp(), Typ: types.Int,
				Callee: fnVal, Args: []Atom{(*Var)(nil)}, Arity: 1,
			}}),
			want: "missing atom",
		},
		{
			name: "typed-nil expression",
			file: fnFile(types.Int, &Body{Tail: (*If)(nil)}),
			want: "missing expression",
		},
		{
			name: "if branch missing",
			file: fnFile(types.Int, &Body{Tail: &If{
				Sp: sp(), Typ: types.Int,
				Cond: &Lit{Sp: sp(), Typ: types.Bool, Kind: syntax.LitBool, Text: "true"},
				Else: &Body{Tail: intLit("1")},
			}}),
			want: "missing body",
		},
		{
			name: "decision-tree case missing test",
			file: func() *File {
				nB, nS := vbind("n", types.Int)
				fn := &Func{
					Sp:     sp(),
					Sig:    fnII,
					Params: []*Bind{nB},
					Body: &Body{Tail: &Match{
						Sp: sp(), Typ: types.Int,
						Tree: &DSwitch{
							Sp: sp(), Typ: types.Int,
							Subj: vref(nS, types.Int), Kind: SwitchValue,
							Cases: []DCase{{
								Sp:   sp(),
								Test: nil,
								Body: &Body{Tail: intLit("0")},
							}},
							Default: &DLeaf{
								Sp: sp(), Typ: types.Int,
								Body: &Body{Tail: intLit("1")},
							},
						},
					}},
				}
				return &File{Sp: sp(), Package: "p", PkgSp: sp(),
					Decls: []Decl{&FuncDecl{Sp: sp(), Sym: fsym("f"), Fn: fn}}}
			}(),
			want: "missing test",
		},
		{
			name: "spawn node type not Unit",
			file: fnFile(types.Int, &Body{Tail: &Spawn{
				Sp: sp(), Typ: types.Int,
				Body: &Body{Tail: unitLit()},
			}}),
			want: "not Unit",
		},
		{
			name: "test decl with params",
			file: &File{Sp: sp(), Package: "p", PkgSp: sp(), Decls: []Decl{&TestDecl{
				Sp: sp(), Name: "bad", NameSp: sp(),
				Fn: &Func{
					Sp:     sp(),
					Sig:    types.Fn(types.Unit, types.P(types.Int)),
					Params: []*Bind{bind(vsym("x"), types.Int)},
					Body:   &Body{Tail: unitLit()},
				},
			}}},
			want: "want 0",
		},
		{
			name: "variant symbol bound twice",
			file: func() *File {
				dup := csym("Dup")
				return &File{Sp: sp(), Package: "p", PkgSp: sp(), Decls: []Decl{&TypeDecl{
					Sp: sp(), Sym: tsym("T"),
					Body: &TSum{Sp: sp(), Variants: []TVariant{
						{Sp: sp(), Sym: dup},
						{Sp: sp(), Sym: dup},
					}},
				}}}
			}(),
			want: "bound twice",
		},
		{
			name: "list switch subject not a List",
			file: func() *File {
				nB, nS := vbind("n", types.Int)
				fn := &Func{
					Sp:     sp(),
					Sig:    fnII,
					Params: []*Bind{nB},
					Body: &Body{Tail: &Match{
						Sp: sp(), Typ: types.Int,
						Tree: &DSwitch{
							Sp: sp(), Typ: types.Int,
							Subj: vref(nS, types.Int), Kind: SwitchList,
							Cases: []DCase{{
								Sp:   sp(),
								Test: &DListTest{Sp: sp(), Cons: false},
								Body: &Body{Tail: intLit("0")},
							}},
							Default: &DPanic{
								Sp: sp(), Typ: types.Int,
								Subj: vref(nS, types.Int), TypeName: "List",
							},
						},
					}},
				}
				return &File{Sp: sp(), Package: "p", PkgSp: sp(),
					Decls: []Decl{&FuncDecl{Sp: sp(), Sym: fsym("f"), Fn: fn}}}
			}(),
			want: "is not a List",
		},
		{
			name: "send arm channel not chan-typed",
			file: func() *File {
				nB, nS := vbind("n", types.Int)
				fn := &Func{
					Sp:     sp(),
					Sig:    types.Fn(types.Unit, types.P(types.Int)),
					Params: []*Bind{nB},
					Body: &Body{Tail: &SelectExpr{
						Sp: sp(), Typ: types.Unit,
						Arms: []SelArm{
							&SelSend{
								Sp: sp(), Chan: vref(nS, types.Int), Value: intLit("1"),
								Body: &Body{Tail: unitLit()},
							},
							&SelDefault{Sp: sp(), Body: &Body{Tail: unitLit()}},
						},
					}},
				}
				return &File{Sp: sp(), Package: "p", PkgSp: sp(),
					Decls: []Decl{&FuncDecl{Sp: sp(), Sym: fsym("f"), Fn: fn}}}
			}(),
			want: "not Chan/SendChan",
		},
		{
			name: "recv arm channel not chan-typed",
			file: func() *File {
				nB, nS := vbind("n", types.Int)
				oB, _ := vbind("o", types.Option(types.Int))
				fn := &Func{
					Sp:     sp(),
					Sig:    types.Fn(types.Unit, types.P(types.Int)),
					Params: []*Bind{nB},
					Body: &Body{Tail: &SelectExpr{
						Sp: sp(), Typ: types.Unit,
						Arms: []SelArm{
							&SelRecv{
								Sp: sp(), Chan: vref(nS, types.Int), Bind: oB,
								Body: &Body{Tail: unitLit()},
							},
							&SelDefault{Sp: sp(), Body: &Body{Tail: unitLit()}},
						},
					}},
				}
				return &File{Sp: sp(), Package: "p", PkgSp: sp(),
					Decls: []Decl{&FuncDecl{Sp: sp(), Sym: fsym("f"), Fn: fn}}}
			}(),
			want: "not Chan/RecvChan",
		},
		{
			name: "field get negative index",
			file: fnFile(types.Int, &Body{Tail: &CallFunc{
				Sp: sp(), Typ: types.Int,
				Fn: &FieldGet{
					Sp: sp(), Typ: fnII,
					Recv:  vref(v, types.Tup(types.Int, types.Int)),
					Index: -1,
				},
				Args: []Atom{intLit("1")},
			}}),
			want: "negative index",
		},
		{
			name: "guard subtree type differs from parent",
			file: func() *File {
				nB, nS := vbind("n", types.Int)
				fn := &Func{
					Sp:     sp(),
					Sig:    fnII,
					Params: []*Bind{nB},
					Body: &Body{Tail: &Match{
						Sp: sp(), Typ: types.Int,
						Tree: &DSwitch{
							Sp: sp(), Typ: types.Int,
							Subj: vref(nS, types.Int), Kind: SwitchValue,
							Cases: []DCase{{
								Sp:   sp(),
								Test: &DLitTest{Sp: sp(), Kind: syntax.LitInt, Text: "0"},
								Body: &Body{Tail: intLit("0")},
							}},
							Default: &DGuard{
								Sp: sp(), Typ: types.String, // != parent's Int
								Cond: &Lit{Sp: sp(), Typ: types.Bool, Kind: syntax.LitBool, Text: "true"},
								Then: &DLeaf{
									Sp: sp(), Typ: types.String,
									Body: &Body{Tail: &Lit{Sp: sp(), Typ: types.String, Kind: syntax.LitStr, Text: "\"x\""}},
								},
								Else: &DLeaf{
									Sp: sp(), Typ: types.String,
									Body: &Body{Tail: &Lit{Sp: sp(), Typ: types.String, Kind: syntax.LitStr, Text: "\"y\""}},
								},
							},
						},
					}},
				}
				return &File{Sp: sp(), Package: "p", PkgSp: sp(),
					Decls: []Decl{&FuncDecl{Sp: sp(), Sym: fsym("f"), Fn: fn}}}
			}(),
			want: "subtree type",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Validate(c.file)
			if len(got) == 0 {
				t.Fatalf("expected a violation containing %q, got none", c.want)
			}
			for _, v := range got {
				if strings.Contains(v.Msg, c.want) || strings.Contains(v.String(), c.want) {
					return
				}
			}
			var b strings.Builder
			for _, v := range got {
				b.WriteString("\n  " + v.String())
			}
			t.Fatalf("no violation contains %q; got:%s", c.want, b.String())
		})
	}
}

// TestValidateEquality pins the plan invariants of the equality
// nodes: an Equal carries leaf recipes only, a composite recipe is
// legal solely at an EqDecl's root, and assert_eq must carry its
// precomputed plan.
func TestValidateEquality(t *testing.T) {
	listT := types.List(types.Int)
	xb, xs := vbind("x", types.Int)
	yb, ys := vbind("y", types.Int)
	lb, ls := vbind("xs", listT)
	mb, ms := vbind("ys", listT)

	params := []*Bind{xb, yb, lb, mb}
	eqFn := func(tail Expr) *Func {
		return &Func{
			Sp:     sp(),
			Sig:    types.Fn(types.Bool, types.P(types.Int), types.P(types.Int), types.P(listT), types.P(listT)),
			Params: params,
			Body:   &Body{Tail: tail},
		}
	}
	eqFile := func(tail Expr, eqs []*EqDecl) *File {
		return &File{Sp: sp(), Package: "p", PkgSp: sp(),
			Decls:   []Decl{&FuncDecl{Sp: sp(), Sym: fsym("f"), Fn: eqFn(tail)}},
			EqDecls: eqs}
	}
	native := &EqPlan{Kind: EqNative}
	cmpDecl := &EqDecl{
		Sp:   sp(),
		Sym:  fsym("gomEq1"),
		Typ:  listT,
		Plan: &EqPlan{Kind: EqSlice, Elem: native},
	}
	call := &EqPlan{Kind: EqCall, Fn: cmpDecl}

	valid := &File{Sp: sp(), Package: "p", PkgSp: sp(),
		Decls: []Decl{&FuncDecl{Sp: sp(), Sym: fsym("f"), Fn: eqFn(&Equal{
			Sp: sp(), Typ: types.Bool,
			X: vref(ls, listT), Y: vref(ms, listT),
			How: call,
		})}},
		EqDecls: []*EqDecl{cmpDecl}}
	if got := Validate(valid); len(got) != 0 {
		for _, v := range got {
			t.Errorf("valid equality file: unexpected violation: %s", v)
		}
	}

	cases := []struct {
		name string
		file *File
		want string
	}{
		{
			name: "composite recipe on Equal",
			file: eqFile(&Equal{
				Sp: sp(), Typ: types.Bool,
				X: vref(ls, listT), Y: vref(ms, listT),
				How: &EqPlan{Kind: EqSlice, Elem: native},
			}, nil),
			want: "legal only at a comparator's root",
		},
		{
			name: "missing recipe",
			file: eqFile(&Equal{
				Sp: sp(), Typ: types.Bool,
				X: vref(xs, types.Int), Y: vref(ys, types.Int),
			}, nil),
			want: "missing comparison recipe",
		},
		{
			name: "comparator without plan",
			file: eqFile(&Equal{
				Sp: sp(), Typ: types.Bool,
				X: vref(xs, types.Int), Y: vref(ys, types.Int),
				How: native,
			}, []*EqDecl{{Sp: sp(), Sym: fsym("gomEq9"), Typ: listT}}),
			want: "missing comparison recipe",
		},
		{
			name: "comparator symbol of the wrong kind",
			file: eqFile(&Equal{
				Sp: sp(), Typ: types.Bool,
				X: vref(xs, types.Int), Y: vref(ys, types.Int),
				How: native,
			}, []*EqDecl{{Sp: sp(), Sym: vsym("gomEq9"), Typ: listT,
				Plan: &EqPlan{Kind: EqSlice, Elem: native}}}),
			want: "want func",
		},
		{
			name: "assert_eq without a plan",
			file: eqFile(&CallBuiltin{
				Sp: sp(), Typ: types.Unit,
				Sym:  &resolve.Symbol{Name: "assert_eq", Kind: resolve.KindBuiltin},
				Args: []Atom{vref(ls, listT), vref(ms, listT)},
			}, nil),
			want: "assert_eq without an Eq plan",
		},
		{
			name: "plan on a non-assert_eq builtin",
			file: eqFile(&CallBuiltin{
				Sp: sp(), Typ: types.Unit,
				Sym:  bsym(),
				Args: []Atom{vref(ls, listT), vref(ms, listT)},
				Eq:   native,
			}, nil),
			want: "non-assert_eq",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Validate(c.file)
			for _, v := range got {
				if strings.Contains(v.Msg, c.want) || strings.Contains(v.String(), c.want) {
					return
				}
			}
			t.Fatalf("no violation contains %q; got %v", c.want, got)
		})
	}
}

func TestViolationImplementsError(t *testing.T) {
	var err error = Violation{Span: diag.Point("a.gom", 3), Node: "If", Msg: "cond is not Bool"}
	if err.Error() == "" {
		t.Error("empty error string")
	}
}

// TestErrorFromGoInvariants pins the never-nil view's mandatory
// consumer: a `GoResultsErrValue` call may not sit bare anywhere,
// ErrorFromGo admits only that shape, and its node type must be the
// builtin GoError (a file-local `type GoError` cannot stand in).
func TestErrorFromGoInvariants(t *testing.T) {
	errCall := func(shape GoResults) *CallGo {
		return &CallGo{
			Sp: sp(), Typ: types.GoError,
			Callee:  gsym("New", "errors"),
			Args:    []Atom{intLit("1")},
			Arity:   1,
			Results: shape,
		}
	}
	errFromGo := func(shape GoResults, typ types.Type) *ErrorFromGo {
		return &ErrorFromGo{
			Sp: sp(), Typ: typ,
			Call: errCall(shape),
		}
	}
	localGoError := &TypeDecl{
		Sp: sp(), Sym: &resolve.Symbol{Name: "GoError", Kind: resolve.KindType},
		Body: &TAlias{Sp: sp(), Type: types.Int},
	}
	tests := []struct {
		name string
		file *File
		want string
	}{
		{"bare err-value call", fnFile(types.GoError, &Body{Tail: errCall(GoResultsErrValue)}),
			"must be wrapped by ResultFromGo/OptionFromGo/TupleFromGo/ErrorFromGo/EscapeCall"},
		{"err-only shape under ErrorFromGo", fnFile(types.GoError, &Body{Tail: errFromGo(GoResultsErrOnly, types.GoError)}),
			"result shape err-only not usable here"},
		{"wrong node type", fnFile(types.Int, &Body{Tail: errFromGo(GoResultsErrValue, types.Int)}),
			"is not GoError"},
		{"local GoError type is not the builtin", func() *File {
			f := fnFile(types.GoError, &Body{Tail: errFromGo(GoResultsErrValue,
				&types.Con{Name: "GoError"})})
			f.Decls = append([]Decl{localGoError}, f.Decls...)
			return f
		}(), "is not GoError"},
		{"valid err-value call", fnFile(types.GoError, &Body{Tail: errFromGo(GoResultsErrValue, types.GoError)}),
			""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := Validate(tc.file)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			for _, err := range errs {
				if strings.Contains(err.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("errors %v do not contain %q", errs, tc.want)
		})
	}
}

func TestZeroResultCallPositions(t *testing.T) {
	makeCall := func(typ types.Type) *CallGo {
		return &CallGo{Sp: sp(), Typ: typ, Callee: &resolve.Symbol{
			Name: "Effect", Kind: resolve.KindFunc, IsGo: true, Package: "example",
		}, Results: GoResultsNone}
	}
	discard := func(typ types.Type) *Bind { return &Bind{Sp: sp(), Typ: typ, Discard: true} }
	bound, _ := vbind("value", types.Unit)
	tests := []struct {
		name string
		body *Body
		want string
	}{
		{"discard", &Body{Lets: []*Let{{Sp: sp(), Name: discard(types.Unit), X: makeCall(types.Unit)}}, Tail: unitLit()}, ""},
		{"tail", &Body{Tail: makeCall(types.Unit)}, "must be a discarded binding"},
		{"bound", &Body{Lets: []*Let{{Sp: sp(), Name: bound, X: makeCall(types.Unit)}}, Tail: unitLit()}, "must be a discarded binding"},
		{"wrong type", &Body{Lets: []*Let{{Sp: sp(), Name: discard(types.Int), X: makeCall(types.Int)}}, Tail: unitLit()}, "must have Unit type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := Validate(fnFile(types.Unit, tc.body))
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			for _, err := range errs {
				if strings.Contains(err.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("errors %v do not contain %q", errs, tc.want)
		})
	}
}
