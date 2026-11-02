package format

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/treedump"
)

var spanRe = regexp.MustCompile(`@[0-9]+:[0-9]+`)

// parseNorm renders src's syntax tree for the "same syntax tree"
// half of spec 1.7's fmt guarantee; it fatals when src does not
// parse. normSyntax is the non-fatal half, for callers inside a
// sandbox where t.Fatalf would lose the DONE marker.
func parseNorm(t *testing.T, src []byte, file string, iface bool) string {
	t.Helper()
	norm, diags := normSyntax(src, file, iface)
	if diags.HasError() {
		t.Fatalf("%s does not parse: %v", file, diags)
	}
	return norm
}

// normSyntax dumps src's surface tree with spans stripped — the
// parse(fmt(x)) == parse(x) invariant — and returns the diagnostics
// so a caller can skip unparseable input instead of failing.
//
// The compare is structural, so the dump's own layout choices must
// not leak in: a form renders flat only under the one-block-child
// rule, and comment/blank trivia count as children — trivia the
// formatter ADDS (the canonical blank line between package clause
// and first decl, the gaps around a decl it re-anchors a comment
// into) would flip file-level flatness and fabricate a diff. The
// formatter also hoists and sorts imports (spec 1.7), so an import
// line's position relative to other decls is not invariant either.
// Dumping each decl as a single-decl File removes both leaks: no
// trivia ever enters the dump (attached Doc comments stay — they
// are part of the tree), and import decls join the stream as a
// sorted set ahead of the others, which keep their source order.
func normSyntax(src []byte, file string, iface bool) (string, diag.List) {
	var f *syntax.File
	var diags diag.List
	if iface {
		f, diags = syntax.ParseInterface(src, file)
	} else {
		f, diags = syntax.Parse(src, file)
	}
	if diags.HasError() {
		return "", diags
	}
	var imps, rest []string
	for _, d := range f.Decls {
		dump := spanRe.ReplaceAllString(treedump.DumpSyntax(
			&syntax.File{Package: f.Package, Decls: []syntax.Decl{d}}), "")
		if _, isImport := d.(*syntax.ImportDecl); isImport {
			imps = append(imps, dump)
		} else {
			rest = append(rest, dump)
		}
	}
	sort.Strings(imps)
	return "package " + f.PackageName() + "\n" +
		strings.Join(imps, "") + strings.Join(rest, ""), diags
}

// fmt is a debugging aid: GOMFMT=<file> go test -run=File -v dumps
// the formatted output of that file.
func TestFile(t *testing.T) {
	path := os.Getenv("GOMFMT")
	if path == "" {
		t.Skip("set GOMFMT=<file> to dump formatted output")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	iface := strings.HasSuffix(path, ".gomi")
	out, diags := Source(src, path, iface)
	for _, d := range diags {
		t.Logf("diag: %v", d)
	}
	if diags.HasError() {
		t.Fatalf("parse errors")
	}
	t.Logf("\n%s", out)
}

// TestGolden runs every testdata/fmt/*.in.gom fixture through Source
// and compares to the adjacent *.out.gom (interface fixtures use
// .in.gomi/.out.gomi). GOMFMT_UPDATE=1 regenerates the .out files.
func TestGolden(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "fmt")
	ins, err := filepath.Glob(filepath.Join(dir, "*.in.gom*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) == 0 {
		t.Fatal("no fixtures under testdata/fmt")
	}
	for _, in := range ins {
		name := filepath.Base(in)
		out := strings.Replace(name, ".in.", ".out.", 1)
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			got, diags := Source(src, in, strings.HasSuffix(in, ".gomi"))
			if diags.HasError() {
				t.Fatalf("input has parse errors: %v", diags)
			}
			want, err := os.ReadFile(filepath.Join(dir, out))
			if err != nil || os.Getenv("GOMFMT_UPDATE") != "" {
				if err := os.WriteFile(filepath.Join(dir, out), got, 0o644); err != nil {
					t.Fatal(err)
				}
				want = got
			}
			if string(got) != string(want) {
				t.Fatalf("format differs:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// TestTreePreserved parses input and formatted output and compares
// their surface trees with source positions stripped — the
// formatter's semantic-preservation contract.
func TestTreePreserved(t *testing.T) {
	src := []byte(`package m
fn f (x: Int) -> Int =
  if x > 0 { x } else { -x }
`)
	out, diags := Source(src, "t.gom", false)
	if diags.HasError() {
		t.Fatal(diags)
	}
	a := parseNorm(t, src, "t.gom", false)
	b := parseNorm(t, out, "t.gom", false)
	if a != b {
		t.Fatalf("trees differ:\n%s\n--- vs ---\n%s", a, b)
	}
}

// TestLabelColonParens covers the two "~label" colon-capture
// regressions. An ascription over an expression ending in a punned
// "~x" must keep parens — bare "f ~x : T" reparses as "f (~x: T)"
// (grammar.ebnf app-arg's greedy label ":"). And a labeled
// constructor-pattern argument is a pat-atom, so non-atom patterns
// keep parens — "~y: (a | b)" printed bare becomes "(B ~y: a) | b",
// and "~v: (Int)" printed bare fails to reparse at all.
func TestLabelColonParens(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`let a = (f ~x) : Int`, `let a = (f ~x) : Int`},
		{`let b = f ~x: Int`, `let b = f ~x: Int`},
		{`let c = f ~x: v : Int`, `let c = f ~x: v : Int`},
		{`let d = (xs |> f ~x) : Int`, `let d = (xs |> f ~x) : Int`},
		{`let e = (a + f ~x) : Int`, `let e = (a + f ~x) : Int`},
		{`let g = (-f ~x) : Int`, `let g = (-f ~x) : Int`},
		{`let h = (f ~x ~y) : Int`, `let h = (f ~x ~y) : Int`},
		{`let i = "${(f ~x) : Int}"`, `let i = "${(f ~x) : Int}"`},
		// A bare pipe-rhs lambda swallows the ":" into its body.
		{`let j = (xs |> fn y -> y) : Int`, `let j = (xs |> fn y -> y) : Int`},
		{`let m = (a |> f |> fn y -> y) : Int`, `let m = (a |> f |> fn y -> y) : Int`},
		{`let n = (x |> (fn y -> y) |> g) : Int`, `let n = x |> (fn y -> y) |> g : Int`},
		{"let p = match s {\n  B ~y: (Int) -> 1\n  _ -> 0\n}", `B ~y: (Int)`},
		{"let q = match s {\n  B ~y: (a | b) -> 1\n  _ -> 0\n}", `B ~y: (a | b)`},
		{"let r = match s {\n  B ~y: (x as r) -> 1\n  _ -> 0\n}", `B ~y: (x as r)`},
		{"let s = match s {\n  B ~y: (N x) -> 1\n  _ -> 0\n}", `B ~y: (N x)`},
	} {
		src := []byte("package m\n" + tc.in + "\n")
		out, diags := Source(src, "t.gom", false)
		if diags.HasError() {
			t.Fatalf("%q: parse errors: %v", tc.in, diags)
		}
		if !strings.Contains(string(out), tc.want) {
			t.Fatalf("%q: output missing %q:\n%s", tc.in, tc.want, out)
		}
		// The full contract: reparse, idempotent, same syntax tree.
		out2, diags2 := Source(out, "t.gom", false)
		if diags2.HasError() {
			t.Fatalf("%q: output does not reparse: %v\n%s", tc.in, diags2, out)
		}
		if string(out) != string(out2) {
			t.Fatalf("%q: not idempotent:\n%s\n--- vs ---\n%s", tc.in, out, out2)
		}
		if a, b := parseNorm(t, src, "t.gom", false), parseNorm(t, out, "t.gom", false); a != b {
			t.Fatalf("%q: syntax tree changed:\n%s\n--- vs ---\n%s\n--- formatted ---\n%s", tc.in, a, b, out)
		}
	}
}

// TestTypeTailParens covers the "rendered tail ends in a type"
// regressions: in a head expression or before an arm's "->", a bare
// ascription tail lets the following "{" or "->" fold into the type
// — "if x : T {a : Int}" reads the block as a record-type argument
// and "x <- ch : Int -> b" reads the arrow as a function type
// (docs/problems/2026-11-02-guard-colon-arrow.md). The same rule
// keeps parens on a nested application callee — "(f a) b" is a
// different tree from "f a b" — and on a nested type-application
// ctor, which is a type-atom like the args.
func TestTypeTailParens(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// Ascription tails in head position.
		{`let a = if (x : Bool) { 1 } else { 2 }`, `if (x : Bool) {`},
		{"let b = match (x : Int) {\n  1 -> 2\n  _ -> 0\n}", `match (x : Int) {`},
		{`let c = for v in (ys : List Int) { v }`, `for v in (ys : List Int) {`},
		// The tail reaches an ascription through a bare pipe-rhs
		// lambda body or a let's value too.
		{`let d = if (xs |> fn y -> y : Bool) { 1 } else { 2 }`, `if (xs |> fn y -> y : Bool) {`},
		// Arm operands before "->": recv channel and match guard.
		{"let e = select {\n  x <- (ch : Int) -> x\n  default -> 0\n}", `x <- (ch : Int) -> x`},
		{"let f = select {\n  y <- (ch |> fn z -> z : Int) -> y\n  default -> 0\n}", `y <- (ch |> fn z -> z : Int) -> y`},
		{"let g = match s {\n  A if (g : Bool) -> 1\n  _ -> 0\n}", `A if (g : Bool) -> 1`},
		{"let h = match s {\n  A if (xs |> fn y -> y : Bool) -> 1\n  _ -> 0\n}", `A if (xs |> fn y -> y : Bool) -> 1`},
		// A nested application callee keeps its parens: "(f a) b" is
		// not "f a b" — arity is judged per Apply node.
		{`let i = (f a b) c d`, `(f a b) c d`},
		{`let j = (f a) b`, `(f a) b`},
		{`let k = ((g x) |> f) y`, `(g x |> f) y`},
		// A nested type-application ctor is a type-atom.
		{`let l : (F A B) C D = y`, `(F A B) C D`},
		{`let m : ((A -> B) C) D = y`, `((A -> B) C) D`},
		// No gratuitous parens: a pipe head ending in a name is safe,
		// and so is a plain multi-arg call or type app.
		{`let n = if a |> f { 1 } else { 2 }`, `if a |> f {`},
		{`let o = f a b`, `let o = f a b`},
		{`let p : F A B = y`, `let p: F A B`},
		{"let q = select {\n  x <- ch -> x\n  default -> 0\n}", `x <- ch -> x`},
		{"let r = match s {\n  A if g -> 1\n  _ -> 0\n}", `A if g -> 1`},
	} {
		src := []byte("package m\n" + tc.in + "\n")
		out, diags := Source(src, "t.gom", false)
		if diags.HasError() {
			t.Fatalf("%q: parse errors: %v", tc.in, diags)
		}
		if !strings.Contains(string(out), tc.want) {
			t.Fatalf("%q: output missing %q:\n%s", tc.in, tc.want, out)
		}
		// The full contract: reparse, idempotent, same syntax tree.
		out2, diags2 := Source(out, "t.gom", false)
		if diags2.HasError() {
			t.Fatalf("%q: output does not reparse: %v\n%s", tc.in, diags2, out)
		}
		if string(out) != string(out2) {
			t.Fatalf("%q: not idempotent:\n%s\n--- vs ---\n%s", tc.in, out, out2)
		}
		if a, b := parseNorm(t, src, "t.gom", false), parseNorm(t, out, "t.gom", false); a != b {
			t.Fatalf("%q: syntax tree changed:\n%s\n--- vs ---\n%s\n--- formatted ---\n%s", tc.in, a, b, out)
		}
	}
}

// TestRepoFiles formats every .gom/.gomi under the repo root,
// checking for panics, reparsability and idempotency.
func TestRepoFiles(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && (info.Name() == ".git" || info.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".gom") || strings.HasSuffix(path, ".gomi") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Skip("no .gom files")
	}
	for _, path := range files {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			iface := strings.HasSuffix(path, ".gomi")
			out, diags := Source(src, path, iface)
			if diags.HasError() {
				t.Skipf("input does not parse cleanly (%d diags)", len(diags))
			}
			out2, diags2 := Source(out, path, iface)
			if diags2.HasError() {
				t.Fatalf("formatted output does not reparse: %v\n--- output ---\n%s", diags2, out)
			}
			if string(out) != string(out2) {
				t.Fatalf("not idempotent:\n--- pass 1 ---\n%s\n--- pass 2 ---\n%s", out, out2)
			}
			// The formatted file must hold the same syntax tree —
			// spec 1.7's parse(fmt(x)) == parse(x) guarantee.
			if a, b := parseNorm(t, src, path, iface), parseNorm(t, out, path, iface); a != b {
				t.Fatalf("syntax tree changed:\n--- in ---\n%s\n--- out ---\n%s", a, b)
			}
		})
	}
}
