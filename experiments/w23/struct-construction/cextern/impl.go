package cextern

import (
	"net/http"
	"time"
)

// NewClient is the hand-written body of the `@extern fn new_client`
// signature in a.gom. Option (c') needs no new language surface —
// the Go code just lives beside the generated package instead of in
// a separate `host` package. The signature must match what a.go
// calls: `func(*http.Request, []*http.Request) error`.
func NewClient(checkRedirect func(*http.Request, []*http.Request) error) *http.Client {
	return &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: checkRedirect,
	}
}
