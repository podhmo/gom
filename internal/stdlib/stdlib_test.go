package stdlib_test

import (
	"testing"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gomstd "github.com/podhmo/gom/internal/stdlib"
)

// TestApplyWiresListModules pins what Apply installs into a
// compile: a `List` module table whose `map` member resolves to a
// package-level function symbol carrying the signature and declared
// arity of the embedded list.gomi — the same facts `gom check`
// enforces the .gom implementation against.
func TestApplyWiresListModules(t *testing.T) {
	o := gomstd.Apply(nil)
	if o == nil {
		t.Fatal("Apply(nil) returned nil")
	}

	table := o.Modules["List"]
	if table == nil {
		t.Fatalf("Modules has no List table; got %v", o.Modules)
	}
	sym := table["map"]
	if sym == nil {
		names := make([]string, 0, len(table))
		for n := range table {
			names = append(names, n)
		}
		t.Fatalf("List table has no `map`; members: %v", names)
	}
	if sym.Kind != resolve.KindFunc {
		t.Fatalf("List.map kind = %v, want KindFunc", sym.Kind)
	}
	if want := "github.com/podhmo/gom/stdlib/list"; sym.Package != want {
		t.Fatalf("List.map package = %q, want %q", sym.Package, want)
	}

	// The .gomi signature is the checker's view of the member:
	// `fn map (f: a -> b) (xs: List a) -> List b`.
	sig, ok := o.Sigs.SignatureOf(sym)
	if !ok {
		t.Fatal("Sigs has no signature for List.map")
	}
	if sig.DeclArity != 2 {
		t.Fatalf("List.map DeclArity = %d, want 2", sig.DeclArity)
	}
	if want := "(a -> b) -> List a -> List b"; sig.Type.String() != want {
		t.Fatalf("List.map type = %q, want %q", sig.Type.String(), want)
	}

	// Lowering sees the same arity through the installed FuncArity.
	if o.Lower == nil || o.Lower.FuncArity == nil {
		t.Fatal("Lower.FuncArity was not installed")
	}
	if got := o.Lower.FuncArity(sym); got != 2 {
		t.Fatalf("FuncArity(List.map) = %d, want 2", got)
	}

	// The other builtin modules land in the same table.
	for m, member := range map[string]string{
		"Option": "get",
		"Result": "map_err",
	} {
		if o.Modules[m][member] == nil {
			t.Fatalf("Modules[%q][%q] missing", m, member)
		}
	}
}

// TestApplyChainsExistingOracles makes sure wiring the stdlib does
// not drop oracles a caller already installed: FuncArity defers to
// the previous one for non-stdlib symbols.
func TestApplyChainsExistingOracles(t *testing.T) {
	other := &resolve.Symbol{Name: "f", Kind: resolve.KindFunc, Package: "x/y"}
	o := gomstd.Apply(&build.Options{
		Lower: &lower.Env{FuncArity: func(sym *resolve.Symbol) int {
			if sym == other {
				return 7
			}
			return -1
		}},
	})
	if got := o.Lower.FuncArity(other); got != 7 {
		t.Fatalf("chained FuncArity = %d, want 7", got)
	}
}

// TestSelfPath pins the stdlib-directory convention: a source under
// `stdlib/<pkg>/` self-imports as the toolchain package so codegen
// emits unqualified self-references; anything else gets "".
func TestSelfPath(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"stdlib/list/list.gom", "github.com/podhmo/gom/stdlib/list"},
		{"stdlib/option/option.gom", "github.com/podhmo/gom/stdlib/option"},
		{"stdlib/result/result.gom", "github.com/podhmo/gom/stdlib/result"},
		{"./stdlib/list/list.gom", "github.com/podhmo/gom/stdlib/list"},
		{"deep/stdlib/list/x.gom", "github.com/podhmo/gom/stdlib/list"},
		{"stdlibx/list/list.gom", ""},
		{"list/list.gom", ""},
		{"stdlib/main.gom", ""},
		{"testdata/spec/ok.gom", ""},
	} {
		if got := gomstd.SelfPath(tc.in); got != tc.want {
			t.Errorf("SelfPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
