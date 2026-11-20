package rt_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/podhmo/gom/rt"
)

func push(s *rt.Scope, log *[]string, name string, err error, onError bool) {
	s.Push(func() error { *log = append(*log, name); return err }, onError)
}

func TestScopeCloseLIFOAndJoin(t *testing.T) {
	var log []string
	s := &rt.Scope{}
	e1, e2 := errors.New("e1"), errors.New("e2")
	push(s, &log, "a", e1, false)
	push(s, &log, "b", nil, true)
	push(s, &log, "c", e2, false)
	err := s.Close(false)
	if want := []string{"c", "a"}; !slices.Equal(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
	if !errors.Is(err, e1) || !errors.Is(err, e2) || err.Error() != "e2\ne1" {
		t.Fatalf("err = %q, want e2 then e1 joined", err)
	}
}

func TestScopeCloseFailedRunsOnErr(t *testing.T) {
	var log []string
	s := &rt.Scope{}
	push(s, &log, "a", nil, false)
	push(s, &log, "b", nil, true)
	if err := s.Close(true); err != nil {
		t.Fatal(err)
	}
	if want := []string{"b", "a"}; !slices.Equal(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
}

func TestScopeRunPanicRunsAllCleanups(t *testing.T) {
	var log []string
	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recover = %v, want boom", r)
		}
		if want := []string{"rollback", "unlock"}; !slices.Equal(log, want) {
			t.Fatalf("log = %v, want %v", log, want)
		}
	}()
	rt.ScopeRun(func(s *rt.Scope) int {
		rt.ScopeDefer(s, func(struct{}) struct{} { log = append(log, "unlock"); return struct{}{} })
		rt.ScopeOnErr(s, func(struct{}) struct{} { log = append(log, "rollback"); return struct{}{} })
		panic("boom")
	})
}

func TestScopeCleanupPanicStillRunsEarlier(t *testing.T) {
	var log []string
	defer func() {
		if r := recover(); r != "cleanup" {
			t.Fatalf("recover = %v, want cleanup", r)
		}
		if want := []string{"first"}; !slices.Equal(log, want) {
			t.Fatalf("log = %v, want %v", log, want)
		}
	}()
	rt.ScopeRun(func(s *rt.Scope) int {
		rt.ScopeDefer(s, func(struct{}) struct{} { log = append(log, "first"); return struct{}{} })
		rt.ScopeDefer(s, func(struct{}) struct{} { panic("cleanup") })
		return 1
	})
}

func TestScopeRegisterAfterClosePanics(t *testing.T) {
	var leaked *rt.Scope
	rt.ScopeRun(func(s *rt.Scope) int { leaked = s; return 0 })
	defer func() {
		if recover() == nil {
			t.Fatal("registering on a closed scope must panic")
		}
	}()
	rt.ScopeDefer(leaked, func(struct{}) struct{} { return struct{}{} })
}
