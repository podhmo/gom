package core

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
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

func TestValidateAcceptsValidFile(t *testing.T) {
	if got := Validate(okFile); len(got) != 0 {
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
					Base: Base{Sp: sp(), Typ: types.Int},
					X:    vref(rS, resIG),
				}},
				{Sp: sp(), Name: sumB, X: &Binary{
					Base: Base{Sp: sp(), Typ: types.Int},
					Op:   syntax.BinAdd, X: vref(nS, types.Int), Y: vref(bS, types.Int),
				}},
			},
			Tail: &CtorApp{
				Base: Base{Sp: sp(), Typ: resIG},
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
				Base: Base{Sp: sp(), Typ: types.Fn(resIG, types.P(types.Int))},
				Fn:   inner,
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
				Base: Base{Sp: sp(), Typ: types.Int},
				X:    vref(srcS, resIG),
			}}},
			Tail: &CtorApp{
				Base: Base{Sp: sp(), Typ: resIG},
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
				Base: Base{Sp: sp(), Typ: types.Unit},
				Body: &Body{
					Lets: []*Let{
						{Sp: sp(), Name: gB, X: &Closure{
							Base: Base{Sp: sp(), Typ: types.Fn(resIG, types.P(types.Unit))},
							Fn:   gfn,
						}},
						{Sp: sp(), Name: &Bind{Sp: sp(), Typ: resIG, Discard: true}, X: &CallFunc{
							Base: Base{Sp: sp(), Typ: resIG},
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
				Base: Base{Typ: types.Unit}, Kind: syntax.LitUnit, Text: "()"}}),
			want: "no valid source span",
		},
		{
			name: "node without type",
			file: fnFile(types.Unit, &Body{Tail: &Lit{
				Base: Base{Sp: sp()}, Kind: syntax.LitUnit, Text: "()"}}),
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
				Base:   Base{Sp: sp(), Typ: types.Int},
				Callee: fnVal, Args: []Atom{intLit("1")}, Arity: 2,
			}}),
			want: "not saturated",
		},
		{
			name: "CallDirect to Go callee",
			file: fnFile(types.Int, &Body{Tail: &CallDirect{
				Base:   Base{Sp: sp(), Typ: types.Int},
				Callee: goFn, Args: []Atom{intLit("1")}, Arity: 1,
			}}),
			want: "not a Gom function",
		},
		{
			name: "CallFunc partial application",
			file: func() *File {
				fb, fs := vbind("f", types.Fn(types.Int, types.P(types.Int), types.P(types.Int)))
				return fnFile(types.Int, &Body{
					Lets: []*Let{
						{Sp: sp(), Name: fb, X: &FuncRef{
							Base: Base{Sp: sp(), Typ: types.Fn(types.Int, types.P(types.Int), types.P(types.Int))},
							Sym:  fnVal, Arity: 2,
						}},
					},
					Tail: &CallFunc{
						Base: Base{Sp: sp(), Typ: types.Int},
						Fn:   vref(fs, types.Fn(types.Int, types.P(types.Int), types.P(types.Int))),
						Args: []Atom{intLit("1")},
					},
				})
			}(),
			want: "must be a Closure",
		},
		{
			name: "CallFunc on a known callee",
			file: fnFile(types.Int, &Body{Tail: &CallFunc{
				Base: Base{Sp: sp(), Typ: types.Int},
				Fn: &FuncRef{
					Base: Base{Sp: sp(), Typ: types.Fn(types.Int, types.P(types.Int))},
					Sym:  fnVal, Arity: 1,
				},
				Args: []Atom{intLit("1")},
			}}),
			want: "must be CallDirect",
		},
		{
			name: "bare multi-result Go call",
			file: fnFile(types.Int, &Body{Tail: &CallGo{
				Base:   Base{Sp: sp(), Typ: types.Int},
				Callee: goPair, Args: []Atom{intLit("1")}, Arity: 1,
				Results: GoResultsErr,
			}}),
			want: "must be wrapped",
		},
		{
			name: "CallGo wrong arity",
			file: fnFile(types.Int, &Body{Tail: &CallGo{
				Base:   Base{Sp: sp(), Typ: types.Int},
				Callee: goFn, Args: nil, Arity: 1,
				Results: GoResultsOne,
			}}),
			want: "Go arity",
		},
		{
			name: "CallGo spread on non-variadic",
			file: fnFile(types.Int, &Body{Tail: &CallGo{
				Base:   Base{Sp: sp(), Typ: types.Int},
				Callee: goFn, Args: []Atom{intLit("1")}, Arity: 1,
				Spread: true, Results: GoResultsOne,
			}}),
			want: "spread",
		},
		{
			name: "sugared operator survived",
			file: fnFile(types.Int, &Body{Tail: &Binary{
				Base: Base{Sp: sp(), Typ: types.Int},
				Op:   syntax.BinPipeFwd,
				X:    intLit("1"), Y: intLit("2"),
			}}),
			want: "survived lowering",
		},
		{
			name: "if condition not Bool",
			file: fnFile(types.Int, &Body{Tail: &If{
				Base: Base{Sp: sp(), Typ: types.Int},
				Cond: intLit("1"),
				Then: &Body{Tail: intLit("1")},
				Else: &Body{Tail: intLit("2")},
			}}),
			want: "not Bool",
		},
		{
			name: "else-less if non-Unit",
			file: fnFile(types.Int, &Body{Tail: &If{
				Base: Base{Sp: sp(), Typ: types.Int},
				Cond: &Lit{Base: Base{Sp: sp(), Typ: types.Bool}, Kind: syntax.LitBool, Text: "true"},
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
						Base: Base{Sp: sp(), Typ: shapeT}, Sym: vsym("w"),
					}}},
					Tail: &Match{
						Base: Base{Sp: sp(), Typ: types.Int},
						Tree: &DSwitch{
							Base: Base{Sp: sp(), Typ: types.Int},
							Subj: vref(sS, shapeT), Kind: SwitchType,
							Cases: []DCase{{
								Sp:   sp(),
								Test: &DCtorTest{Sp: sp(), Ctor: shapeCt},
								Body: &Body{Tail: intLit("0")},
							}},
							Default: &DLeaf{
								Base: Base{Sp: sp(), Typ: types.Int},
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
				Base: Base{Sp: sp(), Typ: types.Int},
				Tree: &DSwitch{
					Base: Base{Sp: sp(), Typ: types.Int},
					Subj: vref(v, shapeT), Kind: SwitchType,
					Cases: []DCase{{
						Sp:   sp(),
						Test: &DLitTest{Sp: sp(), Kind: syntax.LitInt, Text: "0"},
						Body: &Body{Tail: intLit("0")},
					}},
					Default: &DPanic{
						Base: Base{Sp: sp(), Typ: types.Int},
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
						Base: Base{Sp: sp(), Typ: types.Int},
						X:    vref(v, resIG), To: nil,
					}},
				}},
			},
			want: "outside any function",
		},
		{
			name: "escape target not innermost",
			file: func() *File {
				f := fnFile(resIG, &Body{Tail: &If{
					Base: Base{Sp: sp(), Typ: resIG},
					Cond: &Lit{Base: Base{Sp: sp(), Typ: types.Bool}, Kind: syntax.LitBool, Text: "true"},
					Then: &Body{Tail: &EscapeResult{
						Base: Base{Sp: sp(), Typ: types.Int},
						X:    vref(v, resIG),
						To:   &Func{Sp: sp(), Sig: fnSS}, // some other function
					}},
					Else: &Body{Tail: &CtorApp{
						Base: Base{Sp: sp(), Typ: resIG}, Ctor: okCtor,
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
				Base: Base{Sp: sp(), Typ: types.Int},
				X:    vref(v, resIG), To: fnOf(fnFile(types.Int, &Body{Tail: intLit("0")})),
			}}),
			want: "not the innermost",
		},
		{
			name: "escape to a non-Result boundary",
			file: func() *File {
				f := fnFile(types.Int, &Body{Tail: intLit("0")})
				f.Decls[0].(*FuncDecl).Fn.Body.Tail = &EscapeResult{
					Base: Base{Sp: sp(), Typ: types.Int},
					X:    vref(v, resIG), To: fnOf(f),
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
					Base: Base{Sp: sp(), Typ: types.Int},
					X:    vref(v, resTx),
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
					Base: Base{Sp: sp(), Typ: types.Unit},
					Body: &Body{Tail: &EscapeResult{
						Base: Base{Sp: sp(), Typ: types.Int},
						X:    vref(v, resIG),
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
					Base: Base{Sp: sp(), Typ: types.Int},
					Call: &CallGo{
						Base:   Base{Sp: sp(), Typ: types.Int},
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
				Base: Base{Sp: sp(), Typ: resIG},
				Call: &CallGo{
					Base:   Base{Sp: sp(), Typ: types.Int},
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
						Base: Base{Sp: sp(), Typ: types.Int},
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
					Base: Base{Sp: sp(), Typ: types.Int},
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
				Base: Base{Sp: sp(), Typ: types.Int},
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
						Base: Base{Sp: sp(), Typ: types.Int},
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
				Base: Base{Sp: sp(), Typ: types.Unit},
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
						Base: Base{Sp: sp(), Typ: types.List(types.Int)}, Elems: []Atom{intLit("1")},
					}}},
					Tail: &ForRange{
						Base: Base{Sp: sp(), Typ: types.Unit},
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
				Base: Base{Sp: sp(), Typ: types.GoSlice(types.Int)},
				Kind: ConvSliceToGo,
				X:    intLit("1"),
			}}),
			want: "not a List",
		},
		{
			name: "declared arity exceeds flat arity",
			file: fnFile(types.Int, &Body{Tail: &Closure{
				Base: Base{Sp: sp(), Typ: fnII},
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
				Base: Base{Sp: sp(), Typ: types.Int},
				Fn: &GoFuncRef{
					Base: Base{Sp: sp(), Typ: types.Fn(types.Int, types.P(types.Int))},
					Sym:  goFn, Arity: 2, Results: GoResultsErr, // needs adapter
				},
				Args: []Atom{intLit("1")},
			}}),
			want: "adapter Closure",
		},
		{
			name: "FuncRef arity mismatch",
			file: fnFile(fnII, &Body{Tail: &FuncRef{
				Base: Base{Sp: sp(), Typ: types.Fn(types.Int, types.P(types.Int))},
				Sym:  fnVal, Arity: 2, // declared arity 2 but type arity 1 — needs flatten Closure
			}}),
			want: "flatten Closure",
		},
		{
			name: "record field initialized twice",
			file: func() *File {
				radius := fdsym("radius")
				return fnFile(shapeT, &Body{Tail: &RecordNew{
					Base: Base{Sp: sp(), Typ: shapeT},
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
				Tail: &Lit{Base: Base{Sp: sp(), Typ: types.String}, Kind: syntax.LitStr, Text: "\"x\""},
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
				Base: Base{Sp: sp(), Typ: types.Int},
				Fn: &FieldGet{
					Base:  Base{Sp: sp(), Typ: fnII},
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
						Base: Base{Sp: sp(), Typ: types.Int},
						X:    vref(rS, resIG),
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
				Base:   Base{Sp: sp(), Typ: types.Int},
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
				Base: Base{Sp: sp(), Typ: types.Int},
				Cond: &Lit{Base: Base{Sp: sp(), Typ: types.Bool}, Kind: syntax.LitBool, Text: "true"},
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
						Base: Base{Sp: sp(), Typ: types.Int},
						Tree: &DSwitch{
							Base: Base{Sp: sp(), Typ: types.Int},
							Subj: vref(nS, types.Int), Kind: SwitchValue,
							Cases: []DCase{{
								Sp:   sp(),
								Test: nil,
								Body: &Body{Tail: intLit("0")},
							}},
							Default: &DLeaf{
								Base: Base{Sp: sp(), Typ: types.Int},
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
				Base: Base{Sp: sp(), Typ: types.Int},
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
						Base: Base{Sp: sp(), Typ: types.Int},
						Tree: &DSwitch{
							Base: Base{Sp: sp(), Typ: types.Int},
							Subj: vref(nS, types.Int), Kind: SwitchList,
							Cases: []DCase{{
								Sp:   sp(),
								Test: &DListTest{Sp: sp(), Cons: false},
								Body: &Body{Tail: intLit("0")},
							}},
							Default: &DPanic{
								Base: Base{Sp: sp(), Typ: types.Int},
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
						Base: Base{Sp: sp(), Typ: types.Unit},
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
						Base: Base{Sp: sp(), Typ: types.Unit},
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
				Base: Base{Sp: sp(), Typ: types.Int},
				Fn: &FieldGet{
					Base:  Base{Sp: sp(), Typ: fnII},
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
						Base: Base{Sp: sp(), Typ: types.Int},
						Tree: &DSwitch{
							Base: Base{Sp: sp(), Typ: types.Int},
							Subj: vref(nS, types.Int), Kind: SwitchValue,
							Cases: []DCase{{
								Sp:   sp(),
								Test: &DLitTest{Sp: sp(), Kind: syntax.LitInt, Text: "0"},
								Body: &Body{Tail: intLit("0")},
							}},
							Default: &DGuard{
								Base: Base{Sp: sp(), Typ: types.String}, // != parent's Int
								Cond: &Lit{Base: Base{Sp: sp(), Typ: types.Bool}, Kind: syntax.LitBool, Text: "true"},
								Then: &DLeaf{
									Base: Base{Sp: sp(), Typ: types.String},
									Body: &Body{Tail: &Lit{Base: Base{Sp: sp(), Typ: types.String}, Kind: syntax.LitStr, Text: "\"x\""}},
								},
								Else: &DLeaf{
									Base: Base{Sp: sp(), Typ: types.String},
									Body: &Body{Tail: &Lit{Base: Base{Sp: sp(), Typ: types.String}, Kind: syntax.LitStr, Text: "\"y\""}},
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

func TestViolationImplementsError(t *testing.T) {
	var err error = Violation{Span: diag.Point("a.gom", 3), Node: "If", Msg: "cond is not Bool"}
	if err.Error() == "" {
		t.Error("empty error string")
	}
}
