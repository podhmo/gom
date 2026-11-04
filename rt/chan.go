package rt

// Channel primitives backing the builtin `Chan` module's members —
// the runtime half of spec section 10. A `Chan a` is Go's
// bidirectional `chan T`; `RecvChan a` and `SendChan a` are its
// restricted views, and the conversions between them follow Go's
// own channel-direction assignability rules.

// MakeChan returns a buffered channel with capacity n
// (n == 0 is unbuffered) — `Chan.make n`. The Gom `Int` is int64,
// so the capacity arrives as int64 and narrows to Go's int — a
// negative value panics exactly like `make(chan T, -1)`, matching
// the spec's "channels are Go's channels" rule rather than adding
// a Gom-only precondition.
func MakeChan[T any](n int64) chan T { return make(chan T, int(n)) }

// ChanSend sends v on ch — `Chan.send ch v`.
func ChanSend[T any](ch chan<- T, v T) { ch <- v }

// ChanRecv receives from ch — `Chan.recv ch`. The (v, ok) result
// shape lets generated code wrap the closed case as `None`
// (option.FromGo).
func ChanRecv[T any](ch <-chan T) (T, bool) { v, ok := <-ch; return v, ok }

// ChanClose closes ch — `Chan.close ch`. Like Go, only a
// send-capable channel may be closed.
func ChanClose[T any](ch chan<- T) { close(ch) }

// ChanRecvOnly narrows a bidirectional channel to its receive-only
// view — `Chan.recvOnly ch`.
func ChanRecvOnly[T any](ch chan T) <-chan T { return ch }

// ChanSendOnly narrows a bidirectional channel to its send-only
// view — `Chan.sendOnly ch`.
func ChanSendOnly[T any](ch chan T) chan<- T { return ch }
