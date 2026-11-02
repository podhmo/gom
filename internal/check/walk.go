package check

// The typed-AST walkers. reclassifyCalls uses them to revisit use
// sites after a let's kind flips; they are exported so consumers of
// the typed file — the eval-vs-compiled harness collecting Go member
// symbols to stub, phase-level analyses — can traverse expression
// trees without re-deriving the sealed union by hand.

// WalkDecl calls fn on every expression inside a typed declaration
// body — the places an already-checked use site can live.
func WalkDecl(d Decl, fn func(Expr)) {
	switch d := d.(type) {
	case *FnDecl:
		WalkExpr(d.Body, fn)
	case *LetDecl:
		WalkExpr(d.Value, fn)
	case *TestDecl:
		WalkExpr(d.Body, fn)
	case *TypeDecl:
		// Type declarations carry no value-level expressions.
	default:
		panic("unreachable")
	}
}

// WalkExpr calls fn on e and every expression nested inside it.
// Patterns carry no expressions, so they are not descended into.
func WalkExpr(e Expr, fn func(Expr)) {
	if e == nil {
		return
	}
	fn(e)
	switch e := e.(type) {
	case *Lit, *Var, *Hole:
	case *Apply:
		WalkExpr(e.Fn, fn)
		for _, a := range e.Args {
			WalkExpr(a.Value, fn)
		}
	case *MemberExpr:
		WalkExpr(e.Recv, fn)
	case *Unary:
		WalkExpr(e.X, fn)
	case *Binary:
		WalkExpr(e.X, fn)
		WalkExpr(e.Y, fn)
	case *StrInterp:
		for _, p := range e.Parts {
			switch p := p.(type) {
			case *StrExpr:
				WalkExpr(p.X, fn)
			case *StrText:
			default:
				panic("unreachable")
			}
		}
	case *Lambda:
		WalkExpr(e.Body, fn)
	case *If:
		WalkExpr(e.Cond, fn)
		WalkExpr(e.Then, fn)
		WalkExpr(e.Else, fn)
	case *Match:
		WalkExpr(e.Subj, fn)
		for _, a := range e.Arms {
			WalkExpr(a.Guard, fn)
			WalkExpr(a.Body, fn)
		}
	case *Block:
		for _, s := range e.Stmts {
			WalkExpr(s, fn)
		}
	case *Let:
		WalkExpr(e.Value, fn)
	case *Ignore:
		WalkExpr(e.X, fn)
	case *Try:
		WalkExpr(e.X, fn)
	case *GoExpr:
		WalkExpr(e.Body, fn)
	case *For:
		WalkExpr(e.Iter, fn)
		WalkExpr(e.Body, fn)
	case *SelectExpr:
		for _, a := range e.Arms {
			switch a := a.(type) {
			case *RecvArm:
				WalkExpr(a.Chan, fn)
				WalkExpr(a.Body, fn)
			case *SendArm:
				WalkExpr(a.Chan, fn)
				WalkExpr(a.Value, fn)
				WalkExpr(a.Body, fn)
			case *DefaultArm:
				WalkExpr(a.Body, fn)
			default:
				panic("unreachable")
			}
		}
	case *ListLit:
		for _, el := range e.Elems {
			WalkExpr(el, fn)
		}
	case *TupleLit:
		for _, el := range e.Elems {
			WalkExpr(el, fn)
		}
	case *Spread:
		WalkExpr(e.X, fn)
	case *RecordLit:
		WalkExpr(e.With, fn)
		for _, f := range e.Fields {
			WalkExpr(f.Value, fn)
		}
	case *Ascribe:
		WalkExpr(e.X, fn)
	default:
		panic("unreachable")
	}
}
