package treedump

import (
	"fmt"
	"strings"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/types"
)

// DumpCore renders a Core IR file — the output of phase 7
// (internal/lower) and the input of codegen. All semantic decisions
// are visible in the dump: the ANF body lists its lets in evaluation
// order with the tail expression under `(tail ...)`; every call node
// carries its resolved callee, arity and result shape; Go-interop
// conversions, escape boundaries (`to=<fn>`) and match decision trees
// appear as the node kinds lowering fixed.
//
// Symbols are the `name%<n>` references interned during the walk and
// listed in the trailing `(syms)` section, exactly as in DumpTyped.
func DumpCore(f *core.File) string {
	d := &cdump{dumper: newDumper()}
	text := "file" + at(f.Sp)
	if f.Package != "" {
		text += " " + atom(f.Package) + at(f.PkgSp)
	} else {
		text += " -"
	}
	kids := make([]*form, 0, len(f.Imports)+len(f.Decls)+1)
	for _, im := range f.Imports {
		text := "import" + at(im.Sp) + " " + quote(im.Path) + " " + d.symRef(im.Sym)
		if im.IsGo {
			text += " :go"
		}
		kids = append(kids, nodef(text))
	}
	for _, decl := range f.Decls {
		kids = append(kids, d.cdecl(decl))
	}
	kids = append(kids, d.syms())
	return render(nodef(text, kids...))
}

// --- the core dumper ---

// cdump wraps the shared symbol interner with the extra state a core
// dump needs: the stack of enclosing *core.Func boundaries, so the
// escape and tail-call `to=` fields can name their target.
type cdump struct {
	*dumper
	fns []fnCtx
}

// fnCtx is one enclosing function boundary: the Func and the label
// `to=` renders for it — the declaring symbol for a FuncDecl, the
// quoted name for a test, `fn@<span>` for a closure.
type fnCtx struct {
	fn    *core.Func
	label string
}

// toLabel renders an escape / tail-call target: the innermost
// enclosing function's name when it is on the stack, `?` for a nil
// target, and `fn@<span>` for a pointer outside the stack — malformed
// input stays visible rather than crashing.
func (d *cdump) toLabel(to *core.Func) string {
	if to == nil {
		return "?"
	}
	for i := len(d.fns) - 1; i >= 0; i-- {
		if d.fns[i].fn == to {
			return d.fns[i].label
		}
	}
	return "fn" + at(to.Sp)
}

// chead builds the `name@span :Type` prefix every core node shares.
func chead(name string, b core.Base) string {
	return name + at(b.Sp) + " " + typ(b.Typ)
}

// targs renders a type-instantiation list ` targs=[A,B]` — the empty
// list renders nothing.
func targs(ts []types.Type) string {
	if len(ts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(" targs=[")
	for i, t := range ts {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(tatom(t))
	}
	b.WriteByte(']')
	return b.String()
}

// tparams renders declared type parameters `(tparams a e)`.
func tparams(vs []*types.Var) *form {
	kids := make([]*form, 0, len(vs))
	for _, v := range vs {
		kids = append(kids, atomf(atom(v.String())))
	}
	return nodef("tparams", kids...)
}

// --- declarations ---

func (d *cdump) cdecl(dd core.Decl) *form {
	switch dd := dd.(type) {
	case *core.FuncDecl:
		name := d.symRef(dd.Sym)
		return d.cfunc("fn"+at(dd.Sp)+" "+name, dd.Fn, name, dd.TypeParams)
	case *core.VarDecl:
		return nodef("let"+at(dd.Sp)+" "+d.symRef(dd.Sym), d.cbody(dd.Body))
	case *core.TestDecl:
		text := "test" + at(dd.Sp) + " " + quote(dd.Name) + at(dd.NameSp)
		var fn *form
		if dd.Fn != nil {
			fn = d.cfunc("fn"+at(dd.Fn.Sp)+" "+quote(dd.Name), dd.Fn, quote(dd.Name), nil)
		} else {
			fn = nodef("?")
		}
		return nodef(text, fn)
	case *core.TypeDecl:
		// Intern the declared type's symbol before walking the body so
		// %n indices follow source order (type name before members).
		text := "type" + at(dd.Sp) + " " + d.symRef(dd.Sym)
		kids := []*form{}
		if len(dd.TypeParams) > 0 {
			kids = append(kids, tparams(dd.TypeParams))
		}
		kids = append(kids, d.ctypebody(dd.Body))
		return nodef(text, kids...)
	default:
		panic("unreachable")
	}
}

// cfunc renders the Func shared by FuncDecl, TestDecl and Closure:
// `(fn@sp <label> :Sig [:tailloop] [residual=T] (tparams ...) (params
// ...) (body ...))`. label is the `to=` name escapes inside the body
// resolve against; it is pushed while the body is walked so To
// pointers match by identity, which also keeps a dangling target
// (pointing outside the enclosing chain) visible as `fn@<span>`.
func (d *cdump) cfunc(head string, fn *core.Func, label string, tps []*types.Var) *form {
	if fn == nil {
		return nodef(head + " ?")
	}
	text := head + " " + typ(fn.Sig)
	if res := fn.ResidualType(); res != nil && !types.Equal(res, fn.Sig.Ret) {
		text += " residual=" + tatom(res)
	}
	if fn.TailLoop {
		text += " :tailloop"
	}
	kids := []*form{}
	if len(tps) > 0 {
		kids = append(kids, tparams(tps))
	}
	ps := make([]*form, 0, len(fn.Params))
	for _, p := range fn.Params {
		ps = append(ps, d.cbind("param", p))
	}
	kids = append(kids, nodef("params", ps...))
	d.fns = append(d.fns, fnCtx{fn, label})
	kids = append(kids, d.cbody(fn.Body))
	d.fns = d.fns[:len(d.fns)-1]
	return nodef(text, kids...)
}

func (d *cdump) ctypebody(b core.TypeBody) *form {
	switch b := b.(type) {
	case *core.TSum:
		kids := make([]*form, 0, len(b.Variants))
		for _, v := range b.Variants {
			// Intern the ctor symbol before its fields: the variant
			// name precedes the payload in source.
			vtext := "variant" + at(v.Sp) + " " + d.symRef(v.Sym)
			vk := make([]*form, 0, len(v.Fields))
			for _, f := range v.Fields {
				vk = append(vk, d.ctfield(f))
			}
			kids = append(kids, nodef(vtext, vk...))
		}
		return nodef("sum"+at(b.Sp), kids...)
	case *core.TRecord:
		kids := make([]*form, 0, len(b.Fields))
		for _, f := range b.Fields {
			kids = append(kids, d.ctfield(f))
		}
		return nodef("record"+at(b.Sp), kids...)
	case *core.TAlias:
		return nodef("alias"+at(b.Sp), atomf(typ(b.Type)))
	case *core.TAbstract:
		return nodef("abstract" + at(b.Sp))
	default:
		panic("unreachable")
	}
}

// ctfield renders a payload slot / record field — `(field@sp :T name
// -> f%n :i0)` — same shape as the typed dump's tfield.
func (d *cdump) ctfield(f core.TField) *form {
	text := "field" + at(f.Sp) + " " + typ(f.Type)
	switch {
	case f.Label != "":
		text += " ~" + atom(f.Label)
	case f.Name != "":
		text += " " + atom(f.Name)
	}
	if f.Sym != nil {
		text += " -> " + d.symRef(f.Sym)
	}
	return nodef(text + fmt.Sprintf(" :i%d", f.Index))
}

// --- binders and bodies ---

// cbind renders a leaf binder `(param@sp :T x%2)` / `(bind@sp :T _)`
// — head "param" for Func.Params, "bind" for case/receive/for
// binders. `_` prints for a discard binder, `?%` for a missing
// symbol and `?` for a missing binder.
func (d *cdump) cbind(head string, b *core.Bind) *form {
	if b == nil {
		return nodef(head + " ?")
	}
	return nodef(head + at(b.Sp) + " " + d.bindText(b))
}

// bindText renders the binder payload `:T name%<n>` shared by cbind
// and the inline binder of a let head.
func (d *cdump) bindText(b *core.Bind) string {
	text := typ(b.Typ) + " "
	switch {
	case b.Discard:
		return text + "_"
	case b.Sym != nil:
		return text + d.symRef(b.Sym)
	default:
		return text + "?%"
	}
}

// clet renders one ordered binding `(let@sp :T name%<n>@bsp X)`: the
// binder is inlined in the head — type first, then name with its own
// span glued — so the commonest node of the ANF body stays one line.
func (d *cdump) clet(l *core.Let) *form {
	text := "let" + at(l.Sp) + " "
	if l.Name == nil {
		text += "?"
	} else {
		text += d.bindText(l.Name) + at(l.Name.Sp)
	}
	return nodef(text, d.cexpr(l.X))
}

// cbody renders the ANF unit: ordered `(let ...)` bindings then the
// result expression under `(tail ...)` — the position where TailCall
// and branch bodies are legal.
func (d *cdump) cbody(b *core.Body) *form {
	if b == nil {
		return nodef("body ?")
	}
	kids := make([]*form, 0, len(b.Lets)+1)
	for _, l := range b.Lets {
		kids = append(kids, d.clet(l))
	}
	kids = append(kids, nodef("tail", d.cexpr(b.Tail)))
	return nodef("body", kids...)
}

func (d *cdump) catoms(as []core.Atom) []*form {
	kids := make([]*form, 0, len(as))
	for _, a := range as {
		kids = append(kids, d.cexpr(a))
	}
	return kids
}

// cfields renders a record construction/update field `(field@sp f%n
// :i0 value)`.
func (d *cdump) cfields(fs []core.FieldInit) []*form {
	kids := make([]*form, 0, len(fs))
	for _, f := range fs {
		text := "field" + at(f.Sp) + " " + d.symRef(f.Sym) + fmt.Sprintf(" :i%d", f.Index)
		kids = append(kids, nodef(text, d.cexpr(f.Value)))
	}
	return kids
}

// --- expressions ---

func (d *cdump) cexpr(e core.Expr) *form {
	if e == nil {
		return nodef("?")
	}
	switch e := e.(type) {
	case *core.Var:
		return nodef(chead("var", e.Base) + " " + d.symRef(e.Sym))
	case *core.Lit:
		return nodef(chead("lit", e.Base) + " " + e.Kind.String() + " " + atom(e.Text))
	case *core.FuncRef:
		text := chead("funcref", e.Base) + " " + d.symRef(e.Sym)
		return nodef(text + fmt.Sprintf(" arity=%d", e.Arity) + targs(e.TypeArgs))
	case *core.CtorRef:
		return nodef(chead("ctorref", e.Base) + " " + d.symRef(e.Sym) + targs(e.TypeArgs))
	case *core.GoFuncRef:
		text := chead("gofuncref", e.Base) + " " + d.symRef(e.Sym)
		text += fmt.Sprintf(" arity=%d results=%s", e.Arity, e.Results) + targs(e.TypeArgs)
		kids := []*form{}
		if e.Recv != nil {
			kids = append(kids, nodef("recv", d.cexpr(e.Recv)))
		}
		return nodef(text, kids...)
	case *core.FieldGet:
		text := chead("fieldget", e.Base)
		if e.Field != nil {
			text += " " + d.symRef(e.Field)
		}
		return nodef(text+fmt.Sprintf(" :i%d", e.Index), d.cexpr(e.Recv))
	case *core.Closure:
		var fn *form
		if e.Fn != nil {
			fn = d.cfunc("fn"+at(e.Fn.Sp), e.Fn, "fn"+at(e.Fn.Sp), nil)
		} else {
			fn = nodef("?")
		}
		return nodef(chead("closure", e.Base), fn)
	case *core.Unary:
		return nodef(chead("unary", e.Base)+" "+e.Op.String(), d.cexpr(e.X))
	case *core.Binary:
		return nodef(chead("binary", e.Base)+" "+e.Op.String(), d.cexpr(e.X), d.cexpr(e.Y))
	case *core.If:
		kids := []*form{d.cexpr(e.Cond), d.cbody(e.Then)}
		if e.Else != nil {
			kids = append(kids, d.cbody(e.Else))
		}
		return nodef(chead("if", e.Base), kids...)
	case *core.Match:
		return nodef(chead("match", e.Base), d.cdtree(e.Tree))
	case *core.SelectExpr:
		kids := make([]*form, 0, len(e.Arms))
		for _, a := range e.Arms {
			kids = append(kids, d.carm(a))
		}
		return nodef(chead("select", e.Base), kids...)
	case *core.Spawn:
		return nodef(chead("go", e.Base), d.cbody(e.Body))
	case *core.ForRange:
		return nodef(chead("for", e.Base)+" "+e.Kind.String(),
			d.cbind("bind", e.Elem), d.cexpr(e.Iter), d.cbody(e.Body))
	case *core.TupleNew:
		return nodef(chead("tuple", e.Base), d.catoms(e.Elems)...)
	case *core.ListNew:
		kids := d.catoms(e.Elems)
		if e.Rest != nil {
			kids = append(kids, nodef("rest", d.cexpr(e.Rest)))
		}
		return nodef(chead("list", e.Base), kids...)
	case *core.RecordNew:
		return nodef(chead("record", e.Base), d.cfields(e.Fields)...)
	case *core.RecordUpdate:
		kids := []*form{d.cexpr(e.Rec)}
		kids = append(kids, d.cfields(e.Fields)...)
		return nodef(chead("update", e.Base), kids...)
	case *core.CallDirect:
		text := chead("calldirect", e.Base) + " " + d.symRef(e.Callee)
		return nodef(text+fmt.Sprintf(" arity=%d", e.Arity)+targs(e.TypeArgs), d.catoms(e.Args)...)
	case *core.CallFunc:
		kids := []*form{d.cexpr(e.Fn)}
		kids = append(kids, d.catoms(e.Args)...)
		return nodef(chead("callfunc", e.Base), kids...)
	case *core.CallGo:
		text := chead("callgo", e.Base) + " " + d.symRef(e.Callee)
		text += fmt.Sprintf(" arity=%d results=%s", e.Arity, e.Results)
		if e.Variadic {
			text += " variadic"
		}
		if e.Spread {
			text += " spread"
		}
		text += targs(e.TypeArgs)
		kids := []*form{}
		if e.Recv != nil {
			kids = append(kids, nodef("recv", d.cexpr(e.Recv)))
		}
		kids = append(kids, d.catoms(e.Args)...)
		return nodef(text, kids...)
	case *core.CallBuiltin:
		return nodef(chead("builtin", e.Base)+" "+d.symRef(e.Sym), d.catoms(e.Args)...)
	case *core.CtorApp:
		text := chead("ctorapp", e.Base) + " " + d.symRef(e.Ctor)
		return nodef(text+fmt.Sprintf(" arity=%d", e.Arity)+targs(e.TypeArgs), d.catoms(e.Args)...)
	case *core.TailCall:
		return nodef(chead("tailcall", e.Base)+" to="+d.toLabel(e.To), d.catoms(e.Args)...)
	case *core.EscapeResult:
		return nodef(d.escapeHead("escaperesult", e.Base, e.To, e.Wrap), d.cexpr(e.X))
	case *core.EscapeCall:
		return nodef(d.escapeHead("escapecall", e.Base, e.To, e.Wrap), d.cexpr(e.Call))
	case *core.ResultFromGo:
		return nodef(chead("resultfromgo", e.Base), d.cexpr(e.Call))
	case *core.OptionFromGo:
		return nodef(chead("optionfromgo", e.Base), d.cexpr(e.Call))
	case *core.TupleFromGo:
		return nodef(chead("tuplefromgo", e.Base), d.cexpr(e.Call))
	case *core.Conv:
		return nodef(chead("conv", e.Base)+" "+e.Kind.String(), d.cexpr(e.X))
	default:
		panic("unreachable")
	}
}

// escapeHead renders `name@sp :T to=<fn> [wrap="step"]` — the shared
// head of EscapeResult and EscapeCall.
func (d *cdump) escapeHead(name string, b core.Base, to *core.Func, wrap string) string {
	text := chead(name, b) + " to=" + d.toLabel(to)
	if wrap != "" {
		text += " wrap=" + quote(wrap)
	}
	return text
}

// --- select arms ---

func (d *cdump) carm(a core.SelArm) *form {
	switch a := a.(type) {
	case *core.SelRecv:
		return nodef("recv"+at(a.Sp), d.cbind("bind", a.Bind), d.cexpr(a.Chan), d.cbody(a.Body))
	case *core.SelSend:
		return nodef("send"+at(a.Sp), d.cexpr(a.Chan), d.cexpr(a.Value), d.cbody(a.Body))
	case *core.SelDefault:
		return nodef("default"+at(a.Sp), d.cbody(a.Body))
	default:
		panic("unreachable")
	}
}

// --- decision trees ---

// cdtree renders the compiled-match tree: `(dswitch@sp :T kind subj
// (case ...) (default ...))` / `(leaf ...)` / `(panic ...)` /
// `(guard ...)`.
func (d *cdump) cdtree(t core.DTree) *form {
	if t == nil {
		return nodef("?")
	}
	switch t := t.(type) {
	case *core.DSwitch:
		kids := make([]*form, 0, len(t.Cases)+2)
		kids = append(kids, d.cexpr(t.Subj))
		for _, c := range t.Cases {
			ck := make([]*form, 0, len(c.Binds)+2)
			ck = append(ck, d.ctest(c.Test))
			for _, b := range c.Binds {
				ck = append(ck, d.cbind("bind", b))
			}
			ck = append(ck, d.cbody(c.Body))
			kids = append(kids, nodef("case"+at(c.Sp), ck...))
		}
		kids = append(kids, nodef("default", d.cdtree(t.Default)))
		return nodef(chead("dswitch", t.Base)+" "+t.Kind.String(), kids...)
	case *core.DLeaf:
		return nodef(chead("leaf", t.Base), d.cbody(t.Body))
	case *core.DPanic:
		return nodef(chead("panic", t.Base)+" "+quote(t.TypeName), d.cexpr(t.Subj))
	case *core.DGuard:
		return nodef(chead("guard", t.Base), d.cexpr(t.Cond), d.cdtree(t.Then), d.cdtree(t.Else))
	default:
		panic("unreachable")
	}
}

func (d *cdump) ctest(t core.DTest) *form {
	if t == nil {
		return nodef("?")
	}
	switch t := t.(type) {
	case *core.DCtorTest:
		return nodef("ctortest" + at(t.Sp) + " " + d.symRef(t.Ctor))
	case *core.DLitTest:
		return nodef("littest" + at(t.Sp) + " " + t.Kind.String() + " " + atom(t.Text))
	case *core.DListTest:
		text := "listtest" + at(t.Sp) + " "
		if t.Cons {
			text += "cons"
		} else {
			text += "nil"
		}
		return nodef(text)
	default:
		panic("unreachable")
	}
}
