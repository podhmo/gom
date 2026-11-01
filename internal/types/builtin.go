package types

// Builtin types of spec section 3.1, as nullary constructors. They
// are shared for convenience only — Equal is structural, so
// &Con{Name: "Int"} from anywhere equals Int. They all live in the
// builtin scope, i.e. Qual == "" (see Con).
var (
	Unit   Type = &Con{Name: "Unit"}
	Bool   Type = &Con{Name: "Bool"}
	Int    Type = &Con{Name: "Int"}
	Int32  Type = &Con{Name: "Int32"}
	Int64  Type = &Con{Name: "Int64"}
	Float  Type = &Con{Name: "Float"}
	Char   Type = &Con{Name: "Char"}
	String Type = &Con{Name: "String"}

	// GoError is Go's `error` (docs/spec.md sections 3.1 and 8.3).
	GoError Type = &Con{Name: "GoError"}
)

// Constructor helpers for the builtin type constructors of spec
// sections 3.1 and 8.3.

// List returns `List t` — the immutable list.
func List(t Type) Type { return &Con{Name: "List", Args: []Type{t}} }

// Option returns `Option t`.
func Option(t Type) Type { return &Con{Name: "Option", Args: []Type{t}} }

// Result returns `Result t e`.
func Result(t, e Type) Type { return &Con{Name: "Result", Args: []Type{t, e}} }

// Ref returns `Ref t` — the mutable reference cell.
func Ref(t Type) Type { return &Con{Name: "Ref", Args: []Type{t}} }

// Array returns `Array t` — the mutable array.
func Array(t Type) Type { return &Con{Name: "Array", Args: []Type{t}} }

// Ptr returns `Ptr t` — Go's `*T` (docs/spec.md section 8.3).
func Ptr(t Type) Type { return &Con{Name: "Ptr", Args: []Type{t}} }

// Chan returns `Chan t` — the bidirectional channel.
func Chan(t Type) Type { return &Con{Name: "Chan", Args: []Type{t}} }

// RecvChan returns `RecvChan t` — Go's `<-chan T`.
func RecvChan(t Type) Type { return &Con{Name: "RecvChan", Args: []Type{t}} }

// SendChan returns `SendChan t` — Go's `chan<- T`.
func SendChan(t Type) Type { return &Con{Name: "SendChan", Args: []Type{t}} }

// Task returns `Task t` — the spawned-task handle of spec section 10.
func Task(t Type) Type { return &Con{Name: "Task", Args: []Type{t}} }

// GoSlice returns `Go.Slice t` — a Go-owned mutable slice
// (docs/spec.md section 8.5).
func GoSlice(t Type) Type { return &Con{Qual: "Go", Name: "Slice", Args: []Type{t}} }

// GoMap returns `Go.Map k v` — the opaque map type of spec
// section 8.3.
func GoMap(k, v Type) Type {
	return &Con{Qual: "Go", Name: "Map", Args: []Type{k, v}}
}

// GoNamed returns the Go-interop handle for the package-qualified
// named type or interface `path.Name` — `GoNamed("io", "Reader")`.
func GoNamed(path, name string) Type {
	return &Go{Ref: NamedRef{Path: path, Name: name}}
}

// P returns a positional function parameter of the given type —
// helper for writing Func literals.
func P(t Type) FuncParam { return FuncParam{Type: t} }

// LP returns a labeled function parameter `~label: t` — helper for
// writing Func literals.
func LP(label string, t Type) FuncParam { return FuncParam{Label: label, Type: t} }

// Fn returns the function type `p1 -> ... -> ret` in the canonical
// flat form: when ret is itself a *Func its parameters are absorbed,
// so the result's Ret is never a *Func (see Func for the invariant).
// A bound Meta is deliberately not chased — the meta node is
// inference's to keep.
func Fn(ret Type, params ...FuncParam) *Func {
	for g, ok := ret.(*Func); ok; g, ok = ret.(*Func) {
		ps := make([]FuncParam, 0, len(params)+len(g.Params))
		ps = append(ps, params...)
		params = append(ps, g.Params...)
		ret = g.Ret
	}
	return &Func{Params: params, Ret: ret}
}

// Tup returns the tuple type `(t, u, ...)`.
func Tup(elems ...Type) Type { return &Tuple{Elems: elems} }
