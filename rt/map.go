package rt

// Map primitives backing the `Go.Map` member table — the runtime
// half of spec section 8.5's mutable-map surface
// (decisions/2026-11-05-go-map-ops). A `Go.Map k v` is Go's
// `map[K]V` itself: nil maps read, delete and length fine and panic
// on set, matching Go rather than adding Gom-only preconditions.

// MapMake returns a fresh, non-nil map — `Go.Map.make ()`.
func MapMake[K comparable, V any]() map[K]V { return make(map[K]V) }

// MapGet is `Go.Map.get m k`: the (v, ok) pair generated code wraps
// into Some/None — ok=false on a missing key or a nil map.
func MapGet[K comparable, V any](m map[K]V, k K) (V, bool) {
	v, ok := m[k]
	return v, ok
}

// MapSet is `Go.Map.set m k v`. Setting a nil map panics like Go's
// own `m[k] = v` — `Go.Map.make` never hands out nil, so the panic
// marks a nil that arrived through the boundary.
func MapSet[K comparable, V any](m map[K]V, k K, v V) { m[k] = v }

// MapDelete is `Go.Map.delete m k` — a no-op on a missing key or a
// nil map, like `delete`.
func MapDelete[K comparable, V any](m map[K]V, k K) { delete(m, k) }

// MapLen is `Go.Map.length m` — 0 on a nil map, like `len`.
func MapLen[K comparable, V any](m map[K]V) int64 { return int64(len(m)) }

// MapToList snapshots m into a slice of (key, value) pairs —
// `Go.Map.to_list m`, which generated code converts into the Gom
// `List` view. Iteration order is unspecified, like Go's range.
func MapToList[K comparable, V any](m map[K]V) []Tuple2[K, V] {
	out := make([]Tuple2[K, V], 0, len(m))
	for k, v := range m {
		out = append(out, Tuple2[K, V]{V0: k, V1: v})
	}
	return out
}
