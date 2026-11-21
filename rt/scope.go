package rt

import "errors"

// Scope is the Go representation of Gom's builtin `Scope`: the
// cleanup stack a `Scope.run` / `Scope.run_result` body registers on
// (decisions/2026-11-21-scoped-cleanup). Cleanups run LIFO when the
// scope closes — on return, on a `try` escape out of the body and on
// panic. Registering on a closed scope panics.
type Scope struct {
	cleanups []scopeCleanup
	closed   bool
}

type scopeCleanup struct {
	run     func() error
	onError bool
}

// Push registers a cleanup. onError cleanups run only when the scope
// fails (an Err result or a panic).
func (s *Scope) Push(run func() error, onError bool) {
	if s.closed {
		panic("gom: cleanup registered on a closed Scope")
	}
	s.cleanups = append(s.cleanups, scopeCleanup{run: run, onError: onError})
}

// Close runs the cleanups LIFO and joins their errors in run order.
// Each cleanup is its own Go defer, so one that panics does not skip
// the earlier registrations; the panic resumes after all of them.
func (s *Scope) Close(failed bool) (joined error) {
	s.closed = true
	var errs []error
	defer func() { joined = errors.Join(errs...) }()
	for _, c := range s.cleanups {
		if c.onError && !failed {
			continue
		}
		defer func() {
			if err := c.run(); err != nil {
				errs = append(errs, err)
			}
		}()
	}
	return nil
}

// NewScope returns a fresh scope. SC-3's native-defer lowering
// passes it to a body whose registrations became Go defers, so the
// scope exists only to satisfy the body's parameter — nothing ever
// registers on it.
func NewScope() *Scope { return &Scope{} }

// ScopeRun is `Scope.run`: it runs body with a fresh scope and closes
// it afterwards. Cleanup errors are dropped; a panicking body still
// runs every cleanup, on_err ones included.
func ScopeRun[A any](body func(*Scope) A) A {
	s := &Scope{}
	ok := false
	defer func() { _ = s.Close(!ok) }()
	a := body(s)
	ok = true
	return a
}

// ScopeDefer is `Scope.defer`: f runs when the scope closes.
func ScopeDefer(s *Scope, f func(struct{}) struct{}) {
	s.Push(func() error { f(struct{}{}); return nil }, false)
}

// ScopeOnErr is `Scope.on_err`: f runs when the scope closes after an
// Err result or a panic.
func ScopeOnErr(s *Scope, f func(struct{}) struct{}) {
	s.Push(func() error { f(struct{}{}); return nil }, true)
}
