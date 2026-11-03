package codegen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"strings"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Module paths of the support packages generated code imports. The
// stdlib packages are the Go-side representations of the builtin
// types (docs/design.md section 4.6).
const (
	rtPath     = "github.com/podhmo/gom/rt"
	resultPath = "github.com/podhmo/gom/stdlib/result"
	optionPath = "github.com/podhmo/gom/stdlib/option"
	listPath   = "github.com/podhmo/gom/stdlib/list"
)

// ctorDecl records a locally declared variant constructor: the type
// it belongs to and its payload fields, so applications and tests can
// render Type_Ctor struct literals with the right field names.
type ctorDecl struct {
	typeName string
	fields   []core.TField
}

// gen is the per-file generator state. It owns the output buffer, the
// import table, the name map, and the local declaration indexes.
type gen struct {
	li   *diag.LineIndex
	fset *token.FileSet
	buf  bytes.Buffer

	// imports maps a Go import path to the local name bound for it,
	// and names does the reverse for collision checks.
	imports map[string]string
	inames  map[string]bool

	// names is the resolved symbol → emitted identifier cache.
	names map[*resolve.Symbol]string
	// used is every identifier taken in the file (decls, binders,
	// temps) so fresh temps and import bindings cannot collide.
	used map[string]bool
	// pused is the package-scope subset — declared type, function
	// and variable names. A binder may not reuse these: the local
	// would shadow the decl for every reference inside its scope.
	pused map[string]bool
	// fnames is the binder-name set of the declaration being
	// indexed — binder identifiers need only differ inside their
	// own emitted function, so the set resets per declaration.
	fnames map[string]bool

	ctors  map[*resolve.Symbol]*ctorDecl // locally declared variants
	localT map[string]bool               // type names declared in this file

	// exported is the package's public surface — the decl symbols
	// lowering flagged Exported (spec section 5): they emit
	// capitalized identifiers where private decls stay camelCase.
	exported map[*resolve.Symbol]bool

	// selfPath is the file's own Go import path (core.File's
	// SelfImportPath): references into it emit bare identifiers
	// because a package never imports itself. Empty for ordinary
	// inputs, which never collide with a toolchain-known path.
	selfPath string

	fileImports []core.Import // the file's resolved imports (alias lookup)

	testFn *core.Func // the test's fn: escapes targeting it call t.Errorf

	// depth is the current indentation level, pendingIndent marks a
	// fresh output line whose first write must pad. Statements are
	// emitted already-indented because a `//line` directive — which
	// is only recognized at column 1 — pins the next line's column
	// and gofmt will not re-indent it.
	depth         int
	pendingIndent bool

	// marks collects the span of every `//gomsrc` sentinel in
	// emission order; the sentinel carries its index so spliceLines
	// can pair each spliced `//line` directive with the span it maps
	// (docs/design.md section 2.2's correspondence table).
	marks []diag.Span
}

func newGen(li *diag.LineIndex) *gen {
	return &gen{
		li:       li,
		fset:     token.NewFileSet(),
		imports:  map[string]string{},
		inames:   map[string]bool{},
		names:    map[*resolve.Symbol]string{},
		used:     map[string]bool{},
		pused:    map[string]bool{},
		ctors:    map[*resolve.Symbol]*ctorDecl{},
		localT:   map[string]bool{},
		exported: map[*resolve.Symbol]bool{},
	}
}

// indexDecls builds the local declaration indexes: every declared
// name, every local variant constructor, and every local type name —
// used names block fresh temps, local types shadow the builtin map.
func (g *gen) indexDecls(f *core.File) {
	// The public surface is settled before any name is claimed:
	// exported decls claim their capitalized identifier.
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *core.FuncDecl:
			if d.Sym != nil && d.Exported {
				g.exported[d.Sym] = true
			}
		case *core.VarDecl:
			if d.Sym != nil && d.Exported {
				g.exported[d.Sym] = true
			}
		case *core.TypeDecl:
			// no exported flag yet — type names are always
			// emitted capitalized (see typeDeclName).
		case *core.TestDecl:
			// test functions are named from d.Name, deduplicated later
		default:
			panic("unreachable")
		}
	}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *core.FuncDecl:
			if d.Sym != nil {
				g.declSymName(d.Sym)
			}
		case *core.VarDecl:
			if d.Sym != nil {
				g.declSymName(d.Sym)
			}
		case *core.TypeDecl:
			if d.Sym != nil {
				// The type name is a package-scope identifier too
				// — `_foo` valueNames to `Foo` — so it must also
				// block binder names, not just the builtin map.
				name := g.typeDeclName(d.Sym)
				g.localT[name] = true
				g.pused[name] = true
				g.used[name] = true
			}
			if sum, ok := d.Body.(*core.TSum); ok {
				for i := range sum.Variants {
					v := &sum.Variants[i]
					if v.Sym != nil && d.Sym != nil {
						g.ctors[v.Sym] = &ctorDecl{typeName: d.Sym.Name, fields: v.Fields}
					}
				}
			}
		case *core.TestDecl:
			// test functions are named from d.Name, deduplicated later
		default:
			panic("unreachable")
		}
	}
	// Every binder anywhere also blocks temp naming. Binder names
	// dedup inside their own declaration only — the emitted Go
	// function's scope: `let x` in f and `let x` in g never share a
	// block, so both stay `x`.
	for _, d := range f.Decls {
		g.fnames = map[string]bool{}
		walkDeclBinders(d, func(b *core.Bind) {
			if b != nil && !b.Discard && b.Sym != nil {
				g.bindSymName(b.Sym)
			}
		})
	}
	g.fnames = nil
	// Generated comparators claim package-scope names — both halves
	// resolve the same symbols, so a test body's `gomEqN` call names
	// the function the main half emits. Generated display functions
	// are the test half's counterpart.
	for _, d := range f.EqDecls {
		if d.Sym != nil {
			g.declSymName(d.Sym)
		}
	}
	for _, d := range f.ShowDecls {
		if d.Sym != nil {
			g.declSymName(d.Sym)
		}
	}
}

// --- names ---

// symName returns the emitted identifier for a top-level or value
// symbol (no package qualification — callers add it). Every symbol
// the file can reference is pre-resolved by indexDecls, so calls
// during emission hit the cache; the fallback (an unregistered or
// foreign symbol) dedups against the whole file's identifiers, the
// conservative choice.
func (g *gen) symName(sym *resolve.Symbol) string {
	return g.claim(sym, g.used, g.used)
}

// declSymName resolves a package-level symbol — a FuncDecl or
// VarDecl name. Only other package names block: a same-named binder
// is mangled instead (bindSymName), and even without mangling the
// decl could not be referenced inside the binder's scope anyway
// (Gom shadowing).
func (g *gen) declSymName(sym *resolve.Symbol) string {
	return g.claim(sym, g.pused, g.pused)
}

// bindSymName resolves a binder inside the declaration being
// indexed. Distinct symbols sharing a Gom name — a shadowing
// `let x = x + 1`, a case bind reusing a parameter's name — are
// disambiguated with a numeric suffix (`x`, `x1`, `x2`) assigned in
// walk order (params first, then lets as written): a second `x :=`
// in one Go block is a redeclaration error.
func (g *gen) bindSymName(sym *resolve.Symbol) string {
	return g.claim(sym, g.fnames, g.fnames, g.pused)
}

// claim resolves sym to its emitted identifier, choosing the first
// candidate not taken in any blocks set and registering it in mark
// and in the file-wide used set.
func (g *gen) claim(sym *resolve.Symbol, mark map[string]bool, blocks ...map[string]bool) string {
	if n, ok := g.names[sym]; ok {
		return n
	}
	base := g.safeIdent(valueName(sym.Name))
	if g.exported[sym] {
		// .gomi members emit with a capital letter (spec section
		// 5): `fn is_some` → `IsSome`.
		base = g.safeIdent(exportName(sym.Name))
	}
	n := base
outer:
	for i := 1; ; i++ {
		for _, s := range blocks {
			if s[n] {
				n = base + itoa(i)
				continue outer
			}
		}
		break
	}
	g.names[sym] = n
	mark[n] = true
	g.used[n] = true
	return n
}

// bindName resolves a binder to its identifier, `_` for discards.
// It registers non-discard names in used so temps cannot collide.
func (g *gen) bindName(b *core.Bind) string {
	if b.Discard {
		return "_"
	}
	return g.symName(b.Sym)
}

// typeDeclName is the emitted name of a declared type: the Gom type
// name already carries its case (`Shape`, `Option`).
func (g *gen) typeDeclName(sym *resolve.Symbol) string {
	return exportName(sym.Name)
}

// typeName renders a declared type's name from its symbol.
func (g *gen) typeName(sym *resolve.Symbol) string {
	if n, ok := g.names[sym]; ok {
		return n
	}
	n := exportName(sym.Name)
	g.names[sym] = n
	return n
}

// --- imports ---

// imp returns the local name bound to path, registering the import on
// first use. The preferred name is taken from the file's import
// clause when present, else the path's base; a collision with an
// identifier used in the file adds a trailing `_` until unique.
func (g *gen) imp(path string) string {
	if n, ok := g.imports[path]; ok {
		return n
	}
	name := g.importAlias(path)
	for g.used[name] || g.inames[name] {
		name += "_"
	}
	g.imports[path] = name
	g.inames[name] = true
	return name
}

// importAlias finds the preferred local name for path: the bound
// module name when the file imports it explicitly, else the path
// base.
func (g *gen) importAlias(path string) string {
	for _, im := range g.fileImports {
		if im.Path == path && im.Sym != nil && im.Sym.Name != "" {
			return im.Sym.Name
		}
	}
	return pathBase(path)
}

func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	return p
}

// pkgQual renders `pkg.` for the given import path — or the empty
// qualifier for the file's own package, which never imports itself
// (a stdlib package referencing its own representation).
func (g *gen) pkgQual(path string) string {
	if path != "" && path == g.selfPath {
		return ""
	}
	return g.imp(path) + "."
}

// qualIdent renders an imported-package-qualified identifier —
// `pkg.Name` — or a bare `Name` when path is the file's own import
// path.
func (g *gen) qualIdent(path, name string) ast.Expr {
	if path != "" && path == g.selfPath {
		return ast.NewIdent(name)
	}
	return &ast.SelectorExpr{X: ast.NewIdent(g.imp(path)), Sel: ast.NewIdent(name)}
}

// --- output helpers ---

// line emits a sentinel comment marking where a `//line file:N`
// directive goes. A real `//line` directive can only be written at
// column 1 *and* gofmt will not indent the line following one, so
// the directive is spliced in after format.Source — the sentinel is
// an ordinary comment that formats normally and is then replaced
// textually (see render in codegen.go).
func (g *gen) line(sp diag.Span) {
	if !sp.Valid() {
		return
	}
	g.pad()
	idx := len(g.marks)
	g.marks = append(g.marks, sp)
	fmt.Fprintf(&g.buf, "//gomsrc:%d:%d:%s\n", idx, g.li.Pos(sp.Start).Line, sp.File)
	g.pendingIndent = true
}

// pad writes the current indentation once at the start of a fresh
// output line.
func (g *gen) pad() {
	if !g.pendingIndent {
		return
	}
	for i := 0; i < g.depth; i++ {
		g.buf.WriteByte('\t')
	}
	g.pendingIndent = false
}

// raw writes text verbatim, padding a fresh line first.
func (g *gen) raw(s string) { g.pad(); g.buf.WriteString(s) }

// nl ends the output line; the next write pads to depth.
func (g *gen) nl() { g.buf.WriteByte('\n'); g.pendingIndent = true }

// expr renders a go/ast expression with go/printer.
func (g *gen) expr(e ast.Expr) {
	g.pad()
	if err := printer.Fprint(&g.buf, g.fset, e); err != nil {
		panic(fmt.Sprintf("codegen: printing %T: %v", e, err))
	}
}

// typ renders a go/ast type expression with go/printer.
func (g *gen) typ(e ast.Expr) { g.expr(e) }

// open writes ` {`, ends the line, and increases the depth.
func (g *gen) open() { g.raw(" {"); g.nl(); g.depth++ }

// close decreases the depth and writes `}` on its own line.
func (g *gen) close() { g.depth--; g.raw("}"); g.nl() }

// closeInline decreases the depth and writes `}` without a newline
// — for `}()` and the tail of an inline function literal.
func (g *gen) closeInline() { g.depth--; g.raw("}") }

// midElse closes a block and opens the `else` on the same line.
func (g *gen) midElse() {
	g.depth--
	g.raw("} else {")
	g.nl()
	g.depth++
}

// caseLabel writes a `case x:`/`default:` label at the switch's own
// depth and indents the arm body below it.
func (g *gen) caseLabel(f func()) {
	g.depth--
	f()
	g.raw(":")
	g.nl()
	g.depth++
}

// --- builtin type map ---

// builtinCon maps the builtin-scope constructor names to their Go
// representation kind. Lookup happens only when the name is not
// declared locally in the file.
func builtinConKind(name string) conKind {
	switch name {
	case "Unit":
		return conUnit
	case "Bool":
		return conSimple
	case "Int", "Int64":
		return conInt64
	case "Int32":
		return conSimple
	case "Float":
		return conSimple
	case "Char":
		return conSimple
	case "String":
		return conSimple
	case "GoError":
		return conError
	case "List":
		return conList
	case "Option":
		return conOption
	case "Result":
		return conResult
	case "Ref":
		return conPtr
	case "Array":
		return conSlice
	case "Ptr":
		return conPtr
	case "Chan":
		return conChan
	case "RecvChan":
		return conRecvChan
	case "SendChan":
		return conSendChan
	case "Task":
		return conTask
	default:
		return conOther
	}
}

type conKind int

const (
	conOther    conKind = iota // not a builtin: local or qualified type
	conUnit                    // struct{}
	conSimple                  // bool / int32 / float64 / rune / string
	conInt64                   // int64
	conError                   // error
	conList                    // list.List[A]
	conOption                  // option.Option[A]
	conResult                  // result.Result[T, E]
	conPtr                     // *T
	conSlice                   // []T
	conChan                    // chan T
	conRecvChan                // <-chan T
	conSendChan                // chan<- T
	conTask                    // rt.Task[A]
)

// letConv is the conversion a `name := rhs` let needs when the RHS's
// own Go type is narrower than the binder's declared type — `:=`
// re-infers from the RHS, so the declared spelling must appear
// explicitly as `name := T(rhs)`. Two RHS shapes mislead it:
//
//   - an untyped integer constant: `x := 6 * 7` binds `int`, not the
//     `int64` Gom Int maps to (intLitConv — Go adapts untyped
//     constants at every other position, but `:=` locks the
//     default in);
//   - a sealed-ADT constructor application: `x := Shape_C{..}` binds
//     the concrete variant struct, not the ADT interface the binder
//     declares — `x == y` between two such variables then compares
//     different concrete types, and a type switch on one is not
//     compilable Go ("x is not an interface"). The emitted
//     conversion names the ADT interface: `x := Shape(Shape_C{..})`.
//     The builtin Result representation emits constructor *calls*
//     (result.Ok(...)) whose Go type already is the declared type —
//     no conversion.
//
// nil for unaffected RHS shapes.
func (g *gen) letConv(t types.Type, x core.Expr) ast.Expr {
	if s := g.intLitConv(t, x); s != "" {
		return ast.NewIdent(s)
	}
	if ca, ok := x.(*core.CtorApp); ok && !(isResultCon(ca.Type()) && !g.localT["Result"]) {
		return g.goType(t)
	}
	return nil
}

// intLitConv is the conversion a `name := rhs` let needs when the
// binder's type is an integer whose Go literal default differs and
// the RHS is an untyped constant expression (`x := 6 * 7` would
// bind `int`, not the `int64`/`int32` the type says — Go adapts
// untyped constants at every other position, but `:=` locks the
// default in). Empty for unaffected types or a typed RHS.
func (g *gen) intLitConv(t types.Type, x core.Expr) string {
	if !untypedConst(x) {
		return ""
	}
	c, ok := types.Chase(t).(*types.Con)
	if !ok || c.Qual != "" || g.localT[exportName(c.Name)] {
		return ""
	}
	switch c.Name {
	case "Int", "Int64":
		return "int64"
	case "Int32":
		return "int32"
	}
	return ""
}

// untypedConst reports whether a core expr emits as a Go untyped
// constant expression: literals, unary ops, and binary ops composed
// entirely of them. Every other node emits a value with a concrete
// type, so `:=` over it is already safe.
func untypedConst(x core.Expr) bool {
	switch e := x.(type) {
	case *core.Lit:
		return true
	case *core.Unary:
		return untypedConst(e.X)
	case *core.Binary:
		return untypedConst(e.X) && untypedConst(e.Y)
	case *core.Var, *core.FuncRef, *core.CtorRef, *core.GoFuncRef,
		*core.FieldGet, *core.CallDirect, *core.CallFunc,
		*core.CallGo, *core.CallBuiltin, *core.CtorApp, *core.Equal,
		*core.TupleNew, *core.ListNew, *core.RecordNew,
		*core.RecordUpdate, *core.Conv, *core.Closure, *core.If,
		*core.Match, *core.SelectExpr, *core.Spawn, *core.ForRange,
		*core.TailCall, *core.EscapeResult, *core.EscapeCall,
		*core.ResultFromGo, *core.OptionFromGo, *core.TupleFromGo:
		return false
	default:
		panic("unreachable")
	}
}

// simpleBuiltinName gives the Go identifier for the conSimple kinds.
func simpleBuiltinName(name string) string {
	switch name {
	case "Bool":
		return "bool"
	case "Int32":
		return "int32"
	case "Float":
		return "float64"
	case "Char":
		return "rune"
	case "String":
		return "string"
	default:
		panic("unreachable")
	}
}
