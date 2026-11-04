package gomimport

// Implementation-import cycle detection (E0606): the .gomi in-flight
// guard catches interface cycles, but .gom files of loaded packages
// may import back toward the entry package — a cycle the generated
// Go packages would only hit at `go build`. The loader scans .gom
// import clauses and reports a *resolve.ImportCycleError, which
// resolve surfaces as E0606 at the import site.

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
)

// compileEntry compiles the single .gom entry file in dir against a
// loader rooted at it, returning the compile result.
func compileEntry(t *testing.T, root, dir, src string) *build.Result {
	t.Helper()
	l := New(&Config{Dir: dir})
	r, err := build.Compile(
		build.Input{Path: filepath.Join(dir, "main.gom"), Src: []byte(src)},
		&build.Options{Loader: l, Sigs: l})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return r
}

func TestImplImportCycleMutual(t *testing.T) {
	// a imports b, b's .gom imports a back: the interfaces are
	// acyclic, so only the .gom-side scan sees the loop.
	root := writeMod(t, "example.com/m", map[string]string{
		"a/a.gom": `package main

import "example.com/m/b"

fn f () -> Int = b.g
`,
		"b/b.gom": `package b

import "example.com/m/a"

fn g () -> Int = 1
`,
		"b/b.gomi": `package b

fn g () -> Int
`,
	})
	r := compileEntry(t, root, filepath.Join(root, "a"), `package main

import "example.com/m/b"

fn f () -> Int = b.g
`)
	var codes []diag.Code
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
	}
	if len(codes) != 1 || codes[0] != "E0606" {
		t.Fatalf("codes = %v, want exactly E0606", codes)
	}
	d := r.Diagnostics[0]
	want := "example.com/m/a -> example.com/m/b -> example.com/m/a"
	if d.Data["cycle"] != want {
		t.Fatalf("cycle = %v, want %q", d.Data["cycle"], want)
	}
	if r.Output != nil {
		t.Error("output generated despite an import cycle")
	}
}

func TestImplImportCycleTransitive(t *testing.T) {
	// a -> b -> c -> a: a three-package loop, reported with the full
	// chain.
	root := writeMod(t, "example.com/m", map[string]string{
		"a/a.gom": `package main

import "example.com/m/b"
`,
		"b/b.gom": `package b

import "example.com/m/c"
`,
		"b/b.gomi": `package b
`,
		"c/c.gom": `package c

import "example.com/m/a"
`,
		"c/c.gomi": `package c
`,
	})
	r := compileEntry(t, root, filepath.Join(root, "a"), `package main

import "example.com/m/b"
`)
	var codes []diag.Code
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
	}
	if len(codes) != 1 || codes[0] != "E0606" {
		t.Fatalf("codes = %v, want exactly E0606", codes)
	}
	want := "example.com/m/a -> example.com/m/b -> example.com/m/c -> example.com/m/a"
	if got := r.Diagnostics[0].Data["cycle"]; got != want {
		t.Fatalf("cycle = %v, want %q", got, want)
	}
}

func TestImplImportCycleSelf(t *testing.T) {
	// A package that imports its own path is a cycle of one.
	root := writeMod(t, "example.com/m", map[string]string{
		"a/a.gom": `package main

import "example.com/m/a"
`,
		"a/a.gomi": `package a
`,
	})
	r := compileEntry(t, root, filepath.Join(root, "a"), `package main

import "example.com/m/a"
`)
	var codes []diag.Code
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
	}
	if len(codes) != 1 || codes[0] != "E0606" {
		t.Fatalf("codes = %v, want exactly E0606", codes)
	}
	want := "example.com/m/a -> example.com/m/a"
	if got := r.Diagnostics[0].Data["cycle"]; got != want {
		t.Fatalf("cycle = %v, want %q", got, want)
	}
}

func TestImplImportNoCycleWhenOnlyIfaceDepends(t *testing.T) {
	// a.gom imports b; b's .gomi imports a back — an interface
	// edge, not an implementation edge. The nested .gomi load must
	// succeed, so a needs an interface of its own.
	root := writeMod(t, "example.com/m", map[string]string{
		"a/a.gom": `package main

import "example.com/m/b"

fn f () -> Int = b.g ()
`,
		"a/a.gomi": `package a

type A
`,
		"b/b.gom": `package b

fn g () -> Int = 1
`,
		"b/b.gomi": `package b

import "example.com/m/a"

fn g () -> Int
fn h () -> a.A
`,
	})
	r := compileEntry(t, root, filepath.Join(root, "a"), `package main

import "example.com/m/b"

fn f () -> Int = b.g ()
`)
	// The nested .gomi load must succeed outright — a back-edge
	// that only fails differently would still pass a bare "no
	// E0606" check.
	for range r.Diagnostics {
		t.Fatalf("unexpected diagnostic: %v", r.Diagnostics)
	}
}

func TestImplImportCleanDepOutsideCycle(t *testing.T) {
	// a imports b (which loops through c) and d (clean). The cycle
	// verdict must stick to the loop's members: importing d is not a
	// cycle edge and gets no E0606, while importing b still does.
	root := writeMod(t, "example.com/m", map[string]string{
		"a/a.gom": `package main

import "example.com/m/b"
import "example.com/m/d"

fn f () -> Int = d.h ()
`,
		"b/b.gom": `package b

import "example.com/m/c"

fn g () -> Int = 1
`,
		"b/b.gomi": `package b

fn g () -> Int
`,
		"c/c.gom": `package c

import "example.com/m/b"
`,
		"d/d.gom": `package d

fn h () -> Int = 1
`,
		"d/d.gomi": `package d

fn h () -> Int
`,
	})
	r := compileEntry(t, root, filepath.Join(root, "a"), `package main

import "example.com/m/b"
import "example.com/m/d"

fn f () -> Int = d.h ()
`)
	var codes []diag.Code
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
		if d.Code == "E0606" {
			if got := d.Data["path"]; got != "example.com/m/b" {
				t.Fatalf("E0606 path = %v, want the cycle member example.com/m/b", got)
			}
			if got := d.Data["cycle"]; got != "example.com/m/b -> example.com/m/c -> example.com/m/b" {
				t.Fatalf("cycle = %v", got)
			}
		}
	}
	if len(codes) != 1 || codes[0] != "E0606" {
		t.Fatalf("codes = %v, want exactly E0606", codes)
	}
}

func TestLoadReturnsTypedCycleError(t *testing.T) {
	// The raw Loader hands out a *resolve.ImportCycleError — resolve
	// needs the type to pick E0606 over E0203.
	root := writeMod(t, "example.com/m", map[string]string{
		"a/a.gom": `package main

import "example.com/m/b"
`,
		"b/b.gom": `package b

import "example.com/m/a"
`,
		"b/b.gomi": `package b
`,
	})
	l := New(&Config{Dir: filepath.Join(root, "a")})
	_, err := l.Load("example.com/m/b", false)
	var cyc *resolve.ImportCycleError
	if !errors.As(err, &cyc) {
		t.Fatalf("err = %v, want *resolve.ImportCycleError", err)
	}
	if len(cyc.Chain) < 2 || cyc.Chain[0] != "example.com/m/a" || cyc.Chain[len(cyc.Chain)-1] != "example.com/m/a" {
		t.Fatalf("chain = %v", cyc.Chain)
	}
}
