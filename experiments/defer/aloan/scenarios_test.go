package main

import (
	"testing"

	"github.com/podhmo/gom/experiments/defer/fake"
)

func TestCopy(t *testing.T) {
	for _, c := range []struct{ dst, data, want string }{
		{"b", "x", "open:a open:b read:a write:b close:b close:a"},
		{"missing", "x", "open:a open-fail:missing close:a"},
		{"failclose", "x", "open:a open:failclose read:a write:failclose close:failclose close:a"},
	} {
		l := fake.NewLog()
		copyFile(l, "a", c.dst, c.data)
		if got := l.String(); got != c.want {
			t.Errorf("%s: got %q want %q", c.dst, got, c.want)
		}
	}
}

func TestGuarded(t *testing.T) {
	for need, want := range map[bool]string{true: "lock work unlock", false: "work"} {
		l := fake.NewLog()
		guarded(l, fake.NewMutex(l), need)
		if got := l.String(); got != want {
			t.Errorf("need=%v: got %q want %q", need, got, want)
		}
	}
}

// Documents the defect: the lock is never released on panic.
func TestPanicLeaksLock(t *testing.T) {
	l := fake.NewLog()
	func() {
		defer func() { _ = recover() }()
		boom(l, fake.NewMutex(l))
	}()
	if got, want := l.String(), "lock boom"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
