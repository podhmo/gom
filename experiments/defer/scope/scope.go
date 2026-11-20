// Package scope is the runtime half of candidate (D): an explicit
// cleanup-stack handle. A Gom function opens a scope with a loan
// combinator, registers cleanups on the handle wherever it likes
// (inside an if, a match arm, a nested lambda), and the combinator
// runs them LIFO when the body returns, fails, or panics.
package scope

import "github.com/podhmo/gom/stdlib/result"

type Scope struct {
	cleanups []cleanup
	closed   bool
}

type cleanup struct {
	run     func() error
	onError bool // errdefer: run only when the body failed or panicked
}

// Defer registers a cleanup whose own failure is not interesting.
func (s *Scope) Defer(f func()) { s.push(cleanup{run: func() error { f(); return nil }}) }

// DeferErr registers a cleanup whose error joins the scope result when
// the body itself succeeded (the "close error on a written file" case).
func (s *Scope) DeferErr(f func() error) { s.push(cleanup{run: f}) }

// OnError registers a cleanup that runs only when the body returns Err
// or panics (rollback, remove a half-written file).
func (s *Scope) OnError(f func()) {
	s.push(cleanup{run: func() error { f(); return nil }, onError: true})
}

func (s *Scope) push(c cleanup) {
	if s.closed {
		panic("scope: cleanup registered after the scope closed")
	}
	s.cleanups = append(s.cleanups, c)
}

func (s *Scope) close(failed bool) error {
	s.closed = true
	var first error
	for i := len(s.cleanups) - 1; i >= 0; i-- {
		c := s.cleanups[i]
		if c.onError && !failed {
			continue
		}
		if err := c.run(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Run is the plain loan combinator: cleanups run LIFO on return and
// on panic; cleanup errors are dropped.
func Run[A any](body func(*Scope) A) A {
	s := &Scope{}
	ok := false
	defer func() { _ = s.close(!ok) }()
	a := body(s)
	ok = true
	return a
}

// RunResult is the Result-aware combinator: an Err body runs the
// OnError cleanups too, and the first cleanup error replaces an Ok
// result (the body's own error wins otherwise — the Go idiom
// `if cerr := f.Close(); err == nil { err = cerr }`).
func RunResult[A any](body func(*Scope) result.Result[A, error]) (r result.Result[A, error]) {
	s := &Scope{}
	ok := false
	defer func() {
		if !ok { // panicking: run everything, keep the panic
			_ = s.close(true)
			return
		}
		failed := !r.IsOk()
		if err := s.close(failed); err != nil && !failed {
			r = result.Err[A, error](err)
		}
	}()
	r = body(s)
	ok = true
	return r
}
