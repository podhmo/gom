package domain

import (
	"strings"
	"testing"
)

func TestString50CreateCountsRunes(t *testing.T) {
	if got := String50Create(strings.Repeat("あ", 50)); !got.IsOk() {
		t.Fatalf("50 runes should be accepted: %#v", got)
	}
	if got := String50Create(strings.Repeat("あ", 51)); got.IsOk() {
		t.Fatalf("51 runes should be rejected: %#v", got)
	}
}
