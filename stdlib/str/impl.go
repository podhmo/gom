package str

import (
	"github.com/podhmo/gom/stdlib/list"
	"github.com/podhmo/gom/stdlib/option"
)

// Length is `@extern fn length` — a byte count, not a rune count.
func Length(s string) int64 { return int64(len(s)) }

// Get is `@extern fn get` — the byte at byte index n.
func Get(n int64, s string) option.Option[byte] {
	if n < 0 || n >= int64(len(s)) {
		return option.Option_None[byte]{}
	}
	return option.Option_Some[byte]{V0: s[n]}
}

// Slice is `@extern fn slice` — a byte-range substring. Invalid
// bounds panic exactly like Go's s[lo:hi].
func Slice(lo, hi int64, s string) string { return s[lo:hi] }

// Runes is `@extern fn runes` — explicit UTF-8 decoding to Gom
// Chars. Go's conversion replaces invalid byte sequences with U+FFFD.
func Runes(s string) list.List[rune] { return list.List[rune]([]rune(s)) }
