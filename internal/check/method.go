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
	// RawParams/RawResults are the method's parameter and result
	// types in the raw Go view — `int` reads `Go.Int` where the
	// gom-view Sig folds it to `Int` — so the conformance check can
	// tell `int` and `int64` positions apart (the gom view cannot).
	// Nil when the source does not track the raw view; the check
	// then falls back to the gom-view comparison.
	RawParams  []types.Type
	RawResults []types.Type
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
		// `Result T GoError` emits `(T, error)` — the method unwraps
		// via result.ToGo. Any other Err payload has no interface
		// counterpart and stays rejected.
		okRes := len(rt.Args) == 2 && types.Equal(types.Chase(rt.Args[1]), types.GoError)
		if !okRes {
			fail("an \"@method\" function cannot return Result with a non-GoError payload: the (T, error) pair has no interface counterpart")
			return
		}
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
		if !ok || !c.methodSigMatch(s, n) {
			return false
		}
	}
	return true
}

// methodSigMatch reports whether the declared method signature s
// satisfies the interface's requirement n. The gom views compare
// with the int family (`Int`/`Int64`) treated as interchangeable —
// the view folds both Go `int` and `int64` to `Int`, so neither can
// be told apart there — and the width question is answered against
// the raw view instead: a raw `int` position (`Go.Int`) wants the
// type the boundary spells `int` (gom `Int`), a raw `int64`
// position wants the type spelled `int64` (gom `Int64`). Without
// the width pass both directions broke: the all-`Int` spelling
// passed but emitted a signature no interface carries, and the
// correct `Int64`/`Int` spelling failed the folded comparison
// (decisions/2026-11-25-method-iface-width-exact).
func (c *checker) methodSigMatch(s *types.Func, n InterfaceMethod) bool {
	if !c.methodViewEqual(s, n.Sig) {
		return false
	}
	if n.RawParams == nil && n.RawResults == nil {
		// A source without the raw view cannot tell widths apart;
		// fall back to the exact gom-view comparison the check
		// used before the width pass (the relaxed comparison
		// would admit spellings whose emit the source cannot
		// vouch for).
		return types.Equal(s, n.Sig)
	}
	sp := s.Params
	if len(n.RawParams) != len(sp) {
		return false
	}
	for i, rp := range n.RawParams {
		if !c.methodWidthOk(sp[i].Type, rp, true) {
			return false
		}
	}
	// Correlate the folded result structure with the raw results:
	// a `Result T GoError` payload is raw result 0, a Tuple's
	// elements are the results in order, a plain ret is result 0.
	ret := types.Chase(s.Ret)
	if len(n.RawResults) == 0 {
		return true
	}
	if rc, ok := ret.(*types.Con); ok && rc.Qual == "" && rc.Name == "Result" && len(rc.Args) == 2 {
		payload := types.Chase(rc.Args[0])
		// A Tuple payload spreads over the leading results like a
		// plain tuple return — `(T, error)` carries the Option-
		// unwrapped element widths positionally.
		if tp, ok := payload.(*types.Tuple); ok {
			return c.methodWidthResults(tp.Elems, n.RawResults)
		}
		if len(n.RawResults) > 0 && !c.methodWidthOk(payload, n.RawResults[0], true) {
			return false
		}
		return true
	}
	if tp, ok := ret.(*types.Tuple); ok {
		return c.methodWidthResults(tp.Elems, n.RawResults)
	}
	if types.Equal(ret, types.Unit) {
		return true
	}
	return c.methodWidthOk(ret, n.RawResults[0], true)
}

// methodWidthResults pairs spread result elements with the raw
// results positionally, each a root position of the emitted
// signature.
func (c *checker) methodWidthResults(elems, raw []types.Type) bool {
	if len(elems) > len(raw) {
		return false
	}
	for i, e := range elems {
		if !c.methodWidthOk(e, raw[i], true) {
			return false
		}
	}
	return true
}

// methodViewEqual is types.Equal with the int family relaxed at
// leaf positions: gom `Int` and `Int64` are interchangeable here
// because the gom view cannot tell which one an interface position
// asked for — the raw width pass decides that.
func (c *checker) methodViewEqual(a, b types.Type) bool {
	// The interface side is always the builtin — goimport folds the
	// raw signature with the builtin constructors, no file-local
	// declaration can reach it. The user side only counts when its
	// spelling is not shadowed: a file-local `type Int` is never
	// the builtin the interface carries, so it must not relax into
	// — or structurally coincide with — the interface's int leaf.
	if methodBuiltinInt(b) {
		if c.methodIntFamily(a) {
			return true
		}
		if con, ok := types.Chase(a).(*types.Con); ok && con.Qual == "" &&
			(con.Name == "Int" || con.Name == "Int64") {
			return false
		}
	}
	ac, bc := types.Chase(a), types.Chase(b)
	switch at := ac.(type) {
	case *types.Con:
		bt, ok := bc.(*types.Con)
		if !ok || at.Qual != bt.Qual || at.Name != bt.Name || len(at.Args) != len(bt.Args) {
			return types.Equal(ac, bc)
		}
		for i := range at.Args {
			if !c.methodViewEqual(at.Args[i], bt.Args[i]) {
				return false
			}
		}
		return true
	case *types.Tuple:
		bt, ok := bc.(*types.Tuple)
		if !ok || len(at.Elems) != len(bt.Elems) {
			return false
		}
		for i := range at.Elems {
			if !c.methodViewEqual(at.Elems[i], bt.Elems[i]) {
				return false
			}
		}
		return true
	case *types.Func:
		bt, ok := bc.(*types.Func)
		if !ok || len(at.Params) != len(bt.Params) {
			return false
		}
		for i := range at.Params {
			if !c.methodViewEqual(at.Params[i].Type, bt.Params[i].Type) {
				return false
			}
		}
		return c.methodViewEqual(at.Ret, bt.Ret)
	case *types.Meta, *types.Var, *types.Go:
		return types.Equal(ac, bc)
	default:
		panic("unreachable")
	}
}

// methodBuiltinInt reports whether t is structurally a builtin
// int-family constructor — used for the interface side, whose
// types goimport builds with the real builtins regardless of what
// the checked file shadows.
func methodBuiltinInt(t types.Type) bool {
	c, ok := types.Chase(t).(*types.Con)
	if !ok || c.Qual != "" {
		return false
	}
	return c.Name == "Int" || c.Name == "Int64"
}

// methodIntFamily reports whether t is one of the builtin integer
// types the gom view conflates at the Go boundary — `Int` (the
// fold of Go's `int` and `int64`) and `Int64`. A file-local type
// shadowing the builtin name is not the builtin: its emit carries
// none of the builtin's widths.
func (c *checker) methodIntFamily(t types.Type) bool {
	con, ok := types.Chase(t).(*types.Con)
	if !ok || con.Qual != "" {
		return false
	}
	if con.Name != "Int" && con.Name != "Int64" {
		return false
	}
	return !c.shadowedBuiltin(con.Name)
}

// methodWidthOk reports whether the declared type u emits the
// boundary width the interface's raw position r requires, mirroring
// the emit rules exactly: at a root position (a parameter or result
// type itself) methodType spells `Int` as `int` and `Int64` as
// `int64`; inside a container the element emits through goType
// instead, where both spell `int64` — so a nested raw `int64`
// admits either, and a nested raw `int` admits neither (no
// container spelling emits it). Only the int widths can disagree —
// everything else either compares equal in the gom view already or
// has no method-boundary spelling.
func (c *checker) methodWidthOk(u, r types.Type, root bool) bool {
	uc, rc := types.Chase(u), types.Chase(r)
	if gc, ok := rc.(*types.Con); ok && gc.Qual == "Go" && gc.Name == "Int" {
		// A raw `int` position: only the root spelling `Int`
		// emits `int`; nested int-family types emit `int64`.
		if root {
			return types.Equal(uc, types.Int) && !c.shadowedBuiltin("Int")
		}
		return !c.methodIntFamily(uc)
	}
	if types.Equal(rc, types.Int) {
		// A raw `int64` position: the root wants `Int64`
		// (gom `Int` spells `int` there); nested, both spellings
		// emit `int64` — the relaxed view comparison has already
		// pinned u to the int family at this position.
		if root {
			return types.Equal(uc, types.Int64) && !c.shadowedBuiltin("Int64")
		}
		return true
	}
	// Containers pair element-wise when the shapes mirror — a
	// `List Int` element emits through goType, so the width rule
	// recurses with root cleared.
	ucon, uok := uc.(*types.Con)
	rcon, rok := rc.(*types.Con)
	if uok && rok && len(ucon.Args) == len(rcon.Args) {
		for i := range ucon.Args {
			if !c.methodWidthOk(ucon.Args[i], rcon.Args[i], false) {
				return false
			}
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
