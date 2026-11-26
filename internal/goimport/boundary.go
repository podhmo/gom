package goimport

import (
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	gt "github.com/podhmo/gom/internal/types"
)

// InterfaceAssignable uses the same Go type identities as signature loading.
// It is deliberately an assignment relation, never type unification.
func (l *Loader) InterfaceAssignable(from, to gt.Type) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	dst := l.boundaryType(to)
	if dst == nil {
		return false
	}
	if _, ok := dst.Underlying().(*types.Interface); !ok {
		return false
	}
	src := l.boundaryType(from)
	return src != nil && types.AssignableTo(src, dst)
}

// InterfaceMethods lists the methods a Go interface requires — the
// check.InterfaceMethodSource oracle behind a Gom type's @method
// conformance test (decisions/2026-11-21-go-iface-impl).
func (l *Loader) InterfaceMethods(t gt.Type) ([]check.InterfaceMethod, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var dst types.Type
	if gt.Equal(gt.Chase(t), gt.GoError) {
		// The universe error interface has no *types.Go handle in
		// l.named — spell it directly.
		dst = types.Universe.Lookup("error").Type()
	} else {
		dst = l.boundaryType(t)
	}
	if dst == nil {
		return nil, false
	}
	iface, ok := dst.Underlying().(*types.Interface)
	if !ok {
		return nil, false
	}
	var out []check.InterfaceMethod
	for i := 0; i < iface.NumMethods(); i++ {
		m := iface.Method(i)
		sig, ok := m.Type().(*types.Signature)
		if !ok {
			return nil, false
		}
		ck, gs, _ := l.mapSignature(sig)
		fn, ok := ck.Type.(*gt.Func)
		if !ok {
			return nil, false
		}
		out = append(out, check.InterfaceMethod{
			Name:       m.Name(),
			Sig:        fn,
			GoSig:      goMethodSig(sig),
			RawParams:  gs.Params,
			RawResults: gs.ResTypes,
		})
	}
	return out, true
}

// goMethodSig renders an interface method's raw go/types signature
// the way reflect spells a bound method — `func(int64) string`,
// `func() (int64, error)` — without parameter names and without the
// receiver. This is the spelling eval's implements check compares
// against reflect's `MethodByName().Type().String()`.
func goMethodSig(sig *types.Signature) string {
	var b strings.Builder
	b.WriteString("func(")
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		if i > 0 {
			b.WriteString(", ")
		}
		pt := params.At(i).Type()
		if sig.Variadic() && i == params.Len()-1 {
			if sl, ok := pt.(*types.Slice); ok {
				b.WriteString("...")
				pt = sl.Elem()
			}
		}
		b.WriteString(goTypeSpell(pt))
	}
	b.WriteString(")")
	results := sig.Results()
	// reflect never prints result names — a single named result is
	// `func() bool`, and `func() (int, error)` is only for 2+.
	switch {
	case results.Len() == 0:
	case results.Len() == 1:
		b.WriteString(" ")
		b.WriteString(goTypeSpell(results.At(0).Type()))
	default:
		b.WriteString(" (")
		for i := 0; i < results.Len(); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(goTypeSpell(results.At(i).Type()))
		}
		b.WriteString(")")
	}
	return b.String()
}

// goTypeSpell renders a go/types type the way reflect's
// Type.String does: aliases are unaliased (`[]byte` → `[]uint8`),
// the empty interface is `interface {}`, and a named type is
// `pkgbase.Name` regardless of the import path's last element.
func goTypeSpell(t types.Type) string {
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Basic:
		// Precompiled signatures can spell the universe aliases as
		// basics; reflect always uses the underlying name.
		switch t.Name() {
		case "byte":
			return "uint8"
		case "rune":
			return "int32"
		}
		return t.Name()
	case *types.Named:
		name := t.Obj().Name()
		if t.Obj().Pkg() != nil {
			name = t.Obj().Pkg().Name() + "." + name
		}
		if args := t.TypeArgs(); args != nil && args.Len() > 0 {
			var as []string
			for i := 0; i < args.Len(); i++ {
				as = append(as, goTypeSpell(args.At(i)))
			}
			name += "[" + strings.Join(as, ", ") + "]"
		}
		return name
	case *types.Interface:
		if t.Empty() {
			return "interface {}"
		}
		return t.String()
	case *types.Slice:
		return "[]" + goTypeSpell(t.Elem())
	case *types.Array:
		return "[" + strconv.FormatInt(t.Len(), 10) + "]" + goTypeSpell(t.Elem())
	case *types.Pointer:
		return "*" + goTypeSpell(t.Elem())
	case *types.Map:
		return "map[" + goTypeSpell(t.Key()) + "]" + goTypeSpell(t.Elem())
	case *types.Chan:
		switch t.Dir() {
		case types.SendOnly:
			return "chan<- " + goTypeSpell(t.Elem())
		case types.RecvOnly:
			return "<-chan " + goTypeSpell(t.Elem())
		default:
			return "chan " + goTypeSpell(t.Elem())
		}
	case *types.Signature:
		return goMethodSig(t)
	case *types.TypeParam:
		return t.Obj().Name()
	default:
		return t.String()
	}
}

// GoInterface reports whether t's emitted Go representation is an
// interface — the coarse oracle behind the type-test scrutinee
// check (InterfaceMethods additionally requires mappable method
// signatures, which `any` also passes).
func (l *Loader) GoInterface(t gt.Type) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	bt := l.boundaryType(t)
	if bt == nil {
		return false
	}
	_, ok := bt.Underlying().(*types.Interface)
	return ok
}

// boundaryType reconstructs the Go spelling of a Gom type for the
// boundary relations — assignability, comparability, convertibility.
func (l *Loader) boundaryType(t gt.Type) types.Type {
	return l.boundaryTypeOf(t, false)
}

// boundaryTypeOf is boundaryType; nested marks positions inside a
// composite where Go requires identical types — a `List` element's
// faithful spelling there is the named `list.List[A]`, not its
// underlying `[]A` (`func(list.List[int64])` and `func([]int64)` are
// different types), while at the top level the underlying slice
// answers the boundary relations identically.
func (l *Loader) boundaryTypeOf(t gt.Type, nested bool) types.Type {
	switch t := gt.Chase(t).(type) {
	case *gt.Go:
		if len(t.Args) != 0 {
			return nil
		}
		if named, ok := l.named[t.Ref]; ok {
			return named
		}
		return nil
	case *gt.Con:
		if t.Qual == "" && t.Name == "Unit" {
			return types.NewStruct(nil, nil)
		}
		if t.Qual == "" && t.Name == "Ptr" && len(t.Args) == 1 {
			if el := l.boundaryTypeOf(t.Args[0], true); el != nil {
				return types.NewPointer(el)
			}
			return nil
		}
		if t.Qual == "" && t.Name == "List" && len(t.Args) == 1 {
			// The Gom List renders as the named slice list.List[A]
			// — at the top level its underlying slice answers the
			// boundary relations identically, but inside an
			// identity position (FuncN arg, chan element, ...) the
			// name is load-bearing and the named spelling is the
			// faithful one.
			if el := l.boundaryTypeOf(t.Args[0], true); el != nil {
				if nested {
					return l.listType(el)
				}
				return types.NewSlice(el)
			}
			return nil
		}
		if t.Qual == "" && len(t.Args) == 1 {
			direction, channel := types.SendRecv, true
			switch t.Name {
			case "Chan":
			case "RecvChan":
				direction = types.RecvOnly
			case "SendChan":
				direction = types.SendOnly
			default:
				channel = false
			}
			if channel {
				if el := l.boundaryTypeOf(t.Args[0], true); el != nil {
					return types.NewChan(direction, el)
				}
				return nil
			}
		}
		if gt.Equal(t, gt.GoAny) {
			return types.Unalias(types.Universe.Lookup("any").Type())
		}
		if t.Qual == "Go" && t.Name == "Slice" && len(t.Args) == 1 {
			if el := l.boundaryTypeOf(t.Args[0], true); el != nil {
				return types.NewSlice(el)
			}
			return nil
		}
		if t.Qual == "Go" && t.Name == "Map" && len(t.Args) == 2 {
			k, v := l.boundaryTypeOf(t.Args[0], true), l.boundaryTypeOf(t.Args[1], true)
			if k != nil && v != nil {
				return types.NewMap(k, v)
			}
			return nil
		}
		if gt.Equal(t, gt.GoError) {
			return types.Universe.Lookup("error").Type()
		}
		if t.Qual == "Go" && strings.HasPrefix(t.Name, "Func") {
			// A `Go.FuncN` is a genuine Go signature type — the
			// boundary relations need it spelled as one so a
			// FuncN value satisfies interface parameters and
			// `Go.conv f : Go.Any` proves. The args carry the n
			// parameters followed by the flat results.
			n, err := strconv.Atoi(strings.TrimPrefix(t.Name, "Func"))
			if err != nil || n < 0 || len(t.Args) < n {
				return nil
			}
			var params, results []*types.Var
			for i, a := range t.Args {
				bt := l.boundaryTypeOf(a, true)
				if bt == nil {
					return nil
				}
				if i < n {
					params = append(params, types.NewVar(token.NoPos, nil, "", bt))
				} else {
					results = append(results, types.NewVar(token.NoPos, nil, "", bt))
				}
			}
			return types.NewSignatureType(nil, nil, nil,
				types.NewTuple(params...), types.NewTuple(results...), false)
		}
		if gt.Equal(t, gt.Unit) {
			// Unit renders as `struct{}` — needed inside channel
			// types so `Chan Unit` relates to `chan struct{}`.
			return types.NewStruct(nil, nil)
		}
		for _, b := range []struct {
			gom    gt.Type
			goKind types.BasicKind
		}{
			{gt.Int, types.Int64}, {gt.Int32, types.Int32}, {gt.Int64, types.Int64},
			{gt.Uint, types.Uint64}, {gt.Uint64, types.Uint64},
			{gt.Float, types.Float64},
			{gt.Bool, types.Bool}, {gt.String, types.String}, {gt.Char, types.Int32}, {gt.Byte, types.Uint8},
			{gt.GoInt, types.Int}, {gt.GoUint, types.Uint},
		} {
			if gt.Equal(t, b.gom) {
				return types.Typ[b.goKind]
			}
		}
		return nil
	case *gt.Func, *gt.Var, *gt.Tuple, *gt.Meta:
		return nil
	default:
		panic("unreachable")
	}
}

// listType spells `list.List[el]` — the named slice the Gom `List`
// renders as — for nested boundary positions where its name is
// load-bearing (a `[]A` there would claim a different Go type).
func (l *Loader) listType(el types.Type) types.Type {
	if l.listNamed == nil {
		pkg := types.NewPackage("github.com/podhmo/gom/stdlib/list", "list")
		tp := types.NewTypeParam(types.NewTypeName(0, pkg, "A", nil),
			types.Universe.Lookup("any").Type())
		named := types.NewNamed(types.NewTypeName(0, pkg, "List", nil), nil, nil)
		named.SetTypeParams([]*types.TypeParam{tp})
		named.SetUnderlying(types.NewSlice(tp))
		l.listNamed = named
	}
	t, err := types.Instantiate(nil, l.listNamed, []types.Type{el}, true)
	if err != nil {
		return nil
	}
	return t
}

// FuncNOfNamed gives the `Go.FuncN` a method-less named func type's
// `*types.Go` handle reads as at a signature boundary —
// `context.CancelFunc` → `Go.Func0`. The handle itself is the
// nested spelling; this is the equivalence the checker consults at
// value positions (`Go.fnN`, unify of a handle against FuncN).
func (l *Loader) FuncNOfNamed(t gt.Type) (gt.Type, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.funcNOfNamed(t)
}

// funcNOfNamed is FuncNOfNamed without the lock — for call sites
// already running inside a member load (BindExtern's view pairing).
func (l *Loader) funcNOfNamed(t gt.Type) (gt.Type, bool) {
	g, ok := gt.Chase(t).(*gt.Go)
	if !ok || len(g.Args) != 0 {
		return nil, false
	}
	named, ok := l.named[g.Ref]
	if !ok || named.NumMethods() != 0 {
		return nil, false
	}
	sig, ok := named.Underlying().(*types.Signature)
	if !ok {
		return nil, false
	}
	ft, reason := l.mapFuncType(sig)
	if reason != "" {
		return nil, false
	}
	return ft, true
}

// GoComparable reports whether t's Go representation satisfies the
// `comparable` constraint — the check.ComparableSource entry the
// generic-call constraint check consults for *types.Go handles.
// The named type behind the handle answers with types.Comparable.
func (l *Loader) GoComparable(t gt.Type) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	bt := l.boundaryType(t)
	return bt != nil && types.Comparable(bt)
}

// GoConvertible is the check.ConvertSource answer for `Go.conv e : T`
// (spec section 8.4): a pair both sides map to a boundary type obeys
// types.ConvertibleTo. A Gom-only shape on either side — a declared
// ADT or record, a tuple, a type variable — can never be a Go
// conversion and decides against; an unmappable Go view (a generic
// instantiation, a function value) is unproven, not disproven.
func (l *Loader) GoConvertible(from, to gt.Type) (bool, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	src, dst := l.boundaryType(from), l.boundaryType(to)
	if src != nil && dst != nil {
		return true, types.ConvertibleTo(src, dst)
	}
	if gomOnly(from) || gomOnly(to) {
		return true, false
	}
	return false, false
}

// gomOnly reports whether t is a shape with no Go-boundary reading —
// for these a nil boundaryType is a proof the conversion cannot
// exist, not mere ignorance. The mappable constructors and the Go
// views the loader cannot map (instantiated named types, function
// values, unsolved metas) are unproven instead.
func gomOnly(t gt.Type) bool {
	switch t := gt.Chase(t).(type) {
	case *gt.Tuple, *gt.Var:
		return true
	case *gt.Con:
		if t.Qual == "Go" {
			return false
		}
		switch t.Name {
		case "Unit", "Ptr", "List", "Chan", "RecvChan", "SendChan",
			"Int", "Int32", "Int64", "Uint", "Uint64", "Float", "Bool", "String", "Char", "Byte", "GoError":
			return false
		}
		return true
	case *gt.Go, *gt.Func, *gt.Meta:
		return false
	default:
		panic("unreachable")
	}
}

// FieldOf resolves readable Go struct fields, including promoted selectors.
// Unsupported field types remain discoverable and produce E0501 on use only.
func (l *Loader) FieldOf(recv gt.Type, name string) (*resolve.Symbol, check.Signature, gt.Type, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	rt := l.boundaryType(recv)
	if rt == nil {
		return nil, check.Signature{}, nil, false
	}
	obj, _, _ := types.LookupFieldOrMethod(rt, true, nil, name)
	field, ok := obj.(*types.Var)
	if !ok || !field.IsField() || !field.Exported() {
		return nil, check.Signature{}, nil, false
	}
	sym := &resolve.Symbol{Name: name, Kind: resolve.KindField, IsGo: true}
	if field.Pkg() != nil {
		sym.Package = field.Pkg().Path()
	}
	mapped, reason := l.mapBoundaryType(field.Type(), false)
	raw, rawReason := l.mapType(field.Type(), true, true)
	if reason == "" {
		reason = rawReason
	}
	if reason == "" && !viewsCompatible(mapped, raw) {
		reason = divergentReason(field.Type())
	}
	if reason != "" {
		sym.Hidden = reason
	}
	sig := check.Signature{Type: mapped}
	l.syms[sym] = &member{obj: field, ck: sig}
	return sym, sig, raw, true
}
