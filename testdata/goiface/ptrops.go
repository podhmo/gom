package goiface

// Pointer helpers exercising the `Ptr` module's operation surface
// (spec section 8.3): the shapes the P3-7 audit found unusable —
// an out-pointer taken through `any` (errors.As/json.Unmarshal
// style), a `*T` returned from Go, and an optional `*T` parameter
// that accepts nil.

// FillPtr writes v through an out-pointer passed as `any` — the
// `errors.As` target shape: the caller supplies `Ptr.new ()`, the
// callee mutates the pointee, and `Ptr.get` reads it back.
func FillPtr(dst any, v int64) bool {
	p, ok := dst.(*int64)
	if !ok {
		return false
	}
	*p = v
	return true
}

// Int64Box returns a fresh `*int64` — the `flag.Int64` shape: a
// `*T` result whose value is read back with `Ptr.get`.
func Int64Box() *int64 {
	v := int64(41)
	return &v
}

// Config stands in for optional-config parameter structs
// (`tls.Config` style).
type Config struct{ Verbose bool }

// UsesConfig reports whether its optional `*Config` parameter was
// nil — the `tls.Dial`-style shape `Ptr.nil ()` feeds.
func UsesConfig(c *Config) bool { return c == nil }
