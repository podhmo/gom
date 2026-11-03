package iface

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Source is one compiled .gom file as gom info needs it: the parse
// tree (declaration order, doc comments) plus its typed declarations
// (resolved signature types).
type Source struct {
	AST   *syntax.File
	Typed *check.File
}

// Draft renders a .gomi interface draft for the given sources
// (docs/spec.md section 7.7): every non-test top-level member with
// its doc comment, signatures resolved to their inferred types. The
// sources are the .gom files of one package — today usually a
// single file, since same-package multi-file scope is still open
// (docs/open-questions.md). The output is canonical text; gom info
// writes it only when it differs from the existing file.
//
// The draft is a starting point: it publishes every member, so the
// author trims what should stay private and may replace a concrete
// `type T = ...` with the abstract `type T`.
func Draft(srcs []Source) []byte {
	d := &drafter{
		vars:     map[*types.Var]string{},
		imports:  map[string]imp{},
		varCount: 0,
	}
	var out strings.Builder
	pkg := ""
	for _, s := range srcs {
		if s.AST != nil && s.AST.PackageName() != "" {
			pkg = s.AST.PackageName()
			break
		}
		if s.Typed != nil && s.Typed.Package != "" {
			pkg = s.Typed.Package
			break
		}
	}
	fmt.Fprintf(&out, "package %s\n", pkg)

	var body strings.Builder
	for _, s := range srcs {
		d.file(&body, s)
	}

	// Imports are collected while rendering — emit them now, sorted
	// by path for determinism.
	paths := slices.Sorted(maps.Keys(d.imports))
	if len(paths) > 0 {
		out.WriteByte('\n')
		for _, p := range paths {
			im := d.imports[p]
			if im.goImport {
				if im.alias != pathBase(p) {
					fmt.Fprintf(&out, "import go %s %q\n", im.alias, p)
				} else {
					fmt.Fprintf(&out, "import go %q\n", p)
				}
			} else {
				if im.alias != pathBase(p) {
					fmt.Fprintf(&out, "import %s %q\n", im.alias, p)
				} else {
					fmt.Fprintf(&out, "import %q\n", p)
				}
			}
		}
	}
	// Sections are separated by a blank line; the file ends with a
	// single newline.
	text := strings.TrimRight(body.String(), "\n")
	if len(paths) > 0 && text != "" {
		out.WriteByte('\n')
	}
	if text != "" {
		out.WriteByte('\n')
		out.WriteString(text)
		out.WriteByte('\n')
	}
	return []byte(out.String())
}

// imp is one needed import: its path, whether it is a `go` import,
// and the alias the draft qualifies names with.
type imp struct {
	alias    string
	goImport bool
}

// drafter carries the mutable state of one draft rendering:
// type-variable naming (per declaration) and the imports the emitted
// types refer to.
type drafter struct {
	vars      map[*types.Var]string // var → display name, reset per decl
	varCount  int
	imports   map[string]imp    // path → import
	usedAlias map[string]string // alias → path (collision detection)
}

// file renders one source's declarations into w, in surface order.
func (d *drafter) file(w *strings.Builder, s Source) {
	if s.AST == nil || s.Typed == nil {
		return
	}
	typed := typedByName(s.Typed)
	// Member order follows the written declarations; doc comments
	// ride along from the surface decl.
	for _, decl := range s.AST.Decls {
		d.vars = map[*types.Var]string{} // names restart per decl
		d.varCount = 0
		var lines []string
		switch decl := decl.(type) {
		case *syntax.FnDecl:
			if decl.TestOnly || decl.Name == nil {
				continue
			}
			td, _ := typed[decl.Name.Name].(*check.FnDecl)
			if td == nil {
				continue
			}
			lines = []string{d.fnDecl(decl.Name.Name, td)}
		case *syntax.LetDecl:
			if decl.TestOnly {
				continue
			}
			for _, name := range patNames(decl.Pat) {
				lines = append(lines, d.letMember(name, typed[name])...)
			}
		case *syntax.TypeDecl:
			if decl.Name == nil {
				continue
			}
			td, _ := typed[decl.Name.Name].(*check.TypeDecl)
			if td == nil {
				continue
			}
			lines = d.typeDecl(decl.Name.Name, td)
		case *syntax.TestDecl, *syntax.ImportDecl, *syntax.ExternDecl:
			continue
		default:
			panic("unreachable")
		}
		if len(lines) == 0 {
			continue
		}
		writeDoc(w, declDoc(decl))
		for _, l := range lines {
			w.WriteString(l)
			w.WriteByte('\n')
		}
		w.WriteByte('\n')
	}
}

// fnDecl renders `fn name (p: T) ... -> R`. Parameter groups come
// from the typed declaration — name (or pattern) and label from the
// typed parameters, the type from the signature.
func (d *drafter) fnDecl(name string, td *check.FnDecl) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "fn %s", name)
	for i, p := range td.Params {
		if i < len(td.Sig.Params) {
			fp := td.Sig.Params[i]
			if isUnitPat(p.Pat) {
				sb.WriteString(" ()")
				continue
			}
			sb.WriteString(" (")
			if fp.Label != "" {
				sb.WriteByte('~')
			}
			sb.WriteString(patText(p.Pat))
			sb.WriteString(": ")
			d.writeType(&sb, fp.Type)
			sb.WriteByte(')')
		}
	}
	// A typed `fn` always carries Sig; keep a defensive fallback.
	ret := types.Unit
	if td.Sig != nil {
		ret = td.Sig.Ret
	}
	sb.WriteString(" -> ")
	d.writeType(&sb, ret)
	return sb.String()
}

// letMember renders the interface line(s) for one name bound by a
// `let`. A `let f = fn x -> x` that check shaped into a function
// declaration publishes a `fn` signature — the arity is part of the
// contract. A generalized value (`Thunk`) or an ordinary binding
// publishes `let name : T`.
func (d *drafter) letMember(name string, td check.Decl) []string {
	switch td := td.(type) {
	case *check.FnDecl:
		if td.Thunk {
			ret := types.Unit
			if td.Sig != nil {
				ret = td.Sig.Ret
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "let %s : ", name)
			d.writeType(&sb, ret)
			return []string{sb.String()}
		}
		return []string{d.fnDecl(name, td)}
	case *check.LetDecl:
		for _, m := range letMembers(td) {
			if m.Name != name {
				continue
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "let %s : ", name)
			d.writeType(&sb, m.Type)
			return []string{sb.String()}
		}
	}
	return nil
}

// typeDecl renders `type Name a b = <body>` (or bare `type Name`
// for an abstract declaration).
func (d *drafter) typeDecl(name string, td *check.TypeDecl) []string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "type %s", name)
	for _, v := range td.TypeParams {
		sb.WriteByte(' ')
		sb.WriteString(d.varName(v))
	}
	switch b := td.Body.(type) {
	case *check.SumType:
		sb.WriteString(" =")
		lines := []string{sb.String()}
		for _, v := range b.Variants {
			vn := ""
			if v.Sym != nil {
				vn = v.Sym.Name
			}
			var vs strings.Builder
			fmt.Fprintf(&vs, "  | %s", vn)
			for _, f := range v.Fields {
				vs.WriteByte(' ')
				d.writeField(&vs, f)
			}
			lines = append(lines, vs.String())
		}
		return lines
	case *check.RecordType:
		sb.WriteString(" = { ")
		for i, f := range b.Fields {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(f.Name)
			sb.WriteString(" : ")
			d.writeType(&sb, f.Type)
		}
		sb.WriteString(" }")
		return []string{sb.String()}
	case *check.AliasType:
		sb.WriteString(" = ")
		d.writeType(&sb, b.Type)
		return []string{sb.String()}
	case *check.AbstractType, nil:
		return []string{sb.String()}
	default:
		panic("unreachable")
	}
}

// writeField renders one sum-variant payload slot: `(name: T)`,
// `(~label: T)` or positional `T`.
func (d *drafter) writeField(sb *strings.Builder, f check.TField) {
	switch {
	case f.Label != "":
		fmt.Fprintf(sb, "(~%s: ", f.Label)
		d.writeType(sb, f.Type)
		sb.WriteByte(')')
	case f.Name != "":
		fmt.Fprintf(sb, "(%s: ", f.Name)
		d.writeType(sb, f.Type)
		sb.WriteByte(')')
	default:
		d.writeType(sb, f.Type)
	}
}

// --- type rendering ---

// writeType renders t in .gomi surface syntax. Type variables take
// their declared name when it is free, else a fresh name from the
// a, b, … sequence; Go-package types and Gom-package constructors
// register the import they need.
func (d *drafter) writeType(sb *strings.Builder, t types.Type) {
	d.writeT(sb, types.Chase(t), precTop)
}

func (d *drafter) writeT(sb *strings.Builder, t types.Type, p prec) {
	wrap := p != precTop && !atom(t, p)
	if wrap {
		sb.WriteByte('(')
	}
	switch t := t.(type) {
	case *types.Var:
		sb.WriteString(d.varName(t))
	case *types.Meta:
		// An unbound meta should not survive in a checked
		// signature; render it as a fresh variable so the draft
		// stays parseable.
		sb.WriteString(d.freshName())
	case *types.Con:
		if t.Qual != "" {
			sb.WriteString(d.qualifier(t.Qual, false))
			sb.WriteByte('.')
		}
		sb.WriteString(t.Name)
		for _, a := range t.Args {
			sb.WriteByte(' ')
			d.writeT(sb, types.Chase(a), precArg)
		}
	case *types.Func:
		for i, fp := range t.Params {
			if i > 0 {
				sb.WriteString(" -> ")
			}
			if fp.Label != "" {
				sb.WriteByte('~')
				sb.WriteString(fp.Label)
				sb.WriteString(": ")
			}
			d.writeT(sb, types.Chase(fp.Type), precParam)
		}
		sb.WriteString(" -> ")
		d.writeT(sb, types.Chase(t.Ret), precTop)
	case *types.Tuple:
		sb.WriteByte('(')
		for i, e := range t.Elems {
			if i > 0 {
				sb.WriteString(", ")
			}
			d.writeT(sb, types.Chase(e), precTop)
		}
		sb.WriteByte(')')
	case *types.Go:
		sb.WriteString(d.qualifier(t.Ref.Path, true))
		sb.WriteByte('.')
		sb.WriteString(t.Ref.Name)
		if len(t.Args) > 0 {
			sb.WriteByte('[')
			for i, a := range t.Args {
				if i > 0 {
					sb.WriteString(", ")
				}
				d.writeT(sb, types.Chase(a), precTop)
			}
			sb.WriteByte(']')
		}
	default:
		panic("unreachable")
	}
	if wrap {
		sb.WriteByte(')')
	}
}

// prec mirrors types' printing contexts: argument positions need
// parentheses around applications and functions.
type prec int

const (
	precTop prec = iota
	precArg
	precParam
)

// atom reports whether t needs no parentheses in context p — the
// same rule types' own printer uses.
func atom(t types.Type, p prec) bool {
	switch t := t.(type) {
	case *types.Var, *types.Meta, *types.Tuple, *types.Go:
		return true
	case *types.Con:
		return p != precArg || len(t.Args) == 0
	case *types.Func:
		return false
	default:
		panic("unreachable")
	}
}

// varName gives v a printable name for this declaration: its source
// name when free, else a fresh name. Two distinct variables sharing
// one source name get renamed on second use so the draft's type
// stays correct.
func (d *drafter) varName(v *types.Var) string {
	if n, ok := d.vars[v]; ok {
		return n
	}
	name := v.Name
	if name == "" || d.nameTaken(name, v) {
		name = d.freshName()
	}
	d.vars[v] = name
	return name
}

// nameTaken reports whether name is already assigned to a different
// variable in this declaration.
func (d *drafter) nameTaken(name string, v *types.Var) bool {
	for w, n := range d.vars {
		if n == name && w != v {
			return true
		}
	}
	return false
}

// freshName returns the next name in the a, b, …, z, a1, … sequence.
func (d *drafter) freshName() string {
	n := d.varCount
	d.varCount++
	name := string(rune('a' + n%26))
	if n >= 26 {
		name += fmt.Sprint(n / 26)
	}
	// Skip names already used by a source variable this declaration.
	for d.nameTaken(name, nil) {
		n = d.varCount
		d.varCount++
		name = string(rune('a' + n%26))
		if n >= 26 {
			name += fmt.Sprint(n / 26)
		}
	}
	return name
}

// qualifier returns the alias the draft renders for the declaring
// package path and records the import. The reserved "Go" qualifier
// (Go.Slice and friends, types/builtin.go) is written as-is and
// needs no import.
func (d *drafter) qualifier(path_ string, goImport bool) string {
	if path_ == "Go" {
		return "Go"
	}
	if im, ok := d.imports[path_]; ok {
		return im.alias
	}
	alias := pathBase(path_)
	if d.usedAlias == nil {
		d.usedAlias = map[string]string{}
	}
	if other, taken := d.usedAlias[alias]; taken && other != path_ {
		for i := 2; ; i++ {
			cand := fmt.Sprintf("%s%d", alias, i)
			if _, taken := d.usedAlias[cand]; !taken {
				alias = cand
				break
			}
		}
	}
	d.usedAlias[alias] = path_
	d.imports[path_] = imp{alias: alias, goImport: goImport}
	return alias
}

// pathBase returns the last element of an import path — the
// conventional package name.
func pathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// --- helpers ---

// typedByName indexes a typed file's declarations by bound name:
// functions and types by their symbol, lets by every bound variable.
func typedByName(f *check.File) map[string]check.Decl {
	m := map[string]check.Decl{}
	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *check.FnDecl:
			if decl.Sym != nil {
				m[decl.Sym.Name] = decl
			}
		case *check.LetDecl:
			for _, mm := range letMembers(decl) {
				m[mm.Name] = decl
			}
		case *check.TypeDecl:
			if decl.Sym != nil {
				m[decl.Sym.Name] = decl
			}
		case *check.TestDecl:
		default:
			panic("unreachable")
		}
	}
	return m
}

// patNames lists the bound variable names of a surface pattern in
// order — the names a `let pat = e` publishes.
func patNames(p syntax.Pat) []string {
	var out []string
	var walk func(p syntax.Pat)
	// A punned `{ name }` field binds the variable `name`; an
	// explicit `name = pat` binds whatever pat binds.
	walkFieldPat := func(f syntax.FieldPat) {
		if f.Punned() {
			if f.Name != nil {
				out = append(out, f.Name.Name)
			}
			return
		}
		walk(f.Pat)
	}
	walk = func(p syntax.Pat) {
		switch p := p.(type) {
		case nil:
		case *syntax.WildPat, *syntax.LitPat:
		case *syntax.VarPat:
			if p.Name != nil {
				out = append(out, p.Name.Name)
			}
		case *syntax.CtorPat:
			for _, a := range p.Args {
				walk(a.Pat)
			}
			for _, f := range p.Fields {
				walkFieldPat(f)
			}
		case *syntax.TuplePat:
			for _, e := range p.Elems {
				walk(e)
			}
		case *syntax.RecordPat:
			for _, f := range p.Fields {
				walkFieldPat(f)
			}
		case *syntax.ListPat:
			for _, e := range p.Elems {
				walk(e)
			}
			walk(p.Rest)
		case *syntax.OrPat:
			if len(p.Alts) > 0 {
				walk(p.Alts[0])
			}
		case *syntax.AsPat:
			walk(p.Pat)
			if p.Name != nil {
				out = append(out, p.Name.Name)
			}
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return out
}

// patText renders a typed parameter pattern for the draft: a name,
// `_`, `()`, or a tuple `(a, b)`. Refutable forms cannot appear in
// parameter position — they render as `_` defensively.
func patText(p check.Pat) string {
	switch p := p.(type) {
	case nil:
		return "_"
	case *check.WildPat:
		return "_"
	case *check.VarPat:
		if p.Sym != nil {
			return p.Sym.Name
		}
		return "_"
	case *check.LitPat:
		return p.Text
	case *check.TuplePat:
		var sb strings.Builder
		sb.WriteByte('(')
		for i, e := range p.Elems {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(patText(e))
		}
		sb.WriteByte(')')
		return sb.String()
	case *check.AsPat:
		return patText(p.Pat)
	case *check.CtorPat, *check.RecordPat, *check.ListPat, *check.OrPat:
		return "_"
	default:
		panic("unreachable")
	}
}

// isUnitPat reports whether p is the `()` parameter pattern.
func isUnitPat(p check.Pat) bool {
	l, ok := p.(*check.LitPat)
	return ok && l.Kind == syntax.LitUnit
}

// declDoc returns the doc comment attached to a surface decl.
func declDoc(d syntax.Decl) *syntax.Comment {
	switch d := d.(type) {
	case *syntax.FnDecl:
		return d.Doc
	case *syntax.LetDecl:
		return d.Doc
	case *syntax.TypeDecl:
		return d.Doc
	case *syntax.TestDecl, *syntax.ImportDecl, *syntax.ExternDecl:
		return nil
	default:
		panic("unreachable")
	}
}

// writeDoc emits `///` comment lines verbatim.
func writeDoc(w *strings.Builder, c *syntax.Comment) {
	if c == nil {
		return
	}
	w.WriteString("///")
	w.WriteString(c.Text)
	w.WriteByte('\n')
}
