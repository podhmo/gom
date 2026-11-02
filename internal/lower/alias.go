package lower

import (
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
)

// indexStaticAliases follows only pure references rooted in local,
// non-thunk functions. An application or closure factory must still
// execute eagerly, and an alias-only cycle has no static function root.
func (l *lowerer) indexStaticAliases(f *check.File) map[*resolve.Symbol]bool {
	refs := map[*resolve.Symbol]*resolve.Symbol{}
	for _, d := range f.Decls {
		ld, ok := d.(*check.LetDecl)
		if !ok {
			continue
		}
		p, ok := ld.Pat.(*check.VarPat)
		if !ok || flatSig(ld.Value.Type()) == nil {
			continue
		}
		e := ld.Value
		for {
			a, ok := e.(*check.Ascribe)
			if !ok || a.Conv {
				break
			}
			e = a.X
		}
		if r, ok := e.(*check.Var); ok && r.Sym != nil {
			refs[p.Sym] = r.Sym
		}
	}
	aliases := map[*resolve.Symbol]bool{}
	visited := map[*resolve.Symbol]bool{}
	var static func(*resolve.Symbol) bool
	static = func(sym *resolve.Symbol) bool {
		if _, local := l.arity[sym]; local && !l.thunks[sym] {
			return true
		}
		if visited[sym] {
			return aliases[sym]
		}
		visited[sym] = true
		if target := refs[sym]; target != nil && static(target) {
			aliases[sym] = true
		}
		return aliases[sym]
	}
	for sym := range refs {
		static(sym)
	}
	return aliases
}
