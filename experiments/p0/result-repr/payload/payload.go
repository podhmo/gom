// Package payload provides a large value type shared by the candidate
// representations, so that construction / pass / destructuring costs
// are measured on the same payload.
package payload

// Big is a ~1 KB value type. Gom records and large ADT payloads are
// passed by value, so Result[Big, E] is the stress case for any rep
// that copies the whole union on every return.
type Big struct {
	Buf [128]int64
	N   int64
}
