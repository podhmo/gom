package codegen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
)

// File translates a validated Core IR file into Go source.
//
// The input must pass core.Validate — codegen makes no semantic
// decisions of its own, so a malformed IR produces an
// *InvalidFileError instead of best-effort output. src is the .gom
// source text the file's spans index into; it feeds the
// `//line file.gom:N` directives mapping generated statements back
// to the source.
//
// The output is the gofmt'd contents of two files: the package
// source and, when the file declares tests, its _test.go half
// (docs/design.md section 4.8).
func File(f *core.File, src []byte) (*Output, error) {
	if f == nil {
		return nil, fmt.Errorf("codegen: nil file")
	}
	if vs := core.Validate(f); len(vs) > 0 {
		return nil, &InvalidFileError{Violations: vs}
	}

	li := diag.NewLineIndex(src)

	main := newGen(li)
	main.fileImports = f.Imports
	main.indexDecls(f)

	test := newGen(li)
	test.fileImports = f.Imports
	test.indexDecls(f)

	out := &Output{}
	hasTests := false
	for _, d := range f.Decls {
		if td, ok := d.(*core.TestDecl); ok {
			hasTests = true
			out.Tests = append(out.Tests, TestMapping{
				Name:   td.Name,
				Func:   test.testDecl(td),
				Sp:     td.Sp,
				NameSp: td.NameSp,
			})
			continue
		}
		if testOnlyDecl(d) {
			// An `@test` helper is test-only code (spec section
			// 11): it renders into the _test.go half and never
			// reaches production output.
			test.decl(d)
			continue
		}
		main.decl(d)
	}
	// Generated structural comparators belong to the package source
	// even when only test code reaches for them — an unused func is
	// legal Go, and the two halves share the file's symbol table.
	for _, d := range f.EqDecls {
		main.eqDecl(d)
	}
	// Generated display functions live in the test half — only
	// test code reaches for them, so production output stays free
	// of them.
	for _, d := range f.ShowDecls {
		test.showDecl(d)
	}

	source, sm, err := render(f.Package, main)
	if err != nil {
		return nil, err
	}
	out.Source = source
	out.SourceMap = sm

	if hasTests {
		ts, tsm, err := render(f.Package, test)
		if err != nil {
			return nil, err
		}
		out.TestSource = ts
		out.TestSourceMap = tsm
	}
	return out, nil
}

// Output is the result of File.
type Output struct {
	// Source is the formatted contents of `<pkg>.go`.
	Source []byte
	// TestSource is the formatted contents of `<pkg>_test.go` —
	// nil when the Core file declares no tests.
	TestSource []byte
	// Tests maps every `func TestX(t *testing.T)` emitted into
	// TestSource back to its `test "Name" { ... }` declaration, in
	// declaration order — `gom test` translates `go test -json`
	// events through it (docs/design.md section 4.8).
	Tests []TestMapping

	// SourceMap is the generated-Go → .gom anchor table of Source —
	// the byte-offset counterpart of the file's `//line` directives
	// (docs/design.md section 2.2's correspondence table). The build
	// layer exposes it on build.Result so go build / go vet errors
	// and panic stacks can be restored to .gom positions, and the
	// future LSP can query generated positions (P4-5/P4-6).
	SourceMap *diag.SourceMap
	// TestSourceMap is SourceMap's counterpart for TestSource —
	// nil when the file declares no tests.
	TestSourceMap *diag.SourceMap
}

// TestMapping identifies one generated Go test function.
type TestMapping struct {
	// Name is the test's name as written in `test "Name" { ... }`.
	Name string
	// Func is the emitted Go function name (e.g. "TestRectArea",
	// "TestRectArea2" after a collision).
	Func string
	// Sp is the whole test declaration's span.
	Sp diag.Span
	// NameSp is the name literal's span — the test's identifying
	// position.
	NameSp diag.Span
}

// testOnlyDecl reports whether d is an `@test`-marked declaration —
// a FuncDecl or VarDecl whose symbol resolve flagged TestOnly
// (spec section 11). Type declarations cannot be marked.
func testOnlyDecl(d core.Decl) bool {
	switch d := d.(type) {
	case *core.FuncDecl:
		return d.Sym != nil && d.Sym.TestOnly
	case *core.VarDecl:
		return d.Sym != nil && d.Sym.TestOnly
	default:
		return false
	}
}

// ParseFile parses Output.Source into a go/ast tree — a convenience
// for callers and tests that want the AST the bytes describe.
func (o *Output) ParseFile() (*ast.File, *token.FileSet, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", o.Source, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	return f, fset, nil
}

// render assembles a file's final text: the generated-code marker,
// the package clause, the collected imports, and the emitted body —
// then gofmt's it via format.Source. The returned map is the
// file's `//line` anchor table (docs/design.md section 2.2).
func render(pkg string, g *gen) ([]byte, *diag.SourceMap, error) {
	var out bytes.Buffer
	out.WriteString("// Code generated by gom. DO NOT EDIT.\n\n")
	out.WriteString("package " + g.safeIdent(pkg) + "\n")
	if len(g.imports) > 0 {
		out.WriteString("\nimport (\n")
		paths := make([]string, 0, len(g.imports))
		for p := range g.imports {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			name := g.imports[p]
			if name == pathBase(p) {
				fmt.Fprintf(&out, "\t%q\n", p)
			} else {
				fmt.Fprintf(&out, "\t%s %q\n", name, p)
			}
		}
		out.WriteString(")\n")
	}
	out.WriteByte('\n')
	out.Write(g.buf.Bytes())

	src, err := format.Source(out.Bytes())
	if err != nil {
		return nil, nil, fmt.Errorf("codegen: generated source does not gofmt: %w\n%s", err, out.String())
	}
	spliced, sm := spliceLines(src, g.marks)
	return spliced, sm, nil
}

// spliceLines replaces each `//gomsrc:idx:N:file` sentinel with the
// real `//line file:N` directive at column 1. A `//line` directive
// pins the following line's column and gofmt will not re-indent
// after one, so directives are emitted as ordinary comments,
// formatted, and only then rewritten. Each rewritten directive also
// yields one source-map anchor — the line it pins paired with the
// span marks[idx] recorded when the sentinel was emitted — so the
// exported map cannot disagree with the directives in the output.
func spliceLines(src []byte, marks []diag.Span) ([]byte, *diag.SourceMap) {
	const marker = "//gomsrc:"
	var out bytes.Buffer
	out.Grow(len(src))
	sm := &diag.SourceMap{}
	lineNo := 0
	for len(src) > 0 {
		lineNo++
		i := bytes.IndexByte(src, '\n')
		var line []byte
		if i < 0 {
			line, src = src, nil
		} else {
			line, src = src[:i], src[i+1:]
		}
		trimmed := bytes.TrimLeft(line, " \t")
		if bytes.HasPrefix(trimmed, []byte(marker)) {
			rest := trimmed[len(marker):]
			if j := bytes.IndexByte(rest, ':'); j >= 0 {
				idx, err := strconv.Atoi(string(rest[:j]))
				rest = rest[j+1:]
				k := bytes.IndexByte(rest, ':')
				// A malformed sentinel — out-of-range or negative
				// index, missing line or empty file field — is
				// passed through verbatim rather than trusted;
				// marks[-1] must never index.
				if err == nil && k >= 0 && k+1 < len(rest) && 0 <= idx && idx < len(marks) {
					out.WriteString("//line ")
					out.Write(rest[k+1:])
					out.WriteByte(':')
					out.Write(rest[:k])
					out.WriteByte('\n')
					sm.Anchors = append(sm.Anchors, diag.Anchor{
						Line: lineNo + 1,
						Span: marks[idx],
					})
					continue
				}
			}
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes(), sm
}

// InvalidFileError reports a Core IR file that failed validation —
// code generation requires the guarantees the validator checks, so
// this is a caller bug, not a user diagnostic.
type InvalidFileError struct {
	Violations []core.Violation
}

func (e *InvalidFileError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "codegen: core file failed validation (%d violation(s))", len(e.Violations))
	for _, v := range e.Violations {
		fmt.Fprintf(&b, "\n  %s", v)
	}
	return b.String()
}
