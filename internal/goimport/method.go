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
// recv may be a types.Go directly or a `Ptr` to one.
func (l *Loader) MethodOf(recv gt.Type, name string) (*resolve.Symbol, check.Signature, bool) {
	t := gt.Chase(recv)
	ptr := false
	if c, ok := t.(*gt.Con); ok && c.Qual == "" && c.Name == "Ptr" && len(c.Args) == 1 {
		t = c.Args[0]
		ptr = true
	}
	g, ok := gt.Chase(t).(*gt.Go)
	if !ok || len(g.Args) > 0 {
		return nil, check.Signature{}, false
	}
	named, ok := l.named[g.Ref]
	if !ok {
		return nil, check.Signature{}, false
	}
	var sets []*types.MethodSet
	if ptr {
		sets = []*types.MethodSet{types.NewMethodSet(types.NewPointer(named))}
	} else {
		// Pointer methods come second: Go resolves `x.M` on an
		// addressable x by taking &x itself.
		sets = []*types.MethodSet{
			types.NewMethodSet(named),
			types.NewMethodSet(types.NewPointer(named)),
		}
	}
	for _, ms := range sets {
		for i := 0; i < ms.Len(); i++ {
			sel := ms.At(i)
			fn, ok := sel.Obj().(*types.Func)
			if !ok || fn.Name() != name {
				continue
			}
			sig, ok := sel.Type().(*types.Signature)
			if !ok {
				continue
			}
			return l.methodSym(g.Ref.Path, g.Ref.Name, fn, sig)
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
