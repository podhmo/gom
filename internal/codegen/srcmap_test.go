package codegen

// Source-map verification (P4-5, docs/design.md section 2.2): the
// anchor table codegen exports next to each generated file must pair
// 1:1 with the spliced //line directives, and each anchor's pinned
// line must resolve through go/token exactly as the Go toolchain
// resolves it — that is what makes go build errors and panic stacks
// report .gom positions.

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// TestSourceMapFixture checks the whole-fixture output: both halves
// carry anchor tables consistent with their //line directives.
func TestSourceMapFixture(t *testing.T) {
	out, err := File(buildFixture(), fixtureSrc())
	if err != nil {
		t.Fatal(err)
	}
	if out.SourceMap == nil {
		t.Fatal("SourceMap is not set")
	}
	checkSourceMap(t, out.Source, out.SourceMap)
	if len(out.TestSource) == 0 || out.TestSourceMap == nil {
		t.Fatal("fixture declares tests: TestSource and TestSourceMap must be set")
	}
	checkSourceMap(t, out.TestSource, out.TestSourceMap)
}

// checkSourceMap verifies sm against src's own //line directives:
// the counts match, the anchors are strictly increasing, and the
// generated line each anchor pins resolves under go/token to the
// span's file and start line — Go's own reading of the directives.
func checkSourceMap(t *testing.T, src []byte, sm *diag.SourceMap) {
	t.Helper()
	if directives := lineCount(string(src), "//line "); len(sm.Anchors) != directives {
		t.Errorf("%d anchors for %d //line directives", len(sm.Anchors), directives)
	}
	li := diag.NewLineIndex(fixtureSrc())

	fset := token.NewFileSet()
	gf, err := parser.ParseFile(fset, "gen.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("generated source does not parse: %v", err)
	}
	tokFile := fset.File(gf.Package)
	if tokFile == nil {
		t.Fatal("parsed file has no token.File")
	}

	prev := 0
	for _, a := range sm.Anchors {
		if a.Line <= prev {
			t.Fatalf("anchors not strictly increasing: line %d after %d", a.Line, prev)
		}
		prev = a.Line
		if a.Line > tokFile.LineCount() {
			t.Fatalf("anchor line %d out of generated range (%d lines)", a.Line, tokFile.LineCount())
		}
		p := fset.Position(tokFile.LineStart(a.Line))
		wantLine := li.Pos(a.Span.Start).Line
		if p.Filename != a.Span.File || p.Line != wantLine {
			t.Errorf("anchor at gen line %d resolves to %s:%d, want %s:%d",
				a.Line, p.Filename, p.Line, a.Span.File, wantLine)
		}
		if got, ok := sm.SpanAt(a.Line); !ok || got != a.Span {
			t.Errorf("SpanAt(%d) = (%v, %v), want the anchor's own span %v",
				a.Line, got, ok, a.Span)
		}
	}
}

// lineCount counts lines that begin with prefix — a directive only
// takes effect at the start of a line, so a "//line " inside a
// string or a mid-line comment does not count.
func lineCount(src, prefix string) int {
	n := 0
	for l := range strings.SplitSeq(src, "\n") {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

// TestSourceMapLookup exercises the map's two lookup directions on a
// small file: a generated line between anchors resolves to the
// enclosing construct's span, and a .gom offset inside an emitted
// span resolves back to a generated line.
func TestSourceMapLookup(t *testing.T) {
	f := &core.File{Sp: lsp(1), Package: "shapes", PkgSp: lsp(1)}
	aB, aS := vbind("a", types.Int)
	bB, bS := vbind("b", types.Int)
	f.Decls = append(f.Decls, &core.FuncDecl{
		Sp: lsp(3), Sym: fsym("add2"),
		Fn: &core.Func{
			Sp:     lsp(3),
			Sig:    fnIII,
			Params: []*core.Bind{aB, bB},
			Body: &core.Body{Tail: &core.Binary{
				Sp: lsp(4), Typ: types.Int,
				Op: syntax.BinAdd, X: vref(aS, types.Int), Y: vref(bS, types.Int),
			}},
		},
	})
	out, err := File(f, fakeSrc())
	if err != nil {
		t.Fatal(err)
	}
	sm := out.SourceMap

	// The generated file is exactly TestFuncDecl's `want`: header,
	// package clause, directive+func line, directive+return, "}".
	// The two anchors pin lines 5 (func) and 7 (return's body
	// statement)... the exact lines are re-derived here so the
	// assertions track the directives wherever they land.
	var returnAnchor *diag.Anchor
	for i := range sm.Anchors {
		a := &sm.Anchors[i]
		if a.Span == lsp(4) {
			returnAnchor = a
		}
	}
	if returnAnchor == nil {
		t.Fatalf("no anchor carries the body's span %v", lsp(4))
	}
	// The `return a + b` line and its `}` both report the body's
	// span — the directive maps every following line until the next
	// directive; there is none, so the trailing brace belongs to it.
	sp, ok := sm.SpanAt(returnAnchor.Line + 1)
	if !ok || sp != lsp(4) {
		t.Errorf("SpanAt(%d) = (%v, %v), want the body span", returnAnchor.Line+1, sp, ok)
	}
	// A .gom offset inside the body's span resolves to the anchored
	// generated line; an offset before the first emitted construct
	// reports nothing.
	if line, ok := sm.LineAt(lsp(4).Start + 1); !ok || line != returnAnchor.Line {
		t.Errorf("LineAt(%d) = (%d, %v), want line %d", lsp(4).Start+1, line, ok, returnAnchor.Line)
	}
	if _, ok := sm.LineAt(0); ok {
		t.Error("LineAt(0) reported a line — offset 0 is the package clause, unemitted")
	}
}
