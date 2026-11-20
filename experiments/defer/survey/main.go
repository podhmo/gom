// Command survey classifies every `defer` statement under the given
// roots by where it sits (function top level, inside if / loop /
// switch / select) and by what it defers (a method like Close/Unlock,
// a closure that writes or reads named results, recover, ...).
//
//	go run ./survey [-tests] [-samples N] ROOT...
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type site struct {
	pos      string
	place    string   // top | if | loop | switch | select | block
	kind     string   // what is deferred
	tags     []string // extra facts
	funcLit  bool     // enclosing function is a func literal
	namedRes bool     // enclosing function has named results
}

type stats struct {
	sites      []site
	funcs      int // functions (decl+lit) that contain >=1 direct defer
	multiDefer int // functions with >=2 direct defers
	files      int
}

func main() {
	tests := flag.Bool("tests", false, "scan _test.go files instead of non-test files")
	samples := flag.Int("samples", 3, "samples printed per category")
	seed := flag.Int64("seed", 1, "sample seed")
	flag.Parse()
	st := &stats{}
	for _, root := range flag.Args() {
		if err := scanRoot(st, root, *tests); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	report(os.Stdout, st, *samples, rand.New(rand.NewSource(*seed)))
}

func scanRoot(st *stats, root string, tests bool) error {
	fset := token.NewFileSet()
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") != tests {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil
		}
		st.files++
		scanFile(st, fset, f)
		return nil
	})
}

func scanFile(st *stats, fset *token.FileSet, f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			if fn.Body != nil {
				scanFunc(st, fset, fn.Type, fn.Body, false)
			}
		case *ast.FuncLit:
			scanFunc(st, fset, fn.Type, fn.Body, true)
		}
		return true
	})
}

// scanFunc visits the defers that belong to this function (not to
// nested func literals, which scanFile visits on their own).
func scanFunc(st *stats, fset *token.FileSet, ft *ast.FuncType, body *ast.BlockStmt, lit bool) {
	named := map[string]bool{}
	if ft.Results != nil {
		for _, fld := range ft.Results.List {
			for _, nm := range fld.Names {
				if nm.Name != "_" {
					named[nm.Name] = true
				}
			}
		}
	}
	count := 0
	ifShape := map[*ast.BlockStmt]string{}
	var walk func(n ast.Node, place []string)
	walk = func(n ast.Node, place []string) {
		ast.Inspect(n, func(c ast.Node) bool {
			if c == n {
				return true
			}
			switch c := c.(type) {
			case *ast.FuncLit:
				return false
			case *ast.DeferStmt:
				count++
				s := classify(fset, c, place, named, lit)
				if b, ok := n.(*ast.BlockStmt); ok && ifShape[b] != "" {
					// Inside an if branch: is the resource acquired in
					// the same branch (stmts before the defer), or does
					// the branch only guard the registration?
					if b.List[0] == ast.Stmt(c) {
						s.tags = append(s.tags, "if:guard-only")
					} else {
						s.tags = append(s.tags, "if:acquire-in-branch")
					}
					// Does the cleanup outlive the branch? If the branch
					// ends in return/panic the lifetime is the branch
					// itself; otherwise it extends over the code after
					// the if, up to the function's exit.
					if terminates(b.List[len(b.List)-1]) {
						s.tags = append(s.tags, "if:branch-returns")
					} else {
						s.tags = append(s.tags, "if:outlives-branch")
					}
				}
				st.sites = append(st.sites, s)
				return false
			case *ast.IfStmt:
				ifShape[c.Body] = "if-then"
				walk(c.Body, append(place, "if"))
				if c.Else != nil {
					if b, ok := c.Else.(*ast.BlockStmt); ok {
						ifShape[b] = "if-else"
					}
					walk(c.Else, append(place, "if"))
				}
				return false
			case *ast.ForStmt:
				walk(c.Body, append(place, "loop"))
				return false
			case *ast.RangeStmt:
				walk(c.Body, append(place, "loop"))
				return false
			case *ast.SwitchStmt:
				walk(c.Body, append(place, "switch"))
				return false
			case *ast.TypeSwitchStmt:
				walk(c.Body, append(place, "switch"))
				return false
			case *ast.SelectStmt:
				walk(c.Body, append(place, "select"))
				return false
			case *ast.BlockStmt:
				walk(c, append(place, "block"))
				return false
			}
			return true
		})
	}
	walk(body, nil)
	if count > 0 {
		st.funcs++
	}
	if count > 1 {
		st.multiDefer++
	}
}

func classify(fset *token.FileSet, d *ast.DeferStmt, place []string, named map[string]bool, lit bool) site {
	s := site{pos: fset.Position(d.Pos()).String(), funcLit: lit, namedRes: len(named) > 0}
	s.place = placeOf(place)
	call := d.Call
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.FuncLit:
		s.kind = "closure"
		writes, reads, rec := closureFacts(fun.Body, named)
		if rec {
			s.tags = append(s.tags, "recover")
		}
		if writes {
			s.tags = append(s.tags, "writes-named-result")
		} else if reads {
			s.tags = append(s.tags, "reads-named-result")
		}
	case *ast.SelectorExpr:
		s.kind = "method:" + normMethod(fun.Sel.Name)
	case *ast.Ident:
		s.kind = "func:" + normFunc(fun.Name)
	case *ast.CallExpr:
		s.kind = "call-result" // defer f(x)() — e.g. defer trace("x")()
	default:
		s.kind = "other"
	}
	for _, a := range call.Args {
		if _, ok := ast.Unparen(a).(*ast.CallExpr); ok {
			s.tags = append(s.tags, "arg-call") // defer un(trace("x"))
			break
		}
	}
	return s
}

func placeOf(p []string) string {
	// The outermost control structure decides the category: a defer
	// in a block inside a loop is "loop".
	best := "top"
	for _, x := range p {
		switch x {
		case "loop":
			return "loop"
		case "if", "switch", "select":
			if best == "top" || best == "block" {
				best = x
			}
		case "block":
			if best == "top" {
				best = "block"
			}
		}
	}
	return best
}

func closureFacts(body *ast.BlockStmt, named map[string]bool) (writes, reads, rec bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "recover" {
				rec = true
			}
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				return true
			}
			for _, l := range n.Lhs {
				if id, ok := ast.Unparen(l).(*ast.Ident); ok && named[id.Name] {
					writes = true
				}
			}
		case *ast.Ident:
			if named[n.Name] {
				reads = true
			}
		}
		return true
	})
	return
}

func normMethod(m string) string {
	switch m {
	case "Close", "Unlock", "RUnlock", "Done", "Stop", "Wait", "Rollback", "Release", "Cancel", "Flush", "Remove", "RemoveAll", "Restore", "Shutdown", "End", "Finish", "Put":
		return m
	}
	return "other"
}

func normFunc(f string) string {
	switch f {
	case "close", "cancel", "recover", "panic":
		return f
	}
	if strings.HasPrefix(f, "cancel") || strings.HasSuffix(f, "Cancel") {
		return "cancel"
	}
	if strings.HasPrefix(f, "un") || strings.HasSuffix(f, "Unlock") || strings.HasPrefix(f, "restore") || strings.HasPrefix(f, "cleanup") {
		return "undo-like"
	}
	return "other"
}

func report(w *os.File, st *stats, nsamples int, r *rand.Rand) {
	total := len(st.sites)
	fmt.Fprintf(w, "files=%d defers=%d funcs-with-defer=%d funcs-with->=2-defers=%d\n\n", st.files, total, st.funcs, st.multiDefer)
	pct := func(n int) string { return fmt.Sprintf("%5d (%4.1f%%)", n, 100*float64(n)/float64(max(total, 1))) }
	by := func(key func(site) []string) map[string][]site {
		m := map[string][]site{}
		for _, s := range st.sites {
			for _, k := range key(s) {
				m[k] = append(m[k], s)
			}
		}
		return m
	}
	section := func(title string, m map[string][]site) {
		fmt.Fprintf(w, "## %s\n", title)
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return len(m[keys[i]]) > len(m[keys[j]]) || len(m[keys[i]]) == len(m[keys[j]]) && keys[i] < keys[j]
		})
		for _, k := range keys {
			fmt.Fprintf(w, "%-28s %s\n", k, pct(len(m[k])))
			ss := m[k]
			for _, i := range r.Perm(len(ss))[:min(nsamples, len(ss))] {
				fmt.Fprintf(w, "    %s\n", ss[i].pos)
			}
		}
		fmt.Fprintln(w)
	}
	section("place", by(func(s site) []string { return []string{s.place} }))
	section("kind", by(func(s site) []string { return []string{s.kind} }))
	section("tags", by(func(s site) []string { return s.tags }))
	section("place x closure-tag", by(func(s site) []string {
		if s.kind != "closure" {
			return nil
		}
		t := "plain"
		if len(s.tags) > 0 {
			t = strings.Join(s.tags, "+")
		}
		return []string{s.place + "/" + t}
	}))
	section("enclosing", by(func(s site) []string {
		k := "funcdecl"
		if s.funcLit {
			k = "funclit"
		}
		if s.namedRes {
			k += "+named-results"
		}
		return []string{k}
	}))
}

func terminates(st ast.Stmt) bool {
	switch st := st.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.ExprStmt:
		if c, ok := st.X.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	case *ast.BranchStmt:
		return st.Tok == token.GOTO
	}
	return false
}
