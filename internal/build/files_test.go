package build_test

// Package-unit driver tests: build.CompileFiles compiles one
// directory's .gom files as a unit — shared scope through
// resolve/check, per-file pattern/lower/codegen, and outputs are
// all-or-nothing when a member fails after the shared phases.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/lower"
)

func compileFiles(t *testing.T, srcs map[string]string) []*build.Result {
	t.Helper()
	var ins []build.Input
	// deterministic input order for stable result indexing
	var names []string
	for n := range srcs {
		names = append(names, n)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, n := range names {
		ins = append(ins, build.Input{Path: "pkg/" + n, Src: []byte(srcs[n])})
	}
	rs, err := build.CompileFiles(ins, &build.Options{})
	if err != nil {
		t.Fatalf("CompileFiles: %v", err)
	}
	return rs
}

// A sibling `let` is a package-scope var: the other file reads it
// like a same-file one — through resolve's shared scope, check's
// shared env, and Core IR's Unit scope facts.
func TestCompileFilesSiblingLet(t *testing.T) {
	rs := compileFiles(t, map[string]string{
		"a.gom": "package m\n\nlet base = 41\n",
		"b.gom": "package m\n\nfn use () -> Int = base + 1\n",
	})
	for i, r := range rs {
		if r.Diagnostics.HasError() {
			t.Fatalf("result %d diagnostics: %v", i, r.Diagnostics)
		}
		if r.Output == nil {
			t.Fatalf("result %d produced no output", i)
		}
	}
	if !strings.Contains(string(rs[1].Output.Source), "base") {
		t.Fatalf("b.go does not reference the sibling var:\n%s", rs[1].Output.Source)
	}
}

// A `let` bound to a sibling fn is a static alias — the wrapper
// lands in its own file's output and calls across files.
func TestCompileFilesSiblingAlias(t *testing.T) {
	rs := compileFiles(t, map[string]string{
		"a.gom": "package m\n\nfn twice (f: Int -> Int) (x: Int) -> Int = f (f x)\n",
		"b.gom": "package m\n\nlet alias = twice\nfn use () -> Int = alias (fn n -> n + 1) 0\n",
	})
	for i, r := range rs {
		if r.Diagnostics.HasError() {
			t.Fatalf("result %d diagnostics: %v", i, r.Diagnostics)
		}
		if r.Output == nil {
			t.Fatalf("result %d produced no output", i)
		}
	}
}

// An exported sibling `let` is emitted capitalized by its declaring
// file; a reference from a sibling must spell the same exported
// name — codegen claims package names unit-wide from Core IR's
// Unit.Decls rather than re-deriving the export set per file.
func TestCompileFilesSiblingExportedName(t *testing.T) {
	var ins []build.Input
	for _, n := range []string{"a.gom", "b.gom"} {
		src := map[string]string{
			"a.gom": "package m\n\nlet answer = 42\n",
			"b.gom": "package m\n\nfn ask () -> Int = answer\n",
		}[n]
		ins = append(ins, build.Input{Path: "pkg/" + n, Src: []byte(src)})
	}
	rs, err := build.CompileFiles(ins, &build.Options{
		Exports: func(dir, pkg string) map[string]bool {
			return map[string]bool{"answer": true}
		},
	})
	if err != nil {
		t.Fatalf("CompileFiles: %v", err)
	}
	if !strings.Contains(string(rs[0].Output.Source), "var Answer") {
		t.Fatalf("a.go does not emit the exported var:\n%s", rs[0].Output.Source)
	}
	if !strings.Contains(string(rs[1].Output.Source), "Answer") ||
		strings.Contains(string(rs[1].Output.Source), "answer") {
		t.Fatalf("b.go does not reference the exported sibling name:\n%s", rs[1].Output.Source)
	}
}

// A binder may not steal a package name a sibling declares: the
// param `answer` mangles, the package reference keeps `answer` —
// before the unit-wide claim the binder took `answer` and the ref
// emitted `answer1`, which no file defines.
func TestCompileFilesSiblingNameBlockedByBinder(t *testing.T) {
	rs := compileFiles(t, map[string]string{
		"a.gom": "package m\n\nlet answer = 42\n",
		"b.gom": "package m\n\nfn shadowed (answer: Int) -> Int = answer\nfn ask () -> Int = answer\n",
	})
	for i, r := range rs {
		if r.Diagnostics.HasError() {
			t.Fatalf("result %d diagnostics: %v", i, r.Diagnostics)
		}
	}
	b := string(rs[1].Output.Source)
	if !strings.Contains(b, "answer1") {
		t.Fatalf("b.go does not mangle the shadowing param:\n%s", b)
	}
	if !strings.Contains(b, "return answer\n") {
		t.Fatalf("b.go does not reference the sibling package var:\n%s", b)
	}
}

// A sibling `type Option` shadows the builtin package-wide: this
// file's references must emit the local Option_ variant names, not
// the stdlib option import — the builtin map is keyed by localT,
// which the unit pass fills from Unit.Types.
func TestCompileFilesSiblingTypeShadowsBuiltin(t *testing.T) {
	rs := compileFiles(t, map[string]string{
		"a.gom": "package m\n\ntype Option =\n  | Yep (v: Int)\n  | Nope\n",
		"b.gom": "package m\n\nfn pick () -> Int = match Yep 1 {\n  Yep (v) -> v\n  Nope -> 0\n}\n",
	})
	for i, r := range rs {
		if r.Diagnostics.HasError() {
			t.Fatalf("result %d diagnostics: %v", i, r.Diagnostics)
		}
	}
	b := string(rs[1].Output.Source)
	if strings.Contains(b, "stdlib/option") || strings.Contains(b, "option.") {
		t.Fatalf("b.go treats the sibling Option as the builtin:\n%s", b)
	}
	if !strings.Contains(b, "Option_Yep") {
		t.Fatalf("b.go does not emit the local variant name:\n%s", b)
	}
}

// Hidden package-scope temporaries of non-trivial `let` patterns
// carry the file tag: sibling members share the emitted Go package's
// namespace, so two files each minting `var tmp1` collide at `go
// build` (`tmp1 redeclared`) even though gom build succeeds. The
// scratch-module build is the checker's real gate — the generated
// sources must compile together.
func TestCompileFilesTmpVarsDontCollide(t *testing.T) {
	rs := compileFiles(t, map[string]string{
		"a.gom": "package m\n\nlet _ = 40\ntype W = | W (x: Int) (y: Int)\nlet (a1, a2) = (1, 2)\nlet W wa wb = W 3 4\n",
		"b.gom": "package m\n\nlet _ = 41\nlet (b1, b2) = (5, 6)\nlet W _ _ = W 7 8\n",
	})
	gobin, err := lookupGo(t)
	if err != nil {
		t.Skip("no go toolchain on PATH")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	gomod := "module e2e\n\ngo 1.27\n\nrequire github.com/podhmo/gom v0.0.0\n\nreplace github.com/podhmo/gom => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, r := range rs {
		if r.Diagnostics.HasError() {
			t.Fatalf("result %d diagnostics: %v", i, r.Diagnostics)
		}
		if r.Output == nil {
			t.Fatalf("result %d produced no output", i)
		}
		name := fmt.Sprintf("m%d.go", i)
		if err := os.WriteFile(filepath.Join(dir, name), r.Output.Source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(gobin, "build", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated package failed: %v\noutput: %s", err, out)
	}
}

// Two sibling files declaring `test "x"` would both emit
// `func TestX` into one Go package — the colliding base takes the
// file's fnv32a tag, so each file's emission stays deterministic and
// distinct. A name no sibling shares keeps the bare form.
func TestCompileFilesTestNameCollision(t *testing.T) {
	var ins []build.Input
	for _, n := range []string{"a.gom", "b.gom"} {
		src := map[string]string{
			"a.gom": "package m\n\ntest \"x\" {\n  assert true\n}\ntest \"only a\" {\n  assert true\n}\n",
			"b.gom": "package m\n\ntest \"x\" {\n  assert true\n}\n",
		}[n]
		ins = append(ins, build.Input{Path: "pkg/" + n, Src: []byte(src)})
	}
	rs, err := build.CompileFiles(ins, &build.Options{Lower: &lower.Env{Tests: true}})
	if err != nil {
		t.Fatalf("CompileFiles: %v", err)
	}
	var colliding, all []string
	for i, r := range rs {
		if r.Diagnostics.HasError() {
			t.Fatalf("result %d diagnostics: %v", i, r.Diagnostics)
		}
		if r.Output == nil {
			t.Fatalf("result %d produced no output", i)
		}
		for _, m := range r.Output.Tests {
			all = append(all, m.Func)
			if m.Name == "x" {
				colliding = append(colliding, m.Func)
			} else if m.Func != "TestOnlyA" {
				t.Errorf("unshared test name emitted as %q, want TestOnlyA", m.Func)
			}
		}
	}
	if len(all) != 3 {
		t.Fatalf("emitted test funcs = %v, want 3", all)
	}
	if len(colliding) != 2 || colliding[0] == colliding[1] {
		t.Fatalf("colliding test funcs = %v, want two distinct names", colliding)
	}
	for _, f := range colliding {
		if !strings.HasPrefix(f, "TestX_") {
			t.Errorf("colliding test name %q lacks the file-tag suffix", f)
		}
	}
}

// An exported `let foo` wants the Go name `Foo` — colliding with
// `type Foo`, which keeps its exact spelling: the value dedups to
// `Foo1`. The same rule holds whether both live in one file or in
// siblings — unit-wide claiming keeps the two files consistent.
func TestCompileFilesExportedLetVsTypeCollision(t *testing.T) {
	var ins []build.Input
	for _, n := range []string{"a.gom", "b.gom"} {
		src := map[string]string{
			"a.gom": "package m\n\ntype Foo = { x: Int }\n",
			"b.gom": "package m\n\nlet foo = { x = 1 }\n",
		}[n]
		ins = append(ins, build.Input{Path: "pkg/" + n, Src: []byte(src)})
	}
	rs, err := build.CompileFiles(ins, &build.Options{
		Exports: func(dir, pkg string) map[string]bool {
			return map[string]bool{"foo": true}
		},
	})
	if err != nil {
		t.Fatalf("CompileFiles: %v", err)
	}
	var bSrc string
	for i, r := range rs {
		if r.Diagnostics.HasError() {
			t.Fatalf("result %d diagnostics: %v", i, r.Diagnostics)
		}
		if r.Output == nil {
			t.Fatalf("result %d produced no output", i)
		}
		if ins[i].Path == "pkg/b.gom" {
			bSrc = string(r.Output.Source)
		}
	}
	if !strings.Contains(bSrc, "var Foo1 Foo") {
		t.Fatalf("exported `let foo` did not dedup against `type Foo`:\n%s", bSrc)
	}
	if strings.Contains(bSrc, "var Foo ") {
		t.Fatalf("the type's name leaked onto the value:\n%s", bSrc)
	}
	// And a.gom's `type Foo` still emits verbatim.
	aSrc := string(rs[0].Output.Source)
	if !strings.Contains(aSrc, "type Foo struct") {
		t.Fatalf("the type lost its exact name:\n%s", aSrc)
	}
}

// The same collision inside a single file — the exported value
// dedups against the type the same way.
func TestCompileExportedLetVsTypeCollisionSingleFile(t *testing.T) {
	r := compile(t, `package m

type Foo = { x: Int }

let foo = { x = 1 }
`, &build.Options{
		Exports: func(dir, pkg string) map[string]bool {
			return map[string]bool{"foo": true}
		},
	})
	if r.Diagnostics.HasError() {
		t.Fatalf("diagnostics: %v", r.Diagnostics)
	}
	src := string(r.Output.Source)
	if !strings.Contains(src, "var Foo1 Foo") || !strings.Contains(src, "type Foo struct") {
		t.Fatalf("collision not resolved — want `var Foo1` + `type Foo`:\n%s", src)
	}
}

// Pattern/lower failures are unit failures: no member's output may
// be written while a sibling fails after the shared phases — a
// partial package would emit code referencing decls that never
// materialized.
func TestCompileFilesNoPartialOutputs(t *testing.T) {
	rs := compileFiles(t, map[string]string{
		"a.gom": "package m\n\nfn f () -> Int = 1\n",
		// non-exhaustive match: a pattern error, after the shared
		// resolve/check succeeded.
		"b.gom": "package m\n\ntype T =\n  | A\n  | B\n\nfn g (t: T) -> Int = match t {\n  A -> 1\n}\n",
	})
	var anyDiag bool
	for i, r := range rs {
		if r.Output != nil {
			t.Fatalf("result %d kept its output though a package member failed", i)
		}
		anyDiag = anyDiag || r.Diagnostics.HasError()
	}
	if !anyDiag {
		t.Fatal("expected the pattern failure to surface on a member result")
	}
}

// Inputs other than .gom (a .gomi in the batch) compile
// independently of the package unit and keep their own results.
func TestCompileFilesInterfaceInput(t *testing.T) {
	rs := compileFiles(t, map[string]string{
		"m.gom":  "package m\n\nfn f () -> Int = 1\n",
		"m.gomi": "package m\n\nfn f () -> Int\n",
	})
	if len(rs) != 2 {
		t.Fatalf("got %d results, want 2", len(rs))
	}
	for i, r := range rs {
		if r.Diagnostics.HasError() {
			t.Fatalf("result %d diagnostics: %v", i, r.Diagnostics)
		}
	}
}
