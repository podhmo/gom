package iface

import (
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// ScanNotaint collects `//gom:result-notaint <name> <i> ...`
// directive comments from .gomi source text into a name →
// index-list map — the read side of the directive notaintLine
// writes (spec section 6.1). Every importer of a .gomi —
// gomimport's Loader for an ordinary package, internal/stdlib's
// env for the embedded builtin modules — runs it over the
// interface source.
func ScanNotaint(src []byte) map[string][]int {
	return scanDirective(src, "//gom:result-notaint ")
}

// ApplyNotaint turns `//gom:result-notaint` directives into
// per-parameter result-carry flags on the package's members. A
// listed position gets ResultTaint[i]=false; an unlisted position
// stays true — carrying the parameter's products into the result
// is the conservative default the flag slice makes explicit.
// Directives naming unknown members or out-of-range indices are
// ignored: the line is a comment, so a stale one must not break
// the compile.
//
// arityOf answers a member's declared parameter count, sigOf its
// signature — consulted in that order, as for ApplyNoescape.
func ApplyNotaint(members map[string]*resolve.Symbol, arityOf func(*resolve.Symbol) (int, bool), sigOf func(*resolve.Symbol) (check.Signature, bool), dirs map[string][]int) {
	for name, idxs := range dirs {
		sym := members[name]
		if sym == nil {
			continue
		}
		n, _ := arityOf(sym)
		if n <= 0 {
			if sg, ok := sigOf(sym); ok {
				if f, ok := types.Chase(sg.Type).(*types.Func); ok {
					n = len(f.Params)
				}
			}
		}
		if n <= 0 {
			continue
		}
		taint := make([]bool, n)
		for i := range taint {
			taint[i] = true
		}
		for _, i := range idxs {
			if i >= 0 && i < n {
				taint[i] = false
			}
		}
		sym.ResultTaint = taint
	}
}
