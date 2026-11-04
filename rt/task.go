package rt

// Task is the Go representation of Gom's `Task a`: a handle to a
// goroutine whose result — or panic — is delivered by Await
// (docs/spec.md section 10, item 9). The result lives in a shared
// cell: Task copies (the SpawnTask return value versus the local the
// goroutine writes through) must observe the same outcome.
type Task[T any] struct {
	done chan struct{}
	box  *taskBox[T]
}

// taskBox is the shared result cell of a Task: the spawned goroutine
// writes the value or the recovered panic into it before closing
// done, so closing the channel publishes the write to every copy of
// the handle.
type taskBox[T any] struct {
	value T
	panik any
}

// SpawnTask runs f on a new goroutine and returns its handle.
// The standard library's Task.spawn forwards here. f takes a
// struct{} parameter because the Gom `Unit -> a` it forwards renders
// as func(struct{}) T — the unit parameter is part of the language's
// calling convention.
func SpawnTask[T any](f func(struct{}) T) Task[T] {
	t := Task[T]{done: make(chan struct{}), box: &taskBox[T]{}}
	go func() {
		defer close(t.done)
		defer func() { t.box.panik = recover() }()
		t.box.value = f(struct{}{})
	}()
	return t
}

// AwaitTask blocks until the task finishes and returns its value —
// the standard library's `Task.await`. A panic inside the task
// re-panics on the awaiting goroutine.
func AwaitTask[T any](t Task[T]) T { return t.Await() }

// Await blocks until the task finishes and returns its value. A panic
// inside the task re-panics on the awaiting goroutine.
func (t Task[T]) Await() T {
	<-t.done
	if t.box.panik != nil {
		panic(t.box.panik)
	}
	return t.box.value
}
