package rt

// Task is the Go representation of Gom's `Task a`: a handle to a
// goroutine whose result — or panic — is delivered by Await
// (docs/spec.md section 10, item 9).
type Task[T any] struct {
	done  chan struct{}
	value T
	panik any
}

// SpawnTask runs f on a new goroutine and returns its handle.
// The standard library's Task.spawn forwards here.
func SpawnTask[T any](f func() T) Task[T] {
	t := Task[T]{done: make(chan struct{})}
	go func() {
		defer close(t.done)
		defer func() { t.panik = recover() }()
		t.value = f()
	}()
	return t
}

// Await blocks until the task finishes and returns its value. A panic
// inside the task re-panics on the awaiting goroutine.
func (t Task[T]) Await() T {
	<-t.done
	if t.panik != nil {
		panic(t.panik)
	}
	return t.value
}
