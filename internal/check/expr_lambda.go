package check

import (
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

func (c *checker) isRecordType(t types.Type) bool {
	con, ok := types.Chase(t).(*types.Con)
	if !ok {
		return false
	}
	ti, ok := c.tyCons[conKey{qual: con.Qual, name: con.Name}]
	return ok && ti.kind == kindRecord
}

func (c *checker) containsRecordType(t types.Type) bool {
	if c.isRecordType(t) {
		return true
	}
	switch t := types.Chase(t).(type) {
	case *types.Con:
		for _, a := range t.Args {
			if c.containsRecordType(a) {
				return true
			}
		}
	case *types.Func:
		for _, p := range t.Params {
			if c.containsRecordType(p.Type) {
				return true
			}
		}
		return c.containsRecordType(t.Ret)
	case *types.Tuple:
		for _, e := range t.Elems {
			if c.containsRecordType(e) {
				return true
			}
		}
	case *types.Go:
		for _, a := range t.Args {
			if c.containsRecordType(a) {
				return true
			}
		}
	case *types.Meta, *types.Var:
		return false
	default:
		panic("unreachable")
	}
	return false
}

// positionalLambdaChainParams counts the parameter positions consumed by
// a syntactic chain of fully positional lambdas. The count is an upper
// bound for inference-time label erasure: labels after it belong to a
// value the chain did not itself produce positionally.
func positionalLambdaChainParams(e *syntax.Lambda) int {
	n := len(e.Params)
	body, ok := e.Body.(*syntax.Lambda)
	if !ok {
		return n
	}
	for _, p := range body.Params {
		if p.Label != nil {
			return n
		}
	}
	return n + positionalLambdaChainParams(body)
}

// checkLambda types an anonymous function `fn x y -> body`.
// Unannotated parameters normally get metas. A statically expected
// record parameter is the one exception: seeding it lets record-update
// bodies use the same contextual type as the record literal that
// motivated the expectation. The boundary stack grows so a `try`
// inside escapes to this lambda's Result — which therefore becomes
// `p1 ... -> Result a e` when it contains one.
func (c *checker) checkLambda(e *syntax.Lambda, want types.Type) Expr {
	sc := c.newTScope()
	// A variable this scope mints stays registered so finalize can
	// report the ones no enclosing signature or top-level let binds.
	c.lambdaAnnScopes = append(c.lambdaAnnScopes, lambdaAnnScope{sc: sc, sp: e.Sp})
	// An expected function type seeds the body's result want with
	// the residual after the lambda's own parameters: `1` under
	// `Int -> Go.Any` conforms to `any` and boxes at the body
	// boundary, where a fresh result meta would bind Int first and
	// then fail the outer `unifySub` on its num flag. Positional
	// lambda parameters consume want parameters in order; in a fully
	// positional lambda they may consume labeled positions through the
	// inference-only erased view. A labeled lambda parameter consumes
	// the matching label from the labeled run at the consumption point,
	// and anything it cannot pair leaves the residual unknown (the
	// standing meta, unchanged from before).
	var wparams []types.FuncParam
	var wret types.Type
	if f, ok := types.Chase(want).(*types.Func); ok {
		wparams, wret = flattenFunc(f)
	}
	allPositional := true
	for _, p := range e.Params {
		if p.Label != nil {
			allPositional = false
			break
		}
	}
	if allPositional {
		// A fully positional lambda may borrow labels only at final
		// conformance. For inference, erase labels from the parameter
		// positions consumed by this syntactic chain of positional lambdas;
		// labels left beyond that chain still belong to the returned
		// function's contract.
		for i := 0; i < positionalLambdaChainParams(e) && i < len(wparams); i++ {
			wparams[i].Label = ""
		}
	}
	wi := 0       // front of the unconsumed want parameters
	known := true // the residual is still determinable
	consumed := make([]bool, len(wparams))
	params := make([]Param, len(e.Params))
	fparams := make([]types.FuncParam, len(e.Params))
	for i, p := range e.Params {
		var wpt types.Type
		if p.Label == nil {
			for wi < len(wparams) && consumed[wi] {
				wi++
			}
			if wi >= len(wparams) || wparams[wi].Label != "" {
				known = false
			} else {
				wpt = wparams[wi].Type
				consumed[wi] = true
				wi++
			}
		} else {
			matched := false
			for k := wi; k < len(wparams) && wparams[k].Label != ""; k++ {
				if !consumed[k] && wparams[k].Label == p.Label.Name {
					matched = true
					wpt = wparams[k].Type
					consumed[k] = true
					break
				}
			}
			if !matched {
				known = false
			}
		}
		var pt types.Type
		if p.Type != nil {
			pt = c.typeExpr(p.Type, sc)
			c.checkScopeTypeParam(p.Type, pt, "lambda parameter's type")
		} else if c.isRecordType(wpt) {
			pt = wpt
		} else {
			pt = c.newMeta(p.Sp, "the lambda parameter's type")
		}
		label := ""
		if p.Label != nil {
			label = p.Label.Name
		}
		params[i] = Param{Label: label, Pat: c.checkPat(p.Pat, pt)}
		fparams[i] = types.FuncParam{Label: label, Type: pt}
	}
	// The residual seeds `ret` nested — `b -> ~c: Int -> Int`, not
	// types.Fn's flat `b ~c: -> Int` — so the Lambda node keeps the
	// level structure eval's partial-application model and lower's
	// arity split consume.
	ret := wret
	if !known || wret == nil {
		ret = c.newMeta(e.Sp, "the lambda's return type")
	} else {
		var res []types.FuncParam
		for j, wp := range wparams {
			if !consumed[j] {
				res = append(res, wp)
			}
		}
		if len(res) > 0 {
			ret = &types.Func{Params: res, Ret: wret}
		}
	}
	// The lambda's own annotation variables stay visible in its
	// body — `fn (x: a) -> { let y : a = x; y }`.
	prevScope := c.curTScope
	c.curTScope = sc
	c.bounds = append(c.bounds, boundary{ret: ret, sp: e.Sp, goDepth: c.goDepth, kind: "lambda"})
	body := c.checkExpr(e.Body, ret)
	c.bounds = c.bounds[:len(c.bounds)-1]
	c.curTScope = prevScope
	// No types.Fn here: it flattens a concrete `ret` into the
	// parameter list, collapsing the level boundary partial
	// application needs (`b -> (~c -> Int)` is one level, not two
	// parameters of this lambda). The nested Func keeps the shape
	// a fresh `ret` meta would have recorded.
	return &Lambda{
		Base:   baseOf(e.Sp, &types.Func{Params: fparams, Ret: ret}),
		Params: params,
		Body:   body,
	}
}
