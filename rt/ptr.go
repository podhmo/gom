package rt

// Pointer primitives backing the builtin `Ptr` module's members —
// the runtime half of the `Ptr T` operation surface (spec section
// 8.3, decisions/2026-11-05-ptr-ops-surface). A `Ptr T` is Go's
// `*T`; get and set panic on nil exactly like Go's own dereference,
// and is_nil/to_option are the safe forms.

// PtrGet dereferences p — `Ptr.get p`. A nil p panics.
func PtrGet[T any](p *T) T { return *p }

// PtrSet writes v through p — `Ptr.set p v`. A nil p panics.
func PtrSet[T any](p *T, v T) { *p = v }

// PtrIsNil reports whether p is nil — `Ptr.is_nil p`.
func PtrIsNil[T any](p *T) bool { return p == nil }

// PtrNew allocates a zero T and returns its pointer — `Ptr.new ()`,
// Go's new(T): the out-pointer APIs (errors.As's target, decode
// destinations) need a non-nil pointer to a zero value.
func PtrNew[T any]() *T { return new(T) }

// PtrMake returns a pointer to a fresh copy of v — `Ptr.make v`.
// Mutating through the result never reaches v.
//
//go:fix inline
func PtrMake[T any](v T) *T { return new(v) }

// PtrNil returns the nil pointer — `Ptr.nil ()`, for Go calls whose
// optional *T parameter takes nil (tls.Dial's *tls.Config).
func PtrNil[T any]() *T { return nil }

// PtrToOption is `Ptr.to_option p`: the (v, ok) pair generated code
// wraps into Some/None — ok=false when p is nil.
func PtrToOption[T any](p *T) (T, bool) {
	if p == nil {
		var zero T
		return zero, false
	}
	return *p, true
}
