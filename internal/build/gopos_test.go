package build_test

// Go-tool position mapping tests (P4-5, docs/design.md section 2.2):
// "file:line:col:" lines of go build / go vet / go test output and
// the "\tfile:line" frames of panic stacks resolve back to .gom
// byte offsets through the generated files' //line names.

import (
	"path/filepath"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
)

const gposSrc = `package m

fn f (x: Int) -> Int = x + 1

fn g () -> Int = todo ()
`

func TestParseGoErrorLine(t *testing.T) {
	for _, tt := range []struct {
		in       string
		wantFile string
		wantLine int
		wantCol  int
		wantMsg  string
		wantOK   bool
	}{
		{"x.gom:12:5: undefined: foo", "x.gom", 12, 5, "undefined: foo", true},
		{"./sub/x.gom:3:14: cannot use y", "./sub/x.gom", 3, 14, "cannot use y", true},
		{"x.gom:7: missing return", "x.gom", 7, 0, "missing return", true},
		{"/abs/dir/x.go:9:2: declared and not used: v", "/abs/dir/x.go", 9, 2, "declared and not used: v", true},
		{"# example.com/m", "", 0, 0, "", false},
		{"", "", 0, 0, "", false},
		{"go test failed", "", 0, 0, "", false},
	} {
		file, ln, col, msg, ok := build.ParseGoErrorLine(tt.in)
		if ok != tt.wantOK || file != tt.wantFile || ln != tt.wantLine || col != tt.wantCol || msg != tt.wantMsg {
			t.Errorf("ParseGoErrorLine(%q) = (%q, %d, %d, %q, %v), want (%q, %d, %d, %q, %v)",
				tt.in, file, ln, col, msg, ok, tt.wantFile, tt.wantLine, tt.wantCol, tt.wantMsg, tt.wantOK)
		}
	}
}

func TestGoPosIndexResolve(t *testing.T) {
	dir := t.TempDir()
	idx := build.NewGoPosIndex(dir, map[string][]byte{
		"m.gom":        []byte(gposSrc),
		"sub/n.gom":    []byte("package n\n"),
		"./dot/o.gom":  []byte("package o\n"),
		"abs/path.gom": []byte("package p\n"),
	})

	// Exact key, "./"-stripped, and dir-absolutized names all reach
	// the same source; the span is reported against the srcs key.
	abs := filepath.Join(dir, "m.gom")
	for _, name := range []string{"m.gom", "./m.gom", abs} {
		sp, ok := idx.Resolve(name, 5, 8)
		if !ok {
			t.Fatalf("Resolve(%q) unmatched", name)
		}
		want := diag.Point("m.gom", 48) // line 5 head is 41; col 8 is the '-' of "->"
		if sp != want {
			t.Errorf("Resolve(%q) = %v, want %v", name, sp, want)
		}
	}
	// "./dot/o.gom" is a srcs key verbatim: the printed name matches
	// as-is, and "dot/o.gom" reaches the same file through dir —
	// both are the same on-disk input.
	for _, name := range []string{"./dot/o.gom", "dot/o.gom", filepath.Join(dir, "dot/o.gom")} {
		sp, ok := idx.Resolve(name, 1, 1)
		if !ok || sp.File != "./dot/o.gom" {
			t.Errorf("Resolve(%q) = (%v, %v), want the ./dot/o.gom key", name, sp, ok)
		}
	}
	// Line clamped into the file, column clamped into the line.
	sp, ok := idx.Resolve("m.gom", 1000, 1)
	if !ok {
		t.Fatal("Resolve(1000) unmatched")
	}
	if sp.File != "m.gom" {
		t.Errorf("clamped span = %v", sp)
	}
	// Unknown files — handwritten Go, toolchain paths — report false.
	for _, name := range []string{"z.go", "other.gom", "/usr/local/go/src/runtime/panic.go"} {
		if _, ok := idx.Resolve(name, 1, 1); ok {
			t.Errorf("Resolve(%q) matched an unknown file", name)
		}
	}
}

// TestGoPosIndexDeterministic pins the two-pass match: an exact or
// "./"-stripped key wins over an absolutized-path match, and an abs
// collision resolves to the lexicographically-first key — the answer
// cannot wobble with map iteration order.
func TestGoPosIndexDeterministic(t *testing.T) {
	dir := t.TempDir()
	idx := build.NewGoPosIndex(dir, map[string][]byte{
		"a/x.gom":        []byte("package a\n"),
		"sub/../a/x.gom": []byte("package a\n"), // same file, spelled through ..
		"z/../y.gom":     []byte("package y\n"), // D/y.gom, loses the sort to a/../y.gom
		"a/../y.gom":     []byte("package y\n"), // D/y.gom, wins the sort
	})
	// "a/x.gom" is an exact srcs key; "sub/../a/x.gom" only matches
	// by absolutization. The exact key must win regardless of which
	// the map yields first.
	for i := 0; i < 20; i++ {
		sp, ok := idx.Resolve("a/x.gom", 1, 1)
		if !ok || sp.File != "a/x.gom" {
			t.Fatalf("iter %d: Resolve(a/x.gom) = (%v, %v), want the exact key", i, sp, ok)
		}
	}
	// "y.gom" matches no key directly; both abs candidates point at
	// the same file — the lexicographically-first spelling wins.
	for i := 0; i < 20; i++ {
		sp, ok := idx.Resolve("y.gom", 1, 1)
		if !ok || sp.File != "a/../y.gom" {
			t.Fatalf("iter %d: Resolve(y.gom) = (%v, %v), want a/../y.gom", i, sp, ok)
		}
	}
}

func TestGoToolDiagnostics(t *testing.T) {
	idx := build.NewGoPosIndex("", map[string][]byte{"m.gom": []byte(gposSrc)})
	lines := []string{
		"# example.com/m",
		"m.gom:5:8: undefined: todo",
		"z.go:3:1: syntax error",
		"some unpositioned complaint",
	}
	ds := build.GoToolDiagnostics(lines, "go build", idx)
	if len(ds) != 3 {
		t.Fatalf("diagnostics = %d, want 3 (# line skipped): %v", len(ds), ds)
	}
	for _, d := range ds {
		if d.Code != "E0001" || d.Source != diag.SourceGo {
			t.Errorf("diagnostic = %v/%v, want E0001/go", d.Code, d.Source)
		}
	}
	// The .gom line resolves to a span; the .go and unpositioned
	// lines stay positionless.
	if !ds[0].Span.Valid() || ds[0].Span.File != "m.gom" {
		t.Errorf("gom diagnostic span = %v", ds[0].Span)
	}
	if ds[1].Span.Valid() {
		t.Errorf("handwritten .go diagnostic got a gom span: %v", ds[1].Span)
	}
	if ds[2].Span.Valid() || ds[2].Message == "" {
		t.Errorf("unpositioned diagnostic = %+v", ds[2])
	}
	if ds[0].Message != "undefined: todo" {
		t.Errorf("message = %q, want the tool's own text", ds[0].Message)
	}

	// A run with no parseable content still yields one positionless
	// diagnostic.
	ds = build.GoToolDiagnostics([]string{"", "# pkg"}, "go vet", idx)
	if len(ds) != 1 || ds[0].Code != "E0001" || ds[0].Span.Valid() {
		t.Fatalf("empty-run diagnostics = %v", ds)
	}
}

// TestResolveStack: a panic trace whose //line-adjusted frames name
// the .gom file resolves them to point spans; rt and toolchain
// frames stay unmapped.
func TestResolveStack(t *testing.T) {
	idx := build.NewGoPosIndex("", map[string][]byte{"m.gom": []byte(gposSrc)})
	trace := `panic: gom: todo

goroutine 1 [running]:
github.com/podhmo/gom/rt.Todo[...](...)
	/work/gom/rt/rt.go:25 +0x30
main.g()
	m.gom:5 +0x40
main.main()
	m.gom:9 +0x20
exit status 2
`
	frames := idx.ResolveStack(trace)
	if len(frames) != 3 {
		t.Fatalf("frames = %d, want 3: %+v", len(frames), frames)
	}
	if frames[0].Span.Valid() {
		t.Errorf("rt frame mapped to .gom: %v", frames[0].Span)
	}
	// m.gom:5 is the `fn g` line; the span points at its head
	// (stack positions carry no column).
	want := diag.Point("m.gom", 41) // line 5 head — "fn g () -> Int = todo ()"
	if frames[1].Span != want {
		t.Errorf("g() frame = %v, want %v", frames[1].Span, want)
	}
	// Line 9 is past EOF — clamped into the file, still a valid
	// .gom point.
	if !frames[2].Span.Valid() || frames[2].Span.File != "m.gom" {
		t.Errorf("main() frame = %v", frames[2].Span)
	}
}
