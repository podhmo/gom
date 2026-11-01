package pattern

import (
	"strings"
)

// render renders a matrix cell back to Gom pattern syntax — the
// form diagnostics show as counterexamples (spec section 4):
// `Neg (Num _)`, `Some (Cons _ _)`, `[x, ..rest]`.
//
// Constructor applications parenthesize any argument that itself
// carries a payload (`Neg (Add _ _)`); atomic arguments — wildcards,
// literals, zero-arity constructors — print bare, and the
// self-delimiting forms (tuple, list) keep their own brackets.
// Records parenthesize inside constructor arguments because
// `C { f = _ }` would read as the named-field constructor form.
func render(c *cell) string {
	if c == nil || c.key == "" {
		return "_"
	}
	switch c.kind {
	case VLiteral:
		return c.disp
	case VNil:
		return "[]"
	case VCons:
		return renderList(c)
	case VTuple:
		parts := make([]string, len(c.args))
		for i, a := range c.args {
			parts[i] = render(a)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case VRecord:
		parts := make([]string, len(c.args))
		for i, a := range c.args {
			name := "_"
			if i < len(c.names) && c.names[i] != "" {
				name = c.names[i]
			}
			parts[i] = name + " = " + render(a)
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	case VCtor:
		if len(c.args) == 0 {
			return c.disp
		}
		var sb strings.Builder
		sb.WriteString(c.disp)
		for _, a := range c.args {
			sb.WriteByte(' ')
			sb.WriteString(renderArg(a))
		}
		return sb.String()
	default:
		panic("unreachable")
	}
}

// renderArg renders an argument of a constructor pattern, adding
// parentheses where the bare form would misparse or mislead.
func renderArg(c *cell) string {
	if c != nil && c.key != "" {
		switch c.kind {
		case VCtor:
			if len(c.args) > 0 {
				return "(" + render(c) + ")"
			}
		case VCons:
			// Nested patterns are parenthesized (spec section
			// 4.1): `Some ([_, .._])`.
			return "(" + render(c) + ")"
		case VRecord:
			return "(" + render(c) + ")"
		}
	}
	return render(c)
}

// renderList flattens a Cons spine: `Cons(a, Cons(b, Nil))` prints
// `[a, b]`; a non-Nil tail prints `[a, ..t]`.
func renderList(c *cell) string {
	var parts []string
	cur := c
	for cur != nil && cur.kind == VCons && len(cur.args) == 2 {
		parts = append(parts, render(cur.args[0]))
		cur = cur.args[1]
	}
	if cur == nil || cur.kind == VNil {
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return "[" + strings.Join(parts, ", ") + ", .." + render(cur) + "]"
}
