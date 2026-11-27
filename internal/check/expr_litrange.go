package check

import (
	"fmt"
	"go/constant"
	"go/token"
	gotypes "go/types"
	"math"
	"strings"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// watchLit records an integer, float or char literal for the
// post-default range judgment (revisitLitRanges). The remaining
// kinds carry concrete types and cannot overflow. A Char's own type
// never overflows either — the lexer bounds its code point below
// int32 — so it is watched only to let a `Go.conv` target's range
// judge it (`tls.CipherSuite('𐀀')` overflows uint16).
func (c *checker) watchLit(l *Lit) {
	if l.Kind != syntax.LitInt && l.Kind != syntax.LitFloat &&
		l.Kind != syntax.LitChar {
		return
	}
	pl := &pendingLit{lit: l}
	c.pendingLits = append(c.pendingLits, pl)
	if c.pendingLitOf == nil {
		c.pendingLitOf = map[*Lit]*pendingLit{}
	}
	c.pendingLitOf[l] = pl
}

// watchPatLit is watchLit for a literal pattern — the pattern's own
// type (the scrutinee's) supplies the range context, and its text
// already spells a `-` when the pattern is negative.
func (c *checker) watchPatLit(p *LitPat) {
	if p.Kind != syntax.LitInt && p.Kind != syntax.LitFloat {
		return
	}
	c.pendingLits = append(c.pendingLits, &pendingLit{pat: p})
}

// markLitNeg records that l is the direct operand of a unary `-`:
// the emitted `-lit` is one Go constant expression, so the range
// judgment reads the signed value.
func (c *checker) markLitNeg(l *Lit) {
	if pl, ok := c.pendingLitOf[l]; ok {
		pl.neg = true
	}
}

// markLitConv records that l is the direct operand of `Go.conv` —
// the emitted `T(lit)` converts the constant to T, so the range
// judgment reads the literal at the target type.
func (c *checker) markLitConv(l *Lit, to types.Type) {
	if pl, ok := c.pendingLitOf[l]; ok {
		pl.conv = to
	}
}

// revisitLitRanges judges every recorded literal against the range of
// the type its emitted Go constant converts at — its own solved type,
// or the `Go.conv` target when the literal is the direct conversion
// operand (decisions/2026-11-05-int-literal-range-check, E0417). It
// runs with revisitDivs/revisitConvs: only then is every literal
// meta bound or defaulted. The unit of judgment is the literal, not
// the enclosing expression — `0 - 2147483648` at Int32 emits
// `tmp - 2147483648`, where the right literal converts alone. A
// float literal's judgment is the representable-float64 question at
// a float target and Go's constant-conversion rule at an integer
// one — `Go.conv 1.5 : Int32` emits `int32(1.5)`, which Go rejects,
// while `int32(2.0)` is legal and then range-checked as an int.
func (c *checker) revisitLitRanges() {
	for _, pl := range c.pendingLits {
		var sp diag.Span
		var text string
		var typ types.Type
		var kind syntax.LitKind
		switch {
		case pl.lit != nil:
			sp, text, typ, kind = pl.lit.Sp, pl.lit.Text, pl.lit.Type(), pl.lit.Kind
		case pl.pat != nil:
			sp, text, typ, kind = pl.pat.Sp, pl.pat.Text, pl.pat.Type(), pl.pat.Kind
			// The literal's matrix key was normalized at whatever
			// the scrutinee's type stood at when its arm was
			// checked — possibly an unsettled meta; a later
			// expression (inside or after the match) may pin it to
			// a named width (Float, a Go float32). At this
			// post-default point the type is final, so re-judge
			// the key once for the pattern matrix.
			pl.pat.Norm = c.normLit(kind, pl.pat.Text, typ)
		default:
			panic("unreachable")
		}
		neg := pl.neg
		if s, ok := strings.CutPrefix(text, "-"); ok {
			// Pattern literals spell their sign.
			text = s
			neg = true
		}
		// litText is the signed spelling the emitted Go constant
		// carries — `-lit` folds sign and digits into one constant,
		// and a negative pattern literal's `-` was just cut from
		// text — so diagnostics name the constant the code spells.
		litText := text
		if neg {
			litText = "-" + text
		}
		judgeT := types.Chase(typ)
		if pl.conv != nil {
			// A `T(lit)` conversion judges at T when T is a
			// range-bearing type — a named Go type answers
			// through the oracle's underlying kind; an
			// interface or another non-numeric target falls
			// back to the literal's own type (the emitted
			// constant still has to convert at it).
			if _, _, _, ok := c.litRange(pl.conv); ok {
				judgeT = types.Chase(pl.conv)
			}
		}
		bits, unsigned, floating, ok := c.litRange(judgeT)
		if !ok {
			continue // still-unsolved meta (its E0407 stands) or a non-numeric type
		}
		// The constant value the emitted Go spells — digits plus
		// the folded sign of a `-lit` or a negative pattern.
		tok := token.INT
		switch kind {
		case syntax.LitFloat:
			// A float literal reaching an integer judgment is a
			// `Go.conv` operand — `T(lit)` is a constant
			// conversion, so Go's rule applies: `int32(2.0)` is
			// legal, `int32(1.5)` is rejected as non-integral.
			tok = token.FLOAT
		case syntax.LitChar:
			tok = token.CHAR
		}
		v := constant.MakeFromLiteral(text, tok, 0)
		if v.Kind() == constant.Unknown {
			continue
		}
		if neg {
			v = constant.UnaryOp(token.SUB, v, 0)
		}
		if floating {
			// Float-representable at the target's width: an int
			// literal can spell more digits than int64 holds
			// (`let m : Float = 99999999999999999999` is legal —
			// `float64(lit)` rounds); a float literal is judged
			// directly. bits is the width — 32 for a named
			// `float32` underlying.
			var f float64
			if bits == 32 {
				f32, _ := constant.Float32Val(v)
				f = float64(f32)
			} else {
				f, _ = constant.Float64Val(v)
			}
			if math.IsInf(f, 0) {
				c.diags.Add(diag.Error("E0417",
					fmt.Sprintf("float literal %s overflows %s", litText, judgeT)).
					At(sp).Set("literal", litText).Set("type", judgeT.String()))
			}
			continue
		}
		fromFloat := false
		if v.Kind() == constant.Float {
			// ToInt performs the conversion the generated constant
			// spells: an integer-valued float converts and joins
			// the range judgment, a fractional one is rejected
			// before the range question is asked.
			iv := constant.ToInt(v)
			if iv.Kind() != constant.Int {
				c.diags.Add(diag.Error("E0417",
					fmt.Sprintf("float literal %s is not an integer; cannot convert to %s", litText, judgeT)).
					At(sp).Set("literal", litText).Set("type", judgeT.String()))
				continue
			}
			v = iv
			fromFloat = true
		}
		// Bounds: signed two's-complement [-2^(bits-1),
		// 2^(bits-1)-1], or [0, 2^bits) for an unsigned
		// underlying (os.FileMode's uint32).
		hi := constant.Shift(constant.MakeInt64(1), token.SHL, uint(bits-1))
		max := constant.BinaryOp(hi, token.SUB, constant.MakeInt64(1))
		min := constant.UnaryOp(token.SUB, hi, 0)
		if unsigned {
			hi = constant.Shift(constant.MakeInt64(1), token.SHL, uint(bits))
			max = constant.BinaryOp(hi, token.SUB, constant.MakeInt64(1))
			min = constant.MakeInt64(0)
		}
		if constant.Compare(v, token.LEQ, max) &&
			constant.Compare(v, token.GEQ, min) {
			continue
		}
		// An int literal reports its exact digits (hex spellings
		// included); a float or char one keeps the spelled form —
		// `float literal 1e100` reads truer than its 101-digit
		// expansion, `'𐀀'` truer than 65536.
		litStr, kindWord := v.ExactString(), "integer"
		if fromFloat {
			litStr, kindWord = litText, "float"
		} else if kind == syntax.LitChar {
			litStr, kindWord = litText, "char"
		}
		c.diags.Add(diag.Error("E0417",
			fmt.Sprintf("%s literal %s overflows %s", kindWord, litStr, judgeT)).
			At(sp).Set("literal", litStr).Set("type", judgeT.String()))
	}
}

// litRange classifies t for the literal range judgment: bits is the
// width of the integer constructors (`Int`/`Int64`/`Go.Int` are 64,
// `Int32`/`Char` 32) or of a float target — floating reports it at
// 64 for `Float` and 32 for a named `float32`. unsigned marks the
// unsigned integer kinds a named Go type's underlying may carry
// (`os.FileMode` is `uint32`): their bounds are [0, 2^bits), not the
// signed range. ok is false for every other type — a named Go type
// with a non-basic or non-numeric underlying (a struct, an
// interface, string, bool, complex), String, Bool, an unsolved meta
// (its own E0407 stands), or a file-local ADT spelling (a shadowed
// `type Int32` never types a numeric literal — its own E0415 covers
// the site).
func (c *checker) litRange(t types.Type) (bits int64, unsigned, floating, ok bool) {
	// A one-field record is the Go defined type `type X F` — the
	// `X(lit)` constant conversion judges at the field's range
	// (`Go.conv 70000 : U32` where `type U32 = {v: Int32}`).
	t = c.definedUnderlying(t)
	switch t := types.Chase(t).(type) {
	case *types.Go:
		// A named Go type's range is the oracle's knowledge, not
		// the checker's — `os.FileMode`'s uint32, `time.Duration`'s
		// int64. The emitted `T(lit)` constant conversion judges
		// at that underlying.
		return c.goLitRange(t)
	case *types.Con:
		if t.Qual == "Go" && t.Name == "Int" {
			// Defensive: `Go.Int` is not spellable in type position
			// (goTypeArity reserves `Go` for Slice/Map), so no literal
			// ever judges here — `int` is 64-bit if one ever did.
			return 64, false, false, true // same range as Int64
		}
		if t.Qual != "" || c.shadowedBuiltin(t.Name) {
			return 0, false, false, false
		}
		switch t.Name {
		case "Int", "Int64":
			return 64, false, false, true
		case "Uint", "Uint64":
			return 64, true, false, true
		case "Int32", "Char":
			return 32, false, false, true
		case "Byte":
			return 8, true, false, true
		case "Float":
			return 64, false, true, true
		}
		return 0, false, false, false
	case *types.Var, *types.Meta, *types.Func, *types.Tuple:
		return 0, false, false, false
	default:
		panic("unreachable")
	}
}

// goLitRange maps the basic kind of a named Go type's underlying —
// the UnderlyingSource oracle's answer for a *types.Go handle — to
// the range the emitted `T(lit)` constant conversion judges at.
// `int`/`uint`/`uintptr` judge at 64 bits, matching the `Go.Int`
// convention. Everything else answers not-ok: a non-numeric basic
// (string, bool, complex) never participates in a constant
// conversion the pair check admitted, so the literal judges at its
// own type instead.
func (c *checker) goLitRange(t *types.Go) (bits int64, unsigned, floating, ok bool) {
	src, ok := c.src.(UnderlyingSource)
	if !ok {
		return 0, false, false, false
	}
	kind, ok := src.GoUnderlyingKind(t)
	if !ok {
		return 0, false, false, false
	}
	switch kind {
	case gotypes.Int, gotypes.Int64:
		return 64, false, false, true
	case gotypes.Int8:
		return 8, false, false, true
	case gotypes.Int16:
		return 16, false, false, true
	case gotypes.Int32:
		return 32, false, false, true
	case gotypes.Uint, gotypes.Uint64, gotypes.Uintptr:
		return 64, true, false, true
	case gotypes.Uint8:
		return 8, true, false, true
	case gotypes.Uint16:
		return 16, true, false, true
	case gotypes.Uint32:
		return 32, true, false, true
	case gotypes.Float32:
		return 32, false, true, true
	case gotypes.Float64:
		return 64, false, true, true
	}
	return 0, false, false, false
}
