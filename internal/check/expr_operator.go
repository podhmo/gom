package check

import (
	"fmt"
	"go/constant"
	"go/token"
	gotypes "go/types"
	"math"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// checkUnary types `-x` (numeric) and `!x` (Bool).
func (c *checker) checkUnary(e *syntax.Unary) Expr {
	switch e.Op {
	case syntax.UnNeg:
		x := c.checkExpr(e.X, nil)
		// A literal directly inside `-` folds into one Go constant
		// expression with it — the range judgment reads the signed
		// value (`-9223372036854775808` is a legal Int64 literal
		// though its digits alone are not). Only the direct operand
		// folds: `- -lit` binds the inner `-lit` to a temporary and
		// the outer minus runs on the variable.
		if lit, ok := x.(*Lit); ok {
			c.markLitNeg(lit)
		}
		c.constrain(x.Type(), e.X.Span(), "the operand of `-`", func(r *metaRec) {
			r.num = true
		})
		return &Unary{Base: baseOf(e.Sp, x.Type()), Op: e.Op, X: x}
	case syntax.UnNot:
		x := c.checkExpr(e.X, types.Bool)
		return &Unary{Base: baseOf(e.Sp, c.boolResult(e.Sp, "`!`")), Op: e.Op, X: x}
	default:
		panic("unreachable")
	}
}

// checkBinary types the binary operators, including the sugared
// pipeline and composition forms, which stay Binary nodes until
// lowering.
func (c *checker) checkBinary(e *syntax.Binary, want types.Type) Expr {
	op := e.Op
	switch op {
	case syntax.BinAdd, syntax.BinSub, syntax.BinMul, syntax.BinDiv:
		return c.checkArith(e, func(r *metaRec) { r.num = true })
	case syntax.BinMod:
		return c.checkArith(e, func(r *metaRec) { r.intg = true })
	case syntax.BinBitAnd, syntax.BinBitAndNot, syntax.BinBitOr,
		syntax.BinBitXor, syntax.BinBitShl, syntax.BinBitShr:
		return c.checkArith(e, func(r *metaRec) { r.intg = true })
	case syntax.BinEq, syntax.BinNe:
		x := c.checkExpr(e.X, nil)
		y := c.checkExpr(e.Y, nil)
		// `a == 42` under a `Go.Any` operand must not let the
		// concrete side face `unify` in want position — a literal's
		// constrained meta binds `any` and fails its own numeric
		// record (`42 == a` reporting "expected a numeric type"),
		// and a bare `int` rep would compare `int64` to `int` and
		// stay false even when it passed. Box the concrete side
		// first so both operands carry the `any` rep — the same
		// materialization a `Go.Any` parameter applies (spec
		// 8.1.7) — then the operand unify is `any == any` on both
		// orderings.
		switch {
		case isGoAny(types.Chase(x.Type())) && !isGoAny(types.Chase(y.Type())):
			y = c.conformExpr(y, types.Chase(x.Type()), e.Y.Span())
		case isGoAny(types.Chase(y.Type())) && !isGoAny(types.Chase(x.Type())):
			x = c.conformExpr(x, types.Chase(y.Type()), e.X.Span())
		}
		// The constraint check runs only when the operands agreed —
		// a failed unify already reported; probing its type again
		// would restate the same mismatch.
		if c.unify(x.Type(), y.Type(), e.Sp) {
			c.constrain(x.Type(), e.Sp, "the operands of `==`", func(r *metaRec) { r.eq = true })
		}
		return &Binary{Base: baseOf(e.Sp, c.boolResult(e.Sp, "`==`")), Op: op, X: x, Y: y}
	case syntax.BinLt, syntax.BinLe, syntax.BinGt, syntax.BinGe:
		x := c.checkExpr(e.X, nil)
		y := c.checkExpr(e.Y, nil)
		if c.unify(x.Type(), y.Type(), e.Sp) {
			c.constrain(x.Type(), e.Sp, "the operands of a comparison", func(r *metaRec) { r.ord = true })
		}
		return &Binary{Base: baseOf(e.Sp, c.boolResult(e.Sp, "a comparison")), Op: op, X: x, Y: y}
	case syntax.BinAnd, syntax.BinOr:
		x := c.checkExpr(e.X, types.Bool)
		y := c.checkExpr(e.Y, types.Bool)
		return &Binary{Base: baseOf(e.Sp, c.boolResult(e.Sp, fmt.Sprintf("`%s`", e.Op))), Op: op, X: x, Y: y}
	case syntax.BinPipeFwd:
		// `x |> f` is `f x` — the callee checks first so its first
		// parameter can seed the argument's want: `Some 1 |> f`
		// (`f : Option Go.Any -> R`) pins `Option[any]` the way
		// `f (Some 1)` does, rather than settling `Option num`
		// before the callee's want can reach it.
		f := c.checkPipeCallee(e.Y, e.X, want, e.Sp)
		argWant := c.pipeArgExpectation(f, e.X, want, e.Sp)
		x, seedFailed := c.checkPipeOperand(e.X, argWant)
		return c.pipeCall(e, x, f, seedFailed)
	case syntax.BinPipeBack:
		// `f PIPE_BACK x` applies f to x — operands in source order;
		// the callee still checks first for the same seeding reason.
		f := c.checkPipeCallee(e.X, e.Y, want, e.Sp)
		argWant := c.pipeArgExpectation(f, e.Y, want, e.Sp)
		x, seedFailed := c.checkPipeOperand(e.Y, argWant)
		return c.pipeCall(e, x, f, seedFailed)
	case syntax.BinPipeTry:
		return c.checkPipeTry(e)
	case syntax.BinComposeFwd:
		// f >> g is x -> g (f x). The middle slot is an application:
		// g's parameter must *accept* f's result — a `Go.Any`
		// parameter takes whatever `f` produces (spec 8.1.7), not
		// type equality, so the second check goes through
		// conformCallee like the pipe's — but the middle value is
		// synthesized inside the lowering, so no argument expression
		// can carry a boundary conversion: the boundary stays a
		// unify-only judgment (conv=false).
		x := c.checkExpr(e.X, nil)
		g := c.checkExpr(e.Y, nil)
		a := c.newMeta(e.Sp, "the composed function's input")
		b := c.newMeta(e.Sp, "the composed function's middle type")
		r := c.newMeta(e.Sp, "the composed function's result")
		ok1 := c.unify(x.Type(), types.Fn(b, types.P(a)), e.X.Span())
		ok2 := c.conformCallee(g.Type(), b, r, e.Y.Span(), false, nil)
		if !ok1 || !ok2 {
			c.markQuiet(a)
			c.markQuiet(b)
			c.markQuiet(r)
		}
		return &Binary{Base: baseOf(e.Sp, types.Fn(r, types.P(a))), Op: op, X: x, Y: g}
	case syntax.BinComposeBack:
		// g << f is x -> g (f x) — same middle-slot application as
		// `>>` (see above).
		g := c.checkExpr(e.X, nil)
		f := c.checkExpr(e.Y, nil)
		a := c.newMeta(e.Sp, "the composed function's input")
		b := c.newMeta(e.Sp, "the composed function's middle type")
		r := c.newMeta(e.Sp, "the composed function's result")
		ok1 := c.unify(f.Type(), types.Fn(b, types.P(a)), e.Y.Span())
		ok2 := c.conformCallee(g.Type(), b, r, e.X.Span(), false, nil)
		if !ok1 || !ok2 {
			c.markQuiet(a)
			c.markQuiet(b)
			c.markQuiet(r)
		}
		return &Binary{Base: baseOf(e.Sp, types.Fn(r, types.P(a))), Op: op, X: g, Y: f}
	default:
		panic("unreachable")
	}
}

// checkArith types +,-,*,/ and %: both operands share one numeric
// type (integral for %), which is also the result.
func (c *checker) checkArith(e *syntax.Binary, constrain func(*metaRec)) Expr {
	x := c.checkExpr(e.X, nil)
	y := c.checkExpr(e.Y, nil)
	out := &Binary{Base: baseOf(e.Sp, x.Type()), Op: e.Op, X: x, Y: y}
	if c.unify(x.Type(), y.Type(), e.Sp) {
		c.constrain(x.Type(), e.Sp, "the operands of an arithmetic operator", constrain)
		// A constant-zero divisor is always wrong — for an integer
		// division Go itself rejects `x / 0` (there is no integer
		// infinity to produce) and a constant division like `10 / 0`
		// is not even compilable Go. Whether `x / 0.0` is legal IEEE
		// arithmetic depends on the operand type, so the decision is
		// deferred to finalize — see pendingDivs. A quiet operand
		// (an unresolved name, say) already reports elsewhere. The
		// zero check itself is deferred too: `x / (1 / 2)` only
		// folds to zero once the nested division's integral type is
		// fixed by inference or defaulting — a node queued only when
		// it folded zero *now* would skip that later-discovered
		// zero and emit a runtime panic instead of E0414.
		if (e.Op == syntax.BinDiv || e.Op == syntax.BinMod) && !c.isQuiet(x.Type()) {
			c.pendingDivs = append(c.pendingDivs, out)
		}
	}
	return out
}

// revisitDivs reports E0414 for each `/` or `%` whose right operand
// folded to a constant zero — a legality judgment on the written
// expression, using constFold's source-level domain. It runs from
// finalize, after the defaulting pass, so an operand meta is judged
// by its solved type:
//
//   - integral operand (Int, Int32, Int64 — including metas that
//     defaulted there): the division is reported, dividend or not —
//     `x / 0` is a division by a constant zero, and for `x / (1-1)`
//     folding the divisor is what makes the zero visible;
//   - Float operand: reported only when the dividend is also a
//     constant expression (`10.0 / 0.0` folds to an invalid
//     constant; `x / 0.0` is the IEEE division to +Inf that Go
//     allows — and the spec's per-operation semantics produces);
//   - still-unbound operand meta: left alone — the meta's own E0407
//     (or quiet recovery) covers the expression. A non-numeric
//     operand (`true / 0`) already reports the num constraint —
//     piling E0414 on it would cascade.
func (c *checker) revisitDivs() {
	for _, b := range c.pendingDivs {
		// Re-judge the divisor with the solved types: a divisor
		// that only folds to zero after defaulting (`x / (1 / 2)`
		// under an integral outer division) is still E0414, and a
		// non-constant divisor is just skipped.
		if !c.constZero(b.Y) {
			continue
		}
		// A defined-type declared type (`type X F` for a one-field
		// record) divides by its underlying F's rules — `n / 0`
		// with `n: UserId` panics like `int64 / 0`.
		t := c.definedUnderlying(types.Chase(b.X.Type()))
		gk, isGo := c.namedBasicKind(t)
		switch {
		case isIntegral(t) || (isGo && namedIntegral(gk)):
			// Always invalid — see above.
		case isFloat(t) || (isGo && namedFloat(gk)):
			if _, ok := c.constFold(b.X); !ok {
				continue
			}
		default:
			continue
		}
		c.diags.Add(diag.Error("E0414", "division by a constant zero").
			At(b.Y.Span()).Set("op", b.Op.String()))
	}
}

// constZero reports whether e is a constant expression that
// evaluates to numeric zero — see constFold for the folded shapes.
func (c *checker) constZero(e Expr) bool {
	v, ok := c.constFold(e)
	return ok && constant.Sign(v) == 0
}

// constFold evaluates e as a source-level constant expression and
// returns its value, or false when e is not constant. The folded
// shapes are the ones spec 3.3's E0414 rule spells out — numeric
// literals, unary `-`, the arithmetic binary operators over
// constants, and type ascriptions — evaluated with go/constant's
// exact arithmetic (arbitrary precision integers and rationals, so
// the fold agrees with the compiler's judgment, not float64
// arithmetic). This is a legality judgment on the written
// expression, not on the emitted code: lowering binds literal-only
// numeric operations through variables where a Go constant fold
// could diverge from per-operation semantics, so the emitted Go is
// never a *divergent* constant expression (decision
// 2026-11-05-numeric-arithmetic-per-operation). Everything
// else is not a constant: variables and named constants (a `let`
// binding emits a Go variable, not a Go constant — `let z = 0`
// leaves `x / z` a runtime panic, matching Go), function calls,
// blocks and branching expressions. One consequence of treating
// references as opaque: a *Go* constant used as a divisor
// (`x / pkg.Zero`) still emits a division Go rejects — folding
// imported constants is out of scope for this check.
func (c *checker) constFold(e Expr) (constant.Value, bool) {
	switch e := e.(type) {
	case *Lit:
		var tok token.Token
		switch e.Kind {
		case syntax.LitInt:
			tok = token.INT
		case syntax.LitFloat:
			tok = token.FLOAT
		default:
			return nil, false
		}
		v := constant.MakeFromLiteral(e.Text, tok, 0)
		if v.Kind() == constant.Unknown {
			return nil, false
		}
		return v, true
	case *Unary:
		if e.Op != syntax.UnNeg {
			return nil, false
		}
		v, ok := c.constFold(e.X)
		if !ok {
			return nil, false
		}
		return constant.UnaryOp(token.SUB, v, 0), true
	case *Binary:
		// An integral-typed expression folds at its machine width:
		// `1 / 2` truncates to 0 there, so `(1/2)*2-1` divides by
		// -1 — the exact-rational fold would mint a false E0414.
		if w, us, ok := c.intFoldWidth(e.Type()); ok {
			if v, ok2 := c.foldIntAt(e, w, us); ok2 {
				return v, true
			}
		}
		x, xok := c.constFold(e.X)
		y, yok := c.constFold(e.Y)
		// A zero side decides `*` (and the dividend side of `/`
		// and `%`) even when the other side does not fold —
		// `conv * 0` is zero however unanalyzable the cast is.
		// Integral domain only: `x * 0.0` with a Float variable is
		// IEEE arithmetic (x/0.0 → +Inf is legal Go), not a
		// compile-time zero the checker may judge.
		dt := c.definedUnderlying(types.Chase(e.Type()))
		gk, isGo := c.namedBasicKind(dt)
		if isIntegral(dt) || (isGo && namedIntegral(gk)) {
			switch e.Op {
			case syntax.BinMul:
				if (xok && constant.Sign(x) == 0) || (yok && constant.Sign(y) == 0) {
					return constant.MakeInt64(0), true
				}
			case syntax.BinDiv, syntax.BinMod:
				if xok && constant.Sign(x) == 0 {
					return constant.MakeInt64(0), true
				}
			}
		}
		if !xok || !yok {
			return nil, false
		}
		var op token.Token
		switch e.Op {
		case syntax.BinAdd:
			op = token.ADD
		case syntax.BinSub:
			op = token.SUB
		case syntax.BinMul:
			op = token.MUL
		case syntax.BinDiv:
			op = token.QUO
		case syntax.BinMod:
			op = token.REM
		case syntax.BinBitAnd:
			op = token.AND
		case syntax.BinBitAndNot:
			op = token.AND_NOT
		case syntax.BinBitOr:
			op = token.OR
		case syntax.BinBitXor:
			op = token.XOR
		case syntax.BinBitShl:
			op = token.SHL
		case syntax.BinBitShr:
			op = token.SHR
		default:
			return nil, false
		}
		// constant.BinaryOp panics on a division by zero — and the
		// nested node that actually divides by zero reports its own
		// E0414, so this expression simply has no value to fold —
		// and on `%` applied to non-integers (a malformed operand
		// already diagnosed against the intg constraint).
		if (op == token.QUO || op == token.REM) && constant.Sign(y) == 0 {
			return nil, false
		}
		// % and the bitwise operators are integral-only; shifts also
		// need a non-negative count or BinaryOp panics.
		intOnly := op == token.REM || op == token.AND || op == token.AND_NOT ||
			op == token.OR || op == token.XOR || op == token.SHL || op == token.SHR
		if intOnly && (x.Kind() != constant.Int || y.Kind() != constant.Int) {
			return nil, false
		}
		if op == token.SHL || op == token.SHR {
			// constant.BinaryOp does not take shifts — they
			// fold through Shift. The count itself is a folded
			// machine value: it wraps at the count operand's own
			// width when that is known — and its intermediate
			// folds are machine values too: `(2147483647 + 1) % 64`
			// at Int32 is -2^31 % 64 = 0, not the unbounded
			// 2^31 % 64 — so it re-folds at that width rather
			// than wrapping only the top. A negative count is a
			// runtime panic, not a folding concern.
			if cw, cu, ok := c.intFoldWidth(e.Y.Type()); ok {
				// foldIntAt is authoritative when the count has a
				// width — its unbounded value could hide a wrap to
				// zero (a `%` whose divisor wrapped) or a negative
				// count the machine would panic on.
				var wok bool
				y, wok = c.foldIntAt(e.Y, cw, cu)
				if !wok {
					return nil, false
				}
			}
			s, exact := constant.Int64Val(y)
			if !exact || s < 0 {
				return nil, false
			}
			// go/constant is arbitrary-precision but the machine
			// operation wraps at the operand's width — `(255 <<< 60)
			// >>> 68` folds to 0 unwrapped where the wrapped value
			// stays nonzero, which would mint a false E0414. The
			// operand is wrapped too: an earlier `+`/`-`/`*` or
			// `<<<` may have folded a value past the width already
			// (`(MaxInt + 1) >>> 63` must see the wrapped -2^63, not
			// the unbounded 2^63).
			if w, us, ok := c.intFoldWidth(e.Type()); ok {
				return shiftConst(x, op, uint64(s), w, us), true
			}
			// An unsolved meta (a bare literal chain) carries no
			// width. The fold is still safe when 32- and 64-bit
			// agree — `1 >>> 1` is 0 at every width; `255 <<< 60`
			// diverges, so it stays unfolded rather than fold to a
			// machine value only one width produces.
			v32 := shiftConst(x, op, uint64(s), 32, false)
			v64 := shiftConst(x, op, uint64(s), 64, false)
			if !constant.Compare(v32, token.EQL, v64) {
				return nil, false
			}
			return v64, true
		}
		return constant.BinaryOp(x, op, y), true
	case *Ascribe:
		// `Go.conv e : T` emits `T(e)` — e evaluates at its own
		// machine width, then converts: a computed integer value
		// wraps at T's width (`Go.conv (65536 * 65536) : Int32`
		// is 0, not 2^32 — a bare literal is E0417 before folding,
		// so the wrap only meets computed values); a conversion to
		// a widthless named type is the identity machine value,
		// so it folds through unchanged.
		var v constant.Value
		var ok bool
		if w2, u2, ok2 := c.intFoldWidth(e.X.Type()); ok2 {
			v, ok = c.foldIntAt(e.X, w2, u2)
		} else {
			v, ok = c.constFold(e.X)
		}
		if !ok {
			return nil, false
		}
		if w, us, ok := c.intFoldWidth(e.Type()); ok {
			// A machine conversion truncates a fractional value
			// toward zero — `Go.conv (1.0 / 2.0) : Int` is 0 in
			// the emitted `int64(0.5)`, where keeping the folded
			// 0.5 would hide a real division-by-zero panic (and
			// `constant.ToInt` refuses fractions outright). The
			// float path folds in float64 — the emitted code
			// rounds every operation — and an out-of-range
			// conversion is implementation-dependent in Go, so
			// the fold declines rather than claim a value.
			if c.floatKindOf(e.X.Type()) {
				fv, ok := c.foldFloat(e.X)
				if !ok {
					return nil, false
				}
				// float→int conversion out of the TARGET's range is
				// implementation-dependent in Go — `int32(2^32)`
				// is -2^31 on amd64, not the wrapped 0 an int64
				// round-trip would suggest — so the fold declines.
				if us {
					// Truncation happens before the range judgment: a
					// float in (-1, 0) converts to the representable
					// unsigned zero, while -1 itself is out of range.
					if fv <= -1 || fv >= math.Pow(2, float64(w)) {
						return nil, false
					}
				} else if fv < -math.Pow(2, float64(w-1)) || fv >= math.Pow(2, float64(w-1)) {
					return nil, false
				}
				return wrapConstInt(constant.MakeInt64(int64(fv)), w, us), true
			}
			if v.Kind() != constant.Int {
				fv, ok := constant.Float64Val(v)
				if !ok {
					return nil, false
				}
				v = constant.MakeInt64(int64(fv))
			}
			return wrapConstInt(v, w, us), true
		}
		return v, true
	case *Var, *Apply, *Hole, *MemberExpr, *StrInterp, *Lambda, *If,
		*Match, *Block, *Let, *Ignore, *Try, *FieldAssign,
		*GoExpr, *For, *SelectExpr, *ListLit, *Spread, *TupleLit,
		*RecordLit:
		return nil, false
	default:
		panic("unreachable")
	}
}

// foldFloat evaluates a Float-typed constant expression in the
// float64 domain — the semantics the emitted Go computes in, where
// every operation rounds — so a divisor folded for a numeric
// conversion cannot mint a false zero from exact arithmetic
// (`Go.conv 0.999999999999999999 : Int` converts a float64 that
// already rounded to 1.0). Non-float shapes are not foldable.
func (c *checker) foldFloat(e Expr) (float64, bool) {
	switch e := e.(type) {
	case *Lit:
		if e.Kind != syntax.LitFloat && e.Kind != syntax.LitInt {
			return 0, false
		}
		v, ok := constant.Float64Val(constant.MakeFromLiteral(e.Text, token.FLOAT, 0))
		if !ok {
			return 0, false
		}
		return c.narrowFloat(v, e.Type()), true
	case *Unary:
		if e.Op != syntax.UnNeg {
			return 0, false
		}
		v, ok := c.foldFloat(e.X)
		if !ok {
			return 0, false
		}
		return c.narrowFloat(-v, e.Type()), true
	case *Binary:
		x, ok := c.foldFloat(e.X)
		if !ok {
			return 0, false
		}
		y, ok := c.foldFloat(e.Y)
		if !ok {
			return 0, false
		}
		var v float64
		switch e.Op {
		case syntax.BinAdd:
			v = x + y
		case syntax.BinSub:
			v = x - y
		case syntax.BinMul:
			v = x * y
		case syntax.BinDiv:
			if y == 0 {
				return 0, false
			}
			v = x / y
		default:
			return 0, false
		}
		return c.narrowFloat(v, e.Type()), true
	case *Ascribe:
		if c.floatKindOf(e.Type()) {
			var fv float64
			if c.floatKindOf(e.X.Type()) {
				// `Go.conv f : Float` is the identity in the
				// float domain — and so is a defined type emitting
				// as a Go float (`type Score = {v: Float}`), whose
				// conversion keeps the same bits.
				v, ok := c.foldFloat(e.X)
				if !ok {
					return 0, false
				}
				fv = v
			} else {
				// `Go.conv e : Float` on a non-float evaluates e in
				// ITS domain first — `Go.conv (1/2) : Float` is
				// float64(0), not 0.5: integer division keeps its
				// truncation.
				v, ok := c.constFold(e.X)
				if !ok {
					return 0, false
				}
				fv, ok = constant.Float64Val(v)
				if !ok {
					return 0, false
				}
			}
			// The conversion itself narrows: `Go.conv f : Go.float32`
			// rounds to float32 precision — f64-only folding of
			// `Go.conv 0.999999999 : Go.float32` would mint a false
			// zero where Go yields 1.0.
			return c.narrowFloat(fv, e.Type()), true
		}
		// `Go.conv e : T` for an integral T evaluates e then
		// converts — re-fold through constFold, which handles
		// the int path itself.
		v, ok := c.constFold(e)
		if !ok {
			return 0, false
		}
		fv, ok := constant.Float64Val(v)
		return fv, ok
	case *Var, *Apply, *Hole, *MemberExpr, *StrInterp, *Lambda, *If,
		*Match, *Block, *Let, *Ignore, *Try, *FieldAssign,
		*GoExpr, *For, *SelectExpr, *ListLit, *Spread, *TupleLit,
		*RecordLit:
		return 0, false
	default:
		panic("unreachable")
	}
}

// floatKindOf reports whether a conversion target or operand
// lives in the float64 domain — builtin Float, a defined type
// emitting as a Go float (`type Score = {v: Float}` →
// `type Score float64`), or a named Go float type.
func (c *checker) floatKindOf(t types.Type) bool {
	dt := c.definedUnderlying(types.Chase(t))
	gk, isGo := c.namedBasicKind(dt)
	return isFloat(dt) || (isGo && namedFloat(gk))
}

// narrowFloat rounds a folded value to the precision the node's own
// type computes in — a float32 (builtin or via a defined/Go named
// type) keeps float32 rounding, everything else stays float64.
func (c *checker) narrowFloat(v float64, t types.Type) float64 {
	dt := c.definedUnderlying(types.Chase(t))
	if gk, ok := c.namedBasicKind(dt); ok && gk == gotypes.Float32 {
		return float64(float32(v))
	}
	return v
}

// intFoldWidth gives the machine width — and its signedness — that
// constFold wraps a literal shift result at: the width of the
// builtin integral type the emitted Go type carries (`int`/`int64`/
// `int32`, `uint64` for `Uint`/`Uint64`, `rune`'s int32 for Char, `uint8` for Byte), or — through
// the UnderlyingSource oracle — a named Go type's integer
// underlying (time.Duration's int64, os.FileMode's uint32), or a
// declared defined type's field. A width we cannot know (a struct-
// emitted record, an unresolved meta, a non-integer underlying)
// folds conservatively instead.
func (c *checker) intFoldWidth(t types.Type) (w uint, unsigned bool, ok bool) {
	// A defined-type declared type folds at its field's width —
	// `type U32 = {v: Uint32}` emits `type U32 uint32` and divides
	// like uint32. (The peel is a no-op for records that stay
	// structs.)
	t = c.definedUnderlying(types.Chase(t))
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if t.Qual != "" {
			return 0, false, false
		}
		switch t.Name {
		case "Int", "Int64":
			return 64, false, true
		case "Uint", "Uint64":
			return 64, true, true
		case "Int32", "Char":
			return 32, false, true
		case "Byte":
			return 8, true, true
		}
	case *types.Go:
		bits, us, floating, ok := c.goLitRange(t)
		if !ok || floating {
			return 0, false, false
		}
		return uint(bits), us, true
	case *types.Func, *types.Meta, *types.Tuple, *types.Var:
		// Not integral constants — fold conservatively instead.
		return 0, false, false
	}
	return 0, false, false
}

// wrapConstInt interprets an arbitrary-precision constant as the
// machine integer of width w — signed by default, unsigned when the
// type is (`Byte`, a named `uint32` Go type, ...) — the value the
// emitted machine operation produces where Go's own constant fold
// would overflow.
func wrapConstInt(v constant.Value, w uint, unsigned bool) constant.Value {
	var mask uint64
	if w >= 64 {
		mask = math.MaxUint64
	} else {
		mask = (uint64(1) << w) - 1
	}
	u := constant.BinaryOp(v, token.AND, constant.MakeUint64(mask))
	uu, _ := constant.Uint64Val(u)
	if unsigned {
		return constant.MakeUint64(uu)
	}
	switch w {
	case 8:
		return constant.MakeInt64(int64(int8(uu)))
	case 16:
		return constant.MakeInt64(int64(int16(uu)))
	case 32:
		return constant.MakeInt64(int64(int32(uu)))
	}
	return constant.MakeInt64(int64(uu))
}

// shiftConst folds a constant shift at machine width w: the operand
// wraps to w bits first (a previous fold may hold an out-of-range
// value), and a left shift's result wraps as well — `constant.Shift`
// is arbitrary-precision where the machine operation is not.
func shiftConst(x constant.Value, op token.Token, s uint64, w uint, unsigned bool) constant.Value {
	x = wrapConstInt(x, w, unsigned)
	// A count at or past the width empties the value without ever
	// materializing a bigint — `1 <<< (2^62)` is 0, and a right
	// shift leaves the sign bit's fill: 0 for a non-negative
	// operand, -1 for a negative one. Unsigned shifts always empty
	// to 0.
	if s >= uint64(w) {
		if op == token.SHL || unsigned {
			return constant.MakeUint64(0)
		}
		if constant.Sign(x) < 0 {
			return constant.MakeInt64(-1)
		}
		return constant.MakeInt64(0)
	}
	if unsigned {
		// An unsigned right shift fills zeros — fold the masked
		// bits logically rather than constant.Shift's signed
		// arithmetic. The wrapped operand is already non-negative
		// and masked to w bits.
		uu, _ := constant.Uint64Val(x)
		if op == token.SHR {
			return constant.MakeUint64(uu >> uint(s))
		}
		var mask uint64
		if w >= 64 {
			mask = math.MaxUint64
		} else {
			mask = (uint64(1) << w) - 1
		}
		return constant.MakeUint64((uu << uint(s)) & mask)
	}
	v := constant.Shift(x, op, uint(s))
	if op == token.SHL {
		v = wrapConstInt(v, w, unsigned)
	}
	return v
}

// foldIntAt folds a constant integer subtree at machine width w —
// the same shapes constFold admits, but every step wraps so the
// value is what the machine computes, not the unbounded constant.
// A shift count operand uses it when the count has a width. Each
// node's own width governs its fold: a `Go.conv … : Int32` subterm
// inside an Int count still wraps at 32.
func (c *checker) foldIntAt(e Expr, w uint, unsigned bool) (constant.Value, bool) {
	if w2, u2, ok := c.intFoldWidth(e.Type()); ok {
		w, unsigned = w2, u2
	}
	switch e := e.(type) {
	case *Lit:
		if e.Kind != syntax.LitInt {
			return nil, false
		}
		v := constant.MakeFromLiteral(e.Text, token.INT, 0)
		if v.Kind() == constant.Unknown {
			return nil, false
		}
		return wrapConstInt(v, w, unsigned), true
	case *Unary:
		if e.Op != syntax.UnNeg {
			return nil, false
		}
		x, ok := c.foldIntAt(e.X, w, unsigned)
		if !ok {
			return nil, false
		}
		return wrapConstInt(constant.UnaryOp(token.SUB, x, 0), w, unsigned), true
	case *Binary:
		var op token.Token
		switch e.Op {
		case syntax.BinAdd:
			op = token.ADD
		case syntax.BinSub:
			op = token.SUB
		case syntax.BinMul:
			op = token.MUL
		case syntax.BinDiv:
			op = token.QUO
		case syntax.BinMod:
			op = token.REM
		case syntax.BinBitAnd:
			op = token.AND
		case syntax.BinBitAndNot:
			op = token.AND_NOT
		case syntax.BinBitOr:
			op = token.OR
		case syntax.BinBitXor:
			op = token.XOR
		case syntax.BinBitShl:
			op = token.SHL
		case syntax.BinBitShr:
			op = token.SHR
		default:
			return nil, false
		}
		x, ok := c.foldIntAt(e.X, w, unsigned)
		if !ok {
			return nil, false
		}
		y, ok := c.foldIntAt(e.Y, w, unsigned)
		if !ok {
			return nil, false
		}
		if x.Kind() != constant.Int || y.Kind() != constant.Int {
			return nil, false
		}
		if (op == token.QUO || op == token.REM) && constant.Sign(y) == 0 {
			return nil, false
		}
		if op == token.SHL || op == token.SHR {
			// A uint64 count above MaxInt64 is still a runtime count
			// — and always empties the shifted value — so read the
			// unsigned machine value when the signed decoding overflows.
			s, exact := constant.Int64Val(y)
			var sc uint64
			if exact && s >= 0 {
				sc = uint64(s)
			} else if u, uok := constant.Uint64Val(y); uok {
				sc = u
			} else {
				return nil, false
			}
			return shiftConst(x, op, sc, w, unsigned), true
		}
		if op == token.QUO {
			// Machine integer division truncates toward zero —
			// constant.QUO keeps the exact rational (7/2 stays
			// 7/2), which both mis-folds the value and panics the
			// bitwise AND inside wrapConstInt on the non-Int
			// kind. The operands are already wrapped to machine
			// width, so fixed-width division is exact; Go's own
			// division semantics give MinInt64/-1 = MinInt64.
			if unsigned {
				xu, ex := constant.Uint64Val(x)
				yu, ey := constant.Uint64Val(y)
				if !ex || !ey {
					return nil, false
				}
				return wrapConstInt(constant.MakeUint64(xu/yu), w, unsigned), true
			}
			xi, ex := constant.Int64Val(x)
			yi, ey := constant.Int64Val(y)
			if !ex || !ey {
				return nil, false
			}
			return wrapConstInt(constant.MakeInt64(xi/yi), w, unsigned), true
		}
		return wrapConstInt(constant.BinaryOp(x, op, y), w, unsigned), true
	case *Ascribe:
		// `Go.conv e : T` — e evaluates at its own machine width
		// when the checker knows it; a widthless operand (a literal
		// still awaiting defaulting, an unsolved arithmetic meta)
		// folds unbounded instead, since `T(exact)` is what the
		// machine's conversion produces — folding e at T's width
		// would narrow its arithmetic first (`Go.conv 1 : Int32`
		// in a shift count must still fold to 1). The wrap at T's
		// width below IS the conversion when T carries one.
		var v constant.Value
		var ok bool
		if w2, u2, ok2 := c.intFoldWidth(e.X.Type()); ok2 {
			v, ok = c.foldIntAt(e.X, w2, u2)
		} else {
			v, ok = c.constFold(e.X)
		}
		if !ok {
			return nil, false
		}
		w2, u2, ok2 := c.intFoldWidth(e.Type())
		if !ok2 {
			// T has no fold width — a declared type's conversion
			// is the identity machine value, so it must not wrap
			// at the inherited width.
			return v, true
		}
		if v.Kind() != constant.Int {
			return nil, false
		}
		return wrapConstInt(v, w2, u2), true
	case *Var, *Apply, *Hole, *MemberExpr, *StrInterp, *Lambda, *If,
		*Match, *Block, *Let, *Ignore, *Try, *FieldAssign,
		*GoExpr, *For, *SelectExpr, *ListLit, *Spread, *TupleLit,
		*RecordLit:
		return nil, false
	default:
		panic("unreachable")
	}
}
