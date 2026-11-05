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
		// Edge shapes of the "file:line[:col]: msg" head.
		{"x.gom:12:", "", 0, 0, "", false},         // trailing colon, no message
		{"x.gom:12:5:", "", 0, 0, "", false},       // trailing colon after the column
		{"x.gom:12: ", "x.gom", 12, 0, "", true},   // trailing colon+space: empty message
		{"x.gom:12:5: ", "x.gom", 12, 5, "", true}, // same with a column
		{"x.gom:1:1: msg: with: colons", "x.gom", 1, 1, "msg: with: colons", true},
		{"x.gom:5:msg-no-space", "", 0, 0, "", false},                  // missing the space after ':'
		{"x.gom:5: msg", "x.gom", 5, 0, "msg", true},                   // column group optional
		{"dir:part/x.gom:3:4: ok", "dir:part/x.gom", 3, 4, "ok", true}, // ':' inside the path
		{"x.gom:notnum: msg", "", 0, 0, "", false},                     // non-numeric line field
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

// TestGoPosIndexMultiInput: a package compiled from several .gom
// inputs (the V-6 unit) resolves each printed name to its own
// source — including files that share a basename in different
// relative directories, since the tool prints the relative path it
// was handed and never a bare basename.
func TestGoPosIndexMultiInput(t *testing.T) {
	aSrc := "package a\n\nfn f () -> Int = 1\n"   // fn f on line 3
	bSrc := "package b\n\n\nfn g () -> Int = 2\n" // fn g on line 4
	cSrc := "package c\nfn h () -> Int = 3\n"     // fn h on line 2
	idx := build.NewGoPosIndex("", map[string][]byte{
		"pkg/a/util.gom": []byte(aSrc),
		"pkg/b/util.gom": []byte(bSrc), // same basename as pkg/a
		"pkg/c.gom":      []byte(cSrc),
	})

	// Each input resolves through its own relative path with its
	// own line arithmetic.
	for _, tt := range []struct {
		name string
		line int
		want diag.Span
	}{
		{"pkg/a/util.gom", 3, diag.Point("pkg/a/util.gom", 11)},
		{"pkg/b/util.gom", 4, diag.Point("pkg/b/util.gom", 12)},
		{"pkg/c.gom", 2, diag.Point("pkg/c.gom", 10)},
	} {
		sp, ok := idx.Resolve(tt.name, tt.line, 1)
		if !ok {
			t.Fatalf("Resolve(%q, %d) unmatched", tt.name, tt.line)
		}
		if sp != tt.want {
			t.Errorf("Resolve(%q, %d) = %v, want %v", tt.name, tt.line, sp, tt.want)
		}
	}

	// A bare basename is not a name the tool prints — it must not
	// fuzzy-match one of the same-basename keys.
	if _, ok := idx.Resolve("util.gom", 1, 1); ok {
		t.Error("Resolve(util.gom) matched a same-basename key in a different directory")
	}
}

// TestGoToolDiagnosticsMultiLineChunks: one output element carrying
// several newline-separated lines folds each line into its own
// diagnostic — the shape `go test`'s build-output events produce.
func TestGoToolDiagnosticsMultiLineChunks(t *testing.T) {
	idx := build.NewGoPosIndex("", map[string][]byte{"m.gom": []byte(gposSrc)})
	lines := []string{
		"m.gom:3:1: first\nm.gom:5:8: second\n", // two positions in one chunk
		"\nplain complaint\n",                   // blank line skipped inside the chunk
	}
	ds := build.GoToolDiagnostics(lines, "go build", idx)
	if len(ds) != 3 {
		t.Fatalf("diagnostics = %d, want 3: %v", len(ds), ds)
	}
	if ds[0].Message != "first" || ds[1].Message != "second" || ds[2].Message == "" {
		t.Fatalf("messages = %q / %q / %q", ds[0].Message, ds[1].Message, ds[2].Message)
	}
	if ds[0].Span != diag.Point("m.gom", 11) {
		t.Errorf("first span = %v, want line-3 head (11)", ds[0].Span)
	}
	if ds[1].Span != diag.Point("m.gom", 48) {
		t.Errorf("second span = %v, want line-5 col-8 (48)", ds[1].Span)
	}
	if ds[2].Span.Valid() {
		t.Errorf("unpositioned complaint got a span: %v", ds[2].Span)
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

// TestRenderStack: the .gom-position re-render rewrites the frame
// positions that resolve — dropping the "+0x" trailer and clamping
// past-EOF lines into the file — while call lines, goroutine
// headers and toolchain frames stay verbatim. A dump with no
// resolvable frame comes back byte-identical so the caller can
// tell "nothing mapped" from "mapped".
func TestRenderStack(t *testing.T) {
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
	// m.gom:9 is past EOF — Resolve clamps it to the file's last
	// line, so the rendered frame reports m.gom:6.
	want := `panic: gom: todo

goroutine 1 [running]:
github.com/podhmo/gom/rt.Todo[...](...)
	/work/gom/rt/rt.go:25 +0x30
main.g()
	m.gom:5
main.main()
	m.gom:6
exit status 2
`
	if got := idx.RenderStack(trace); got != want {
		t.Errorf("RenderStack =\n%s\nwant:\n%s", got, want)
	}

	// Ordinary test output has no frame lines — it returns
	// byte-identical rather than picking up a partial rewrite.
	plain := "    m.gom:4: gom: assertion failed:\n          actual:   1\n"
	if got := idx.RenderStack(plain); got != plain {
		t.Errorf("RenderStack on non-stack output = %q, want verbatim", got)
	}

	// A stack entirely in toolchain code resolves nothing — the
	// caller sees the raw dump, not an empty rendered half.
	alien := "panic: x\n\n\t/usr/lib/go/src/runtime/proc.go:1 +0x1\n"
	if got := idx.RenderStack(alien); got != alien {
		t.Errorf("RenderStack on unresolvable stack = %q, want verbatim", got)
	}
}
