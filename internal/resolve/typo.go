package resolve

import (
	"fmt"
	"sort"
	"strings"

	"github.com/podhmo/gom/internal/diag"
)

type memberTypoCandidate struct {
	name     string
	distance int
}

// memberTypoCandidates returns the nearest member names in table for
// name. The first implementation deliberately keeps the threshold at
// optimal-string-alignment distance 1: enough for one-letter typos and
// transpositions, too narrow for the noisy distance-2 lookalikes.
func memberTypoCandidates(table map[string]*Symbol, name string) []memberTypoCandidate {
	var out []memberTypoCandidate
	for member, sym := range table {
		if sym != nil && sym.Hidden != "" {
			continue // correcting to this member would only move the user to E0501
		}
		if d := osaDistance(name, member); d == 1 {
			out = append(out, memberTypoCandidate{name: member, distance: d})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].distance != out[j].distance {
			return out[i].distance < out[j].distance
		}
		return out[i].name < out[j].name
	})
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// valueTypoCandidates returns nearby value-level names visible through
// the lexical scope chain. Names are deduplicated before filtering so an
// inner binding — even one unusable in this position — shadows an outer
// binding of the same spelling.
func valueTypoCandidates(sc *scope, name string, inTest bool) []memberTypoCandidate {
	if len([]rune(name)) < 3 {
		return nil
	}
	seen := map[string]bool{}
	var out []memberTypoCandidate
	for ; sc != nil; sc = sc.parent {
		for member, sym := range sc.terms {
			if seen[member] {
				continue
			}
			seen[member] = true
			if sym == nil || !valueCandidateKind(sym.Kind) {
				continue
			}
			if sym.Hidden != "" || (sym.TestOnly && !inTest) || (sym.Kind == KindBuiltin && testOnlyBuiltinName(member)) {
				continue
			}
			if d := osaDistance(name, member); d == 1 {
				out = append(out, memberTypoCandidate{name: member, distance: d})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].distance != out[j].distance {
			return out[i].distance < out[j].distance
		}
		return out[i].name < out[j].name
	})
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func valueCandidateKind(k Kind) bool {
	switch k {
	case KindVar, KindFunc, KindCtor, KindBuiltin:
		return true
	case KindType, KindField, KindModule, KindTypeVar, KindMethod:
		return false
	default:
		panic("unreachable")
	}
}

func testOnlyBuiltinName(name string) bool {
	switch name {
	case "assert", "assert_eq", "snapshot":
		return true
	default:
		return false
	}
}

func addValueTypoCandidates(d diag.Diagnostic, sc *scope, name string, inTest bool) diag.Diagnostic {
	return addTypoCandidates(d, valueTypoCandidates(sc, name, inTest))
}

func addMemberTypoCandidates(d diag.Diagnostic, table map[string]*Symbol, name string) diag.Diagnostic {
	return addTypoCandidates(d, memberTypoCandidates(table, name))
}

func addTypoCandidates(d diag.Diagnostic, candidates []memberTypoCandidate) diag.Diagnostic {
	if len(candidates) == 0 {
		return d
	}
	data := make([]map[string]any, len(candidates))
	quoted := make([]string, len(candidates))
	for i, c := range candidates {
		data[i] = map[string]any{"name": c.name, "edit_distance": c.distance}
		quoted[i] = "`" + c.name + "`"
	}
	d = d.Set("candidates", data)
	if len(quoted) == 1 {
		return d.AddNote(fmt.Sprintf("did you mean %s?", quoted[0]))
	}
	return d.AddNote(fmt.Sprintf("did you mean one of %s, or %s?",
		strings.Join(quoted[:len(quoted)-1], ", "), quoted[len(quoted)-1]))
}

// osaDistance is the optimal string alignment edit distance: insertion,
// deletion, substitution, and adjacent transposition each cost one. It
// operates on runes because member names are user-facing identifiers.
func osaDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prevprev := make([]int, len(rb)+1)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				cur[j] = min(cur[j], prevprev[j-2]+1)
			}
		}
		copy(prevprev, prev)
		copy(prev, cur)
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	return min(min(a, b), c)
}
