package iface

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/podhmo/gom/internal/syntax"
)

// Hash returns the semantic hash of a .gomi interface file
// (docs/spec.md section 7.8): a digest of the normalized syntax tree,
// so a comment or formatting change keeps the hash while any change
// to the declared API — a signature, a variant, a type shape — moves
// it and recompiles dependents.
//
// Normalization (the design's "semantic hash from the normalized
// syntax tree"):
//   - comments and trivia are not hashed (doc comments are attached
//     to decls but never enter the encoding);
//   - declaration order is irrelevant: members sort by name;
//   - type variables normalize by first occurrence within each
//     declaration — `fn id (x: a) -> a` and `fn id (x: b) -> b`
//     hash identically, matching the alpha-equality of section 7.6;
//   - parameter names and patterns do not enter the encoding —
//     `fn f ()` and `fn f (u: Unit)` are both a Unit parameter;
//   - labeled-parameter runs normalize like types.Equal (maximal
//     runs sort by label);
//   - qualified type names resolve through the import table — the
//     hash records the path, never the written alias.
//
// Test declarations are admitted by the parser for recovery but
// excluded by normalization — a file carrying one also carries its
// diagnostic, so the hash is still total.
func Hash(f *syntax.File) string {
	sc := hscope{vars: map[string]int{}, imports: importTable(f)}
	var sb strings.Builder
	sb.WriteString("(pkg ")
	if f.Package != nil {
		sb.WriteString(f.Package.Name)
	}
	sb.WriteByte(')')
	for _, n := range hashImports(f) {
		sb.WriteString(n)
	}
	for _, m := range hashMembers(f, sc.imports) {
		sb.WriteString(m)
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

// hscope carries the normalization context for one file: the
// alias→path import table. Per-declaration type-variable scopes are
// opened on top of it.
type hscope struct {
	vars    map[string]int
	imports map[string]string
}

// importTable maps a written qualifier (the alias — explicit or the
// path's last element) to the imported path.
func importTable(f *syntax.File) map[string]string {
	m := map[string]string{}
	for _, d := range f.Decls {
		im, ok := d.(*syntax.ImportDecl)
		if !ok {
			continue
		}
		alias := im.Path
		if im.Name != nil {
			alias = im.Name.Name
		}
		m[alias] = im.Path
	}
	return m
}

// hashImports encodes the file's imports sorted by path.
func hashImports(f *syntax.File) []string {
	var out []string
	for _, d := range f.Decls {
		im, ok := d.(*syntax.ImportDecl)
		if !ok {
			continue
		}
		kind := "gom"
		if im.Go {
			kind = "go"
		}
		out = append(out, fmt.Sprintf("(import %s %q)", kind, im.Path))
	}
	slices.Sort(out)
	return out
}

// hashMembers encodes the file's member declarations sorted by the
// member's name — two interfaces listing the same API in different
// orders hash identically.
func hashMembers(f *syntax.File, imports map[string]string) []string {
	var out []string
	for _, d := range f.Decls {
		sc := hscope{vars: map[string]int{}, imports: imports}
		switch d := d.(type) {
		case *syntax.FnDecl:
			out = append(out, fmt.Sprintf("(fn %s %s)",
				identName(d.Name), sc.hashFnType(d.Params, d.Ret)))
		case *syntax.LetDecl:
			for _, name := range patNames(d.Pat) {
				out = append(out, fmt.Sprintf("(let %s %s)",
					name, sc.hashType(d.Type)))
			}
		case *syntax.TypeDecl:
			out = append(out, sc.hashTypeDecl(d))
		case *syntax.ImportDecl, *syntax.TestDecl, *syntax.ExternDecl:
			// Imports are encoded by hashImports; tests and any
			// other recovered-but-disallowed decls do not enter
			// the interface hash (docs/design.md section 5).
		default:
			panic("unreachable")
		}
	}
	slices.Sort(out)
	return out
}

// hashFnType encodes a signature as the TYPE it declares:
// `(sig ((~label <type>) (<type>) ...) <ret>)`. A `()` parameter
// contributes a Unit type — `fn f () -> T` and `fn f (u: Unit) -> T`
// are the same signature, matching the checker.
func (sc *hscope) hashFnType(params []syntax.Param, ret syntax.TypeExpr) string {
	var sb strings.Builder
	sb.WriteString("(sig (")
	for _, p := range params {
		label := ""
		if p.Label != nil {
			label = p.Label.Name
		}
		fmt.Fprintf(&sb, "(%q ", label)
		if p.Type != nil {
			sc.writeType(&sb, p.Type)
		} else {
			sb.WriteString("(unit)")
		}
		sb.WriteByte(')')
	}
	sb.WriteString(") ")
	sc.writeType(&sb, ret)
	sb.WriteByte(')')
	return sb.String()
}

// hashTypeDecl encodes `type Name a b = body`: parameters become
// positions (`v0`, `v1`), and the body is the type's shape.
func (sc *hscope) hashTypeDecl(d *syntax.TypeDecl) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "(type %s (", identName(d.Name))
	// Type variables are bound by the declaration: their names are
	// positions here, so `type Box a` and `type Box b` hash the
	// same and the body's references resolve through the table.
	vars := map[string]int{}
	for i, p := range d.Params {
		if p != nil {
			vars[p.Name] = i
		}
		fmt.Fprintf(&sb, "v%d", i)
		if i < len(d.Params)-1 {
			sb.WriteByte(' ')
		}
	}
	sb.WriteString(") ")
	body := &hscope{vars: vars, imports: sc.imports}
	switch b := d.Body.(type) {
	case *syntax.SumBody:
		sb.WriteString("(sum")
		for _, v := range b.Variants {
			fmt.Fprintf(&sb, " (variant %s (", identName(v.Name))
			for i, f := range v.Params {
				if i > 0 {
					sb.WriteByte(' ')
				}
				fmt.Fprintf(&sb, "(f %q ", fieldName(f))
				body.writeType(&sb, f.Type)
				sb.WriteByte(')')
			}
			sb.WriteString("))")
		}
		sb.WriteByte(')')
	case *syntax.RecordBody:
		sb.WriteString("(rec")
		for _, f := range b.Fields {
			fmt.Fprintf(&sb, " (f %q ", fieldName(f))
			body.writeType(&sb, f.Type)
			sb.WriteByte(')')
		}
		sb.WriteByte(')')
	case *syntax.AliasBody:
		sb.WriteString("(alias ")
		body.writeType(&sb, b.Type)
		sb.WriteByte(')')
	case *syntax.AbstractBody, nil:
		sb.WriteString("(abstract)")
	default:
		panic("unreachable")
	}
	sb.WriteByte(')')
	return sb.String()
}

// hashType encodes a type expression in this scope — signatures
// number their variables by first occurrence.
func (sc *hscope) hashType(t syntax.TypeExpr) string {
	var sb strings.Builder
	sc.writeType(&sb, t)
	return sb.String()
}

// writeType is the recursive encoder.
func (sc *hscope) writeType(sb *strings.Builder, t syntax.TypeExpr) {
	switch t := t.(type) {
	case nil:
		sb.WriteString("(none)")
	case *syntax.TypeVar:
		name := identName(t.Name)
		if i, ok := sc.vars[name]; ok {
			fmt.Fprintf(sb, "(v %d)", i)
			return
		}
		i := len(sc.vars)
		sc.vars[name] = i
		fmt.Fprintf(sb, "(v %d)", i)
	case *syntax.TypeName:
		name := identName(t.Name)
		if t.Qual != nil {
			qual := t.Qual.Name
			if path, ok := sc.imports[qual]; ok {
				qual = path // resolve the written alias to its path
			}
			fmt.Fprintf(sb, "(name %q %q)", qual, name)
		} else {
			fmt.Fprintf(sb, "(name %q)", name)
		}
	case *syntax.TypeApp:
		sb.WriteString("(app ")
		sc.writeType(sb, t.Ctor)
		for _, a := range t.Args {
			sb.WriteByte(' ')
			sc.writeType(sb, a)
		}
		sb.WriteByte(')')
	case *syntax.TypeFn:
		sb.WriteString("(fn (")
		// The labeled-run normalization of types.Equal applies to
		// the encoding too: within each maximal run of labeled
		// params, order is not semantic.
		for _, grp := range labelRuns(t.Params) {
			for _, p := range grp {
				sb.WriteByte('(')
				if p.Label != nil {
					fmt.Fprintf(sb, "~%s ", p.Label.Name)
				}
				sc.writeType(sb, p.Type)
				sb.WriteByte(')')
			}
		}
		sb.WriteString(") ")
		sc.writeType(sb, t.Ret)
		sb.WriteByte(')')
	case *syntax.TypeTuple:
		sb.WriteString("(tup")
		for _, e := range t.Elems {
			sb.WriteByte(' ')
			sc.writeType(sb, e)
		}
		sb.WriteByte(')')
	case *syntax.TypeRecord:
		sb.WriteString("(rec")
		for _, f := range t.Fields {
			fmt.Fprintf(sb, " (f %q ", fieldName(f))
			sc.writeType(sb, f.Type)
			sb.WriteByte(')')
		}
		sb.WriteByte(')')
	default:
		panic("unreachable")
	}
}

// labelRuns groups a parameter list into segments where each maximal
// run of labeled parameters is sorted by label — positional
// parameters keep position and split runs, exactly like
// types.Equal's normalizeLabels.
func labelRuns(ps []syntax.TypeParam) [][]syntax.TypeParam {
	var out [][]syntax.TypeParam
	for i := 0; i < len(ps); {
		if ps[i].Label == nil {
			out = append(out, []syntax.TypeParam{ps[i]})
			i++
			continue
		}
		j := i
		for j < len(ps) && ps[j].Label != nil {
			j++
		}
		run := slices.Clone(ps[i:j])
		slices.SortFunc(run, func(a, b syntax.TypeParam) int {
			return strings.Compare(a.Label.Name, b.Label.Name)
		})
		out = append(out, run)
		i = j
	}
	return out
}

// fieldName renders a surface field's member name ("" for positional
// payload, which keeps its index in the encoding order).
func fieldName(f syntax.Field) string {
	switch {
	case f.Label != nil:
		return "~" + f.Label.Name
	case f.Name != nil:
		return f.Name.Name
	default:
		return ""
	}
}

// identName is the nil-tolerant Ident text accessor.
func identName(id *syntax.Ident) string {
	if id == nil {
		return ""
	}
	return id.Name
}
