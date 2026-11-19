package rt

// Mutable-cell primitives backing the builtin `Ref` module's
// members — the runtime half of the `Ref a` operation surface
// (spec section 3.1, decisions/2026-11-19-ref-members). A `Ref a`
// emits as `*T` like `Ptr a`, but a `Ref` is always initialized —
// there is no nil Ref, so get and set are plain dereferences.

// RefNew allocates a cell holding v — `Ref.new v`. Mutating through
// the result never reaches v's other referents.
//
//go:fix inline
func RefNew[T any](v T) *T { return new(v) }

// RefGet reads the cell — `Ref.get r`, a plain dereference.
//
//go:fix inline
func RefGet[T any](r *T) T { return *r }

// RefSet writes v into the cell — `Ref.set r v`.
func RefSet[T any](r *T, v T) { *r = v }
