package pattern

import (
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// typeKey identifies a nominal type: the declaring package's import
// path ("" for the builtin scope and for the package being compiled,
// matching types.Con.Qual) plus the declared name.
type typeKey struct {
	qual string
	name string
}

// keyOf returns the key of a nominal type, and false when t is not a
// *types.Con (a bound Meta is chased first).
func keyOf(t types.Type) (typeKey, bool) {
	if c, ok := types.Chase(t).(*types.Con); ok {
		return typeKey{c.Qual, c.Name}, true
	}
	return typeKey{}, false
}

// VariantKind selects how a signature member renders in a
// counterexample pattern (docs/spec.md section 4.1).
type VariantKind int

const (
	// VCtor is an ordinary named constructor: `C a1 a2`.
	VCtor VariantKind = iota + 1
	// VLiteral is a literal member of a small enum domain:
	// `true`, `false`, `()`.
	VLiteral
	// VNil is the empty-list constructor, rendered `[]`.
	VNil
	// VCons is the list cons constructor, rendered `[a, ..rest]`.
	VCons
	// VTuple is the tuple constructor, rendered `(a1, a2)`.
	VTuple
	// VRecord is the single constructor of a record type,
	// rendered `{ name = p1, ... }`.
	VRecord
)

// Variant is one member of a type's constructor signature — an ADT
// variant, a record's single constructor, a literal of a finite enum
// domain (Bool, Unit), or a structural constructor (tuple, cons).
type Variant struct {
	// Name is the constructor's written name ("Some") or, for
	// VLiteral members, the literal's display text ("true", "()").
	Name string
	// Kind selects the rendering form.
	Kind VariantKind
	// Fields are the payload slots in declaration order; the
	// variant's arity is len(Fields).
	Fields []Field
}

// Field is one payload slot of a Variant.
type Field struct {
	// Name is the field name; "" for positional payload.
	Name string
	// Label is the `~name` label when the slot is a labeled
	// parameter (docs/spec.md section 2.6); "" otherwise.
	Label string
	// Type is the field's type, over the declaring type's Params
	// until instantiated by signature.
	Type types.Type
	// Sym is the resolved field symbol (KindField); nil for
	// unnamed positional payload.
	Sym *resolve.Symbol
}

// TypeInfo describes one nominal type's constructor signature: the
// full set of constructors a value of the type can have, in
// declaration order (the order counterexamples enumerate).
type TypeInfo struct {
	// Qual is the declaring package's import path; "" for the
	// builtin scope or the package being compiled.
	Qual string
	// Name is the declared type name.
	Name string
	// Params are the declared type parameters (`type Option a`
	// binds a); nil for monomorphic types.
	Params []*types.Var
	// Variants are the constructors in declaration order. Records
	// have exactly one VRecord variant; an empty list declares an
	// uninhabited type.
	Variants []*Variant
	// Alias is non-nil when the declaration is a type alias: the
	// signature is the aliased type's. Types defined by an alias
	// carry no Variants.
	Alias types.Type
	// Opaque marks an abstract declaration: the type is inhabited
	// but its constructors are not visible, so its domain cannot be
	// enumerated — signature returns nil and only a wildcard covers
	// it. An empty sum type is the opposite case: Variants empty
	// without Opaque enumerates nothing and is vacuously covered.
	Opaque bool
}

// Env is the constructor universe pattern checking draws on: which
// constructors exist for each nominal type and what payload they
// carry. It covers the builtin types (Bool, Unit, Option, Result,
// List) plus every TypeDecl it is given; types declared in the file
// shadow builtin names of the same qualifier, matching types.Con.
type Env struct {
	types map[typeKey]*TypeInfo
}

// NewEnv returns an Env holding the builtin signatures plus every
// *check.TypeDecl among decls.
func NewEnv(decls []check.Decl) *Env {
	e := &Env{types: map[typeKey]*TypeInfo{}}
	for _, d := range decls {
		e.AddDecl(d)
	}
	return e
}

// AddDecl registers d when it is a *check.TypeDecl; other
// declarations are ignored. AddDecl is the incremental form of
// NewEnv for callers that walk declarations one at a time.
func (e *Env) AddDecl(d check.Decl) {
	td, ok := d.(*check.TypeDecl)
	if !ok {
		return
	}
	e.AddDeclType(td)
}

// AddDeclType registers one typed type declaration.
func (e *Env) AddDeclType(td *check.TypeDecl) {
	key := typeKey{qual: td.Sym.Package, name: td.Sym.Name}
	ti := &TypeInfo{
		Qual:   key.qual,
		Name:   key.name,
		Params: td.TypeParams,
	}
	switch body := td.Body.(type) {
	case *check.SumType:
		for _, v := range body.Variants {
			vv := &Variant{Name: v.Sym.Name, Kind: VCtor}
			for _, f := range v.Fields {
				vv.Fields = append(vv.Fields, Field{
					Name:  f.Name,
					Label: f.Label,
					Type:  f.Type,
					Sym:   f.Sym,
				})
			}
			ti.Variants = append(ti.Variants, vv)
		}
	case *check.RecordType:
		v := &Variant{Kind: VRecord}
		for _, f := range body.Fields {
			v.Fields = append(v.Fields, Field{
				Name:  f.Name,
				Label: f.Label,
				Type:  f.Type,
				Sym:   f.Sym,
			})
		}
		ti.Variants = []*Variant{v}
	case *check.AliasType:
		ti.Alias = body.Type
	case *check.AbstractType:
		// Opaque: no constructors are visible, the signature
		// stays open (infinite).
		ti.Opaque = true
	default:
		panic("unreachable")
	}
	e.types[key] = ti
}

// AddType registers ti — the form for constructor sets that do not
// come from a *check.TypeDecl, such as ADTs imported from .gomi
// interface files.
func (e *Env) AddType(ti TypeInfo) {
	cp := ti
	e.types[typeKey{cp.Qual, cp.Name}] = &cp
}

// signature is the constructor signature of one type, instantiated
// at the use site: field types have the type's arguments substituted
// for the declaration's parameters. A nil signature means the type's
// domain cannot be enumerated (infinite or opaque): only a wildcard
// arm can cover it.
type signature struct {
	ctors []*Variant
}

// signature returns the instantiated constructor signature of t, or
// nil when t has no enumerable signature.
func (e *Env) signature(t types.Type) *signature {
	return e.sigOf(types.Chase(t), 0)
}

// sigOf is signature with an alias-expansion depth guard.
func (e *Env) sigOf(t types.Type, depth int) *signature {
	if depth > 16 {
		return nil // cyclic alias; give up
	}
	switch t := t.(type) {
	case *types.Con:
		key := typeKey{t.Qual, t.Name}
		if ti, ok := e.types[key]; ok {
			return ti.instantiate(e, t, depth)
		}
		if sig := builtinSignature(key, t); sig != nil {
			return sig
		}
		return nil
	case *types.Tuple:
		v := &Variant{Kind: VTuple}
		for _, el := range t.Elems {
			v.Fields = append(v.Fields, Field{Type: el})
		}
		return &signature{ctors: []*Variant{v}}
	case *types.Var, *types.Meta, *types.Func, *types.Go:
		return nil
	default:
		panic("unreachable")
	}
}

// instantiate returns the type's signature with the declaration
// parameters replaced by the applied arguments of t. Aliases defer
// to the aliased type's signature.
func (ti *TypeInfo) instantiate(e *Env, t *types.Con, depth int) *signature {
	subst := types.Subst{}
	for i, p := range ti.Params {
		if i < len(t.Args) {
			subst[p.ID] = t.Args[i]
		}
	}
	if ti.Alias != nil {
		return e.sigOf(subst.Apply(ti.Alias), depth+1)
	}
	if ti.Opaque {
		return nil
	}
	sig := &signature{ctors: make([]*Variant, len(ti.Variants))}
	for i, v := range ti.Variants {
		cv := &Variant{Name: v.Name, Kind: v.Kind, Fields: make([]Field, len(v.Fields))}
		for j, f := range v.Fields {
			cv.Fields[j] = Field{
				Name:  f.Name,
				Label: f.Label,
				Type:  subst.Apply(f.Type),
				Sym:   f.Sym,
			}
		}
		sig.ctors[i] = cv
	}
	return sig
}

// builtinSignature returns the constructor signature of a builtin
// type, or nil when key does not name one. Only reached when no
// declared type shadows the builtin name.
func builtinSignature(key typeKey, t *types.Con) *signature {
	if key.qual != "" {
		return nil
	}
	switch key.name {
	case "Bool":
		return &signature{ctors: []*Variant{
			{Name: "true", Kind: VLiteral},
			{Name: "false", Kind: VLiteral},
		}}
	case "Unit":
		return &signature{ctors: []*Variant{
			{Name: "()", Kind: VLiteral},
		}}
	case "Option":
		var a types.Type = &types.Var{}
		if len(t.Args) == 1 {
			a = t.Args[0]
		}
		return &signature{ctors: []*Variant{
			{Name: "Some", Kind: VCtor, Fields: []Field{{Type: a}}},
			{Name: "None", Kind: VCtor},
		}}
	case "Result":
		var a, e types.Type = &types.Var{}, &types.Var{}
		if len(t.Args) == 2 {
			a, e = t.Args[0], t.Args[1]
		}
		return &signature{ctors: []*Variant{
			{Name: "Ok", Kind: VCtor, Fields: []Field{{Type: a}}},
			{Name: "Err", Kind: VCtor, Fields: []Field{{Type: e}}},
		}}
	case "List":
		var a types.Type = &types.Var{}
		if len(t.Args) == 1 {
			a = t.Args[0]
		}
		return &signature{ctors: []*Variant{
			{Kind: VNil},
			{Kind: VCons, Fields: []Field{{Type: a}, {Type: t}}},
		}}
	default:
		return nil
	}
}
