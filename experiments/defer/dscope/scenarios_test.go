package main

import (
	"testing"

	"github.com/podhmo/gom/experiments/defer/fake"
	"github.com/podhmo/gom/experiments/defer/scope"
	"github.com/podhmo/gom/stdlib/result"
)

func errText(r result.Result[struct{}, error]) string {
	if r.IsOk() {
		return ""
	}
	_, err := r.Unwrap()
	return err.Error()
}

func TestScenarios(t *testing.T) {
	cases := []struct {
		name    string
		run     func(*fake.Log) result.Result[struct{}, error]
		wantLog string
		wantErr string
	}{
		{"copy/ok", func(l *fake.Log) result.Result[struct{}, error] { return copyFile(l, "a", "b", "x") },
			"open:a open:b read:a write:b close:b close:a", ""},
		{"copy/open-dst-fails", func(l *fake.Log) result.Result[struct{}, error] { return copyFile(l, "a", "missing", "x") },
			"open:a open-fail:missing close:a", "open missing: not found"},
		{"copy/close-error-surfaces", func(l *fake.Log) result.Result[struct{}, error] { return copyFile(l, "a", "failclose", "x") },
			"open:a open:failclose read:a write:failclose close:failclose close:a", "close failclose: flush failed"},
		{"copy/body-error-wins", func(l *fake.Log) result.Result[struct{}, error] { return copyFile(l, "a", "failclose", "bad") },
			"open:a open:failclose read:a write-fail:failclose close:failclose close:a", "write failclose: bad data"},
		{"pair/close-errors-join", func(l *fake.Log) result.Result[struct{}, error] { return writePair(l, "failclose1", "failclose2") },
			"open:failclose1 open:failclose2 write:failclose1 write:failclose2 close:failclose2 close:failclose1", "close failclose2: flush failed\nclose failclose1: flush failed"},
		{"pair/body-error-wins", func(l *fake.Log) result.Result[struct{}, error] { return writePair(l, "failclose1", "missing") },
			"open:failclose1 open-fail:missing close:failclose1", "open missing: not found"},
		{"tx/commit", func(l *fake.Log) result.Result[struct{}, error] { return transfer(l, "q1", "q2") },
			"begin exec:q1 exec:q2 commit", ""},
		{"tx/rollback", func(l *fake.Log) result.Result[struct{}, error] { return transfer(l, "q1", "bad") },
			"begin exec:q1 exec-fail rollback", "exec: bad query"},
		{"loop/per-iteration", func(l *fake.Log) result.Result[struct{}, error] { return eachFile(l, []string{"a", "b"}) },
			"open:a write:a close:a open:b write:b close:b", ""},
		{"loop/stops-at-failure", func(l *fake.Log) result.Result[struct{}, error] { return eachFile(l, []string{"a", "failclose", "c"}) },
			"open:a write:a close:a open:failclose write:failclose close:failclose", "close failclose: flush failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := fake.NewLog()
			r := c.run(l)
			if got := l.String(); got != c.wantLog {
				t.Errorf("log\n got: %s\nwant: %s", got, c.wantLog)
			}
			if got := errText(r); got != c.wantErr {
				t.Errorf("err got %q want %q", got, c.wantErr)
			}
		})
	}
}

func TestGuardedUnlockOutlivesIf(t *testing.T) {
	for need, want := range map[bool]string{true: "lock work unlock", false: "work"} {
		l := fake.NewLog()
		guarded(l, fake.NewMutex(l), need)
		if got := l.String(); got != want {
			t.Errorf("need=%v: got %q want %q", need, got, want)
		}
	}
}

func TestPanicRunsCleanup(t *testing.T) {
	l := fake.NewLog()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic to propagate")
			}
		}()
		boom(l)
	}()
	if got, want := l.String(), "boom cleanup"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestPanickingCleanupDoesNotSkipOthers(t *testing.T) {
	l := fake.NewLog()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic to propagate")
			}
		}()
		scope.Run(func(s *scope.Scope) struct{} {
			s.Defer(func() { l.Add("first") })
			s.Defer(func() { l.Add("second"); panic("cleanup") })
			return struct{}{}
		})
	}()
	if got, want := l.String(), "second first"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
