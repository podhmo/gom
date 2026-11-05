package fuzzutil

import (
	"fmt"
	"go/ast"
	"go/importer"
	goparser "go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// vetSnippet parses one generated-style Go file, optionally typechecks
// it (a nil *types.Info exercises the AST-only degradation path), and
// returns VetGenerated's findings as "check: message" strings.
func vetSnippet(t *testing.T, src string, withTypes bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "gen.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	var info *types.Info
	if withTypes {
		info = &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Defs:       map[*ast.Ident]types.Object{},
			Uses:       map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
		}
		conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
		if _, err := conf.Check(f.Name.Name, fset, []*ast.File{f}, info); err != nil {
			t.Fatalf("typecheck: %v\n%s", err, src)
		}
	}
	var got []string
	for _, f := range VetGenerated(fset, []*ast.File{f}, info) {
		got = append(got, f.Check+": "+f.Msg)
	}
	return got
}

func checkVet(t *testing.T, src string, withTypes bool, want []string) {
	t.Helper()
	got := vetSnippet(t, src, withTypes)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("VetGenerated mismatch\ngot:  %v\nwant: %v\n---\n%s", got, want, src)
	}
}

func TestVetUnreachable(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{"after return", `package m
func f() int { return 1; return 2 }
`,
			[]string{"unreachable: unreachable code"}},
		{"after terminating if/else", `package m
func f(c bool) int {
	if c {
		return 1
	} else {
		return 2
	}
	return 3
}
`,
			[]string{"unreachable: unreachable code"}},
		{"after panic", `package m
func f() { panic("x"); println("y") }
`,
			[]string{"unreachable: unreachable code"}},
		{"after infinite for", `package m
func f() { for { println(1) }; println(2) }
`,
			[]string{"unreachable: unreachable code"}},
		{"after select all arms terminate", `package m
func f(ch chan int) int {
	select {
	case <-ch:
		return 1
	default:
		return 2
	}
	println(3)
	return 0
}
`,
			[]string{"unreachable: unreachable code"}},
		{"goto target stays reachable", `package m
func f() { goto L; println(1); L: println(2) }
`,
			[]string{"unreachable: unreachable code"}},
		// Negative cases.
		{"if without else", `package m
func f(c bool) { if c { return }; println(1) }
`, nil},
		{"for with break", `package m
func f() { for { break }; println(1) }
`, nil},
		{"if/else assigning (branching value)", `package m
func f(t bool) int {
	var y int64
	if t {
		y = 1
	} else {
		y = 0 - 1
	}
	return y
}
`, nil},
		{"tailloop shape: continue in else", `package m
func keep(n int64, k func(struct{}) int64) int64 {
	for {
		n1 := n
		tmp3 := n1 <= 0
		if tmp3 {
			return k(struct{}{})
		} else {
			tmp2 := n1 - 1
			n, k = tmp2, k
			continue
		}
	}
}
`, nil},
		{"type switch with panic default", `package m
type I interface{ isI() }
type A struct{ V0 int64 }

func (A) isI() {}
func f(x I) int {
	switch v := x.(type) {
	case A:
		tmp := v
		return tmp.V0
	default:
		panic("bad")
	}
}
`, nil},
		{"declared panic is not the builtin", `package m
func f() {
	panic := func(string) {}
	panic("x")
	println("y")
}
`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkVet(t, tc.src, false, tc.want)
		})
	}
}

func TestVetAssign(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{"plain self-assign", `package m
func f(x int) { x = x }
`,
			[]string{"assign: self-assignment of x"}},
		{"field self-assign", `package m
type S struct{ b int }

func f(s S) { s.b = s.b }
`,
			[]string{"assign: self-assignment of s.b"}},
		{"slice index self-assign", `package m
func f(a []int) { a[0] = a[0] }
`,
			[]string{"assign: self-assignment of a[0]"}},
		{"swap is not self-assign", `package m
func f(x, y int) { x, y = y, x }
`, nil},
		{"map index is not judged", `package m
func f(m map[int]int, k int) { m[k] = m[k] }
`, nil},
		{"define is not assign", `package m
func f() { x := 1; _ = x }
`, nil},
		{"call rhs has effects", `package m
func g() int { return 1 }
func f(x int) { x = g() }
`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkVet(t, tc.src, true, tc.want)
		})
	}
}

func TestVetUnusedResult(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 0.91)
	}

	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{"fmt.Sprintf dropped", `package m
import "fmt"
func f() { fmt.Sprintf("%d", 1) }
`,
			[]string{"unusedresult: result of fmt.Sprintf call not used"}},
		{"errors.New dropped", `package m
import "errors"
func f() { errors.New("x") }
`,
			[]string{"unusedresult: result of errors.New call not used"}},
		{"slices.Clone dropped", `package m
import "slices"
func f(xs []int) { slices.Clone(xs) }
`,
			[]string{"unusedresult: result of slices.Clone call not used"}},
		{"Error method dropped", `package m
type E struct{}

func (E) Error() string { return "e" }
func f(e E) { e.Error() }
`,
			[]string{"unusedresult: result of (m.E).Error call not used"}},
		{"String method dropped", `package m
type S struct{}

func (S) String() string { return "s" }
func f(s S) { s.String() }
`,
			[]string{"unusedresult: result of (m.S).String call not used"}},
		// Negative cases.
		{"fmt.Println is allowed", `package m
import "fmt"
func f() { fmt.Println("x") }
`, nil},
		{"ordinary call is allowed", `package m
func compute(int) int { return 1 }
func f() { compute(40) }
`, nil},
		{"builtin call is allowed", `package m
func f() { println("x") }
`, nil},
		{"result used", `package m
import "fmt"
func f() string { return fmt.Sprintf("%d", 1) }
`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkVet(t, tc.src, true, tc.want)
		})
	}
}

func TestVetStringIntConv(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{"string(int)", `package m
func f(i int) string { return string(i) }
`,
			[]string{"stringintconv: conversion from int to string yields a string of one rune, not a string of digits"}},
		{"string(int64)", `package m
func f(i int64) string { return string(i) }
`,
			[]string{"stringintconv: conversion from int64 to string yields a string of one rune, not a string of digits"}},
		{"string(int32) is rune-kind", `package m
func f(i int32) string { return string(i) }
`, nil},
		{"named string target", `package m
type S string

func f(i int) S { return S(i) }
`,
			[]string{"stringintconv: conversion from int to m.S yields a string of one rune, not a string of digits"}},
		{"untyped constant", `package m
func f() string { return string(65) }
`,
			[]string{"stringintconv: conversion from untyped int to string yields a string of one rune, not a string of digits"}},
		// Negative cases — Char display emits string(rune).
		{"string(rune)", `package m
func f(c rune) string { return string(c) }
`, nil},
		{"string(byte)", `package m
func f(b byte) string { return string(b) }
`, nil},
		{"non-numeric source", `package m
func f(s string) string { return string(s) }
`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkVet(t, tc.src, true, tc.want)
		})
	}
}

func TestVetShadow(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want []string
	}{
		{"body var shadows param", `package m
func f(x int) { { x := 1; _ = x } }
`,
			[]string{`shadow: declaration of "x" shadows declaration at gen.go:2:8`}},
		{"inner block shadows outer", `package m
func f() { y := 1; { y := 2; _ = y }; _ = y }
`,
			[]string{`shadow: declaration of "y" shadows declaration at gen.go:2:12`}},
		{"for init shadows param", `package m
func f(i int) { for i := 0; i < 3; i++ { println(i) } }
`,
			[]string{`shadow: declaration of "i" shadows declaration at gen.go:2:8`}},
		{"type switch guard shadows outer", `package m
func f(x any) {
	v := 1
	_ = v
	switch v := x.(type) {
	case int:
		_ = v
	}
}
`,
			[]string{`shadow: declaration of "v" shadows declaration at gen.go:3:2`}},
		{"func literal param shadows outer", `package m
func f() { x := 1; _ = func(x int) { println(x) }; _ = x }
`,
			[]string{`shadow: declaration of "x" shadows declaration at gen.go:2:12`}},
		{"range var shadows outer", `package m
func f(xs []int) { i := 0; _ = i; for i := range xs { println(i) } }
`,
			[]string{`shadow: declaration of "i" shadows declaration at gen.go:2:20`}},
		{"named result shadows param", `package m
func f(y int) (y2 int) { { y2 := y; _ = y2 }; return }
`,
			[]string{`shadow: declaration of "y2" shadows declaration at gen.go:2:16`}},
		// Negative cases — generated code patterns.
		{"same-scope redeclare is legal", `package m
func g() (int, error) { return 0, nil }
func f() { v, err := g(); v2, err := g(); _, _, _ = v, v2, err }
`, nil},
		{"sibling scopes do not collide", `package m
func f() { { y := 1; _ = y }; { y := 2; _ = y } }
`, nil},
		{"loop scope is a sibling", `package m
func f() { for i := 0; i < 3; i++ { y := i; _ = y }; y := 1; _ = y }
`, nil},
		{"type switch cases do not collide", `package m
func f(x any) {
	switch v := x.(type) {
	case int:
		_ = v
	case string:
		_ = v
	}
}
`, nil},
		{"select arms do not collide", `package m
func f(ch chan int) {
	select {
	case x := <-ch:
		_ = x
	case y := <-ch:
		_ = y
	default:
	}
}
`, nil},
		{"uniquified generated names", `package m
func f(x int64) int64 {
	tmp1 := x > 0
	var y int64
	if tmp1 {
		y = x
	} else {
		y = 0 - x
	}
	y1 := y + 1
	return y1
}
`, nil},
		{"underscores are ignored", `package m
func f() { _ = func(_ struct{}) { _ = 1 } }
`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkVet(t, tc.src, true, tc.want)
		})
	}
}

// Without types.Info only the AST-only checks (unreachable, shadow)
// run — the no-toolchain degradation of the canary.
func TestVetGeneratedNilInfo(t *testing.T) {
	got := vetSnippet(t, `package m
func f(x int) int {
	x = x
	return 1
	println(2)
	{
		x := 3
		_ = x
	}
	return x
}
`, false)
	wantChecks := []string{"unreachable", "shadow"}
	if len(got) != len(wantChecks) {
		t.Fatalf("got %v, want %d findings", got, len(wantChecks))
	}
	for i, g := range got {
		if !strings.HasPrefix(g, wantChecks[i]+":") {
			t.Errorf("finding %d = %q, want check %q", i, g, wantChecks[i])
		}
	}
}
