// govet.go — `go vet`-equivalent analysis for generated Go, running
// inside the compile canary (docs/decisions/2026-11-02-fuzz-strategy.md).
//
// The canary already parses and typechecks codegen output; parse +
// go/types catch malformed trees and type errors but NOT the
// "compiles yet wrong" classes vet exists for: unreachable code after
// a fully-terminating branch, self-assignment, a dropped result of a
// must-use call, `string(int)` conversions, and identifier shadowing.
// Real `go vet` is off the table — it needs a temp module plus a
// toolchain subprocess per input, and golang.org/x/tools would be the
// module's first external dependency. These passes therefore port the
// relevant analyzers' logic onto go/ast + go/types (stdlib only).
//
// Check selection targets what generated code can actually produce:
//
//   - unreachable: codegen emits `return`/`panic` in every branch;
//     code after a terminating construct signals an emission bug.
//     Direct port of cmd/vet's unreachable pass.
//   - assign (self-assignment): `x = x` means the renamer or an
//     emitted multi-assign confused a name — impossible deliberately.
//   - unusedresult: bare expression statements exist in generated
//     code (`ignore e` → `compute(40)`), so only vet's own whitelist
//     of always-must-use calls (fmt.Sprintf-class, errors.New,
//     slices/maps/sort helpers, context.With*) is checked, plus
//     dropped `Error`/`String` methods. A hit means `ignore` of a
//     pure call emitted a statement real `go vet` would flag — a
//     genuine wart of the generated module.
//   - stringintconv: `string(x)` on a non-rune integer — codegen
//     emits `string(c)` for Char display (Char → rune, legal); a hit
//     means an Int slipped into a rune conversion, THE classic
//     Go-targeting bug.
//   - shadow: generated identifiers are uniquely renamed (tmpN, vN,
//     y1), so ANY shadow inside a function — including a function
//     literal's parameters — is a renamer defect. Stricter than vet's
//     (non-default) shadow check: every collision with an enclosing
//     function-local declaration reports.
//
// Checks needing types.Info (assign, unusedresult, stringintconv)
// run only when info is non-nil; unreachable and shadow are AST-only
// and run even in the no-toolchain degraded mode. For the `panic`
// builtin check, files should be parsed WITHOUT
// parser.SkipObjectResolution (the canary parses with mode 0): a nil
// info falls back to ast.Ident.Obj like upstream.
//
// Deliberately NOT ported: printf (would flag user-written format
// strings — a user bug, not a compiler bug), bools (`x && x` is a
// faithful translation of user-level `and x x`, likewise), structtag
// /tests/asmdecl/cgocall (no such constructs emitted), copylock
// (rt types carry no locks), shift (constant over-shifts are already
// go/types errors).

package fuzzutil

import (
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"
)

// VetFinding is one vet-equivalent complaint about generated Go.
// Check names the analyzer class it came from.
type VetFinding struct {
	Check string
	Pos   token.Pos
	Msg   string
}

// VetGenerated runs the ported checks over the parsed generated
// files and returns findings sorted by source position. info may be
// nil; then only the AST-only checks (unreachable, shadow) run.
func VetGenerated(fset *token.FileSet, files []*ast.File, info *types.Info) []VetFinding {
	v := &vetPass{fset: fset, info: info}
	for _, f := range files {
		v.checkUnreachable(f)
		v.checkShadow(f)
		if info != nil {
			v.checkAssign(f)
			v.checkUnusedResult(f)
			v.checkStringIntConv(f)
		}
	}
	sort.Slice(v.findings, func(i, j int) bool {
		pi, pj := fset.Position(v.findings[i].Pos), fset.Position(v.findings[j].Pos)
		if pi.Filename != pj.Filename {
			return pi.Filename < pj.Filename
		}
		return pi.Offset < pj.Offset
	})
	return v.findings
}

type vetPass struct {
	fset     *token.FileSet
	info     *types.Info
	findings []VetFinding
}

func (v *vetPass) report(check string, pos token.Pos, msg string) {
	v.findings = append(v.findings, VetFinding{Check: check, Pos: pos, Msg: msg})
}

// Format renders one finding like a vet diagnostic line:
// "unreachable: gen.go:12:3: unreachable code".
func (f VetFinding) Format(fset *token.FileSet) string {
	return fmt.Sprintf("%s: %s: %s", f.Check, fset.Position(f.Pos), f.Msg)
}

// ---------------------------------------------------------------------------
// unreachable — direct port of cmd/vet's unreachable pass
// (GOROOT cmd/vendor/golang.org/x/tools/go/analysis/passes/unreachable):
// a control-flow walk per function body tracking a `reachable` flag;
// labels/gotos/breaks are gathered in a findLabels pre-pass.
// ---------------------------------------------------------------------------

func (v *vetPass) checkUnreachable(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		var body *ast.BlockStmt
		switch n := n.(type) {
		case *ast.FuncDecl:
			body = n.Body
		case *ast.FuncLit:
			body = n.Body
		}
		if body == nil {
			return true
		}
		d := &deadState{
			v:        v,
			hasBreak: make(map[ast.Stmt]bool),
			hasGoto:  make(map[string]bool),
			labels:   make(map[string]ast.Stmt),
		}
		d.findLabels(body)
		d.reachable = true
		d.findDead(body)
		return true
	})
}

type deadState struct {
	v           *vetPass
	hasBreak    map[ast.Stmt]bool
	hasGoto     map[string]bool
	labels      map[string]ast.Stmt
	breakTarget ast.Stmt

	reachable bool
}

// findLabels gathers information about the labels defined and used by stmt
// and about which statements break, whether a label is involved or not.
func (d *deadState) findLabels(stmt ast.Stmt) {
	switch x := stmt.(type) {
	default:
		panic(fmt.Sprintf("govet unreachable: unexpected statement %T", x))

	case *ast.AssignStmt,
		*ast.BadStmt,
		*ast.DeclStmt,
		*ast.DeferStmt,
		*ast.EmptyStmt,
		*ast.ExprStmt,
		*ast.GoStmt,
		*ast.IncDecStmt,
		*ast.ReturnStmt,
		*ast.SendStmt:
		// no statements inside

	case *ast.BlockStmt:
		for _, stmt := range x.List {
			d.findLabels(stmt)
		}

	case *ast.BranchStmt:
		switch x.Tok {
		case token.GOTO:
			if x.Label != nil {
				d.hasGoto[x.Label.Name] = true
			}

		case token.BREAK:
			stmt := d.breakTarget
			if x.Label != nil {
				stmt = d.labels[x.Label.Name]
			}
			if stmt != nil {
				d.hasBreak[stmt] = true
			}
		}

	case *ast.IfStmt:
		d.findLabels(x.Body)
		if x.Else != nil {
			d.findLabels(x.Else)
		}

	case *ast.LabeledStmt:
		d.labels[x.Label.Name] = x.Stmt
		d.findLabels(x.Stmt)

	// These cases are all the same, but the x.Body only works
	// when the specific type of x is known, so the cases cannot
	// be merged.
	case *ast.ForStmt:
		outer := d.breakTarget
		d.breakTarget = x
		d.findLabels(x.Body)
		d.breakTarget = outer

	case *ast.RangeStmt:
		outer := d.breakTarget
		d.breakTarget = x
		d.findLabels(x.Body)
		d.breakTarget = outer

	case *ast.SelectStmt:
		outer := d.breakTarget
		d.breakTarget = x
		d.findLabels(x.Body)
		d.breakTarget = outer

	case *ast.SwitchStmt:
		outer := d.breakTarget
		d.breakTarget = x
		d.findLabels(x.Body)
		d.breakTarget = outer

	case *ast.TypeSwitchStmt:
		outer := d.breakTarget
		d.breakTarget = x
		d.findLabels(x.Body)
		d.breakTarget = outer

	case *ast.CommClause:
		for _, stmt := range x.Body {
			d.findLabels(stmt)
		}

	case *ast.CaseClause:
		for _, stmt := range x.Body {
			d.findLabels(stmt)
		}
	}
}

// findDead walks the statement looking for dead code.
// If d.reachable is false on entry, stmt itself is dead.
// When findDead returns, d.reachable tells whether the
// statement following stmt is reachable.
func (d *deadState) findDead(stmt ast.Stmt) {
	// Is this a labeled goto target?
	// If so, assume it is reachable due to the goto.
	if x, isLabel := stmt.(*ast.LabeledStmt); isLabel && d.hasGoto[x.Label.Name] {
		d.reachable = true
	}

	if !d.reachable {
		switch stmt.(type) {
		case *ast.EmptyStmt:
			// do not warn about unreachable empty statements
		default:
			d.v.report("unreachable", stmt.Pos(), "unreachable code")
			d.reachable = true // silence error about next statement
		}
	}

	switch x := stmt.(type) {
	default:
		panic(fmt.Sprintf("govet unreachable: unexpected statement %T", x))

	case *ast.AssignStmt,
		*ast.BadStmt,
		*ast.DeclStmt,
		*ast.DeferStmt,
		*ast.EmptyStmt,
		*ast.GoStmt,
		*ast.IncDecStmt,
		*ast.SendStmt:
		// no control flow

	case *ast.BlockStmt:
		for _, stmt := range x.List {
			d.findDead(stmt)
		}

	case *ast.BranchStmt:
		switch x.Tok {
		case token.BREAK, token.GOTO, token.FALLTHROUGH:
			d.reachable = false
		case token.CONTINUE:
			// NOTE: We accept "continue" statements as terminating.
			// They are not necessary in the spec definition of terminating,
			// because a continue statement cannot be the final statement
			// before a return. But for the more general problem of syntactically
			// identifying dead code, continue redirects control flow just
			// like the other terminating statements.
			d.reachable = false
		}

	case *ast.ExprStmt:
		// Call to panic?
		call, ok := x.X.(*ast.CallExpr)
		if ok {
			name, ok := call.Fun.(*ast.Ident)
			if ok && name.Name == "panic" && d.v.isPanicBuiltin(name) {
				d.reachable = false
			}
		}

	case *ast.ForStmt:
		d.findDead(x.Body)
		d.reachable = x.Cond != nil || d.hasBreak[x]

	case *ast.IfStmt:
		d.findDead(x.Body)
		if x.Else != nil {
			r := d.reachable
			d.reachable = true
			d.findDead(x.Else)
			d.reachable = d.reachable || r
		} else {
			// might not have executed if statement
			d.reachable = true
		}

	case *ast.LabeledStmt:
		d.findDead(x.Stmt)

	case *ast.RangeStmt:
		d.findDead(x.Body)
		d.reachable = true

	case *ast.ReturnStmt:
		d.reachable = false

	case *ast.SelectStmt:
		// NOTE: Unlike switch and type switch below, we don't care
		// whether a select has a default, because a select without a
		// default blocks until one of the cases can run. That's different
		// from a switch without a default, which behaves like it has
		// a default with an empty body.
		anyReachable := false
		for _, comm := range x.Body.List {
			d.reachable = true
			for _, stmt := range comm.(*ast.CommClause).Body {
				d.findDead(stmt)
			}
			anyReachable = anyReachable || d.reachable
		}
		d.reachable = anyReachable || d.hasBreak[x]

	case *ast.SwitchStmt:
		anyReachable := false
		hasDefault := false
		for _, cas := range x.Body.List {
			cc := cas.(*ast.CaseClause)
			if cc.List == nil {
				hasDefault = true
			}
			d.reachable = true
			for _, stmt := range cc.Body {
				d.findDead(stmt)
			}
			anyReachable = anyReachable || d.reachable
		}
		d.reachable = anyReachable || d.hasBreak[x] || !hasDefault

	case *ast.TypeSwitchStmt:
		anyReachable := false
		hasDefault := false
		for _, cas := range x.Body.List {
			cc := cas.(*ast.CaseClause)
			if cc.List == nil {
				hasDefault = true
			}
			d.reachable = true
			for _, stmt := range cc.Body {
				d.findDead(stmt)
			}
			anyReachable = anyReachable || d.reachable
		}
		d.reachable = anyReachable || d.hasBreak[x] || !hasDefault
	}
}

// isPanicBuiltin reports whether id refers to the panic builtin —
// via types.Info when available, else via parser object resolution
// (an ident with nil Obj is not declared in the file, so it is the
// builtin; a declared `panic` marks the call as non-terminating,
// matching upstream).
func (v *vetPass) isPanicBuiltin(id *ast.Ident) bool {
	if v.info != nil {
		_, ok := v.info.Uses[id].(*types.Builtin)
		return ok
	}
	return id.Obj == nil
}

// ---------------------------------------------------------------------------
// assign — self-assignment, port of cmd/vet's assign pass (diagnostic
// only; the suggested-fix machinery is dropped).
// ---------------------------------------------------------------------------

func (v *vetPass) checkAssign(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		stmt, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if stmt.Tok != token.ASSIGN {
			return true // ignore :=
		}
		if len(stmt.Lhs) != len(stmt.Rhs) {
			return true
		}
		var exprs []string
		for i, lhs := range stmt.Lhs {
			rhs := stmt.Rhs[i]
			if noEffects(v.info, lhs) &&
				noEffects(v.info, rhs) &&
				!isMapIndex(v.info, lhs) &&
				reflect.TypeOf(lhs) == reflect.TypeOf(rhs) {
				le := formatNode(v.fset, lhs)
				re := formatNode(v.fset, rhs)
				if le == re {
					exprs = append(exprs, le)
				}
			}
		}
		if len(exprs) != 0 {
			v.report("assign", stmt.Pos(), "self-assignment of "+strings.Join(exprs, ", "))
		}
		return true
	})
}

// isMapIndex returns true if e is a map index expression.
func isMapIndex(info *types.Info, e ast.Expr) bool {
	if idx, ok := ast.Unparen(e).(*ast.IndexExpr); ok {
		if typ := info.Types[idx.X].Type; typ != nil {
			_, ok := typ.Underlying().(*types.Map)
			return ok
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// unusedresult — port of cmd/vet's unusedresult pass: results of a
// whitelisted set of calls (and of parameterless Error/String
// methods) must not be discarded. `ignore e` legitimately emits bare
// expression statements, so only vet's own whitelist applies.
// ---------------------------------------------------------------------------

// unusedResultFuncs mirrors the upstream funcs list: package path +
// function name of calls whose results must be used.
var unusedResultFuncs = map[[2]string]bool{
	{"context", "WithCancel"}:      true,
	{"context", "WithDeadline"}:    true,
	{"context", "WithTimeout"}:     true,
	{"context", "WithValue"}:       true,
	{"errors", "New"}:              true,
	{"fmt", "Append"}:              true,
	{"fmt", "Appendf"}:             true,
	{"fmt", "Appendln"}:            true,
	{"fmt", "Errorf"}:              true,
	{"fmt", "Sprint"}:              true,
	{"fmt", "Sprintf"}:             true,
	{"fmt", "Sprintln"}:            true,
	{"maps", "All"}:                true,
	{"maps", "Clone"}:              true,
	{"maps", "Collect"}:            true,
	{"maps", "Equal"}:              true,
	{"maps", "EqualFunc"}:          true,
	{"maps", "Keys"}:               true,
	{"maps", "Values"}:             true,
	{"slices", "All"}:              true,
	{"slices", "AppendSeq"}:        true,
	{"slices", "Backward"}:         true,
	{"slices", "BinarySearch"}:     true,
	{"slices", "BinarySearchFunc"}: true,
	{"slices", "Chunk"}:            true,
	{"slices", "Clip"}:             true,
	{"slices", "Clone"}:            true,
	{"slices", "Collect"}:          true,
	{"slices", "Compact"}:          true,
	{"slices", "CompactFunc"}:      true,
	{"slices", "Compare"}:          true,
	{"slices", "CompareFunc"}:      true,
	{"slices", "Concat"}:           true,
	{"slices", "Contains"}:         true,
	{"slices", "ContainsFunc"}:     true,
	{"slices", "Delete"}:           true,
	{"slices", "DeleteFunc"}:       true,
	{"slices", "Equal"}:            true,
	{"slices", "EqualFunc"}:        true,
	{"slices", "Grow"}:             true,
	{"slices", "Index"}:            true,
	{"slices", "IndexFunc"}:        true,
	{"slices", "Insert"}:           true,
	{"slices", "IsSorted"}:         true,
	{"slices", "IsSortedFunc"}:     true,
	{"slices", "Max"}:              true,
	{"slices", "MaxFunc"}:          true,
	{"slices", "Min"}:              true,
	{"slices", "MinFunc"}:          true,
	{"slices", "Repeat"}:           true,
	{"slices", "Replace"}:          true,
	{"slices", "Sorted"}:           true,
	{"slices", "SortedFunc"}:       true,
	{"slices", "SortedStableFunc"}: true,
	{"slices", "Values"}:           true,
	{"sort", "Reverse"}:            true,
}

// unusedResultStringMethods mirrors the upstream stringmethods list:
// names of methods of type func() string whose results must be used.
var unusedResultStringMethods = map[string]bool{"Error": true, "String": true}

var sigNoArgsStringResult = types.NewSignatureType(nil, nil, nil, nil,
	types.NewTuple(types.NewParam(token.NoPos, nil, "", types.Typ[types.String])), false)

func (v *vetPass) checkUnusedResult(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return true
		}
		call, ok := ast.Unparen(stmt.X).(*ast.CallExpr)
		if !ok {
			return true // not a call statement
		}
		// Call to function or method?
		fn := calleeOf(v.info, call)
		if fn == nil {
			return true // e.g. var or builtin
		}
		if sig := fn.Signature(); sig.Recv() != nil {
			// method (e.g. foo.String())
			if types.Identical(sig, sigNoArgsStringResult) && unusedResultStringMethods[fn.Name()] {
				v.report("unusedresult", call.Pos(), fmt.Sprintf(
					"result of (%s).%s call not used", sig.Recv().Type(), fn.Name()))
			}
		} else {
			// package-level function (e.g. fmt.Errorf)
			if pkg := fn.Pkg(); pkg != nil &&
				unusedResultFuncs[[2]string{pkg.Path(), fn.Name()}] {
				v.report("unusedresult", call.Pos(), fmt.Sprintf(
					"result of %s.%s call not used", pkg.Path(), fn.Name()))
			}
		}
		return true
	})
}

// calleeOf is the essential part of go/types/typeutil.Callee: the
// *types.Func a call resolves to, through instantiation indices.
func calleeOf(info *types.Info, call *ast.CallExpr) *types.Func {
	f := ast.Unparen(call.Fun)
	// Unwrap explicit instantiations: F[T](...) or pkg.F[T](...).
	for {
		switch ix := f.(type) {
		case *ast.IndexExpr:
			f = ast.Unparen(ix.X)
		case *ast.IndexListExpr:
			f = ast.Unparen(ix.X)
		default:
			goto unwrapped
		}
	}
unwrapped:
	var obj types.Object
	switch f := f.(type) {
	case *ast.Ident:
		obj = info.Uses[f]
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[f]; ok {
			obj = sel.Obj()
		} else {
			obj = info.Uses[f.Sel] // qualified identifier pkg.Fn
		}
	}
	fn, _ := obj.(*types.Func)
	return fn
}

// ---------------------------------------------------------------------------
// stringintconv — simplified port of cmd/vet's stringintconv pass:
// T(x) where T's underlying is string and x's type is a non-byte/rune
// integer reports. The upstream's type-set machinery handles generic
// conversions; here a non-Basic underlying (type parameters included)
// just declines to judge, and the suggested fixes are dropped.
// ---------------------------------------------------------------------------

func (v *vetPass) checkStringIntConv(f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		arg := call.Args[0]

		// Target type T must have underlying string.
		var tname *types.TypeName
		switch fun := ast.Unparen(call.Fun).(type) {
		case *ast.Ident:
			tname, _ = v.info.Uses[fun].(*types.TypeName)
		case *ast.SelectorExpr:
			if _, ok := v.info.Selections[fun]; !ok {
				tname, _ = v.info.Uses[fun.Sel].(*types.TypeName)
			}
		}
		if tname == nil {
			return true
		}
		tu, _ := tname.Type().Underlying().(*types.Basic)
		if tu == nil || tu.Kind() != types.String {
			return true
		}

		// Source type V must be a non-byte/rune integer — including
		// named integer types via their underlying. Anything else
		// (type parameters whose underlying is an interface included)
		// is not judged.
		V := v.info.TypeOf(arg)
		if V == nil {
			return true
		}
		vu, _ := V.Underlying().(*types.Basic)
		if vu == nil || vu.Info()&types.IsInteger == 0 {
			return true
		}
		switch vu.Kind() {
		case types.Byte, types.Rune, types.UntypedRune:
			return true
		}

		v.report("stringintconv", call.Pos(), fmt.Sprintf(
			"conversion from %s to %s yields a string of one rune, not a string of digits",
			V, tname.Type()))
		return true
	})
}

// ---------------------------------------------------------------------------
// shadow — stricter than vet's (non-default) shadow pass: generated
// identifiers are uniquely renamed, so ANY local declaration whose
// name is already bound in an enclosing function-local scope is a
// renamer defect. Same-scope redeclarations (`v, err :=` then
// `v2, err :=`) are legal Go and do NOT report. Function literals
// share their enclosing scope chain — a literal's parameter shadowing
// an outer local still reports.
// ---------------------------------------------------------------------------

type shadowScope struct {
	names  map[string]token.Pos
	parent *shadowScope
}

func (s *shadowScope) declare(name string, pos token.Pos) (shadowed token.Pos, ok bool) {
	for p := s.parent; p != nil; p = p.parent {
		if prev, found := p.names[name]; found {
			return prev, true
		}
	}
	return 0, false
}

func (v *vetPass) checkShadow(f *ast.File) {
	w := &shadowWalk{v: v}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		s := &shadowScope{names: map[string]token.Pos{}}
		w.declareFields(s, fd.Recv)
		if fd.Type.TypeParams != nil {
			w.declareFields(s, fd.Type.TypeParams)
		}
		w.declareFields(s, fd.Type.Params)
		w.declareFields(s, fd.Type.Results)
		// Per the Go spec, receiver/parameter/result scope IS the
		// function body — the outermost block shares this scope
		// (a := collision there is a same-scope redeclare error,
		// not a shadow — and go/types has already rejected it).
		w.stmts(s, fd.Body.List)
	}
}

type shadowWalk struct {
	v *vetPass
}

func (w *shadowWalk) declare(s *shadowScope, id *ast.Ident) {
	if id == nil || id.Name == "_" {
		return
	}
	if prev, ok := s.declare(id.Name, id.Pos()); ok {
		w.v.report("shadow", id.Pos(), fmt.Sprintf(
			"declaration of %q shadows declaration at %s",
			id.Name, w.v.fset.Position(prev)))
		return
	}
	s.names[id.Name] = id.Pos()
}

func (w *shadowWalk) declareFields(s *shadowScope, fl *ast.FieldList) {
	if fl == nil {
		return
	}
	for _, field := range fl.List {
		for _, name := range field.Names {
			w.declare(s, name)
		}
	}
}

// expr walks an expression for declarations inside function literals.
// A FuncLit opens a scope sharing the enclosing chain — closures can
// shadow outer locals.
func (w *shadowWalk) expr(s *shadowScope, e ast.Expr) {
	if e == nil {
		return
	}
	ast.Inspect(e, func(n ast.Node) bool {
		fl, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		w.funcLit(s, fl)
		return false // nested lits are walked via funcLit's body
	})
}

func (w *shadowWalk) funcLit(outer *shadowScope, fl *ast.FuncLit) {
	s := &shadowScope{names: map[string]token.Pos{}, parent: outer}
	if fl.Type.TypeParams != nil {
		w.declareFields(s, fl.Type.TypeParams)
	}
	w.declareFields(s, fl.Type.Params)
	w.declareFields(s, fl.Type.Results)
	w.stmts(s, fl.Body.List)
}

func (w *shadowWalk) stmts(s *shadowScope, list []ast.Stmt) {
	for _, st := range list {
		w.stmt(s, st)
	}
}

func (w *shadowWalk) stmt(s *shadowScope, st ast.Stmt) {
	if st == nil {
		return
	}
	switch x := st.(type) {
	default:
		panic(fmt.Sprintf("govet shadow: unexpected statement %T", x))

	case *ast.BadStmt, *ast.EmptyStmt, *ast.BranchStmt:
		// no declarations, no expressions

	case *ast.AssignStmt:
		for _, e := range x.Rhs {
			w.expr(s, e)
		}
		for _, e := range x.Lhs {
			w.expr(s, e)
		}
		if x.Tok == token.DEFINE {
			// The scope of a short variable begins at the end
			// of the declaration, so rhs sees only outer names.
			for _, e := range x.Lhs {
				if id, ok := e.(*ast.Ident); ok {
					w.declare(s, id)
				}
			}
		}

	case *ast.DeclStmt:
		if gd, ok := x.Decl.(*ast.GenDecl); ok {
			for _, spec := range gd.Specs {
				switch spec := spec.(type) {
				case *ast.ValueSpec:
					for _, e := range spec.Values {
						w.expr(s, e)
					}
					for _, name := range spec.Names {
						w.declare(s, name)
					}
				case *ast.TypeSpec:
					w.expr(s, spec.Type)
					w.declare(s, spec.Name)
				}
			}
		}

	case *ast.ExprStmt:
		w.expr(s, x.X)

	case *ast.SendStmt:
		w.expr(s, x.Chan)
		w.expr(s, x.Value)

	case *ast.IncDecStmt:
		w.expr(s, x.X)

	case *ast.ReturnStmt:
		for _, e := range x.Results {
			w.expr(s, e)
		}

	case *ast.GoStmt:
		w.expr(s, x.Call)

	case *ast.DeferStmt:
		w.expr(s, x.Call)

	case *ast.LabeledStmt:
		w.stmt(s, x.Stmt)

	case *ast.BlockStmt:
		w.stmts(&shadowScope{names: map[string]token.Pos{}, parent: s}, x.List)

	case *ast.IfStmt:
		s2 := &shadowScope{names: map[string]token.Pos{}, parent: s}
		w.stmt(s2, x.Init)
		w.expr(s2, x.Cond)
		w.stmt(s2, x.Body)
		if x.Else != nil {
			w.stmt(s2, x.Else)
		}

	case *ast.ForStmt:
		s2 := &shadowScope{names: map[string]token.Pos{}, parent: s}
		w.stmt(s2, x.Init)
		w.expr(s2, x.Cond)
		w.stmt(s2, x.Post)
		w.stmt(s2, x.Body)

	case *ast.RangeStmt:
		w.expr(s, x.X) // the range expression sees the outer scope
		s2 := &shadowScope{names: map[string]token.Pos{}, parent: s}
		if x.Tok == token.DEFINE {
			if id, ok := x.Key.(*ast.Ident); ok {
				w.declare(s2, id)
			}
			if id, ok := x.Value.(*ast.Ident); ok {
				w.declare(s2, id)
			}
		} else {
			w.expr(s, x.Key)
			w.expr(s, x.Value)
		}
		w.stmt(s2, x.Body)

	case *ast.SwitchStmt:
		s2 := &shadowScope{names: map[string]token.Pos{}, parent: s}
		w.stmt(s2, x.Init)
		w.expr(s2, x.Tag)
		for _, c := range x.Body.List {
			cc := c.(*ast.CaseClause)
			for _, e := range cc.List {
				w.expr(s2, e)
			}
			w.stmts(&shadowScope{names: map[string]token.Pos{}, parent: s2}, cc.Body)
		}

	case *ast.TypeSwitchStmt:
		s2 := &shadowScope{names: map[string]token.Pos{}, parent: s}
		w.stmt(s2, x.Init)
		// The guard `v := x.(type)` binds one variable per clause;
		// declaring it once in the switch scope is equivalent for
		// shadow detection (an inner `v` still reports; the per-clause
		// copies never collide with each other).
		if as, ok := x.Assign.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
			for _, e := range as.Rhs {
				w.expr(s2, e)
			}
			for _, e := range as.Lhs {
				if id, ok := e.(*ast.Ident); ok {
					w.declare(s2, id)
				}
			}
		} else {
			w.stmt(s2, x.Assign)
		}
		for _, c := range x.Body.List {
			cc := c.(*ast.CaseClause)
			for _, e := range cc.List {
				w.expr(s2, e)
			}
			w.stmts(&shadowScope{names: map[string]token.Pos{}, parent: s2}, cc.Body)
		}

	case *ast.SelectStmt:
		for _, c := range x.Body.List {
			cc := c.(*ast.CommClause)
			c2 := &shadowScope{names: map[string]token.Pos{}, parent: s}
			if cc.Comm != nil {
				w.stmt(c2, cc.Comm)
			}
			w.stmts(c2, cc.Body)
		}
	}
}

// ---------------------------------------------------------------------------
// shared helpers — ports of x/tools/internal/typesinternal utilities.
// ---------------------------------------------------------------------------

// noEffects reports whether the expression has no side effects, i.e., it
// does not modify the memory state. Conservative: it may return false
// even when the expression has no effect. Ported from
// cmd/vendor/golang.org/x/tools/internal/typesinternal/fx.go.
func noEffects(info *types.Info, expr ast.Expr) bool {
	noEffects := true
	ast.Inspect(expr, func(n ast.Node) bool {
		switch v := n.(type) {
		case nil, *ast.Ident, *ast.BasicLit, *ast.BinaryExpr, *ast.ParenExpr,
			*ast.SelectorExpr, *ast.IndexExpr, *ast.SliceExpr, *ast.TypeAssertExpr,
			*ast.StarExpr, *ast.CompositeLit,
			// non-expressions that may appear within expressions
			*ast.KeyValueExpr,
			*ast.FieldList,
			*ast.Field,
			*ast.Ellipsis,
			*ast.IndexListExpr:
			// No effect.

		case *ast.ArrayType,
			*ast.StructType,
			*ast.ChanType,
			*ast.FuncType,
			*ast.MapType,
			*ast.InterfaceType:
			// Type syntax: no effects, recursively.
			// Prune descent.
			return false

		case *ast.UnaryExpr:
			// Channel send <-ch has effects.
			if v.Op == token.ARROW {
				noEffects = false
			}

		case *ast.CallExpr:
			// Type conversion has no effects.
			if !info.Types[v.Fun].IsType() {
				if callsPureBuiltin(info, v) {
					// A call such as len(e) has no effects of its
					// own, though the subexpression e might.
				} else {
					noEffects = false
				}
			}

		case *ast.FuncLit:
			// A FuncLit has no effects, but do not descend into it.
			return false

		default:
			// All other expressions have effects
			noEffects = false
		}

		return noEffects
	})
	return noEffects
}

// callsPureBuiltin reports whether call is a call of a built-in
// function that is a pure computation over its operands.
func callsPureBuiltin(info *types.Info, call *ast.CallExpr) bool {
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		if b, ok := info.ObjectOf(id).(*types.Builtin); ok {
			switch b.Name() {
			case "len", "cap", "complex", "imag", "real", "make", "new", "max", "min":
				return true
			}
			// Not: append clear close copy delete panic print println recover
		}
	}
	return false
}

// formatNode renders an expression for syntactic-equality comparison
// (the role gofmt's output plays in the upstream analyzers).
func formatNode(fset *token.FileSet, n ast.Node) string {
	var sb strings.Builder
	if err := printer.Fprint(&sb, fset, n); err != nil {
		return fmt.Sprintf("%T@%d", n, n.Pos())
	}
	return sb.String()
}
