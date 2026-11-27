package check

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// checkSelectExpr types a channel `select`. recv arms bind
// `Option a`; send arms require a send-capable channel and a value of
// the element type; at least one arm is required (E0705) and at most
// one default arm is allowed (E0703).
func (c *checker) checkSelectExpr(e *syntax.SelectExpr, want types.Type) Expr {
	bt := want
	if len(e.Arms) == 0 {
		// `select {}` cannot evaluate — Go's empty select blocks
		// forever, and the Core validator would only report an
		// internal invariant. The user-facing rule lives here.
		c.diags.Add(diag.Error("E0705",
			"a `select` requires at least one arm").At(e.Sp))
		if bt == nil {
			bt = c.quietMeta(e.Sp, "the select arms' type")
		} else {
			c.markQuiet(bt)
		}
	}
	if bt == nil {
		bt = c.newMeta(e.Sp, "the select arms' type")
	}
	out := &SelectExpr{Base: baseOf(e.Sp, bt)}
	defaults := 0
	var prev []*Expr
	armBody := func(body syntax.Expr) Expr {
		w := bt
		detached := want == nil && c.armSelfContained(body) && c.armDetached(bt)
		if detached {
			w = nil
		}
		b := c.checkExpr(body, w)
		if detached {
			bt, b = c.joinArm(bt, b, body.Span(), prev)
		}
		return b
	}
	for _, a := range e.Arms {
		switch a := a.(type) {
		case *syntax.RecvArm:
			ch := c.checkExpr(a.Chan, nil)
			elem, dir, ok := c.chanElem(ch.Type())
			if !ok {
				_, isMeta := types.Chase(ch.Type()).(*types.Meta)
				if !isMeta {
					if con, isCon := types.Chase(ch.Type()).(*types.Con); isCon &&
						con.Qual == "" && c.shadowedBuiltin(con.Name) &&
						(con.Name == "Chan" || con.Name == "RecvChan" || con.Name == "SendChan") {
						c.shadowedErr(a.Chan.Span(),
							"a select receive arm's channel", con.Name)
					} else {
						c.mismatch(a.Chan.Span(), "a receivable channel", ch.Type())
					}
				}
				elem = c.newMeta(a.Chan.Span(), "the received element's type")
				if !isMeta || c.isQuiet(ch.Type()) {
					c.markQuiet(elem)
				}
			} else if dir == "send" {
				c.diags.Add(diag.Error("E0401",
					"cannot receive on a send-only channel").At(a.Chan.Span()))
			}
			// The bind side takes only a variable or `_` (spec
			// section 10.5): each arm is one Go select case, so a
			// destructuring pattern has nowhere to run. Anything
			// else is E0704; the pattern still checks for recovery
			// (its names stay visible to the body) but the typed
			// arm keeps the VarPat/WildPat invariant with a
			// wildcard.
			var bind Pat
			switch a.Bind.(type) {
			case nil, *syntax.VarPat, *syntax.WildPat:
				bind = c.checkPat(a.Bind, types.Option(elem))
				// The bound name's declared type is the builtin
				// `Option a` — under a file-local `type Option`
				// that reading is the local ADT, which the
				// emitted `option.FromGo` is not (E0415). An
				// unused bind never materializes the Option, so
				// only a bind the arm's body consumes is an error.
				if vp, isVar := bind.(*VarPat); isVar && vp.Sym != nil &&
					c.shadowedBuiltin("Option") {
					// referenced is marked by the arm-body check
					// below; defer the diagnostic until then.
					defer func() {
						if c.referenced[vp.Sym] {
							c.shadowedErr(vp.Sp,
								"a select receive arm's variable bind (it receives the builtin `Option a`)", "Option")
						}
					}()
				}
			case *syntax.LitPat, *syntax.CtorPat, *syntax.TuplePat,
				*syntax.RecordPat, *syntax.ListPat, *syntax.OrPat,
				*syntax.AsPat, *syntax.TypePat:
				c.diags.Add(diag.Error("E0704",
					"a select receive arm must bind a variable or `_`, not a destructuring pattern").
					At(a.Bind.Span()))
				_ = c.checkPat(a.Bind, types.Option(elem))
				bind = &WildPat{Base: baseOf(a.Bind.Span(), types.Option(elem))}
			default:
				panic("unreachable")
			}
			ra := &RecvArm{
				Bind: bind,
				Chan: ch,
				Body: armBody(a.Body),
			}
			out.Arms = append(out.Arms, ra)
			prev = append(prev, &ra.Body)
		case *syntax.SendArm:
			ch := c.checkExpr(a.Chan, nil)
			elem, dir, ok := c.chanElem(ch.Type())
			if !ok {
				_, isMeta := types.Chase(ch.Type()).(*types.Meta)
				if !isMeta {
					if con, isCon := types.Chase(ch.Type()).(*types.Con); isCon &&
						con.Qual == "" && c.shadowedBuiltin(con.Name) &&
						(con.Name == "Chan" || con.Name == "RecvChan" || con.Name == "SendChan") {
						c.shadowedErr(a.Chan.Span(),
							"a select send arm's channel", con.Name)
					} else {
						c.mismatch(a.Chan.Span(), "a sendable channel", ch.Type())
					}
				}
				elem = c.newMeta(a.Chan.Span(), "the sent element's type")
				if !isMeta || c.isQuiet(ch.Type()) {
					c.markQuiet(elem)
				}
			} else if dir == "recv" {
				c.diags.Add(diag.Error("E0401",
					"cannot send on a receive-only channel").At(a.Chan.Span()))
			}
			sa := &SendArm{
				Chan:  ch,
				Value: c.checkExpr(a.Value, elem),
				Body:  armBody(a.Body),
			}
			out.Arms = append(out.Arms, sa)
			prev = append(prev, &sa.Body)
		case *syntax.DefaultArm:
			defaults++
			if defaults > 1 {
				c.diags.Add(diag.Error("E0703",
					"a select may have at most one default arm").At(a.Sp))
			}
			da := &DefaultArm{
				Sp:   a.Sp,
				Body: armBody(a.Body),
			}
			out.Arms = append(out.Arms, da)
			prev = append(prev, &da.Body)
		default:
			panic("unreachable")
		}
	}
	// A late `any` arm moves the join after the node was born —
	// repin the select's type to the settled join (the `any`
	// arm's conform rewrote prior arm bodies in place through
	// `prev`).
	out.Base = baseOf(e.Sp, bt)
	return out
}
