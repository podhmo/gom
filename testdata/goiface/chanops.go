package goiface

import "time"

// Channel helpers exercising the channel type mapping of spec
// section 8.5 and the implicit direction conversion of section
// 10.2 at Go-call boundaries.
func SendOn(ch chan<- string, v string)      { ch <- v }
func RecvOn(ch <-chan string) (string, bool) { v, ok := <-ch; return v, ok }
func TakeChan(ch chan string)                {}

// Element types nested in channels follow the ordinary mapping:
// `[]int64` is `Go.Slice Int`, `time.Duration` an opaque named Go
// type, and `error` is `GoError` — the channel itself carries the
// value through untouched.
func SendSlice(ch chan<- []int64, v []int64) { ch <- v }
func TakeDurChan(ch chan time.Duration)      {}
func TakeErrChan(ch chan error)              {}

// NilSig returns a nil `<-chan struct{}` — nil channels cross the
// boundary as nil; in a select a nil channel is simply never ready.
func NilSig() <-chan struct{} { return nil }

// Signal channels — `chan struct{}`/`<-chan struct{}` are the shape
// of `ctx.Done()`; struct{} maps to Gom's Unit.
func Signal() chan struct{}          { return make(chan struct{}) }
func Done() <-chan struct{}          { return make(chan struct{}) }
func CloseSignal(ch chan<- struct{}) { close(ch) }
func NotifyDone(done <-chan struct{}, out chan<- string, v string) {
	<-done
	out <- v
}
