package gomgen_test

// Tests for the grammar-driven generator (docs/decisions/
// 2026-11-02-grammar-based-generator.md):
//
//   - the EBNF reader must accept docs/grammar.ebnf as-is, with every
//     rule reachable from source-file covered (ParseGrammar enforces
//     that at load)
//   - Generate is deterministic per seed and varies across seeds
//   - mechanical output mostly parses (parse diagnostics are user
//     diagnostics — well-formed and in-bounds — never panics)
//   - typed-mode output parses at a higher rate still
//
// Measured rates for the knobs below are recorded in
// docs/sketch/2026-11-02-v2-gen-measurements.md.

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"github.com/podhmo/gom/internal/gomgen"
	"github.com/podhmo/gom/internal/syntax"
)

func loadDocsGrammar(t *testing.T) *gomgen.Grammar {
	t.Helper()
	g, err := gomgen.LoadGrammar(filepath.Join("..", "..", "docs", "grammar.ebnf"))
	if err != nil {
		t.Fatalf("LoadGrammar(docs/grammar.ebnf): %v", err)
	}
	return g
}

func TestParseDocsGrammar(t *testing.T) {
	g := loadDocsGrammar(t)
	// The grammar file defines ~80 rules; a sanity floor catches a
	// reader that silently dropped half the file.
	if n := len(g.Rules()); n < 60 {
		t.Fatalf("expected at least 60 rules in grammar.ebnf, got %d", n)
	}
}

func TestDeterministic(t *testing.T) {
	g := loadDocsGrammar(t)
	for _, opts := range []gomgen.Options{
		{},
		{Typed: true},
		{Typed: true, Depth: 4, MaxDecls: 12},
		{Perturb: 0.02},
		{Depth: 2, MaxTokens: 60},
	} {
		var first string
		for seed := range uint64(5) {
			out := gomgen.Generate(g, rand.New(rand.NewPCG(seed, seed^0x9e37)), opts)
			if seed == 0 {
				first = out
				continue
			}
			if out == first {
				// Identical output across consecutive seeds would mean
				// the seed is being ignored.
				t.Fatalf("opts %+v: seed %d produced identical output to seed 0", opts, seed)
			}
		}
		// Same seed twice → byte-identical.
		a := gomgen.Generate(g, rand.New(rand.NewPCG(42, 42)), opts)
		b := gomgen.Generate(g, rand.New(rand.NewPCG(42, 42)), opts)
		if a != b {
			t.Fatalf("opts %+v: same seed produced different output\n--- a ---\n%s\n--- b ---\n%s", opts, a, b)
		}
	}
}

// parseFailures applies the same contract FuzzParse checks: a File
// is always returned and every diagnostic is well-formed with an
// in-bounds span. A parse error itself is NOT a violation — the
// generator is allowed to produce inputs the parser rejects with
// user diagnostics.
func parseFailures(src []byte) (failures []string) {
	f, diags := syntax.Parse(src, "gen.gom")
	if f == nil {
		failures = append(failures, "Parse returned nil File")
	}
	for i, d := range diags {
		if err := d.Validate(); err != nil {
			failures = append(failures, fmt.Sprintf(
				"diagnostic %d fails validation: %v\ndiag: %+v", i, err, d))
			continue
		}
		if !d.Span.IsZero() && (d.Span.End > len(src) || d.Span.Start > len(src)) {
			failures = append(failures, fmt.Sprintf(
				"diagnostic %d has out-of-bounds span %v (input %d bytes)",
				i, d.Span, len(src)))
		}
	}
	return failures
}

func TestGeneratedParses(t *testing.T) {
	g := loadDocsGrammar(t)
	const n = 300
	for _, mode := range []struct {
		name string
		opts gomgen.Options
		min  float64 // minimum clean-parse fraction required
	}{
		{"mechanical", gomgen.Options{}, 0.92},
		{"typed", gomgen.Options{Typed: true}, 0.95},
	} {
		clean := 0
		for seed := range uint64(n) {
			src := gomgen.Generate(g, rand.New(rand.NewPCG(seed, 0xbeef+seed)), mode.opts)
			for _, f := range parseFailures([]byte(src)) {
				t.Fatalf("mode %s seed %d: contract violation %s\n---\n%s", mode.name, seed, f, src)
			}
			if _, diags := syntax.Parse([]byte(src), "gen.gom"); !diags.HasError() {
				clean++
			}
		}
		rate := float64(clean) / n
		t.Logf("%s: %d/%d clean parses (%.1f%%)", mode.name, clean, n, rate*100)
		if rate < mode.min {
			t.Errorf("%s: clean-parse rate %.1f%% below floor %.1f%%", mode.name, rate*100, mode.min*100)
		}
	}
}

// TestGenerationNeverPanics runs generation itself under a panic
// guard — a generator panic is a generator bug, period.
func TestGenerationNeverPanics(t *testing.T) {
	g := loadDocsGrammar(t)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Generate panicked: %v", r)
		}
	}()
	for seed := range uint64(500) {
		opts := gomgen.Options{
			Typed:   seed%3 == 0,
			Perturb: map[uint64]float64{1: 0.05, 2: 0.15}[seed%3],
			Depth:   1 + int(seed%9),
		}
		out := gomgen.Generate(g, rand.New(rand.NewPCG(seed, seed+1)), opts)
		if len(out) == 0 {
			t.Fatalf("seed %d: empty output", seed)
		}
	}
}

func TestEBNFScannerRejectsGarbage(t *testing.T) {
	if _, err := gomgen.ParseGrammar([]byte("not a grammar")); err == nil {
		t.Fatal("expected an error parsing non-EBNF input")
	}
	if _, err := gomgen.ParseGrammar([]byte("a = \"x\" ;\nb = c ;\n")); err == nil {
		t.Fatal("expected undefined-reference error")
	}
	if _, err := gomgen.ParseGrammar([]byte("source-file = \"x\" ;\n")); err != nil {
		t.Fatalf("minimal grammar should parse: %v", err)
	}
}
