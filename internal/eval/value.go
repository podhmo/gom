package eval

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Value is a runtime value produced by the evaluator. The interface
// is sealed: every variant is declared in this package and type
// switches over Value must end with `default: panic("unreachable")`.
// All values are immutable; String renders a deterministic,
// comparison-ready form (see doc.go and the docs/decisions draft for
// the format).
type Value interface {
	String() string
	isValue()
}

// VUnit is the unit value `()`.
type VUnit struct{}

func (*VUnit) isValue()       {}
func (*VUnit) String() string { return "()" }

// VBool is a Bool.
type VBool struct{ B bool }

func (*VBool) isValue() {}
func (v *VBool) String() string {
	if v.B {
		return "true"
	}
	return "false"
}

// VInt is an Int (and the sized Int32/Int64 literals, which share the
// 64-bit representation).
type VInt struct{ N int64 }

func (*VInt) isValue()         {}
func (v *VInt) String() string { return strconv.FormatInt(v.N, 10) }

// VFloat is a Float.
// VFloat is a float value. W is the machine width the value was
// constructed at — 32 for float32-canonical images, zero/64 for
// ordinary float64. Width only shapes the textual rendering: nested
// inside composites a width-32 value prints at float32 precision
// like the compiled gomShow does for a float32-typed field, where a
// plain float64 print would expand the canonical image
// (`0.10000000149011612` for 0.1).
type VFloat struct {
	F float64
	W int
}

func (*VFloat) isValue() {}
func (v *VFloat) String() string {
	if v.W == 32 {
		return strconv.FormatFloat(v.F, 'g', -1, 32)
	}
	return strconv.FormatFloat(v.F, 'g', -1, 64)
}

// VChar is a Char (a Unicode code point).
type VChar struct{ R rune }

func (*VChar) isValue()         {}
func (v *VChar) String() string { return strconv.QuoteRune(v.R) }

// VStr is a String.
type VStr struct{ S string }

func (*VStr) isValue()         {}
func (v *VStr) String() string { return strconv.Quote(v.S) }

// VTuple is a tuple `(a, b, ...)`.
type VTuple struct{ Elems []Value }

func (*VTuple) isValue() {}
func (v *VTuple) String() string {
	var sb strings.Builder
	sb.WriteByte('(')
	for i, e := range v.Elems {
		if i > 0 {
			sb.WriteString(", ")
		}
		writeOperand(&sb, e)
	}
	sb.WriteByte(')')
	return sb.String()
}

// VList is an immutable `List a`. Elems must not be mutated after the
// value is built.
type VList struct{ Elems []Value }

func (*VList) isValue() {}
func (v *VList) String() string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, e := range v.Elems {
		if i > 0 {
			sb.WriteString(", ")
		}
		writeOperand(&sb, e)
	}
	sb.WriteByte(']')
	return sb.String()
}

// VField is one field of a record or variant payload value. Index is
// the declared position of the field (−1 when the layout is unknown
// because the file declared no type body). Sym and Name are nil-able
// for positional payload slots.
type VField struct {
	Index int
	Name  string
	Sym   *resolve.Symbol
	V     Value
}

// VRecord is a record value. Fields are kept in declaration order
// when the record type is declared in the evaluated file, and in
// literal order otherwise.
type VRecord struct {
	Type   *types.Con
	Fields []VField
}

func (*VRecord) isValue() {}
func (v *VRecord) String() string {
	var sb strings.Builder
	sb.WriteString("{ ")
	for i, f := range v.Fields {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(f.Name)
		sb.WriteString(" = ")
		writeOperand(&sb, f.V)
	}
	sb.WriteString(" }")
	return sb.String()
}

// FieldByIndex returns the field at declared position i, or false.
func (v *VRecord) FieldByIndex(i int) (VField, bool) {
	for _, f := range v.Fields {
		if f.Index == i {
			return f, true
		}
	}
	return VField{}, false
}

// FieldBySym returns the field bound to sym, matching by symbol
// pointer first and by name as a fallback for hand-built values.
func (v *VRecord) FieldBySym(sym *resolve.Symbol, name string) (VField, bool) {
	for _, f := range v.Fields {
		if f.Sym != nil && f.Sym == sym {
			return f, true
		}
	}
	for _, f := range v.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return VField{}, false
}

// VVariant is an ADT variant value: which constructor plus the
// payload fields in declared order. Ctor is the constructor's symbol
// (KindCtor); identity comparisons during matching are symbol pointer
// equality. Type is the variant's nominal type constructor (e.g.
// `Shape`, `Option Int`, `Result a e`) — it may be nil for values
// built from constructors whose type could not be determined.
type VVariant struct {
	Type   *types.Con
	Ctor   *resolve.Symbol
	Name   string // cached Ctor.Name for rendering when Ctor is nil
	Fields []VField
}

func (*VVariant) isValue() {}
func (v *VVariant) String() string {
	name := v.Name
	if name == "" && v.Ctor != nil {
		name = v.Ctor.Name
	}
	if len(v.Fields) == 0 {
		return name
	}
	var sb strings.Builder
	sb.WriteString(name)
	for _, f := range v.Fields {
		sb.WriteByte(' ')
		if f.Name != "" {
			sb.WriteByte('~')
			sb.WriteString(f.Name)
			sb.WriteString(": ")
		}
		writeOperand(&sb, f.V)
	}
	return sb.String()
}

// FieldByIndex returns the payload field at declared position i, or
// false.
func (v *VVariant) FieldByIndex(i int) (VField, bool) {
	if i < 0 || i >= len(v.Fields) {
		return VField{}, false
	}
	f := v.Fields[i]
	if f.Index == i || f.Index < 0 {
		return f, true
	}
	for _, g := range v.Fields {
		if g.Index == i {
			return g, true
		}
	}
	return VField{}, false
}

// IsResult reports whether the value is an Ok or Err of the builtin
// `Result a e` type. localT names the evaluated file's declared
// types: a file-local `type Result` shadows the builtin, so its Ok
// and Err variants are ordinary ADT values here. A nil Type
// (hand-built ASTs that never declared Result) falls back to the
// constructor name.
func (v *VVariant) IsResult(localT map[string]bool) (ok bool, isOk bool) {
	resultType := v.Type != nil && v.Type.Qual == "" && v.Type.Name == "Result" && !localT["Result"]
	name := v.Name
	if name == "" && v.Ctor != nil {
		name = v.Ctor.Name
	}
	switch name {
	case "Ok":
		return resultType || v.Type == nil, true
	case "Err":
		return resultType || v.Type == nil, false
	}
	return false, false
}

// VGoErr is a Go error value (`GoError`). The evaluator cannot call
// Go, so GoError values arise from host functions bound by the
// harness and from the `|?>` auto-wrap of spec section 9.2. Wrapped
// is the wrapped error when the value was produced by a `|?>` step
// (`<step>: <orig>`), preserving the causal chain shape of
// fmt.Errorf("%w").
type VGoErr struct {
	Text    string
	Wrapped Value // nil when not produced by a wrap
}

func (*VGoErr) isValue() {}
func (v *VGoErr) String() string {
	return fmt.Sprintf("error(%s)", strconv.Quote(v.Text))
}

// Error returns the error text — Go's err.Error() equivalent.
func (v *VGoErr) Error() string { return v.Text }

// VHost is an opaque Go value — the evaluator cannot call Go, so
// host values arise only from host functions bound by the harness
// (e.g. a `time.Unix` stub returning a real time.Time). They are
// receivers and arguments of further host calls; String renders the
// Go value's `%v` form so fmt-print comparisons stay faithful.
type VHost struct {
	V any
	// T is the gom type the value carried when it was boxed into
	// `Go.Any` — nil for opaque host values. `Go.try_as` asserts
	// against it at the emitted-Go-type grain (Int and Int64 share
	// int64), the way `x.(T)` does.
	T types.Type
}

func (*VHost) isValue() {}
func (v *VHost) String() string {
	if inner, ok := v.V.(Value); ok {
		// A gom value inside the `any` box displays the way the
		// emitted Go repr would under %v — `[1 2]`, `{a b}` — not
		// the gom spellings String() produces.
		return goDisplay(inner)
	}
	return fmt.Sprintf("%v", v.V)
}

// goDisplay renders an eval value in its emitted Go `%v` shape —
// the fmt form a host call sees through an `any` slot.
func goDisplay(v Value) string {
	switch v := v.(type) {
	case *VStr:
		return v.S
	case *VChar:
		return strconv.FormatInt(int64(v.R), 10)
	case *VUnit:
		return "{}"
	case *VBool, *VInt, *VFloat:
		return v.String()
	case *VList:
		var sb strings.Builder
		sb.WriteByte('[')
		for i, el := range v.Elems {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(goDisplay(el))
		}
		sb.WriteByte(']')
		return sb.String()
	case *VTuple:
		return goDisplayFields(v.Elems)
	case *VRecord:
		elems := make([]Value, len(v.Fields))
		for i, f := range v.Fields {
			elems[i] = f.V
		}
		return goDisplayFields(elems)
	case *VVariant:
		elems := make([]Value, len(v.Fields))
		for i, f := range v.Fields {
			elems[i] = f.V
		}
		return goDisplayFields(elems)
	case *VGoErr:
		return v.Text
	case *VFunc, *VHost:
		return v.String()
	default:
		panic("unreachable")
	}
}

// goDisplayFields renders a struct-shaped `%v` — `{a b c}`.
func goDisplayFields(elems []Value) string {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, el := range elems {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(goDisplay(el))
	}
	sb.WriteByte('}')
	return sb.String()
}

// VFunc is a function value with declared arity and a set of already
// bound parameters (partial application captures them). Applying
// fills remaining parameters in CallInfo order; when all are filled
// the body runs, otherwise a new VFunc with more bindings is the
// result.
type VFunc struct {
	name string
	kind fnKind

	// sig is the callee's declared signature (canonical flat form:
	// params in declaration order, labels preserved). bound lines up
	// with sig.Params.
	sig   *types.Func
	bound []slot

	// flatArity marks parameter slots carried under the flat
	// calling convention (declared >=2-arrow function types on a
	// `fn` declaration — the generated repr is one Go func for the
	// chain). Indexed like bound; 0 or nil means canonical.
	// fnLambda values built for declarations get it from fnValue;
	// lambda literals and other kinds stay nil — their parameters
	// keep the canonical repr in generated code too.
	flatArity []int

	// thunk marks a generalized value-let's FnDecl value (P2-1):
	// the `() -> T` shape is a thunk whose references denote the
	// *value* — forceThunk applies it on lookup. A declared
	// `fn f () -> T` is not a thunk; `f` in value position is the
	// function itself.
	thunk bool

	// tyvars records type-parameter instantiations observed at
	// apply time (param type vs the argument's static type) — the
	// eval-side channel that lets an `any` box under `a` name its
	// Go representation faithfully.
	tyvars map[*types.Var]types.Type

	// kind-specific payload.
	params  []check.Param                     // fnLambda / fnDecl
	body    check.Expr                        // fnLambda / fnDecl
	env     *Env                              // fnLambda / fnDecl captured environment
	builtin func(args []Value) (Value, error) // fnBuiltin
	ctor    *ctorInfo                         // fnCtor
	compose [2]Value                          // fnCompose: [0] takes the input, [1] takes its result
	gocall  *goCall                           // fnGoPartial (_ placeholder)
}

func (*VFunc) isValue() {}
func (v *VFunc) String() string {
	rem := 0
	for _, s := range v.bound {
		if !s.set {
			rem++
		}
	}
	name := v.name
	if name == "" {
		name = "fn"
	}
	return fmt.Sprintf("<fn %s/%d>", name, rem)
}

// Arity returns the declared parameter count of the whole signature.
func (v *VFunc) Arity() int { return len(v.bound) }

// Remaining returns the indices (into the declared parameter list)
// of the parameters not yet bound, in declaration order. It is the
// list CallInfo.ArgToParam indexes for applications of a function
// value.
func (v *VFunc) Remaining() []int {
	var rem []int
	for i, s := range v.bound {
		if !s.set {
			rem = append(rem, i)
		}
	}
	return rem
}

// Name returns the function's display name (symbol name, builtin
// name, or "" for anonymous lambdas).
func (v *VFunc) Name() string { return v.name }

// slot is one parameter slot of a VFunc: set when bound.
type slot struct {
	set bool
	v   Value
}

// writeOperand renders v in operand position: composite values are
// parenthesized so `Add (Num 1) (Num 2)` and `[(1, 2)]` stay
// unambiguous.
func writeOperand(sb *strings.Builder, v Value) {
	switch v.(type) {
	case *VUnit, *VBool, *VInt, *VFloat, *VChar, *VStr, *VTuple, *VList:
		sb.WriteString(v.String())
	case *VRecord, *VVariant, *VGoErr, *VHost, *VFunc:
		sb.WriteByte('(')
		sb.WriteString(v.String())
		sb.WriteByte(')')
	default:
		panic("unreachable")
	}
}

// hostBoxRepr names the emitted Go type a boxed value was recorded
// with. Foreign named scalars need the static tag: their eval
// payload is only the underlying scalar Value, so the payload alone
// cannot carry the nominal type. Opaque host values and boxes whose
// static type is unnameable report false; their payload comparison
// keeps the pre-existing best-effort semantics.
func hostBoxRepr(v *VHost) (string, bool) {
	if v.T == nil {
		return "", false
	}
	r := types.CanonGoRepr(types.GoRepr(v.T))
	if r == "" {
		return "", false
	}
	// GoRepr renders a Go-defined type under its package's base
	// name because that is how the emitted source spells it. Type
	// identity needs the declaring import path too: `a/units.Count`
	// and `b/units.Count` both render `units.Count` but are distinct
	// dynamic types in compiled reflect.DeepEqual. Keep the emitted
	// repr for builtin representation sharing (Int/Int64), then add
	// the exact Go refs as an identity-only suffix.
	return r + "\x00" + goTypeRefKey(v.T), true
}

// goTypeRefKey renders the exact Go-defined type refs inside t in a
// deterministic traversal order. It is not a type spelling — only a
// disambiguator for hostBoxRepr — so the encoding needs no inverse.
func goTypeRefKey(t types.Type) string {
	var sb strings.Builder
	writeGoTypeRefKey(&sb, t)
	return sb.String()
}

func writeGoTypeRefKey(sb *strings.Builder, t types.Type) {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		for _, a := range t.Args {
			writeGoTypeRefKey(sb, a)
		}
	case *types.Tuple:
		for _, el := range t.Elems {
			writeGoTypeRefKey(sb, el)
		}
	case *types.Func:
		for _, p := range t.Params {
			writeGoTypeRefKey(sb, p.Type)
		}
		writeGoTypeRefKey(sb, t.Ret)
	case *types.Go:
		fmt.Fprintf(sb, "G%d:%s%d:%s", len(t.Ref.Path), t.Ref.Path, len(t.Ref.Name), t.Ref.Name)
		for _, a := range t.Args {
			writeGoTypeRefKey(sb, a)
		}
	case *types.Meta, *types.Var:
	default:
		panic("unreachable")
	}
}

// Equal reports structural equality — the `==` semantics of spec
// section 3.3 restricted to non-function types. Function values
// compare by pointer identity (the harness uses Equal on expected
// values; `==` in evaluated programs routes through eqValue, which
// reports a diagnostic on functions instead).
func Equal(a, b Value) bool {
	switch a := a.(type) {
	case *VUnit:
		_, ok := b.(*VUnit)
		return ok
	case *VBool:
		bv, ok := b.(*VBool)
		return ok && a.B == bv.B
	case *VInt:
		bv, ok := b.(*VInt)
		return ok && a.N == bv.N
	case *VFloat:
		bv, ok := b.(*VFloat)
		return ok && a.F == bv.F
	case *VChar:
		bv, ok := b.(*VChar)
		return ok && a.R == bv.R
	case *VStr:
		bv, ok := b.(*VStr)
		return ok && a.S == bv.S
	case *VTuple:
		bv, ok := b.(*VTuple)
		return ok && equalSlice(a.Elems, bv.Elems)
	case *VList:
		bv, ok := b.(*VList)
		return ok && equalSlice(a.Elems, bv.Elems)
	case *VRecord:
		bv, ok := b.(*VRecord)
		if !ok || len(a.Fields) != len(bv.Fields) {
			return false
		}
		for i := range a.Fields {
			if a.Fields[i].Name != bv.Fields[i].Name || !Equal(a.Fields[i].V, bv.Fields[i].V) {
				return false
			}
		}
		return true
	case *VVariant:
		bv, ok := b.(*VVariant)
		if !ok || a.ctorIdentity() != bv.ctorIdentity() || len(a.Fields) != len(bv.Fields) {
			return false
		}
		for i := range a.Fields {
			if !Equal(a.Fields[i].V, bv.Fields[i].V) {
				return false
			}
		}
		return true
	case *VGoErr:
		bv, ok := b.(*VGoErr)
		return ok && a.Text == bv.Text
	case *VHost:
		bv, ok := b.(*VHost)
		if !ok {
			return false
		}
		// Eval keeps foreign named scalars as their underlying
		// Value plus the boxed static type, so DeepEqual on the
		// payload alone would erase the nominal type distinction
		// compiled rt.Equal gets from reflect.DeepEqual
		// (`time.Duration(1)` != `goiface.Ticks(1)` even though
		// both payloads eval as VInt 1). Compare the emitted-Go
		// type identity first when both sides name one.
		if ar, aok := hostBoxRepr(a); aok {
			if br, bok := hostBoxRepr(bv); bok && ar != br {
				return false
			}
		}
		return reflect.DeepEqual(a.V, bv.V)
	case *VFunc:
		bv, ok := b.(*VFunc)
		return ok && a == bv
	default:
		panic("unreachable")
	}
}

func (v *VVariant) ctorIdentity() string {
	if v.Ctor != nil {
		return v.Ctor.Package + "\x00" + v.Ctor.Name
	}
	return "\x00" + v.Name
}

func equalSlice(a, b []Value) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
