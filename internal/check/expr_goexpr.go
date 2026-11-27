package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// checkGoExpr types `go { ... }` — a spawned task. The body must be
// Unit (E0701) and the expression is Unit. Inside it, `try` no longer
// escapes to an outer function (E0702).
func (c *checker) checkGoExpr(e *syntax.GoExpr) Expr {
	c.goDepth++
	body := c.checkExpr(e.Body, nil)
	c.goDepth--
	switch t := types.Chase(body.Type()).(type) {
	case *types.Meta:
		// An unconstrained unknown may still be Unit; a
		// constrained one can never be — that is E0701. A literal
		// meta reports its default rather than the raw `?n`, and a
		// defaultless one reports the renumbered placeholder, not
		// the internal mint ID.
		if r := c.recOf(t); r != nil && (r.num || r.flo || r.intg || r.ord) {
			shown := body.Type()
			if r.dflt != nil {
				shown = r.dflt
			}
			td := types.Display(shown, metaNamer())
			c.diags.Add(diag.Error("E0701",
				fmt.Sprintf("a `go` body's type must be Unit, got %s", td)).
				At(body.Span()).Set("type", td))
		} else {
			c.bind(t, types.Unit, body.Span())
		}
	case *types.Con:
		if t.Qual == "" && t.Name == "Unit" {
			// The `go` body's rule names the builtin Unit — a
			// file-local `type Unit` is an ADT, not it.
			if c.shadowedBuiltin("Unit") {
				c.shadowedErr(body.Span(), "a `go` body", "Unit")
			}
		} else {
			td := types.Display(body.Type(), metaNamer())
			c.diags.Add(diag.Error("E0701",
				fmt.Sprintf("a `go` body's type must be Unit, got %s", td)).
				At(body.Span()).Set("type", td))
		}
	case *types.Var, *types.Func, *types.Tuple, *types.Go:
		td := types.Display(body.Type(), metaNamer())
		c.diags.Add(diag.Error("E0701",
			fmt.Sprintf("a `go` body's type must be Unit, got %s", td)).
			At(body.Span()).Set("type", td))
	default:
		panic("unreachable")
	}
	return &GoExpr{Base: baseOf(e.Sp, c.unitResult(e.Sp, "`go {}`")), Body: body}
}
