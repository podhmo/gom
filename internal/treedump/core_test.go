package treedump

import (
	"testing"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/gomtest"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// --- builders (the core package's own test helpers are not
// exported, so the dump tests build their fixtures here) ---

// cbase builds a core node base — same `t.gom` spans as the other
// treedump tests so `@start:end` atoms stay readable.
func cbase(a, b int, t types.Type) core.Base { return core.Base{Sp: sp(a, b), Typ: t} }

func cvs(name string) *resolve.Symbol { return &resolve.Symbol{Name: name, Kind: resolve.KindVar} }
func cfs(name string) *resolve.Symbol { return &resolve.Symbol{Name: name, Kind: resolve.KindFunc} }
func ccs(name string) *resolve.Symbol { return &resolve.Symbol{Name: name, Kind: resolve.KindCtor} }
func cts(name string) *resolve.Symbol { return &resolve.Symbol{Name: name, Kind: resolve.KindType} }
func cfsd(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindField}
}
func cgs(name, pkg string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindFunc, IsGo: true, Package: pkg}
}
func cgms(name, pkg string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindMethod, IsGo: true, Package: pkg}
}

// cbind builds a binder; the symbol is returned too so use sites can
// share it.
func cbind(a, b int, name string, t types.Type) (*core.Bind, *resolve.Symbol) {
	s := cvs(name)
	return &core.Bind{Sp: sp(a, b), Sym: s, Typ: t}, s
}

func cvref(a, b int, s *resolve.Symbol, t types.Type) *core.Var {
	return &core.Var{Base: cbase(a, b, t), Sym: s}
}

func cilit(a, b int, text string) *core.Lit {
	return &core.Lit{Base: cbase(a, b, types.Int), Kind: syntax.LitInt, Text: text}
}

func culit(a, b int) *core.Lit {
	return &core.Lit{Base: cbase(a, b, types.Unit), Kind: syntax.LitUnit, Text: "()"}
}

// clet builds one `name = x` binding of a body.
func clet(a, b int, name *core.Bind, x core.Expr) *core.Let {
	return &core.Let{Sp: sp(a, b), Name: name, X: x}
}

// shared types for the fixtures below.
var (
	cshapeT = &types.Con{Name: "Shape"}
	cpointT = &types.Con{Name: "Point"}
	cresIG  = types.Result(types.Int, types.GoError)
	cfnII   = types.Fn(types.Int, types.P(types.Int))
	cfnIII  = types.Fn(types.Int, types.P(types.Int), types.P(types.Int))
	clistI  = types.List(types.Int)
	coptI   = types.Option(types.Int)
	cchanI  = types.Chan(types.Int)
	ctupII  = types.Tup(types.Int, types.Int)
)

// coreSample builds a Core IR file exercising every node family —
// the same coverage as internal/core's okFile fixture, condensed:
//
//	type Shape = | Circle (radius: Float) | Empty
//	type Point = { x: Float, y: Float }
//	type Meters = Float / type Opaque / type Box a = | Box a
//	let answer = 42
//	fn area / classify / sign / head / countdown — all SwitchKinds
//	fn load / fetch — escapes and Go-interop conversions
//	fn apply2 / mk_add / generic — call nodes, arity split, tparams
//	fn misc — closures, refs, literals, select, loops, records
//	test "smoke" — escape through a test boundary
func coreSample() *core.File {
	circle := ccs("Circle")
	empty := ccs("Empty")
	okC := ccs("Ok")
	errC := ccs("Err")
	boxC := ccs("Box")
	radiusF := cfsd("radius")
	pointXF := cfsd("x")
	pointYF := cfsd("y")
	add2 := cfs("add2")
	atoi := cgs("Atoi", "strconv")
	getS := cgs("Get", "cache")
	divmod := cgs("DivMod", "num")
	sum := cgs("Sum", "num")
	writeS := cgms("WriteString", "bytes")
	itoa := cgs("Itoa", "strconv")
	todo := &resolve.Symbol{Name: "todo", Kind: resolve.KindBuiltin}

	f := &core.File{Sp: sp(0, 900), Package: "shapes", PkgSp: sp(8, 14)}
	f.Imports = []core.Import{
		{Sp: sp(16, 31), Path: "strconv", IsGo: true,
			Sym: &resolve.Symbol{Name: "strconv", Kind: resolve.KindModule, IsGo: true, Package: "strconv"}},
		{Sp: sp(32, 47), Path: "shapes/util",
			Sym: &resolve.Symbol{Name: "util", Kind: resolve.KindModule, Package: "shapes/util"}},
	}

	// --- type Shape = | Circle (radius: Float) | Empty
	f.Decls = append(f.Decls, &core.TypeDecl{
		Sp: sp(50, 100), Sym: cts("Shape"),
		Body: &core.TSum{Sp: sp(64, 100), Variants: []core.TVariant{
			{Sp: sp(66, 82), Sym: circle, Fields: []core.TField{
				{Sp: sp(73, 81), Name: "radius", Index: 0, Sym: radiusF, Type: types.Float},
			}},
			{Sp: sp(85, 90), Sym: empty},
		}},
	})

	// --- type Point = { x: Float, y: Float } / Meters / Opaque /
	//     Box a = | Box a  (generic TSum)
	f.Decls = append(f.Decls,
		&core.TypeDecl{
			Sp: sp(102, 132), Sym: cts("Point"),
			Body: &core.TRecord{Sp: sp(115, 132), Fields: []core.TField{
				{Sp: sp(117, 125), Name: "x", Index: 0, Sym: pointXF, Type: types.Float},
				{Sp: sp(126, 134), Name: "y", Index: 1, Sym: pointYF, Type: types.Float},
			}},
		},
		&core.TypeDecl{
			Sp: sp(136, 154), Sym: cts("Meters"),
			Body: &core.TAlias{Sp: sp(148, 153), Type: types.Float},
		},
		&core.TypeDecl{
			Sp: sp(156, 168), Sym: cts("Opaque"),
			Body: &core.TAbstract{Sp: sp(167, 168)},
		},
		&core.TypeDecl{
			Sp: sp(170, 192), Sym: cts("Box"),
			TypeParams: []*types.Var{
				{ID: 0, Name: "a"},
			},
			Body: &core.TSum{Sp: sp(179, 192), Variants: []core.TVariant{
				{Sp: sp(181, 190), Sym: boxC, Fields: []core.TField{
					{Sp: sp(185, 189), Index: 0, Type: &types.Var{ID: 0, Name: "a"}},
				}},
			}},
		},
	)

	// --- let answer = 42
	answer := cvs("answer")
	f.Decls = append(f.Decls, &core.VarDecl{
		Sp: sp(195, 210), Sym: answer,
		Body: &core.Body{Tail: cilit(207, 209, "42")},
	})

	// --- fn area (s: Shape) -> Float: SwitchType + DCase binds +
	//     named FieldGet + DPanic
	{
		sB, sS := cbind(220, 221, "s", cshapeT)
		cB, cS := cbind(232, 233, "c", cshapeT)
		rB, rS := cbind(240, 241, "r", types.Float)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: sp(215, 280), Sym: cfs("area"),
			Fn: &core.Func{
				Sp:     sp(215, 280),
				Sig:    types.Fn(types.Float, types.P(cshapeT)),
				Params: []*core.Bind{sB},
				Body: &core.Body{Tail: &core.Match{
					Base: cbase(230, 280, types.Float),
					Tree: &core.DSwitch{
						Base: cbase(230, 280, types.Float),
						Subj: cvref(236, 237, sS, cshapeT), Kind: core.SwitchType,
						Cases: []core.DCase{
							{
								Sp:    sp(240, 265),
								Test:  &core.DCtorTest{Sp: sp(240, 246), Ctor: circle},
								Binds: []*core.Bind{cB},
								Body: &core.Body{
									Lets: []*core.Let{
										clet(248, 260, rB, &core.FieldGet{
											Base:  cbase(252, 259, types.Float),
											Recv:  cvref(252, 253, cS, cshapeT),
											Field: radiusF, Index: 0,
										}),
									},
									Tail: &core.Binary{
										Base: cbase(261, 265, types.Float),
										Op:   syntax.BinMul,
										X:    cvref(261, 262, rS, types.Float),
										Y:    &core.Lit{Base: cbase(263, 265, types.Float), Kind: syntax.LitFloat, Text: "2.0"},
									},
								},
							},
							{
								Sp:   sp(266, 272),
								Test: &core.DCtorTest{Sp: sp(266, 271), Ctor: empty},
								Body: &core.Body{Tail: &core.Lit{
									Base: cbase(271, 272, types.Float), Kind: syntax.LitFloat, Text: "0.0",
								}},
							},
						},
						Default: &core.DPanic{
							Base:     cbase(273, 279, types.Float),
							Subj:     cvref(273, 274, sS, cshapeT),
							TypeName: "shapes.Shape",
						},
					},
				}},
			},
		})
	}

	// --- fn add2 (a: Int) (b: Int) -> Int = a + b
	{
		aB, aS := cbind(290, 291, "a", types.Int)
		bB, bS := cbind(293, 294, "b", types.Int)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: sp(285, 305), Sym: add2,
			Fn: &core.Func{
				Sp:     sp(285, 305),
				Sig:    cfnIII,
				Params: []*core.Bind{aB, bB},
				Body: &core.Body{Tail: &core.Binary{
					Base: cbase(298, 303, types.Int),
					Op:   syntax.BinAdd, X: cvref(298, 299, aS, types.Int), Y: cvref(301, 302, bS, types.Int),
				}},
			},
		})
	}

	// --- fn classify (r: Result Int GoError) -> Int: SwitchTag,
	//     payload binds, a discard bind, Unary
	{
		rB, rS := cbind(315, 316, "r", cresIG)
		nB, nS := cbind(325, 326, "n", types.Int)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: sp(310, 350), Sym: cfs("classify"),
			Fn: &core.Func{
				Sp:     sp(310, 350),
				Sig:    types.Fn(types.Int, types.P(cresIG)),
				Params: []*core.Bind{rB},
				Body: &core.Body{Tail: &core.Match{
					Base: cbase(322, 350, types.Int),
					Tree: &core.DSwitch{
						Base: cbase(322, 350, types.Int),
						Subj: cvref(328, 329, rS, cresIG), Kind: core.SwitchTag,
						Cases: []core.DCase{
							{
								Sp:    sp(330, 336),
								Test:  &core.DCtorTest{Sp: sp(330, 332), Ctor: okC},
								Binds: []*core.Bind{nB},
								Body:  &core.Body{Tail: cvref(335, 336, nS, types.Int)},
							},
							{
								Sp:    sp(337, 346),
								Test:  &core.DCtorTest{Sp: sp(337, 340), Ctor: errC},
								Binds: []*core.Bind{{Sp: sp(341, 342), Typ: types.GoError, Discard: true}},
								Body: &core.Body{Tail: &core.Unary{
									Base: cbase(344, 346, types.Int),
									Op:   syntax.UnNeg, X: cilit(345, 346, "1"),
								}},
							},
						},
						Default: &core.DPanic{
							Base:     cbase(347, 349, types.Int),
							Subj:     cvref(347, 348, rS, cresIG),
							TypeName: "Result",
						},
					},
				}},
			},
		})
	}

	// --- fn sign (n: Int) -> Int: SwitchValue + DLitTest + DGuard +
	//     DLeaf
	{
		nB, nS := cbind(360, 361, "n", types.Int)
		gB, gS := cbind(368, 369, "g", types.Bool)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: sp(355, 395), Sym: cfs("sign"),
			Fn: &core.Func{
				Sp:     sp(355, 395),
				Sig:    cfnII,
				Params: []*core.Bind{nB},
				Body: &core.Body{
					Lets: []*core.Let{
						clet(368, 376, gB, &core.Binary{
							Base: cbase(372, 376, types.Bool),
							Op:   syntax.BinGt, X: cvref(372, 373, nS, types.Int), Y: cilit(375, 376, "0"),
						}),
					},
					Tail: &core.Match{
						Base: cbase(380, 395, types.Int),
						Tree: &core.DSwitch{
							Base: cbase(380, 395, types.Int),
							Subj: cvref(386, 387, nS, types.Int), Kind: core.SwitchValue,
							Cases: []core.DCase{{
								Sp:   sp(388, 390),
								Test: &core.DLitTest{Sp: sp(388, 389), Kind: syntax.LitInt, Text: "0"},
								Body: &core.Body{Tail: cilit(390, 391, "0")},
							}},
							Default: &core.DGuard{
								Base: cbase(392, 395, types.Int),
								Cond: cvref(392, 393, gS, types.Bool),
								Then: &core.DLeaf{
									Base: cbase(393, 394, types.Int),
									Body: &core.Body{Tail: cilit(393, 394, "1")},
								},
								Else: &core.DLeaf{
									Base: cbase(394, 395, types.Int),
									Body: &core.Body{Tail: &core.Unary{
										Base: cbase(394, 395, types.Int),
										Op:   syntax.UnNeg, X: cilit(394, 395, "1"),
									}},
								},
							},
						},
					},
				},
			},
		})
	}

	// --- fn head (xs: List Int) -> Int: SwitchList + DListTest with
	//     two binds
	{
		xsB, xsS := cbind(405, 407, "xs", clistI)
		hB, hS := cbind(415, 416, "h", types.Int)
		tB, _ := cbind(418, 419, "t", clistI)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: sp(400, 440), Sym: cfs("head"),
			Fn: &core.Func{
				Sp:     sp(400, 440),
				Sig:    types.Fn(types.Int, types.P(clistI)),
				Params: []*core.Bind{xsB},
				Body: &core.Body{Tail: &core.Match{
					Base: cbase(412, 440, types.Int),
					Tree: &core.DSwitch{
						Base: cbase(412, 440, types.Int),
						Subj: cvref(418, 420, xsS, clistI), Kind: core.SwitchList,
						Cases: []core.DCase{
							{
								Sp:   sp(420, 426),
								Test: &core.DListTest{Sp: sp(420, 422), Cons: false},
								Body: &core.Body{Tail: cilit(425, 426, "0")},
							},
							{
								Sp:    sp(427, 435),
								Test:  &core.DListTest{Sp: sp(427, 433), Cons: true},
								Binds: []*core.Bind{hB, tB},
								Body:  &core.Body{Tail: cvref(434, 435, hS, types.Int)},
							},
						},
						Default: &core.DPanic{
							Base:     cbase(436, 439, types.Int),
							Subj:     cvref(436, 438, xsS, clistI),
							TypeName: "List",
						},
					},
				}},
			},
		})
	}

	// --- fn countdown (n: Int) -> Int: :tailloop + TailCall inside an
	//     If else-branch
	{
		nB, nS := cbind(450, 451, "n", types.Int)
		zB, zS := cbind(458, 459, "z", types.Bool)
		mB, mS := cbind(466, 467, "m", types.Int)
		fn := &core.Func{
			Sp:       sp(445, 490),
			Sig:      cfnII,
			Params:   []*core.Bind{nB},
			TailLoop: true,
			Body: &core.Body{
				Lets: []*core.Let{
					clet(458, 465, zB, &core.Binary{
						Base: cbase(462, 465, types.Bool),
						Op:   syntax.BinEq, X: cvref(462, 463, nS, types.Int), Y: cilit(464, 465, "0"),
					}),
					clet(466, 473, mB, &core.Binary{
						Base: cbase(470, 473, types.Int),
						Op:   syntax.BinSub, X: cvref(470, 471, nS, types.Int), Y: cilit(472, 473, "1"),
					}),
				},
				Tail: &core.If{
					Base: cbase(478, 490, types.Int),
					Cond: cvref(481, 482, zS, types.Bool),
					Then: &core.Body{Tail: cilit(485, 486, "0")},
					Else: &core.Body{Tail: &core.TailCall{
						Base: cbase(488, 490, types.Int),
						Args: []core.Atom{cvref(489, 490, mS, types.Int)},
					}},
				},
			},
		}
		fn.Body.Tail.(*core.If).Else.Tail.(*core.TailCall).To = fn
		f.Decls = append(f.Decls, &core.FuncDecl{Sp: sp(445, 490), Sym: cfs("countdown"), Fn: fn})
	}

	// --- fn load (p: String) -> Result Int GoError: EscapeCall
	//     (fused try over a Go error call) + wrap, CtorApp
	{
		pB, pS := cbind(500, 501, "p", types.String)
		nB, nS := cbind(510, 511, "n", types.Int)
		fn := &core.Func{
			Sp:     sp(495, 530),
			Sig:    types.Fn(cresIG, types.P(types.String)),
			Params: []*core.Bind{pB},
			Body: &core.Body{
				Lets: []*core.Let{
					clet(510, 525, nB, &core.EscapeCall{
						Base: cbase(510, 525, types.Int),
						Call: &core.CallGo{
							Base:    cbase(514, 524, types.Int),
							Callee:  atoi,
							Args:    []core.Atom{cvref(519, 520, pS, types.String)},
							Arity:   1,
							Results: core.GoResultsErr,
						},
						Wrap: "load",
					}),
				},
				Tail: &core.CtorApp{
					Base: cbase(527, 530, cresIG),
					Ctor: okC, Args: []core.Atom{cvref(529, 530, nS, types.Int)}, Arity: 1,
				},
			},
		}
		fn.Body.Lets[0].X.(*core.EscapeCall).To = fn
		f.Decls = append(f.Decls, &core.FuncDecl{Sp: sp(495, 530), Sym: cfs("load"), Fn: fn})
	}

	// --- fn fetch (u: Unit) -> Result Int GoError: ResultFromGo,
	//     EscapeResult, Conv, OptionFromGo, TupleFromGo
	{
		uB, _ := cbind(540, 541, "u", types.Unit)
		resB, resS := cbind(548, 551, "res", cresIG)
		nB, nS := cbind(560, 561, "n", types.Int)
		i64B, _ := cbind(570, 573, "i64", types.Int64)
		optB, _ := cbind(580, 583, "opt", coptI)
		tupB, _ := cbind(590, 593, "tup", types.Tup(types.Int, types.Bool))
		outB, outS := cbind(600, 603, "out", cresIG)
		fn := &core.Func{
			Sp:     sp(535, 620),
			Sig:    types.Fn(cresIG, types.P(types.Unit)),
			Params: []*core.Bind{uB},
			Body: &core.Body{
				Lets: []*core.Let{
					clet(548, 558, resB, &core.ResultFromGo{
						Base: cbase(554, 558, cresIG),
						Call: &core.CallGo{
							Base:   cbase(554, 558, types.Int),
							Callee: atoi,
							Args: []core.Atom{&core.Lit{
								Base: cbase(555, 557, types.String), Kind: syntax.LitStr, Text: `"1"`,
							}},
							Arity: 1, Results: core.GoResultsErr,
						},
					}),
					clet(560, 568, nB, &core.EscapeResult{
						Base: cbase(564, 568, types.Int),
						X:    cvref(568, 571, resS, cresIG),
					}),
					clet(570, 578, i64B, &core.Conv{
						Base: cbase(574, 578, types.Int64),
						Kind: core.ConvNum, X: cvref(578, 579, nS, types.Int),
					}),
					clet(580, 588, optB, &core.OptionFromGo{
						Base: cbase(584, 588, coptI),
						Call: &core.CallGo{
							Base:   cbase(584, 588, types.Int),
							Callee: getS,
							Args:   []core.Atom{cilit(587, 588, "0")},
							Arity:  1, Results: core.GoResultsOK,
						},
					}),
					clet(590, 598, tupB, &core.TupleFromGo{
						Base: cbase(594, 598, types.Tup(types.Int, types.Bool)),
						Call: &core.CallGo{
							Base:   cbase(594, 598, types.Tup(types.Int, types.Bool)),
							Callee: divmod,
							Args:   []core.Atom{cilit(597, 598, "7"), cilit(598, 599, "2")},
							Arity:  2, Results: core.GoResultsPair,
						},
					}),
					clet(600, 608, outB, &core.CtorApp{
						Base: cbase(604, 608, cresIG),
						Ctor: okC, Args: []core.Atom{cvref(607, 608, nS, types.Int)}, Arity: 1,
					}),
				},
				Tail: cvref(615, 618, outS, cresIG),
			},
		}
		fn.Body.Lets[1].X.(*core.EscapeResult).To = fn
		f.Decls = append(f.Decls, &core.FuncDecl{Sp: sp(535, 620), Sym: cfs("fetch"), Fn: fn})
	}

	// --- fn apply2 (f: Int -> Int) -> Int: CallFunc on a function
	//     value
	{
		fB, fS := cbind(630, 631, "f", cfnII)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: sp(625, 640), Sym: cfs("apply2"),
			Fn: &core.Func{
				Sp:     sp(625, 640),
				Sig:    types.Fn(types.Int, types.P(cfnII)),
				Params: []*core.Bind{fB},
				Body: &core.Body{Tail: &core.CallFunc{
					Base: cbase(634, 638, types.Int),
					Fn:   cvref(634, 635, fS, cfnII),
					Args: []core.Atom{cilit(636, 637, "2")},
				}},
			},
		})
	}

	// --- fn misc (u: Unit) (jobs: Chan Int) -> Unit: FuncRef,
	//     CtorRef, GoFuncRef (free + bound method), Closure partial
	//     application, literals, select arms, loops, records, builtin,
	//     spread + method CallGo, Conv slice-to-go, positional FieldGet
	{
		uB, _ := cbind(650, 651, "u", types.Unit)
		jB, jS := cbind(653, 657, "jobs", cchanI)
		pB, _ := cbind(660, 661, "p", cfnIII)
		iB, iS := cbind(668, 669, "inc", cfnII)
		lB, lS := cbind(676, 677, "l", clistI)
		l2B, l2S := cbind(684, 686, "l2", clistI)
		tB, tS := cbind(692, 693, "t", ctupII)
		cB, _ := cbind(700, 701, "c", cshapeT)
		fvB, _ := cbind(708, 709, "fv", cfnII)
		bufB, bufS := cbind(716, 719, "buf", types.GoNamed("bytes", "Buffer"))
		wB, _ := cbind(724, 725, "w", cfnII)
		sB, _ := cbind(732, 733, "s", types.String)
		slB, slS := cbind(740, 742, "sl", types.GoSlice(types.Int))
		nB, _ := cbind(748, 749, "n", types.Int)
		mB, _ := cbind(756, 757, "m", types.Int)
		bxB, _ := cbind(764, 766, "bx", &types.Con{Name: "Box", Args: []types.Type{types.Int}})
		jB2, _ := cbind(772, 773, "j", types.Int)
		rB, _ := cbind(778, 779, "r", coptI)
		eB, _ := cbind(788, 789, "e", types.Int)
		iB2, _ := cbind(796, 797, "i", types.Int)
		ptB, ptS := cbind(804, 806, "pt", cpointT)
		pt2B, pt2S := cbind(812, 815, "pt2", cpointT)
		fxB, _ := cbind(820, 822, "fx", types.Float)
		i2B, _ := cbind(828, 830, "i2", types.Int)

		clB, clS := cbind(662, 663, "b", types.Int)
		partial := &core.Closure{
			Base: cbase(664, 676, cfnII),
			Fn: &core.Func{
				Sp:     sp(664, 676),
				Sig:    cfnII,
				Params: []*core.Bind{clB},
				Body: &core.Body{Tail: &core.CallDirect{
					Base:   cbase(670, 676, types.Int),
					Callee: add2, Args: []core.Atom{cilit(671, 672, "1"), cvref(674, 675, clS, types.Int)},
					Arity: 2,
				}},
			},
		}

		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: sp(645, 840), Sym: cfs("misc"),
			Fn: &core.Func{
				Sp:     sp(645, 840),
				Sig:    types.Fn(types.Unit, types.P(types.Unit), types.P(cchanI)),
				Params: []*core.Bind{uB, jB},
				Body: &core.Body{
					Lets: []*core.Let{
						clet(660, 666, pB, &core.FuncRef{
							Base: cbase(664, 668, cfnIII),
							Sym:  add2, Arity: 2,
						}),
						clet(668, 676, iB, partial),
						clet(676, 682, lB, &core.ListNew{
							Base:  cbase(680, 682, clistI),
							Elems: []core.Atom{cilit(681, 682, "1"), cilit(683, 684, "2")},
						}),
						clet(684, 690, l2B, &core.ListNew{
							Base:  cbase(688, 690, clistI),
							Elems: []core.Atom{cilit(689, 690, "3")},
							Rest:  cvref(690, 692, lS, clistI),
						}),
						clet(692, 698, tB, &core.TupleNew{
							Base:  cbase(695, 698, ctupII),
							Elems: []core.Atom{cilit(696, 697, "1"), cilit(697, 698, "2")},
						}),
						clet(700, 706, cB, &core.CtorRef{
							Base: cbase(703, 709, types.Fn(cshapeT, types.P(types.Float))),
							Sym:  circle,
						}),
						clet(708, 714, fvB, &core.GoFuncRef{
							Base: cbase(711, 715, cfnII),
							Sym:  itoa, Arity: 1, Results: core.GoResultsOne,
						}),
						clet(716, 722, bufB, &core.CallBuiltin{
							Base: cbase(719, 723, types.GoNamed("bytes", "Buffer")),
							Sym:  todo, Args: []core.Atom{culit(722, 724)},
						}),
						clet(724, 730, wB, &core.GoFuncRef{
							Base: cbase(727, 731, cfnII),
							Sym:  writeS, Recv: cvref(728, 731, bufS, types.GoNamed("bytes", "Buffer")),
							Arity: 1, Results: core.GoResultsErrOnly,
						}),
						clet(732, 738, sB, &core.CallGo{
							Base:   cbase(735, 739, types.String),
							Callee: itoa,
							Args:   []core.Atom{cilit(738, 739, "5")},
							Arity:  1, Results: core.GoResultsOne,
						}),
						clet(740, 746, slB, &core.Conv{
							Base: cbase(743, 747, types.GoSlice(types.Int)),
							Kind: core.ConvSliceToGo, X: cvref(747, 748, l2S, clistI),
						}),
						clet(748, 754, nB, &core.CallGo{
							Base:   cbase(751, 755, types.Int),
							Callee: sum,
							Args:   []core.Atom{cvref(754, 756, slS, types.GoSlice(types.Int))},
							Arity:  1, Results: core.GoResultsOne,
							Variadic: true, Spread: true,
						}),
						clet(756, 762, mB, &core.CallGo{
							Base:   cbase(759, 763, types.Int),
							Callee: writeS,
							Recv:   cvref(760, 763, bufS, types.GoNamed("bytes", "Buffer")),
							Args:   []core.Atom{cilit(763, 764, "0")},
							Arity:  1, Results: core.GoResultsErrOnly,
						}),
						clet(764, 770, bxB, &core.CtorApp{
							Base: cbase(767, 771, &types.Con{Name: "Box", Args: []types.Type{types.Int}}),
							Ctor: boxC, TypeArgs: []types.Type{types.Int},
							Args: []core.Atom{cilit(770, 771, "7")}, Arity: 1,
						}),
						clet(772, 786, jB2, &core.SelectExpr{
							Base: cbase(775, 786, types.Int),
							Arms: []core.SelArm{
								&core.SelRecv{
									Sp:   sp(778, 782),
									Bind: rB,
									Chan: cvref(780, 784, jS, cchanI),
									Body: &core.Body{Tail: cilit(785, 786, "0")},
								},
								&core.SelSend{
									Sp:    sp(788, 792),
									Chan:  cvref(789, 792, jS, cchanI),
									Value: cilit(793, 794, "9"),
									Body:  &core.Body{Tail: cilit(795, 796, "1")},
								},
								&core.SelDefault{Sp: sp(798, 800), Body: &core.Body{Tail: cilit(799, 800, "2")}},
							},
						}),
						clet(788, 794, &core.Bind{Sp: sp(790, 791), Typ: types.Unit, Discard: true},
							&core.ForRange{
								Base: cbase(791, 794, types.Unit),
								Kind: core.ForList,
								Iter: cvref(794, 796, l2S, clistI),
								Elem: &core.Bind{Sp: sp(795, 796), Typ: types.Int, Discard: true},
								Body: &core.Body{Tail: culit(797, 799)},
							}),
						clet(800, 806, &core.Bind{Sp: sp(802, 803), Typ: types.Unit, Discard: true},
							&core.ForRange{
								Base: cbase(803, 806, types.Unit),
								Kind: core.ForChan,
								Iter: cvref(806, 810, jS, cchanI),
								Elem: eB,
								Body: &core.Body{Tail: culit(807, 809)},
							}),
						clet(812, 818, &core.Bind{Sp: sp(814, 815), Typ: types.Unit, Discard: true},
							&core.Spawn{
								Base: cbase(815, 818, types.Unit),
								Body: &core.Body{Tail: culit(817, 819)},
							}),
						clet(820, 826, iB2, &core.CallFunc{
							Base: cbase(823, 826, types.Int),
							Fn:   cvref(823, 826, iS, cfnII),
							Args: []core.Atom{cilit(826, 827, "3")},
						}),
						clet(796, 802, ptB, &core.RecordNew{
							Base: cbase(799, 802, cpointT),
							Fields: []core.FieldInit{
								{Sp: sp(800, 801), Sym: pointXF, Index: 0,
									Value: &core.Lit{Base: cbase(800, 801, types.Float), Kind: syntax.LitFloat, Text: "1.0"}},
								{Sp: sp(801, 802), Sym: pointYF, Index: 1,
									Value: &core.Lit{Base: cbase(801, 802, types.Float), Kind: syntax.LitFloat, Text: "2.0"}},
							},
						}),
						clet(804, 810, pt2B, &core.RecordUpdate{
							Base: cbase(807, 810, cpointT),
							Rec:  cvref(808, 810, ptS, cpointT),
							Fields: []core.FieldInit{
								{Sp: sp(809, 810), Sym: pointXF, Index: 0,
									Value: &core.Lit{Base: cbase(809, 810, types.Float), Kind: syntax.LitFloat, Text: "3.0"}},
							},
						}),
						clet(812, 818, fxB, &core.FieldGet{
							Base:  cbase(815, 818, types.Float),
							Recv:  cvref(815, 818, pt2S, cpointT),
							Field: pointXF, Index: 0,
						}),
						clet(828, 834, i2B, &core.FieldGet{
							Base: cbase(831, 834, types.Int),
							Recv: cvref(831, 832, tS, ctupII),
							// positional projection: no field symbol
							Index: 1,
						}),
					},
					Tail: culit(836, 838),
				},
			},
		})
	}

	// --- fn mk_add (a: Int) -> (Int -> Int): arity-split — Sig has
	//     two params, one binder, so residual=(Int -> Int) shows.
	{
		aB, aS := cbind(840, 841, "a", types.Int)
		bB, bS := cbind(848, 849, "b", types.Int)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: sp(835, 860), Sym: cfs("mk_add"),
			Fn: &core.Func{
				Sp:     sp(835, 860),
				Sig:    cfnIII,
				Params: []*core.Bind{aB},
				Body: &core.Body{Tail: &core.Closure{
					Base: cbase(844, 860, cfnII),
					Fn: &core.Func{
						Sp:     sp(844, 860),
						Sig:    cfnII,
						Params: []*core.Bind{bB},
						Body: &core.Body{Tail: &core.Binary{
							Base: cbase(852, 857, types.Int),
							Op:   syntax.BinAdd, X: cvref(852, 853, aS, types.Int), Y: cvref(855, 856, bS, types.Int),
						}},
					},
				}},
			},
		})
	}

	// --- fn id (x: a) -> a: declared type params
	{
		tvA := &types.Var{ID: 0, Name: "a"}
		xB, xS := cbind(870, 871, "x", tvA)
		f.Decls = append(f.Decls, &core.FuncDecl{
			Sp: sp(865, 880), Sym: cfs("id"),
			TypeParams: []*types.Var{tvA},
			Fn: &core.Func{
				Sp:     sp(865, 880),
				Sig:    types.Fn(tvA, types.P(tvA)),
				Params: []*core.Bind{xB},
				Body:   &core.Body{Tail: cvref(876, 877, xS, tvA)},
			},
		})
	}

	// --- test "smoke": an escape targeting the test boundary
	{
		res2B, res2S := cbind(890, 894, "res2", cresIG)
		n2B, _ := cbind(896, 898, "n2", types.Int)
		tf := &core.Func{
			Sp:     sp(885, 910),
			Sig:    types.Fn(types.Unit),
			Params: nil,
			Body: &core.Body{
				Lets: []*core.Let{
					clet(890, 894, res2B, cvref(890, 895, cvs("res_in"), cresIG)),
					clet(896, 902, n2B, &core.EscapeResult{
						Base: cbase(899, 902, types.Int),
						X:    cvref(900, 905, res2S, cresIG),
					}),
				},
				Tail: culit(906, 908),
			},
		}
		tf.Body.Lets[1].X.(*core.EscapeResult).To = tf
		f.Decls = append(f.Decls, &core.TestDecl{
			Sp: sp(882, 910), Name: "smoke", NameSp: sp(887, 893), Fn: tf,
		})
	}

	return f
}

func TestDumpCore(t *testing.T) {
	got := DumpCore(coreSample())
	want := `(file@0:900 shapes@8:14
  (import@16:31 "strconv" strconv%1 :go)
  (import@32:47 "shapes/util" util%2)
  (type@50:100 Shape%3
    (sum@64:100
      (variant@66:82 Circle%4 (field@73:81 :Float radius -> radius%5 :i0))
      (variant@85:90 Empty%6)))
  (type@102:132 Point%7
    (record@115:132
      (field@117:125 :Float x -> x%8 :i0)
      (field@126:134 :Float y -> y%9 :i1)))
  (type@136:154 Meters%10 (alias@148:153 :Float))
  (type@156:168 Opaque%11 (abstract@167:168))
  (type@170:192 Box%12
    (tparams a)
    (sum@179:192 (variant@181:190 Box%13 (field@185:189 :a :i0))))
  (let@195:210 answer%14 (body (tail (lit@207:209 :Int int 42))))
  (fn@215:280 area%15 :(Shape -> Float)
    (params (param@220:221 :Shape s%16))
    (body
      (tail
        (match@230:280 :Float
          (dswitch@230:280 :Float type
            (var@236:237 :Shape s%16)
            (case@240:265
              (ctortest@240:246 Circle%4)
              (bind@232:233 :Shape c%17)
              (body
                (let@248:260 :Float r%18@240:241 (fieldget@252:259 :Float radius%5 :i0 (var@252:253 :Shape c%17)))
                (tail
                  (binary@261:265 :Float *
                    (var@261:262 :Float r%18)
                    (lit@263:265 :Float float 2.0)))))
            (case@266:272
              (ctortest@266:271 Empty%6)
              (body (tail (lit@271:272 :Float float 0.0))))
            (default (panic@273:279 :Float "shapes.Shape" (var@273:274 :Shape s%16))))))))
  (fn@285:305 add2%19 :(Int -> Int -> Int)
    (params
      (param@290:291 :Int a%20)
      (param@293:294 :Int b%21))
    (body
      (tail
        (binary@298:303 :Int +
          (var@298:299 :Int a%20)
          (var@301:302 :Int b%21)))))
  (fn@310:350 classify%22 :(Result Int GoError -> Int)
    (params (param@315:316 :(Result Int GoError) r%23))
    (body
      (tail
        (match@322:350 :Int
          (dswitch@322:350 :Int tag
            (var@328:329 :(Result Int GoError) r%23)
            (case@330:336
              (ctortest@330:332 Ok%24)
              (bind@325:326 :Int n%25)
              (body (tail (var@335:336 :Int n%25))))
            (case@337:346
              (ctortest@337:340 Err%26)
              (bind@341:342 :GoError _)
              (body (tail (unary@344:346 :Int - (lit@345:346 :Int int 1)))))
            (default (panic@347:349 :Int "Result" (var@347:348 :(Result Int GoError) r%23))))))))
  (fn@355:395 sign%27 :(Int -> Int)
    (params (param@360:361 :Int n%28))
    (body
      (let@368:376 :Bool g%29@368:369
        (binary@372:376 :Bool >
          (var@372:373 :Int n%28)
          (lit@375:376 :Int int 0)))
      (tail
        (match@380:395 :Int
          (dswitch@380:395 :Int value
            (var@386:387 :Int n%28)
            (case@388:390
              (littest@388:389 int 0)
              (body (tail (lit@390:391 :Int int 0))))
            (default
              (guard@392:395 :Int
                (var@392:393 :Bool g%29)
                (leaf@393:394 :Int (body (tail (lit@393:394 :Int int 1))))
                (leaf@394:395 :Int (body (tail (unary@394:395 :Int - (lit@394:395 :Int int 1))))))))))))
  (fn@400:440 head%30 :(List Int -> Int)
    (params (param@405:407 :(List Int) xs%31))
    (body
      (tail
        (match@412:440 :Int
          (dswitch@412:440 :Int list
            (var@418:420 :(List Int) xs%31)
            (case@420:426
              (listtest@420:422 nil)
              (body (tail (lit@425:426 :Int int 0))))
            (case@427:435
              (listtest@427:433 cons)
              (bind@415:416 :Int h%32)
              (bind@418:419 :(List Int) t%33)
              (body (tail (var@434:435 :Int h%32))))
            (default (panic@436:439 :Int "List" (var@436:438 :(List Int) xs%31))))))))
  (fn@445:490 countdown%34 :(Int -> Int) :tailloop
    (params (param@450:451 :Int n%35))
    (body
      (let@458:465 :Bool z%36@458:459
        (binary@462:465 :Bool ==
          (var@462:463 :Int n%35)
          (lit@464:465 :Int int 0)))
      (let@466:473 :Int m%37@466:467
        (binary@470:473 :Int -
          (var@470:471 :Int n%35)
          (lit@472:473 :Int int 1)))
      (tail
        (if@478:490 :Int
          (var@481:482 :Bool z%36)
          (body (tail (lit@485:486 :Int int 0)))
          (body (tail (tailcall@488:490 :Int to=countdown%34 (var@489:490 :Int m%37))))))))
  (fn@495:530 load%38 :(String -> Result Int GoError)
    (params (param@500:501 :String p%39))
    (body
      (let@510:525 :Int n%40@510:511 (escapecall@510:525 :Int to=load%38 wrap="load" (callgo@514:524 :Int Atoi%41 arity=1 results=err (var@519:520 :String p%39))))
      (tail (ctorapp@527:530 :(Result Int GoError) Ok%24 arity=1 (var@529:530 :Int n%40)))))
  (fn@535:620 fetch%42 :(Unit -> Result Int GoError)
    (params (param@540:541 :Unit u%43))
    (body
      (let@548:558 :(Result Int GoError) res%44@548:551 (resultfromgo@554:558 :(Result Int GoError) (callgo@554:558 :Int Atoi%41 arity=1 results=err (lit@555:557 :String string "\"1\""))))
      (let@560:568 :Int n%45@560:561 (escaperesult@564:568 :Int to=fetch%42 (var@568:571 :(Result Int GoError) res%44)))
      (let@570:578 :Int64 i64%46@570:573 (conv@574:578 :Int64 num (var@578:579 :Int n%45)))
      (let@580:588 :(Option Int) opt%47@580:583 (optionfromgo@584:588 :(Option Int) (callgo@584:588 :Int Get%48 arity=1 results=ok (lit@587:588 :Int int 0))))
      (let@590:598 :((Int, Bool)) tup%49@590:593
        (tuplefromgo@594:598 :((Int, Bool))
          (callgo@594:598 :((Int, Bool)) DivMod%50 arity=2 results=pair
            (lit@597:598 :Int int 7)
            (lit@598:599 :Int int 2))))
      (let@600:608 :(Result Int GoError) out%51@600:603 (ctorapp@604:608 :(Result Int GoError) Ok%24 arity=1 (var@607:608 :Int n%45)))
      (tail (var@615:618 :(Result Int GoError) out%51))))
  (fn@625:640 apply2%52 :((Int -> Int) -> Int)
    (params (param@630:631 :(Int -> Int) f%53))
    (body
      (tail
        (callfunc@634:638 :Int
          (var@634:635 :(Int -> Int) f%53)
          (lit@636:637 :Int int 2)))))
  (fn@645:840 misc%54 :(Unit -> Chan Int -> Unit)
    (params
      (param@650:651 :Unit u%55)
      (param@653:657 :(Chan Int) jobs%56))
    (body
      (let@660:666 :(Int -> Int -> Int) p%57@660:661 (funcref@664:668 :(Int -> Int -> Int) add2%19 arity=2))
      (let@668:676 :(Int -> Int) inc%58@668:669
        (closure@664:676 :(Int -> Int)
          (fn@664:676 :(Int -> Int)
            (params (param@662:663 :Int b%59))
            (body
              (tail
                (calldirect@670:676 :Int add2%19 arity=2
                  (lit@671:672 :Int int 1)
                  (var@674:675 :Int b%59)))))))
      (let@676:682 :(List Int) l%60@676:677
        (list@680:682 :(List Int)
          (lit@681:682 :Int int 1)
          (lit@683:684 :Int int 2)))
      (let@684:690 :(List Int) l2%61@684:686
        (list@688:690 :(List Int)
          (lit@689:690 :Int int 3)
          (rest (var@690:692 :(List Int) l%60))))
      (let@692:698 :((Int, Int)) t%62@692:693
        (tuple@695:698 :((Int, Int))
          (lit@696:697 :Int int 1)
          (lit@697:698 :Int int 2)))
      (let@700:706 :Shape c%63@700:701 (ctorref@703:709 :(Float -> Shape) Circle%4))
      (let@708:714 :(Int -> Int) fv%64@708:709 (gofuncref@711:715 :(Int -> Int) Itoa%65 arity=1 results=one))
      (let@716:722 :bytes.Buffer buf%66@716:719 (builtin@719:723 :bytes.Buffer todo%67 (lit@722:724 :Unit unit "()")))
      (let@724:730 :(Int -> Int) w%68@724:725 (gofuncref@727:731 :(Int -> Int) WriteString%69 arity=1 results=err-only (recv (var@728:731 :bytes.Buffer buf%66))))
      (let@732:738 :String s%70@732:733 (callgo@735:739 :String Itoa%65 arity=1 results=one (lit@738:739 :Int int 5)))
      (let@740:746 :(Go.Slice Int) sl%71@740:742 (conv@743:747 :(Go.Slice Int) slice-to-go (var@747:748 :(List Int) l2%61)))
      (let@748:754 :Int n%72@748:749 (callgo@751:755 :Int Sum%73 arity=1 results=one variadic spread (var@754:756 :(Go.Slice Int) sl%71)))
      (let@756:762 :Int m%74@756:757
        (callgo@759:763 :Int WriteString%69 arity=1 results=err-only
          (recv (var@760:763 :bytes.Buffer buf%66))
          (lit@763:764 :Int int 0)))
      (let@764:770 :(Box Int) bx%75@764:766 (ctorapp@767:771 :(Box Int) Box%13 arity=1 targs=[Int] (lit@770:771 :Int int 7)))
      (let@772:786 :Int j%76@772:773
        (select@775:786 :Int
          (recv@778:782
            (bind@778:779 :(Option Int) r%77)
            (var@780:784 :(Chan Int) jobs%56)
            (body (tail (lit@785:786 :Int int 0))))
          (send@788:792
            (var@789:792 :(Chan Int) jobs%56)
            (lit@793:794 :Int int 9)
            (body (tail (lit@795:796 :Int int 1))))
          (default@798:800 (body (tail (lit@799:800 :Int int 2))))))
      (let@788:794 :Unit _@790:791
        (for@791:794 :Unit list
          (bind@795:796 :Int _)
          (var@794:796 :(List Int) l2%61)
          (body (tail (lit@797:799 :Unit unit "()")))))
      (let@800:806 :Unit _@802:803
        (for@803:806 :Unit chan
          (bind@788:789 :Int e%78)
          (var@806:810 :(Chan Int) jobs%56)
          (body (tail (lit@807:809 :Unit unit "()")))))
      (let@812:818 :Unit _@814:815 (go@815:818 :Unit (body (tail (lit@817:819 :Unit unit "()")))))
      (let@820:826 :Int i%79@796:797
        (callfunc@823:826 :Int
          (var@823:826 :(Int -> Int) inc%58)
          (lit@826:827 :Int int 3)))
      (let@796:802 :Point pt%80@804:806
        (record@799:802 :Point
          (field@800:801 x%8 :i0 (lit@800:801 :Float float 1.0))
          (field@801:802 y%9 :i1 (lit@801:802 :Float float 2.0))))
      (let@804:810 :Point pt2%81@812:815
        (update@807:810 :Point
          (var@808:810 :Point pt%80)
          (field@809:810 x%8 :i0 (lit@809:810 :Float float 3.0))))
      (let@812:818 :Float fx%82@820:822 (fieldget@815:818 :Float x%8 :i0 (var@815:818 :Point pt2%81)))
      (let@828:834 :Int i2%83@828:830 (fieldget@831:834 :Int :i1 (var@831:832 :((Int, Int)) t%62)))
      (tail (lit@836:838 :Unit unit "()"))))
  (fn@835:860 mk_add%84 :(Int -> Int -> Int) residual=(Int -> Int)
    (params (param@840:841 :Int a%85))
    (body
      (tail
        (closure@844:860 :(Int -> Int)
          (fn@844:860 :(Int -> Int)
            (params (param@848:849 :Int b%86))
            (body
              (tail
                (binary@852:857 :Int +
                  (var@852:853 :Int a%85)
                  (var@855:856 :Int b%86)))))))))
  (fn@865:880 id%87 :(a -> a)
    (tparams a)
    (params (param@870:871 :a x%88))
    (body (tail (var@876:877 :a x%88))))
  (test@882:910 "smoke"@887:893
    (fn@885:910 "smoke" :( -> Unit)
      (params)
      (body
        (let@890:894 :(Result Int GoError) res2%89@890:894 (var@890:895 :(Result Int GoError) res_in%90))
        (let@896:902 :Int n2%91@896:898 (escaperesult@899:902 :Int to="smoke" (var@900:905 :(Result Int GoError) res2%89)))
        (tail (lit@906:908 :Unit unit "()")))))
  (syms
    (sym %1 strconv module :pkg "strconv" :go)
    (sym %2 util module :pkg "shapes/util")
    (sym %3 Shape type)
    (sym %4 Circle ctor)
    (sym %5 radius field)
    (sym %6 Empty ctor)
    (sym %7 Point type)
    (sym %8 x field)
    (sym %9 y field)
    (sym %10 Meters type)
    (sym %11 Opaque type)
    (sym %12 Box type)
    (sym %13 Box ctor)
    (sym %14 answer var)
    (sym %15 area func)
    (sym %16 s var)
    (sym %17 c var)
    (sym %18 r var)
    (sym %19 add2 func)
    (sym %20 a var)
    (sym %21 b var)
    (sym %22 classify func)
    (sym %23 r var)
    (sym %24 Ok ctor)
    (sym %25 n var)
    (sym %26 Err ctor)
    (sym %27 sign func)
    (sym %28 n var)
    (sym %29 g var)
    (sym %30 head func)
    (sym %31 xs var)
    (sym %32 h var)
    (sym %33 t var)
    (sym %34 countdown func)
    (sym %35 n var)
    (sym %36 z var)
    (sym %37 m var)
    (sym %38 load func)
    (sym %39 p var)
    (sym %40 n var)
    (sym %41 Atoi func :pkg "strconv" :go)
    (sym %42 fetch func)
    (sym %43 u var)
    (sym %44 res var)
    (sym %45 n var)
    (sym %46 i64 var)
    (sym %47 opt var)
    (sym %48 Get func :pkg "cache" :go)
    (sym %49 tup var)
    (sym %50 DivMod func :pkg "num" :go)
    (sym %51 out var)
    (sym %52 apply2 func)
    (sym %53 f var)
    (sym %54 misc func)
    (sym %55 u var)
    (sym %56 jobs var)
    (sym %57 p var)
    (sym %58 inc var)
    (sym %59 b var)
    (sym %60 l var)
    (sym %61 l2 var)
    (sym %62 t var)
    (sym %63 c var)
    (sym %64 fv var)
    (sym %65 Itoa func :pkg "strconv" :go)
    (sym %66 buf var)
    (sym %67 todo builtin)
    (sym %68 w var)
    (sym %69 WriteString method :pkg "bytes" :go)
    (sym %70 s var)
    (sym %71 sl var)
    (sym %72 n var)
    (sym %73 Sum func :pkg "num" :go)
    (sym %74 m var)
    (sym %75 bx var)
    (sym %76 j var)
    (sym %77 r var)
    (sym %78 e var)
    (sym %79 i var)
    (sym %80 pt var)
    (sym %81 pt2 var)
    (sym %82 fx var)
    (sym %83 i2 var)
    (sym %84 mk_add func)
    (sym %85 a var)
    (sym %86 b var)
    (sym %87 id func)
    (sym %88 x var)
    (sym %89 res2 var)
    (sym %90 res_in var)
    (sym %91 n2 var)))
`
	if got != want {
		t.Errorf("DumpCore mismatch:\n%s", got)
	}
}

// TestDumpCoreNilMarkers pins the nil-tolerance conventions: missing
// type (:?), missing symbol (?%), missing binder and escape target
// (?, to=?), missing expr/dtree (?), and discard binders (_).
func TestDumpCoreNilMarkers(t *testing.T) {
	fn := &core.Func{
		Sp:     sp(10, 40),
		Sig:    cfnII,
		Params: []*core.Bind{nil, {Sp: sp(12, 13), Typ: types.Int}},
		Body: &core.Body{
			Lets: []*core.Let{
				clet(20, 30, nil, &core.Lit{Base: cbase(22, 24, types.Int), Kind: syntax.LitInt, Text: "1"}),
				clet(31, 35, &core.Bind{Sp: sp(31, 32), Typ: types.Unit, Discard: true},
					&core.EscapeResult{Base: cbase(33, 35, types.Int), X: nil, To: nil, Wrap: "w"}),
			},
			Tail: &core.Match{
				Base: cbase(36, 40, types.Int),
				Tree: &core.DSwitch{
					Base:    cbase(36, 40, types.Int),
					Subj:    nil,
					Cases:   []core.DCase{{Sp: sp(37, 38), Test: nil, Body: nil}},
					Default: nil,
				},
			},
		},
	}
	got := DumpCore(&core.File{
		Sp: sp(0, 50), Package: "p", PkgSp: sp(0, 1),
		Decls: []core.Decl{&core.FuncDecl{Sp: sp(10, 40), Sym: nil, Fn: fn}},
	})
	want := `(file@0:50 p@0:1
  (fn@10:40 ?% :(Int -> Int)
    (params
      (param ?)
      (param@12:13 :Int ?%))
    (body
      (let@20:30 ? (lit@22:24 :Int int 1))
      (let@31:35 :Unit _@31:32 (escaperesult@33:35 :Int to=? wrap="w" (?)))
      (tail
        (match@36:40 :Int
          (dswitch@36:40 :Int SwitchKind(?)
            (?)
            (case@37:38
              (?)
              (body ?))
            (default (?)))))))
  (syms))
`
	if got != want {
		t.Errorf("DumpCore nil markers mismatch:\n%s", got)
	}
}

// TestDumpCoreDeterminism dumps the same file repeatedly — output
// must be byte-identical since nothing in the walk consults map
// order or pointer values.
func TestDumpCoreDeterminism(t *testing.T) {
	f := coreSample()
	want := DumpCore(f)
	for i := 0; i < 10; i++ {
		if got := DumpCore(f); got != want {
			t.Fatalf("dump %d differs:\n%s", i, got)
		}
	}
}

// TestCoreDumpSuite runs the module-level dump suite — the consumer
// gomtest.RunDumpTests exists for. Phase suites register generators
// per name; "<phase>/<case>" names nest under testdata/dump/<phase>/.
func TestCoreDumpSuite(t *testing.T) {
	gomtest.RunDumpTests(t, "", map[string]gomtest.DumpFunc{
		"core/sample": func() (string, error) { return DumpCore(coreSample()), nil },
	})
}
