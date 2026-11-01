package pattern

import (
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// cell is one column of a pattern-matrix row: a constructor-pattern
// head applied to sub-cells, or a wildcard. A cell is identified for
// specialization by key; "" marks a wildcard.
type cell struct {
	typ   types.Type // the type of the value this cell matches
	key   string     // signature key of the head constructor; "" = wildcard
	disp  string     // display atom: literal text or constructor name
	kind  VariantKind
	args  []*cell  // sub-patterns (arity)
	names []string // VRecord: field names parallel to args; nil otherwise
}

// wild returns a wildcard cell of type t.
func wild(t types.Type) *cell { return &cell{typ: t} }

// ctorKey is the signature key of constructor name on the type with
// key tk. Keys are only ever compared inside one column, whose type
// is fixed, so the type qualifier is redundant — it is kept to make
// accidental cross-type matches impossible.
func ctorKey(tk typeKey, name string) string { return "c:" + tk.qual + "/" + tk.name + "/" + name }

// litKey is the signature key of a literal member.
func litKey(norm string) string { return "l:" + norm }

// unknownKey is the signature key of a constructor whose variant set
// the Env does not know (an unregistered imported ADT, or a type
// that has no declared signature). The pattern still only covers
// itself: it contributes a key that belongs to no signature.
func unknownKey(t types.Type, name string) string {
	if tk, ok := keyOf(t); ok {
		return "u:" + tk.qual + "/" + tk.name + "/" + name
	}
	return "u:?/" + name
}

// conv turns typed patterns into matrix cells. Or-patterns and
// as-patterns never appear in cells: or-pattern alternatives expand
// into separate cells and as-bindings drop to their sub-pattern.
type conv struct {
	env *Env
}

// alts returns the alternative cells p expands to — a list of one
// for or-free patterns.
func (c conv) alts(p check.Pat) []*cell {
	switch p := p.(type) {
	case *check.WildPat, *check.VarPat:
		return []*cell{wild(p.Type())}
	case *check.AsPat:
		// An as-binding matches whatever its sub-pattern matches;
		// the bound name does not affect coverage.
		return c.alts(p.Pat)
	case *check.LitPat:
		return []*cell{litCell(p)}
	case *check.CtorPat:
		return c.ctorAlts(p)
	case *check.TuplePat:
		per := make([][]*cell, len(p.Elems))
		for i, e := range p.Elems {
			per[i] = c.alts(e)
		}
		var out []*cell
		for _, args := range product(per) {
			out = append(out, &cell{
				typ:  p.Type(),
				key:  tupleKey(len(p.Elems)),
				kind: VTuple,
				args: args,
			})
		}
		return out
	case *check.RecordPat:
		return c.recordAlts(p)
	case *check.ListPat:
		return c.listAlts(p)
	case *check.OrPat:
		var out []*cell
		for _, a := range p.Alts {
			out = append(out, c.alts(a)...)
		}
		return out
	default:
		panic("unreachable")
	}
}

// tupleKey is the signature key of the n-ary tuple constructor.
func tupleKey(n int) string { return "t:" + strconv.Itoa(n) }

// litCell converts a literal pattern to its cell.
func litCell(p *check.LitPat) *cell {
	if p.Kind == syntax.LitUnit {
		return &cell{typ: p.Type(), key: litKey("()"), disp: "()", kind: VLiteral}
	}
	norm := normalizeLit(p.Kind, p.Text)
	return &cell{typ: p.Type(), key: litKey(norm), disp: p.Text, kind: VLiteral}
}

// normalizeLit canonicalizes a literal's source text so that
// different spellings of the same value share one signature key.
// Spellings it cannot parse are kept verbatim: distinct keys merely
// behave like distinct (never-completing) constructors, which is
// conservative for infinite domains.
func normalizeLit(k syntax.LitKind, text string) string {
	switch k {
	case syntax.LitInt:
		if i, ok := new(big.Int).SetString(strings.ReplaceAll(text, "_", ""), 0); ok {
			return i.String()
		}
	case syntax.LitFloat:
		if f, err := strconv.ParseFloat(text, 64); err == nil {
			return strconv.FormatFloat(f, 'g', -1, 64)
		}
	case syntax.LitStr, syntax.LitChar:
		if s, err := strconv.Unquote(text); err == nil {
			return s
		}
	case syntax.LitBool, syntax.LitUnit:
		// Canonical spellings already.
	}
	return text
}

// ctorAlts converts a constructor pattern `C a1 .. an` or
// `C { f = p, .. }` to its alternative cells.
func (c conv) ctorAlts(p *check.CtorPat) []*cell {
	t := p.Type()
	sig := c.env.signature(t)
	var v *Variant
	if sig != nil && p.Ctor != nil {
		v = sig.findCtor(p.Ctor.Name)
	}
	if v == nil {
		// Unknown constructor: the type has no enumerable
		// signature or names no variant of it. The cell still
		// covers exactly this constructor shape — its key is
		// absent from every signature, so it never expands into
		// coverage of other constructors.
		per := make([][]*cell, 0, len(p.Args)+len(p.Fields))
		if p.Fields != nil {
			fs := append([]check.FieldPat(nil), p.Fields...)
			sort.SliceStable(fs, func(i, j int) bool {
				ii, jj := fs[i].Index, fs[j].Index
				if ii >= 0 && jj >= 0 && ii != jj {
					return ii < jj
				}
				return fs[i].Name < fs[j].Name
			})
			for _, f := range fs {
				per = append(per, c.alts(f.Pat))
			}
		} else {
			for _, a := range p.Args {
				per = append(per, c.alts(a.Pat))
			}
		}
		name := ""
		if p.Ctor != nil {
			name = p.Ctor.Name
		}
		var out []*cell
		for _, args := range product(per) {
			out = append(out, &cell{
				typ:  t,
				key:  unknownKey(t, name),
				disp: name,
				kind: VCtor,
				args: args,
			})
		}
		return out
	}
	// Known variant: align the written arguments to the declared
	// field positions.
	per := make([][]*cell, len(v.Fields))
	for i := range per {
		per[i] = []*cell{wild(v.Fields[i].Type)}
	}
	if p.Fields != nil {
		for _, f := range p.Fields {
			idx := fieldIndex(v, f)
			if idx >= 0 {
				per[idx] = c.alts(f.Pat)
			}
		}
	} else {
		for i, a := range p.Args {
			idx := argIndex(v, i, a)
			if idx >= 0 {
				per[idx] = c.alts(a.Pat)
			}
		}
	}
	var out []*cell
	for _, args := range product(per) {
		tk, _ := keyOf(t)
		out = append(out, &cell{
			typ:   t,
			key:   variantKey(v, tk),
			disp:  v.Name,
			kind:  v.Kind,
			args:  args,
			names: fieldNames(v),
		})
	}
	return out
}

// recordAlts converts a record pattern `{ f = p, .. }` to its
// alternative cells. A record is a single-constructor type; fields
// the pattern omits are wildcards.
func (c conv) recordAlts(p *check.RecordPat) []*cell {
	t := p.Type()
	v := c.env.recordOf(t)
	if v == nil {
		// Unknown record type: keep the written fields in a stable
		// order. Alignment across rows can only drift when the type
		// is unregistered — see docs/decisions for the boundary.
		fs := append([]check.FieldPat(nil), p.Fields...)
		sort.SliceStable(fs, func(i, j int) bool {
			ii, jj := fs[i].Index, fs[j].Index
			if ii >= 0 && jj >= 0 && ii != jj {
				return ii < jj
			}
			return fs[i].Name < fs[j].Name
		})
		per := make([][]*cell, len(fs))
		names := make([]string, len(fs))
		for i, f := range fs {
			per[i] = c.alts(f.Pat)
			names[i] = f.Name
		}
		var out []*cell
		for _, args := range product(per) {
			out = append(out, &cell{
				typ:   t,
				key:   unknownKey(t, "{}"),
				kind:  VRecord,
				args:  args,
				names: names,
			})
		}
		return out
	}
	per := make([][]*cell, len(v.Fields))
	for i := range per {
		per[i] = []*cell{wild(v.Fields[i].Type)}
	}
	for _, f := range p.Fields {
		idx := fieldIndex(v, f)
		if idx >= 0 {
			per[idx] = c.alts(f.Pat)
		}
	}
	var out []*cell
	for _, args := range product(per) {
		tk, _ := keyOf(t)
		out = append(out, &cell{
			typ:   t,
			key:   variantKey(v, tk),
			kind:  VRecord,
			args:  args,
			names: fieldNames(v),
		})
	}
	return out
}

// listAlts converts a list pattern to cons/nil constructor cells.
// `[p1, .., pn]` is `Cons(p1, .. Cons(pn, Nil))`; a `..rest` tail
// replaces the innermost Nil.
func (c conv) listAlts(p *check.ListPat) []*cell {
	t := p.Type()
	tails := []*cell{listCell(t, true)}
	if p.Rest != nil {
		tails = c.alts(p.Rest)
	}
	for i := len(p.Elems) - 1; i >= 0; i-- {
		heads := c.alts(p.Elems[i])
		var next []*cell
		for _, h := range heads {
			for _, tl := range tails {
				next = append(next, &cell{
					typ:  t,
					key:  listConsKey,
					disp: "Cons",
					kind: VCons,
					args: []*cell{h, tl},
				})
			}
		}
		tails = next
	}
	return tails
}

const (
	listNilKey  = "c:/List/Nil"
	listConsKey = "c:/List/Cons"
)

// listCell builds the cell of a list constructor applied to args.
func listCell(t types.Type, nilCons bool) *cell {
	if nilCons {
		return &cell{typ: t, key: listNilKey, disp: "[]", kind: VNil}
	}
	return &cell{typ: t, key: listConsKey, disp: "Cons", kind: VCons}
}

// variantKey returns the signature key of variant v on the type with
// key tk. List constructors keep fixed keys so list patterns meet
// the builtin List signature.
func variantKey(v *Variant, tk typeKey) string {
	switch v.Kind {
	case VNil:
		return listNilKey
	case VCons:
		return listConsKey
	case VLiteral:
		return litKey(v.Name)
	case VTuple:
		return tupleKey(len(v.Fields))
	case VRecord:
		return "r:" + tk.qual + "/" + tk.name
	default:
		return ctorKey(tk, v.Name)
	}
}

// findCtor returns the signature member named name, or nil. Literal
// members match too, so a constructor-pattern spelling of `true`
// still resolves against Bool's signature.
func (s *signature) findCtor(name string) *Variant {
	if s == nil {
		return nil
	}
	for _, v := range s.ctors {
		switch v.Kind {
		case VCtor, VLiteral:
			if v.Name == name {
				return v
			}
		}
	}
	return nil
}

// recordOf returns the single VRecord variant of a record type, or
// nil when t is not a record the Env knows.
func (e *Env) recordOf(t types.Type) *Variant {
	sig := e.signature(t)
	if sig == nil {
		return nil
	}
	for _, v := range sig.ctors {
		if v.Kind == VRecord {
			return v
		}
	}
	return nil
}

// fieldNames returns the per-arg field names for record rendering.
func fieldNames(v *Variant) []string {
	if v.Kind != VRecord {
		return nil
	}
	names := make([]string, len(v.Fields))
	for i, f := range v.Fields {
		names[i] = f.Name
	}
	return names
}

// fieldIndex locates the written field f inside variant v: by its
// resolved position when known, else by field symbol, else by name.
// It returns -1 when the field does not belong to v.
func fieldIndex(v *Variant, f check.FieldPat) int {
	if f.Index >= 0 && f.Index < len(v.Fields) {
		return f.Index
	}
	for i, vf := range v.Fields {
		if f.Sym != nil && vf.Sym != nil && f.Sym == vf.Sym {
			return i
		}
	}
	for i, vf := range v.Fields {
		if vf.Name == f.Name {
			return i
		}
	}
	return -1
}

// argIndex locates the field a positional or labeled constructor
// argument fills. A labeled argument `~name: p` names the field by
// its declared label or field name; a positional argument fills the
// field at its own index.
func argIndex(v *Variant, i int, a check.CtorArg) int {
	if a.Label != "" {
		for j, f := range v.Fields {
			if f.Label == a.Label {
				return j
			}
		}
		for j, f := range v.Fields {
			if f.Name == a.Label {
				return j
			}
		}
		return -1
	}
	if i < len(v.Fields) {
		return i
	}
	return -1
}

// product returns the cartesian product of per-position
// alternatives: every combination of one cell per position, as a
// list of argument vectors.
func product(per [][]*cell) [][]*cell {
	out := [][]*cell{{}}
	for _, alts := range per {
		var next [][]*cell
		for _, prefix := range out {
			for _, a := range alts {
				nv := make([]*cell, 0, len(prefix)+1)
				nv = append(nv, prefix...)
				nv = append(nv, a)
				next = append(next, nv)
			}
		}
		out = next
	}
	return out
}

// boundVars collects the variables a pattern binds, for the
// or-pattern binding check: every VarPat symbol and every AsPat
// name, mapped by name to its type.
func boundVars(p check.Pat, out map[string]types.Type) {
	switch p := p.(type) {
	case *check.WildPat:
		// binds nothing
	case *check.VarPat:
		if p.Sym != nil {
			out[p.Sym.Name] = p.Type()
		}
	case *check.AsPat:
		if p.Sym != nil {
			out[p.Sym.Name] = p.Type()
		}
		boundVars(p.Pat, out)
	case *check.LitPat:
	case *check.CtorPat:
		for _, a := range p.Args {
			boundVars(a.Pat, out)
		}
		for _, f := range p.Fields {
			boundVars(f.Pat, out)
		}
	case *check.TuplePat:
		for _, e := range p.Elems {
			boundVars(e, out)
		}
	case *check.RecordPat:
		for _, f := range p.Fields {
			boundVars(f.Pat, out)
		}
	case *check.ListPat:
		for _, e := range p.Elems {
			boundVars(e, out)
		}
		if p.Rest != nil {
			boundVars(p.Rest, out)
		}
	case *check.OrPat:
		for _, a := range p.Alts {
			boundVars(a, out)
		}
	default:
		panic("unreachable")
	}
}
