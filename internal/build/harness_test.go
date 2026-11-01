package build_test

import (
	"errors"
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/gomtest"
)

// These tests run the file-based suites under testdata/ end to end.
// The compile functions below are placeholders: every non-skipped case
// fails until the real pipeline exists (P1-3 onwards). As phases land,
// tests here (or in the phase packages) should swap the stubs for
// partial pipelines, e.g. a check-only compile that runs
// syntax → resolve → check and returns the collected diagnostics.
//
// A case that cannot pass yet is kept in the tree with a
// <name>.case.json skip reason instead of being removed.

func stubCompile(gomtest.File) ([]diag.Diagnostic, error) {
	return nil, errors.New("gom pipeline is not implemented yet")
}

func stubGenerate(gomtest.File) ([]byte, error) {
	return nil, errors.New("gom codegen is not implemented yet")
}

func TestSpec(t *testing.T) {
	gomtest.RunSpecTests(t, "", stubCompile)
}

func TestGolden(t *testing.T) {
	gomtest.RunGoldenTests(t, "", stubGenerate)
}
