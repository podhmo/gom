package pattern

import (
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// rootDomain is the set of root constructor signature keys a stable
// local variable can still have at a lexical position. An empty keys
// map is a valid unreachable domain; a nil *rootDomain means no
// enumerable narrowing is available.
type rootDomain struct {
	keys map[string]bool
}

// stableSubject returns the local variable symbol used as a match
// scrutinee. Only a direct reference to source-defined, non-extern
// storage is stable enough for the syntactic narrowing of
// decisions/2026-11-27-match-nested-narrowing.
func (c *checker) stableSubject(e check.Expr) (*resolve.Symbol, bool) {
	v, ok := e.(*check.Var)
	if !ok || v.Sym == nil || v.Sym.Kind != resolve.KindVar {
		return nil, false
	}
	if v.Sym.IsGo || v.Sym.Def == nil || c.unstableVars[v.Sym] {
		return nil, false
	}
	return v.Sym, true
}

// rootDomainFor returns the inherited domain when present, else the
// type's full constructor signature as a domain.
func (c *checker) rootDomainFor(t types.Type, inherited *rootDomain) *rootDomain {
	if inherited != nil {
		return inherited
	}
	sig := c.env.signature(t)
	if sig == nil {
		return nil
	}
	tk := mustKeyOf(t)
	keys := make(map[string]bool, len(sig.ctors))
	for _, v := range sig.ctors {
		keys[variantKey(v, tk)] = true
	}
	return &rootDomain{keys: keys}
}

// filteredSignature returns sig's view restricted to domain. The
// variants themselves stay shared; only the root constructor list is
// narrowed.
func (c *checker) filteredSignature(t types.Type, domain *rootDomain) *signature {
	sig := c.env.signature(t)
	if sig == nil || domain == nil {
		return sig
	}
	tk := mustKeyOf(t)
	out := &signature{ctors: make([]*Variant, 0, len(sig.ctors))}
	for _, v := range sig.ctors {
		if domain.keys[variantKey(v, tk)] {
			out.ctors = append(out.ctors, v)
		}
	}
	return out
}

// positiveRootKeys returns the root constructor keys the pattern can
// match, or nil when the pattern places no root constructor restriction
// on the value (a wildcard/var root, an unknown form, or a mixture).
func (c *checker) positiveRootKeys(p check.Pat) map[string]bool {
	alts := c.cv.alts(p)
	if len(alts) == 0 {
		return nil
	}
	keys := make(map[string]bool)
	for _, alt := range alts {
		if alt.key == "" || strings.HasPrefix(alt.key, "u:") {
			return nil
		}
		keys[alt.key] = true
	}
	return keys
}

// definiteRootKeys returns the root constructor keys the unguarded
// pattern covers for every payload. A root wildcard/var covers the
// whole domain and reports all=true. Complementary partial patterns
// are intentionally not joined across arms or or-pattern alternatives;
// the first stage only uses constructors one alternative covers whole.
func (c *checker) definiteRootKeys(p check.Pat) (keys map[string]bool, all bool) {
	keys = make(map[string]bool)
	for _, alt := range c.cv.alts(p) {
		if alt.key == "" {
			return nil, true
		}
		covered := true
		for _, arg := range alt.args {
			if !c.cellCoversAll(arg) {
				covered = false
				break
			}
		}
		if covered {
			keys[alt.key] = true
		}
	}
	return keys, false
}

// cellCoversAll reports whether one converted pattern cell covers its
// whole type. Wildcard cells are total; constructor cells are tested
// through the ordinary matrix so single-constructor payloads and
// or-expanded total sub-patterns count as whole-covering patterns.
func (c *checker) cellCoversAll(x *cell) bool {
	if x.key == "" {
		return true
	}
	return len(c.missing(matrix{rows: [][]*cell{{x}}}, []types.Type{x.typ}, 1)) == 0
}

func intersectDomain(domain *rootDomain, keys map[string]bool) *rootDomain {
	out := make(map[string]bool)
	for key := range domain.keys {
		if keys[key] {
			out[key] = true
		}
	}
	return &rootDomain{keys: out}
}

func withoutKeys(domain *rootDomain, keys map[string]bool) *rootDomain {
	out := make(map[string]bool, len(domain.keys))
	for key := range domain.keys {
		if !keys[key] {
			out[key] = true
		}
	}
	return &rootDomain{keys: out}
}

func emptyDomain() *rootDomain {
	return &rootDomain{keys: map[string]bool{}}
}

// rootAlias returns the symbol bound by a root plain variable
// pattern. `as` and variables inside or-patterns are deliberately not
// aliases in the first stage.
func rootAlias(p check.Pat) *resolve.Symbol {
	if v, ok := p.(*check.VarPat); ok {
		return v.Sym
	}
	return nil
}

type narrowingSave struct {
	sym     *resolve.Symbol
	old     *rootDomain
	existed bool
}

func (c *checker) pushNarrowing(syms []*resolve.Symbol, domain *rootDomain) []narrowingSave {
	seen := make(map[*resolve.Symbol]bool, len(syms))
	var saved []narrowingSave
	for _, sym := range syms {
		if sym == nil || seen[sym] {
			continue
		}
		seen[sym] = true
		old, existed := c.narrowed[sym]
		saved = append(saved, narrowingSave{sym: sym, old: old, existed: existed})
		c.narrowed[sym] = domain
	}
	return saved
}

func (c *checker) popNarrowing(saved []narrowingSave) {
	for i := len(saved) - 1; i >= 0; i-- {
		s := saved[i]
		if s.existed {
			c.narrowed[s.sym] = s.old
		} else {
			delete(c.narrowed, s.sym)
		}
	}
}
