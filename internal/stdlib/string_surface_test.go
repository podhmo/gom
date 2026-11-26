package stdlib_test

import (
	"testing"

	"github.com/podhmo/gom/stdlib/option"
	strpkg "github.com/podhmo/gom/stdlib/str"
)

func TestStringRuntimeSurface(t *testing.T) {
	if got := strpkg.Length("é"); got != 2 {
		t.Fatalf("Length(é) = %d, want 2 bytes", got)
	}
	if got := strpkg.Get(1, "é"); got != (option.Option_Some[byte]{V0: 0xa9}) {
		t.Fatalf("Get(1, é) = %#v, want byte 0xa9", got)
	}
	if got := strpkg.Get(2, "é"); got != (option.Option_None[byte]{}) {
		t.Fatalf("Get(2, é) = %#v, want None", got)
	}
	if got := strpkg.Slice(0, 1, "é"); got != "\xc3" {
		t.Fatalf("Slice(0, 1, é) = %q, want first byte", got)
	}
	runes := strpkg.Runes("a\xff")
	if len(runes) != 2 || runes[0] != 'a' || runes[1] != '\uFFFD' {
		t.Fatalf("Runes invalid UTF-8 = %#v", runes)
	}
}
