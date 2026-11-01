package codegen

// This file builds the validator-clean fixture exercising every
// node family — the same coverage as internal/core's okFile, ported
// so codegen tests do not depend on core's test helpers. The fixture
// mirrors the same constructs:
//
//	type Shape = | Circle (radius: Float) | Empty   // TSum + DCtorTest
//	type Point = { x: Float, y: Float }             // TRecord
//	type Meters = Float / type Opaque               // TAlias / TAbstract
//	let answer = 42                                 // VarDecl
//
//	fn area (s: Shape) -> Float = match s { ... }   // SwitchType
//	fn add2 (a: Int) (b: Int) -> Int = a + b
//	fn load (p: String) -> Result Int GoError =     // EscapeCall + CtorApp
//	  let n = try atoi(p); Ok n
//	fn apply2 (f: Int -> Int) -> Int = f 2          // CallFunc
//	fn main (u: Unit) (jobs: Chan Int) -> Unit      // lets, closure, select, go, for
//	test "smoke" { ... }
//	fn classify (r: Result Int GoError) -> Int      // SwitchTag + Unary
//	fn sign (n: Int) -> Int                         // SwitchValue + DGuard
//	fn head (xs: List Int) -> Int                   // SwitchList
//	fn countdown (n: Int) -> Int                    // TailCall (TailLoop)
//	fn fetch (u: Unit) -> Result Int GoError        // EscapeResult + FromGo + Conv
//	fn mk_add (a: Int) -> (Int -> Int)              // arity-split FuncDecl
//	fn adders (u: Unit) -> (Int -> Int -> Int)      // arity-split Closure
//	fn worker (jobs: Chan Int) (src: Result Int GoError) -> Unit
//	fn misc (u: Unit) -> Float                      // CtorRef + GoFuncRef + Record*
//	fn use_answer (u: Unit) -> Int                  // VarDecl reference
//	fn regress (p: String) (c: Bool) (def: Int) -> Result Int GoError
//	                                                // regressions (see below)

import (
	"fmt"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// goCalleePkg is the Go import path the fixture's callees point at
// — "strconv" for the golden fixture, remapped to "gostub" by
// compileFixture so the compile test can control signatures.
func buildFixture() *core.File {
	tspn = 0 // deterministic //line spans across -count=N reruns
	f := &core.File{Sp: tsp(), Package: "shapes", PkgSp: tsp()}

	f.Imports = []core.Import{{Sp: tsp(), Path: "strconv", IsGo: true,
		Sym: &resolve.Symbol{Name: "strconv", Kind: resolve.KindModule, IsGo: true, Package: "strconv"}}}

	// --- type Shape = Circle (radius: Float) | Empty
	f.Decls = append(f.Decls, &core.TypeDecl{
		Sp:  tsp(),
		Sym: &resolve.Symbol{Name: "Shape", Kind: resolve.KindType},
		Body: &core.TSum{Sp: tsp(), Variants: []core.TVariant{
			{Sp: tsp(), Sym: shapeCt, Fields: []core.TField{
				{Sp: tsp(), Name: "radius", Index: 0, Sym: fdsym("radius"), Type: types.Float},
			}},
			{Sp: tsp(), Sym: shapeEm},
		}},
	})

	// --- fn area (s: Shape) -> Float = match s { Circle c -> ..; Empty -> 0.0 }
	{
		sB, sS := vbind("s", shapeT)
		cB, cS := vbind("c", shapeT)
		rB, rS := vbind("r", types.Float)
		fn := &core.Func{
			Sp:     tsp(),
			Sig:    types.Fn(types.Float, types.P(shapeT)),
			Params: []*core.Bind{sB},
			Body: &core.Body{Tail: &core.Match{
				Base: core.Base{Sp: tsp(), Typ: types.Float},
				Tree: &core.DSwitch{
					Base: core.Base{Sp: tsp(), Typ: types.Float},
					Subj: vref(sS, shapeT),
					Kind: core.SwitchType,
					Cases: []core.DCase{
						{
							Sp:    tsp(),
							Test:  &core.DCtorTest{Sp: tsp(), Ctor: shapeCt},
							Binds: []*core.Bind{cB},
							Body: &core.Body{
								Lets: []*core.Let{{Sp: tsp(), Name: rB, X: &core.FieldGet{
									Base: core.Base{Sp: tsp(), Typ: types.Float},
									Recv: vref(cS, shapeT), Field: fdsym("radius"), Index: 0,
								}}},
								Tail: &core.Binary{
									Base: core.Base{Sp: tsp(), Typ: types.Float},
									Op:   syntax.BinMul,
									X:    vref(rS, types.Float),
									Y: &core.Lit{
										Base: core.Base{Sp: tsp(), Typ: types.Float},
										Kind: syntax.LitFloat, Text: "2.0",
									},
								},
							},
						},
						{
							Sp:   tsp(),
							Test: &core.DCtorTest{Sp: tsp(), Ctor: shapeEm},
							Body: &core.Body{Tail: &core.Lit{
								Base: core.Base{Sp: tsp(), Typ: types.Float},
								Kind: syntax.LitFloat, Text: "0.0",
							}},
						},
					},
					Default: &core.DPanic{
						Base:     core.Base{Sp: tsp(), Typ: types.Float},
						Subj:     vref(sS, shapeT),
						TypeName: "shapes.Shape",
					},
				},
			}},
		}
		f.Decls = append(f.Decls, &core.FuncDecl{Sp: tsp(), Sym: fsym("area"), Fn: fn})
	}

	// --- fn add2 (a: Int) (b: Int) -> Int = a + b
	add2 := fsym("add2")
	{
		aB, aS := vbind("a", types.Int)
		bB, bS := vbind("b", types.Int)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: tsp(), Sym: add2,
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    fnIII,
				Params: []*core.Bind{aB, bB},
				Body: &core.Body{Tail: &core.Binary{
					Base: core.Base{Sp: tsp(), Typ: types.Int},
					Op:   syntax.BinAdd, X: vref(aS, types.Int), Y: vref(bS, types.Int),
				}},
			},
		})
	}

	// --- fn load (p: String) -> Result Int GoError = try atoi(p); Ok n
	{
		pB, pS := vbind("p", types.String)
		nB, nS := vbind("n", types.Int)
		rB, rS := vbind("r", resIG)
		fn := &core.Func{
			Sp:     tsp(),
			Sig:    types.Fn(resIG, types.P(types.String)),
			Params: []*core.Bind{pB},
			Body: &core.Body{
				Lets: []*core.Let{
					{Sp: tsp(), Name: nB, X: &core.EscapeCall{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Call: &core.CallGo{
							Base:    core.Base{Sp: tsp(), Typ: types.Int},
							Callee:  gsym("Atoi", "strconv"),
							Args:    []core.Atom{vref(pS, types.String)},
							Arity:   1,
							Results: core.GoResultsErr,
						},
						Wrap: "load",
					}},
					{Sp: tsp(), Name: rB, X: &core.CtorApp{
						Base: core.Base{Sp: tsp(), Typ: resIG},
						Ctor: okCtor, Args: []core.Atom{vref(nS, types.Int)}, Arity: 1,
					}},
				},
				Tail: vref(rS, resIG),
			},
		}
		fn.Body.Lets[0].X.(*core.EscapeCall).To = fn
		f.Decls = append(f.Decls, &core.FuncDecl{Sp: tsp(), Sym: fsym("load"), Fn: fn})
	}

	// --- fn apply2 (f: Int -> Int) -> Int = f 2 — CallFunc
	{
		fB, fS := vbind("f", fnII)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: tsp(), Sym: fsym("apply2"),
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    types.Fn(types.Int, types.P(fnII)),
				Params: []*core.Bind{fB},
				Body: &core.Body{Tail: &core.CallFunc{
					Base: core.Base{Sp: tsp(), Typ: types.Int},
					Fn:   vref(fS, fnII), Args: []core.Atom{intLit("2")},
				}},
			},
		})
	}

	// --- fn main (u: Unit) (jobs: Chan Int) -> Unit
	{
		uB, _ := vbind("u", types.Unit)
		jobs := vsym("jobs")
		pB, _ := vbind("p", types.Fn(types.Int, types.P(types.Int), types.P(types.Int)))
		cB, cS := vbind("inc", fnII)
		lB, lS := vbind("l", types.List(types.Int))
		tB, _ := vbind("tp", types.Tup(types.Int, types.Int))
		iB, _ := vbind("i", types.Int)
		jB, _ := vbind("j", types.Int)
		recvB, _ := vbind("rc", types.Option(types.Int))

		clB, clS := vbind("b", types.Int)
		partial := &core.Closure{
			Base: core.Base{Sp: tsp(), Typ: fnII},
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    fnII,
				Params: []*core.Bind{clB},
				Body: &core.Body{Tail: &core.CallDirect{
					Base:   core.Base{Sp: tsp(), Typ: types.Int},
					Callee: add2, Args: []core.Atom{intLit("1"), vref(clS, types.Int)}, Arity: 2,
				}},
			},
		}

		fn := &core.Func{
			Sp:  tsp(),
			Sig: types.Fn(types.Unit, types.P(types.Unit), types.P(types.Chan(types.Int))),
			Params: []*core.Bind{uB,
				bind(jobs, types.Chan(types.Int)),
			},
			Body: &core.Body{
				Lets: []*core.Let{
					{Sp: tsp(), Name: pB, X: &core.FuncRef{
						Base: core.Base{Sp: tsp(), Typ: types.Fn(types.Int, types.P(types.Int), types.P(types.Int))},
						Sym:  add2, Arity: 2,
					}},
					{Sp: tsp(), Name: cB, X: partial},
					{Sp: tsp(), Name: lB, X: &core.ListNew{
						Base:  core.Base{Sp: tsp(), Typ: types.List(types.Int)},
						Elems: []core.Atom{intLit("1"), intLit("2")},
					}},
					{Sp: tsp(), Name: tB, X: &core.TupleNew{
						Base:  core.Base{Sp: tsp(), Typ: types.Tup(types.Int, types.Int)},
						Elems: []core.Atom{intLit("1"), intLit("2")},
					}},
					{Sp: tsp(), Name: iB, X: &core.CallFunc{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Fn:   vref(cS, fnII), Args: []core.Atom{intLit("3")},
					}},
					{Sp: tsp(), Name: jB, X: &core.SelectExpr{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Arms: []core.SelArm{
							&core.SelRecv{
								Sp:   tsp(),
								Chan: vref(jobs, types.Chan(types.Int)),
								Bind: recvB,
								Body: &core.Body{Tail: intLit("0")},
							},
							&core.SelDefault{Sp: tsp(), Body: &core.Body{Tail: intLit("1")}},
						},
					}},
					{Sp: tsp(), Name: &core.Bind{Sp: tsp(), Typ: types.Unit, Discard: true},
						X: &core.CallBuiltin{
							Base: core.Base{Sp: tsp(), Typ: types.Unit},
							Sym:  bsym("todo"), Args: []core.Atom{unitLit()},
						}},
					{Sp: tsp(), Name: &core.Bind{Sp: tsp(), Typ: types.Unit, Discard: true},
						X: &core.Spawn{
							Base: core.Base{Sp: tsp(), Typ: types.Unit},
							Body: &core.Body{Tail: unitLit()},
						}},
					{Sp: tsp(), Name: &core.Bind{Sp: tsp(), Typ: types.Unit, Discard: true},
						X: &core.ForRange{
							Base: core.Base{Sp: tsp(), Typ: types.Unit},
							Kind: core.ForList,
							Iter: vref(lS, types.List(types.Int)),
							Elem: &core.Bind{Sp: tsp(), Typ: types.Int, Discard: true},
							Body: &core.Body{Tail: unitLit()},
						}},
				},
				Tail: unitLit(),
			},
		}
		f.Decls = append(f.Decls, &core.FuncDecl{Sp: tsp(), Sym: fsym("main"), Fn: fn})
	}

	// --- test "smoke" { () }
	{
		tf := &core.Func{
			Sp:     tsp(),
			Sig:    types.Fn(types.Unit),
			Params: nil,
			Body:   &core.Body{Tail: unitLit()},
		}
		f.Decls = append(f.Decls, &core.TestDecl{Sp: tsp(), Name: "smoke", NameSp: tsp(), Fn: tf})
	}

	// --- let answer = 42 — VarDecl
	answer := vsym("answer")
	f.Decls = append(f.Decls, &core.VarDecl{
		Sp: tsp(), Sym: answer,
		Body: &core.Body{Tail: intLit("42")},
	})

	// --- let computed = { let n = add2 1 2; n } — VarDecl whose
	//     body has lets: the sanctioned IIFE initializer
	{
		nB, nS := vbind("cn", types.Int)
		f.Decls = append(f.Decls, &core.VarDecl{
			Sp: tsp(), Sym: vsym("computed"),
			Body: &core.Body{
				Lets: []*core.Let{{Sp: tsp(), Name: nB, X: &core.CallDirect{
					Base:   core.Base{Sp: tsp(), Typ: types.Int},
					Callee: add2, Args: []core.Atom{intLit("1"), intLit("2")},
					Arity: 2,
				}}},
				Tail: vref(nS, types.Int),
			},
		})
	}

	// --- type Box a = | Boxed (v: a) | Nada — generic TSum
	boxA := &types.Var{Name: "a", ID: 1}
	boxCt := csym("Boxed")
	boxNd := csym("Nada")
	f.Decls = append(f.Decls, &core.TypeDecl{
		Sp:         tsp(),
		Sym:        &resolve.Symbol{Name: "Box", Kind: resolve.KindType},
		TypeParams: []*types.Var{boxA},
		Body: &core.TSum{Sp: tsp(), Variants: []core.TVariant{
			{Sp: tsp(), Sym: boxCt, Fields: []core.TField{
				{Sp: tsp(), Name: "v", Index: 0, Sym: fdsym("v"), Type: boxA},
			}},
			{Sp: tsp(), Sym: boxNd},
		}},
	})

	// --- fn ident [a] (x: a) -> a = x — generic FuncDecl
	{
		idA := &types.Var{Name: "a", ID: 2}
		xB, xS := vbind("x", idA)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp:         tsp(),
			Sym:        fsym("ident"),
			TypeParams: []*types.Var{idA},
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    types.Fn(idA, types.P(idA)),
				Params: []*core.Bind{xB},
				Body:   &core.Body{Tail: vref(xS, idA)},
			},
		})
	}

	// --- fn unwrap (b: Box Int) -> Int — SwitchType on an
	//     instantiated generic ADT: `case Box_Boxed[int64]`; the case
	//     binds the narrowed subject and the payload is a field read
	//     (tree.go SwitchType).
	{
		boxIntT := &types.Con{Name: "Box", Args: []types.Type{types.Int}}
		bB, bS := vbind("b", boxIntT)
		vB, vS := vbind("v", boxIntT)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp:  tsp(),
			Sym: fsym("unwrap"),
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    types.Fn(types.Int, types.P(boxIntT)),
				Params: []*core.Bind{bB},
				Body: &core.Body{Tail: &core.Match{
					Base: core.Base{Sp: tsp(), Typ: types.Int},
					Tree: &core.DSwitch{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Subj: vref(bS, boxIntT),
						Kind: core.SwitchType,
						Cases: []core.DCase{
							{
								Sp:    tsp(),
								Test:  &core.DCtorTest{Sp: tsp(), Ctor: boxCt},
								Binds: []*core.Bind{vB},
								Body: &core.Body{Tail: &core.FieldGet{
									Base:  core.Base{Sp: tsp(), Typ: types.Int},
									Recv:  vref(vS, boxIntT),
									Field: fdsym("v"), Index: 0,
								}},
							},
							{
								Sp:   tsp(),
								Test: &core.DCtorTest{Sp: tsp(), Ctor: boxNd},
								Body: &core.Body{Tail: intLit("0")},
							},
						},
						Default: &core.DPanic{
							Base:     core.Base{Sp: tsp(), Typ: types.Int},
							Subj:     vref(bS, &types.Con{Name: "Box", Args: []types.Type{types.Int}}),
							TypeName: "shapes.Box",
						},
					},
				}},
			},
		})
	}

	// --- test "result" { let r = ResultFromGo atoi; let n = try r;
	//     Ok () } — a Result-bodied test exercising AssertOk and the
	//     t.Errorf escape.
	{
		resUE := types.Result(types.Unit, types.GoError)
		tf := &core.Func{
			Sp:  tsp(),
			Sig: types.Fn(resUE),
			Body: &core.Body{
				Lets: []*core.Let{},
				Tail: &core.CtorApp{
					Base: core.Base{Sp: tsp(), Typ: resUE},
					Ctor: okCtor, Args: []core.Atom{unitLit()}, Arity: 1,
				},
			},
		}
		rB, rS := vbind("res", resIG)
		nB, _ := vbind("n", types.Int)
		tf.Body.Lets = append(tf.Body.Lets,
			&core.Let{Sp: tsp(), Name: rB, X: &core.ResultFromGo{
				Base: core.Base{Sp: tsp(), Typ: resIG},
				Call: &core.CallGo{
					Base:   core.Base{Sp: tsp(), Typ: types.Int},
					Callee: gsym("Atoi", "strconv"),
					Args: []core.Atom{&core.Lit{
						Base: core.Base{Sp: tsp(), Typ: types.String},
						Kind: syntax.LitStr, Text: `"7"`,
					}},
					Arity: 1, Results: core.GoResultsErr,
				},
			}},
			&core.Let{Sp: tsp(), Name: nB, X: &core.EscapeResult{
				Base: core.Base{Sp: tsp(), Typ: types.Int},
				X:    vref(rS, resIG), To: tf,
			}},
		)
		f.Decls = append(f.Decls, &core.TestDecl{Sp: tsp(), Name: "result", NameSp: tsp(), Fn: tf})
	}

	// --- type Point / Meters / Opaque
	pointX := fdsym("x")
	pointY := fdsym("y")
	f.Decls = append(f.Decls,
		&core.TypeDecl{
			Sp:  tsp(),
			Sym: tsym("Point"),
			Body: &core.TRecord{Sp: tsp(), Fields: []core.TField{
				{Sp: tsp(), Name: "x", Index: 0, Sym: pointX, Type: types.Float},
				{Sp: tsp(), Name: "y", Index: 1, Sym: pointY, Type: types.Float},
			}},
		},
		&core.TypeDecl{
			Sp:   tsp(),
			Sym:  tsym("Meters"),
			Body: &core.TAlias{Sp: tsp(), Type: types.Float},
		},
		&core.TypeDecl{
			Sp:   tsp(),
			Sym:  tsym("Opaque"),
			Body: &core.TAbstract{Sp: tsp()},
		},
	)

	// --- fn classify (r: Result Int GoError) -> Int — SwitchTag
	{
		rB, rS := vbind("r", resIG)
		nB, nS := vbind("n", types.Int)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp:  tsp(),
			Sym: fsym("classify"),
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    types.Fn(types.Int, types.P(resIG)),
				Params: []*core.Bind{rB},
				Body: &core.Body{Tail: &core.Match{
					Base: core.Base{Sp: tsp(), Typ: types.Int},
					Tree: &core.DSwitch{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Subj: vref(rS, resIG),
						Kind: core.SwitchTag,
						Cases: []core.DCase{
							{
								Sp:    tsp(),
								Test:  &core.DCtorTest{Sp: tsp(), Ctor: okCtor},
								Binds: []*core.Bind{nB},
								Body:  &core.Body{Tail: vref(nS, types.Int)},
							},
							{
								Sp:    tsp(),
								Test:  &core.DCtorTest{Sp: tsp(), Ctor: errCtor},
								Binds: []*core.Bind{{Sp: tsp(), Typ: types.GoError, Discard: true}},
								Body: &core.Body{Tail: &core.Unary{
									Base: core.Base{Sp: tsp(), Typ: types.Int},
									Op:   syntax.UnNeg, X: intLit("1"),
								}},
							},
						},
						Default: &core.DPanic{
							Base:     core.Base{Sp: tsp(), Typ: types.Int},
							Subj:     vref(rS, resIG),
							TypeName: "Result",
						},
					},
				}},
			},
		})
	}

	// --- fn sign (n: Int) -> Int — SwitchValue + DGuard
	{
		nB, nS := vbind("n", types.Int)
		gB, gS := vbind("g", types.Bool)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp:  tsp(),
			Sym: fsym("sign"),
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    fnII,
				Params: []*core.Bind{nB},
				Body: &core.Body{
					Lets: []*core.Let{{Sp: tsp(), Name: gB, X: &core.Binary{
						Base: core.Base{Sp: tsp(), Typ: types.Bool},
						Op:   syntax.BinGt, X: vref(nS, types.Int), Y: intLit("0"),
					}}},
					Tail: &core.Match{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Tree: &core.DSwitch{
							Base: core.Base{Sp: tsp(), Typ: types.Int},
							Subj: vref(nS, types.Int),
							Kind: core.SwitchValue,
							Cases: []core.DCase{{
								Sp:   tsp(),
								Test: &core.DLitTest{Sp: tsp(), Kind: syntax.LitInt, Text: "0"},
								Body: &core.Body{Tail: intLit("0")},
							}},
							Default: &core.DGuard{
								Base: core.Base{Sp: tsp(), Typ: types.Int},
								Cond: vref(gS, types.Bool),
								Then: &core.DLeaf{
									Base: core.Base{Sp: tsp(), Typ: types.Int},
									Body: &core.Body{Tail: intLit("1")},
								},
								Else: &core.DLeaf{
									Base: core.Base{Sp: tsp(), Typ: types.Int},
									Body: &core.Body{Tail: &core.Unary{
										Base: core.Base{Sp: tsp(), Typ: types.Int},
										Op:   syntax.UnNeg, X: intLit("1"),
									}},
								},
							},
						},
					},
				},
			},
		})
	}

	// --- fn head (xs: List Int) -> Int — SwitchList
	{
		xsB, xsS := vbind("xs", types.List(types.Int))
		hB, hS := vbind("h", types.Int)
		tlB, _ := vbind("tl", types.List(types.Int))
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp:  tsp(),
			Sym: fsym("head"),
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    types.Fn(types.Int, types.P(types.List(types.Int))),
				Params: []*core.Bind{xsB},
				Body: &core.Body{Tail: &core.Match{
					Base: core.Base{Sp: tsp(), Typ: types.Int},
					Tree: &core.DSwitch{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Subj: vref(xsS, types.List(types.Int)),
						Kind: core.SwitchList,
						Cases: []core.DCase{
							{
								Sp:   tsp(),
								Test: &core.DListTest{Sp: tsp(), Cons: false},
								Body: &core.Body{Tail: intLit("0")},
							},
							{
								Sp:    tsp(),
								Test:  &core.DListTest{Sp: tsp(), Cons: true},
								Binds: []*core.Bind{hB, tlB},
								Body:  &core.Body{Tail: vref(hS, types.Int)},
							},
						},
						Default: &core.DPanic{
							Base:     core.Base{Sp: tsp(), Typ: types.Int},
							Subj:     vref(xsS, types.List(types.Int)),
							TypeName: "List",
						},
					},
				}},
			},
		})
	}

	// --- fn countdown (n: Int) -> Int — TailCall in a TailLoop
	{
		nB, nS := vbind("n", types.Int)
		zB, zS := vbind("z", types.Bool)
		mB, mS := vbind("m", types.Int)
		fn := &core.Func{
			Sp:       tsp(),
			Sig:      fnII,
			Params:   []*core.Bind{nB},
			TailLoop: true,
			Body: &core.Body{
				Lets: []*core.Let{
					{Sp: tsp(), Name: zB, X: &core.Binary{
						Base: core.Base{Sp: tsp(), Typ: types.Bool},
						Op:   syntax.BinEq, X: vref(nS, types.Int), Y: intLit("0"),
					}},
					{Sp: tsp(), Name: mB, X: &core.Binary{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Op:   syntax.BinSub, X: vref(nS, types.Int), Y: intLit("1"),
					}},
				},
				Tail: &core.If{
					Base: core.Base{Sp: tsp(), Typ: types.Int},
					Cond: vref(zS, types.Bool),
					Then: &core.Body{Tail: intLit("0")},
					Else: &core.Body{Tail: &core.TailCall{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Args: []core.Atom{vref(mS, types.Int)},
					}},
				},
			},
		}
		fn.Body.Tail.(*core.If).Else.Tail.(*core.TailCall).To = fn
		f.Decls = append(f.Decls, &core.FuncDecl{Sp: tsp(), Sym: fsym("countdown"), Fn: fn})
	}

	// --- fn fetch (u: Unit) -> Result Int GoError —
	//     EscapeResult + FromGo wrappers + Conv
	{
		uB, _ := vbind("u", types.Unit)
		resB, resS := vbind("res", resIG)
		nB, nS := vbind("n", types.Int)
		i64B, _ := vbind("i64", types.Int64)
		optB, _ := vbind("opt", types.Option(types.Int))
		tupB, _ := vbind("tup", types.Tup(types.Int, types.Bool))
		outB, outS := vbind("out", resIG)
		fn := &core.Func{
			Sp:     tsp(),
			Sig:    types.Fn(resIG, types.P(types.Unit)),
			Params: []*core.Bind{uB},
			Body: &core.Body{
				Lets: []*core.Let{
					{Sp: tsp(), Name: resB, X: &core.ResultFromGo{
						Base: core.Base{Sp: tsp(), Typ: resIG},
						Call: &core.CallGo{
							Base:   core.Base{Sp: tsp(), Typ: types.Int},
							Callee: gsym("Atoi", "strconv"),
							Args: []core.Atom{&core.Lit{
								Base: core.Base{Sp: tsp(), Typ: types.String},
								Kind: syntax.LitStr, Text: `"1"`,
							}},
							Arity: 1, Results: core.GoResultsErr,
						},
					}},
					{Sp: tsp(), Name: nB, X: &core.EscapeResult{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						X:    vref(resS, resIG),
					}},
					{Sp: tsp(), Name: i64B, X: &core.Conv{
						Base: core.Base{Sp: tsp(), Typ: types.Int64},
						Kind: core.ConvNum, X: vref(nS, types.Int),
					}},
					{Sp: tsp(), Name: optB, X: &core.OptionFromGo{
						Base: core.Base{Sp: tsp(), Typ: types.Option(types.Int)},
						Call: &core.CallGo{
							Base:   core.Base{Sp: tsp(), Typ: types.Int},
							Callee: gsym("Get", "cache"),
							Args:   []core.Atom{intLit("0")},
							Arity:  1, Results: core.GoResultsOK,
						},
					}},
					{Sp: tsp(), Name: tupB, X: &core.TupleFromGo{
						Base: core.Base{Sp: tsp(), Typ: types.Tup(types.Int, types.Bool)},
						Call: &core.CallGo{
							Base:   core.Base{Sp: tsp(), Typ: types.Tup(types.Int, types.Bool)},
							Callee: gsym("DivMod", "num"),
							Args:   []core.Atom{intLit("0")},
							Arity:  1, Results: core.GoResultsPair,
						},
					}},
					{Sp: tsp(), Name: outB, X: &core.CtorApp{
						Base: core.Base{Sp: tsp(), Typ: resIG},
						Ctor: okCtor, Args: []core.Atom{vref(nS, types.Int)}, Arity: 1,
					}},
				},
				Tail: vref(outS, resIG),
			},
		}
		fn.Body.Lets[1].X.(*core.EscapeResult).To = fn
		f.Decls = append(f.Decls, &core.FuncDecl{Sp: tsp(), Sym: fsym("fetch"), Fn: fn})
	}

	// --- fn mk_add (a: Int) -> (Int -> Int) — arity-split FuncDecl
	{
		aB, aS := vbind("a", types.Int)
		bB, bS := vbind("b", types.Int)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp:  tsp(),
			Sym: fsym("mk_add"),
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    fnIII,
				Params: []*core.Bind{aB},
				Body: &core.Body{Tail: &core.Closure{
					Base: core.Base{Sp: tsp(), Typ: fnII},
					Fn: &core.Func{
						Sp:     tsp(),
						Sig:    fnII,
						Params: []*core.Bind{bB},
						Body: &core.Body{Tail: &core.Binary{
							Base: core.Base{Sp: tsp(), Typ: types.Int},
							Op:   syntax.BinAdd, X: vref(aS, types.Int), Y: vref(bS, types.Int),
						}},
					},
				}},
			},
		})
	}

	// --- fn adders (u: Unit) -> (Int -> Int -> Int) — arity-split
	//     FuncDecl whose body is one flat two-param Closure: the
	//     residual `Int -> Int -> Int` is exactly the closure's Sig
	//     (node.go — Sigs are canonical flat arrows).
	{
		uB, _ := vbind("u", types.Unit)
		xB, xS := vbind("x", types.Int)
		yB, yS := vbind("y", types.Int)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp:  tsp(),
			Sym: fsym("adders"),
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    types.Fn(types.Int, types.P(types.Unit), types.P(types.Int), types.P(types.Int)),
				Params: []*core.Bind{uB},
				Body: &core.Body{Tail: &core.Closure{
					Base: core.Base{Sp: tsp(), Typ: fnIII},
					Fn: &core.Func{
						Sp:     tsp(),
						Sig:    fnIII,
						Params: []*core.Bind{xB, yB},
						Body: &core.Body{Tail: &core.Binary{
							Base: core.Base{Sp: tsp(), Typ: types.Int},
							Op:   syntax.BinAdd, X: vref(xS, types.Int), Y: vref(yS, types.Int),
						}},
					},
				}},
			},
		})
	}

	// --- fn worker (jobs: Chan Int) (src: Result Int GoError) -> Unit
	{
		jB, jS := vbind("jobs", types.Chan(types.Int))
		srcB, srcS := vbind("src", resIG)
		guB, _ := vbind("gu", types.Unit)
		nB, nS := vbind("n", types.Int)
		gB, gS := vbind("g", types.Fn(resIG, types.P(types.Unit)))
		gfn := &core.Func{
			Sp:     tsp(),
			Sig:    types.Fn(resIG, types.P(types.Unit)),
			Params: []*core.Bind{guB},
			Body: &core.Body{
				Lets: []*core.Let{{Sp: tsp(), Name: nB, X: &core.EscapeResult{
					Base: core.Base{Sp: tsp(), Typ: types.Int},
					X:    vref(srcS, resIG),
				}}},
				Tail: &core.CtorApp{
					Base: core.Base{Sp: tsp(), Typ: resIG},
					Ctor: okCtor, Args: []core.Atom{vref(nS, types.Int)}, Arity: 1,
				},
			},
		}
		gfn.Body.Lets[0].X.(*core.EscapeResult).To = gfn
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp:  tsp(),
			Sym: fsym("worker"),
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    types.Fn(types.Unit, types.P(types.Chan(types.Int)), types.P(resIG)),
				Params: []*core.Bind{jB, srcB},
				Body: &core.Body{Tail: &core.Spawn{
					Base: core.Base{Sp: tsp(), Typ: types.Unit},
					Body: &core.Body{
						Lets: []*core.Let{
							{Sp: tsp(), Name: gB, X: &core.Closure{
								Base: core.Base{Sp: tsp(), Typ: types.Fn(resIG, types.P(types.Unit))},
								Fn:   gfn,
							}},
							{Sp: tsp(), Name: &core.Bind{Sp: tsp(), Typ: resIG, Discard: true}, X: &core.CallFunc{
								Base: core.Base{Sp: tsp(), Typ: resIG},
								Fn:   vref(gS, types.Fn(resIG, types.P(types.Unit))),
								Args: []core.Atom{unitLit()},
							}},
							{Sp: tsp(), Name: &core.Bind{Sp: tsp(), Typ: types.Unit, Discard: true}, X: &core.SelectExpr{
								Base: core.Base{Sp: tsp(), Typ: types.Unit},
								Arms: []core.SelArm{
									&core.SelSend{
										Sp:    tsp(),
										Chan:  vref(jS, types.Chan(types.Int)),
										Value: intLit("7"),
										Body:  &core.Body{Tail: unitLit()},
									},
									&core.SelDefault{Sp: tsp(), Body: &core.Body{Tail: unitLit()}},
								},
							}},
							{Sp: tsp(), Name: &core.Bind{Sp: tsp(), Typ: types.Unit, Discard: true}, X: &core.ForRange{
								Base: core.Base{Sp: tsp(), Typ: types.Unit},
								Kind: core.ForChan,
								Iter: vref(jS, types.Chan(types.Int)),
								Elem: &core.Bind{Sp: tsp(), Typ: types.Int, Discard: true},
								Body: &core.Body{Tail: unitLit()},
							}},
						},
						Tail: unitLit(),
					},
				}},
			},
		})
	}

	// --- fn misc (u: Unit) -> Float — CtorRef + GoFuncRef +
	//     RecordNew / RecordUpdate / FieldGet
	{
		uB, _ := vbind("u", types.Unit)
		cB, _ := vbind("mkc", types.Fn(shapeT, types.P(types.Float)))
		itoaB, _ := vbind("itoa", types.Fn(types.String, types.P(types.Int)))
		pB, pS := vbind("p", pointT)
		qB, qS := vbind("q", pointT)
		px := fdsym("x")
		py := fdsym("y")
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp:  tsp(),
			Sym: fsym("misc"),
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    types.Fn(types.Float, types.P(types.Unit)),
				Params: []*core.Bind{uB},
				Body: &core.Body{
					Lets: []*core.Let{
						{Sp: tsp(), Name: cB, X: &core.CtorRef{
							Base: core.Base{Sp: tsp(), Typ: types.Fn(shapeT, types.P(types.Float))},
							Sym:  shapeCt,
						}},
						{Sp: tsp(), Name: itoaB, X: &core.GoFuncRef{
							Base:  core.Base{Sp: tsp(), Typ: types.Fn(types.String, types.P(types.Int))},
							Sym:   gsym("Itoa", "strconv"),
							Arity: 1, Results: core.GoResultsOne,
						}},
						{Sp: tsp(), Name: pB, X: &core.RecordNew{
							Base: core.Base{Sp: tsp(), Typ: pointT},
							Fields: []core.FieldInit{
								{Sp: tsp(), Sym: px, Index: 0, Value: &core.Lit{Base: core.Base{Sp: tsp(), Typ: types.Float}, Kind: syntax.LitFloat, Text: "1.0"}},
								{Sp: tsp(), Sym: py, Index: 1, Value: &core.Lit{Base: core.Base{Sp: tsp(), Typ: types.Float}, Kind: syntax.LitFloat, Text: "2.0"}},
							},
						}},
						{Sp: tsp(), Name: qB, X: &core.RecordUpdate{
							Base: core.Base{Sp: tsp(), Typ: pointT},
							Rec:  vref(pS, pointT),
							Fields: []core.FieldInit{
								{Sp: tsp(), Sym: px, Index: 0, Value: &core.Lit{Base: core.Base{Sp: tsp(), Typ: types.Float}, Kind: syntax.LitFloat, Text: "3.0"}},
							},
						}},
					},
					Tail: &core.FieldGet{
						Base: core.Base{Sp: tsp(), Typ: types.Float},
						Recv: vref(qS, pointT), Field: px, Index: 0,
					},
				},
			},
		})
	}

	// --- fn use_answer (u: Unit) -> Int — VarDecl reference
	{
		uB, _ := vbind("u", types.Unit)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp:  tsp(),
			Sym: fsym("use_answer"),
			Fn: &core.Func{
				Sp:     tsp(),
				Sig:    types.Fn(types.Int, types.P(types.Unit)),
				Params: []*core.Bind{uB},
				Body:   &core.Body{Tail: vref(answer, types.Int)},
			},
		})
	}

	// --- fn regress (p: String) (c: Bool) (def: Int) ->
	//     Result Int GoError — regression coverage:
	//   - `let n = if c { try atoi p } else { def }`: a branch-tail
	//     EscapeCall under Assign must take a fresh variable
	//     (`v1, err := ...; n = v1`), not `n, err :=` — that would
	//     shadow the outer `var n` inside the if-block.
	//   - `let x = def; let x = x + 1`: two lets sharing one Gom
	//     name are distinct symbols — Go needs `x` and `x1`, a
	//     second `x :=` is a redeclaration error.
	//   - `let f = Ok`: a CtorRef of the builtin Result ctor emits
	//     the `result.Ok[T, E]` function value, not a nonexistent
	//     `Result_Ok` struct literal.
	{
		pB, pS := vbind("p", types.String)
		cB, cS := vbind("c", types.Bool)
		dB, dS := vbind("def", types.Int)
		x1B, x1S := vbind("x", types.Int)
		x2B, x2S := vbind("x", types.Int)
		nB, nS := vbind("n", types.Int)
		okFn := types.Fn(resIG, types.P(types.Int))
		fB, fS := vbind("f", okFn)
		fn := &core.Func{
			Sp:     tsp(),
			Sig:    types.Fn(resIG, types.P(types.String), types.P(types.Bool), types.P(types.Int)),
			Params: []*core.Bind{pB, cB, dB},
			Body: &core.Body{
				Lets: []*core.Let{
					{Sp: tsp(), Name: x1B, X: vref(dS, types.Int)},
					{Sp: tsp(), Name: x2B, X: &core.Binary{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Op:   syntax.BinAdd, X: vref(x1S, types.Int), Y: intLit("1"),
					}},
					{Sp: tsp(), Name: nB, X: &core.If{
						Base: core.Base{Sp: tsp(), Typ: types.Int},
						Cond: vref(cS, types.Bool),
						Then: &core.Body{Tail: &core.EscapeCall{
							Base: core.Base{Sp: tsp(), Typ: types.Int},
							Call: &core.CallGo{
								Base:    core.Base{Sp: tsp(), Typ: types.Int},
								Callee:  gsym("Atoi", "strconv"),
								Args:    []core.Atom{vref(pS, types.String)},
								Arity:   1,
								Results: core.GoResultsErr,
							},
						}},
						Else: &core.Body{Tail: vref(x2S, types.Int)},
					}},
					{Sp: tsp(), Name: fB, X: &core.CtorRef{
						Base: core.Base{Sp: tsp(), Typ: okFn},
						Sym:  okCtor,
					}},
				},
				Tail: &core.CallFunc{
					Base: core.Base{Sp: tsp(), Typ: resIG},
					Fn:   vref(fS, okFn), Args: []core.Atom{vref(nS, types.Int)},
				},
			},
		}
		fn.Body.Lets[2].X.(*core.If).Then.Tail.(*core.EscapeCall).To = fn
		f.Decls = append(f.Decls, &core.FuncDecl{Sp: tsp(), Sym: fsym("regress"), Fn: fn})
	}

	return f
}

// fixtureSrc is fake .gom source the fixture spans point into: 40
// lines of 100 bytes so a span offset n*10 lands roughly on line n.
func fixtureSrc() []byte {
	var b []byte
	for i := 0; i < 60; i++ {
		b = append(b, []byte(fmt.Sprintf("// source line %03d %s\n", i,
			"..........................................................................."))...)
	}
	return b
}

// compileFixture rebuilds the fixture with every Go callee and the
// import clause retargeted at the "gostub" package — the stub module
// the compile test writes, whose signatures match the Gom view
// (int64, not int) so the generated code type-checks.
func compileFixture() *core.File {
	f := buildFixture()
	f.Imports[0].Path = "gostub"
	f.Imports[0].Sym = &resolve.Symbol{
		Name: "gostub", Kind: resolve.KindModule, IsGo: true, Package: "gostub"}
	retarget(f, "gostub")
	return f
}

// retarget rewrites every Go callee's package to pkg.
func retarget(f *core.File, pkg string) {
	mut := func(e core.Expr) {
		switch e := e.(type) {
		case *core.CallGo:
			e.Callee = cloneGoSym(e.Callee, pkg)
		case *core.GoFuncRef:
			e.Sym = cloneGoSym(e.Sym, pkg)
		case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
			*core.FieldGet, *core.Unary, *core.Binary,
			*core.CallDirect, *core.CallFunc, *core.CallBuiltin,
			*core.CtorApp, *core.TupleNew, *core.ListNew,
			*core.RecordNew, *core.RecordUpdate, *core.Conv,
			*core.TailCall, *core.EscapeResult, *core.EscapeCall,
			*core.ResultFromGo, *core.OptionFromGo, *core.TupleFromGo,
			*core.Closure, *core.If, *core.Match, *core.SelectExpr,
			*core.Spawn, *core.ForRange:
			// no Go callee reference to rewrite
		default:
			panic("unreachable")
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *core.FuncDecl:
			walkFnExprs(d.Fn, mut)
		case *core.VarDecl:
			walkBodyExprs(d.Body, mut)
		case *core.TestDecl:
			walkFnExprs(d.Fn, mut)
		case *core.TypeDecl:
		default:
			panic("unreachable")
		}
	}
}

func cloneGoSym(s *resolve.Symbol, pkg string) *resolve.Symbol {
	c := *s
	c.Package = pkg
	return &c
}

// walkFnExprs/walkBodyExprs call mut on every expression reachable
// from the function/body — a mutation walk used by retarget.
func walkFnExprs(fn *core.Func, mut func(core.Expr)) {
	if fn != nil {
		walkBodyExprs(fn.Body, mut)
	}
}

func walkBodyExprs(b *core.Body, mut func(core.Expr)) {
	if b == nil {
		return
	}
	for _, l := range b.Lets {
		walkExprNodes(l.X, mut)
	}
	walkExprNodes(b.Tail, mut)
}

func walkExprNodes(e core.Expr, mut func(core.Expr)) {
	mut(e)
	switch e := e.(type) {
	case *core.Closure:
		walkFnExprs(e.Fn, mut)
	case *core.If:
		walkBodyExprs(e.Then, mut)
		walkBodyExprs(e.Else, mut)
	case *core.Match:
		walkDTreeExprs(e.Tree, mut)
	case *core.SelectExpr:
		for _, a := range e.Arms {
			switch a := a.(type) {
			case *core.SelRecv:
				mut(a.Chan)
				walkBodyExprs(a.Body, mut)
			case *core.SelSend:
				mut(a.Chan)
				mut(a.Value)
				walkBodyExprs(a.Body, mut)
			case *core.SelDefault:
				walkBodyExprs(a.Body, mut)
			default:
				panic("unreachable")
			}
		}
	case *core.Spawn:
		walkBodyExprs(e.Body, mut)
	case *core.ForRange:
		mut(e.Iter)
		walkBodyExprs(e.Body, mut)
	case *core.EscapeCall:
		mut(e.Call)
	case *core.ResultFromGo:
		mut(e.Call)
	case *core.OptionFromGo:
		mut(e.Call)
	case *core.TupleFromGo:
		mut(e.Call)
	case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
		*core.GoFuncRef, *core.FieldGet,
		*core.Unary, *core.Binary,
		*core.CallDirect, *core.CallFunc, *core.CallGo, *core.CallBuiltin,
		*core.CtorApp, *core.TupleNew, *core.ListNew,
		*core.RecordNew, *core.RecordUpdate, *core.Conv,
		*core.TailCall, *core.EscapeResult:
		// leaf nodes — no nested function bodies to walk
	default:
		panic("unreachable")
	}
}

func walkDTreeExprs(t core.DTree, mut func(core.Expr)) {
	switch t := t.(type) {
	case *core.DLeaf:
		walkBodyExprs(t.Body, mut)
	case *core.DGuard:
		mut(t.Cond)
		walkDTreeExprs(t.Then, mut)
		walkDTreeExprs(t.Else, mut)
	case *core.DSwitch:
		mut(t.Subj)
		for _, c := range t.Cases {
			walkBodyExprs(c.Body, mut)
		}
		if t.Default != nil {
			walkDTreeExprs(t.Default, mut)
		}
	case *core.DPanic:
		mut(t.Subj)
	default:
		panic("unreachable")
	}
}
