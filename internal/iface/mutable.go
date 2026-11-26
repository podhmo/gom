package iface

import (
	"strings"

	"github.com/podhmo/gom/internal/resolve"
)

// ScanMutable collects the type names of `//gom:mutable <name>`
// directive comments from .gomi source text (spec section 10) — the
// read side of the directive `gom info` carries across regeneration.
// Every importer of a .gomi — gomimport's Loader for an ordinary
// package, internal/stdlib's env for the embedded builtin modules —
// runs it over the interface source.
//
// The directive is name-only (unlike `//gom:noescape`, it lists no
// parameter indices): it marks a declaration type whose
// implementation the interface does not show — an abstract `type T`
// line or an extern-backed body — as containing mutable values, so
// an importer's mutable-sharing lint judges its values like `Ref` or
// `Array` payloads.
func ScanMutable(src []byte) []string {
	var names []string
	for _, line := range attachedDirectiveLines(src, "//gom:mutable ") {
		fields := strings.Fields(strings.TrimPrefix(line, "//gom:mutable "))
		if len(fields) != 1 {
			continue
		}
		names = append(names, fields[0])
	}
	return names
}

// ApplyMutable turns `//gom:mutable` directives into the Mutable
// flag on the named members' symbols. Lines naming unknown members
// or non-type members are ignored: the line is a comment, so a
// stale one must not break the compile.
func ApplyMutable(members map[string]*resolve.Symbol, names []string) {
	for _, name := range names {
		if sym := members[name]; sym != nil && sym.Kind == resolve.KindType {
			sym.Mutable = true
		}
	}
}
