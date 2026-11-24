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
				return "[]" + GoRepr(t.Args[0])
			case t.Name == "Map" && len(t.Args) == 2:
				return "map[" + GoRepr(t.Args[0]) + "]" + GoRepr(t.Args[1])
			case t.Name == "Int":
				return "int"
			case t.Name == "Any":
				return "any"
			}
			return ""
		}
		if t.Qual != "" {
			return t.Qual + "." + t.Name + reprArgs(t.Args)
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
			return "list.List[" + GoRepr(t.Args[0]) + "]"
		case t.Name == "Option" && len(t.Args) == 1:
			return "option.Option[" + GoRepr(t.Args[0]) + "]"
		case t.Name == "Result" && len(t.Args) == 2:
			return "result.Result[" + GoRepr(t.Args[0]) + ", " + GoRepr(t.Args[1]) + "]"
		case t.Name == "Task" && len(t.Args) == 1:
			return "rt.Task[" + GoRepr(t.Args[0]) + "]"
		case t.Name == "View" && len(t.Args) == 1:
			return "list.View[" + GoRepr(t.Args[0]) + "]"
		case (t.Name == "Ptr" || t.Name == "Ref") && len(t.Args) == 1:
			return "*" + GoRepr(t.Args[0])
		case t.Name == "Array" && len(t.Args) == 1:
			return "[]" + GoRepr(t.Args[0])
		case t.Name == "Chan" && len(t.Args) == 1:
			return "chan " + GoRepr(t.Args[0])
		case t.Name == "RecvChan" && len(t.Args) == 1:
			return "<-chan " + GoRepr(t.Args[0])
		case t.Name == "SendChan" && len(t.Args) == 1:
			return "chan<- " + GoRepr(t.Args[0])
		}
		// Declared record/sum — nominal: asserts only to itself.
		// A generic instantiation names its arguments too, as the
		// emitted `Box[int64]` does (`Box Int` and `Box String`
		// assert differently).
		return t.Name + reprArgs(t.Args)
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
		}
		return fmt.Sprintf("rt.Tuple%d[%s]", len(t.Elems), strings.Join(parts, ", "))
	case *Meta, *Var:
		return ""
	default:
		panic("unreachable")
	}
}

// reprArgs renders a generic instantiation's argument list the way
// the emitted Go name spells it — `[int64, string]`.
func reprArgs(args []Type) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = GoRepr(a)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
