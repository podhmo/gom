// Package acomposite is the hand-written Go that option (a) — a
// composite literal for named Go struct types — would emit for the
// Gom mock in mock.gom.txt. It exists to check that the generated
// shape compiles and produces the same observable client as
// examples/gist-cli/host.NewClient.
package acomposite

import (
	"net/http"
	"time"
)

// redirectPolicy is what `Go.func2 redirect_policy` would emit: a
// Gom (A, B) -> Result curried fn becomes a Go func returning error.
func redirectPolicy(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

// NewClient is what `Ptr.make (http.Client { ... })` lowers to: a
// keyed composite literal whose unlisted fields keep Go zero values,
// addressed into a fresh pointer (Ptr.make = &copy).
func NewClient() *http.Client {
	return &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: redirectPolicy,
	}
}
