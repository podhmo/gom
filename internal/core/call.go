package core

import (
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// CallDirect is a saturated call to a known Gom callee — len(Args)
// equals the declared Arity and the args are already in declaration
// order (labeled-argument reordering happened during lowering, with
// source-order evaluation preserved by the enclosing Lets). The call
// emits as a plain multi-argument Go call.
//
// Over-application — a callee returning a function applied to more
// than Arity arguments — does not exist here: lowering splits it into
// a saturated CallDirect whose result is bound, followed by a
// CallFunc on the bound variable. Partial application likewise does
// not exist as a call: it is an explicit Closure (design.md section
// 2.1, item 2).
type CallDirect struct {
	Base
	Callee   *resolve.Symbol // KindFunc, not IsGo
	TypeArgs []types.Type
	Args     []Atom
	Arity    int // the callee's declared parameter count; len(Args) == Arity
	// Adapter marks the firing call inside the synthesized value
	// adapter — the nested-Closure eta-expansion lowering emits when
	// a declaration whose declared arity exceeds 1 is used as a
	// function value (`let g = f` on `fn f (a) (b)`; FuncRef's doc
	// covers why a bare reference is impossible there). The call is
	// not a user-written call site: it runs only if the adapted
	// value is applied, and it stands where a FuncRef — a *value* —
	// would. @tailrec's requirement pass reads it as R3's
	// value-escape, not as one of the call sites R1/R2 count.
	Adapter bool
}

func (*CallDirect) isExpr() {}

// CallFunc applies a function *value* — a Var, a FieldGet result or
// a GoFuncRef whose callee is not statically a Gom function — to
// exactly one argument: `f(a)`. The canonical value representation
// is one Go func per arrow (spec 1.3's currying made concrete), so
// every application step is a real one-argument Go call and a
// partial application is simply a value of the residual arrow type —
// no extra Closure stands between `f 20` and `f 20 30`.
type CallFunc struct {
	Base
	Fn   Atom   // a function value — never FuncRef / CtorRef (those are CallDirect / CtorApp)
	Args []Atom // exactly one argument — the canonical chain applies one arrow per call
}

func (*CallFunc) isExpr() {}

// GoResults classifies the result shape of a Go callee — the
// convention that decides which consumer nodes may wrap a CallGo.
type GoResults int

const (
	// GoResultsOne is a single result `v := f(...)`.
	GoResultsOne GoResults = iota + 1
	// GoResultsErr is `v, err := f(...)` — the `(T, error)` shape.
	// Type() is the first result's type.
	GoResultsErr
	// GoResultsErrOnly is `err := f(...)` — a lone `error` result,
	// seen from Gom as `Result Unit GoError`. Type() is Unit.
	GoResultsErrOnly
	// GoResultsOK is `v, ok := f(...)` — the `(T, bool)` comma-ok
	// shape. Type() is the first result's type.
	GoResultsOK
	// GoResultsPair is `a, b := f(...)` — any other two-result shape,
	// mapped to a tuple. Type() is the (A, B) tuple type.
	GoResultsPair
	// GoResultsNone is a zero-result call, typed Unit but usable only
	// as a discarded statement. Lowering supplies Unit separately.
	GoResultsNone
	// GoResultsNil is a single `*T` result — `v := f(...)` — an
	// extern binding declared `Option` for. Type() is the raw
	// pointer type; OptionFromGo tests `v != nil` (spec section 8.4).
	GoResultsNil
)

// String returns the shape name for debugging.
func (r GoResults) String() string {
	switch r {
	case GoResultsNone:
		return "none"
	case GoResultsOne:
		return "one"
	case GoResultsErr:
		return "err"
	case GoResultsErrOnly:
		return "err-only"
	case GoResultsOK:
		return "ok"
	case GoResultsPair:
		return "pair"
	case GoResultsNil:
		return "nil"
	default:
		return "GoResults(?)"
	}
}

// CallGo is a call to a Go function or method, emitted with Go's
// calling convention — `pkg.F(a, b)`, `recv.M(a)`, `f(a...)` — with
// no currying. The surface argument tuple is already unpacked into
// positional Args, and the `_` placeholder is gone (lowering wrapped
// the call in a Closure).
//
// Types on and around a CallGo are the *raw* Go-side view: `[]T`
// appears as `Go.Slice T`, a `(T, error)` result reports its first
// component's type, and a `T` that maps to `List` is still its Go
// self. Gom-facing types are built by the explicit consumer nodes —
// ResultFromGo, OptionFromGo, TupleFromGo, EscapeCall — and by Conv,
// per design.md section 2.1 item 3.
//
// A GoResultsNone call may appear only at a discarded Let.X. Other
// shapes besides GoResultsOne require their corresponding multi-result
// consumer and may not appear directly at Let.X or Body.Tail.
type CallGo struct {
	Base
	Callee   *resolve.Symbol // IsGo: KindFunc, or KindMethod with Recv
	Recv     Atom            // receiver for method calls; nil for package functions
	TypeArgs []types.Type    // instantiation for generic Go callees
	Args     []Atom
	Arity    int       // the callee's fixed parameter count (the variadic element counts once)
	Variadic bool      // the callee's last parameter is variadic
	Spread   bool      // the last arg is spliced `a...`; requires Variadic
	Results  GoResults // result shape
}

func (*CallGo) isExpr() {}

// CallBuiltin is a call to a compiler-provided builtin — `assert_eq`,
// `todo`, and friends — whose implementation has no Gom definition
// and is emitted from a fixed table in codegen, dispatched on the
// builtin's symbol.
type CallBuiltin struct {
	Base
	Sym  *resolve.Symbol // KindBuiltin
	Args []Atom
	// Eq is the comparison recipe — set for `assert_eq` only, where
	// it describes how Args[0] and Args[1] compare so codegen emits
	// the verdict as the pre-computed operand of rt.AssertEq. Nil
	// for every other builtin.
	Eq *EqPlan
	// Show is the display recipe — set for `assert_eq` only, where
	// it describes how the operand type renders to text so codegen
	// emits the displayed operands of rt.AssertEq through it
	// (docs/design.md section 4.8). Nil for every other builtin.
	Show *ShowPlan
}

func (*CallBuiltin) isExpr() {}

// CtorApp is a saturated constructor application — a variant
// construction `Shape_Circle{...}` / `Option_Some[A]{...}` with Args
// in the variant's declared payload order (labeled payload arguments
// reordered during lowering, exactly like CallDirect). Partial
// constructor application is an explicit Closure; the bare
// constructor value is a CtorRef.
type CtorApp struct {
	Base
	Ctor     *resolve.Symbol // KindCtor
	TypeArgs []types.Type    // instantiation of the ADT's type parameters
	Args     []Atom
	Arity    int // the variant's declared payload count; len(Args) == Arity
}

func (*CtorApp) isExpr() {}

// TailCall is a self-tail call: a call to the enclosing FuncDecl's
// own function in tail position. It exists only inside a Func with
// TailLoop set, and emits reassign-arguments-and-continue inside the
// enclosing loop (docs/design.md section 4.5 — self-tail recursion
// becomes a Go `for`). Mutual recursion is not covered.
//
// "Tail position" means a Body.Tail slot — at any depth through
// If / Match / SelectExpr branch bodies — of the To function.
// A TailCall may not sit at Let.X or in a non-tail branch.
type TailCall struct {
	Base
	To   *Func  // the enclosing FuncDecl's function — always the innermost enclosing Func
	Args []Atom // exactly len(To.Params)
}

func (*TailCall) isExpr() {}

// EscapeResult is the `try e` / `|?>` escape on a Result-typed value
// (flat2 representation, P0-4): it emits `if x.Tag() == ErrTag {
// return <the To function's Err construction> }` and evaluates to
// x's Ok payload. To is the innermost enclosing Func — the named
// function boundary the escape leaves through — whose result type
// must be `Result _ e` with e matching x's error type.
//
// Wrap is the `|?>` step name — `fmt.Errorf("<step>: %w", err)` is
// emitted around the error before escaping; it is "" for `try`,
// which never wraps, and for `|?>` steps whose error type is not
// GoError. The choice is fixed at lowering.
type EscapeResult struct {
	Base
	X    Atom   // a Result a e value
	To   *Func  // the innermost enclosing function boundary
	Wrap string // the |?> step name to %w-wrap the error in, or ""
}

func (*EscapeResult) isExpr() {}

// EscapeCall is the fused form of EscapeResult for a Go `(T, error)`
// call (design.md section 4.6): `v, err := call(...); if err != nil
// { return <To's Err construction>(wrap err) }` — no Result value is
// materialized on the success path. The node's type is the Go call's
// first result type — raw, like CallGo's; conversions on the payload
// apply after the escape.
type EscapeCall struct {
	Base
	Call *CallGo // Results must be GoResultsErr or GoResultsErrOnly
	To   *Func
	Wrap string
}

func (*EscapeCall) isExpr() {}

// ResultFromGo materializes a `Result T GoError` from a `(T, error)`
// Go call — the naive (non-fused) boundary conversion, emitted as a
// runtime/helper application over the call's result pair
// (`result.FromGo(...)` / `result.FromErr(...)`). Use it for Go
// results that are stored or returned, never inside `|?>` / `try`
// (that is EscapeCall's job).
type ResultFromGo struct {
	Base
	Call *CallGo // Results must be GoResultsErr or GoResultsErrOnly
	// ValConv wraps the payload variable in a Go conversion when the
	// raw result type differs from the payload's Gom spelling —
	// the `int` → `int64` of `strconv.Atoi` (`int64(v)`). Zero means
	// none. The conversion target is the Result's payload type.
	ValConv ConvKind
}

func (*ResultFromGo) isExpr() {}

// OptionFromGo materializes an `Option T` from a Go call whose
// absence is reported out-of-band — a comma-ok `(T, bool)` pair, or
// a `*T` result an `extern` binding declared `Option` for
// (spec section 8.3 and 8.4).
type OptionFromGo struct {
	Base
	Call *CallGo // Results must be GoResultsOK or GoResultsNil
	// ValConv wraps the payload variable in a Go conversion when the
	// raw result type differs from the Option's payload spelling;
	// zero means none. The conversion target is the Option's payload
	// type.
	ValConv ConvKind
}

func (*OptionFromGo) isExpr() {}

// TupleFromGo materializes a tuple `(A, B)` from a two-result Go
// call — covering both `(T, bool)` results kept as tuples and other
// `(A, B)` shapes (spec section 8.3).
type TupleFromGo struct {
	Base
	Call *CallGo // Results must be GoResultsOK or GoResultsPair
	// ElemConv wraps each result variable in a Go conversion when the
	// raw result type differs from the tuple element's Gom spelling —
	// ElemConv[i] applies to result i; a nil slice or a zero element
	// means none. Conversion targets are the Tuple's element types.
	ElemConv []ConvKind
}

func (*TupleFromGo) isExpr() {}

// ConvKind classifies the explicit Go-interop conversion a Conv node
// performs (design.md section 2.1, item 3).
type ConvKind int

const (
	// ConvNum is a numeric conversion `T(x)` — the implicit
	// loss-free number conversions of spec 8.1 item 5 and explicit
	// `Go.conv` numeric conversions.
	ConvNum ConvKind = iota + 1
	// ConvNamed converts between a Go named type and its underlying
	// type — the `Go.conv` of spec 8.4.
	ConvNamed
	// ConvBoxAny boxes a Gom value into a Go `any` — allowed only at
	// `any` parameter positions (spec 8.1 item 7).
	ConvBoxAny
	// ConvIface assigns a Go-origin value to a Go interface-typed
	// parameter under Go's assignability rules (spec 8.1 item 8).
	ConvIface
	// ConvSliceToGo copies a `List a` into a Go-owned `Go.Slice a` —
	// `[]T` — for a Go argument (spec 8.5).
	ConvSliceToGo
	// ConvSliceFromGo copies a `[]T` received from Go into a Gom
	// `List a` (spec 8.5).
	ConvSliceFromGo
	// ConvSliceBox copies a `List a` or `Go.Slice a` into a Go-owned
	// `[]any`, boxing each element — the conversion a `..xs` spread
	// into a `...any` variadic needs, which Go itself cannot express
	// (a []T cannot splice into ...any).
	ConvSliceBox
)

// String returns the kind name for debugging.
func (k ConvKind) String() string {
	switch k {
	case ConvNum:
		return "num"
	case ConvNamed:
		return "named"
	case ConvBoxAny:
		return "box-any"
	case ConvIface:
		return "iface"
	case ConvSliceToGo:
		return "slice-to-go"
	case ConvSliceFromGo:
		return "slice-from-go"
	case ConvSliceBox:
		return "slice-box"
	default:
		return "ConvKind(?)"
	}
}

// Conv is an explicit Go-interop conversion. Every boundary
// adjustment the spec applies implicitly — number conversion, `any`
// boxing, slice copying, interface assignment — is a Conv node; the
// unchecked remainder (Go.conv named conversions) uses the same node
// because conversion legality was already settled at check time.
// The node's Type() is the conversion's result type.
type Conv struct {
	Base
	Kind ConvKind
	X    Atom
}

func (*Conv) isExpr() {}
