package syntax_test

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/gomtest"
	"github.com/podhmo/gom/internal/syntax"
)

// TestSpec runs the parse-only spec suite in testdata/spec: each
// case's member files are fed through Parse — .gomi members through
// ParseInterface — and the diagnostics are compared against the
// expected JSON. These cases pin the grammar rules of
// docs/grammar.ebnf that a parser can check without later phases —
// error recovery is deliberately not represented here (it is
// covered by unit tests in parser_test.go).
func TestSpec(t *testing.T) {
	gomtest.RunSpecTests(t, "testdata/spec", func(fs []gomtest.File) ([]diag.Diagnostic, error) {
		var out []diag.Diagnostic
		for _, f := range fs {
			parse := syntax.Parse
			if gomtest.IsInterfaceFile(f.Path) {
				parse = syntax.ParseInterface
			}
			_, diags := parse(f.Src, f.Path)
			out = append(out, []diag.Diagnostic(diags)...)
		}
		return out, nil
	})
}
