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
// no extra Closure stands between `f 20` and `f 20 30`. The single
// exception is a `-> T` callee — a Go zero-parameter signature's
// shape — whose `f ()` application checks to no arguments at all;
// it lowers to a zero-argument CallFunc, a real `f()` call.
type CallFunc struct {
	Base
	Fn   Atom   // a function value — never FuncRef / CtorRef (those are CallDirect / CtorApp)
	Args []Atom // one argument per canonical chain step; empty only for the `-> T` `f()` call
	// FlatArity is the callee's flat arity when Fn names a value in
	// the flat representation — a declared parameter of ≥2-arrow
	// function type emits `func(T1', ..., Tk') R'`, so its full
	// application is one `f(a, b, ...)` call. 0 is the canonical
	// one-argument step (len(Args) is 1 or 0 for `-> T`).
	FlatArity int
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
	// GoResultsNary is `v0, v1, v2, ... := f(...)` — three or more
	// results without a trailing error, mapped to an N-tuple
	// (spec section 8.3). Type() is the raw N-tuple type.
	GoResultsNary
	// GoResultsTupErr is `v0, v1, ..., err := f(...)` — three or
	// more results whose last is an `error`, mapped to
	// `Result (Tuple ...) GoError` (spec section 8.3). Type() is
	// the raw payload tuple type.
	GoResultsTupErr
	// GoResultsNone is a zero-result call, typed Unit but usable only
	// as a discarded statement. Lowering supplies Unit separately.
	GoResultsNone
	// GoResultsNil is a single nil-able result — `v := f(...)` —
	// an extern or standard binding declared `Option` for. Type()
	// is the raw result type (a `*T`, or an `error` under an
	// `Option GoError` declaration); OptionFromGo tests `v != nil`
	// (spec section 8.4).
	GoResultsNil
	// GoResultsErrValue is a single `error` result — `v := f(...)` —
	// a binding declared `GoError` for: the claim that the callee
	// creates an error value and never returns nil. Type() is the
	// raw error type; ErrorFromGo emits the boundary check
	// `if v == nil { panic }` before yielding `v` (spec section
	// 8.4 — a violated claim is a contract violation).
	GoResultsErrValue
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
	case GoResultsNary:
		return "nary"
	case GoResultsTupErr:
		return "tup-err"
	case GoResultsNil:
		return "nil"
	case GoResultsErrValue:
		return "err-value"
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
	Callee *resolve.Symbol // IsGo: KindFunc, or KindMethod with Recv; nil when Fn carries the callee
	Recv   Atom            // receiver for method calls; nil for package functions
	// Fn is the callee as a `Go.FuncN`-typed value — a Go func-typed
	// value applied with the flat Go convention `f(a, b)`, the call
	// a `Go.fnN` adapter emits (docs/decisions/2026-11-18-fn-interop.md).
	// Callee and Recv are nil then; Fn, Recv and Callee are mutually
	// exclusive.
	Fn       Atom
	TypeArgs []types.Type // instantiation for generic Go callees
	Args     []Atom
	Arity    int       // the callee's fixed parameter count (the variadic element counts once)
	Variadic bool      // the callee's last parameter is variadic
	Spread   bool      // the last arg is spliced `a...`; requires Variadic
	Results  GoResults // result shape
	// FlatArgs records the declared arity of each argument's Go
	// parameter slot, parallel to Args: -1 for a non-function
	// parameter, >= 0 for a function-typed parameter whose argument
	// must emit a `func` of that many parameters — a `func(A, B) R`
	// slot wants a two-parameter literal (GoFunc, Uncurry, a flat
	// FuncRef/GoFuncRef/Var) where a canonical one-argument Closure
	// would mistype the call. Lowering fills it from the callee's
	// declared parameter types; core.Validate checks it because the
	// validator has no access to the Go signature oracle.
	FlatArgs []int
	// ResNilable reports whether result 0's Go type is a
	// method-bearing interface — a declared non-Option `Result T
	// GoError` payload over it is the never-nil claim the boundary
	// panics on (`if err == nil && v == nil`). lower.GoSig carries
	// the same fact from go/types.
	ResNilable bool
	// ChanMake marks the `rt.MakeChan` call — codegen emits the
	// bare `make(chan T, n)` builtin for it (vet's sigchanyzer
	// cannot see the capacity through the generic helper). The
	// calling convention is decided at lowering so codegen never
	// re-derives it from the callee's name (AGENTS.md).
	ChanMake bool
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
	// Show is the display recipe — set for `assert_eq`, where it
	// describes how the operand type renders to text so codegen
	// emits the displayed operands of rt.AssertEq through it
	// (docs/design.md section 4.8), and for `display`, where it
	// renders the spliced value (docs/design.md section 4.8 —
	// composite splices reuse the gomShow machinery), and for
	// `snapshot`, where it renders the recorded value. Nil for
	// every other builtin.
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
	Ctor  *resolve.Symbol // KindCtor
	Args  []Atom
	Arity int // the variant's declared payload count; len(Args) == Arity
	// Fields are the variant's declared payload slots — their names
	// and labels — so the variant literal emits `T_C{Name: v}` for
	// constructors codegen has no declaration table for (sibling-
	// file and imported ADTs). Nil means positional-only slots,
	// emitted V0, V1, ...
	Fields []TField
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
	// Prepend is the cons-loop specialization's element payload
	// (docs/design.md section 4.5 — lower's cons-recursion pass).
	// The node's source shape was `[h0, .., hk, ..self args]` — a
	// self call feeding a list cons — so the loop restart first
	// appends the cons heads to the function's build buffer, then
	// rebinds and continues. Non-empty only when To.ConsLoop is
	// set; nil for an ordinary tail call.
	Prepend []Atom
}

func (*TailCall) isExpr() {}

// EscapeResult is the `try e` / `|?>` escape on a Result-typed value
// (flat2 representation, P0-4): it emits `if x.Tag() == ErrTag {
// return <the To function's Err construction> }` and evaluates to
// x's Ok payload. To is the innermost enclosing Func — the named
// function boundary the escape leaves through — whose result type
// must be `Result _ e` with e matching x's error type.
//
// Wrap is the `|?>` step name — the error escapes inside an
// `rt.StepErr(Wrap, err)` frame whose rendered text is
// `Wrap + ": " + err.Error()` and whose Unwrap preserves the
// errors.Is/As chain; it is "" for `try`, which never wraps, and
// for `|?>` steps whose error type is not GoError. The choice is
// fixed at lowering.
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
	// NilGuard marks a declared never-nil payload claim over an
	// `(error, error)` call — `Result GoError GoError` where Go's
	// types cannot prove the first result non-nil. The emitted
	// code verifies it after the err escape: `if v == nil { panic }`
	// — a `(nil, nil)` return is a contract violation (spec
	// section 8.6). Valid only under GoResultsErr.
	NilGuard bool
}

func (*EscapeCall) isExpr() {}

// ResultFromGo materializes a `Result T GoError` from a `(T, error)`
// Go call — the naive (non-fused) boundary conversion, emitted as a
// runtime/helper application over the call's result pair
// (`result.FromGo(...)` / `result.FromErr(...)`). Under
// GoResultsTupErr the call is a `(T, U, ..., error)` shape and the
// materialized payload is a tuple — `Result (Tuple ...) GoError`.
// Use it for Go results that are stored or returned, never inside
// `|?>` / `try` (that is EscapeCall's job).
type ResultFromGo struct {
	Base
	Call *CallGo // Results must be GoResultsErr, GoResultsErrOnly or GoResultsTupErr
	// ValConv wraps the payload variable in a Go conversion when the
	// raw result type differs from the payload's Gom spelling —
	// the `int` → `int64` of `strconv.Atoi` (`int64(v)`). Zero means
	// none. The conversion target is the Result's payload type.
	ValConv ConvKind
	// ElemConv wraps each payload element in a Go conversion under
	// GoResultsTupErr — ElemConv[i] applies to result i; a nil
	// slice or a zero element means none. Conversion targets are
	// the payload Tuple's element types. Unused under GoResultsErr.
	ElemConv []ConvKind
	// NilGuard marks a declared never-nil payload claim — `Result
	// GoError GoError` over `(error, error)`. The emitted code
	// verifies it before materializing: `if err == nil && v == nil
	// { panic }` — a `(nil, nil)` return is a contract violation
	// (spec section 8.6). Valid only under GoResultsErr.
	NilGuard bool
}

func (*ResultFromGo) isExpr() {}

// OptionFromGo materializes an `Option T` from a Go call whose
// absence is reported out-of-band — a comma-ok `(T, bool)` pair, or
// a `*T`/`error` result an `extern` binding declared `Option` for
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

// ErrorFromGo yields the `GoError` value of a lone `error`-result
// Go call whose binding claims the result is never nil — an
// `extern` or standard binding's `-> GoError` declaration
// (spec section 8.4). Go's types cannot prove the claim, so the
// emitted code verifies it: `v := f(...); if v == nil { panic }`,
// then `v` is the value. A nil proves the claim false — a contract
// violation in the sense of spec section 8.6.
type ErrorFromGo struct {
	Base
	Call *CallGo // Results must be GoResultsErrValue
}

func (*ErrorFromGo) isExpr() {}

// TupleFromGo materializes a tuple `(A, B, ...)` from a multi-result
// Go call — covering both `(T, bool)` results kept as tuples, other
// `(A, B)` shapes, and three-or-more-result calls mapped to an
// N-tuple (spec section 8.3).
type TupleFromGo struct {
	Base
	Call *CallGo // Results must be GoResultsOK, GoResultsPair or GoResultsNary
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
	// ConvChanDir narrows a `Chan a` to `SendChan a` or `RecvChan a` —
	// the implicit channel direction conversion of spec section 10.2
	// at Go-call boundaries (`chan<- T(x)` / `(<-chan T)(x)`).
	ConvChanDir
	// ConvOptNil reads a nil-able Go result value into `Option T` —
	// `option.FromGo(v, v != nil)`. It is the Ok-side analogue of
	// OptionFromGo's `*T`/`error` materialization: a `Result
	// (Option T) GoError` view over `(error, error)` (or a declared
	// `*T`-payload pair) applies it to the first result (spec
	// section 8.4).
	ConvOptNil
	// ConvOptToNil writes an `Option T` back to the nil-able Go
	// value — `option.NilOr(v)`: `Some` is the payload, `None` the
	// nil. It is the Gom→Go half of the `(T, error)` boundary — a
	// `Result (Option T) GoError` callback adapted by `Go.funcN`
	// applies it to the first result.
	ConvOptToNil
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
	case ConvChanDir:
		return "chan-dir"
	case ConvOptNil:
		return "opt-nil"
	case ConvOptToNil:
		return "opt-to-nil"
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

// TryAs is the `Go.try_as x : Option T` extraction — a comma-ok Go
// type assertion `v, ok := x.(T)` materialized to `option.FromGo(v,
// ok)` (decision 2026-11-24-go-any-extraction). Typ is the `Option T`
// result; Target is T, the assertion target.
type TryAs struct {
	Base
	X      Atom
	Target types.Type
}

func (*TryAs) isExpr() {}
