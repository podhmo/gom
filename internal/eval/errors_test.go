package eval_test

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
)

// TestEvalCodesRegistered keeps every diagnostic code the evaluator
// emits (errors.go) registered in internal/diag's code table, in the
// E90xx "Evaluation" category with phase "eval".
func TestEvalCodesRegistered(t *testing.T) {
	for _, code := range []diag.Code{
		eval.CodeUnsupported,
		eval.CodeMatchFail,
		eval.CodePanic,
		eval.CodeInternal,
		eval.CodeTestFail,
	} {
		if string(code)[1:3] != "90" {
			t.Errorf("%s is outside the reserved E90xx category", code)
		}
		info, ok := diag.LookupCode(code)
		if !ok {
			t.Errorf("%s is not registered in the diag code table", code)
			continue
		}
		if info.Severity != diag.SeverityError {
			t.Errorf("%s: severity %v, want error", code, info.Severity)
		}
		if info.Phase != diag.PhaseEval {
			t.Errorf("%s: phase %q, want %q", code, info.Phase, diag.PhaseEval)
		}
		if info.Retired {
			t.Errorf("%s must not be retired", code)
		}
	}
}
