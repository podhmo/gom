package eval

import (
	"github.com/podhmo/gom/internal/diag"
)

// Diagnostic codes emitted by the evaluator. They are registered in
// internal/diag/codes.go under the E90xx "Evaluation" category with
// phase "eval" (see docs/error-codes.md).
const (
	// CodeUnsupported marks a typed-AST construct the evaluator does
	// not implement in P1 scope: go/select/channel operations,
	// unstubbed Go calls, Ref/Array/Ptr values, Go.conv, misplaced
	// holes and spreads.
	CodeUnsupported = diag.Code("E9001")
	// CodeMatchFail marks a match expression whose arms all failed —
	// in checked programs this means a contract-violating variant
	// value (spec section 8.6); in hand-built ASTs it is a real
	// dispatch failure.
	CodeMatchFail = diag.Code("E9002")
	// CodePanic marks a Gom-level panic: todo (), a failed assert /
	// assert_eq, an invalid variant at a match, division by zero, and
	// panics raised by host functions.
	CodePanic = diag.Code("E9003")
	// CodeInternal marks a broken evaluator or compiler invariant:
	// an unbound symbol, a non-function applied, a pattern typed as
	// irrefutable that failed anyway, an escaping try with no
	// enclosing function boundary, or malformed literal text.
	CodeInternal = diag.Code("E9004")
	// CodeTestFail marks a `test` declaration that returned an Err
	// result (spec section 11).
	CodeTestFail = diag.Code("E9005")
)

// fail panics with a runtime failure carrying one diagnostic. The
// panic is recovered at the API boundary (evalWrapper) and turned
// into Result.Diags — evaluation panics never escape as Go panics.
func fail(code diag.Code, message string, sp diag.Span) {
	panic(failure{d: diag.Error(code, message).At(sp)})
}

// failData is fail with one structured-data entry.
func failData(code diag.Code, message string, sp diag.Span, key string, value any) {
	panic(failure{d: diag.Error(code, message).At(sp).Set(key, value)})
}

// failure is the panic value for runtime evaluation failures.
type failure struct{ d diag.Diagnostic }

// escape is the panic value for `try` escaping to the innermost
// enclosing function boundary (fn decl, lambda, test block). v is the
// Err value the function returns; sp is the try's own span, used for
// diagnostics when the escape crosses its last boundary.
type escape struct {
	v  Value
	sp diag.Span
}

// runEval executes f, converting eval panics into a diagnostic list.
// escape panics that reach here have crossed their last function
// boundary — an invariant violation (the checker requires every try
// to have an enclosing Result-returning function).
func runEval(f func() Value) (v Value, diags diag.List) {
	defer func() {
		switch r := recover(); r := r.(type) {
		case nil:
		case failure:
			diags = append(diags, r.d)
			v = nil
		case escape:
			diags = append(diags, diag.Error(CodeInternal,
				"try escaped with no enclosing function boundary").At(r.sp))
			v = nil
		default:
			panic(r)
		}
	}()
	v = f()
	return v, diags
}

// runBody executes a function body, converting a `try` escape into
// the returned Err value. Every function invocation is a boundary.
func runBody(f func() Value) (v Value) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(escape); ok {
				v = e.v
				return
			}
			panic(r)
		}
	}()
	return f()
}
