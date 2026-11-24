package types

import (
	"fmt"
	"regexp"
	"strings"
)

// CanonGoRepr normalizes a type-name spelling to the name Go's
// runtime reports — predeclared aliases collapse: `rune` is
// `int32` and `byte` is `uint8` (x.(rune) and x.(int32) are the
// same assertion, and instantiations like `Box[rune]` are the
// same type as `Box[int32]`).
var canonAlias = regexp.MustCompile(`\b(?:rune|byte)\b`)

func CanonGoRepr(s string) string {
	return canonAlias.ReplaceAllStringFunc(s, func(w string) string {
		if w == "rune" {
			return "int32"
		}
		return "uint8"
	})
}

// GoRepr renders the emitted Go type name of a gom type — the
// spelling `x.(T)` and reflect use ("int64", "list.List[int64]",
// "pkgbase.Name"). A builtin name shares a declared type's spelling
// only when its arity matches — a user-declared `type List` keeps
// its nominal name. Metas and type parameters answer "" and never
// match (check already requires a concrete target).
func GoRepr(t Type) string {
	switch t := Chase(t).(type) {
	case *Con:
		if t.Qual == "Go" {
			switch {
			case t.Name == "Slice" && len(t.Args) == 1:
				return cat("[]", GoRepr(t.Args[0]))
			case t.Name == "Map" && len(t.Args) == 2:
				return cat("map[", GoRepr(t.Args[0]), "]", GoRepr(t.Args[1]))
			case t.Name == "Int":
				return "int"
			case t.Name == "Any":
				return "any"
			}
			if n, ok := GoFuncArity(t.Name); ok && len(t.Args) >= n {
				// `Go.FuncN` emits the flat `func(p...) (r...)`.
				return FlatFuncRepr(t.Args[:n], t.Args[n:])
			}
			return ""
		}
		if t.Qual != "" {
			if len(t.Args) > 0 {
				if ra := reprArgs(t.Args); ra != "" {
					return t.Qual + "." + t.Name + ra
				}
				return ""
			}
			return t.Qual + "." + t.Name
		}
		switch {
		case t.Name == "Unit":
			return "struct{}"
		case t.Name == "Int" || t.Name == "Int64":
			return "int64"
		case t.Name == "Int32":
			return "int32"
		case t.Name == "Byte":
			return "byte"
		case t.Name == "Float" || t.Name == "Float64":
			return "float64"
		case t.Name == "Char":
			return "rune"
		case t.Name == "String":
			return "string"
		case t.Name == "Bool":
			return "bool"
		case t.Name == "GoError":
			return "error"
		case t.Name == "List" && len(t.Args) == 1:
			return cat("list.List[", GoRepr(t.Args[0]), "]")
		case t.Name == "Option" && len(t.Args) == 1:
			return cat("option.Option[", GoRepr(t.Args[0]), "]")
		case t.Name == "Result" && len(t.Args) == 2:
			return cat("result.Result[", GoRepr(t.Args[0]), ", ", GoRepr(t.Args[1]), "]")
		case t.Name == "Task" && len(t.Args) == 1:
			return cat("rt.Task[", GoRepr(t.Args[0]), "]")
		case t.Name == "View" && len(t.Args) == 1:
			return cat("list.View[", GoRepr(t.Args[0]), "]")
		case (t.Name == "Ptr" || t.Name == "Ref") && len(t.Args) == 1:
			return cat("*", GoRepr(t.Args[0]))
		case t.Name == "Array" && len(t.Args) == 1:
			return cat("[]", GoRepr(t.Args[0]))
		case t.Name == "Chan" && len(t.Args) == 1:
			return cat("chan ", GoRepr(t.Args[0]))
		case t.Name == "RecvChan" && len(t.Args) == 1:
			return cat("<-chan ", GoRepr(t.Args[0]))
		case t.Name == "SendChan" && len(t.Args) == 1:
			return cat("chan<- ", GoRepr(t.Args[0]))
		}
		// Declared record/sum — nominal: asserts only to itself.
		// A generic instantiation names its arguments too, as the
		// emitted `Box[int64]` does (`Box Int` and `Box String`
		// assert differently).
		if len(t.Args) > 0 {
			if ra := reprArgs(t.Args); ra != "" {
				return t.Name + ra
			}
			return ""
		}
		return t.Name
	case *Go:
		base := t.Ref.Path
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		return base + "." + t.Ref.Name
	case *Func:
		// Curried emission — `func(A) func(B) R`, one FuncType
		// nesting per parameter group.
		var b strings.Builder
		for {
			// One FuncType per parameter — matching codegen's
			// funcType, which wraps each param singly regardless
			// of parameter grouping.
			for _, p := range t.Params {
				if GoRepr(p.Type) == "" {
					return ""
				}
				b.WriteString("func(")
				b.WriteString(GoRepr(p.Type))
				b.WriteString(") ")
			}
			if r, ok := Chase(t.Ret).(*Func); ok {
				t = r
				continue
			}
			b.WriteString(GoRepr(t.Ret))
			return b.String()
		}
	case *Tuple:
		parts := make([]string, len(t.Elems))
		for i, el := range t.Elems {
			parts[i] = GoRepr(el)
			if parts[i] == "" {
				return ""
			}
		}
		return fmt.Sprintf("rt.Tuple%d[%s]", len(t.Elems), strings.Join(parts, ", "))
	case *Meta, *Var:
		return ""
	default:
		panic("unreachable")
	}
}

// reprArgs renders a generic instantiation's argument list the way
// the emitted Go name spells it — `[int64, string]`. An argument
// that names no Go type spoils the whole spelling.
func reprArgs(args []Type) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = GoRepr(a)
		if parts[i] == "" {
			return ""
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// cat joins already-rendered pieces; any empty piece (a part that
// names no Go type) spoils the whole spelling.
func cat(parts ...string) string {
	for _, p := range parts {
		if p == "" {
			return ""
		}
	}
	return strings.Join(parts, "")
}

// FlatFuncRepr renders `func(p...) (r...)` the way reflect and a
// Go type switch case spell it: `func(int64) string`,
// `func() (int64, error)`.
func FlatFuncRepr(params, results []Type) string {
	ps := make([]string, len(params))
	for i, p := range params {
		ps[i] = GoRepr(p)
		if ps[i] == "" {
			return ""
		}
	}
	var b strings.Builder
	b.WriteString("func(")
	b.WriteString(strings.Join(ps, ", "))
	b.WriteString(")")
	switch len(results) {
	case 0:
	case 1:
		r := GoRepr(results[0])
		if r == "" {
			return ""
		}
		b.WriteString(" " + r)
	default:
		rs := make([]string, len(results))
		for i, r := range results {
			rs[i] = GoRepr(r)
			if rs[i] == "" {
				return ""
			}
		}
		b.WriteString(" (" + strings.Join(rs, ", ") + ")")
	}
	return b.String()
}
