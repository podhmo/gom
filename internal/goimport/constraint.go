package goimport

import (
	"go/types"

	"github.com/podhmo/gom/internal/check"
	gt "github.com/podhmo/gom/internal/types"
)

// GoSatisfies is the check.GoConstraintSource answer for the
// widened constraint set of spec section 8.5 — the structural
// `~[]E`/`~map[K]V` shapes and the concrete-term unions. The
// judgment runs on the same boundaryType mapping the signature
// binding uses: `bt.Underlying()` is what a named slice or map
// repr contributes to a `~` term, and an unmappable argument
// (a function value, an unsolved meta, a generic instantiation)
// fails rather than guesses — the emitted instantiation must
// type-check under Go's rules, so honesty is a rejection.
func (l *Loader) GoSatisfies(targs []gt.Type, i int, c check.GoConstraint) bool {
	if i < 0 || i >= len(targs) {
		return false
	}
	bt := l.boundaryType(targs[i])
	if bt == nil {
		return false
	}
	switch c.Kind {
	case check.GoConstraintSlice:
		s, ok := bt.Underlying().(*types.Slice)
		if !ok || c.Elem < 0 || c.Elem >= len(targs) {
			return false
		}
		el := l.boundaryType(targs[c.Elem])
		// `~[]E` holds when the argument's underlying is a slice
		// of E — a named element on either side must match by
		// identity, not underlying: `[]int` does not admit
		// `E := MyInt`.
		return el != nil && types.Identical(s.Elem(), el)
	case check.GoConstraintMap:
		m, ok := bt.Underlying().(*types.Map)
		if !ok || c.Key < 0 || c.Key >= len(targs) || c.Val < 0 || c.Val >= len(targs) {
			return false
		}
		k, v := l.boundaryType(targs[c.Key]), l.boundaryType(targs[c.Val])
		return k != nil && v != nil &&
			types.Identical(m.Key(), k) && types.Identical(m.Elem(), v)
	case check.GoConstraintUnion:
		// A `List` argument never satisfies a union term over a
		// slice or map underlying: the emitted type argument is
		// the named `list.List[T]`, which no `[]T`-spelled term
		// admits — and even the `~[]T` spelling it *would* pass is
		// unsafe, since the verbatim call hands the callee the
		// List's backing to mutate. `Go.Slice`/`Go.Map` spell
		// anonymously and keep the honest check.
		isList := false
		if con, ok := gt.Chase(targs[i]).(*gt.Con); ok {
			isList = con.Qual == "" && con.Name == "List"
		}
		for _, tm := range c.Terms {
			switch tm.Type.Underlying().(type) {
			case *types.Slice, *types.Map:
				if isList {
					continue
				}
			}
			if tm.Approx {
				if types.Identical(bt.Underlying(), tm.Type.Underlying()) {
					return true
				}
			} else if types.Identical(bt, tm.Type) {
				return true
			}
		}
		return false
	case check.GoConstraintAny, check.GoConstraintComparable:
		// The comparable and any kinds are judged checker-side;
		// they never reach the oracle.
		return false
	default:
		return false
	}
}
