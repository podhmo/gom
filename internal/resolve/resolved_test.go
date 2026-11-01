package resolve

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
)

var sp = diag.SpanIn("test.gom", 10, 20)

func TestKindString(t *testing.T) {
	kinds := map[Kind]string{
		KindVar:     "var",
		KindFunc:    "func",
		KindType:    "type",
		KindCtor:    "ctor",
		KindField:   "field",
		KindModule:  "module",
		KindTypeVar: "typevar",
		KindMethod:  "method",
		KindBuiltin: "builtin",
	}
	for k, want := range kinds {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
}

func TestSymbolCarriesMetadata(t *testing.T) {
	def := &syntax.VarPat{Sp: sp, Name: &syntax.Ident{Sp: sp, Name: "x"}}
	local := &Symbol{Name: "x", Kind: KindVar, Def: def}
	if got := local.DefSpan(); got != sp {
		t.Errorf("DefSpan = %v, want %v", got, sp)
	}
	if local.IsGo || local.TestOnly || local.Package != "" {
		t.Error("local symbol carries foreign metadata")
	}

	goSym := &Symbol{Name: "Println", Kind: KindFunc, IsGo: true, Package: "fmt"}
	if !goSym.IsGo || goSym.Package != "fmt" {
		t.Error("go symbol lost its origin")
	}
	if got := goSym.DefSpan(); !got.IsZero() {
		t.Errorf("sourceless symbol DefSpan = %v, want zero", got)
	}

	testSym := &Symbol{Name: "unit_square", Kind: KindFunc, Def: def, TestOnly: true}
	if !testSym.TestOnly {
		t.Error("test-scope flag lost")
	}
}

func TestResolvedTables(t *testing.T) {
	binder := &syntax.Ident{Sp: sp, Name: "x"}
	use := &syntax.Ident{Sp: diag.SpanIn("test.gom", 30, 31), Name: "x"}
	sym := &Symbol{Name: "x", Kind: KindVar, Def: &syntax.VarPat{Sp: sp, Name: binder}}

	r := &Resolved{
		File: &syntax.File{Sp: diag.SpanIn("test.gom", 0, 40)},
		Defs: map[*syntax.Ident]*Symbol{binder: sym},
		Uses: map[*syntax.Ident]*Symbol{use: sym},
	}
	if got := r.DefOf(binder); got != sym {
		t.Error("DefOf missed the binding symbol")
	}
	if got := r.UseOf(use); got != sym {
		t.Error("UseOf missed the use symbol")
	}
	if got := r.UseOf(binder); got != nil {
		t.Error("binder in Uses")
	}
	if got := r.DefOf(use); got != nil {
		t.Error("use in Defs")
	}
	var nilR *Resolved
	if nilR.DefOf(binder) != nil || nilR.UseOf(use) != nil {
		t.Error("nil Resolved not nil-safe")
	}
}
