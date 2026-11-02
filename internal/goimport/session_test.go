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
 func Unsupported(f func()) {}
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
		if len(ds) != 1 || ds[0].Code != "E0501" || !strings.Contains(ds[0].Message, "func-typed") {
			t.Fatalf("diagnostics = %+v", ds)
		}
	}
}

// Go calls the generated Gom wrappers, which call the replaced Go dependency.
// Named values/interfaces and nil slices pass through both sides unchanged.
func TestModuleInteropBothDirections(t *testing.T) {
	l, dir := moduleLoader(t)
	src := `package bridge
 import go "example.test/dependency/model"
 import go "example.test/dependency/service"
 fn roundtrip (n: Int) -> Int = model.Number (service.Echo (model.New n))
 fn slice (xs: Go.Slice String) -> Go.Slice String = model.EchoSlice xs
 fn reader (r: model.Reader) -> model.Reader = model.EchoReader r
 fn parse (ok: Bool) -> Result model.Value GoError = model.Parse ok
 fn fail () -> Result Unit GoError = model.Fail ()
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
  if xs[0]!="b" {t.Fatal("Go.Slice was copied")}
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
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated bridge failed: %v\n%s\n%s", err, out, gen)
	}
}
