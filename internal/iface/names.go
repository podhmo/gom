package iface

import (
	"os"
	"path/filepath"

	"github.com/podhmo/gom/internal/syntax"
)

// ExportNames is the build.Options.Exports hook: it reads
// <dir>/<pkg>.gomi and returns the member names it publishes —
// the names an implementation file emits capitalized (spec section
// 5). A missing or unparsable interface file answers nil; the
// conformance pass reports the real diagnostics.
func ExportNames(dir, pkg string) map[string]bool {
	src, err := os.ReadFile(filepath.Join(dir, pkg+".gomi"))
	if err != nil {
		return nil
	}
	f, _ := syntax.ParseInterface(src, filepath.Join(dir, pkg+".gomi"))
	return PublicNames(f)
}

// PublicNames returns the member names a parsed interface file
// publishes — every `fn`, `let` and `type` name the file declares.
// It is the names-only read of a .gomi used to decide which
// implementation declarations emit capitalized Go identifiers
// (spec section 5); the full conformance check stays Surface+Check.
//
// The walk is syntactic: a `let` signature's pattern is usually a
// bare variable, but bound names are collected from whatever
// pattern was written so a destructuring signature cannot hide a
// member. Test blocks and imports are not interface members.
func PublicNames(f *syntax.File) map[string]bool {
	out := map[string]bool{}
	if f == nil {
		return out
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *syntax.FnDecl:
			if d.Name != nil {
				out[d.Name.Name] = true
			}
		case *syntax.LetDecl:
			bindNames(d.Pat, out)
		case *syntax.TypeDecl:
			if d.Name != nil {
				out[d.Name.Name] = true
			}
		case *syntax.ImportDecl, *syntax.TestDecl, *syntax.ExternDecl:
			// not members of the public surface
		default:
			panic("unreachable")
		}
	}
	return out
}

// bindNames collects every variable a pattern binds.
func bindNames(p syntax.Pat, out map[string]bool) {
	switch p := p.(type) {
	case *syntax.VarPat:
		if p.Name != nil {
			out[p.Name.Name] = true
		}
	case *syntax.WildPat, *syntax.LitPat:
		// binds nothing
	case *syntax.CtorPat:
		for _, a := range p.Args {
			bindNames(a.Pat, out)
		}
		for _, f := range p.Fields {
			if f.Punned() {
				if f.Name != nil {
					out[f.Name.Name] = true
				}
			} else {
				bindNames(f.Pat, out)
			}
		}
	case *syntax.TuplePat:
		for _, e := range p.Elems {
			bindNames(e, out)
		}
	case *syntax.RecordPat:
		for _, f := range p.Fields {
			if f.Punned() {
				if f.Name != nil {
					out[f.Name.Name] = true
				}
			} else {
				bindNames(f.Pat, out)
			}
		}
	case *syntax.ListPat:
		for _, e := range p.Elems {
			bindNames(e, out)
		}
		if p.Rest != nil {
			bindNames(p.Rest, out)
		}
	case *syntax.OrPat:
		// Every alternative binds the same names — one suffices.
		if len(p.Alts) > 0 {
			bindNames(p.Alts[0], out)
		}
	case *syntax.AsPat:
		if p.Name != nil {
			out[p.Name.Name] = true
		}
		bindNames(p.Pat, out)
	default:
		panic("unreachable")
	}
}
