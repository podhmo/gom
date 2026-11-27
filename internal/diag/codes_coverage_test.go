package diag_test

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
)

type coverageKind string

const (
	coverageSpec     coverageKind = "spec fixture"
	coverageCLI      coverageKind = "CLI test"
	coverageUnit     coverageKind = "unit integration test"
	coverageRecovery coverageKind = "checker recovery unit test"
	coverageInternal coverageKind = "compiler-internal, coverage exempt"
	coverageReserved coverageKind = "reserved, currently unemitted"
)

type coverageException struct {
	kind coverageKind
	why  string
	// tests names the positive Go test functions carrying unit or
	// recovery coverage. Naming them keeps a negative "must not emit"
	// assertion or an incidental literal from counting as coverage.
	tests []string
}

// coverageExceptions is the explicit negative space of the diagnostic
// registry. A live source-language diagnostic normally needs a spec
// fixture; driver/interface diagnostics need a CLI test or spec fixture;
// evaluator diagnostics need a unit test. Anything else must be named
// here so a missing fixture is a conscious classification, not drift.
var coverageExceptions = map[diag.Code]coverageException{
	"E0204": {
		kind: coverageReserved,
		why:  "unknown attributes are currently rejected by the parser as E0101; the resolve-phase code is reserved until the AST can carry attributes",
	},
	"E0502": {
		kind:  coverageRecovery,
		why:   "value method selections are type-directed and unresolved by the resolver; the Gom-receiver branch is checker recovery for inconsistent resolution",
		tests: []string{"TestMethodOnGomValue"},
	},
	"E0503": {
		kind: coverageReserved,
		why:  "implicit numeric conversion currently has no production emitter",
	},
	"E0606": {
		kind:  coverageUnit,
		why:   "package import cycles need multi-module package-layout setup covered by integration tests",
		tests: []string{"TestImplImportCycleMutual", "TestIfaceImportCycleMutual"},
	},
	"E0704": {
		kind:  coverageRecovery,
		why:   "the source parser rejects destructuring select receive patterns as E0101; E0704 is checker recovery coverage",
		tests: []string{"TestSelectRecvArmCtorPat"},
	},
	"E0901": {
		kind: coverageInternal,
		why:  "lowering invariant violations are compiler-internal contracts, not expected user-source diagnostics",
	},
}

var expectedCodeRE = regexp.MustCompile(`"code"\s*:\s*"([EWN][0-9]{4})"`)

var evalTestConstantCodes = map[string]diag.Code{
	"CodeUnsupported": eval.CodeUnsupported,
	"CodeMatchFail":   eval.CodeMatchFail,
	"CodePanic":       eval.CodePanic,
	"CodeInternal":    eval.CodeInternal,
	"CodeTestFail":    eval.CodeTestFail,
	"CodeBudget":      eval.CodeBudget,
}

func TestLiveDiagnosticCoverageClassification(t *testing.T) {
	root := filepath.Join("..", "..")
	specCodes := scanSpecCodes(t, filepath.Join(root, "testdata", "spec"))
	cliCodes := scanGoStringCodes(t, filepath.Join(root, "cmd", "gom"), true)
	unitCodes := scanGoStringCodes(t, filepath.Join(root, "internal"), true)
	unitEvidence := scanGoTestCodeEvidence(t, filepath.Join(root, "internal"))

	for _, info := range diag.CodeTable() {
		if info.Retired {
			continue
		}
		code := info.Code
		if ex, ok := coverageExceptions[code]; ok {
			switch ex.kind {
			case coverageSpec:
				if !specCodes[code] {
					t.Errorf("%s: classified as %s (%s), but no spec fixture emits it", code, ex.kind, ex.why)
				}
			case coverageCLI:
				if !cliCodes[code] {
					t.Errorf("%s: classified as %s (%s), but no CLI test names it", code, ex.kind, ex.why)
				}
			case coverageUnit, coverageRecovery:
				if len(ex.tests) == 0 {
					t.Errorf("%s: classified as %s (%s), but names no positive evidence test", code, ex.kind, ex.why)
					continue
				}
				for _, testName := range ex.tests {
					if !unitEvidence[code][testName] {
						t.Errorf("%s: classified as %s (%s), but %s does not name it", code, ex.kind, ex.why, testName)
					}
				}
			case coverageInternal:
				// Compiler-internal invariant diagnostics are exempt from user-source
				// coverage. The explicit classification is the coverage decision.
			case coverageReserved:
				// The classification itself is the coverage. Emission sites are
				// checked below so a newly implemented code must be reclassified.
			}
			continue
		}

		switch info.Phase {
		case diag.PhaseSyntax, diag.PhaseResolve, diag.PhaseCheck, diag.PhasePattern, diag.PhaseLower, diag.PhaseCodegen, diag.PhaseGoImport, diag.PhaseLint:
			if !specCodes[code] {
				t.Errorf("%s (%s): live language diagnostic has no testdata/spec fixture", code, info.Phase)
			}
		case diag.PhaseDriver, diag.PhaseIface:
			if !cliCodes[code] && !specCodes[code] {
				t.Errorf("%s (%s): driver/interface diagnostic has neither CLI test nor spec fixture coverage", code, info.Phase)
			}
		case diag.PhaseEval:
			if !unitCodes[code] {
				t.Errorf("%s (eval): evaluator diagnostic has no unit test coverage", code)
			}
		default:
			t.Errorf("%s: unhandled phase %q in coverage classification", code, info.Phase)
		}
	}
}

// TestReservedDiagnosticsHaveNoEmitter keeps reserved codes honest: a
// production emitter appearing for one means it is live again and needs
// fixture or test coverage instead of the reserved classification.
func TestReservedDiagnosticsHaveNoEmitter(t *testing.T) {
	root := filepath.Join("..", "..")
	production := scanGoStringCodes(t, filepath.Join(root, "internal"), false)
	for code, ex := range coverageExceptions {
		if ex.kind != coverageReserved {
			continue
		}
		if production[code] {
			t.Errorf("%s is classified as reserved (%s), but a production emitter string exists; reclassify and add coverage", code, ex.why)
		}
	}
}

func scanSpecCodes(t *testing.T, dir string) map[diag.Code]bool {
	t.Helper()
	codes := map[diag.Code]bool{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		base := filepath.Base(path)
		if base != "expected.json" && filepath.Ext(base[:len(base)-len(".json")]) != ".expected" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range expectedCodeRE.FindAllSubmatch(b, -1) {
			codes[diag.Code(m[1])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return codes
}

// scanGoTestCodeEvidence maps diagnostic-code literals to the top-level
// Go test functions whose bodies contain them. Unlike a package-wide
// literal scan, this lets the classification name the positive test it
// means; a literal in an unrelated negative test does not count.
func scanGoTestCodeEvidence(t *testing.T, dir string) map[diag.Code]map[string]bool {
	t.Helper()
	out := map[diag.Code]map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Base(path) == "codes_coverage_test.go" || filepath.Base(path) == "explain_examples_test.go" {
			return nil
		}
		fset := token.NewFileSet()
		f, err := goparser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil || !diag.WellFormed(diag.Code(s)) {
					return true
				}
				code := diag.Code(s)
				if out[code] == nil {
					out[code] = map[string]bool{}
				}
				out[code][fn.Name.Name] = true
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// scanGoStringCodes finds exact diagnostic-code string literals in Go
// files. testsOnly=true scans only *_test.go; false scans production
// files. Comments are ignored, so design notes do not count as emitters.
func scanGoStringCodes(t *testing.T, dir string, testsOnly bool) map[diag.Code]bool {
	t.Helper()
	codes := map[diag.Code]bool{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		isTest := strings.HasSuffix(path, "_test.go")
		if testsOnly != isTest {
			return nil
		}
		base := filepath.Base(path)
		if base == "codes_coverage_test.go" || base == "explain_examples_test.go" || (!testsOnly && base == "codes.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := goparser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(n.Value)
				if err != nil || !diag.WellFormed(diag.Code(s)) {
					return true
				}
				codes[diag.Code(s)] = true
			case *ast.SelectorExpr:
				if testsOnly {
					if x, ok := n.X.(*ast.Ident); ok && x.Name == "eval" {
						if code, ok := evalTestConstantCodes[n.Sel.Name]; ok {
							codes[code] = true
						}
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return codes
}
