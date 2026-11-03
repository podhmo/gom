package resolve

// Package-unit tests for Files: the .gom files of one directory share
// one top-level scope while imports stay file-local, package clauses
// must agree (E0605), and cross-file duplicates are E0202.

import (
	"testing"

	"github.com/podhmo/gom/internal/syntax"
)

func parseFile(t *testing.T, name, src string) *syntax.File {
	t.Helper()
	f, ds := syntax.Parse([]byte(src), name)
	if ds.HasError() {
		t.Fatalf("parse %s: %v", name, ds)
	}
	return f
}

// A decl in one file is visible to uses in a sibling file, and both
// files end up pointing at the same *Symbol.
func TestFilesSharedScope(t *testing.T) {
	a := parseFile(t, "a.gom", "package m\n\nfn f () -> Int = g\n")
	b := parseFile(t, "b.gom", "package m\n\nfn g () -> Int = 1\n")
	rs, ds := Files([]*syntax.File{a, b}, nil)
	if ds.HasError() {
		t.Fatalf("unexpected diagnostics: %v", ds)
	}
	if len(rs) != 2 {
		t.Fatalf("Files returned %d resolved files, want 2", len(rs))
	}
	var use, def *Symbol
	for _, r := range rs {
		for _, sym := range r.Uses {
			if sym.Name == "g" {
				use = sym
			}
		}
		for _, sym := range r.Defs {
			if sym.Name == "g" {
				def = sym
			}
		}
	}
	if use == nil || def == nil {
		t.Fatalf("g not resolved: use=%v def=%v", use, def)
	}
	if use != def {
		t.Fatalf("use of g is %p, def is %p — sibling scope not shared", use, def)
	}
}

// The same top-level name in two files of one package is a duplicate,
// reported as E0202 like a same-file duplicate.
func TestFilesDuplicateAcrossFiles(t *testing.T) {
	a := parseFile(t, "a.gom", "package m\n\nfn f () -> Int = 1\n")
	b := parseFile(t, "b.gom", "package m\n\nfn f () -> Int = 2\n")
	_, ds := Files([]*syntax.File{a, b}, nil)
	if !hasCode(ds, "E0202") {
		t.Fatalf("expected E0202 for cross-file duplicate, got %v", codes(ds))
	}
}

// Sibling type decls share scope too: a constructor defined in one
// file resolves in a pattern of the other.
func TestFilesSharedCtors(t *testing.T) {
	a := parseFile(t, "a.gom", "package m\n\ntype T =\n  | A Int\n")
	b := parseFile(t, "b.gom", "package m\n\nfn f (x: T) -> Int = match x {\n  A n -> n\n}\n")
	rs, ds := Files([]*syntax.File{a, b}, nil)
	if ds.HasError() {
		t.Fatalf("unexpected diagnostics: %v", ds)
	}
	var ctor, def *Symbol
	for _, r := range rs {
		for _, sym := range r.Uses {
			if sym.Name == "A" && sym.Kind == KindCtor {
				ctor = sym
			}
		}
		for _, sym := range r.Defs {
			if sym.Name == "A" && sym.Kind == KindCtor {
				def = sym
			}
		}
	}
	if ctor == nil || def == nil {
		t.Fatalf("constructor A not resolved: use=%v def=%v", ctor, def)
	}
	if ctor != def {
		t.Fatalf("pattern use of A is %p, def is %p — sibling scope not shared", ctor, def)
	}
}

// Package clauses must agree across the unit; a mismatch is E0605
// pointing at the later clause.
func TestFilesPackageMismatch(t *testing.T) {
	a := parseFile(t, "a.gom", "package m\n\nfn f () -> Int = 1\n")
	b := parseFile(t, "b.gom", "package other\n\nfn g () -> Int = 2\n")
	_, ds := Files([]*syntax.File{a, b}, nil)
	if !hasCode(ds, "E0605") {
		t.Fatalf("expected E0605 for mismatched package clauses, got %v", codes(ds))
	}
	for _, d := range ds {
		if d.Code == "E0605" && d.Span.File != "b.gom" {
			t.Fatalf("E0605 attached to %s, want the later file b.gom", d.Span.File)
		}
	}
}

// Imports are file-local: `fmt` imported by a.gom is not visible in
// b.gom, which reports its own unresolved name.
func TestFilesImportsStayFileLocal(t *testing.T) {
	println := &Symbol{Name: "Println", Kind: KindFunc, IsGo: true, Package: "fmt"}
	loader := MapLoader{
		"go:fmt": {Path: "fmt", Members: map[string]*Symbol{"Println": println}},
	}
	a := parseFile(t, "a.gom", "package m\n\nimport go \"fmt\"\n\nfn f () -> Int = 1\n")
	b := parseFile(t, "b.gom", "package m\n\nfn g () -> Int = fmt.Println\n")
	_, ds := Files([]*syntax.File{a, b}, &Config{Loader: loader})
	if !ds.HasError() {
		t.Fatalf("expected an unresolved-name diagnostic for fmt in b.gom, got none")
	}
	for _, d := range ds {
		if d.Span.File == "b.gom" {
			return
		}
	}
	t.Fatalf("expected the diagnostic on b.gom, got %v", ds)
}
