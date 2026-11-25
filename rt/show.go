package rt

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// TypeOf returns the reflect type of T without depending on a
// representable zero literal. Generated show metadata registrations
// use it for both struct and defined-type representations.
func TypeOf[T any]() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }

type showMeta struct {
	variant bool
	ctor    string
	defined bool
	fields  []string // "" is a positional variant payload slot
}

var showTypes sync.Map   // type-show key -> showMeta
var showUnwraps sync.Map // type-show key -> field representation type

func showTypeKey(t reflect.Type) string {
	name := t.Name()
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	return t.PkgPath() + "\x00" + name
}

// RegisterShowRecord records the Gom field spellings of a record
// representation. Defined is true for the one-field newtype form
// (`type X F`), whose runtime value is the field value itself.
func RegisterShowRecord(t reflect.Type, defined bool, fields ...string) bool {
	key := showTypeKey(t)
	if _, ok := showTypes.Load(key); ok {
		return true
	}
	showTypes.LoadOrStore(key, showMeta{defined: defined, fields: append([]string(nil), fields...)})
	return true
}

// RegisterShowUnwrap records the underlying field representation of a
// one-field record. Runtime reflection can then convert the defined
// value back to that representation and recurse into composite fields
// instead of treating the whole value as opaque.
func RegisterShowUnwrap(t reflect.Type, unwrap reflect.Type) bool {
	key := showTypeKey(t)
	if _, ok := showUnwraps.Load(key); ok {
		return true
	}
	showUnwraps.LoadOrStore(key, unwrap)
	return true
}

// RegisterShowVariant records the constructor and payload spellings
// of one variant representation.
func RegisterShowVariant(t reflect.Type, ctor string, fields ...string) bool {
	key := showTypeKey(t)
	if _, ok := showTypes.Load(key); ok {
		return true
	}
	showTypes.LoadOrStore(key, showMeta{variant: true, ctor: ctor, fields: append([]string(nil), fields...)})
	return true
}

// ShowRuntime renders a type-erased value in the comparison display
// generated gomShow functions use. Unlike fmt's %v it follows the
// runtime representation of Gom composites — comma-separated slices,
// rt tuples, registered records and variants — while unregistered Go
// values keep fmt's opaque rendering.
func ShowRuntime(v any) string {
	return showRuntimeValue(reflect.ValueOf(v), 0)
}

// ShowDisplay renders a top-level runtime-fallback splice. It shares
// ShowRuntime's composite canonicalization, but a root string keeps
// interpolation's unquoted scalar form.
func ShowDisplay(v any) string {
	rv := reflect.ValueOf(v)
	if rv.IsValid() && rv.Kind() == reflect.String {
		if _, ok := lookupShowMeta(rv.Type()); !ok {
			return rv.String()
		}
	}
	return ShowRuntime(v)
}

func showRuntimeOperand(v reflect.Value, depth int) string {
	s := showRuntimeValue(v, depth)
	if !v.IsValid() {
		return s
	}
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	if meta, ok := lookupShowMeta(v.Type()); ok {
		if meta.variant || v.Kind() == reflect.Struct || meta.defined {
			return "(" + s + ")"
		}
	}
	if isTupleType(v.Type()) || (v.Kind() == reflect.Struct && v.Type().Name() == "" && v.Type().NumField() == 0) {
		return s
	}
	if (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) && opaqueNamedSliceType(v.Type()) {
		return "(" + s + ")"
	}
	switch v.Kind() {
	case reflect.Struct, reflect.Func, reflect.Chan, reflect.Map, reflect.Pointer, reflect.UnsafePointer:
		return "(" + s + ")"
	}
	return s
}

func lookupShowMeta(t reflect.Type) (showMeta, bool) {
	m, ok := showTypes.Load(showTypeKey(t))
	if !ok {
		return showMeta{}, false
	}
	return m.(showMeta), true
}

func showRuntimeValue(v reflect.Value, depth int) string {
	if depth > 64 {
		return "<cycle>"
	}
	if !v.IsValid() {
		return fmt.Sprint(nil)
	}
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "nil"
		}
		return showRuntimeValue(v.Elem(), depth)
	}
	t := v.Type()
	if meta, ok := lookupShowMeta(t); ok {
		if meta.variant {
			return showRuntimeVariant(v, meta, depth)
		}
		return showRuntimeRecord(v, meta, depth)
	}
	if v.CanInterface() {
		if err, ok := v.Interface().(error); ok {
			return ShowError(err)
		}
	}
	if isTupleType(t) {
		var sb strings.Builder
		sb.WriteByte('(')
		for i := 0; i < v.NumField(); i++ {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(showRuntimeOperand(v.Field(i), depth+1))
		}
		sb.WriteByte(')')
		return sb.String()
	}
	if isOptionVariantType(t) {
		if strings.HasSuffix(showTypeKey(t), "Option_None") {
			return "None"
		}
		return "Some " + showRuntimeOperand(v.Field(0), depth+1)
	}
	if isResultType(t) {
		tag := v.MethodByName("Tag")
		if tag.IsValid() && tag.Type().NumIn() == 0 && tag.Type().NumOut() == 1 {
			out := tag.Call(nil)[0].Uint()
			switch out {
			case 1:
				return "Ok " + showRuntimeOperand(v.FieldByName("Value"), depth+1)
			case 2:
				return "Err " + showRuntimeErrorOperand(v.FieldByName("Error"), depth+1)
			}
		}
		return fmt.Sprintf("%v", v.Interface())
	}

	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'g', -1, 32)
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.String:
		return strconv.Quote(v.String())
	case reflect.Slice, reflect.Array:
		if opaqueNamedSliceType(t) {
			return fmt.Sprintf("%v", v.Interface())
		}
		return showRuntimeSlice(v, depth+1)
	case reflect.Struct:
		if t.Name() == "" && t.NumField() == 0 {
			return "()"
		}
	}
	return fmt.Sprintf("%v", v.Interface())
}

func opaqueNamedSliceType(t reflect.Type) bool {
	return t.Name() != "" && !(t.PkgPath() == "github.com/podhmo/gom/stdlib/list" && (strings.HasPrefix(t.Name(), "List") || strings.HasPrefix(t.Name(), "View")))
}

func showRuntimeSlice(v reflect.Value, depth int) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i := 0; i < v.Len(); i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(showRuntimeOperand(v.Index(i), depth+1))
	}
	sb.WriteByte(']')
	return sb.String()
}

func showRuntimeScalar(v reflect.Value) (string, bool) {
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(v.Uint(), 10), true
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'g', -1, 32), true
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64), true
	case reflect.String:
		return strconv.Quote(v.String()), true
	}
	return "", false
}

func showRuntimeRecord(v reflect.Value, meta showMeta, depth int) string {
	if meta.defined {
		if len(meta.fields) == 1 {
			if m, ok := showUnwraps.Load(showTypeKey(v.Type())); ok {
				if uv, ok := convertRuntimeValue(v, m.(reflect.Type)); ok {
					return "{ " + meta.fields[0] + " = " + showRuntimeOperand(uv, depth+1) + " }"
				}
			}
			if scalar, ok := showRuntimeScalar(v); ok {
				return "{ " + meta.fields[0] + " = " + scalar + " }"
			}
			if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
				return "{ " + meta.fields[0] + " = " + showRuntimeSlice(v, depth+1) + " }"
			}
			return "{ " + meta.fields[0] + " = " + fmt.Sprintf("%v", v.Interface()) + " }"
		}
		return fmt.Sprintf("%v", v.Interface())
	}
	var sb strings.Builder
	sb.WriteString("{ ")
	for i, name := range meta.fields {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(name)
		sb.WriteString(" = ")
		if i < v.NumField() {
			sb.WriteString(showRuntimeOperand(v.Field(i), depth+1))
		} else {
			sb.WriteString("<missing>")
		}
	}
	sb.WriteString(" }")
	return sb.String()
}

func convertRuntimeValue(v reflect.Value, target reflect.Type) (out reflect.Value, ok bool) {
	defer func() {
		if recover() != nil {
			out, ok = reflect.Value{}, false
		}
	}()
	if !v.Type().ConvertibleTo(target) {
		return reflect.Value{}, false
	}
	return v.Convert(target), true
}

func showRuntimeVariant(v reflect.Value, meta showMeta, depth int) string {
	var sb strings.Builder
	sb.WriteString(meta.ctor)
	for i, name := range meta.fields {
		sb.WriteByte(' ')
		if name != "" {
			sb.WriteByte('~')
			sb.WriteString(name)
			sb.WriteString(": ")
		}
		if i < v.NumField() {
			sb.WriteString(showRuntimeOperand(v.Field(i), depth+1))
		} else {
			sb.WriteString("<missing>")
		}
	}
	return sb.String()
}

// showRuntimeErrorOperand renders the statically error-typed payload of
// the builtin Result representation. A record may implement error while
// also carrying show metadata; inside Result's Error slot the interface
// contract is the observable representation, so the Error() text wins.
func showRuntimeErrorOperand(v reflect.Value, depth int) string {
	staticallyError := v.IsValid() && v.Kind() == reflect.Interface && v.Type().Implements(reflect.TypeOf((*error)(nil)).Elem())
	for v.IsValid() && v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	if staticallyError && v.IsValid() && v.CanInterface() {
		if err, ok := v.Interface().(error); ok {
			return ShowError(err)
		}
	}
	return showRuntimeOperand(v, depth)
}

func isTupleType(t reflect.Type) bool {
	if t.PkgPath() == "github.com/podhmo/gom/rt" && strings.HasPrefix(t.Name(), "Tuple") {
		return true
	}
	// Tuples wider than Tuple8 lower to an anonymous struct with
	// positional V<N> fields. That anonymous representation is the
	// tuple shape; unregistered named structs stay opaque.
	if t.Kind() != reflect.Struct || t.Name() != "" || t.NumField() < 2 {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Name != "V"+strconv.Itoa(i) {
			return false
		}
	}
	return true
}

func isOptionVariantType(t reflect.Type) bool {
	return t.PkgPath() == "github.com/podhmo/gom/stdlib/option" && strings.HasPrefix(t.Name(), "Option_")
}

func isResultType(t reflect.Type) bool {
	name := t.Name()
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	return t.PkgPath() == "github.com/podhmo/gom/stdlib/result" && name == "Result"
}
