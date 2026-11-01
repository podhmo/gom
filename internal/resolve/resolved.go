package resolve

import "github.com/podhmo/gom/internal/syntax"

// Resolved is the resolved AST — the output of name resolution
// (phase 3 of the pipeline, docs/design.md section 2).
//
// It is the surface File shared — not copied — together with the
// binding of every identifier occurrence to a *Symbol, kept in two
// side tables in the style of go/types.Info:
//
//   - Defs maps each *syntax.Ident in binding position (VarPat.Name,
//     declaration names, AsPat.Name, import package names, bound type
//     variables, ...) to the symbol it introduces.
//   - Uses maps each *syntax.Ident in reference position (variable
//     references, Select.Sel member selections, qualified names,
//     constructor and type names) to the symbol it refers to.
//
// Keeping the binding out of the tree leaves the surface AST a valid
// input for gom fmt and avoids a second isomorphic node hierarchy;
// the typed AST built by internal/check is a real tree again because
// its nodes have a different shape (types, structured call info).
type Resolved struct {
	File *syntax.File
	Defs map[*syntax.Ident]*Symbol
	Uses map[*syntax.Ident]*Symbol
}

// DefOf returns the symbol bound by an identifier in binding
// position, or nil.
func (r *Resolved) DefOf(id *syntax.Ident) *Symbol {
	if r == nil {
		return nil
	}
	return r.Defs[id]
}

// UseOf returns the symbol an identifier in reference position refers
// to, or nil.
func (r *Resolved) UseOf(id *syntax.Ident) *Symbol {
	if r == nil {
		return nil
	}
	return r.Uses[id]
}
