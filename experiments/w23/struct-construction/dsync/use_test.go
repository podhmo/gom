package dsync

import (
	"sync"
	"sync/atomic"
	"testing"
)

// The generated NewServer path: a zero-value *http.Server whose
// unexported sync internals are intact (never copied).
func TestNewServerIsUsable(t *testing.T) {
	s := NewServer(struct{}{})
	if s == nil {
		t.Fatal("nil server")
	}
	s.Addr = ":0"
}

// The generated OnceRun drives (*sync.Once).Do through the Go.func0
// adapter: the body must run exactly once across two calls.
func TestOnceRunsExactlyOnce(t *testing.T) {
	var o sync.Once
	var calls int64
	f := func(struct{}) struct{} {
		atomic.AddInt64(&calls, 1)
		return struct{}{}
	}
	OnceRun(&o, f)
	OnceRun(&o, f)
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

// WaitGroup Add/Done through the boundary.
func TestWaitGroupRoundTrip(t *testing.T) {
	var w sync.WaitGroup
	WaitAdd(&w, 2)
	WaitDone(&w)
	WaitDone(&w)
	w.Wait() // returns without hanging
}
