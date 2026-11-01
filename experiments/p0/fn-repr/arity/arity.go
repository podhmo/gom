package arity

// Hypothetical Gom module compiled in this style — identical to the one in
// the curried package:
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

// --- generic helpers ------------------------------------------------------
//
// The compiler does not need these — it can always emit an anonymous
// closure of the remaining arity — but shared instantiations are an option
// to reduce code size.

// Apply1of3 binds the first argument of a 3-argument function.
func Apply1of3[A, B, C, R any](f func(A, B, C) R, a A) func(B, C) R {
	return func(b B, c C) R {
		return f(a, b, c)
	}
}

// Apply1of2 binds the first argument of a 2-argument function.
func Apply1of2[A, B, R any](f func(A, B) R, a A) func(B) R {
	return func(b B) R {
		return f(a, b)
	}
}

// Apply2of3 binds the first two arguments of a 3-argument function.
func Apply2of3[A, B, C, R any](f func(A, B, C) R, a A, b B) func(C) R {
	return func(c C) R {
		return f(a, b, c)
	}
}

// --- fn add3 --------------------------------------------------------------

// Add3 is both the direct-call form and the value form: a Gom value of
// type Int -> Int -> Int -> Int is a func(int, int, int) int, so `add3`
// used as a value is just Add3 — no wrapper.
func Add3(a, b, c int) int { return a + b + c }

// Add3_1 is the generated closure for the partial application `add3 a`
// (one argument bound, two remain).
func Add3_1(a int) func(int, int) int {
	return func(b, c int) int {
		return Add3(a, b, c)
	}
}

// --- fn add2 --------------------------------------------------------------

// Add2 is already the value form of Gom `add2 : Int -> Int -> Int`.
func Add2(a, b int) int { return a + b }

// --- fn inc ---------------------------------------------------------------

// Inc: arity 1, where the two representations coincide.
func Inc(x int) int { return x + 1 }

// --- fn cmp_desc ----------------------------------------------------------

// CmpDesc : Int -> Int -> Int; directly assignable to the func(int, int) int
// parameter of slices.SortFunc and similar Go APIs.
func CmpDesc(a, b int) int { return b - a }

// --- fn mk_add ------------------------------------------------------------

// MkAdd models `fn mk_add (a: Int) -> (Int -> Int) = fn b -> a + b`: a
// definition whose declared arity (1) is smaller than the arrow length of
// its type (Int -> Int -> Int). It compiles to the declared-arity form,
// because the outer body must run when the outer call happens.
func MkAdd(a int) func(int) int {
	return func(b int) int { return a + b }
}

// MkAddFlat is the canonical value form of `mk_add`. Gom type
// Int -> Int -> Int flattens to func(int, int) int in value position, so a
// wrapper is generated where `mk_add` is used as a value — the rare case
// in this style that still needs a conversion wrapper. Direct calls are
// unaffected: `mk_add x` is MkAdd(x), `mk_add x y` is MkAdd(x)(y).
func MkAddFlat(a, b int) int { return MkAdd(a)(b) }

// --- fn transfer ----------------------------------------------------------

// Transfer erases the labels; the Go signature keeps declaration order
// (from, to, amount).
func Transfer(from, to, amount int) int { return (from - to) + amount }

// Transfer_From_Amount is the generated closure for
// `transfer alice ~amount: m`. Only `~to` remains — same shape as in the
// curried style.
func Transfer_From_Amount(from, amount int) func(int) int {
	return func(to int) int {
		return Transfer(from, to, amount)
	}
}

// Transfer_Amount is the generated closure for `transfer ~amount: m`. Two
// arguments remain, so the result is a flat func(int, int) int here and a
// nested func in the curried style.
func Transfer_Amount(amount int) func(int, int) int {
	return func(from, to int) int {
		return Transfer(from, to, amount)
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

// Map_F is `map f` (one argument remains — identical shape in both styles).
func Map_F[A, B any](f func(A) B) func([]A) []B {
	return func(xs []A) []B {
		return Map(f, xs)
	}
}

// `map` used as a value needs no helper at all: the Go expression
// Map[int, string] already is the value func(func(int) string, []int) []string.

// --- fn zip_with ----------------------------------------------------------

// ZipWith takes a flat 2-argument function, so the per-element call is a
// single indirect call with no closure allocation.
func ZipWith[A, B, C any](f func(A, B) C, xs []A, ys []B) []C {
	n := min(len(xs), len(ys))
	out := make([]C, n)
	for i := 0; i < n; i++ {
		out[i] = f(xs[i], ys[i])
	}
	return out
}

// ZipWith_F is `zip_with f` (two arguments remain).
func ZipWith_F[A, B, C any](f func(A, B) C) func([]A, []B) []C {
	return func(xs []A, ys []B) []C {
		return ZipWith(f, xs, ys)
	}
}

// --- fn fold --------------------------------------------------------------

// Fold is the signature as Gom would declare it — and it is already the
// fast form, because the folder's remaining arity (2) is in its Go type.
func Fold[A, Acc any](f func(Acc, A) Acc, init Acc, xs []A) Acc {
	acc := init
	for _, x := range xs {
		acc = f(acc, x)
	}
	return acc
}
