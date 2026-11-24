package check

// @method — a Gom record type can declare the Go methods it
// implements (docs/decisions/2026-11-21-go-iface-impl.md). The
// declaration's first parameter is the receiver; codegen emits the
// ordinary function plus a forwarding Go method. checkMethodDecl
// validates the declaration shape and records the method on the
// receiver's constructor; implementsIface is the conformance test a
// Go interface boundary applies.

import (
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// GomMethod is one declared Go method of a Gom type — recorded per
// receiver for conformance checks and .gomi round-trips.
type GomMethod struct {
	Name    string      // Go method name — core.ExportName of the fn's name
	Sig     *types.Func // the signature without the receiver
	PtrRecv bool        // declared on `Ptr T` — only `*T` carries it in Go
}

// InterfaceMethod is one method a Go interface requires.
type InterfaceMethod struct {
	Name string
	Sig  *types.Func
	// GoSig is the method's raw Go signature the way reflect spells
	// a bound method — `func(int64) string`. Unlike Sig it is not
	// gom-adapted (a `-> Result` method stays `func() error`), which
	// is the form eval's implements check compares against. "" when
	// the source cannot render it.
	GoSig string
}

// checkMethodDecl validates an @method fn declaration and records
// its method under the receiver's constructor. v1 admits value or
// pointer receivers to an own-package, non-generic record type and
// non-Result returns (E0420 on violation).
func (c *checker) checkMethodDecl(d *syntax.FnDecl, sym *resolve.Symbol, sig *types.Func) {
	fail := func(msg string) {
		c.diags.Add(diag.Error("E0420", msg).At(d.Sp))
	}
	if d.Extern {
		fail("the \"@method\" attribute cannot combine with \"@extern\"")
		return
	}
	if len(d.Params) == 0 || sig == nil || len(sig.Params) == 0 {
		fail("an \"@method\" declaration requires a receiver parameter")
		return
	}
	ptrRecv := false
	base := types.Chase(sig.Params[0].Type)
	if p, ok := base.(*types.Con); ok && p.Qual == "" && p.Name == "Ptr" && len(p.Args) == 1 {
		ptrRecv = true
		base = types.Chase(p.Args[0])
	}
	rc, ok := base.(*types.Con)
	if !ok || rc.Qual != "" {
		fail("an \"@method\" receiver must be a record type declared in this package")
		return
	}
	ti, ok := c.tyCons[conKey{qual: "", name: rc.Name}]
	if !ok || ti.kind != kindRecord {
		fail("an \"@method\" receiver must be a record type declared in this package")
		return
	}
	if len(ti.params) > 0 {
		fail("an \"@method\" receiver cannot be a generic record type: Go methods cannot take type parameters")
		return
	}
	if rt, ok := types.Chase(sig.Ret).(*types.Con); ok && rt.Qual == "" && rt.Name == "Result" {
		fail("an \"@method\" function cannot return Result: the (T, error) pair has no interface counterpart")
		return
	}
	if c.methods == nil {
		c.methods = map[conKey][]GomMethod{}
	}
	// Unit parameters carry no Go meaning — drop them like the
	// emitted method wrapper does.
	var ps []types.FuncParam
	for _, fp := range sig.Params[1:] {
		if types.Equal(fp.Type, types.Unit) {
			continue
		}
		ps = append(ps, fp)
	}
	key := conKey{qual: "", name: rc.Name}
	mname := d.MethodName
	if mname == "" {
		mname = core.ExportName(sym.Name)
	}
	c.methods[key] = append(c.methods[key], GomMethod{
		Name:    mname,
		Sig:     types.Fn(sig.Ret, ps...),
		PtrRecv: ptrRecv,
	})
}

// implementsIface reports whether the Gom type got conforms to the
// Go interface want — every required method is present in got's
// declared @method set with an equal signature (receiver stripped;
// types.Equal compares the flattened curried forms).
func (c *checker) implementsIface(want, got types.Type) bool {
	if isGoAny(types.Chase(want)) {
		return false
	}
	src, ok := c.src.(InterfaceMethodSource)
	if !ok {
		return false
	}
	need, ok := src.InterfaceMethods(want)
	if !ok || len(need) == 0 {
		return false
	}
	have := c.gomMethodsOf(got)
	if len(have) == 0 {
		return false
	}
	// Go's method-set rule: `*T` carries value and pointer methods,
	// a bare `T` only value-receiver ones.
	ptrGot := false
	if p, ok := types.Chase(got).(*types.Con); ok && p.Qual == "" && p.Name == "Ptr" && len(p.Args) == 1 {
		ptrGot = true
	}
	set := map[string]*types.Func{}
	for _, m := range have {
		if m.PtrRecv && !ptrGot {
			continue
		}
		set[m.Name] = m.Sig
	}
	for _, n := range need {
		s, ok := set[n.Name]
		if !ok || !types.Equal(s, n.Sig) {
			return false
		}
	}
	return true
}

// gomMethodsOf collects the declared Go methods of the Gom
// constructor behind t — in-package declarations from the checker's
// own table, imported ones through GomMethodSource.
func (c *checker) gomMethodsOf(t types.Type) []GomMethod {
	base := types.Chase(t)
	if p, ok := base.(*types.Con); ok && p.Qual == "" && p.Name == "Ptr" && len(p.Args) == 1 {
		base = types.Chase(p.Args[0])
	}
	con, ok := base.(*types.Con)
	if !ok {
		return nil
	}
	if con.Qual == "" {
		return c.methods[conKey{qual: "", name: con.Name}]
	}
	if src, ok := c.src.(GomMethodSource); ok {
		ms, _ := src.GomMethodsOf(con)
		return ms
	}
	return nil
}
