package check

// Package-unit checking tests: check.Files merges sibling Resolved
// files into one environment — sibling decls type exactly like
// same-file decls — and partitions the typed declarations back to
// one *File per input.

import (
	"testing"

	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
)

func resolveFiles(t *testing.T, srcs map[string]string) []*resolve.Resolved {
	t.Helper()
	var names []string
	for name := range srcs {
		names = append(names, name)
	}
	// sort for deterministic file order
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	var files []*syntax.File
	for _, name := range names {
		f, ds := syntax.Parse([]byte(srcs[name]), name)
		if ds.HasError() {
			t.Fatalf("parse %s: %v", name, ds)
		}
		files = append(files, f)
	}
	ress, ds := resolve.Files(files, nil)
	if ds.HasError() {
		t.Fatalf("resolve: %v", ds)
	}
	return ress
}

// A call across files typechecks like a same-file call: the callee's
// declared signature is the one the call site is checked against.
func TestFilesSiblingCall(t *testing.T) {
	ress := resolveFiles(t, map[string]string{
		"a.gom": "package m\n\nfn f () -> Int = g ()\n",
		"b.gom": "package m\n\nfn g () -> Int = 1\n",
	})
	files, ds := Files(ress, nil)
	if ds.HasError() {
		t.Fatalf("unexpected diagnostics: %v", ds)
	}
	if len(files) != 2 {
		t.Fatalf("Files returned %d, want 2", len(files))
	}
	// The typed declarations partition back to their own files.
	for i, names := range [][]string{{"f"}, {"g"}} {
		var got []string
		for _, d := range files[i].Decls {
			switch d := d.(type) {
			case *FnDecl:
				got = append(got, d.Sym.Name)
			case *TypeDecl:
				got = append(got, d.Sym.Name)
			default:
				got = append(got, "?")
			}
		}
		if len(got) != len(names) || got[0] != names[0] {
			t.Fatalf("file %d decls = %v, want %v", i, got, names)
		}
	}
}

// A sibling's signature is fully visible: returning a String where
// Int is declared — through the cross-file reference — is a type
// error attached to the calling file.
func TestFilesSiblingTypeError(t *testing.T) {
	ress := resolveFiles(t, map[string]string{
		"a.gom": "package m\n\nfn f () -> Int = g ()\n",
		"b.gom": "package m\n\nfn g () -> String = \"s\"\n",
	})
	_, ds := Files(ress, nil)
	if !ds.HasError() {
		t.Fatal("expected a type error for Int = String via sibling")
	}
	for _, d := range ds {
		if d.Span.File == "a.gom" {
			return
		}
	}
	t.Fatalf("expected the diagnostic on a.gom, got %v", ds)
}

// A sibling ADT typechecks: the constructor application in the other
// file builds the very same type the declaration introduced.
func TestFilesSiblingAdt(t *testing.T) {
	ress := resolveFiles(t, map[string]string{
		"a.gom": "package m\n\ntype T =\n  | A Int\n",
		"b.gom": "package m\n\nfn f () -> T = A 1\n",
	})
	files, ds := Files(ress, nil)
	if ds.HasError() {
		t.Fatalf("unexpected diagnostics: %v", ds)
	}
	if len(files) != 2 {
		t.Fatalf("Files returned %d, want 2", len(files))
	}
}
