package gomgen

// White-box tests for the grammar validation internals that the
// load contract relies on: minimum-depth bookkeeping (which rules
// fail to converge and only those become depthInfinite) and the
// merge-risk table derived from the grammar's own terminals.

import (
	"strings"
	"testing"
)

// A reachable rule whose recursion has no base case can never
// complete an expansion — Generate would recurse until the goroutine
// stack bursts. ParseGrammar must reject it at load: the grammar is
// the drift detector, not a crash source.
func TestParseGrammarRejectsReachableDivergence(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"self seq", "source-file = a ;\na = a , \"x\" ;\n"},
		{"mutual seq", "source-file = a ;\na = \"[\" , b ;\nb = \"]\" , a ;\n"},
		{"deep", "source-file = a ;\na = b ;\nb = b , \"x\" ;\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseGrammar([]byte(tc.src))
			if err == nil {
				t.Fatalf("divergent reachable grammar must fail to load:\n%s", tc.src)
			}
			if !strings.Contains(err.Error(), "expand finitely") {
				t.Fatalf("expected 'cannot expand finitely' error, got: %v", err)
			}
		})
	}
	// "a = a , { \"x\" }" is divergent too, but through a different
	// detector: the rep is skippable so the ref is the sequence's
	// only mandatory member — a forced-descent self-cycle.
	if _, err := ParseGrammar([]byte("source-file = a ;\na = a , { \"x\" } ;\n")); err == nil {
		t.Fatal("rep-shielded self recursion must fail to load")
	}
}

// Forced-descent cycles ("a = b ; b = a ;") converge to a finite
// minDepth — they spend no depth budget at all — so they need their
// own detector. Without it the walk alternates between the two
// rules forever, never emitting a token.
func TestParseGrammarRejectsForcedCycle(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"alias pair", "source-file = a ;\na = b ;\nb = a ;\n"},
		{"self alias", "source-file = a ;\na = a ;\n"},
		{"seq sole ref", "source-file = a ;\na = [ \"(\" ] , b , { \")\" } ;\nb = a ;\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseGrammar([]byte(tc.src))
			if err == nil {
				t.Fatalf("forced-descent cycle must fail to load:\n%s", tc.src)
			}
			if !strings.Contains(err.Error(), "forced-descent cycle") {
				t.Fatalf("expected 'forced-descent cycle' error, got: %v", err)
			}
		})
	}
}

// Rules that source-file cannot reach are outside the walker's
// contract: a divergent rule there is dead weight, not a load
// error — and only the non-converged rules may take depthInfinite.
func TestParseGrammarUnreachableDivergenceAllowed(t *testing.T) {
	g, err := ParseGrammar([]byte(
		"source-file = \"x\" ;\n" +
			"ok = \"y\" ;\n" +
			"bad = bad , \"z\" ;\n"))
	if err != nil {
		t.Fatalf("unreachable divergent rule must not fail the load: %v", err)
	}
	if got := g.minDepth["bad"]; got != depthInfinite {
		t.Fatalf("minDepth[bad] = %d, want depthInfinite", got)
	}
	if got := g.minDepth["ok"]; got != 0 {
		t.Fatalf("minDepth[ok] = %d, want 0 — the convergence fallback must not poison converged rules", got)
	}
	if got := g.minDepth[SourceFile]; got != 0 {
		t.Fatalf("minDepth[source-file] = %d, want 0", got)
	}
}

// Alias chains and single-mandatory-member descents are finite —
// they must load and their refs must expand at zero depth cost.
func TestParseGrammarForcedChainsAreFinite(t *testing.T) {
	g, err := ParseGrammar([]byte(
		"source-file = a ;\n" +
			"a = b ;\n" +
			"b = c ;\n" +
			"c = \"x\" ;\n"))
	if err != nil {
		t.Fatalf("finite alias chain must load: %v", err)
	}
	for _, name := range []string{"a", "b", "c", SourceFile} {
		if got := g.minDepth[name]; got != 0 {
			t.Fatalf("minDepth[%s] = %d, want 0 (forced descent is free)", name, got)
		}
	}
}

// mergeRisk is derived from the grammar's own terminal set: every
// terminal of two or more bytes contributes its first two bytes as
// a pair that must not be glued. Checking coverage keeps the table
// honest when the operator set in docs/grammar.ebnf changes.
func TestMergeRiskCoversAllMultiByteTerminals(t *testing.T) {
	g, err := LoadGrammar("../../docs/grammar.ebnf")
	if err != nil {
		t.Fatalf("LoadGrammar: %v", err)
	}
	var check func(e gexpr)
	check = func(e gexpr) {
		switch x := e.(type) {
		case term:
			if len(x) >= 2 && !g.mergeRisk[[2]byte{x[0], x[1]}] {
				t.Errorf("terminal %q first-two-byte pair %q not in mergeRisk", string(x), string(x[:2]))
			}
		case seq:
			for _, m := range x {
				check(m)
			}
		case alt:
			for _, m := range x {
				check(m)
			}
		case opt:
			check(x.x)
		case rep:
			check(x.x)
		case ref, special:
		default:
			panic("unreachable")
		}
	}
	for _, name := range g.order {
		check(g.rules[name])
	}
}
