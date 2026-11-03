package lower

import (
	"fmt"
	"slices"
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
)

// codeInitCycle reports a package-level initialization cycle the
// wrapper lowering could not order — E0907 (the lowering stage's
// next free user-facing code after E0904–E0906).
const codeInitCycle diag.Code = "E0907"

// checkInitCycles applies Go's package-initialization dependency
// rule to the lowered Core file, so a program `go build` would
// reject is reported here — naming the user's bindings instead of
// the generated temporaries. A package variable depends on every
// variable referenced by its initializer, with references followed
// transitively through the bodies of functions (and function
// literals) the initializer mentions; a function-to-function edge
// is not a dependency. A variable-to-variable cycle that remains —
// including a self-loop through a function literal — has no valid
// initialization order (docs/decisions/2026-11-05-aggregate-function-bindings.md).
//
// The graph is built on the emitted Core IR for the same reason
// @tailrec's R4 runs post-lowering: the dependency set is exactly
// the one codegen hands Go, so the diagnostic cannot drift from the
// generated program. The pass sees only `out.Decls` — generated
// comparators and display helpers attach to the File afterwards as
// EqDecls/ShowDecls, not as declarations — and is per-file like R4;
// a multi-file package would have to build the graph over every
// file's Core to match Go's per-package analysis.
func (l *lowerer) checkInitCycles(out *core.File) {
	funcs := map[*resolve.Symbol]bool{}
	vars := map[*resolve.Symbol]*core.VarDecl{}
	var order []*resolve.Symbol
	for _, d := range out.Decls {
		switch d := d.(type) {
		case *core.FuncDecl:
			if d.Sym != nil {
				funcs[d.Sym] = true
			}
		case *core.VarDecl:
			if d.Sym != nil {
				vars[d.Sym] = d
				order = append(order, d.Sym)
			}
		case *core.TestDecl, *core.TypeDecl:
			// Test bodies run at test time, not package init;
			// type declarations carry no initializer.
		default:
			panic("unreachable")
		}
	}
	if len(vars) == 0 {
		return
	}
	// refset[x] is the set of package-level declarations x's body
	// references — Var and FuncRef atoms plus CallDirect callees.
	// Nested Closure and Spawn bodies count: Go reads the
	// initializer's references lexically, so `var x = func() { y() }`
	// still depends on y.
	refset := map[*resolve.Symbol]map[*resolve.Symbol]bool{}
	for _, d := range out.Decls {
		var from *resolve.Symbol
		var b *core.Body
		switch d := d.(type) {
		case *core.FuncDecl:
			if d.Sym == nil || d.Fn == nil {
				continue
			}
			from, b = d.Sym, d.Fn.Body
		case *core.VarDecl:
			if d.Sym == nil || d.Body == nil {
				continue
			}
			from, b = d.Sym, d.Body
		case *core.TestDecl, *core.TypeDecl:
			continue
		default:
			panic("unreachable")
		}
		walkTailrec(b, func(e core.Expr, _, _ bool) {
			var to *resolve.Symbol
			switch e := e.(type) {
			case *core.Var:
				to = e.Sym
			case *core.FuncRef:
				to = e.Sym
			case *core.CallDirect:
				to = e.Callee
			case *core.Binary, *core.CallBuiltin, *core.CallFunc,
				*core.CallGo, *core.Closure, *core.Conv, *core.CtorApp,
				*core.CtorRef, *core.Equal, *core.EscapeCall,
				*core.EscapeResult, *core.FieldGet, *core.ForRange,
				*core.GoFuncRef, *core.If, *core.ListNew, *core.Lit,
				*core.Match, *core.OptionFromGo, *core.RecordNew,
				*core.RecordUpdate, *core.ResultFromGo, *core.SelectExpr,
				*core.Spawn, *core.TailCall, *core.TupleFromGo,
				*core.TupleNew, *core.Unary:
				// Only direct symbol references join the init
				// dependency set; every other expression kind
				// contributes through walkTailrec's traversal.
			default:
				panic("unreachable")
			}
			if to != nil && (funcs[to] || vars[to] != nil) {
				if refset[from] == nil {
					refset[from] = map[*resolve.Symbol]bool{}
				}
				refset[from][to] = true
			}
		})
	}
	// vardep[v] is the set of package variables v's initialization
	// must wait for — every variable reachable from v over refset
	// paths that pass only through functions. A var neighbour is a
	// dependency in itself; it is not traversed further.
	vardep := map[*resolve.Symbol]map[*resolve.Symbol]bool{}
	for v := range vars {
		deps := map[*resolve.Symbol]bool{}
		seen := map[*resolve.Symbol]bool{v: true}
		var rec func(n *resolve.Symbol)
		rec = func(n *resolve.Symbol) {
			for _, h := range tailrecNeighbors(refset[n]) {
				switch {
				case vars[h] != nil:
					deps[h] = true
				case funcs[h]:
					if !seen[h] {
						seen[h] = true
						rec(h)
					}
				}
			}
		}
		rec(v)
		vardep[v] = deps
	}
	// A variable is on a cycle when it reaches itself through vardep.
	// Each gets one diagnostic, the way `go build` reports one per
	// variable on the cycle; the witness path is reconstructed on the
	// raw refset so the "refers to" chain names every step.
	for _, v := range order {
		if !reachesVar(vardep, v, v) {
			continue
		}
		path := cyclePath(refset, v)
		names := make([]string, len(path))
		for i, n := range path {
			names[i] = "`" + l.nodeName(n) + "`"
		}
		l.diags = append(l.diags, diag.Error(codeInitCycle,
			fmt.Sprintf("initialization cycle for `%s`: %s",
				l.nodeName(v), strings.Join(names, " refers to "))).
			At(vars[v].Sp).
			Set("var", l.nodeName(v)).
			Set("path", pathNames(path, l)))
	}
}

// reachesVar reports whether `to` is reachable from `from` through
// the variable-dependency graph — `from` itself included when a
// cycle routes back to it.
func reachesVar(vardep map[*resolve.Symbol]map[*resolve.Symbol]bool, from, to *resolve.Symbol) bool {
	seen := map[*resolve.Symbol]bool{from: true}
	queue := []*resolve.Symbol{from}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, h := range tailrecNeighbors(vardep[n]) {
			if h == to {
				return true
			}
			if !seen[h] {
				seen[h] = true
				queue = append(queue, h)
			}
		}
	}
	return false
}

// cyclePath returns a witness v → … → v through the raw reference
// graph — the shortest path that leaves v and comes back. The
// caller established that one exists.
func cyclePath(refset map[*resolve.Symbol]map[*resolve.Symbol]bool, v *resolve.Symbol) []*resolve.Symbol {
	prev := map[*resolve.Symbol]*resolve.Symbol{v: nil}
	seen := map[*resolve.Symbol]bool{v: true}
	queue := []*resolve.Symbol{v}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, h := range tailrecNeighbors(refset[n]) {
			if h == v {
				var rev []*resolve.Symbol
				for p := n; p != nil; p = prev[p] {
					rev = append(rev, p)
				}
				slices.Reverse(rev) // v → … → n
				return append(rev, v)
			}
			if !seen[h] {
				seen[h] = true
				prev[h] = n
				queue = append(queue, h)
			}
		}
	}
	panic("unreachable") // reachesVar guaranteed a cycle back to v
}

// pathNames renders a witness path as node names for diagnostic data.
func pathNames(path []*resolve.Symbol, l *lowerer) []string {
	out := make([]string, len(path))
	for i, n := range path {
		out[i] = l.nodeName(n)
	}
	return out
}

// nodeName renders a graph node for diagnostics. Hidden temporaries
// minted for a top-level `let` are named by the pattern they carry —
// `let (again, _)` — so the refers-to chain stays in the user's
// vocabulary. Every hidden `VarDecl` lowering mints must register in
// `tmpLets`, or the fallback prints the generated `tmpN` name.
func (l *lowerer) nodeName(sym *resolve.Symbol) string {
	if d, ok := l.tmpLets[sym]; ok {
		return "let " + patText(d.Pat)
	}
	return sym.Name
}

// patText renders a pattern compactly for diagnostic node names.
func patText(p check.Pat) string {
	switch p := p.(type) {
	case *check.WildPat:
		return "_"
	case *check.VarPat:
		if p.Sym != nil {
			return p.Sym.Name
		}
		return "_"
	case *check.LitPat:
		return p.Text
	case *check.CtorPat:
		name := "?"
		if p.Ctor != nil {
			name = p.Ctor.Name
		}
		var b strings.Builder
		b.WriteString(name)
		for _, a := range p.Args {
			b.WriteString(" ")
			if a.Label != "" {
				b.WriteString("~" + a.Label + ": ")
			}
			b.WriteString(patText(a.Pat))
		}
		if p.Fields != nil {
			var fs []string
			for _, f := range p.Fields {
				fs = append(fs, f.Name+" = "+patText(f.Pat))
			}
			b.WriteString(" {" + strings.Join(fs, ", ") + "}")
		}
		return b.String()
	case *check.TuplePat:
		var es []string
		for _, e := range p.Elems {
			es = append(es, patText(e))
		}
		return "(" + strings.Join(es, ", ") + ")"
	case *check.RecordPat:
		var fs []string
		for _, f := range p.Fields {
			fs = append(fs, f.Name+" = "+patText(f.Pat))
		}
		if p.Rest {
			fs = append(fs, "..")
		}
		return "{" + strings.Join(fs, ", ") + "}"
	case *check.ListPat:
		var es []string
		for _, e := range p.Elems {
			es = append(es, patText(e))
		}
		if p.Rest != nil {
			es = append(es, ".."+patText(p.Rest))
		}
		return "[" + strings.Join(es, ", ") + "]"
	case *check.OrPat:
		var alts []string
		for _, a := range p.Alts {
			alts = append(alts, patText(a))
		}
		return strings.Join(alts, " | ")
	case *check.AsPat:
		name := "_"
		if p.Sym != nil {
			name = p.Sym.Name
		}
		return patText(p.Pat) + " as " + name
	default:
		panic("unreachable")
	}
}
