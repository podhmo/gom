package result_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/podhmo/gom/rt"
	"github.com/podhmo/gom/stdlib/result"
)

func closer(log *[]string, name string, err error) func(struct{}) result.Result[struct{}, error] {
	return func(struct{}) result.Result[struct{}, error] {
		*log = append(*log, name)
		if err != nil {
			return result.Err[struct{}, error](err)
		}
		return result.Ok[struct{}, error](struct{}{})
	}
}

func rollback(log *[]string) func(struct{}) struct{} {
	return func(struct{}) struct{} { *log = append(*log, "rollback"); return struct{}{} }
}

func TestScopeRunResultJoinsCleanupErrors(t *testing.T) {
	var log []string
	e1, e2 := errors.New("close a"), errors.New("close b")
	r := result.ScopeRunResult(func(s *rt.Scope) result.Result[int, error] {
		rt.ScopeOnErr(s, rollback(&log))
		result.ScopeDeferErr(s, closer(&log, "a", e1))
		result.ScopeDeferErr(s, closer(&log, "b", e2))
		return result.Ok[int, error](1)
	})
	if r.IsOk() || r.Error.Error() != "close b\nclose a" {
		t.Fatalf("r = %#v, want Err(close b; close a)", r)
	}
	if want := []string{"b", "a"}; !slices.Equal(log, want) {
		t.Fatalf("log = %v, want %v (on_err does not run for a cleanup error)", log, want)
	}
}

func TestScopeRunResultBodyErrorWins(t *testing.T) {
	var log []string
	body := errors.New("body")
	r := result.ScopeRunResult(func(s *rt.Scope) result.Result[int, error] {
		result.ScopeDeferErr(s, closer(&log, "a", errors.New("close a")))
		rt.ScopeOnErr(s, rollback(&log))
		return result.Err[int, error](body)
	})
	if r.IsOk() || r.Error != body {
		t.Fatalf("r = %#v, want Err(body)", r)
	}
	if want := []string{"rollback", "a"}; !slices.Equal(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
}

func TestScopeRunResultOk(t *testing.T) {
	var log []string
	r := result.ScopeRunResult(func(s *rt.Scope) result.Result[int, error] {
		rt.ScopeOnErr(s, rollback(&log))
		result.ScopeDeferErr(s, closer(&log, "a", nil))
		return result.Ok[int, error](7)
	})
	if !r.IsOk() || r.Value != 7 {
		t.Fatalf("r = %#v, want Ok(7)", r)
	}
	if want := []string{"a"}; !slices.Equal(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
}

func TestScopeDeferErrInvalidResultPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an invalid cleanup Result must panic")
		}
	}()
	result.ScopeRunResult(func(s *rt.Scope) result.Result[int, error] {
		result.ScopeDeferErr(s, func(struct{}) result.Result[struct{}, error] {
			return result.Result[struct{}, error]{}
		})
		return result.Ok[int, error](1)
	})
	t.Fatal("unreachable: ScopeRunResult returned")
}
