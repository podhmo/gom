package goiface

// Named array shapes for `Go.conv` List → named-Go-type fixtures:
// a plain array value casts directly (`A(xs)` copies the prefix
// itself), while a pointer to an array must route through the
// copy-then-cast composite so the emitted pointer cannot share the
// List's backing.
type Int64Arr [3]int64

type Int64ArrPtr *[3]int64
