package goimport

// Method lookup for `x.M` on Go-typed receivers — the optional
// check.MethodSource (docs/decisions/2026-11-03-go-types-mapping.md
// "Methods"). The resolver cannot enumerate a named type's method set
// from package members, so the checker asks here when a selector on a
// types.Go (or Ptr-of-types.Go) receiver resolved to no ordinary
// member. Found methods mint KindMethod symbols registered in the
// same per-symbol table, so SignatureOf/GoSigOf/CallGo/member codegen
// all work through the ordinary machinery.
//
// Method sets follow the receiver's Gom type: `T` searches the value
// method set of the named type, `Ptr T` the pointer method set. As a
// fallback the `T` lookup also consults the pointer set — Go takes
// `(&x).M()` for addressable receivers, and every atom a selector
// lands on is a bound variable, hence addressable. (A non-addressable
// receiver with a pointer method surfaces as a Go compile error in
// generated code — a known gap.)

import (
	"go/types"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	gt "github.com/podhmo/gom/internal/types"
)

// MethodOf returns the method symbol and Gom-view signature of name
// on the Go named type behind recv — the check.MethodSource entry.
// recv may be a types.Go directly or a `Ptr` to one; the builtin
// GoError type is also admitted, standing for the universe `error`
// interface.
func (l *Loader) MethodOf(recv gt.Type, name string) (*resolve.Symbol, check.Signature, bool) {
	t := gt.Chase(recv)
	ptr := false
	if c, ok := t.(*gt.Con); ok && c.Qual == "" && c.Name == "Ptr" && len(c.Args) == 1 {
		t = c.Args[0]
		ptr = true
	}
	t = gt.Chase(t)

	// named is the go/types named type behind the receiver and
	// path/recvName feed the interned method symbol's identity.
	var named *types.Named
	path, recvName := "", ""
	if g, ok := t.(*gt.Go); ok && len(g.Args) == 0 {
		n, found := l.named[g.Ref]
		if !found {
			return nil, check.Signature{}, false
		}
		named, path, recvName = n, g.Ref.Path, g.Ref.Name
	} else if gt.Equal(t, gt.GoError) {
		// The universe `error` interface maps to the builtin GoError
		// type rather than a types.Go handle — the one Go-typed
		// receiver l.named cannot hold. Its method set is the
		// interface's own (Error); `Ptr GoError` reaches the same
		// method through Go's implicit deref.
		if n, ok := types.Universe.Lookup("error").Type().(*types.Named); ok {
			named, recvName = n, "error"
		}
	}
	if named == nil {
		return nil, check.Signature{}, false
	}
	var sets []*types.MethodSet
	switch {
	case gt.Equal(t, gt.GoError):
		// The interface's own set — also what `pe.M` on a `*error`
		// consults through the deref rule (the pointer's method
		// set proper is empty).
		sets = []*types.MethodSet{types.NewMethodSet(named)}
	case ptr:
		sets = []*types.MethodSet{types.NewMethodSet(types.NewPointer(named))}
	default:
		// Pointer methods come second: Go resolves `x.M` on an
		// addressable x by taking &x itself.
		sets = []*types.MethodSet{
			types.NewMethodSet(named),
			types.NewMethodSet(types.NewPointer(named)),
		}
	}
	for _, ms := range sets {
		for sel := range ms.Methods() {
			sel := sel
			fn, ok := sel.Obj().(*types.Func)
			if !ok || fn.Name() != name {
				continue
			}
			sig, ok := sel.Type().(*types.Signature)
			if !ok {
				continue
			}
			return l.methodSym(path, recvName, fn, sig)
		}
	}
	return nil, check.Signature{}, false
}

// methodSym returns the KindMethod symbol for a Go method — one
// symbol per (package, receiver type, method) so every `x.M` site
// shares the callee — and registers its metadata so SignatureOf and
// GoSigOf answer it like any loaded member.
func (l *Loader) methodSym(path, recvName string, fn *types.Func, sig *types.Signature) (*resolve.Symbol, check.Signature, bool) {
	key := path + "." + recvName + "." + fn.Name()
	if sym, ok := l.msyms[key]; ok {
		return sym, l.syms[sym].ck, true
	}
	// The selection type should already be receiver-free (the method
	// value type); drop it defensively when it is not.
	if sig.Recv() != nil {
		sig = types.NewSignatureType(nil, nil, nil,
			sig.Params(), sig.Results(), sig.Variadic())
	}
	sym := &resolve.Symbol{
		Name:    fn.Name(),
		Kind:    resolve.KindMethod,
		IsGo:    true,
		Package: path,
	}
	m := &member{obj: fn, sig: sig}
	l.sigOf(sym, m, sig)
	l.syms[sym] = m
	l.msyms[key] = sym
	// A method whose signature is unrepresentable still binds — the
	// checker's hiddenDiag reports E0501 with the reason where the
	// member expression is checked.
	return sym, m.ck, true
}
