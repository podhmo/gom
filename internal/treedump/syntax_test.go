package treedump

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
)

// declsFile exercises every top-level declaration form plus comment,
// blank-line and doc trivia.
func declsFile() *syntax.File {
	return &syntax.File{
		Sp:      sp(0, 400),
		Package: id(8, 12, "geom"),
		Decls: []syntax.Decl{
			&syntax.ImportDecl{Sp: sp(14, 28), Path: "math", PathSp: sp(21, 27), Name: id(20, 27, "math")},
			&syntax.ImportDecl{Sp: sp(29, 50), Go: true, Path: "fmt", PathSp: sp(40, 45), Name: id(46, 49, "fmt"), Alias: true},
			&syntax.TypeDecl{
				Sp: sp(52, 110), Name: id(57, 62, "Shape"),
				Params: []*syntax.Ident{id(63, 64, "a")},
				Body: &syntax.SumBody{Sp: sp(67, 110), Variants: []syntax.Variant{
					{Sp: sp(69, 88), Doc: &syntax.Comment{Sp: sp(68, 69), Kind: syntax.CommentDoc, Text: " round"},
						Name: id(69, 75, "Circle"), Params: []syntax.Field{
							{Sp: sp(76, 87), Name: id(76, 82, "radius"), Type: tn(84, 87, "Int")},
						}},
					{Sp: sp(90, 110), Name: id(90, 98, "Transfer"), Params: []syntax.Field{
						{Sp: sp(99, 109), Label: id(100, 102, "to"), Type: tn(104, 109, "Shape")},
						{Sp: sp(110, 111), Type: tn(110, 111, "a")},
					}},
				}},
			},
			&syntax.TypeDecl{Sp: sp(112, 140), Name: id(117, 123, "Point"),
				Body: &syntax.RecordBody{Sp: sp(126, 140), Fields: []syntax.Field{
					{Sp: sp(128, 135), Name: id(128, 129, "x"), Type: tn(131, 135, "Int")},
				}}},
			&syntax.TypeDecl{Sp: sp(141, 160), Name: id(146, 151, "Meter"),
				Body: &syntax.AliasBody{Sp: sp(154, 160), Type: tn(156, 160, "Int")}},
			&syntax.TypeDecl{Sp: sp(161, 170), Name: id(166, 169, "Abs"),
				Body: &syntax.AbstractBody{Sp: sp(161, 170)}},
			&syntax.LetDecl{Sp: sp(172, 200), Doc: &syntax.Comment{Sp: sp(170, 171), Kind: syntax.CommentDoc, Text: " top"}, TestOnly: true,
				Pat:  &syntax.VarPat{Sp: sp(176, 181), Name: id(176, 181, "level")},
				Type: &syntax.TypeApp{Sp: sp(184, 190), Ctor: tn(184, 188, "List"), Args: []syntax.TypeExpr{&syntax.TypeVar{Sp: sp(189, 190), Name: id(189, 190, "a")}}},
				Value: &syntax.ListLit{Sp: sp(193, 200), Elems: []syntax.Expr{
					&syntax.Lit{Sp: sp(194, 195), Kind: syntax.LitInt, Text: "1"},
					&syntax.Spread{Sp: sp(197, 200), X: id(199, 200, "r")},
				}},
			},
			&syntax.ExternDecl{Sp: sp(301, 330), Path: "os", PathSp: sp(311, 315), Entries: []syntax.ExternEntry{
				&syntax.ExternFn{Sp: sp(317, 329), Doc: &syntax.Comment{Sp: sp(316, 317), Kind: syntax.CommentDoc, Text: " env"},
					Name: id(320, 325, "Getenv"),
					Params: []syntax.Param{
						{Sp: sp(326, 327), Pat: &syntax.VarPat{Sp: sp(326, 327), Name: id(326, 327, "k")}, Type: tn(327, 328, "a")},
					},
					Ret: tn(328, 329, "a")},
			}},
			&syntax.TestDecl{Sp: sp(331, 360), Name: "smoke", NameSp: sp(336, 343),
				Body: &syntax.Block{Sp: sp(345, 360), Stmts: []syntax.Expr{
					&syntax.For{Sp: sp(346, 358), Pat: &syntax.WildPat{Sp: sp(350, 351)}, Iter: id(355, 356, "xs"), Body: &syntax.Block{Sp: sp(357, 358)}},
				}}},
		},
		Comments: []*syntax.Comment{
			{Sp: sp(13, 14), Kind: syntax.CommentLine, Text: " header"},
			{Sp: sp(51, 52), Kind: syntax.CommentBlock, Text: " types "},
		},
		Blanks: []diag.Span{sp(111, 112)},
	}
}

// exprsBody exercises the expression and pattern variants inside one
// block: application forms, operators, control flow, literals and
// every pattern kind.
func exprsBody() *syntax.Block {
	return &syntax.Block{Sp: sp(10, 390), Stmts: []syntax.Expr{
		&syntax.Let{Sp: sp(12, 30), Pat: &syntax.TuplePat{Sp: sp(16, 22), Elems: []syntax.Pat{
			&syntax.VarPat{Sp: sp(17, 18), Name: id(17, 18, "a")},
			&syntax.WildPat{Sp: sp(20, 21)},
		}}, Value: &syntax.TupleLit{Sp: sp(25, 30), Elems: []syntax.Expr{
			&syntax.Lit{Sp: sp(26, 27), Kind: syntax.LitInt, Text: "1"},
			&syntax.Unary{Sp: sp(28, 29), Op: syntax.UnNeg, X: id(29, 30, "n")},
		}}},
		&syntax.Apply{Sp: sp(32, 60), Fn: &syntax.Select{Sp: sp(32, 40), Recv: id(32, 35, "fmt"), Sel: id(36, 40, "Println")}, Args: []syntax.Arg{
			{Sp: sp(41, 50), Value: &syntax.RecordLit{Sp: sp(41, 50), Fields: []syntax.FieldInit{
				{Sp: sp(43, 48), Name: id(43, 44, "x"), Value: &syntax.Lit{Sp: sp(47, 48), Kind: syntax.LitInt, Text: "1"}},
			}}},
			{Sp: sp(51, 60), Label: id(52, 54, "to"), Value: &syntax.Hole{Sp: sp(55, 56)}},
			{Sp: sp(57, 60), Label: id(58, 59, "v"), Punned: true, Value: id(58, 59, "v")},
		}},
		&syntax.Binary{Sp: sp(62, 80), Op: syntax.BinPipeFwd, X: id(62, 63, "xs"),
			Y: &syntax.Lambda{Sp: sp(67, 80), Params: []syntax.Param{
				{Sp: sp(71, 72), Pat: &syntax.VarPat{Sp: sp(71, 72), Name: id(71, 72, "e")}},
			}, Body: &syntax.Binary{Sp: sp(76, 80), Op: syntax.BinComposeFwd, X: id(76, 77, "f"), Y: id(79, 80, "g")}}},
		&syntax.Ignore{Sp: sp(82, 90), X: &syntax.Apply{Sp: sp(89, 90), Fn: id(89, 90, "f")}},
		&syntax.Try{Sp: sp(92, 100), X: &syntax.Apply{Sp: sp(96, 100), Fn: id(96, 99, "run")}},
		&syntax.GoExpr{Sp: sp(102, 110), Body: &syntax.Block{Sp: sp(105, 110), Stmts: []syntax.Expr{
			&syntax.Apply{Sp: sp(106, 109), Fn: id(106, 109, "f0"), Args: nil},
		}}},
		&syntax.SelectExpr{Sp: sp(112, 150), Arms: []syntax.SelectArm{
			&syntax.RecvArm{Sp: sp(114, 125), Bind: &syntax.VarPat{Sp: sp(114, 115), Name: id(114, 115, "v")}, Chan: id(119, 120, "ch"), Body: id(124, 125, "v")},
			&syntax.SendArm{Sp: sp(126, 140), Chan: id(131, 132, "ch"), Value: &syntax.Lit{Sp: sp(133, 134), Kind: syntax.LitInt, Text: "0"}, Body: &syntax.Lit{Sp: sp(138, 140), Kind: syntax.LitUnit, Text: "()"}},
			&syntax.DefaultArm{Sp: sp(141, 150), Body: &syntax.Lit{Sp: sp(149, 150), Kind: syntax.LitUnit, Text: "()"}},
		}},
		&syntax.Ascribe{Sp: sp(152, 165), X: &syntax.Apply{Sp: sp(152, 160), Fn: &syntax.Select{Sp: sp(152, 158), Recv: id(152, 154, "Go"), Sel: id(155, 158, "conv")}, Args: []syntax.Arg{{Sp: sp(159, 160), Value: id(159, 160, "e")}}},
			Type: &syntax.TypeName{Sp: sp(162, 165), Qual: id(162, 163, "T"), Name: id(164, 165, "U")}},
		&syntax.Match{Sp: sp(167, 260), Subj: id(173, 174, "s"), Arms: []syntax.MatchArm{
			{Sp: sp(176, 200),
				Pat: &syntax.RecordPat{Sp: sp(176, 195), Fields: []syntax.FieldPat{
					{Sp: sp(178, 183), Name: id(178, 179, "x"), Pat: &syntax.VarPat{Sp: sp(182, 183), Name: id(182, 183, "px")}},
					{Sp: sp(185, 190), Name: id(185, 186, "y")},
				}, Rest: true},
				Body: id(198, 200, "px")},
			{Sp: sp(202, 230),
				Pat: &syntax.ListPat{Sp: sp(202, 215), Elems: []syntax.Pat{
					&syntax.LitPat{Sp: sp(203, 204), Lit: &syntax.Lit{Sp: sp(203, 204), Kind: syntax.LitInt, Text: "0"}},
				}, Rest: &syntax.VarPat{Sp: sp(208, 213), Name: id(208, 213, "rest")}},
				Body: id(225, 230, "rest")},
			{Sp: sp(232, 259),
				Pat: &syntax.CtorPat{Sp: sp(232, 250), Ctor: &syntax.Select{Sp: sp(232, 240), Recv: id(232, 235, "Opt"), Sel: id(236, 240, "Some")},
					Args:   []syntax.CtorArg{{Sp: sp(241, 245), Label: id(242, 244, "v"), Pat: &syntax.VarPat{Sp: sp(246, 247), Name: id(246, 247, "v")}}},
					Fields: []syntax.FieldPat{{Sp: sp(248, 249), Name: id(248, 249, "w"), Pat: &syntax.WildPat{Sp: sp(249, 250)}}}},
				Guard: &syntax.Binary{Sp: sp(252, 256), Op: syntax.BinGt, X: id(252, 253, "v"), Y: &syntax.Lit{Sp: sp(255, 256), Kind: syntax.LitInt, Text: "0"}},
				Body:  id(258, 259, "v")},
		}},
		&syntax.If{Sp: sp(262, 280), Cond: id(265, 266, "c"),
			Then: &syntax.Block{Sp: sp(268, 273), Stmts: []syntax.Expr{id(270, 271, "t")}},
			Else: &syntax.Block{Sp: sp(274, 279), Stmts: []syntax.Expr{id(276, 277, "e")}}},
		&syntax.StrInterp{Sp: sp(282, 300), Parts: []syntax.StrPart{
			&syntax.StrText{Sp: sp(283, 290), Text: "go to "},
			&syntax.StrExpr{Sp: sp(290, 297), X: &syntax.Apply{Sp: sp(292, 295), Fn: id(292, 295, "f0"), Args: []syntax.Arg{{Sp: sp(296, 297), Value: id(296, 297, "w")}}}},
			&syntax.StrText{Sp: sp(297, 300), Text: "!"},
		}},
		&syntax.RecordLit{Sp: sp(302, 320), Base: id(302, 305, "old"), Fields: []syntax.FieldInit{
			{Sp: sp(310, 318), Name: id(310, 311, "x"), Value: &syntax.Lit{Sp: sp(315, 318), Kind: syntax.LitInt, Text: "9"}},
		}},
		&syntax.Let{Sp: sp(322, 340), Pat: &syntax.VarPat{Sp: sp(326, 327), Name: id(326, 327, "fn")},
			Type: &syntax.TypeFn{Sp: sp(330, 339), Params: []syntax.TypeParam{
				{Sp: sp(330, 334), Type: tn(330, 334, "Int")},
				{Sp: sp(335, 339), Label: id(335, 337, "to"), Type: tn(338, 339, "b")},
			}, Ret: &syntax.TypeTuple{Sp: sp(340, 348), Elems: []syntax.TypeExpr{tn(341, 344, "Int"), tn(345, 348, "Int")}}},
			Value: id(350, 352, "f0")},
		&syntax.Let{Sp: sp(342, 360), Pat: &syntax.VarPat{Sp: sp(346, 347), Name: id(346, 347, "rec")},
			Type: &syntax.TypeRecord{Sp: sp(350, 359), Fields: []syntax.Field{
				{Sp: sp(351, 358), Name: id(351, 352, "f"), Type: tn(354, 358, "Int")},
			}},
			Value: &syntax.Lit{Sp: sp(359, 360), Kind: syntax.LitChar, Text: "'a'"}},
	}}
}

func TestDumpSyntaxDecls(t *testing.T) {
	got := DumpSyntax(declsFile())
	want := `(file@0:400 geom@8:12
  (comment@13:14 line " header")
  (import@14:28 "math"@21:27 math@20:27)
  (import@29:50 "fmt"@40:45 fmt@46:49 :go :alias)
  (comment@51:52 block " types ")
  (type@52:110 Shape@57:62
    (tparams a@63:64)
    (sum@67:110
      (variant@69:88 Circle@69:75
        (doc@68:69 " round")
        (field@76:87 radius@76:82 Int@84:87))
      (variant@90:110 Transfer@90:98
        (field@99:109 ~to@100:102 Shape@104:109)
        (field@110:111 a@110:111))))
  (blank@111:112)
  (type@112:140 Point@117:123 (record@126:140 (field@128:135 x@128:129 Int@131:135)))
  (type@141:160 Meter@146:151 (alias@154:160 Int@156:160))
  (type@161:170 Abs@166:169 (abstract@161:170))
  (let@172:200 :test
    (doc@170:171 " top")
    (varpat@176:181 level@176:181)
    (: (tapp@184:190 List@184:188 'a@189:190))
    (list@193:200
      (lit@194:195 int 1)
      (spread@197:200 r@199:200)))
  (extern@301:330 "os"@311:315
    (externfn@317:329 Getenv@320:325
      (doc@316:317 " env")
      (params
        (param@326:327
          (varpat@326:327 k@326:327)
          (: a@327:328)))
      (-> a@328:329)))
  (test@331:360 "smoke"@336:343
    (block@345:360
      (for@346:358
        (wildpat@350:351)
        xs@355:356
        (block@357:358)))))
`
	if got != want {
		t.Errorf("DumpSyntax mismatch:\n%s", got)
	}
}

func TestDumpSyntaxExprs(t *testing.T) {
	f := &syntax.File{
		Sp:      sp(0, 400),
		Package: id(8, 9, "m"),
		Decls: []syntax.Decl{
			&syntax.FnDecl{Sp: sp(10, 395), Name: id(13, 16, "run"), Body: exprsBody()},
		},
	}
	got := DumpSyntax(f)
	want := `(file@0:400 m@8:9
  (fn@10:395 run@13:16
    (params)
    (block@10:390
      (let@12:30
        (tuplepat@16:22
          (varpat@17:18 a@17:18)
          (wildpat@20:21))
        (tuple@25:30
          (lit@26:27 int 1)
          (unary@28:29 - n@29:30)))
      (apply@32:60
        (sel@32:40 fmt@32:35 Println@36:40)
        (arg@41:50 (record@41:50 (field@43:48 x@43:44 (lit@47:48 int 1))))
        (arg@51:60 ~to@52:54 (hole@55:56))
        (arg@57:60 ~v@58:59 :pun v@58:59))
      (binary@62:80 |>
        xs@62:63
        (lambda@67:80
          (params (param@71:72 (varpat@71:72 e@71:72)))
          (binary@76:80 >> f@76:77 g@79:80)))
      (ignore@82:90 (apply@89:90 f@89:90))
      (try@92:100 (apply@96:100 run@96:99))
      (go@102:110 (block@105:110 (apply@106:109 f0@106:109)))
      (select@112:150
        (recv@114:125 (varpat@114:115 v@114:115) ch@119:120 v@124:125)
        (send@126:140
          ch@131:132
          (lit@133:134 int 0)
          (lit@138:140 unit "()"))
        (default@141:150 (lit@149:150 unit "()")))
      (ascribe@152:165
        (apply@152:160
          (sel@152:158 Go@152:154 conv@155:158)
          (arg@159:160 e@159:160))
        (: T.U@162:165))
      (match@167:260
        s@173:174
        (arm@176:200
          (recordpat@176:195
            (fpat@178:183 x@178:179 (varpat@182:183 px@182:183))
            (fpat@185:190 y@185:186 :pun)
            :rest)
          px@198:200)
        (arm@202:230
          (listpat@202:215
            (litpat@203:204 int 0)
            (rest (varpat@208:213 rest@208:213)))
          rest@225:230)
        (arm@232:259
          (ctorpat@232:250
            (sel@232:240 Opt@232:235 Some@236:240)
            (carg@241:245 ~v@242:244 (varpat@246:247 v@246:247))
            (fpat@248:249 w@248:249 (wildpat@249:250)))
          (guard (binary@252:256 > v@252:253 (lit@255:256 int 0)))
          v@258:259))
      (if@262:280
        c@265:266
        (block@268:273 t@270:271)
        (block@274:279 e@276:277))
      (strinterp@282:300
        (strtext@283:290 "go to ")
        (splice@290:297 (apply@292:295 f0@292:295 (arg@296:297 w@296:297)))
        (strtext@297:300 "!"))
      (record@302:320
        (with old@302:305)
        (field@310:318 x@310:311 (lit@315:318 int 9)))
      (let@322:340
        (varpat@326:327 fn@326:327)
        (:
          (tfn@330:339
            (tparam@330:334 Int@330:334)
            (tparam@335:339 ~to@335:337 b@338:339)
            (-> (ttuple@340:348 Int@341:344 Int@345:348))))
        f0@350:352)
      (let@342:360
        (varpat@346:347 rec@346:347)
        (: (trecord@350:359 (field@351:358 f@351:352 Int@354:358)))
        (lit@359:360 char 'a')))))
`
	if got != want {
		t.Errorf("DumpSyntax mismatch:\n%s", got)
	}
}
