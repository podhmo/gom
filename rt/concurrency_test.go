package rt_test

import (
	"testing"

	"github.com/podhmo/gom/rt"
)

// The tests below pin the runtime half of spec section 10 — the
// helpers the builtin `Chan` and `Task` modules forward to. Every
// case is deterministic: channels are buffered or the test
// goroutine is the sole sender/receiver, so no scheduling order is
// ever observable.

// TestChanOps: buffered sends do not block, receives arrive in send
// order, and receiving past close reports ok == false — the (v, ok)
// pair generated code wraps as `None`.
func TestChanOps(t *testing.T) {
	ch := rt.MakeChan[int64](2)
	rt.ChanSend(ch, 1)
	rt.ChanSend(ch, 2)
	rt.ChanClose(ch)
	if v, ok := rt.ChanRecv(ch); !ok || v != 1 {
		t.Errorf("first recv = (%d, %v), want (1, true)", v, ok)
	}
	if v, ok := rt.ChanRecv(ch); !ok || v != 2 {
		t.Errorf("second recv = (%d, %v), want (2, true)", v, ok)
	}
	if _, ok := rt.ChanRecv(ch); ok {
		t.Error("recv past close = ok, want closed")
	}
}

// TestChanDirectionViews: recvOnly and sendOnly are views of the
// same channel — a send through one is a receive on the other.
func TestChanDirectionViews(t *testing.T) {
	ch := rt.MakeChan[int64](1)
	rt.ChanSend(rt.ChanSendOnly(ch), 7)
	if v, ok := rt.ChanRecv(rt.ChanRecvOnly(ch)); !ok || v != 7 {
		t.Errorf("recv = (%d, %v), want (7, true)", v, ok)
	}
}

// TestMakeChanNegativePanics: a negative capacity is a Go-level
// precondition violation — `make(chan T, -1)` panics and so does
// `Chan.make (-1)`, with no extra Gom-only check in between.
func TestMakeChanNegativePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MakeChan(-1) returned without panicking")
		}
	}()
	_ = rt.MakeChan[int64](-1)
}

// TestSpawnAwaitTask: the result written by the spawned goroutine
// reaches the awaiter through the shared box — including a second
// copy of the handle awaiting again.
func TestSpawnAwaitTask(t *testing.T) {
	task := rt.SpawnTask(func(_ struct{}) int64 { return 49 })
	if got := rt.AwaitTask(task); got != 49 {
		t.Errorf("AwaitTask = %d, want 49", got)
	}
	if got := task.Await(); got != 49 {
		t.Errorf("second Await = %d, want 49 (shared result cell)", got)
	}
}

// TestAwaitTaskRethrowsPanic: a panic inside the task is recovered
// by the spawned goroutine and re-panicked by Await (spec section
// 10, item 9).
func TestAwaitTaskRethrowsPanic(t *testing.T) {
	task := rt.SpawnTask(func(_ struct{}) int64 { panic("boom") })
	defer func() {
		if r := recover(); r != "boom" {
			t.Errorf("recovered %v, want the task's panic \"boom\"", r)
		}
	}()
	_ = rt.AwaitTask(task)
	t.Error("AwaitTask returned without re-panicking")
}
