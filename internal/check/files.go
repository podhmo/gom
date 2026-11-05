package check

import (
	"maps"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Files type-checks the resolved files of one package as a unit:
// every file's declarations register into one shared environment —
// type constructors, value signatures, constructors and record
// fields — so a name a sibling file declares is typed exactly like a
// same-file declaration (docs/decisions/2026-11-05-package-
// compilation-unit.md). The typed declarations then partition back
// into one *File per input, in input order; per-file File.Imports
// hold that file's own import clauses.
//
// The merge is of Resolved's side tables only: Defs and Uses are
// keyed by *syntax.Ident, so concatenating the files' declaration
// lists and unioning the tables preserves every binding the unit
// resolver established — including the cross-file ones.
func Files(ress []*resolve.Resolved, src SignatureSource) ([]*File, diag.List) {
	if len(ress) == 0 {
		return nil, nil
	}
	if len(ress) == 1 {
		f, diags := Check(ress[0], src)
		return []*File{f}, diags
	}
	merged := &resolve.Resolved{
		File: &syntax.File{Sp: ress[0].File.Sp, Package: ress[0].File.Package},
		Defs: make(map[*syntax.Ident]*resolve.Symbol),
		Uses: make(map[*syntax.Ident]*resolve.Symbol),
	}
	for _, res := range ress {
		merged.File.Decls = append(merged.File.Decls, res.File.Decls...)
		maps.Copy(merged.Defs, res.Defs)
		maps.Copy(merged.Uses, res.Uses)
	}
	f, diags := Check(merged, src)
	files := make([]*File, len(ress))
	byFile := make(map[string]*File, len(ress))
	for i, res := range ress {
		nf := &File{Sp: res.File.Sp}
		if res.File.Package != nil {
			nf.Package = res.File.Package.Name
			nf.PkgSp = res.File.Package.Sp
		}
		byFile[res.File.Sp.File] = nf
		files[i] = nf
	}
	for _, d := range f.Decls {
		if nf := byFile[d.Span().File]; nf != nil {
			nf.Decls = append(nf.Decls, d)
		}
	}
	for _, im := range f.Imports {
		if nf := byFile[im.Sp.File]; nf != nil {
			nf.Imports = append(nf.Imports, im)
		}
	}
	return files, diags
}

// TypeDeclSource is an optional SignatureSource capability: a source
// that loads Gom packages can expose the (requalified) type
// declarations behind its member signatures so `.field`, record
// literals and constructor payload names type against the real
// declaration rather than failing to the detached builtinVariant
// entry. The declarations' own names carry the package path — a
// Shape the gomimport loader requalified is Con{Qual: path}.
type TypeDeclSource interface {
	// TypeDecls returns the loaded packages' type declarations,
	// discovered so far; the checker registers them before bodies
	// are checked.
	TypeDecls() []*TypeDecl
}

// registerForeignTypes installs the type-declaration tables of
// packages the signature source loaded — sibling declarations of the
// package being compiled are covered by the ordinary registration
// pass; this is for types that arrive through imports only.
func (c *checker) registerForeignTypes(decls []*TypeDecl) {
	for _, td := range decls {
		if td == nil || td.Sym == nil {
			continue
		}
		key := conKey{qual: td.Sym.Package, name: td.Sym.Name}
		if _, ok := c.tyCons[key]; ok {
			continue // declared or already loaded
		}
		args := make([]types.Type, len(td.TypeParams))
		for i, v := range td.TypeParams {
			args[i] = v
		}
		ti := &typeInfo{
			sym:    td.Sym,
			con:    &types.Con{Qual: td.Sym.Package, Name: td.Sym.Name, Args: args},
			params: td.TypeParams,
			sp:     td.Sp,
		}
		switch body := td.Body.(type) {
		case *SumType:
			ti.kind = kindSum
			for i, v := range body.Variants {
				vi := &variantInfo{
					sym:    v.Sym,
					name:   v.Sym.Name,
					index:  i,
					parent: ti,
					sp:     v.Sp,
				}
				for j, f := range v.Fields {
					fi := &fieldInfo{
						name:  f.Name,
						label: f.Label,
						index: j,
						sym:   f.Sym,
						typ:   f.Type,
						sp:    f.Sp,
					}
					vi.fields = append(vi.fields, fi)
					if fi.sym != nil {
						c.fieldSyms[fi.sym] = fi
					}
				}
				ti.variants = append(ti.variants, vi)
				c.ctors[vi.sym] = vi
				// The constructor's signature — `fields... -> Parent`
				// — is registered like a declared ctor's so sigOf
				// answers without consulting the source again.
				var sig types.Type = ti.con
				if len(vi.fields) > 0 {
					params := make([]types.FuncParam, len(vi.fields))
					for k, f := range vi.fields {
						params[k] = types.FuncParam{Label: f.label, Type: f.typ}
					}
					sig = types.Fn(ti.con, params...)
				}
				c.vals[vi.sym] = sig
			}
		case *RecordType:
			ti.kind = kindRecord
			for i, f := range body.Fields {
				fi := &fieldInfo{
					name:  f.Name,
					label: f.Label,
					index: i,
					sym:   f.Sym,
					typ:   f.Type,
					sp:    f.Sp,
				}
				ti.fields = append(ti.fields, fi)
				if fi.sym != nil {
					c.fieldSyms[fi.sym] = fi
				}
			}
			c.records = append(c.records, ti)
		case *AliasType:
			ti.kind = kindAlias
			ti.alias = body.Type
			ti.aliasRes = true
		case *AbstractType, nil:
			ti.kind = kindAbstract
		default:
			panic("unreachable")
		}
		c.tyCons[key] = ti
		c.tyBySym[td.Sym] = ti
	}
}
