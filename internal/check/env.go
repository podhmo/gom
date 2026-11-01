package check

import (
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Signature is the Gom-level view of a symbol the checker did not
// declare itself — an imported function, a Go function or method, a
// builtin name, or an imported type. Type may contain *types.Var for
// the symbol's own type parameters; each use site instantiates them
// with fresh metas.
type Signature struct {
	Type types.Type
	// Variadic marks a Go callee whose final parameter is variadic:
	// the number of applied arguments may then exceed Arity-1 and a
	// `..spread` is allowed as the last argument.
	Variadic bool
	// DeclArity is the callee's own declared parameter count — what
	// CallInfo.Arity records (docs/design.md section 2.1). It differs
	// from the flattened parameter count of Type exactly when the
	// callee returns a function: `fn mk () -> (Int -> Int)` flattens
	// to two parameters but declares one. A value <= 0 means "derive
	// it": the outer parameter count of Type — which is the declared
	// count whenever Type keeps the unflattened declared shape —
	// or the flattened count when Type is not a function. (Every Gom
	// callee has at least one parameter, even if only `()`, so 0 is
	// never a real declared arity.)
	DeclArity int
}

// SignatureSource supplies the signatures of symbols not declared in
// the file being checked: imported module members, Go functions and
// methods (filled by internal/goimport in later tasks), and names the
// resolver bound in the builtin scope. The checker consults it when a
// referenced symbol has no type in its own declaration environment.
//
// A nil source means "no external signatures"; the checker still knows
// the builtin-scope constructors and functions in builtinSigs.
type SignatureSource interface {
	// SignatureOf returns the signature for sym, or false when the
	// source does not know it.
	SignatureOf(sym *resolve.Symbol) (Signature, bool)
}

// MapSource is a SignatureSource backed by a fixed map — the form
// tests and early drivers use.
type MapSource map[*resolve.Symbol]Signature

// SignatureOf returns the signature stored for sym.
func (m MapSource) SignatureOf(sym *resolve.Symbol) (Signature, bool) {
	sig, ok := m[sym]
	return sig, ok
}

// Chain returns a SignatureSource consulting each source in order.
func Chain(sources ...SignatureSource) SignatureSource {
	return chain(sources)
}

type chain []SignatureSource

func (c chain) SignatureOf(sym *resolve.Symbol) (Signature, bool) {
	for _, s := range c {
		if s == nil {
			continue
		}
		if sig, ok := s.SignatureOf(sym); ok {
			return sig, true
		}
	}
	return Signature{}, false
}

// builtinTVar hands out the rigid variables used by the builtin
// signatures below. They are created once per checker so two builtin
// signatures never share a variable by accident.
type builtinTVar struct {
	next int
	vars map[string]*types.Var
}

func (b *builtinTVar) named(name string) *types.Var {
	if b.vars == nil {
		b.vars = map[string]*types.Var{}
	}
	if v, ok := b.vars[name]; ok {
		return v
	}
	v := &types.Var{ID: builtinVarBase + b.next, Name: name}
	b.next++
	b.vars[name] = v
	return v
}

// builtinVarBase starts the ID range of the rigid variables appearing
// in builtin signatures, kept clear of the IDs a checker hands out for
// declared type parameters (which count up from 0 per declaration —
// Var identity is its ID, so colliding ranges would equate distinct
// variables when a file declares `a` and a builtin uses `a` too).
const builtinVarBase = 1 << 20

// builtinSigs are the signatures the checker itself knows: the
// constructors of the builtin-scope sum types Option and Result (spec
// section 3.1) and the compiler-provided functions assert, assert_eq
// and todo (spec section 11.4 and the diagnostics' placeholder fixes).
// They are keyed by symbol name and consulted only for symbols the
// declaration environment and the SignatureSource do not cover —
// builtin-scope symbols carry Package == "" (internal/types.Con).
//
// stdlib-level names (List.map, Chan.send, not, show, ...) are not
// here: they belong to real packages and arrive through a
// SignatureSource once the resolver learns them.
func builtinSigs(v *builtinTVar) map[string]Signature {
	a := v.named("a")
	e := v.named("e")
	return map[string]Signature{
		// Option a = | Some a | None
		"Some": {Type: types.Fn(types.Option(a), types.P(a))},
		"None": {Type: types.Option(a)},
		// Result a e = | Ok a | Err e
		"Ok":  {Type: types.Fn(types.Result(a, e), types.P(a))},
		"Err": {Type: types.Fn(types.Result(a, e), types.P(e))},
		// assert cond : Unit
		"assert": {Type: types.Fn(types.Unit, types.P(types.Bool))},
		// assert_eq actual expected : Unit — the no-function
		// restriction of `==` (spec 11.4) is enforced at the call.
		"assert_eq": {Type: types.Fn(types.Unit, types.P(a), types.P(a))},
		// todo () : a — diverges (panics) when reached.
		"todo": {Type: types.Fn(v.named("todo_ret"), types.P(types.Unit))},
	}
}

// isConv reports whether sym names the `Go.conv` conversion builtin
// (spec section 8.4): the `conv` member of the compiler-owned Go
// namespace, a KindBuiltin symbol with Package "Go".
func isConv(sym *resolve.Symbol) bool {
	return sym != nil && sym.Kind == resolve.KindBuiltin &&
		sym.Name == "conv" && sym.Package == "Go"
}
