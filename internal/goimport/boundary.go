package goimport

import (
	"go/types"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	gt "github.com/podhmo/gom/internal/types"
)

// InterfaceAssignable uses the same Go type identities as signature loading.
// It is deliberately an assignment relation, never type unification.
func (l *Loader) InterfaceAssignable(from, to gt.Type) bool {
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

func (l *Loader) boundaryType(t gt.Type) types.Type {
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
		if t.Qual == "" && t.Name == "Ptr" && len(t.Args) == 1 {
			if el := l.boundaryType(t.Args[0]); el != nil {
				return types.NewPointer(el)
			}
			return nil
		}
		if t.Qual == "" && t.Name == "List" && len(t.Args) == 1 {
			// The Gom List renders as the named slice list.List[A]
			// — for the boundary relations (assignability,
			// comparability, convertibility) its underlying slice
			// is the faithful Go spelling.
			if el := l.boundaryType(t.Args[0]); el != nil {
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
				if el := l.boundaryType(t.Args[0]); el != nil {
					return types.NewChan(direction, el)
				}
				return nil
			}
		}
		if gt.Equal(t, gt.GoAny) {
			return types.Unalias(types.Universe.Lookup("any").Type())
		}
		if t.Qual == "Go" && t.Name == "Slice" && len(t.Args) == 1 {
			if el := l.boundaryType(t.Args[0]); el != nil {
				return types.NewSlice(el)
			}
			return nil
		}
		if t.Qual == "Go" && t.Name == "Map" && len(t.Args) == 2 {
			k, v := l.boundaryType(t.Args[0]), l.boundaryType(t.Args[1])
			if k != nil && v != nil {
				return types.NewMap(k, v)
			}
			return nil
		}
		if gt.Equal(t, gt.GoError) {
			return types.Universe.Lookup("error").Type()
		}
		for _, b := range []struct {
			gom    gt.Type
			goKind types.BasicKind
		}{
			{gt.Int, types.Int64}, {gt.Int32, types.Int32}, {gt.Float, types.Float64},
			{gt.Bool, types.Bool}, {gt.String, types.String}, {gt.Char, types.Int32}, {gt.GoInt, types.Int},
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

// GoComparable reports whether t's Go representation satisfies the
// `comparable` constraint — the check.ComparableSource entry the
// generic-call constraint check consults for *types.Go handles.
// The named type behind the handle answers with types.Comparable.
func (l *Loader) GoComparable(t gt.Type) bool {
	bt := l.boundaryType(t)
	return bt != nil && types.Comparable(bt)
}

// FieldOf resolves readable Go struct fields, including promoted selectors.
// Unsupported field types remain discoverable and produce E0501 on use only.
func (l *Loader) FieldOf(recv gt.Type, name string) (*resolve.Symbol, check.Signature, gt.Type, bool) {
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
	raw, rawReason := l.mapType(field.Type(), true)
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
