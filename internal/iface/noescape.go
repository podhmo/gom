package iface

import (
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// ScanNoescape collects `//gom:noescape <name> <i> ...` directive
// comments from .gomi source text into a name → index-list map —
// the read side of the directive noescapeLine writes (spec section
// 6.1, SC-2). Every importer of a .gomi — gomimport's Loader for an
// ordinary package, internal/stdlib's env for the embedded builtin
// modules — runs it over the interface source.
func ScanNoescape(src []byte) map[string][]int {
	return scanDirective(src, "//gom:noescape ")
}

// scanDirective is the shared `//gom:<directive> <name> <i> ...`
// line scan — a leading name field plus non-negative indices, the
// whole line ignored when a field fails to parse. Only directives
// attached to a declaration count: spec's comment-run rule is the
// same one doc comments use, so a blank line or non-comment line
// between the directive and the declaration detaches it.
func scanDirective(src []byte, prefix string) map[string][]int {
	var dirs map[string][]int
	for _, line := range attachedDirectiveLines(src, prefix) {
		fields := strings.Fields(strings.TrimPrefix(line, prefix))
		if len(fields) < 2 {
			continue
		}
		var idxs []int
		for _, f := range fields[1:] {
			i, err := strconv.Atoi(f)
			if err != nil || i < 0 {
				idxs = nil
				break
			}
			idxs = append(idxs, i)
		}
		if idxs == nil {
			continue
		}
		if dirs == nil {
			dirs = map[string][]int{}
		}
		dirs[fields[0]] = idxs
	}
	return dirs
}

// attachedDirectiveLines returns trimmed directive lines that belong
// to a contiguous line-comment run immediately preceding a .gomi
// declaration. The declaration may start with an attribute line
// (`@extern`/`@method`), in which case the run attaches there.
type attachedDirectiveLine struct {
	text   string
	target string
}

func attachedDirectiveLines(src []byte, prefix string) []string {
	pairs := attachedDirectivePairs(src, prefix)
	out := make([]string, len(pairs))
	for i, p := range pairs {
		out[i] = p.text
	}
	return out
}

func attachedDirectivePairs(src []byte, prefix string) []attachedDirectiveLine {
	var out []attachedDirectiveLine
	var pending []string
	inBlock := false
	for _, raw := range strings.Split(string(src), "\n") {
		inside := inBlock
		inBlock = updateBlockState(raw, inBlock)
		line := strings.TrimSpace(raw)
		if inside || line == "" {
			pending = nil
			continue
		}
		if strings.HasPrefix(line, "//") {
			if strings.HasPrefix(line, prefix) {
				pending = append(pending, line)
			}
			continue
		}
		if interfaceDeclStart(line) {
			for _, text := range pending {
				out = append(out, attachedDirectiveLine{text: text, target: line})
			}
		}
		pending = nil
	}
	return out
}

func interfaceDeclStart(line string) bool {
	if strings.HasPrefix(line, "@") {
		return true
	}
	fields := strings.Fields(line)
	return len(fields) > 0 && (fields[0] == "fn" || fields[0] == "let" || fields[0] == "type")
}

// ApplyNoescape turns `//gom:noescape` directives into
// per-parameter escape flags on the package's members. A listed
// position gets Escapes[i]=false; an unlisted position stays true —
// escaping is the default the flag slice makes explicit.
// Directives naming unknown members or out-of-range indices are
// ignored: the line is a comment, so a stale one must not break the
// compile.
//
// arityOf answers a member's declared parameter count, sigOf its
// signature — consulted in that order: an arity misses members the
// table did not record one for, a signature's flat Params always
// counts them.
func ApplyNoescape(members map[string]*resolve.Symbol, arityOf func(*resolve.Symbol) (int, bool), sigOf func(*resolve.Symbol) (check.Signature, bool), dirs map[string][]int) {
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
		esc := make([]bool, n)
		for i := range esc {
			esc[i] = true
		}
		for _, i := range idxs {
			if i >= 0 && i < n {
				esc[i] = false
			}
		}
		sym.Escapes = esc
	}
}

// updateBlockState walks one source line and returns the `/* */`
// block-comment state at its end. A `//` outside a block ends the
// scan — comment text cannot nest inside a line comment, and a
// block opener inside one never opens.
func updateBlockState(line string, in bool) bool {
	for i := 0; i < len(line); {
		if in {
			if strings.HasPrefix(line[i:], "*/") {
				in = false
				i += 2
			} else {
				i++
			}
		} else if strings.HasPrefix(line[i:], "/*") {
			in = true
			i += 2
		} else if strings.HasPrefix(line[i:], "//") {
			break
		} else {
			i++
		}
	}
	return in
}
