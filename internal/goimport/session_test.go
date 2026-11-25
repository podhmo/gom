package goimport

import (
	"fmt"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	gt "github.com/podhmo/gom/internal/types"
)

// The dependency is a separate module reached through replace. This exercises
// the configured source module, without the network or the test process's cwd.
func moduleLoader(t *testing.T) (*Loader, string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":            fmt.Sprintf("module example.test/bridge\n\ngo 1.27\n\nrequire (\nexample.test/dependency v0.0.0\ngithub.com/podhmo/gom v0.0.0\n)\nreplace example.test/dependency => ./dependency\nreplace github.com/podhmo/gom => %s\n", repoRoot(t)),
		"dependency/go.mod": "module example.test/dependency\n\ngo 1.27\n",
		"dependency/model/model.go": `package model
 import "cmp"
 type Value struct{ N int64 }
 type Reader interface { Read() string }
 type Failure = error
 type FailureAgain = Failure
 func Fail() FailureAgain { return nil }
 func New(n int64) Value { return Value{n} }
 func Number(v Value) int64 { return v.N }
 func Slice() []string { return nil }
 func EchoSlice(s []string) []string { return s }
 func EchoReader(r Reader) Reader { return r }
 func Unsupported(f func(int, int, int, int, int)) {}
 func NewMap() map[string]int64 { return map[string]int64{"a": 1, "b": 2} }
 func Identity[T any](v T) T { return v }
 // Second's declared parameter order (A, B) is the reverse of first
 // occurrence (B then A) — the emitted instantiation must keep the
 // declared order.
 func Second[A any, B any](b B, a A) A { return a }
 func Count[E comparable](xs []E, v E) int64 { var n int64; for _, x := range xs { if x == v { n++ } }; return n }
 func Lookup[K comparable, V any](m map[K]V, k K) (V, bool) { v, ok := m[k]; return v, ok }
 func Keys[K comparable, V any](m map[K]V) []K { ks := []K{}; for k := range m { ks = append(ks, k) }; return ks }
 // Index's S parameter carries a structural ~[]E constraint —
 // admitted by the widened callable set (spec 8.5), with E's
 // repr checked against the slice element at the call site.
 func Index[S ~[]E, E comparable](s S, v E) int { for i, x := range s { if x == v { return i } }; return -1 }
 // Clamp's ~int and G's int|string type sets map to union
 // constraints — callable, with each argument's repr checked
 // against the terms at the call site (E0508).
 func Clamp[T ~int](x, lo, hi T) T { if x < lo { return lo }; if x > hi { return hi }; return x }
 func G[T int|string](v T) T { return v }
 // Run's method-set constraint stays outside the callable set
 // (E0506) — a named interface constraint is an intersection
 // with a method set, not a union of concrete terms.
 type Caller interface { Call() int64 }
 func Run[T Caller](t T) int64 { return t.Call() }
 // First's constraint spells the identical type set as comparable —
 // it stays callable.
 func First[T interface{ comparable }](a, b T) T { return a }
 // Wrapped's constraint is a named union interface — admitted like
 // the direct "T cmp.Ordered" spelling.
 func Wrapped[T interface{ cmp.Ordered }](a, b T) int { return cmp.Compare(a, b) }
 // Rec's constraint references the constrained parameter itself —
 // hidden; the recursive shape has no Gom spelling.
 func Rec[S ~[]S](s S) S { return s }
 // Inter is an intersection of unions — hidden; admitting its
 // flattened terms would weaken the type set.
 func Inter[T interface{ ~int64 | ~string; ~int64 | ~bool }](v T) T { return v }
 // Bytef's ~[]byte and ByteOrStr's []byte|string are concrete
 // unions — List arguments are rejected honestly: the emitted
 // named list.List is no member, and a verbatim pass would share
 // the backing with a callee free to mutate it.
 func Bytef[T ~[]byte](b T) int { return len(b) }
 func ByteOrStr[T []byte | string](v T) int { return len(v) }
 // Sum's []int and Tally's map[string]int split the Int/Go.Int view
 // inside a container — an element-wise conversion the boundary
 // cannot spell yet, so both stay hidden (E0501).
 func Sum(xs []int) int64 { var n int64; for _, x := range xs { n += int64(x) }; return n }
 func Tally() map[string]int { return map[string]int{"a": 1} }
 // Dup nests a view-equal []string — representable, stays exposed.
 func Dup(xs [][]string) [][]string { return xs }
 // EchoBytes / NthByte exercise the byte (uint8) → Byte
 // mapping in both directions.
 func EchoBytes(b []byte) []byte { return b }
 func NthByte(b []byte, i int64) byte { return b[i] }
 func Parse(ok bool) (Value, Failure) {
  if !ok { return Value{}, bad{} }; return New(42), nil
 }
 type bad struct{}
 func (bad) Error() string { return "bad" }
 `,
		"dependency/service/service.go": `package service
 import "example.test/dependency/model"
 type Value struct{ N int64 }
 type Alias = model.Value
 func Echo(v model.Value) model.Value { return v }
 `,
	}
	for path, src := range files {
		p := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return New(&Config{Dir: dir}), dir
}

func TestModuleSessionIdentity(t *testing.T) {
	t.Parallel()

	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprint(reverse), func(t *testing.T) {
			l, _ := moduleLoader(t)
			paths := []string{"example.test/dependency/model", "example.test/dependency/service"}
			if reverse {
				paths[0], paths[1] = paths[1], paths[0]
			}
			for _, p := range paths {
				membersOf(t, l, p)
			}
			model := membersOf(t, l, "example.test/dependency/model")
			service := membersOf(t, l, "example.test/dependency/service")
			again := membersOf(t, l, "example.test/dependency/model")
			if model != again || model.Members["Value"] != again.Members["Value"] {
				t.Fatal("package/symbol identity was not cached")
			}
			original := l.syms[model.Members["Value"]].obj.Type()
			alias := l.syms[service.Members["Alias"]].obj.Type()
			echo := l.syms[service.Members["Echo"]].sig
			if !types.Identical(original, alias) || !types.Identical(original, echo.Params().At(0).Type()) || !types.Identical(original, echo.Results().At(0).Type()) {
				t.Fatal("named identity lost across imports")
			}
			other := l.syms[service.Members["Value"]].obj.Type()
			if types.Identical(original, other) {
				t.Fatal("distinct named types were conflated")
			}
			a, _ := l.SignatureOf(model.Members["Value"])
			b, _ := l.SignatureOf(service.Members["Alias"])
			c, _ := l.SignatureOf(service.Members["Value"])
			if !gt.Equal(a.Type, b.Type) || gt.Equal(a.Type, c.Type) {
				t.Fatal("Gom mapping lost package identity or alias transparency")
			}
		})
	}
}

func TestAliasedErrorResults(t *testing.T) {
	t.Parallel()

	l, _ := moduleLoader(t)
	p := membersOf(t, l, "example.test/dependency/model")
	sig, ok := l.SignatureOf(p.Members["Parse"])
	if !ok || sig.Type.String() != "Bool -> Result example.test/dependency/model.Value GoError" {
		t.Fatalf("Parse = %v, want Bool -> Result model.Value GoError", sig.Type)
	}
	only, ok := l.SignatureOf(p.Members["Fail"])
	if !ok || only.Type.String() != " -> Result Unit GoError" {
		t.Fatalf("Fail = %v, want niladic Result Unit GoError", only.Type)
	}
	if gs, ok := l.GoSigOf(p.Members["Fail"]); !ok || gs.Results != core.GoResultsErrOnly {
		t.Fatalf("Go result shape = %v, want error-only", gs.Results)
	}
	gs, ok := l.GoSigOf(p.Members["Parse"])
	if !ok || gs.Results != core.GoResultsErr {
		t.Fatalf("Go result shape = %v, want error pair", gs.Results)
	}
}

func TestUnsupportedMemberOnlyDiagnosedOnUse(t *testing.T) {
	t.Parallel()

	l, _ := moduleLoader(t)
	for _, use := range []bool{false, true} {
		src := "package bridge\nimport go \"example.test/dependency/model\"\nfn value () -> Int = model.Number (model.New 42)\n"
		if use {
			src += "let bad = model.Unsupported\n"
		}
		sf, ds := syntax.Parse([]byte(src), "module.gom")
		if requireNoErrors(t, ds) {
			t.Fatal("parse")
		}
		rf, ds := resolve.File(sf, &resolve.Config{Loader: l})
		if requireNoErrors(t, ds) {
			t.Fatal("resolve")
		}
		_, ds = check.Check(rf, l)
		if !use {
			if requireNoErrors(t, ds) {
				t.Fatal("unused hidden member caused error")
			}
			continue
		}
		if len(ds) != 1 || ds[0].Code != "E0501" || !strings.Contains(ds[0].Message, "func types") {
			t.Fatalf("diagnostics = %+v", ds)
		}
	}
}

// Go calls the generated Gom wrappers, which call the replaced Go dependency.
// Named values/interfaces and nil slices pass through both sides unchanged;
// the []T boundary copies per spec section 8.5, even when the Gom
// function's own signature spells the Go-owned `Go.Slice` view.
func TestModuleInteropBothDirections(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.17)
	}

	t.Parallel()

	l, dir := moduleLoader(t)
	src := `package bridge
 import go "example.test/dependency/model"
 import go "example.test/dependency/service"
 fn roundtrip (n: Int) -> Int = model.Number (service.Echo (model.New n))
 fn slice (xs: Go.Slice String) -> Go.Slice String = model.EchoSlice xs
 fn reader (r: model.Reader) -> model.Reader = model.EchoReader r
 fn parse (ok: Bool) -> Result model.Value GoError = model.Parse ok
 fn fail () -> Result Unit GoError = model.Fail ()
 fn echo_bytes (xs: Go.Slice Byte) -> Go.Slice Byte = model.EchoBytes xs
 fn nth_byte (xs: Go.Slice Byte) (i: Int) -> Byte = model.NthByte xs i
 `
	gen := compileSource(t, l, src)
	test := `package bridge
 import (
  "testing"
  "example.test/dependency/model"
 )
 type readerValue struct{}
 func (readerValue) Read() string { return "ok" }
 func TestBridge(t *testing.T) {
  if n := roundtrip(42); n != 42 { t.Fatal(n) }
  if xs := slice(nil); xs != nil { t.Fatalf("nil slice became %#v",xs) }
  xs := []string{"a"}; ys := slice(xs); ys[0]="b"
  // EchoSlice's parameter is a Gom List — the Go.Slice argument is
  // copied into it, and the List result copied back out, so writes
  // through ys never reach xs (spec section 8.5).
  if xs[0]!="a" {t.Fatal("List boundary shared the backing array")}
  if b := nthByte([]byte{0x61, 0x62}, 1); b != 'b' { t.Fatalf("nthByte = %v", b) }
  bs := []byte{0x61}; out := echoBytes(bs); out[0] = 0x63
  // Same List copy story as EchoSlice: []byte crosses as a List —
  // writes through out never reach bs (spec section 8.5).
  if bs[0]!=0x61 {t.Fatal("byte List boundary shared the backing array")}
  if out[0]!=0x63 || len(out)!=1 {t.Fatalf("echoBytes = %v", out)}
  if reader(nil)!=nil {t.Fatal("nil interface changed")}
  var r model.Reader = readerValue{}
  if reader(r) != r || reader(r).Read()!="ok" {t.Fatal("interface identity lost")}
  if v:=fail(struct{}{}); !v.IsOk() {t.Fatal(v)}
  if v:=parse(true); !v.IsOk() || v.Value.N!=42 {t.Fatal(v)}
  if v:=parse(false); v.IsOk() || v.Error.Error()!="bad" {t.Fatal(v)}
 }
 `
	for name, data := range map[string][]byte{"bridge.go": gen, "bridge_test.go": []byte(test)} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated bridge failed: %v\n%s\n%s", err, out, gen)
	}
}

// Generic Go callees instantiate by declared type-parameter order:
// `model.Identity n` infers T, `model.Second ("s", 1)` proves the
// emitted `Second[int64, string]` follows the declared (A, B) — not
// the first-occurrence (B, A). `model.Count`'s comparable parameter
// is solved to String and `model.Lookup`/`model.Keys` exercise the
// Go.Map boundary in both directions. A `List` crossing into `[]E`
// still copies, and nil lists stay nil (spec section 8.5).
func TestModuleInteropGenerics(t *testing.T) {
	if testing.Short() {
		t.Skipf("long test: %.2fs on last run, skipping in short mode", 1.13)
	}

	t.Parallel()

	l, dir := moduleLoader(t)
	src := `package bridge
 import go "example.test/dependency/model"
 import go "maps"
 fn ident (n: Int) -> Int = model.Identity n
 fn swap (s: String) (n: Int) -> Int = model.Second s n
 fn count (xs: List String) (s: String) -> Int = model.Count xs s
 fn lookup_or (m: Go.Map String Int) (k: String) -> Int = {
   let (v, ok) = model.Lookup m k
   if ok { v } else { 0 }
 }
 fn keys (m: Go.Map String Int) -> List String = model.Keys m
 fn newmap () -> Go.Map String Int = model.NewMap ()
 fn echo (xs: List String) -> List String = model.EchoSlice xs
 fn first (n: Int) (m: Int) -> Int = model.First n m
 fn dup (xs: List (Go.Slice String)) -> List (Go.Slice String) = model.Dup xs
 // The widened constraint set (spec 8.5): ~[]E admits
 // list.List[int64] as S with E=int64, ~map[K]V admits
 // map[string]int64 as M1/M2 with K=string V=int64, and
 // int | string admits String.
 fn index (xs: List Int) (n: Int) -> Int = model.Index xs n
 fn map_eq (a: Go.Map String Int) (b: Go.Map String Int) -> Bool = maps.Equal a b
 fn gstr (s: String) -> String = model.G s
 // The named-union spelling interface{cmp.Ordered} admits the
 // same type set as the direct T cmp.Ordered constraint.
 fn wrapped (a: Int) (b: Int) -> Int = model.Wrapped a b
 fn bytef (xs: Go.Slice Byte) -> Int = model.Bytef xs
 `
	gen := compileSource(t, l, src)
	if !strings.Contains(string(gen), "model.Second[int64, string](") {
		t.Fatalf("generated source lacks declared-order instantiation:\n%s", gen)
	}
	if !strings.Contains(string(gen), "model.Index[[]int64, int64](") ||
		!strings.Contains(string(gen), "xs.ToSlice()") {
		t.Fatalf("generated source lacks the ~[]E instantiation:\n%s", gen)
	}
	if !strings.Contains(string(gen), "maps.Equal[map[string]int64, map[string]int64, string, int64](a, b)") {
		t.Fatalf("generated source lacks the ~map[K]V instantiation:\n%s", gen)
	}
	test := `package bridge
 import (
  "testing"
  "github.com/podhmo/gom/stdlib/list"
 )
 func TestGenerics(t *testing.T) {
  if ident(42)!=42 {t.Fatal(ident(42))}
  if swap("s",7)!=7 {t.Fatal(swap("s",7))}
  xs:=list.List[string]{"a","b","a"}
  if count(xs,"a")!=2 {t.Fatal(count(xs,"a"))}
  // The copy crossing the []E boundary keeps writes on the other
  // side invisible here (spec section 8.5).
  xs[0]="z"
  if count(list.List[string]{"a","b","a"},"a")!=2 {t.Fatal("Count saw a mutated list")}
  m:=newmap(struct{}{})
  if m["a"]!=1 || m["b"]!=2 {t.Fatal(m)}
  if lookupOr(m,"a")!=1 || lookupOr(m,"z")!=0 {t.Fatal("lookupOr")}
  if len(keys(m))!=2 {t.Fatal(keys(m))}
  if echo(nil)!=nil {t.Fatal("nil list changed")}
  ys:=list.List[string]{"x"}
  if echo(ys)[0]!="x" {t.Fatal(echo(ys))}
  if first(3,4)!=3 {t.Fatal(first(3,4))}
  ns:=list.List[[]string]{{"a"}}
  if dup(ns)[0][0]!="a" {t.Fatal(dup(ns))}
  if index(list.List[int64]{7,8,9},8)!=1 {t.Fatal(index(list.List[int64]{7,8,9},8))}
  if !mapEq(m,m) {t.Fatal("mapEq")}
  if gstr("x")!="x" {t.Fatal(gstr("x"))}
  if wrapped(1,2)!=-1 {t.Fatal(wrapped(1,2))}
  if bytef([]byte("a"))!=1 {t.Fatal(bytef(nil))}
 }
 `
	for name, data := range map[string][]byte{"bridge.go": gen, "bridge_test.go": []byte(test)} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated bridge failed: %v\n%s\n%s", err, out, gen)
	}
}

// Generic Go diagnostics: a callee behind a method-set constraint
// reports E0506 at the use site, and a type argument that fails an
// admitted constraint — comparable, ~[]E, or a union term —
// reports E0508.
func TestModuleInteropGenericDiagnostics(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, body, code, reason string }{
		{"method_set", "fn f (v: Go.Any) -> Int = model.Run v", "E0506", "method set"},
		{"slice_elem_mismatch", "fn f (xs: List Int) (s: String) -> Int = model.Index xs s", "E0401", "expected Int, got String"},
		{"approx_subset", "fn f (n: Int) -> Int = model.Clamp n 0 9", "E0508", "~int"},
		{"union_subset", "fn f (n: Int) -> Int = model.G n", "E0508", "int | string"},
		{"split_slice", "fn f (xs: List Int) -> Int = model.Sum xs", "E0501", "element-wise"},
		{"split_map", "fn f () -> Go.Map String Int = model.Tally ()", "E0501", "element-wise"},
		{"non_comparable_arg", "fn f (xs: List (List Int)) (v: List Int) -> Int = model.Count xs v", "E0508", "comparable"},
		// Constraint classification regressions: a self-referencing
		// operand (~[]S) and an intersection of unions stay hidden;
		// a List argument never satisfies a slice-typed union term.
		{"recursive_constraint", "fn f (xs: List Int) -> List Int = model.Rec xs", "E0506", "type parameters"},
		{"union_intersection", "fn f (n: Int64) -> Int64 = model.Inter n", "E0506", "intersection"},
		{"approx_list_arg", "fn f (xs: List Byte) -> Int = model.Bytef xs", "E0508", "~[]byte"},
		{"union_list_arg", "fn f (xs: List Byte) -> Int = model.ByteOrStr xs", "E0508", "[]byte | string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, _ := moduleLoader(t)
			src := "package bridge\nimport go \"example.test/dependency/model\"\n" + tc.body + "\n"
			sf, ds := syntax.Parse([]byte(src), "generics.gom")
			if requireNoErrors(t, ds) {
				t.Fatal("parse")
			}
			rf, ds := resolve.File(sf, &resolve.Config{Loader: l})
			if requireNoErrors(t, ds) {
				t.Fatal("resolve")
			}
			_, ds = check.Check(rf, l)
			found := false
			for _, d := range ds {
				if string(d.Code) == tc.code && strings.Contains(d.Message, tc.reason) {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s containing %s; got %+v", tc.code, tc.reason, ds)
			}
		})
	}
}
