package core

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

var spn int

// sp returns a fresh valid span — contents do not matter, only validity.
func sp() diag.Span {
	spn++
	return diag.SpanIn("t.gom", spn*10, spn*10+5)
}

func vsym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindVar}
}

func fsym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindFunc}
}

func csym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindCtor}
}

func tsym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindType}
}

func fdsym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindField}
}

func gsym(name, pkg string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindFunc, IsGo: true, Package: pkg}
}

func bsym() *resolve.Symbol { return &resolve.Symbol{Name: "todo", Kind: resolve.KindBuiltin} }

func bind(s *resolve.Symbol, t types.Type) *Bind {
	return &Bind{Sp: sp(), Sym: s, Typ: t}
}

func vbind(name string, t types.Type) (*Bind, *resolve.Symbol) {
	s := vsym(name)
	return bind(s, t), s
}

func vref(s *resolve.Symbol, t types.Type) *Var {
	return &Var{Sp: sp(), Typ: t, Sym: s}
}

func intLit(text string) *Lit {
	return &Lit{Sp: sp(), Typ: types.Int, Kind: syntax.LitInt, Text: text}
}

func unitLit() *Lit {
	return &Lit{Sp: sp(), Typ: types.Unit, Kind: syntax.LitUnit, Text: "()"}
}

var (
	shapeT  = &types.Con{Name: "Shape"}
	pointT  = &types.Con{Name: "Point"}
	resIG   = types.Result(types.Int, types.GoError)
	fnII    = types.Fn(types.Int, types.P(types.Int))
	fnIII   = types.Fn(types.Int, types.P(types.Int), types.P(types.Int))
	fnSS    = types.Fn(resIG, types.P(types.String))
	shapeCt = csym("Circle")
	shapeEm = csym("Empty")
	okCtor  = csym("Ok")
	errCtor = csym("Err")
	pointX  = fdsym("x")
	pointY  = fdsym("y")
)

// okFile builds a valid Core IR file exercising every node family:
//
//	type Shape = | Circle (radius: Float) | Empty   // TSum + DCtorTest
//	type Point = { x: Float, y: Float }             // TRecord
//	type Meters = Float / type Opaque               // TAlias / TAbstract
//	let answer = 42                                 // VarDecl
//
//	fn area (s: Shape) -> Float = match s { ... }   // SwitchType
//	fn add2 (a: Int) (b: Int) -> Int = a + b
//	fn load (p: String) -> Result Int GoError =      // EscapeCall + CtorApp
//	  let n = try atoi(p)
//	  Ok n
//	fn apply2 (f: Int -> Int) -> Int = f 2            // CallFunc
//	fn main (u: Unit) (jobs: Chan Int) -> Unit = ...  // lets, partial closure, select, go, for
//	test "smoke" { ... }
//	fn classify (r: Result Int GoError) -> Int        // SwitchTag + Unary
//	fn sign (n: Int) -> Int                           // SwitchValue + DLitTest + DGuard
//	fn head (xs: List Int) -> Int                     // SwitchList + DListTest
//	fn countdown (n: Int) -> Int                      // TailCall (TailLoop)
//	fn fetch (u: Unit) -> Result Int GoError          // EscapeResult + FromGo wrappers + Conv
//	fn mk_add (a: Int) -> (Int -> Int)                // arity-split FuncDecl
//	fn adders (u: Unit) -> (Int -> Int -> Int)        // arity-split Closure
//	fn worker (jobs: Chan Int) (src: Result Int GoError) -> Unit
//	                                                  // go{ let g = fn () -> try ..; g () } + SelSend + ForChan
//	fn misc (u: Unit) -> Float                        // CtorRef + GoFuncRef + RecordNew/Update
//	fn use_answer (u: Unit) -> Int                    // VarDecl reference
var okFile = buildOKFile()

func buildOKFile() *File {
	f := &File{Sp: sp(), Package: "shapes", PkgSp: sp()}

	f.Imports = []Import{{Sp: sp(), Path: "strconv", IsGo: true,
		Sym: &resolve.Symbol{Name: "strconv", Kind: resolve.KindModule, IsGo: true, Package: "strconv"}}}

	// --- type Shape = Circle (radius: Float) | Empty
	f.Decls = append(f.Decls, &TypeDecl{
		Sp:  sp(),
		Sym: &resolve.Symbol{Name: "Shape", Kind: resolve.KindType},
		Body: &TSum{Sp: sp(), Variants: []TVariant{
			{Sp: sp(), Sym: shapeCt, Fields: []TField{
				{Sp: sp(), Name: "radius", Index: 0, Sym: fdsym("radius"), Type: types.Float},
			}},
			{Sp: sp(), Sym: shapeEm},
		}},
	})

	// --- fn area (s: Shape) -> Float = match s { Circle c -> c.radius * 2.0; Empty -> 0.0 }
	{
		sB, sS := vbind("s", shapeT)
		cB, cS := vbind("c", shapeT)
		rB, rS := vbind("r", types.Float)
		fn := &Func{
			Sp:     sp(),
			Sig:    types.Fn(types.Float, types.P(shapeT)),
			Params: []*Bind{sB},
			Body: &Body{Tail: &Match{
				Sp: sp(), Typ: types.Float,
				Tree: &DSwitch{
					Sp: sp(), Typ: types.Float,
					Subj: vref(sS, shapeT),
					Kind: SwitchType,
					Cases: []DCase{
						{
							Sp:    sp(),
							Test:  &DCtorTest{Sp: sp(), Ctor: shapeCt},
							Binds: []*Bind{cB},
							Body: &Body{
								Lets: []*Let{
									{Sp: sp(), Name: rB, X: &FieldGet{
										Sp: sp(), Typ: types.Float,
										Recv: vref(cS, shapeT), Field: fdsym("radius"), Index: 0,
									}},
								},
								Tail: &Binary{
									Sp: sp(), Typ: types.Float,
									Op: syntax.BinMul,
									X:  vref(rS, types.Float),
									Y:  &Lit{Sp: sp(), Typ: types.Float, Kind: syntax.LitFloat, Text: "2.0"},
								},
							},
						},
						{
							Sp:   sp(),
							Test: &DCtorTest{Sp: sp(), Ctor: shapeEm},
							Body: &Body{Tail: &Lit{
								Sp: sp(), Typ: types.Float, Kind: syntax.LitFloat, Text: "0.0",
							}},
						},
					},
					Default: &DPanic{
						Sp: sp(), Typ: types.Float,
						Subj:     vref(sS, shapeT),
						TypeName: "shapes.Shape",
					},
				},
			}},
		}
		f.Decls = append(f.Decls, &FuncDecl{
			Sp: sp(), Sym: fsym("area"), Fn: fn,
		})
	}

	// --- fn add2 (a: Int) (b: Int) -> Int = a + b
	add2 := fsym("add2")
	{
		aB, aS := vbind("a", types.Int)
		bB, bS := vbind("b", types.Int)
		f.Decls = append(f.Decls, &FuncDecl{
			Sp: sp(), Sym: add2,
			Fn: &Func{
				Sp:     sp(),
				Sig:    types.Fn(types.Int, types.P(types.Int), types.P(types.Int)),
				Params: []*Bind{aB, bB},
				Body: &Body{Tail: &Binary{
					Sp: sp(), Typ: types.Int,
					Op: syntax.BinAdd, X: vref(aS, types.Int), Y: vref(bS, types.Int),
				}},
			},
		})
	}

	// --- fn load (p: String) -> Result Int GoError = try atoi(p) then Ok
	{
		pB, pS := vbind("p", types.String)
		nB, nS := vbind("n", types.Int)
		rB, rS := vbind("r", resIG)
		fn := &Func{
			Sp:     sp(),
			Sig:    fnSS,
			Params: []*Bind{pB},
			Body: &Body{
				Lets: []*Let{
					{Sp: sp(), Name: nB, X: &EscapeCall{
						Sp: sp(), Typ: types.Int,
						Call: &CallGo{
							Sp: sp(), Typ: types.Int,
							Callee:  gsym("Atoi", "strconv"),
							Args:    []Atom{vref(pS, types.String)},
							Arity:   1,
							Results: GoResultsErr,
						},
						To: nil, // set below
					}},
					{Sp: sp(), Name: rB, X: &CtorApp{
						Sp: sp(), Typ: resIG,
						Ctor: okCtor, Args: []Atom{vref(nS, types.Int)}, Arity: 1,
					}},
				},
				Tail: vref(rS, resIG),
			},
		}
		// wire the escape target
		ec := fn.Body.Lets[0].X.(*EscapeCall)
		ec.To = fn
		f.Decls = append(f.Decls, &FuncDecl{Sp: sp(), Sym: fsym("load"), Fn: fn})
	}

	// --- fn apply2 (f: Int -> Int) -> Int = f 2 — CallFunc at full arity
	{
		fB, fS := vbind("f", fnII)
		f.Decls = append(f.Decls, &FuncDecl{
			Sp: sp(), Sym: fsym("apply2"),
			Fn: &Func{
				Sp:     sp(),
				Sig:    types.Fn(types.Int, types.P(fnII)),
				Params: []*Bind{fB},
				Body: &Body{Tail: &CallFunc{
					Sp: sp(), Typ: types.Int,
					Fn:   vref(fS, fnII),
					Args: []Atom{intLit("2")},
				}},
			},
		})
	}

	// --- fn main (u: Unit) (jobs: Chan Int) -> Unit: func ref, partial
	//     closure (add2 1), list, tuple, call on a func value, builtin,
	//     go, for, select
	{
		uB, _ := vbind("u", types.Unit)
		jobs := vsym("jobs")
		pB, _ := vbind("p", types.Fn(types.Int, types.P(types.Int), types.P(types.Int)))
		cB, cS := vbind("inc", fnII)
		lB, lS := vbind("l", types.List(types.Int))
		tB, _ := vbind("t", types.Tup(types.Int, types.Int))
		iB, _ := vbind("i", types.Int)
		jB, _ := vbind("j", types.Int)
		recvB, _ := vbind("r", types.Option(types.Int))

		// `let inc = add2 1` — partial application is an explicit
		// Closure whose body is the saturated CallDirect.
		clB, clS := vbind("b", types.Int)
		partial := &Closure{
			Sp: sp(), Typ: fnII,
			Fn: &Func{
				Sp:     sp(),
				Sig:    fnII,
				Params: []*Bind{clB},
				Body: &Body{Tail: &CallDirect{
					Sp: sp(), Typ: types.Int,
					Callee: add2, Args: []Atom{intLit("1"), vref(clS, types.Int)}, Arity: 2,
				}},
			},
		}

		// `add2`'s declared arity is 2 — the bare reference is not a
		// canonical value, so the binding carries the eta adapter
		// `func(a) func(b) { add2(a, b) }` that lowering emits for it.
		adA, adAS := vbind("a", types.Int)
		adB, adBS := vbind("b", types.Int)
		adapter := &Closure{
			Sp: sp(), Typ: fnIII,
			Fn: &Func{
				Sp:     sp(),
				Sig:    fnIII,
				Params: []*Bind{adA},
				Body: &Body{Tail: &Closure{
					Sp: sp(), Typ: fnII,
					Fn: &Func{
						Sp:     sp(),
						Sig:    fnII,
						Params: []*Bind{adB},
						Body: &Body{Tail: &CallDirect{
							Sp: sp(), Typ: types.Int,
							Callee: add2, Args: []Atom{vref(adAS, types.Int), vref(adBS, types.Int)}, Arity: 2,
						}},
					},
				}},
			},
		}

		fn := &Func{
			Sp:  sp(),
			Sig: types.Fn(types.Unit, types.P(types.Unit), types.P(types.Chan(types.Int))),
			Params: []*Bind{uB,
				bind(jobs, types.Chan(types.Int)),
			},
			Body: &Body{
				Lets: []*Let{
					{Sp: sp(), Name: pB, X: adapter},
					{Sp: sp(), Name: cB, X: partial},
					{Sp: sp(), Name: lB, X: &ListNew{
						Sp: sp(), Typ: types.List(types.Int),
						Elems: []Atom{intLit("1"), intLit("2")},
					}},
					{Sp: sp(), Name: tB, X: &TupleNew{
						Sp: sp(), Typ: types.Tup(types.Int, types.Int),
						Elems: []Atom{intLit("1"), intLit("2")},
					}},
					{Sp: sp(), Name: iB, X: &CallFunc{
						Sp: sp(), Typ: types.Int,
						Fn: vref(cS, fnII), Args: []Atom{intLit("3")},
					}},
					{Sp: sp(), Name: jB, X: &SelectExpr{
						Sp: sp(), Typ: types.Int,
						Arms: []SelArm{
							&SelRecv{
								Sp:   sp(),
								Chan: vref(jobs, types.Chan(types.Int)),
								Bind: recvB,
								Body: &Body{Tail: intLit("0")},
							},
							&SelDefault{Sp: sp(), Body: &Body{Tail: intLit("1")}},
						},
					}},
					{Sp: sp(), Name: &Bind{Sp: sp(), Typ: types.Unit, Discard: true},
						X: &CallBuiltin{
							Sp: sp(), Typ: types.Unit,
							Sym: bsym(), Args: []Atom{unitLit()},
						}},
					{Sp: sp(), Name: &Bind{Sp: sp(), Typ: types.Unit, Discard: true},
						X: &Spawn{
							Sp: sp(), Typ: types.Unit,
							Body: &Body{Tail: unitLit()},
						}},
					{Sp: sp(), Name: &Bind{Sp: sp(), Typ: types.Unit, Discard: true},
						X: &ForRange{
							Sp: sp(), Typ: types.Unit,
							Kind: ForList,
							Iter: vref(lS, types.List(types.Int)),
							Elem: &Bind{Sp: sp(), Typ: types.Int, Discard: true},
							Body: &Body{Tail: unitLit()},
						}},
				},
				Tail: unitLit(),
			},
		}
		f.Decls = append(f.Decls, &FuncDecl{Sp: sp(), Sym: fsym("main"), Fn: fn})
	}

	// --- test "smoke" { () }
	{
		tf := &Func{
			Sp:     sp(),
			Sig:    types.Fn(types.Unit),
			Params: nil,
			Body:   &Body{Tail: unitLit()},
		}
		f.Decls = append(f.Decls, &TestDecl{Sp: sp(), Name: "smoke", NameSp: sp(), Fn: tf})
	}

	// --- let answer = 42 — VarDecl
	answer := vsym("answer")
	f.Decls = append(f.Decls, &VarDecl{
		Sp: sp(), Sym: answer,
		Body: &Body{Tail: intLit("42")},
	})

	// --- type Point = { x: Float, y: Float } / type Meters = Float /
	//     type Opaque — TRecord / TAlias / TAbstract
	f.Decls = append(f.Decls,
		&TypeDecl{
			Sp:  sp(),
			Sym: tsym("Point"),
			Body: &TRecord{Sp: sp(), Fields: []TField{
				{Sp: sp(), Name: "x", Index: 0, Sym: pointX, Type: types.Float},
				{Sp: sp(), Name: "y", Index: 1, Sym: pointY, Type: types.Float},
			}},
		},
		&TypeDecl{
			Sp:   sp(),
			Sym:  tsym("Meters"),
			Body: &TAlias{Sp: sp(), Type: types.Float},
		},
		&TypeDecl{
			Sp:   sp(),
			Sym:  tsym("Opaque"),
			Body: &TAbstract{Sp: sp()},
		},
	)

	// --- fn classify (r: Result Int GoError) -> Int — SwitchTag over
	//     the flat2 Result, Unary on the Err arm:
	//     match r { Ok n -> n; Err _ -> -1 }
	{
		rB, rS := vbind("r", resIG)
		nB, nS := vbind("n", types.Int)
		f.Decls = append(f.Decls, &FuncDecl{
			Sp:  sp(),
			Sym: fsym("classify"),
			Fn: &Func{
				Sp:     sp(),
				Sig:    types.Fn(types.Int, types.P(resIG)),
				Params: []*Bind{rB},
				Body: &Body{Tail: &Match{
					Sp: sp(), Typ: types.Int,
					Tree: &DSwitch{
						Sp: sp(), Typ: types.Int,
						Subj: vref(rS, resIG),
						Kind: SwitchTag,
						Cases: []DCase{
							{
								Sp:    sp(),
								Test:  &DCtorTest{Sp: sp(), Ctor: okCtor},
								Binds: []*Bind{nB},
								Body:  &Body{Tail: vref(nS, types.Int)},
							},
							{
								Sp:    sp(),
								Test:  &DCtorTest{Sp: sp(), Ctor: errCtor},
								Binds: []*Bind{{Sp: sp(), Typ: types.GoError, Discard: true}},
								Body: &Body{Tail: &Unary{
									Sp: sp(), Typ: types.Int,
									Op: syntax.UnNeg, X: intLit("1"),
								}},
							},
						},
						Default: &DPanic{
							Sp: sp(), Typ: types.Int,
							Subj:     vref(rS, resIG),
							TypeName: "Result",
						},
					},
				}},
			},
		})
	}

	// --- fn sign (n: Int) -> Int — SwitchValue + DLitTest with a
	//     DGuard continuation:
	//     let g = n > 0; match n { 0 -> 0; _ if g -> 1; _ -> -1 }
	{
		nB, nS := vbind("n", types.Int)
		gB, gS := vbind("g", types.Bool)
		f.Decls = append(f.Decls, &FuncDecl{
			Sp:  sp(),
			Sym: fsym("sign"),
			Fn: &Func{
				Sp:     sp(),
				Sig:    fnII,
				Params: []*Bind{nB},
				Body: &Body{
					Lets: []*Let{{Sp: sp(), Name: gB, X: &Binary{
						Sp: sp(), Typ: types.Bool,
						Op: syntax.BinGt, X: vref(nS, types.Int), Y: intLit("0"),
					}}},
					Tail: &Match{
						Sp: sp(), Typ: types.Int,
						Tree: &DSwitch{
							Sp: sp(), Typ: types.Int,
							Subj: vref(nS, types.Int),
							Kind: SwitchValue,
							Cases: []DCase{{
								Sp:   sp(),
								Test: &DLitTest{Sp: sp(), Kind: syntax.LitInt, Text: "0"},
								Body: &Body{Tail: intLit("0")},
							}},
							Default: &DGuard{
								Sp: sp(), Typ: types.Int,
								Cond: vref(gS, types.Bool),
								Then: &DLeaf{
									Sp: sp(), Typ: types.Int,
									Body: &Body{Tail: intLit("1")},
								},
								Else: &DLeaf{
									Sp: sp(), Typ: types.Int,
									Body: &Body{Tail: &Unary{
										Sp: sp(), Typ: types.Int,
										Op: syntax.UnNeg, X: intLit("1"),
									}},
								},
							},
						},
					},
				},
			},
		})
	}

	// --- fn head (xs: List Int) -> Int — SwitchList + DListTest:
	//     match xs { [] -> 0; [h, ..t] -> h }
	{
		xsB, xsS := vbind("xs", types.List(types.Int))
		hB, hS := vbind("h", types.Int)
		tB, _ := vbind("t", types.List(types.Int))
		f.Decls = append(f.Decls, &FuncDecl{
			Sp:  sp(),
			Sym: fsym("head"),
			Fn: &Func{
				Sp:     sp(),
				Sig:    types.Fn(types.Int, types.P(types.List(types.Int))),
				Params: []*Bind{xsB},
				Body: &Body{Tail: &Match{
					Sp: sp(), Typ: types.Int,
					Tree: &DSwitch{
						Sp: sp(), Typ: types.Int,
						Subj: vref(xsS, types.List(types.Int)),
						Kind: SwitchList,
						Cases: []DCase{
							{
								Sp:   sp(),
								Test: &DListTest{Sp: sp(), Cons: false},
								Body: &Body{Tail: intLit("0")},
							},
							{
								Sp:    sp(),
								Test:  &DListTest{Sp: sp(), Cons: true},
								Binds: []*Bind{hB, tB},
								Body:  &Body{Tail: vref(hS, types.Int)},
							},
						},
						Default: &DPanic{
							Sp: sp(), Typ: types.Int,
							Subj:     vref(xsS, types.List(types.Int)),
							TypeName: "List",
						},
					},
				}},
			},
		})
	}

	// --- fn countdown (n: Int) -> Int — TailCall in the else branch
	//     of a TailLoop function:
	//     let z = n == 0; let m = n - 1; if z { 0 } else { countdown m }
	{
		nB, nS := vbind("n", types.Int)
		zB, zS := vbind("z", types.Bool)
		mB, mS := vbind("m", types.Int)
		fn := &Func{
			Sp:       sp(),
			Sig:      fnII,
			Params:   []*Bind{nB},
			TailLoop: true,
			Body: &Body{
				Lets: []*Let{
					{Sp: sp(), Name: zB, X: &Binary{
						Sp: sp(), Typ: types.Bool,
						Op: syntax.BinEq, X: vref(nS, types.Int), Y: intLit("0"),
					}},
					{Sp: sp(), Name: mB, X: &Binary{
						Sp: sp(), Typ: types.Int,
						Op: syntax.BinSub, X: vref(nS, types.Int), Y: intLit("1"),
					}},
				},
				Tail: &If{
					Sp: sp(), Typ: types.Int,
					Cond: vref(zS, types.Bool),
					Then: &Body{Tail: intLit("0")},
					Else: &Body{Tail: &TailCall{
						Sp: sp(), Typ: types.Int,
						Args: []Atom{vref(mS, types.Int)},
					}},
				},
			},
		}
		fn.Body.Tail.(*If).Else.Tail.(*TailCall).To = fn
		f.Decls = append(f.Decls, &FuncDecl{Sp: sp(), Sym: fsym("countdown"), Fn: fn})
	}

	// --- fn fetch (u: Unit) -> Result Int GoError —
	//     EscapeResult on a stored Result plus the naive FromGo
	//     wrappers and a Conv:
	//     let res = ResultFromGo atoi("1"); let n = try res;
	//     let i64 = conv n; let opt = OptionFromGo ..; let tup = TupleFromGo ..;
	//     Ok n
	{
		uB, _ := vbind("u", types.Unit)
		resB, resS := vbind("res", resIG)
		nB, nS := vbind("n", types.Int)
		i64B, _ := vbind("i64", types.Int64)
		optB, _ := vbind("opt", types.Option(types.Int))
		tupB, _ := vbind("tup", types.Tup(types.Int, types.Bool))
		outB, outS := vbind("out", resIG)
		fn := &Func{
			Sp:     sp(),
			Sig:    types.Fn(resIG, types.P(types.Unit)),
			Params: []*Bind{uB},
			Body: &Body{
				Lets: []*Let{
					{Sp: sp(), Name: resB, X: &ResultFromGo{
						Sp: sp(), Typ: resIG,
						Call: &CallGo{
							Sp: sp(), Typ: types.Int,
							Callee: gsym("Atoi", "strconv"),
							Args:   []Atom{&Lit{Sp: sp(), Typ: types.String, Kind: syntax.LitStr, Text: "\"1\""}},
							Arity:  1, Results: GoResultsErr,
						},
					}},
					{Sp: sp(), Name: nB, X: &EscapeResult{
						Sp: sp(), Typ: types.Int,
						X: vref(resS, resIG),
					}},
					{Sp: sp(), Name: i64B, X: &Conv{
						Sp: sp(), Typ: types.Int64,
						Kind: ConvNum, X: vref(nS, types.Int),
					}},
					{Sp: sp(), Name: optB, X: &OptionFromGo{
						Sp: sp(), Typ: types.Option(types.Int),
						Call: &CallGo{
							Sp: sp(), Typ: types.Int,
							Callee: gsym("Get", "cache"),
							Args:   []Atom{intLit("0")},
							Arity:  1, Results: GoResultsOK,
						},
					}},
					{Sp: sp(), Name: tupB, X: &TupleFromGo{
						Sp: sp(), Typ: types.Tup(types.Int, types.Bool),
						Call: &CallGo{
							Sp: sp(), Typ: types.Tup(types.Int, types.Bool),
							Callee: gsym("DivMod", "num"),
							Args:   []Atom{intLit("0")},
							Arity:  1, Results: GoResultsPair,
						},
					}},
					{Sp: sp(), Name: outB, X: &CtorApp{
						Sp: sp(), Typ: resIG,
						Ctor: okCtor, Args: []Atom{vref(nS, types.Int)}, Arity: 1,
					}},
				},
				Tail: vref(outS, resIG),
			},
		}
		fn.Body.Lets[1].X.(*EscapeResult).To = fn
		f.Decls = append(f.Decls, &FuncDecl{Sp: sp(), Sym: fsym("fetch"), Fn: fn})
	}

	// --- fn mk_add (a: Int) -> (Int -> Int) — arity-split FuncDecl:
	//     Sig flattens to Int -> Int -> Int but the declared arity is
	//     one, so the body returns the residual Int -> Int (a Closure).
	{
		aB, aS := vbind("a", types.Int)
		bB, bS := vbind("b", types.Int)
		f.Decls = append(f.Decls, &FuncDecl{
			Sp:  sp(),
			Sym: fsym("mk_add"),
			Fn: &Func{
				Sp:     sp(),
				Sig:    fnIII,
				Params: []*Bind{aB},
				Body: &Body{Tail: &Closure{
					Sp: sp(), Typ: fnII,
					Fn: &Func{
						Sp:     sp(),
						Sig:    fnII,
						Params: []*Bind{bB},
						Body: &Body{Tail: &Binary{
							Sp: sp(), Typ: types.Int,
							Op: syntax.BinAdd, X: vref(aS, types.Int), Y: vref(bS, types.Int),
						}},
					},
				}},
			},
		})
	}

	// --- fn adders (u: Unit) -> (Int -> Int -> Int) — arity-split fn
	//     whose body is itself an arity-split Closure:
	//     fn x -> (fn y -> x + y)
	{
		uB, _ := vbind("u", types.Unit)
		xB, xS := vbind("x", types.Int)
		yB, yS := vbind("y", types.Int)
		f.Decls = append(f.Decls, &FuncDecl{
			Sp:  sp(),
			Sym: fsym("adders"),
			Fn: &Func{
				Sp:     sp(),
				Sig:    types.Fn(types.Int, types.P(types.Unit), types.P(types.Int), types.P(types.Int)),
				Params: []*Bind{uB},
				Body: &Body{Tail: &Closure{
					Sp: sp(), Typ: fnIII,
					Fn: &Func{
						Sp:     sp(),
						Sig:    fnIII,
						Params: []*Bind{xB},
						Body: &Body{Tail: &Closure{
							Sp: sp(), Typ: fnII,
							Fn: &Func{
								Sp:     sp(),
								Sig:    fnII,
								Params: []*Bind{yB},
								Body: &Body{Tail: &Binary{
									Sp: sp(), Typ: types.Int,
									Op: syntax.BinAdd, X: vref(xS, types.Int), Y: vref(yS, types.Int),
								}},
							},
						}},
					},
				}},
			},
		})
	}

	// --- fn worker (jobs: Chan Int) (src: Result Int GoError) -> Unit
	//     — the sanctioned go{} pattern: a `try` inside a lambda bound
	//     inside the go block, applied there (spec section 10.3 —
	//     the lambda is a real function boundary); plus SelSend and
	//     ForChan:
	//     go { let g = fn (u: Unit) -> Result Int GoError = try src, Ok n
	//          let _ = g ()
	//          select { send jobs 7 -> (); default -> () }
	//          for _ in jobs { () } }
	{
		jB, jS := vbind("jobs", types.Chan(types.Int))
		srcB, srcS := vbind("src", resIG)
		guB, _ := vbind("gu", types.Unit)
		nB, nS := vbind("n", types.Int)
		gB, gS := vbind("g", types.Fn(resIG, types.P(types.Unit)))
		gfn := &Func{
			Sp:     sp(),
			Sig:    types.Fn(resIG, types.P(types.Unit)),
			Params: []*Bind{guB},
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
		f.Decls = append(f.Decls, &FuncDecl{
			Sp:  sp(),
			Sym: fsym("worker"),
			Fn: &Func{
				Sp:     sp(),
				Sig:    types.Fn(types.Unit, types.P(types.Chan(types.Int)), types.P(resIG)),
				Params: []*Bind{jB, srcB},
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
							{Sp: sp(), Name: &Bind{Sp: sp(), Typ: types.Unit, Discard: true}, X: &SelectExpr{
								Sp: sp(), Typ: types.Unit,
								Arms: []SelArm{
									&SelSend{
										Sp:    sp(),
										Chan:  vref(jS, types.Chan(types.Int)),
										Value: intLit("7"),
										Body:  &Body{Tail: unitLit()},
									},
									&SelDefault{Sp: sp(), Body: &Body{Tail: unitLit()}},
								},
							}},
							{Sp: sp(), Name: &Bind{Sp: sp(), Typ: types.Unit, Discard: true}, X: &ForRange{
								Sp: sp(), Typ: types.Unit,
								Kind: ForChan,
								Iter: vref(jS, types.Chan(types.Int)),
								Elem: &Bind{Sp: sp(), Typ: types.Int, Discard: true},
								Body: &Body{Tail: unitLit()},
							}},
						},
						Tail: unitLit(),
					},
				}},
			},
		})
	}

	// --- fn misc (u: Unit) -> Float — CtorRef and a direct GoFuncRef
	//     as stored values, RecordNew / RecordUpdate / FieldGet:
	//     let mkc = Circle; let itoa = strconv.Itoa;
	//     let p = {x = 1.0, y = 2.0}; let q = {p with x = 3.0}; q.x
	{
		uB, _ := vbind("u", types.Unit)
		cB, _ := vbind("mkc", types.Fn(shapeT, types.P(types.Float)))
		itoaB, _ := vbind("itoa", types.Fn(types.String, types.P(types.Int)))
		pB, pS := vbind("p", pointT)
		qB, qS := vbind("q", pointT)
		f.Decls = append(f.Decls, &FuncDecl{
			Sp:  sp(),
			Sym: fsym("misc"),
			Fn: &Func{
				Sp:     sp(),
				Sig:    types.Fn(types.Float, types.P(types.Unit)),
				Params: []*Bind{uB},
				Body: &Body{
					Lets: []*Let{
						{Sp: sp(), Name: cB, X: &CtorRef{
							Sp: sp(), Typ: types.Fn(shapeT, types.P(types.Float)),
							Sym: shapeCt,
						}},
						{Sp: sp(), Name: itoaB, X: &GoFuncRef{
							Sp: sp(), Typ: types.Fn(types.String, types.P(types.Int)),
							Sym:   gsym("Itoa", "strconv"),
							Arity: 1, Results: GoResultsOne,
						}},
						{Sp: sp(), Name: pB, X: &RecordNew{
							Sp: sp(), Typ: pointT,
							Fields: []FieldInit{
								{Sp: sp(), Sym: pointX, Index: 0, Value: &Lit{Sp: sp(), Typ: types.Float, Kind: syntax.LitFloat, Text: "1.0"}},
								{Sp: sp(), Sym: pointY, Index: 1, Value: &Lit{Sp: sp(), Typ: types.Float, Kind: syntax.LitFloat, Text: "2.0"}},
							},
						}},
						{Sp: sp(), Name: qB, X: &RecordUpdate{
							Sp: sp(), Typ: pointT,
							Rec: vref(pS, pointT),
							Fields: []FieldInit{
								{Sp: sp(), Sym: pointX, Index: 0, Value: &Lit{Sp: sp(), Typ: types.Float, Kind: syntax.LitFloat, Text: "3.0"}},
							},
						}},
					},
					Tail: &FieldGet{
						Sp: sp(), Typ: types.Float,
						Recv: vref(qS, pointT), Field: pointX, Index: 0,
					},
				},
			},
		})
	}

	// --- fn use_answer (u: Unit) -> Int — references the VarDecl.
	{
		uB, _ := vbind("u", types.Unit)
		f.Decls = append(f.Decls, &FuncDecl{
			Sp:  sp(),
			Sym: fsym("use_answer"),
			Fn: &Func{
				Sp:     sp(),
				Sig:    types.Fn(types.Int, types.P(types.Unit)),
				Params: []*Bind{uB},
				Body:   &Body{Tail: vref(answer, types.Int)},
			},
		})
	}

	return f
}

// TestSealedFamiliesIsExpr is a compile-level probe: one switch per
// sealed family proves the expected node set — go-check-sumtype
// keeps it exhaustive.
func TestSealedFamiliesIsExpr(t *testing.T) {
	var e Expr
	if e != nil {
		switch e.(type) {
		case *Var, *Lit, *FuncRef, *CtorRef, *GoFuncRef, *FieldGet,
			*Closure, *CallDirect, *CallFunc, *CallGo, *CallBuiltin, *CtorApp,
			*Unary, *Binary, *Equal, *If, *Match, *SelectExpr, *Spawn, *ForRange,
			*TupleNew, *ListNew, *RecordNew, *RecordUpdate,
			*TailCall, *EscapeResult, *EscapeCall,
			*ResultFromGo, *OptionFromGo, *TupleFromGo, *ErrorFromGo, *Conv:
		default:
			panic("unreachable")
		}
	}
	var a Atom
	if a != nil {
		switch a.(type) {
		case *Var, *Lit, *FuncRef, *CtorRef, *GoFuncRef, *FieldGet:
		default:
			panic("unreachable")
		}
	}
	var d Decl
	if d != nil {
		switch d.(type) {
		case *FuncDecl, *VarDecl, *TypeDecl, *TestDecl:
		default:
			panic("unreachable")
		}
	}
	var tb TypeBody
	if tb != nil {
		switch tb.(type) {
		case *TSum, *TRecord, *TAlias, *TAbstract:
		default:
			panic("unreachable")
		}
	}
	var dt DTree
	if dt != nil {
		switch dt.(type) {
		case *DSwitch, *DLeaf, *DPanic, *DGuard:
		default:
			panic("unreachable")
		}
	}
	var dts DTest
	if dts != nil {
		switch dts.(type) {
		case *DCtorTest, *DLitTest, *DListTest:
		default:
			panic("unreachable")
		}
	}
	var sa SelArm
	if sa != nil {
		switch sa.(type) {
		case *SelRecv, *SelSend, *SelDefault:
		default:
			panic("unreachable")
		}
	}
}

// TestAtomsAreExprs pins the atom set: only the six operand nodes may
// sit inside complex expressions.
func TestAtomsAreExprs(t *testing.T) {
	atoms := []Atom{
		&Var{}, &Lit{}, &FuncRef{}, &CtorRef{}, &GoFuncRef{}, &FieldGet{},
	}
	for _, a := range atoms {
		if _, ok := a.(Expr); !ok {
			t.Errorf("%T is not an Expr", a)
		}
	}
	nonAtoms := []Expr{
		&Closure{}, &CallDirect{}, &If{}, &Match{}, &SelectExpr{},
		&EscapeResult{}, &Conv{}, &TupleNew{},
	}
	for _, e := range nonAtoms {
		if _, ok := e.(Atom); ok {
			t.Errorf("%T must not be an Atom", e)
		}
	}
}

func TestSpanAndType(t *testing.T) {
	s := diag.SpanIn("x.gom", 3, 9)
	v := &Var{Sp: s, Typ: types.Int, Sym: vsym("x")}
	if v.Span() != s {
		t.Errorf("Span() = %v, want %v", v.Span(), s)
	}
	if v.Type() != types.Int {
		t.Errorf("Type() = %v, want Int", v.Type())
	}
	if v.BaseOf() != v.Base {
		t.Errorf("BaseOf() mismatch")
	}
}
