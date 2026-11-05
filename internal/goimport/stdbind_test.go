package goimport

// Standard-binding tests — the curated table in stdbind.go re-voices
// a package function's derived signature through the same result
// admission externs pass. Verified on the real loader: `errors.New`
// and `fmt.Errorf` surface `-> GoError` with the ErrValue call shape,
// `errors.Join`/`errors.Unwrap` `-> Option GoError` with Nil.

import (
	"go/types"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/core"
	gt "github.com/podhmo/gom/internal/types"
)

// TestStdBindErrorViews pins each inaugural table entry's declared
// view on both oracles: SignatureOf (the Gom-facing type) and
// GoSigOf (the lowering shape).
func TestStdBindErrorViews(t *testing.T) {
	t.Parallel()

	l := loaderFor(t)
	for _, tc := range []struct {
		path, name, want string
		results          core.GoResults
		variadic         bool
	}{
		{"errors", "New", "String -> GoError", core.GoResultsErrValue, false},
		{"fmt", "Errorf", "String -> List Go.Any -> GoError", core.GoResultsErrValue, true},
		{"errors", "Join", "List GoError -> Option GoError", core.GoResultsNil, true},
		{"errors", "Unwrap", "GoError -> Option GoError", core.GoResultsNil, false},
	} {
		pkg := membersOf(t, l, tc.path)
		sym := pkg.Members[tc.name]
		if sym == nil {
			t.Fatalf("%s.%s missing", tc.path, tc.name)
		}
		if sym.Hidden != "" {
			t.Fatalf("%s.%s hidden: %s", tc.path, tc.name, sym.Hidden)
		}
		sig, ok := l.SignatureOf(sym)
		if !ok {
			t.Fatalf("SignatureOf(%s.%s) not found", tc.path, tc.name)
		}
		if got := sig.Type.String(); got != tc.want {
			t.Errorf("%s.%s signature = %s, want %s", tc.path, tc.name, got, tc.want)
		}
		if sig.Variadic != tc.variadic {
			t.Errorf("%s.%s variadic = %v, want %v", tc.path, tc.name, sig.Variadic, tc.variadic)
		}
		gs, ok := l.GoSigOf(sym)
		if !ok {
			t.Fatalf("GoSigOf(%s.%s) not found", tc.path, tc.name)
		}
		if gs.Results != tc.results {
			t.Errorf("%s.%s results = %s, want %s", tc.path, tc.name, gs.Results, tc.results)
		}
	}
}

// A method named after a bound member must not inherit the package
// binding — stdbind applies at package-member derivation (member),
// never at method lookup (methodSym shares sigOf only).
func TestStdBindMethodIsolation(t *testing.T) {
	t.Parallel()

	l := loaderFor(t)
	errType := types.Universe.Lookup("error").Type()
	sig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewParam(0, nil, "s", types.Typ[types.String])),
		types.NewTuple(types.NewParam(0, nil, "", errType)), false)
	fn := types.NewFunc(0, nil, "New", sig)
	sym, ck, ok := l.methodSym("errors", "Widget", fn, sig)
	if !ok {
		t.Fatal("methodSym failed")
	}
	if got := ck.Type.String(); got != "String -> Result Unit GoError" {
		t.Errorf("method New signature = %s — the errors.New standard binding leaked", got)
	}
	if gs, ok := l.GoSigOf(sym); !ok || gs.Results != core.GoResultsErrOnly {
		t.Errorf("method New GoSig = %+v (ok %v), want ErrOnly", gs, ok)
	}
}

// A drifted table entry — one whose declared view the raw signature
// no longer admits — must hide the member with the drift reason
// rather than fall back or panic.
func TestStdBindDriftHidesMember(t *testing.T) {
	// Stays sequential: it mutates the package-global stdBinds table.
	old, had := stdBinds["strconv"]
	defer func() {
		if had {
			stdBinds["strconv"] = old
		} else {
			delete(stdBinds, "strconv")
		}
	}()
	stdBinds["strconv"] = map[string]stdBind{
		// `(int, error)` cannot take a lone-error view — declared
		// Option GoError over a two-result shape is inadmissible.
		"Atoi": {result: gt.Option(gt.GoError), note: "deliberately drifted test entry"},
	}
	l := loaderFor(t)
	pkg := membersOf(t, l, "strconv")
	atoi := pkg.Members["Atoi"]
	if atoi == nil {
		t.Fatal("Atoi missing")
	}
	if !strings.Contains(atoi.Hidden, "standard binding drifted") {
		t.Fatalf("Atoi hidden = %q, want a 'standard binding drifted' reason", atoi.Hidden)
	}
	if _, ok := l.SignatureOf(atoi); ok {
		t.Error("SignatureOf must not answer a drift-hidden member")
	}
}
