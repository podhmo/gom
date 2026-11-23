package assets_test

// Execution assertions for the @extern let surface — the generated
// package hands an embedded asset and a Go-supplied variable up to
// this side through exported functions.

import (
	"testing"

	"example.com/gocall/assets"
)

func TestEmbedAndExternVar(t *testing.T) {
	if got := assets.GreetingText(struct{}{}); got != "hello from the embedded file\n" {
		t.Errorf("GreetingText() = %q, want the embedded hello.txt contents", got)
	}
	if got := assets.TokenText(struct{}{}); got != "tok-supplied-by-go" {
		t.Errorf("TokenText() = %q, want the Go-side var", got)
	}
}
