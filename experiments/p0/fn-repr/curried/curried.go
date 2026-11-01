package curried

// Hypothetical Gom module compiled in this style:
//
//   fn add3 (a: Int) (b: Int) (c: Int) -> Int = a + b + c
//   fn add2 (a: Int) (b: Int) -> Int = a + b
//   fn inc (x: Int) -> Int = x + 1
//   fn cmp_desc (a: Int) (b: Int) -> Int = b - a
//   fn transfer (from: Int) (~to: Int) (~amount: Int) -> Int = (from - to) + amount
//   fn map (f: a -> b) (xs: List a) -> List b
//   fn zip_with (f: a -> b -> c) (xs: List a) (ys: List b) -> List c
//   fn fold (f: acc -> a -> acc) (init: acc) (xs: List a) -> acc
//
// (Go slices stand in for Gom List values.)

// --- conversion helpers -------------------------------------------------
//
// The compiler could emit one shared instantiation of each of these per
// concrete signature instead of repeating anonymous wrappers.

// Curry2 converts a flat 2-argument function to the curried value form.
func Curry2[A, B, R any](f func(A, B) R) func(A) func(B) R {
	return func(a A) func(B) R {
		return func(b B) R {
			return f(a, b)
		}
	}
}

// Curry3 converts a flat 3-argument function to the curried value form.
func Curry3[A, B, C, R any](f func(A, B, C) R) func(A) func(B) func(C) R {
	return func(a A) func(B) func(C) R {
		return func(b B) func(C) R {
			return func(c C) R {
				return f(a, b, c)
			}
		}
	}
}

// Uncurry2 converts a curried value back to a flat function. This is the
// adapter needed when a Gom function value flows into a Go API (or into a
// specialized Gom API such as FoldFlat) that takes a multi-argument func.
func Uncurry2[A, B, R any](f func(A) func(B) R) func(A, B) R {
	return func(a A, b B) R {
		return f(a)(b)
	}
}

// Uncurry3 converts a 3-argument curried value to a flat function.
func Uncurry3[A, B, C, R any](f func(A) func(B) func(C) R) func(A, B, C) R {
	return func(a A, b B, c C) R {
		return f(a)(b)(c)
	}
}

// --- fn add3 --------------------------------------------------------------

// Add3 is the direct-call form used for saturated calls `add3 x y z`.
func Add3(a, b, c int) int { return a + b + c }

// Add3Val is the value form of `add3`: where Gom code mentions the
// function without applying it (`let f = add3`, `pair add3 id`), the
// compiler emits a reference to one shared wrapper per (function,
// instantiation).
var Add3Val = Curry3(Add3)

// Add3_1 is the generated closure for the partial application `add3 a`
// (one argument bound, two remain).
func Add3_1(a int) func(int) func(int) int {
	return func(b int) func(int) int {
		return func(c int) int {
			return Add3(a, b, c)
		}
	}
}

// --- fn add2 --------------------------------------------------------------

func Add2(a, b int) int { return a + b }

var Add2Val = Curry2(Add2)

// --- fn inc ---------------------------------------------------------------

func Inc(x int) int { return x + 1 }

// IncVal: a one-argument function value is func(int) int in both styles —
// arity 1 is where the two representations coincide.
var IncVal func(int) int = Inc

// --- fn cmp_desc ----------------------------------------------------------

// CmpDesc : Int -> Int -> Int, the example used for slices.SortFunc.
func CmpDesc(a, b int) int { return b - a }

var CmpDescVal = Curry2(CmpDesc)

// --- fn mk_add ------------------------------------------------------------

// MkAdd models `fn mk_add (a: Int) -> (Int -> Int) = fn b -> a + b`. In the
// curried style the declared form and the value form coincide — the one
// case where this style needs no wrapper at all.
func MkAdd(a int) func(int) int {
	return func(b int) int { return a + b }
}

// --- fn transfer ----------------------------------------------------------

// Transfer erases the labels; the Go signature keeps declaration order
// (from, to, amount).
func Transfer(from, to, amount int) int { return (from - to) + amount }

// TransferVal is `transfer` used as a value.
var TransferVal = Curry3(Transfer)

// Transfer_From_Amount is the generated closure for
// `transfer alice ~amount: m`. Only `~to` remains, so the remaining arity
// is 1 and this reordering closure has the same shape in both styles.
func Transfer_From_Amount(from, amount int) func(int) int {
	return func(to int) int {
		return Transfer(from, to, amount)
	}
}

// Transfer_Amount is the generated closure for `transfer ~amount: m`:
// two arguments remain (`from -> ~to:Int -> Int`), so the result is nested
// here and flat in the arity style.
func Transfer_Amount(amount int) func(int) func(int) int {
	return func(from int) func(int) int {
		return func(to int) int {
			return Transfer(from, to, amount)
		}
	}
}

// --- fn map ---------------------------------------------------------------

func Map[A, B any](f func(A) B, xs []A) []B {
	out := make([]B, len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

// MapVal is `map` used as a value: instantiate, then curry.
func MapVal[A, B any]() func(func(A) B) func([]A) []B {
	return Curry2(Map[A, B])
}

// Map_F is `map f` (one argument remains — identical shape in both styles).
func Map_F[A, B any](f func(A) B) func([]A) []B {
	return func(xs []A) []B {
		return Map(f, xs)
	}
}

// --- fn zip_with ----------------------------------------------------------

// ZipWith takes a 2-argument function f; in this style f is
// func(A) func(B) C, so the per-element call f(x)(y) allocates an inner
// closure on every iteration unless the call chain is inlined.
func ZipWith[A, B, C any](f func(A) func(B) C, xs []A, ys []B) []C {
	n := min(len(xs), len(ys))
	out := make([]C, n)
	for i := 0; i < n; i++ {
		out[i] = f(xs[i])(ys[i])
	}
	return out
}

// ZipWith_F is `zip_with f` (two arguments remain).
func ZipWith_F[A, B, C any](f func(A) func(B) C) func([]A) func([]B) []C {
	return func(xs []A) func([]B) []C {
		return func(ys []B) []C {
			return ZipWith(f, xs, ys)
		}
	}
}

// --- fn fold --------------------------------------------------------------

// Fold is the signature as Gom would declare it: the folder is curried,
// so each element costs f(acc)(x) — an inner closure allocation plus two
// calls.
func Fold[A, Acc any](f func(Acc) func(A) Acc, init Acc, xs []A) Acc {
	acc := init
	for _, x := range xs {
		acc = f(acc)(x)
	}
	return acc
}

// FoldFlat is the docs/design.md 4.4 "specialization escape hatch": a
// second version of the same function taking a flat func directly, so hot
// loops can avoid the curried call chain.
func FoldFlat[A, Acc any](f func(Acc, A) Acc, init Acc, xs []A) Acc {
	acc := init
	for _, x := range xs {
		acc = f(acc, x)
	}
	return acc
}
