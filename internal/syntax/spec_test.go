package syntax_test

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/gomtest"
	"github.com/podhmo/gom/internal/syntax"
)

// TestSpec runs the parse-only spec suite in testdata/spec: each
// .gom file is fed through Parse and the diagnostics are compared
// against the expected JSON. These cases pin the grammar rules of
// docs/grammar.ebnf that a parser can check without later phases —
// error recovery is deliberately not represented here (it is
// covered by unit tests in parser_test.go).
func TestSpec(t *testing.T) {
	gomtest.RunSpecTests(t, "testdata/spec", func(f gomtest.File) ([]diag.Diagnostic, error) {
		_, diags := syntax.Parse(f.Src, f.Path)
		return []diag.Diagnostic(diags), nil
	})
}
