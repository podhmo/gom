package gomimport

// Per-type constructor tables: the loader publishes each sum type's
// variant-name -> ctor-symbol table (CtorsOf, resolve.CtorSource) so
// the `pkg.Type.Ctor` qualified spelling resolves in expressions and
// patterns. The end-to-end cases run the full build.Compile pipeline
// to prove resolve, check, and codegen agree on the spelling.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/build"
)

func TestLoadTypeCtors(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"shapes/shapes.gomi": `package shapes

type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)

type Point = { x: Float, y: Float }
`,
	})
	l := New(&Config{Dir: filepath.Join(root, "app")})
	p, err := l.Load("example.com/m/shapes", false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	shape := p.Members["Shape"]
	if shape == nil {
		t.Fatal("Shape member missing")
	}
	ctors := l.CtorsOf(shape)
	if len(ctors) != 2 || ctors["Circle"] == nil || ctors["Rect"] == nil {
		t.Fatalf("CtorsOf(Shape) = %v, want Circle+Rect", ctors)
	}
	for name, cs := range ctors {
		if cs.Package != "example.com/m/shapes" || cs.Name != name {
			t.Errorf("ctor %s = %#v — package/name not requalified", name, cs)
		}
	}
	// A record type has no constructors.
	point := p.Members["Point"]
	if point == nil {
		t.Fatal("Point member missing")
	}
	if ct := l.CtorsOf(point); ct != nil {
		t.Fatalf("CtorsOf(Point) = %v, want nil", ct)
	}
	// And the member table still hands out the variants directly.
	if p.Members["Circle"] != ctors["Circle"] {
		t.Error("member table and ctor table disagree on Circle")
	}
}

func TestPkgTypeCtorEndToEnd(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"shapes/shapes.gomi": `package shapes

type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)

fn area (s: Shape) -> Float
`,
	})
	l := New(&Config{Dir: filepath.Join(root, "app")})
	src := []byte(`package main

import "example.com/m/shapes"

fn describe (s: shapes.Shape) -> String = match s {
  shapes.Shape.Circle r -> "circle ${r}"
  shapes.Shape.Rect w h -> "rect ${w} ${h}"
}

fn mk () -> shapes.Shape = shapes.Shape.Circle 1.0
`)
	r, err := build.Compile(
		build.Input{Path: filepath.Join(root, "app", "app.gom"), Src: src},
		&build.Options{Loader: l, Sigs: l})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(r.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", r.Diagnostics)
	}
	if r.Output == nil {
		t.Fatal("no output")
	}
	for _, want := range []string{"shapes.Shape_Circle{", "case shapes.Shape_Circle:", "case shapes.Shape_Rect:"} {
		if !strings.Contains(string(r.Output.Source), want) {
			t.Errorf("generated source lacks %q\n%s", want, r.Output.Source)
		}
	}
}

func TestPkgTypeCtorUnknownName(t *testing.T) {
	root := writeMod(t, "example.com/m", map[string]string{
		"shapes/shapes.gomi": `package shapes

type Shape =
  | Circle (radius: Float)
`,
	})
	l := New(&Config{Dir: filepath.Join(root, "app")})
	src := []byte(`package main

import "example.com/m/shapes"

fn mk () -> shapes.Shape = shapes.Shape.Square 1.0
`)
	r, err := build.Compile(
		build.Input{Path: filepath.Join(root, "app", "app.gom"), Src: src},
		&build.Options{Loader: l, Sigs: l})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	found := false
	for _, d := range r.Diagnostics {
		if d.Code == "E0201" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want E0201 for `shapes.Shape.Square`, got %v", r.Diagnostics)
	}
	if r.Output != nil {
		t.Error("output generated despite an unresolved ctor")
	}
}
