package iface

import (
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/types"
)

// MemberKind distinguishes the three things an interface file can
// declare (docs/spec.md section 7.1): function signatures, constant
// signatures and type declarations.
type MemberKind int

const (
	// MemberFn is a `fn` signature — its declared arity is part of
	// the contract.
	MemberFn MemberKind = iota
	// MemberLet is a `let` constant signature.
	MemberLet
	// MemberType is a `type` declaration.
	MemberType
)

// Member is one name in a package's public surface. For MemberFn and
// MemberLet the Type is the full signature type; a MemberFn also
// records its declared arity (the parameter-group count, `()`
// included — spec section 7.2 makes it interface-relevant). For
// MemberType, TParams is the declared type-parameter count and Body
// the typed declaration body — an AbstractType for a gomi `type T`,
// or the concrete SumType/RecordType/AliasType.
type Member struct {
	Kind    MemberKind
	Name    string
	Sp      diag.Span // decl span — the diagnostics site
	Type    types.Type
	Arity   int
	TParams int
	Body    check.TypeBody
}

// Package is the public surface of one compiled file — or the merged
// surface of a whole package — as the implementation↔interface
// comparison sees it (docs/design.md section 3, internal/iface).
type Package struct {
	Name    string    // the package clause name ("" when absent)
	PkgSp   diag.Span // span of the package clause
	Members map[string]*Member
}

// Surface extracts the public surface of a typed file: the typed
// declarations that a .gomi can require (fn signatures, let
// constants, type declarations), keyed by member name. Imports,
// test blocks and @test declarations do not enter the surface —
// they are not part of the public API (spec sections 7 and 11;
// design section 5 keeps tests out of the interface hash too).
//
// A `let` binding that check generalized into a function-shaped
// declaration contributes a MemberFn when it carries declared
// parameters (`let f = fn x -> x`) and a MemberLet whose type is the
// niladic signature's return — the value's type — when it is a
// generalized value (`let x = <value>`, the Thunk shape).
func Surface(f *check.File) *Package {
	p := &Package{Members: map[string]*Member{}}
	if f == nil {
		return p
	}
	p.Name = f.Package
	p.PkgSp = f.PkgSp
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *check.FnDecl:
			if d.Sym != nil && d.Sym.TestOnly {
				continue
			}
			name := ""
			if d.Sym != nil {
				name = d.Sym.Name
			}
			if d.Thunk {
				// `let x = <value>` — the interface member is the
				// value, so its type is the thunk signature's
				// return, not the niladic `() -> T`.
				p.add(&Member{Kind: MemberLet, Name: name, Sp: d.Sp,
					Type: d.Sig.Ret})
				continue
			}
			p.add(&Member{Kind: MemberFn, Name: name, Sp: d.Sp,
				Type: d.Sig, Arity: len(d.Sig.Params)})
		case *check.LetDecl:
			for _, m := range letMembers(d) {
				p.add(m)
			}
		case *check.TypeDecl:
			name := ""
			params := len(d.TypeParams)
			if d.Sym != nil {
				name = d.Sym.Name
			}
			p.add(&Member{Kind: MemberType, Name: name, Sp: d.Sp,
				TParams: params, Body: d.Body})
		case *check.TestDecl:
			// Not part of the public surface.
		default:
			panic("unreachable")
		}
	}
	return p
}

// add registers m under its name; the first member of a name wins.
// Duplicate names inside one file are resolve's E0202 — an errored
// file never reaches Surface. Duplicates across files of one package
// are E0202 via resolve.Files (the package unit); merging remains
// first-wins as a defensive default for callers outside the unit
// pipeline.
func (p *Package) add(m *Member) {
	if m.Name == "" {
		return
	}
	if _, dup := p.Members[m.Name]; dup {
		return
	}
	p.Members[m.Name] = m
}

// Merge folds src's members into p (first wins, like add) and returns
// p for chaining. Names and package clauses are not reconciled —
// callers group by package name first.
func (p *Package) Merge(src *Package) *Package {
	for _, m := range src.Members {
		p.add(m)
	}
	return p
}

// letMembers expands a typed top-level `let` into one MemberLet per
// bound name — `let (a, b) = e` exposes two constants. Wildcards and
// non-binding positions contribute nothing. A `let` bound by @test is
// test-only and contributes nothing either.
func letMembers(d *check.LetDecl) []*Member {
	var out []*Member
	var walk func(p check.Pat)
	walk = func(p check.Pat) {
		switch p := p.(type) {
		case nil:
		case *check.WildPat, *check.LitPat:
		case *check.VarPat:
			if p.Sym == nil || p.Sym.TestOnly {
				return
			}
			out = append(out, &Member{Kind: MemberLet,
				Name: p.Sym.Name, Sp: p.Sp, Type: p.Typ})
		case *check.CtorPat:
			for _, a := range p.Args {
				walk(a.Pat)
			}
			for i := range p.Fields {
				walk(p.Fields[i].Pat)
			}
		case *check.TuplePat:
			for _, e := range p.Elems {
				walk(e)
			}
		case *check.RecordPat:
			for i := range p.Fields {
				walk(p.Fields[i].Pat)
			}
		case *check.ListPat:
			for _, e := range p.Elems {
				walk(e)
			}
			walk(p.Rest)
		case *check.OrPat:
			if len(p.Alts) > 0 {
				walk(p.Alts[0]) // alternatives bind the same names
			}
		case *check.AsPat:
			walk(p.Pat)
			if p.Sym != nil && !p.Sym.TestOnly {
				out = append(out, &Member{Kind: MemberLet,
					Name: p.Sym.Name, Sp: p.Sp, Type: p.Typ})
			}
		default:
			panic("unreachable")
		}
	}
	walk(d.Pat)
	return out
}
