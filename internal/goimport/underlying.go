package goimport

// Underlying-kind query for the checker's literal range judgment —
// the optional check.UnderlyingSource protocol. A `Go.conv lit : T`
// emits `T(lit)`, a constant conversion at T; when T is a named Go
// type (os.FileMode, time.Duration) the range the literal must fit
// is T's underlying basic type — knowledge only the go/types side
// has, since a *types.Go handle carries just its NamedRef.

import (
	"go/types"

	gt "github.com/podhmo/gom/internal/types"
)

// GoUnderlyingKind reports the basic kind of the underlying type of
// the named Go type behind t, a *types.Go handle the loader minted:
// os.FileMode (the alias of io/fs.FileMode) answers types.Uint32,
// time.Duration types.Int64. ok is false for a non-Go type, an
// instantiated or unknown handle, and a non-basic underlying (a
// struct, an interface) — the literal then judges at its own type.
func (l *Loader) GoUnderlyingKind(t gt.Type) (types.BasicKind, bool) {
	g, ok := gt.Chase(t).(*gt.Go)
	if !ok || len(g.Args) != 0 {
		return types.Invalid, false
	}
	n, ok := l.named[g.Ref]
	if !ok {
		return types.Invalid, false
	}
	b, ok := n.Underlying().(*types.Basic)
	if !ok {
		return types.Invalid, false
	}
	return b.Kind(), true
}
