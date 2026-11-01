package pattern

import (
	"github.com/podhmo/gom/internal/types"
)

// This file implements the pattern-matrix algorithm of Luc Maranget,
// "Warnings for pattern matching" (JFP 2007) — the usefulness test
// U(P, q) — plus the missing-pattern enumeration the diagnostics of
// spec section 4 and the testdata/spec expectations require:
// witnesses enumerate depth-first over the type's constructor
// signature in declaration order, so `Neg (Num _)` precedes
// `Neg (Add _ _)` precedes `Let _ _ _` (see check.go for the
// diagnostic use).
//
// Both recursions split on Σ₁, the set of constructors appearing at
// the root of the matrix's first column (U_rec / I of the paper):
// when Σ₁ is a complete signature they specialize on each
// constructor; when it is incomplete they take only the default
// matrix D(P). The split is what makes the recursion terminate on
// recursive types: specializing a wildcard column under Cons would
// re-derive an identical matrix state forever (the Cons field is the
// same List type), and the incomplete-Σ₁ default branch consumes the
// column instead. The termination measure — the number of
// constructor nodes in the matrix and the pattern vector (wildcards
// not counted), then the column count — decreases lexicographically,
// matching the Agda mechanization of the same algorithm
// (wasabi315/coverage-checking). The all-wildcard-row cut in each
// function is a shortcut for the common covered case, not the
// termination guarantee.

// matrix is a set of pattern vectors sharing a column count and the
// per-column types of tys.
type matrix struct {
	rows [][]*cell
}

// specialize computes S(c, M) (Maranget 2007 §3): the sub-matrix of
// rows whose first column matches constructor key. Wildcard-headed
// rows expand to arity wildcard columns of the given field types;
// rows headed by other constructors drop out. The result's column
// types are fieldTys ++ tys[1:].
func specialize(m matrix, tys []types.Type, key string, fieldTys []types.Type) (matrix, []types.Type) {
	var rows [][]*cell
	arity := len(fieldTys)
	for _, r := range m.rows {
		h := r[0]
		switch {
		case h.key == "":
			row := make([]*cell, 0, arity+len(r)-1)
			for _, ft := range fieldTys {
				row = append(row, wild(ft))
			}
			row = append(row, r[1:]...)
			rows = append(rows, row)
		case h.key == key:
			row := make([]*cell, 0, len(h.args)+len(r)-1)
			row = append(row, h.args...)
			row = append(row, r[1:]...)
			rows = append(rows, row)
		}
	}
	return matrix{rows: rows}, append(append([]types.Type{}, fieldTys...), tys[1:]...)
}

// defaultMatrix computes D(M) (Maranget 2007 §3): the rows whose
// first column is a wildcard, with that column removed. Used when
// the column's domain cannot be enumerated — the wildcard column
// then stands for every value outside the named constructors.
func defaultMatrix(m matrix) matrix {
	var rows [][]*cell
	for _, r := range m.rows {
		if r[0].key == "" {
			rows = append(rows, r[1:])
		}
	}
	return matrix{rows: rows}
}

// useful reports whether the pattern vector q is useful with respect
// to matrix m (Maranget 2007 §3): whether some value matching q is
// matched by no row of m. A nil or empty matrix matches nothing, so
// any q is useful against it.
func (c *checker) useful(m matrix, tys []types.Type, q []*cell) bool {
	if len(m.rows) == 0 {
		return true
	}
	if allWildRow(m.rows) {
		// A row of pure wildcards matches every value of the column
		// space: nothing escapes it, so q cannot be useful. This is
		// a shortcut for the common case; the termination argument
		// is the Σ₁ split below (file header comment).
		return false
	}
	if len(q) == 0 {
		return false
	}
	if h := q[0]; h.key != "" {
		// q starts with a constructor: specialize both sides on it.
		fieldTys := make([]types.Type, len(h.args))
		for i, a := range h.args {
			fieldTys[i] = a.typ
		}
		sm, stys := specialize(m, tys, h.key, fieldTys)
		return c.useful(sm, stys, append(append([]*cell{}, h.args...), q[1:]...))
	}
	sig := c.env.signature(q[0].typ)
	if sig != nil && completeHeads(m, tys[0], sig) {
		// Σ₁ — the constructors heading column 0 — is a complete
		// signature: every head a value of this column can have is
		// named by a row, so q's wildcard is useful iff it is
		// useful under some constructor's specialization (the
		// complete case of U_rec).
		tk := mustKeyOf(q[0].typ)
		for _, v := range sig.ctors {
			ft := fieldTypes(v)
			sm, stys := specialize(m, tys, variantKey(v, tk), ft)
			w := make([]*cell, 0, len(ft)+len(q)-1)
			for _, f := range ft {
				w = append(w, wild(f))
			}
			w = append(w, q[1:]...)
			if c.useful(sm, stys, w) {
				return true
			}
		}
		return false
	}
	// Σ₁ is incomplete — or the domain cannot be enumerated at all
	// (infinite or opaque): at least one head is named by no row,
	// so a value under it is decided by the wildcard rows alone.
	// The missing-ctor specializations would only re-derive the
	// default matrix's question (and on recursive types, forever);
	// U_rec takes D(P) directly.
	return c.useful(defaultMatrix(m), tys[1:], q[1:])
}

// rootKeys returns Σ₁ for column 0 of m: the set of constructor
// keys heading a row (Maranget 2007 §3).
func rootKeys(m matrix) map[string]bool {
	keys := make(map[string]bool)
	for _, r := range m.rows {
		if k := r[0].key; k != "" {
			keys[k] = true
		}
	}
	return keys
}

// completeHeads reports whether the root keys of column 0 are a
// complete signature for the column's type t: every member of sig
// heads at least one row.
func completeHeads(m matrix, t types.Type, sig *signature) bool {
	seen := rootKeys(m)
	tk := mustKeyOf(t)
	for _, v := range sig.ctors {
		if !seen[variantKey(v, tk)] {
			return false
		}
	}
	return true
}

// allWildRow reports whether some row of m is all wildcards.
func allWildRow(rows [][]*cell) bool {
	for _, r := range rows {
		w := true
		for _, cl := range r {
			if cl.key != "" {
				w = false
				break
			}
		}
		if w {
			return true
		}
	}
	return false
}

// fieldTypes returns the field types of a variant.
func fieldTypes(v *Variant) []types.Type {
	out := make([]types.Type, len(v.Fields))
	for i, f := range v.Fields {
		out[i] = f.Type
	}
	return out
}

// mustKeyOf is keyOf for positions that always carry a nominal or
// structural type; the zero key keeps unknown domains harmless.
func mustKeyOf(t types.Type) typeKey {
	tk, _ := keyOf(t)
	return tk
}

// missing enumerates counterexample pattern vectors for matrix m:
// one representative pattern per uncovered region of the column
// space tys, in deterministic order — constructor order within a
// signature (the declaration order of spec section 4), then
// depth-first into arguments. At most limit vectors are returned;
// limit <= 0 means unbounded.
func (c *checker) missing(m matrix, tys []types.Type, limit int) [][]*cell {
	if len(tys) == 0 {
		if len(m.rows) == 0 {
			return [][]*cell{{}}
		}
		return nil
	}
	if allWildRow(m.rows) {
		// Same shortcut as in useful: an all-wild row leaves
		// nothing uncovered. Termination itself follows from the
		// Σ₁ split below (file header comment).
		return nil
	}
	sig := c.env.signature(tys[0])
	if sig == nil {
		// Infinite or opaque domain: values outside the named
		// constructors are missing exactly when the wildcard-only
		// sub-matrix has missing rows.
		sub := c.missing(defaultMatrix(m), tys[1:], limit)
		out := make([][]*cell, 0, len(sub))
		for _, w := range sub {
			out = append(out, append([]*cell{wild(tys[0])}, w...))
		}
		return out
	}
	// Enumerable domain, I of the paper: iterate the signature in
	// declaration order — the counterexample order of spec
	// section 4. A constructor heading the column (a member of
	// Σ₁) is refined by specialization; a constructor absent
	// from Σ₁ is missing exactly where the wildcard rows leave
	// the tail uncovered, so it emits one c(w̄)·w per missing
	// tail — specializing on it would only re-derive the default
	// matrix's question (forever, on recursive types). The
	// default-matrix result is computed once, lazily, and shared
	// by every absent constructor.
	tk := mustKeyOf(tys[0])
	seen := rootKeys(m)
	var out [][]*cell
	var def [][]*cell
	defDone := false
	for _, v := range sig.ctors {
		if limit > 0 && len(out) >= limit {
			break
		}
		key := variantKey(v, tk)
		if seen[key] {
			rest := 0
			if limit > 0 {
				rest = limit - len(out)
			}
			sm, stys := specialize(m, tys, key, fieldTypes(v))
			for _, w := range c.missing(sm, stys, rest) {
				args := make([]*cell, len(v.Fields))
				copy(args, w[:len(v.Fields)])
				row := make([]*cell, 0, len(tys))
				row = append(row, c.ctorCell(tys[0], v, tk, args))
				row = append(row, w[len(v.Fields):]...)
				out = append(out, row)
				if limit > 0 && len(out) >= limit {
					break
				}
			}
			continue
		}
		if !defDone {
			def = c.missing(defaultMatrix(m), tys[1:], limit)
			defDone = true
		}
		for _, w := range def {
			args := make([]*cell, len(v.Fields))
			for i, f := range v.Fields {
				args[i] = wild(f.Type)
			}
			row := make([]*cell, 0, len(tys))
			row = append(row, c.ctorCell(tys[0], v, tk, args))
			row = append(row, w...)
			out = append(out, row)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out
}

// ctorCell builds the cell of signature member v applied to args.
func (c *checker) ctorCell(t types.Type, v *Variant, tk typeKey, args []*cell) *cell {
	return &cell{
		typ:   t,
		key:   variantKey(v, tk),
		disp:  v.Name,
		kind:  v.Kind,
		args:  args,
		names: fieldNames(v),
	}
}
