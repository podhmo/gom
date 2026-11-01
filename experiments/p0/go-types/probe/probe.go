// Package probe explores go/types to answer the questions of TODO.md
// task P0-5 ("Go の interface と named type"): how Go's interfaces,
// named types, signatures, generics and nil-ability appear through
// go/types, and what that means for Gom's type representation.
//
// Loading: the production compiler will use go/packages (design.md
// 2.5), but go/importer from the standard library is enough to make
// every point about go/types itself. The only hard requirement both
// share is that ONE importer/load session is shared for the whole
// compilation — see identity_test.go.
package probe

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
)

// Loader wraps a single go/types importer. All packages imported through
// one Loader share type identity: the same nominal Go type is the same
// *types.Named object. Creating a second Loader breaks that invariant.
type Loader struct {
	imp types.Importer
}

// NewLoader returns a Loader backed by importer.Default() (the gc
// importer, which reads export data — the same identity model that
// go/packages has).
func NewLoader() *Loader {
	return &Loader{imp: importer.Default()}
}

// Import loads a package by path, e.g. Import("io").
func (l *Loader) Import(path string) (*types.Package, error) {
	return l.imp.Import(path)
}

// Lookup is Import + Scope().Lookup in one call.
func (l *Loader) Lookup(pkgPath, name string) (types.Object, error) {
	pkg, err := l.Import(pkgPath)
	if err != nil {
		return nil, err
	}
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return nil, fmt.Errorf("%s.%s not found", pkgPath, name)
	}
	return obj, nil
}

// CheckSource type-checks a single Go source file through the same
// importer, so its types share identity with imported packages.
// Returns the checked package plus its types.Info for expression
// probing.
func (l *Loader) CheckSource(pkgPath, filename, src string) (*types.Package, *types.Info, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, nil, err
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: l.imp}
	pkg, err := conf.Check(pkgPath, fset, []*ast.File{f}, info)
	if err != nil {
		return nil, nil, err
	}
	return pkg, info, nil
}

// ErrorType returns the universe `error` type (a *types.Named).
func ErrorType() types.Type {
	return types.Universe.Lookup("error").Type()
}

// IsError reports whether t is exactly the predeclared `error` type.
func IsError(t types.Type) bool {
	return types.Identical(t, ErrorType())
}

// IsAny reports whether t is the empty interface (any / interface{}),
// looking through aliases.
func IsAny(t types.Type) bool {
	u := types.Unalias(t)
	iface, ok := u.Underlying().(*types.Interface)
	if !ok {
		// u.Underlying() on a *types.Interface returns itself, so this
		// branch only fires for non-interface types.
		return false
	}
	return iface.NumMethods() == 0 && iface.NumEmbeddeds() == 0
}

// NilableByAssignability reports whether the untyped nil is assignable
// to t — the authoritative "can this type be nil" check.
func NilableByAssignability(t types.Type) bool {
	return types.AssignableTo(types.Typ[types.UntypedNil], t)
}

// NilableByShape reports the same fact by walking the underlying type
// with a type switch. Kept next to NilableByAssignability to prove the
// manual enumeration and the official rule agree.
func NilableByShape(t types.Type) bool {
	t = types.Unalias(t)
	if _, ok := t.(*types.TypeParam); ok {
		// A type parameter's Underlying() is its constraint
		// interface — check the parameter itself first, or every
		// interface-constrained T would look nilable.
		return false
	}
	u := t.Underlying()
	switch u := u.(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan,
		*types.Signature, *types.Interface:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer || u.Kind() == types.UntypedNil
	default:
		return false
	}
}

// ConstraintClass classifies a type-parameter constraint for the
// go-interop boundary (spec 8.5: only `any` / `comparable` are callable
// in v1). The two "liberal" classes extend the strict boundary to
// method-set interfaces, which is a possible decision amendment.
type ConstraintClass string

const (
	ConstraintAny        ConstraintClass = "any"                    // empty interface
	ConstraintComparable ConstraintClass = "comparable"             // IsComparable
	ConstraintMethodSet  ConstraintClass = "method-set interface"   // IsMethodSet but non-empty (e.g. Stringer-ish)
	ConstraintTypeSet    ConstraintClass = "type-set (unsupported)" // union / ~T / core-type constraint
)

// ClassifyConstraint inspects a *types.TypeParam constraint.
func ClassifyConstraint(c types.Type) ConstraintClass {
	u := types.Unalias(c).Underlying()
	iface, ok := u.(*types.Interface)
	if !ok {
		return ConstraintTypeSet
	}
	switch {
	case iface.IsComparable() && iface.NumMethods() == 0 && iface.NumEmbeddeds() == 0:
		return ConstraintComparable
	case iface.IsMethodSet():
		if iface.NumMethods() == 0 && iface.NumEmbeddeds() == 0 {
			return ConstraintAny
		}
		return ConstraintMethodSet
	default:
		return ConstraintTypeSet
	}
}

// IsCallableGeneric reports whether a signature's type parameters all
// satisfy the strict spec boundary (any / comparable only).
func IsCallableGeneric(sig *types.Signature, strict bool) bool {
	tps := sig.TypeParams()
	for i := 0; i < tps.Len(); i++ {
		c := ClassifyConstraint(tps.At(i).Constraint())
		if strict {
			if c != ConstraintAny && c != ConstraintComparable {
				return false
			}
		} else if c == ConstraintTypeSet {
			return false
		}
	}
	return true
}

// SignatureFacts is a printable deconstruction of *types.Signature.
type SignatureFacts struct {
	Params    []ParamFacts
	Results   []ParamFacts
	Variadic  bool
	TypeParam []TypeParamFacts
}

// ParamFacts describes one parameter or result variable.
type ParamFacts struct {
	Name     string
	Type     string // types.TypeString
	Concrete string // fmt %T of the RAW types.Type node (alias shown as A->B)
	IsError  bool
	IsAny    bool
	Nilable  bool
}

// TypeParamFacts describes one type parameter of a generic signature.
type TypeParamFacts struct {
	Name            string
	Index           int
	Constraint      string // printed constraint
	ConstraintClass ConstraintClass
}

// DeconstructSignature breaks a *types.Signature into inspectable parts.
func DeconstructSignature(sig *types.Signature) SignatureFacts {
	var f SignatureFacts
	f.Variadic = sig.Variadic()
	for i := 0; i < sig.Params().Len(); i++ {
		f.Params = append(f.Params, varFacts(sig.Params().At(i)))
	}
	for i := 0; i < sig.Results().Len(); i++ {
		f.Results = append(f.Results, varFacts(sig.Results().At(i)))
	}
	tps := sig.TypeParams()
	for i := 0; i < tps.Len(); i++ {
		tp := tps.At(i)
		f.TypeParam = append(f.TypeParam, TypeParamFacts{
			Name:            tp.Obj().Name(),
			Index:           tp.Index(),
			Constraint:      types.TypeString(tp.Constraint(), qualifier),
			ConstraintClass: ClassifyConstraint(tp.Constraint()),
		})
	}
	return f
}

func varFacts(v *types.Var) ParamFacts {
	return ParamFacts{
		Name:     v.Name(),
		Type:     types.TypeString(v.Type(), qualifier),
		Concrete: nodeString(v.Type()),
		IsError:  IsError(v.Type()),
		IsAny:    IsAny(v.Type()),
		Nilable:  NilableByAssignability(v.Type()),
	}
}

// nodeString prints the concrete node type, e.g. "*types.Named" or
// "*types.Alias->*types.Interface" when the type is an alias (the raw
// node matters: export data delivers `any` params as *types.Alias).
func nodeString(t types.Type) string {
	if _, ok := t.(*types.Alias); ok {
		return fmt.Sprintf("%T->%T", t, types.Unalias(t))
	}
	return fmt.Sprintf("%T", t)
}

// qualifier renders a type's package as its short name, so
// io.Reader prints "io.Reader" instead of the full import path.
func qualifier(p *types.Package) string {
	if p == nil {
		return ""
	}
	return p.Name()
}

// ResultShape classifies a Go signature's result tuple for the
// spec 8.3 conversion table.
type ResultShape string

const (
	ResultUnit      ResultShape = "()"          // no results
	ResultValue     ResultShape = "T"           // single non-error value
	ResultErrorOnly ResultShape = "error"       // only error -> Result Unit GoError
	ResultValueErr  ResultShape = "(T, error)"  // -> Result T GoError
	ResultValueBool ResultShape = "(T, bool)"   // -> tuple (comma-ok candidates)
	ResultMulti     ResultShape = "(A, B, ...)" // -> tuple
)

// ClassifyResults applies the spec 8.3 rules to a result tuple.
func ClassifyResults(sig *types.Signature) ResultShape {
	r := sig.Results()
	switch r.Len() {
	case 0:
		return ResultUnit
	case 1:
		if IsError(r.At(0).Type()) {
			return ResultErrorOnly
		}
		return ResultValue
	case 2:
		if IsError(r.At(1).Type()) {
			return ResultValueErr
		}
		if isBool(r.At(1).Type()) {
			return ResultValueBool
		}
		return ResultMulti
	default:
		return ResultMulti
	}
}

func isBool(t types.Type) bool {
	b, ok := types.Unalias(t).Underlying().(*types.Basic)
	return ok && b.Kind() == types.Bool
}

// LastResultIsError is the concrete "(T, error)" detector:
// two results, second identical to universe error.
func LastResultIsError(sig *types.Signature) bool {
	r := sig.Results()
	return r.Len() == 2 && IsError(r.At(1).Type())
}
