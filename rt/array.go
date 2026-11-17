package rt

// ArrayMake returns a mutable array of n zero elements — Gom's
// `Array.make`. A negative length panics like Go's make.
func ArrayMake[A any](n int64) []A { return make([]A, n) }

// ArrayLen is len(a) — Gom's `Array.length`.
func ArrayLen[A any](a []A) int64 { return int64(len(a)) }

// ArrayGet is a[i] — Gom's `Array.get`. Out of range panics like Go.
func ArrayGet[A any](a []A, i int64) A { return a[i] }

// ArraySet is a[i] = x — Gom's `Array.set`. Out of range panics like Go.
func ArraySet[A any](a []A, i int64, x A) { a[i] = x }
