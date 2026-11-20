package result

import rt "github.com/podhmo/gom/rt"

// ScopeRunResult is `Scope.run_result`: like rt.ScopeRun, but an Err
// body also runs the on_err cleanups, and the body's own error wins
// over cleanup errors. When the body is Ok, the joined errors of the
// failed defer_err cleanups replace the result
// (decisions/2026-11-21-scoped-cleanup).
func ScopeRunResult[A any](body func(*rt.Scope) Result[A, error]) (r Result[A, error]) {
	s := &rt.Scope{}
	ok := false
	defer func() {
		if !ok {
			_ = s.Close(true)
			return
		}
		failed := !r.IsOk()
		if err := s.Close(failed); err != nil && !failed {
			r = Err[A, error](err)
		}
	}()
	r = body(s)
	ok = true
	return r
}

// ScopeDeferErr is `Scope.defer_err`: f runs when the scope closes and
// its Err joins the scope result (see ScopeRunResult). An invalid
// (zero) Result panics rather than counting as a successful cleanup.
func ScopeDeferErr(s *rt.Scope, f func(struct{}) Result[struct{}, error]) {
	s.Push(func() error {
		_, err := f(struct{}{}).Unwrap()
		return err
	}, false)
}
