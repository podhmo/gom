package pattern

import (
	"fmt"
	"sort"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/types"
)

// sortStrings sorts xs in place; a tiny helper kept for the
// deterministic data fields of E0304.
func sortStrings(xs []string) { sort.Strings(xs) }

// Options configures the checks. The zero value is usable.
type Options struct {
	// Resolver, when non-nil, supplies the .gom sources so the
	// "add the missing arms" fix can match the closing brace's
	// indentation. Without it the fix still inserts syntactically
	// valid arms at a fixed indentation.
	Resolver diag.Resolver
	// MaxCounterexamples caps the number of missing patterns an
	// exhaustiveness or refutability diagnostic lists (spec
	// section 4 and design.md section 4.2 say "先頭 5 件"). Values
	// <= 0 select the default of 5.
	MaxCounterexamples int
}

// checker carries the shared state of one checking run.
type checker struct {
	env *Env
	cv  conv
	res diag.Resolver
	max int
}

func newChecker(env *Env, opts Options) *checker {
	if env == nil {
		env = NewEnv(nil)
	}
	max := opts.MaxCounterexamples
	if max <= 0 {
		max = 5
	}
	return &checker{env: env, cv: conv{env: env}, res: opts.Resolver, max: max}
}

// CheckMatch checks one match expression (docs/design.md section
// 4.2):
//
//   - E0301 non-exhaustive match: the unguarded arms do not cover
//     the scrutinee type. The diagnostic's data lists up to
//     MaxCounterexamples counterexamples in "missing_patterns", the
//     scrutinee type in "scrutinee_type", and the fix "add the
//     missing arms" inserts `todo ()` bodies before the closing
//     brace (ApplicabilityPlaceholder). A match containing a guarded
//     arm gains the note "arms with guards do not count toward
//     exhaustiveness".
//   - E0302 unreachable match arm: an unguarded arm whose every
//     alternative is already covered by earlier unguarded arms.
//     Guarded arms are exempt — they can still run when the earlier
//     arm's guard fails — and never count as coverage either
//     (docs/spec.md section 4).
//   - E0304 or-pattern alternatives bind different variables: every
//     or-pattern inside an arm pattern must bind the same names at
//     the same types in each alternative.
//
// env must describe every type the arms can mention; NewEnv over the
// file's declarations covers declared and builtin types.
func CheckMatch(m *check.Match, env *Env, opts Options) []diag.Diagnostic {
	return newChecker(env, opts).checkMatch(m)
}

// checkMatch is the method form of CheckMatch, reused by CheckFile.
func (c *checker) checkMatch(m *check.Match) []diag.Diagnostic {
	var diags []diag.Diagnostic

	// Per-arm checks and row expansion. Guarded arms never enter
	// the coverage matrix, so their rows are not converted at all.
	cover := matrix{}
	var guarded bool
	rows := make([][][]*cell, len(m.Arms))
	for i := range m.Arms {
		arm := &m.Arms[i]
		diags = append(diags, c.orPatDiags(arm.Pat)...)
		if arm.Guard != nil {
			guarded = true
			continue
		}
		for _, alt := range c.cv.alts(arm.Pat) {
			rows[i] = append(rows[i], []*cell{alt})
		}
	}
	subjTy := m.Subj.Type()

	// Redundancy: arm i is unreachable when every expanded row is
	// already covered by the earlier unguarded rows.
	for i := range m.Arms {
		arm := &m.Arms[i]
		if arm.Guard == nil {
			reachable := false
			for _, alt := range rows[i] {
				if c.useful(cover, []types.Type{subjTy}, alt) {
					reachable = true
					break
				}
			}
			if !reachable {
				diags = append(diags,
					diag.Error("E0302", "unreachable match arm").
						At(arm.Sp).
						Set("arm_index", i))
			}
			cover.rows = append(cover.rows, rows[i]...)
		}
	}

	// Exhaustiveness against the unguarded arms.
	miss := c.missing(cover, []types.Type{subjTy}, c.max)
	if len(miss) > 0 {
		pats := make([]string, len(miss))
		for i, w := range miss {
			pats[i] = render(w[0])
		}
		d := diag.Error("E0301",
			fmt.Sprintf("non-exhaustive match on `%s`", subjTy)).
			At(m.Span()).
			Set("scrutinee_type", subjTy.String()).
			Set("missing_patterns", pats)
		if guarded {
			d = d.AddNote("arms with guards do not count toward exhaustiveness")
		}
		if f := c.missingArmsFix(m, pats); f != nil {
			d = d.AddFix(*f)
		}
		diags = append(diags, d)
	}
	return diags
}

// Context names the irrefutable position a standalone pattern is
// checked in — it selects the E0303 message.
type Context int

const (
	// CtxLet is the left side of `let pat = e`.
	CtxLet Context = iota + 1
	// CtxFor is the left side of `for pat in e`.
	CtxFor
	// CtxParam is a function or lambda parameter.
	CtxParam
)

// phrase renders the context inside a diagnostic message.
func (c Context) phrase() string {
	switch c {
	case CtxLet:
		return "a let binding"
	case CtxFor:
		return "a for binding"
	case CtxParam:
		return "a function parameter"
	default:
		return "an irrefutable position"
	}
}

// CheckPattern checks a pattern that must be irrefutable — a let or
// for binding or a function parameter (spec section 4 "同じ網羅性判定を、
// let の左辺（論駁不可能性）と関数引数パターンにも使う"):
//
//   - E0303 refutable pattern in an irrefutable position: the
//     pattern does not cover every value of its type. The data
//     carries "scrutinee_type", "missing_patterns" and a "context"
//     of "let" / "for" / "parameter".
//   - E0304 or-pattern alternatives bind different variables.
func CheckPattern(p check.Pat, ctx Context, env *Env, opts Options) []diag.Diagnostic {
	return newChecker(env, opts).checkPattern(p, ctx)
}

// checkPattern is the method form of CheckPattern, reused by
// CheckFile.
func (c *checker) checkPattern(p check.Pat, ctx Context) []diag.Diagnostic {
	var diags []diag.Diagnostic
	diags = append(diags, c.orPatDiags(p)...)
	var rows [][]*cell
	for _, alt := range c.cv.alts(p) {
		rows = append(rows, []*cell{alt})
	}
	miss := c.missing(matrix{rows: rows}, []types.Type{p.Type()}, c.max)
	if len(miss) > 0 {
		ty := p.Type()
		pats := make([]string, len(miss))
		for i, w := range miss {
			pats[i] = render(w[0])
		}
		diags = append(diags,
			diag.Error("E0303",
				fmt.Sprintf("refutable pattern in %s", ctx.phrase())).
				At(p.Span()).
				Set("scrutinee_type", ty.String()).
				Set("missing_patterns", pats).
				Set("context", ctxName(ctx)))
	}
	return diags
}

// ctxName is the structured-data name of a Context.
func ctxName(c Context) string {
	switch c {
	case CtxLet:
		return "let"
	case CtxFor:
		return "for"
	case CtxParam:
		return "parameter"
	default:
		return "irrefutable"
	}
}

// orPatDiags emits E0304 for every or-pattern inside p whose
// alternatives do not all bind the same names at the same types.
func (c *checker) orPatDiags(p check.Pat) []diag.Diagnostic {
	var diags []diag.Diagnostic
	var walk func(p check.Pat)
	walk = func(p check.Pat) {
		switch p := p.(type) {
		case *check.WildPat, *check.VarPat, *check.LitPat:
		case *check.AsPat:
			walk(p.Pat)
		case *check.CtorPat:
			for _, a := range p.Args {
				walk(a.Pat)
			}
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *check.TuplePat:
			for _, e := range p.Elems {
				walk(e)
			}
		case *check.RecordPat:
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *check.ListPat:
			for _, e := range p.Elems {
				walk(e)
			}
			if p.Rest != nil {
				walk(p.Rest)
			}
		case *check.OrPat:
			diags = append(diags, c.orPatDiag(p)...)
			for _, a := range p.Alts {
				walk(a)
			}
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return diags
}

// orPatDiag checks one or-pattern node's alternatives for identical
// bindings. It reports only the first alternative that differs from
// the first — one E0304 per or-pattern is enough to point at the
// offending arm.
func (c *checker) orPatDiag(o *check.OrPat) []diag.Diagnostic {
	if len(o.Alts) < 2 {
		return nil
	}
	ref := map[string]types.Type{}
	boundVars(o.Alts[0], ref)
	for i := 1; i < len(o.Alts); i++ {
		cur := map[string]types.Type{}
		boundVars(o.Alts[i], cur)
		missing, extra, mistyped := diffBindings(ref, cur)
		if len(missing) == 0 && len(extra) == 0 && len(mistyped) == 0 {
			continue
		}
		d := diag.Error("E0304",
			"or-pattern alternatives bind different variables").
			At(o.Span()).
			Set("alternative", i)
		if len(missing) > 0 {
			d = d.Set("missing", missing)
		}
		if len(extra) > 0 {
			d = d.Set("extra", extra)
		}
		if len(mistyped) > 0 {
			d = d.Set("mistyped", mistyped)
		}
		return []diag.Diagnostic{d}
	}
	return nil
}

// diffBindings compares the bound-variable sets of two or-pattern
// alternatives. missing holds names bound by ref but not cur, extra
// the reverse, and mistyped the names both bind at different types.
func diffBindings(ref, cur map[string]types.Type) (missing, extra, mistyped []string) {
	for name, rt := range ref {
		ct, ok := cur[name]
		if !ok {
			missing = append(missing, name)
		} else if !types.Equal(rt, ct) {
			mistyped = append(mistyped, name)
		}
	}
	for name := range cur {
		if _, ok := ref[name]; !ok {
			extra = append(extra, name)
		}
	}
	sortStrings(missing)
	sortStrings(extra)
	sortStrings(mistyped)
	return missing, extra, mistyped
}

// CheckFile checks every pattern-bearing position of a typed file:
// match expressions, let bindings (statement and top-level), for
// bindings, and function/lambda parameters (docs/design.md section 3
// — match、let、関数引数で共用). It returns the collected
// diagnostics in traversal order; callers sort with diag.List.Sort.
func CheckFile(f *check.File, opts Options) diag.List {
	c := newChecker(NewEnv(f.Decls), opts)
	var diags diag.List
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *check.FnDecl:
			for _, p := range d.Params {
				diags = append(diags, c.checkPattern(p.Pat, CtxParam)...)
			}
			c.walkExpr(d.Body, &diags)
		case *check.LetDecl:
			diags = append(diags, c.checkPattern(d.Pat, CtxLet)...)
			c.walkExpr(d.Value, &diags)
		case *check.TypeDecl:
			// already in the env
		case *check.TestDecl:
			c.walkExpr(d.Body, &diags)
		default:
			panic("unreachable")
		}
	}
	return diags
}

// walkExpr recurses through e checking every pattern site.
func (c *checker) walkExpr(e check.Expr, diags *diag.List) {
	switch e := e.(type) {
	case *check.Lit, *check.Var, *check.Hole:
	case *check.Apply:
		c.walkExpr(e.Fn, diags)
		for _, a := range e.Args {
			c.walkExpr(a.Value, diags)
		}
	case *check.MemberExpr:
		c.walkExpr(e.Recv, diags)
	case *check.Unary:
		c.walkExpr(e.X, diags)
	case *check.Binary:
		c.walkExpr(e.X, diags)
		c.walkExpr(e.Y, diags)
	case *check.StrInterp:
		for _, part := range e.Parts {
			if se, ok := part.(*check.StrExpr); ok {
				c.walkExpr(se.X, diags)
			}
		}
	case *check.Lambda:
		for _, p := range e.Params {
			*diags = append(*diags, c.checkPattern(p.Pat, CtxParam)...)
		}
		c.walkExpr(e.Body, diags)
	case *check.If:
		c.walkExpr(e.Cond, diags)
		c.walkExpr(e.Then, diags)
		if e.Else != nil {
			c.walkExpr(e.Else, diags)
		}
	case *check.Match:
		*diags = append(*diags, c.checkMatch(e)...)
		c.walkExpr(e.Subj, diags)
		for i := range e.Arms {
			arm := &e.Arms[i]
			if arm.Guard != nil {
				c.walkExpr(arm.Guard, diags)
			}
			c.walkExpr(arm.Body, diags)
		}
	case *check.Block:
		for _, s := range e.Stmts {
			c.walkExpr(s, diags)
		}
	case *check.Let:
		*diags = append(*diags, c.checkPattern(e.Pat, CtxLet)...)
		c.walkExpr(e.Value, diags)
	case *check.Ignore:
		c.walkExpr(e.X, diags)
	case *check.Try:
		c.walkExpr(e.X, diags)
	case *check.GoExpr:
		c.walkExpr(e.Body, diags)
	case *check.For:
		*diags = append(*diags, c.checkPattern(e.Pat, CtxFor)...)
		c.walkExpr(e.Iter, diags)
		c.walkExpr(e.Body, diags)
	case *check.SelectExpr:
		for _, arm := range e.Arms {
			switch arm := arm.(type) {
			case *check.RecvArm:
				// The bind is restricted to var/wild by the
				// grammar (spec section 10); only the
				// expressions can hold pattern sites.
				c.walkExpr(arm.Chan, diags)
				c.walkExpr(arm.Body, diags)
			case *check.SendArm:
				c.walkExpr(arm.Chan, diags)
				c.walkExpr(arm.Value, diags)
				c.walkExpr(arm.Body, diags)
			case *check.DefaultArm:
				c.walkExpr(arm.Body, diags)
			default:
				panic("unreachable")
			}
		}
	case *check.ListLit:
		for _, el := range e.Elems {
			c.walkExpr(el, diags)
		}
	case *check.Spread:
		c.walkExpr(e.X, diags)
	case *check.TupleLit:
		for _, el := range e.Elems {
			c.walkExpr(el, diags)
		}
	case *check.RecordLit:
		if e.With != nil {
			c.walkExpr(e.With, diags)
		}
		for _, f := range e.Fields {
			c.walkExpr(f.Value, diags)
		}
	case *check.Ascribe:
		c.walkExpr(e.X, diags)
	default:
		panic("unreachable")
	}
}
